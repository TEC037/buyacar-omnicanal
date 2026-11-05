package inventory

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrWorkerAlreadyRunning = errors.New("dms_sync: worker is already running")
	ErrWorkerNotRunning     = errors.New("dms_sync: worker is not running")
)

// DMSSyncStats expone metricas de sincronizacion periodica con el DMS.
type DMSSyncStats struct {
	Running          bool          `json:"running"`
	Interval         time.Duration `json:"interval"`
	SyncCount        int64         `json:"sync_count"`
	NotModifiedCount int64         `json:"not_modified_count"`
	ErrorCount       int64         `json:"error_count"`
	LastSync         time.Time     `json:"last_sync"`
	LastError        string        `json:"last_error,omitempty"`
}

// DMSSyncWorker ejecuta el ciclo en segundo plano de sincronizacion de inventario DMS
// con validacion condicional por ETag (< 30 min) y manejo de 304 Not Modified.
type DMSSyncWorker struct {
	mu               sync.RWMutex
	fetcher          *HTTPFetcher
	cache            *Cache
	interval         time.Duration
	stopCh           chan struct{}
	doneCh           chan struct{}
	running          bool
	syncCount        int64
	notModifiedCount int64
	errorCount       int64
	lastSync         time.Time
	lastError        error
}

// NewDMSSyncWorker crea una nueva instancia de DMSSyncWorker asegurando una ventana < 30 minutos.
func NewDMSSyncWorker(fetcher *HTTPFetcher, cache *Cache, interval time.Duration) *DMSSyncWorker {
	if interval <= 0 || interval >= 30*time.Minute {
		interval = 15 * time.Minute
	}
	return &DMSSyncWorker{
		fetcher:  fetcher,
		cache:    cache,
		interval: interval,
	}
}

// Start inicia la goroutine en segundo plano para sincronizaciones periodicas.
func (w *DMSSyncWorker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return ErrWorkerAlreadyRunning
	}
	w.running = true
	w.stopCh = make(chan struct{})
	w.doneCh = make(chan struct{})
	interval := w.interval
	w.mu.Unlock()

	go w.runLoop(ctx, interval)
	return nil
}

func (w *DMSSyncWorker) runLoop(ctx context.Context, interval time.Duration) {
	defer close(w.doneCh)

	// Ejecutar primera sincronizacion de inmediato
	_, _ = w.SyncOnce(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.mu.Lock()
			w.running = false
			w.mu.Unlock()
			return
		case <-w.stopCh:
			return
		case <-ticker.C:
			_, _ = w.SyncOnce(ctx)
		}
	}
}

// Stop detiene de forma limpia el bucle en segundo plano esperando su finalizacion.
func (w *DMSSyncWorker) Stop() error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return ErrWorkerNotRunning
	}
	w.running = false
	close(w.stopCh)
	w.mu.Unlock()

	<-w.doneCh
	return nil
}

// SyncOnce ejecuta un ciclo unico de sincronizacion, actualizando contadores y metricas.
func (w *DMSSyncWorker) SyncOnce(ctx context.Context) (bool, error) {
	if w.fetcher == nil || w.cache == nil {
		return false, errors.New("dms_sync: fetcher or cache is nil")
	}

	now := time.Now().UTC()
	notModified, err := w.fetcher.Sync(ctx, w.cache, now)

	w.mu.Lock()
	defer w.mu.Unlock()

	w.lastSync = now
	if err != nil {
		w.errorCount++
		w.lastError = err
		return false, err
	}

	w.syncCount++
	w.lastError = nil
	if notModified {
		w.notModifiedCount++
	}
	return notModified, nil
}

// Stats retorna una fotografia thread-safe del estado y metricas del sincronizador DMS.
func (w *DMSSyncWorker) Stats() DMSSyncStats {
	w.mu.RLock()
	defer w.mu.RUnlock()

	lastErrStr := ""
	if w.lastError != nil {
		lastErrStr = w.lastError.Error()
	}

	return DMSSyncStats{
		Running:          w.running,
		Interval:         w.interval,
		SyncCount:        w.syncCount,
		NotModifiedCount: w.notModifiedCount,
		ErrorCount:       w.errorCount,
		LastSync:         w.lastSync,
		LastError:        lastErrStr,
	}
}

// IsRunning indica si el worker se encuentra en ejecucion.
func (w *DMSSyncWorker) IsRunning() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.running
}
