package server

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"buyacar.com.co/omnicanal/internal/channel"
	"buyacar.com.co/omnicanal/internal/escalate"
	"buyacar.com.co/omnicanal/internal/obs"
)

func TestHTTPServer_Lifecycle(t *testing.T) {
	escQ := escalate.NewQueue()
	res := channel.NewMemoryResilienceTracker(escQ)
	rc := channel.NewMemoryRateController(channel.DefaultRateConfig())
	obsColl := obs.NewMetricsCollector()

	exporter := NewMetricsExporter(obsColl, escQ, res, rc, "SIMULATION", []string{"whatsapp"})
	dashboard := NewDashboardHandler(escQ, res, rc, exporter, "SIMULATION", []string{"whatsapp"})

	// Usar puerto 0 para que el sistema operativo asigne un puerto libre
	cfg := Config{
		Port:            0,
		Host:            "127.0.0.1",
		OperatingMode:   "SIMULATION",
		EnableDashboard: true,
		EnableMetrics:   true,
	}

	srv := NewServer(cfg, dashboard, exporter)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- srv.Start(ctx)
	}()

	// Esperar a que el servidor esté activo
	time.Sleep(50 * time.Millisecond)

	serverURL := srv.URL()
	if !strings.HasPrefix(serverURL, "http://127.0.0.1:") {
		t.Fatalf("unexpected server URL: %s", serverURL)
	}

	// 1. Probar GET / (Dashboard UI)
	resp, err := http.Get(serverURL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on /, got: %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "BuyaCAR Supervisor Console") {
		t.Fatalf("expected dashboard body to contain console title")
	}

	// 2. Probar GET /metrics (Prometheus Exporter)
	respM, errM := http.Get(serverURL + "/metrics")
	if errM != nil {
		t.Fatalf("GET /metrics failed: %v", errM)
	}
	bodyM, _ := io.ReadAll(respM.Body)
	respM.Body.Close()
	if respM.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on /metrics, got: %d", respM.StatusCode)
	}
	if !strings.Contains(string(bodyM), "buyacar_fastpath_latency_p99") {
		t.Fatalf("expected /metrics to contain buyacar_fastpath_latency_p99")
	}

	// 3. Probar 1-Click Draft Approve a través del servidor activo
	draftID := "draft-live-01"
	dashboard.RegisterDraft(DraftItem{DraftID: draftID, Status: "pending"})

	postResp, postErr := http.Post(serverURL+"/api/draft/approve?id="+draftID, "application/json", nil)
	if postErr != nil || postResp.StatusCode != http.StatusOK {
		t.Fatalf("live 1-click draft approve failed: err=%v, status=%d", postErr, postResp.StatusCode)
	}
	postResp.Body.Close()

	status, ok := dashboard.GetDraftStatus(draftID)
	if !ok || status != "approved" {
		t.Fatalf("expected draft status approved, got: %s (ok=%v)", status, ok)
	}

	// 4. Probar Channel Unpause a través del servidor activo
	res.RecordFailure("whatsapp", "err")
	res.RecordFailure("whatsapp", "err")
	res.RecordFailure("whatsapp", "err")
	if !res.IsPaused("whatsapp") {
		t.Fatalf("expected whatsapp paused")
	}

	unpResp, unpErr := http.Post(serverURL+"/api/channel/unpause?channel=whatsapp", "application/json", nil)
	if unpErr != nil || unpResp.StatusCode != http.StatusOK {
		t.Fatalf("live channel unpause failed: err=%v, status=%d", unpErr, unpResp.StatusCode)
	}
	unpResp.Body.Close()

	if res.IsPaused("whatsapp") {
		t.Fatalf("expected whatsapp to be unpaused")
	}

	// 5. Apagado ordenado
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("server shutdown failed: %v", err)
	}

	cancel()
	_ = <-errChan
}

func TestHTTPServer_ContextCancellationShutdown(t *testing.T) {
	cfg := Config{
		Port:            0,
		Host:            "127.0.0.1",
		OperatingMode:   "SIMULATION",
		EnableDashboard: true,
	}

	srv := NewServer(cfg, NewDashboardHandler(nil, nil, nil, nil, "SIMULATION", nil), nil)

	ctx, cancel := context.WithCancel(context.Background())

	errChan := make(chan error, 1)
	go func() {
		errChan <- srv.Start(ctx)
	}()

	time.Sleep(30 * time.Millisecond)

	// Cancelar contexto directamente
	cancel()

	select {
	case err := <-errChan:
		if err != nil && err != context.Canceled {
			t.Fatalf("expected context.Canceled or nil, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for server to stop on context cancellation")
	}
}

func TestHTTPServer_DoubleStartError(t *testing.T) {
	cfg := Config{Port: 0, Host: "127.0.0.1"}
	srv := NewServer(cfg, NewDashboardHandler(nil, nil, nil, nil, "SIMULATION", nil), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = srv.Start(ctx)
	}()

	time.Sleep(30 * time.Millisecond)
	defer func() {
		_ = srv.Shutdown(context.Background())
	}()

	err := srv.Start(ctx)
	if err == nil {
		t.Fatalf("expected error when starting already running server")
	}
}
