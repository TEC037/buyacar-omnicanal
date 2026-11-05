package crm

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"buyacar.com.co/omnicanal/internal/escalate"
)

func TestWebhookExporter_SuccessOnFirstAttempt(t *testing.T) {
	secret := "secret-crm-key-xyz"
	var receivedSig, receivedTimestamp string
	var receivedTask Task

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSig = r.Header.Get("X-BuyaCAR-Signature-256")
		receivedTimestamp = r.Header.Get("X-BuyaCAR-Timestamp")

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedTask)

		// Verificar HMAC
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		expectedSig := hex.EncodeToString(mac.Sum(nil))

		if receivedSig != expectedSig {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"received"}`))
	}))
	defer server.Close()

	queue := escalate.NewQueue()
	exporter := NewWebhookExporter(WebhookExporterConfig{
		Endpoint:   server.URL,
		Secret:     secret,
		MaxRetries: 3,
		BaseDelay:  5 * time.Millisecond,
		Timeout:    2 * time.Second,
		HTTPClient: server.Client(),
	}, queue)

	task := Task{
		ID:          "task-export-1",
		ConvID:      "conv-cust-1",
		ChannelID:   "whatsapp",
		Type:        TaskCommercialFollowup,
		Status:      TaskStatusPending,
		Description: "Seguimiento urgente por Hilux",
		CreatedAt:   time.Now().UTC(),
	}

	ctx := context.Background()
	if err := exporter.ExportTask(ctx, task); err != nil {
		t.Fatalf("unexpected error exporting task: %v", err)
	}

	if receivedTask.ID != "task-export-1" || receivedTask.ConvID != "conv-cust-1" {
		t.Fatalf("expected received task to match, got %+v", receivedTask)
	}
	if receivedSig == "" || receivedTimestamp == "" {
		t.Fatalf("expected headers X-BuyaCAR-Signature-256 and X-BuyaCAR-Timestamp to be present")
	}

	stats := exporter.Stats()
	if stats.SuccessCount != 1 || stats.RetryCount != 0 || stats.ExhaustedCount != 0 {
		t.Fatalf("expected stats (1 success, 0 retries, 0 exhausted), got %+v", stats)
	}
	if len(queue.Pending()) != 0 {
		t.Fatalf("expected 0 escalation tickets on success")
	}
}

func TestWebhookExporter_SuccessAfterRetry(t *testing.T) {
	secret := "retry-secret-key"
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&attempts, 1)
		if current < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"temporary CRM downtime"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	queue := escalate.NewQueue()
	exporter := NewWebhookExporter(WebhookExporterConfig{
		Endpoint:   server.URL,
		Secret:     secret,
		MaxRetries: 3,
		BaseDelay:  5 * time.Millisecond,
		Timeout:    2 * time.Second,
		HTTPClient: server.Client(),
	}, queue)

	task := Task{
		ID:        "task-retry-1",
		ConvID:    "conv-retry-1",
		ChannelID: "messenger",
		Type:      TaskGeneralInquiry,
	}

	ctx := context.Background()
	if err := exporter.ExportTask(ctx, task); err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}

	stats := exporter.Stats()
	if stats.SuccessCount != 1 || stats.RetryCount != 2 || stats.ExhaustedCount != 0 {
		t.Fatalf("expected stats (1 success, 2 retries, 0 exhausted), got %+v", stats)
	}
	if len(queue.Pending()) != 0 {
		t.Fatalf("expected 0 escalation tickets")
	}
}

func TestWebhookExporter_Exhaustion_FallsBackToEscalateQueue(t *testing.T) {
	secret := "exhaust-secret-key"
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"CRM unavailable indefinitely"}`))
	}))
	defer server.Close()

	queue := escalate.NewQueue()
	maxRetries := 2
	exporter := NewWebhookExporter(WebhookExporterConfig{
		Endpoint:   server.URL,
		Secret:     secret,
		MaxRetries: maxRetries,
		BaseDelay:  5 * time.Millisecond,
		Timeout:    1 * time.Second,
		HTTPClient: server.Client(),
	}, queue)

	task := Task{
		ID:          "task-fail-1",
		ConvID:      "conv-fail-99",
		ChannelID:   "whatsapp",
		Type:        TaskCreditApplication,
		Description: "Solicitud credito fallida",
	}

	ctx := context.Background()
	err := exporter.ExportTask(ctx, task)
	if err == nil {
		t.Fatalf("expected error on retry exhaustion, got nil")
	}

	// Debe haber intentado 1 inicial + 2 reintentos = 3 llamadas
	if atomic.LoadInt32(&attempts) != 3 {
		t.Fatalf("expected 3 attempts, got %d", atomic.LoadInt32(&attempts))
	}

	stats := exporter.Stats()
	if stats.SuccessCount != 0 || stats.ExhaustedCount != 1 {
		t.Fatalf("expected 1 exhausted count, got %+v", stats)
	}

	// Verificar alerta en escalate.Queue
	tickets := queue.Pending()
	if len(tickets) != 1 {
		t.Fatalf("expected 1 escalation ticket in queue, got %d", len(tickets))
	}
	if tickets[0].Reason != "crm_webhook_export_exhausted" {
		t.Fatalf("expected reason crm_webhook_export_exhausted, got %q", tickets[0].Reason)
	}
	if tickets[0].ChannelID != "whatsapp" || tickets[0].ConversationID != "conv-fail-99" {
		t.Fatalf("expected ticket channel and conv to match, got %+v", tickets[0])
	}
}

func TestWebhookExporter_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	defer server.Close()

	exporter := NewWebhookExporter(WebhookExporterConfig{
		Endpoint:   server.URL,
		Secret:     "secret",
		MaxRetries: 5,
		BaseDelay:  500 * time.Millisecond,
		HTTPClient: server.Client(),
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancelar casi inmediatamente
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := exporter.ExportTask(ctx, Task{ID: "task-ctx", ConvID: "conv-ctx"})
	if err == nil || ctx.Err() == nil {
		t.Fatalf("expected context cancellation error, got %v", err)
	}
}
