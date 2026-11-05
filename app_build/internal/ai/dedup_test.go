package ai

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeduplicator_CoalescesConcurrentCalls(t *testing.T) {
	dedup := NewDeduplicator()

	var executionCount atomic.Int32
	var wg sync.WaitGroup

	const concurrency = 25
	results := make([]ExecutionResult, concurrency)
	sharedFlags := make([]bool, concurrency)

	// Iniciar 25 llamadas concurrentes para la misma clave "hilux_price_query"
	startGate := make(chan struct{})

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-startGate

			res, shared, err := dedup.Do("hilux_price_query", func() (ExecutionResult, error) {
				executionCount.Add(1)
				// Simular un pequeno retardo de red/inferencia
				time.Sleep(30 * time.Millisecond)
				return ExecutionResult{
					ProviderUsed: "ollama",
					Output: StructuredOutput{
						ProposedReply: "Toyota Hilux 2022 en $185.000.000 COP",
					},
					InputTokens:  120,
					OutputTokens: 45,
				}, nil
			})

			if err != nil {
				t.Errorf("call %d returned error: %v", idx, err)
			}
			results[idx] = res
			sharedFlags[idx] = shared
		}(i)
	}

	close(startGate)
	wg.Wait()

	// La funcion de inferencia debio ejecutarse exactamente 1 vez
	if count := executionCount.Load(); count != 1 {
		t.Fatalf("expected exactly 1 execution, got %d", count)
	}

	// Al menos concurrency-1 llamadas debieron recibir shared == true
	sharedCount := 0
	for _, shared := range sharedFlags {
		if shared {
			sharedCount++
		}
	}
	if sharedCount != concurrency-1 {
		t.Fatalf("expected %d shared results, got %d", concurrency-1, sharedCount)
	}

	// Todos los resultados deben ser identicos
	for i, r := range results {
		if r.Output.ProposedReply != "Toyota Hilux 2022 en $185.000.000 COP" {
			t.Fatalf("call %d received unexpected reply: %s", i, r.Output.ProposedReply)
		}
	}

	total, deduped := dedup.Stats()
	if total != concurrency || deduped != int64(concurrency-1) {
		t.Fatalf("unexpected stats: total=%d, deduped=%d", total, deduped)
	}
}

func TestDeduplicator_DoWithContext_Cancellation(t *testing.T) {
	dedup := NewDeduplicator()

	started := make(chan struct{})
	finishExecution := make(chan struct{})

	// Primera llamada de larga duracion
	go func() {
		_, _, _ = dedup.DoWithContext(context.Background(), "slow_key", func(ctx context.Context) (ExecutionResult, error) {
			close(started)
			<-finishExecution
			return ExecutionResult{ProviderUsed: "gemini"}, nil
		})
	}()

	<-started

	// Segunda llamada concurrente pero con un timeout muy corto
	ctxTimeout, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, shared, err := dedup.DoWithContext(ctxTimeout, "slow_key", func(ctx context.Context) (ExecutionResult, error) {
		return ExecutionResult{}, errors.New("should not be called")
	})

	if !shared {
		t.Fatalf("expected second call to be shared")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}

	close(finishExecution)
}
