package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"buyacar.com.co/omnicanal/internal/channel"
	"buyacar.com.co/omnicanal/internal/escalate"
	"buyacar.com.co/omnicanal/internal/obs"
)

func TestMetricsExporter_PrometheusFormat(t *testing.T) {
	escQ := escalate.NewQueue()
	escQ.Enqueue("whatsapp", "conv-01", "pricing_question", "Precio hilux 2022", time.Now().UTC())

	res := channel.NewMemoryResilienceTracker(escQ)
	res.RecordFailure("whatsapp", "timeout")
	res.RecordFailure("whatsapp", "timeout")

	rc := channel.NewMemoryRateController(channel.DefaultRateConfig())
	obsColl := obs.NewMetricsCollector()
	obsColl.RecordFastPathLatency(0.00259) // 2.59 microseconds in ms

	exporter := NewMetricsExporter(
		obsColl,
		escQ,
		res,
		rc,
		string(channel.ModeSimulation),
		[]string{"whatsapp", "mercadolibre"},
	)
	exporter.SetDraftsProvider(func() int { return 3 })

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	exporter.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got: %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/plain") {
		t.Fatalf("expected text/plain Content-Type, got: %s", contentType)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)

	// Verify required Prometheus metrics
	expectedMetrics := []string{
		"buyacar_fastpath_latency_p99",
		"buyacar_fastpath_latency_p95",
		"buyacar_cache_hits_total",
		"buyacar_cache_misses_total",
		"buyacar_escalate_queue_size 1",
		"buyacar_drafts_pending 3",
		`buyacar_operating_mode{mode="SIMULATION"} 1`,
		`buyacar_channel_consecutive_failures{channel="whatsapp"} 2`,
		`buyacar_channel_paused{channel="whatsapp"} 0`,
		`buyacar_hourly_quota_remaining{channel="whatsapp"}`,
		`buyacar_channel_messages_total{channel="whatsapp"}`,
	}

	for _, em := range expectedMetrics {
		if !strings.Contains(body, em) {
			t.Errorf("missing expected Prometheus metric substring: %q in:\n%s", em, body)
		}
	}
}

func TestMetricsExporter_MethodNotAllowed(t *testing.T) {
	exporter := NewMetricsExporter(nil, nil, nil, nil, "SIMULATION", nil)

	req := httptest.NewRequest(http.MethodPost, "/metrics", nil)
	w := httptest.NewRecorder()
	exporter.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed, got: %d", w.Result().StatusCode)
	}
}

func TestMetricsExporter_ConcurrentUpdatesNoRaces(t *testing.T) {
	exporter := NewMetricsExporter(nil, nil, nil, nil, "DRAFT", []string{"whatsapp", "email"})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			exporter.RecordCacheHit()
			exporter.RecordCacheMiss()
			exporter.SetFastPathLatency(2.1, 2.59)
			exporter.IncrementChannelMessages("whatsapp")
			exporter.IncrementChannelMessages("email")
			_ = exporter.Snapshot()
			_ = exporter.ExportPrometheus()
		}(i)
	}
	wg.Wait()

	snap := exporter.Snapshot()
	if snap.CacheHitsTotal < 50 {
		t.Fatalf("expected at least 50 cache hits, got: %d", snap.CacheHitsTotal)
	}
	if snap.ChannelsMessagesTotal["whatsapp"] != 50 {
		t.Fatalf("expected 50 whatsapp messages, got: %d", snap.ChannelsMessagesTotal["whatsapp"])
	}
}
