package inventory

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestDMSSyncWorker_SyncOnce_And_Loop(t *testing.T) {
	callCount := 0
	server := newInventoryTLSServer(t, func(ifNoneMatch string) (int, string, []Vehicle) {
		callCount++
		if ifNoneMatch == "etag-initial" {
			return http.StatusNotModified, "etag-initial", nil
		}
		return http.StatusOK, "etag-initial", []Vehicle{
			{
				ID:          "veh-sync-1",
				Brand:       "Toyota",
				Model:       "Corolla",
				Year:        2022,
				PriceCOP:    85000000,
				Status:      "available",
				OfficialURL: "https://buyacar.com.co/vehiculos/corolla-2022",
			},
		}
	})
	defer server.Close()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("failed parsing server URL: %v", err)
	}

	fetcher, err := NewHTTPFetcher(server.URL, []string{u.Hostname(), "buyacar.com.co"}, 5*time.Second)
	if err != nil {
		t.Fatalf("failed creating HTTPFetcher: %v", err)
	}
	fetcher.client = server.Client() // usar cliente con TLS del servidor de prueba

	cache := NewCache(15, 30)
	worker := NewDMSSyncWorker(fetcher, cache, 50*time.Millisecond)

	ctx := context.Background()

	// 1. Primera sincronizacion manual: debe recibir 200 OK y actualizar cache
	notModified, err := worker.SyncOnce(ctx)
	if err != nil {
		t.Fatalf("unexpected error on SyncOnce: %v", err)
	}
	if notModified {
		t.Fatalf("expected initial sync to not be 304 Not Modified")
	}

	v, ok := cache.GetVehicle("veh-sync-1")
	if !ok || v.Model != "Corolla" {
		t.Fatalf("expected vehicle veh-sync-1 in cache, got ok=%v", ok)
	}

	// 2. Segunda sincronizacion: debe enviar If-None-Match y recibir 304 Not Modified
	notModified, err = worker.SyncOnce(ctx)
	if err != nil {
		t.Fatalf("unexpected error on second sync: %v", err)
	}
	if !notModified {
		t.Fatalf("expected second sync to be 304 Not Modified")
	}

	stats := worker.Stats()
	if stats.SyncCount != 2 || stats.NotModifiedCount != 1 {
		t.Fatalf("expected 2 syncs and 1 notModified, got %+v", stats)
	}

	// 3. Iniciar loop de sincronizacion en segundo plano
	loopCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := worker.Start(loopCtx); err != nil {
		t.Fatalf("failed starting worker: %v", err)
	}
	if !worker.IsRunning() {
		t.Fatalf("expected worker to be running")
	}

	// Debe rechazar segundo Start si ya corre
	if err := worker.Start(loopCtx); err != ErrWorkerAlreadyRunning {
		t.Fatalf("expected ErrWorkerAlreadyRunning, got %v", err)
	}

	// Esperar un par de ticks
	time.Sleep(120 * time.Millisecond)

	if err := worker.Stop(); err != nil {
		t.Fatalf("failed stopping worker: %v", err)
	}
	if worker.IsRunning() {
		t.Fatalf("expected worker to not be running after Stop")
	}
}
