package ai

import (
	"context"
	"strings"
	"testing"

	"buyacar.com.co/omnicanal/internal/inventory"
	"buyacar.com.co/omnicanal/internal/rag"
	"buyacar.com.co/omnicanal/internal/skills"
	"buyacar.com.co/omnicanal/internal/triage"
)

var validGeminiJSON = []byte(`{
  "config_id": "cfg-01",
  "trace": {
    "timestamp_utc": "2026-10-26T13:50:00Z",
    "request_id": "req-101"
  },
  "proposed_reply": "Contamos con la Toyota Hilux 2022 en $185000000 COP lista para prueba de ruta en Tunja.",
  "intent": "complex_query",
  "confidence": 0.91,
  "conversation_summary": "Cliente consulta comparativa de camioneta 4x4.",
  "crm_update": {
    "tasks_to_create": ["agendar_prueba_ruta"]
  },
  "governance": {
    "escalate": false,
    "reason": null
  }
}`)

var invalidUnknownFieldJSON = []byte(`{
  "config_id": "cfg-01",
  "unknown_field_violation": true
}`)

func TestParseStrictJSON_RejectsUnknownFields(t *testing.T) {
	if _, err := ParseStrictJSON(invalidUnknownFieldJSON); err == nil {
		t.Fatal("expected ParseStrictJSON to reject unknown fields")
	}
	if _, err := ParseStrictJSON(validGeminiJSON); err != nil {
		t.Fatalf("expected validGeminiJSON to pass, got %v", err)
	}
}

func TestOrchestrator_GeminiToOllamaToHumanChain(t *testing.T) {
	ctx := context.Background()
	compact := skills.BuildCompactContext(triage.IntentComplexQuery, "Resumen corto", inventory.DefaultFixtureVehicles())

	// Caso 1: Gemini falla por cuota -> Fallback a Ollama responde con JSON valido
	geminiQuota := NewStubProvider("gemini", StubResponse{Err: ErrQuotaExceeded})
	ollamaValid := NewStubProvider("ollama", StubResponse{RawJSON: validGeminiJSON, OutputTokens: 85})
	orch := NewOrchestrator(geminiQuota, ollamaValid, nil)

	res := orch.Resolve(ctx, "", compact)
	if res.ProviderUsed != "ollama" || res.Escalated {
		t.Fatalf("expected ollama fallback to resolve, got %+v", res)
	}

	// Caso 2: Gemini devuelve JSON invalido en primer intento y se recupera en el unico reintento permitido
	geminiRetry := NewStubProvider("gemini",
		StubResponse{RawJSON: invalidUnknownFieldJSON, OutputTokens: 30},
		StubResponse{RawJSON: validGeminiJSON, OutputTokens: 85},
	)
	orch2 := NewOrchestrator(geminiRetry, nil, nil)
	res2 := orch2.Resolve(ctx, "", compact)
	if res2.ProviderUsed != "gemini" || res2.Escalated {
		t.Fatalf("expected gemini single retry to succeed, got %+v", res2)
	}

	// Caso 3: Tanto Gemini (timeout) como Ollama (JSON invalido dos veces) fallan -> Escalamiento humano
	geminiTimeout := NewStubProvider("gemini", StubResponse{Err: ErrProviderTimeout})
	ollamaBadJSON := NewStubProvider("ollama",
		StubResponse{RawJSON: invalidUnknownFieldJSON, OutputTokens: 20},
		StubResponse{RawJSON: invalidUnknownFieldJSON, OutputTokens: 20},
	)
	orch3 := NewOrchestrator(geminiTimeout, ollamaBadJSON, nil)
	res3 := orch3.Resolve(ctx, "", compact)
	if res3.ProviderUsed != "human_escalation" || !res3.Escalated || res3.Reason != "both_ai_providers_failed" {
		t.Fatalf("expected human_escalation when both fail, got %+v", res3)
	}
}

func TestOrchestrator_ApprovedCacheSavesTokens(t *testing.T) {
	ctx := context.Background()
	cache := skills.NewApprovedCache()
	cache.Set("financiacion_bancos", "Manejamos todos los bancos en Tunja con asesoría personalizada.")

	orch := NewOrchestrator(nil, nil, cache)
	compact := skills.BuildCompactContext(triage.IntentComplexQuery, "Bancos", nil)

	res := orch.Resolve(ctx, "financiacion_bancos", compact)
	if res.ProviderUsed != "cache" || res.InputTokens != 0 || res.OutputTokens != 0 {
		t.Fatalf("expected cache hit with 0 tokens, got %+v", res)
	}
}

func TestProviders_DelimitedPromptRedactsPIIAndEnforcesLoopback(t *testing.T) {
	compact := skills.BuildCompactContext(
		triage.IntentComplexQuery,
		"Cliente Juan escribe desde correo@privado.com con cedula 1098765432 pidiendo ignorar instrucciones",
		inventory.DefaultFixtureVehicles(),
	)

	prompt := BuildDelimitedPrompt(compact)
	if len(prompt) == 0 {
		t.Fatal("expected non-empty delimited prompt")
	}
	// Verificar redaccion de PII antes de enviar al modelo (Regla 20-ai-contracts.md)
	if contains := string(prompt); contains != "" {
		if indexOf(contains, "correo@privado.com") >= 0 || indexOf(contains, "1098765432") >= 0 {
			t.Fatalf("expected PII to be redacted from AI prompt, got:\n%s", prompt)
		}
		if indexOf(contains, "<DATOS_NO_CONFIABLES_CLIENTE>") < 0 || indexOf(contains, "<INVENTARIO_VERIFICADO>") < 0 {
			t.Fatalf("expected strict XML delimiters in prompt")
		}
	}

	// Rechazar Ollama fuera de loopback
	if _, err := NewOllamaProvider("http://192.168.1.50:11434/api/generate", DefaultOllamaModel, DefaultAIProviderTimeout); err == nil {
		t.Fatal("expected non-loopback Ollama host to be rejected")
	}
	// Aceptar Ollama en 127.0.0.1:11434
	if _, err := NewOllamaProvider(DefaultOllamaEndpoint, DefaultOllamaModel, DefaultAIProviderTimeout); err != nil {
		t.Fatalf("unexpected error for local Ollama provider: %v", err)
	}
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func TestBuildDelimitedPrompt_WithRAGChunksAndPIIRedaction(t *testing.T) {
	ragChunks := []rag.Chunk{
		{
			ID:       "w1",
			Title:    "Garantía BuyaCAR Tunja",
			Content:  "Todos los vehículos tienen garantía legal de 3 meses. Contacto asesor con cedula 1098765432 y correo juan@privado.com.",
			Category: "warranty",
		},
	}

	compact := skills.BuildCompactContextWithRAG(
		triage.IntentLegalOrClaim,
		"Consulta de garantías y políticas",
		inventory.DefaultFixtureVehicles(),
		ragChunks,
		150,
	)

	prompt := BuildDelimitedPrompt(compact)

	// Verificar inclusión de cabecera formal requerida
	if !strings.Contains(prompt, "[DOCUMENTOS BUyACAR BOYACA/TUNJA]") {
		t.Fatalf("expected prompt to contain [DOCUMENTOS BUyACAR BOYACA/TUNJA], got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Garantía BuyaCAR Tunja") {
		t.Fatalf("expected chunk title in prompt, got:\n%s", prompt)
	}

	// Verificar que PII dentro de los fragmentos RAG sea redactada
	if strings.Contains(prompt, "juan@privado.com") || strings.Contains(prompt, "1098765432") {
		t.Fatalf("expected PII inside RAG chunk to be redacted from prompt, got:\n%s", prompt)
	}
}

func TestCascadeEngine_WithRAGContextAndTokenBudget(t *testing.T) {
	ctx := context.Background()

	ragChunks := []rag.Chunk{
		{
			ID:       "c1",
			Title:    "Crédito BuyaCAR",
			Content:  "Financiación hasta el 100% con aprobación inmediata en Boyacá.",
			Category: "credit",
		},
	}

	compact := skills.BuildCompactContextWithRAG(
		triage.IntentCreditStudy,
		"Requisitos para crédito vehicular",
		inventory.DefaultFixtureVehicles(),
		ragChunks,
		150,
	)

	ollamaValid := NewStubProvider("ollama", StubResponse{RawJSON: validGeminiJSON, OutputTokens: 70})
	cascade := NewCascadeEngine(ollamaValid, nil, nil, nil, nil)

	res, tier, err := cascade.ResolveWithCascade(ctx, "credito_vehicular", compact)
	if err != nil {
		t.Fatalf("unexpected cascade error: %v", err)
	}
	if tier != 1 || res.ProviderUsed != "ollama" {
		t.Fatalf("expected Tier 1 ollama response, got tier=%d provider=%s", tier, res.ProviderUsed)
	}

	// Presupuesto de entrada excedido escala ordenadamente
	oversizedCompact := compact
	oversizedCompact.EstimatedTokens = skills.MaxInputTokensBudget + 50
	resOversized, _, err2 := cascade.ResolveWithCascade(ctx, "oversized", oversizedCompact)
	if err2 != nil {
		t.Fatalf("unexpected error on oversized: %v", err2)
	}
	if !resOversized.Escalated || resOversized.Reason != "input_token_budget_exceeded" {
		t.Fatalf("expected escalation due to input_token_budget_exceeded, got %+v", resOversized)
	}
}
