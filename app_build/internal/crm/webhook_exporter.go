package crm

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"buyacar.com.co/omnicanal/internal/escalate"
)

var (
	ErrExportExhausted = errors.New("crm: webhook export exhausted retries")
	ErrInvalidEndpoint = errors.New("crm: invalid webhook endpoint")
)

// WebhookExporterConfig parametriza el despachador saliente de tareas hacia el CRM.
type WebhookExporterConfig struct {
	Endpoint   string        // URL destino del webhook CRM
	Secret     string        // Clave HMAC-SHA256 para firma
	MaxRetries int           // Intentos maximos (default: 3)
	BaseDelay  time.Duration // Retardo inicial para backoff (default: 50ms)
	MaxDelay   time.Duration // Retardo maximo para backoff (default: 2s)
	Timeout    time.Duration // Timeout por intento (default: 10s, max 30s)
	HTTPClient *http.Client  // Cliente HTTP configurable (para mocks e inyeccion en pruebas)
}

// ExporterStats expone metricas de observabilidad sobre la exportacion hacia CRM.
type ExporterStats struct {
	DispatchedCount int64     `json:"dispatched_count"`
	SuccessCount    int64     `json:"success_count"`
	RetryCount      int64     `json:"retry_count"`
	ExhaustedCount  int64     `json:"exhausted_count"`
	LastExport      time.Time `json:"last_export"`
	LastError       string    `json:"last_error,omitempty"`
}

// WebhookExporter gestiona el envio saliente de tareas/leads firmadas con HMAC-SHA256,
// reintentos exponenciales y escalamiento a escalate.Queue ante agotamiento.
type WebhookExporter struct {
	mu     sync.RWMutex
	cfg    WebhookExporterConfig
	client *http.Client
	queue  *escalate.Queue
	stats  ExporterStats
}

// NewWebhookExporter inicializa un despachador saliente con valores por defecto seguros.
func NewWebhookExporter(cfg WebhookExporterConfig, queue *escalate.Queue) *WebhookExporter {
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = 50 * time.Millisecond
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 2 * time.Second
	}
	if cfg.Timeout <= 0 || cfg.Timeout > 30*time.Second {
		cfg.Timeout = 10 * time.Second
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: cfg.Timeout,
		}
	}

	return &WebhookExporter{
		cfg:    cfg,
		client: client,
		queue:  queue,
	}
}

// ComputeSignature genera la firma HMAC-SHA256 en formato hexadecimal para las cabeceras del webhook.
func ComputeSignature(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// ExportTask despacha una tarea al CRM con firma criptografica, reintentos con backoff exponencial
// y derivacion automatica a escalate.Queue si se agotan todos los reintentos.
func (e *WebhookExporter) ExportTask(ctx context.Context, task Task) error {
	if e.cfg.Endpoint == "" {
		return ErrInvalidEndpoint
	}

	payload, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("crm: failed to marshal task: %w", err)
	}

	e.mu.Lock()
	e.stats.DispatchedCount++
	e.stats.LastExport = time.Now().UTC()
	e.mu.Unlock()

	var lastErr error
	maxRetries := e.cfg.MaxRetries

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.cfg.Endpoint, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("crm: failed to build request: %w", err)
		}

		timestamp := time.Now().UTC().Format(time.RFC3339)
		sig := ComputeSignature(e.cfg.Secret, payload)

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-BuyaCAR-Signature-256", sig)
		req.Header.Set("X-BuyaCAR-Timestamp", timestamp)
		req.Header.Set("User-Agent", "BuyaCAR-CRM-Exporter/1.0")

		resp, err := e.client.Do(req)
		if err == nil {
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()

				e.mu.Lock()
				e.stats.SuccessCount++
				e.stats.LastError = ""
				e.mu.Unlock()
				return nil
			}

			bodySnippet, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
			lastErr = fmt.Errorf("crm: upstream returned status %d: %s", resp.StatusCode, string(bodySnippet))
		} else {
			lastErr = err
		}

		e.mu.Lock()
		e.stats.RetryCount++
		e.stats.LastError = lastErr.Error()
		e.mu.Unlock()

		// Si quedan reintentos por ejecutar, esperar aplicando backoff exponencial
		if attempt < maxRetries {
			delay := e.cfg.BaseDelay * (1 << attempt)
			if delay > e.cfg.MaxDelay {
				delay = e.cfg.MaxDelay
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
	}

	// Agotamiento total de reintentos: registrar metrica y derivar alerta a escalate.Queue
	e.mu.Lock()
	e.stats.ExhaustedCount++
	e.mu.Unlock()

	if e.queue != nil {
		channelID := task.ChannelID
		if channelID == "" {
			channelID = "crm"
		}
		convID := task.ConvID
		if convID == "" {
			convID = task.ID
		}

		e.queue.Enqueue(
			channelID,
			convID,
			"crm_webhook_export_exhausted",
			fmt.Sprintf("Fallo en exportacion de tarea %s (%s) al CRM tras %d intentos: %v", task.ID, task.Type, maxRetries+1, lastErr),
			time.Now().UTC(),
		)
	}

	return fmt.Errorf("%w: %v", ErrExportExhausted, lastErr)
}

// Stats retorna una fotografia thread-safe de las metricas del despachador CRM.
func (e *WebhookExporter) Stats() ExporterStats {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.stats
}
