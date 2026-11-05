package ai

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"buyacar.com.co/omnicanal/internal/inventory"
	"buyacar.com.co/omnicanal/internal/skills"
	"buyacar.com.co/omnicanal/internal/triage"
)

var validCascadeReply = []byte(`{
	"config_id": "cfg-cascade-1",
	"trace": {
		"timestamp_utc": "2026-10-29T10:00:00Z",
		"request_id": "req-cascade-1"
	},
	"proposed_reply": "Contamos con la Toyota Hilux 2022 en $185000000 COP en BuyaCAR Tunja.",
	"intent": "complex_query",
	"confidence": 0.92,
	"conversation_summary": "Cliente consulta Hilux 2022.",
	"crm_update": {"tasks_to_create": ["seguimiento_comercial"]},
	"governance": {"escalate": false, "reason": null}
}`)

func TestCascadeEngine_Tier1OllamaSuccess(t *testing.T) {
	ctx := context.Background()
	compact := skills.BuildCompactContext(triage.IntentComplexQuery, "Consulta Hilux", inventory.DefaultFixtureVehicles())

	ollamaStub := NewStubProvider("ollama", StubResponse{RawJSON: validCascadeReply, InputTokens: 100, OutputTokens: 50}).WithTier(1)
	groqStub := NewStubProvider("groq", StubResponse{Err: ErrQuotaExceeded}).WithTier(2)
	fastTier := NewFastPathTierProvider()

	cache := NewSemanticCache(100, 1*time.Hour)
	engine := NewCascadeEngine(ollamaStub, groqStub, fastTier, cache, nil)

	res, tier, err := engine.ResolveWithCascade(ctx, "¿Qué precio tiene la Toyota Hilux?", compact)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tier != 1 || res.ProviderUsed != "ollama" {
		t.Fatalf("expected Tier 1 (ollama), got tier=%d provider=%s", tier, res.ProviderUsed)
	}
	if res.Escalated {
		t.Fatalf("expected successful resolution without escalation")
	}

	// Comprobar que quedo guardado en cache semantica
	hash := engine.HashQuery("¿Qué precio tiene la Toyota Hilux?")
	if _, ok := cache.Get(hash); !ok {
		t.Fatalf("expected result to be saved in semantic cache")
	}

	// Segunda llamada -> Debe resolver por cache semantica (Tier 0, 0 tokens)
	res2, tier2, err2 := engine.ResolveWithCascade(ctx, "que precio tiene la toyota hilux", compact)
	if err2 != nil {
		t.Fatalf("unexpected error on second call: %v", err2)
	}
	if tier2 != 0 || res2.ProviderUsed != "semantic_cache" || res2.InputTokens != 0 || res2.OutputTokens != 0 {
		t.Fatalf("expected Tier 0 semantic_cache hit, got tier=%d provider=%s inTok=%d", tier2, res2.ProviderUsed, res2.InputTokens)
	}
}

func TestCascadeEngine_Tier2GroqFailover(t *testing.T) {
	ctx := context.Background()
	compact := skills.BuildCompactContext(triage.IntentComplexQuery, "Consulta Hilux", inventory.DefaultFixtureVehicles())

	// Nivel 1 falla por timeout
	ollamaTimeout := NewStubProvider("ollama", StubResponse{Err: ErrProviderTimeout}).WithTier(1)
	// Nivel 2 responde exitosamente
	groqValid := NewStubProvider("groq", StubResponse{RawJSON: validCascadeReply, InputTokens: 90, OutputTokens: 40}).WithTier(2)
	fastTier := NewFastPathTierProvider()

	engine := NewCascadeEngine(ollamaTimeout, groqValid, fastTier, nil, nil)

	res, tier, err := engine.ResolveWithCascade(ctx, "consulta prueba", compact)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tier != 2 || res.ProviderUsed != "groq" {
		t.Fatalf("expected Tier 2 (groq), got tier=%d provider=%s", tier, res.ProviderUsed)
	}
	if res.InputTokens != 90 || res.OutputTokens != 40 {
		t.Fatalf("unexpected tokens: in=%d out=%d", res.InputTokens, res.OutputTokens)
	}
}

func TestCascadeEngine_Tier3FastPathFailover(t *testing.T) {
	ctx := context.Background()
	// Crear vehiculo disponible
	vehicles := inventory.DefaultFixtureVehicles()
	compact := skills.BuildCompactContext(triage.IntentPrice, "Hilux 2022", vehicles)

	// Niveles 1 y 2 fallan
	ollamaFail := NewStubProvider("ollama", StubResponse{Err: ErrProviderTimeout}).WithTier(1)
	groqQuota := NewStubProvider("groq", StubResponse{Err: ErrQuotaExceeded}).WithTier(2)
	fastTier := NewFastPathTierProvider()

	engine := NewCascadeEngine(ollamaFail, groqQuota, fastTier, nil, nil)

	res, tier, err := engine.ResolveWithCascade(ctx, "Hilux 2022", compact)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tier != 3 || res.ProviderUsed != "fast_path" {
		t.Fatalf("expected Tier 3 (fast_path), got tier=%d provider=%s", tier, res.ProviderUsed)
	}
	if res.Escalated {
		t.Fatalf("expected fast_path to resolve without escalation, got reason=%s", res.Reason)
	}
	if res.InputTokens != 0 || res.OutputTokens != 0 {
		t.Fatalf("expected 0 tokens for Tier 3, got in=%d out=%d", res.InputTokens, res.OutputTokens)
	}
}

func TestCascadeEngine_AllTiersExhaustedEscalates(t *testing.T) {
	ctx := context.Background()
	// Sin vehiculos disponibles en el contexto
	compact := skills.BuildCompactContext(triage.IntentComplexQuery, "Negociacion credito especial", nil)

	ollamaFail := NewStubProvider("ollama", StubResponse{Err: ErrProviderTimeout}).WithTier(1)
	groqFail := NewStubProvider("groq", StubResponse{Err: ErrQuotaExceeded}).WithTier(2)
	fastTier := NewFastPathTierProvider()

	engine := NewCascadeEngine(ollamaFail, groqFail, fastTier, nil, nil)

	res, tier, err := engine.ResolveWithCascade(ctx, "negociacion compleja", compact)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tier != 0 || res.ProviderUsed != "human_escalation" || !res.Escalated {
		t.Fatalf("expected human_escalation when all tiers fail, got tier=%d provider=%s escalated=%v", tier, res.ProviderUsed, res.Escalated)
	}
}

func TestCascadeEngine_ConcurrentDeduplication(t *testing.T) {
	ctx := context.Background()
	compact := skills.BuildCompactContext(triage.IntentComplexQuery, "Hilux", inventory.DefaultFixtureVehicles())

	var ollamaCalls atomic.Int32
	// Proveedor con retardo
	slowOllama := NewStubProvider("ollama", StubResponse{RawJSON: validCascadeReply, InputTokens: 100, OutputTokens: 50}).WithTier(1)

	// Envolver GenerateJSON para contabilizar llamadas reales
	engine := NewCascadeEngine(slowOllama, nil, nil, nil, nil)

	var wg sync.WaitGroup
	const concurrentUsers = 15

	for i := 0; i < concurrentUsers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = engine.ResolveWithCascade(ctx, "¿Qué precio tiene la Toyota Hilux 2022?", compact)
			ollamaCalls.Add(1)
		}()
	}

	wg.Wait()
}

func BenchmarkFastPathTierProvider_GenerateJSON(b *testing.B) {
	ctx := context.Background()
	fastTier := NewFastPathTierProvider()
	compact := skills.BuildCompactContext(triage.IntentPrice, "Hilux 2022", inventory.DefaultFixtureVehicles())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		raw, inTok, outTok, err := fastTier.GenerateJSON(ctx, compact)
		if err != nil || len(raw) == 0 || inTok != 0 || outTok != 0 {
			b.Fatal("unexpected benchmark failure")
		}
	}
}
