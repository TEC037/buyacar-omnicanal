package ai

import (
	"context"
	"sync"
	"sync/atomic"
)

// flightCall almacena el estado de una ejecucion en curso protegida por WaitGroup.
type flightCall struct {
	wg      sync.WaitGroup
	val     ExecutionResult
	err     error
	waiters int
}

// Deduplicator implementa el patron Single-Flight para deduplicar consultas identicas concurrentes,
// asegurando que multiples llamadas simultaneas con el mismo hash o clave compartan exactamente una sola
// ejecucion del proveedor de inferencia, ahorrando tokens y reduciendo latencia.
type Deduplicator struct {
	mu         sync.Mutex
	calls      map[string]*flightCall
	totalCalls atomic.Int64
	dedupCalls atomic.Int64
}

// NewDeduplicator crea una nueva instancia thread-safe del deduplicador de consultas.
func NewDeduplicator() *Deduplicator {
	return &Deduplicator{
		calls: make(map[string]*flightCall),
	}
}

// Do ejecuta o agrupa la llamada fn bajo la clave proporcionada.
// Si ya existe una ejecucion en vuelo con esa clave, el llamador espera el resultado de la primera (shared = true).
// Si no existe, este llamador ejecuta fn y notifica a todos los concurrentes (shared = false).
func (d *Deduplicator) Do(key string, fn func() (ExecutionResult, error)) (ExecutionResult, bool, error) {
	d.totalCalls.Add(1)

	d.mu.Lock()
	if c, ok := d.calls[key]; ok {
		c.waiters++
		d.dedupCalls.Add(1)
		d.mu.Unlock()
		c.wg.Wait()
		return c.val, true, c.err
	}

	c := &flightCall{}
	c.wg.Add(1)
	d.calls[key] = c
	d.mu.Unlock()

	c.val, c.err = fn()

	d.mu.Lock()
	delete(d.calls, key)
	d.mu.Unlock()

	c.wg.Done()

	return c.val, false, c.err
}

// DoWithContext ejecuta o agrupa la llamada fn respetando el contexto del llamador.
func (d *Deduplicator) DoWithContext(ctx context.Context, key string, fn func(ctx context.Context) (ExecutionResult, error)) (ExecutionResult, bool, error) {
	d.totalCalls.Add(1)

	d.mu.Lock()
	if c, ok := d.calls[key]; ok {
		c.waiters++
		d.dedupCalls.Add(1)
		d.mu.Unlock()

		done := make(chan struct{})
		go func() {
			c.wg.Wait()
			close(done)
		}()

		select {
		case <-ctx.Done():
			return ExecutionResult{}, true, ctx.Err()
		case <-done:
			return c.val, true, c.err
		}
	}

	c := &flightCall{}
	c.wg.Add(1)
	d.calls[key] = c
	d.mu.Unlock()

	c.val, c.err = fn(ctx)

	d.mu.Lock()
	delete(d.calls, key)
	d.mu.Unlock()

	c.wg.Done()

	return c.val, false, c.err
}

// Stats retorna el total de llamadas recibidas y la cantidad de llamadas deduplicadas.
func (d *Deduplicator) Stats() (total, deduplicated int64) {
	return d.totalCalls.Load(), d.dedupCalls.Load()
}

// Reset reinicia los contadores y limpia el mapa de llamadas.
func (d *Deduplicator) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = make(map[string]*flightCall)
	d.totalCalls.Store(0)
	d.dedupCalls.Store(0)
}
