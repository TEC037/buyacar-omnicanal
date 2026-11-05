package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"buyacar.com.co/omnicanal/internal/skills"
	"buyacar.com.co/omnicanal/internal/triage"
)

func TestGroqProvider_EndpointValidation(t *testing.T) {
	// 1. Rechazar HTTP en host remoto no-loopback
	if _, err := NewGroqProvider("dummy-key", "llama-3.1-8b-instant", "http://api.groq.com/openai/v1/chat/completions", 5*time.Second); err == nil {
		t.Fatal("expected non-https remote host to be rejected")
	}

	// 2. Rechazar host no confiable
	if _, err := NewGroqProvider("dummy-key", "llama-3.1-8b-instant", "https://untrusted-api.evil.com/v1/chat", 5*time.Second); err == nil {
		t.Fatal("expected untrusted host to be rejected")
	}

	// 3. Aceptar endpoint oficial
	p, err := NewGroqProvider("dummy-key", "llama-3.1-8b-instant", DefaultGroqEndpoint, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error for official groq endpoint: %v", err)
	}
	if p.Name() != "groq" || p.Tier() != 2 {
		t.Fatalf("unexpected provider metadata: name=%s, tier=%d", p.Name(), p.Tier())
	}
}

func TestGroqProvider_MissingKeyReturnsError(t *testing.T) {
	p, err := NewGroqProvider("", "llama-3.1-8b-instant", "http://127.0.0.1:1234/v1/chat/completions", 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected constructor error: %v", err)
	}

	compact := skills.BuildCompactContext(triage.IntentComplexQuery, "Consulta prueba", nil)
	_, _, _, err = p.GenerateJSON(context.Background(), compact)
	if !errors.Is(err, ErrMissingGroqAPIKey) {
		t.Fatalf("expected ErrMissingGroqAPIKey, got %v", err)
	}
}

func TestGroqProvider_SuccessfulInferenceAndHeaders(t *testing.T) {
	validStructuredReply := `{
		"config_id": "cfg-groq-1",
		"trace": {
			"timestamp_utc": "2026-10-29T10:00:00Z",
			"request_id": "req-groq-1"
		},
		"proposed_reply": "Tenemos la Toyota Hilux 2022 en $185.000.000 COP en BuyaCAR Tunja.",
		"intent": "complex_query",
		"confidence": 0.95,
		"conversation_summary": "Cliente consulta precio Hilux.",
		"crm_update": {"tasks_to_create": ["seguimiento_hilux"]},
		"governance": {"escalate": false, "reason": null}
	}`

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Validar encabezados obligatorios
		if r.Header.Get("Authorization") != "Bearer gsk-secret-test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "bad content-type", http.StatusBadRequest)
			return
		}

		// Validar payload
		var req groqRequestPayload
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if req.ResponseFormat == nil || req.ResponseFormat.Type != "json_object" {
			http.Error(w, "missing json_object format", http.StatusBadRequest)
			return
		}

		resp := groqResponsePayload{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{
				{
					Message: struct {
						Content string `json:"content"`
					}{Content: validStructuredReply},
					FinishReason: "stop",
				},
			},
			Usage: struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			}{
				PromptTokens:     145,
				CompletionTokens: 68,
				TotalTokens:      213,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	p, err := NewGroqProvider("gsk-secret-test-key", "llama-3.1-8b-instant", mockServer.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected provider error: %v", err)
	}

	compact := skills.BuildCompactContext(triage.IntentComplexQuery, "Hilux 2022", nil)
	raw, inTok, outTok, err := p.GenerateJSON(context.Background(), compact)
	if err != nil {
		t.Fatalf("GenerateJSON failed: %v", err)
	}
	if inTok != 145 || outTok != 68 {
		t.Fatalf("unexpected tokens: in=%d, out=%d", inTok, outTok)
	}

	out, parseErr := ParseStrictJSON(raw)
	if parseErr != nil {
		t.Fatalf("expected valid schema output: %v", parseErr)
	}
	if out.ConfigID != "cfg-groq-1" || out.Confidence != 0.95 {
		t.Fatalf("unexpected structured output: %+v", out)
	}
}

func TestGroqProvider_QuotaExceeded(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"Rate limit reached"}}`, http.StatusTooManyRequests)
	}))
	defer mockServer.Close()

	p, err := NewGroqProvider("gsk-secret-test-key", "llama-3.1-8b-instant", mockServer.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	compact := skills.BuildCompactContext(triage.IntentComplexQuery, "Hilux 2022", nil)
	_, _, _, err = p.GenerateJSON(context.Background(), compact)
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("expected ErrQuotaExceeded, got %v", err)
	}
}
