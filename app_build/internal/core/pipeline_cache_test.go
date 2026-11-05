package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"buyacar.com.co/omnicanal/internal/ai"
	"buyacar.com.co/omnicanal/internal/channel"
	"buyacar.com.co/omnicanal/internal/crm"
	"buyacar.com.co/omnicanal/internal/escalate"
	"buyacar.com.co/omnicanal/internal/fast"
	"buyacar.com.co/omnicanal/internal/guard"
	"buyacar.com.co/omnicanal/internal/inventory"
	"buyacar.com.co/omnicanal/internal/store"
)

func TestPipeline_SemanticCacheHitBypassesInference(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)

	st := store.NewMemoryStore()
	cfg := DefaultSafeConfig()
	cfg.EnableFastPath = false // Desactivar Fast-Path inicial para forzar paso por CP4
	eng := NewEngine(cfg, st)

	waStub := channel.NewStubChannel("whatsapp", 650)
	_ = eng.RegisterChannel(waStub)

	conv := channel.Conversation{ID: "conv-cache-1", ChannelID: "whatsapp", CustomerAlias: "Cliente_Cache", UpdatedAt: now}
	inquiry := "¿Qué precio tiene la Toyota Hilux 2022?"
	waStub.SeedThread(conv, []channel.Message{
		{ID: "msg-1", Sender: "customer", Text: inquiry, Timestamp: now.Add(-1 * time.Minute)},
	})

	sess, err := waStub.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})
	if err != nil {
		t.Fatalf("unexpected open error: %v", err)
	}

	// Configurar SemanticCache con respuesta pre-aprobada
	semCache := ai.NewSemanticCache(100, 1*time.Hour)
	approvedReply := "Contamos con la Toyota Hilux 2022 en $185000000 COP (caja Automática, 42000 km). ¡Vehículos certificados en BuyaCAR Tunja! Detalles: https://buyacar.com.co/vehiculos/toyota-hilux-2022"
	semCache.SetReply(inquiry, approvedReply, "price", 1*time.Hour)

	// Orquestador de IA sin proveedores externos configurados, pero con cache semantica
	aiOrch := ai.NewOrchestrator(nil, nil, nil)
	aiOrch.SetSemanticCache(semCache)

	pipe := &Pipeline{
		Engine:   eng,
		InvCache: inventory.NewSeededStubCache(now),
		Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
		Guard:    guard.NewGuard([]string{"buyacar.com.co"}),
		AIOrch:   aiOrch,
		EscQueue: escalate.NewQueue(),
		CRM:      crm.NewMemoryManager(),
	}

	out, err := pipe.ProcessConversation(ctx, waStub, sess, conv, now)
	if err != nil {
		t.Fatalf("unexpected pipeline error: %v", err)
	}

	// 1. Debe haber omitido inferencia externa (0 tokens y UsedAI = false)
	if out.UsedAI {
		t.Fatalf("expected UsedAI to be false on semantic cache hit")
	}
	if out.InputTokens != 0 || out.OutputTokens != 0 {
		t.Fatalf("expected 0 tokens on cache hit, got in=%d out=%d", out.InputTokens, out.OutputTokens)
	}

	// 2. CP5 Guard debio validar exitosamente la respuesta
	if !strings.Contains(out.SentReply, "185000000") || !strings.Contains(out.SentReply, "https://buyacar.com.co") {
		t.Fatalf("expected approved reply sent, got: %s", out.SentReply)
	}

	// 3. Verificamos que se completaron los checkpoints hasta cp7
	if out.CheckpointsOK != 6 {
		t.Fatalf("expected 6 checkpoints ok, got %d", out.CheckpointsOK)
	}

	hits, _, _, _ := semCache.Stats()
	if hits < 1 {
		t.Fatalf("expected at least 1 cache hit, got %d", hits)
	}
}

func TestPipeline_CascadeFailoverInPipeline(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)

	st := store.NewMemoryStore()
	cfg := DefaultSafeConfig()
	cfg.EnableFastPath = false // Desactivar Fast-Path inicial
	eng := NewEngine(cfg, st)

	waStub := channel.NewStubChannel("whatsapp", 650)
	_ = eng.RegisterChannel(waStub)

	conv := channel.Conversation{ID: "conv-cascade-1", ChannelID: "whatsapp", CustomerAlias: "Cliente_Cascade", UpdatedAt: now}
	inquiry := "Consulta técnica de tracción Hilux"
	waStub.SeedThread(conv, []channel.Message{
		{ID: "msg-1", Sender: "customer", Text: inquiry, Timestamp: now.Add(-1 * time.Minute)},
	})

	sess, _ := waStub.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})

	groqReply := []byte(`{
		"config_id": "cfg-groq-cascade",
		"trace": {"timestamp_utc": "2026-10-29T10:00:00Z", "request_id": "req-1"},
		"proposed_reply": "La Toyota Hilux 2022 cuenta con tracción 4x4 y precio de $185000000 COP en Tunja. Detalles: https://buyacar.com.co/vehiculos/toyota-hilux-2022",
		"intent": "complex_query",
		"confidence": 0.95,
		"conversation_summary": "Cliente consulta traccion Hilux.",
		"crm_update": {"tasks_to_create": ["seguimiento_traccion"]},
		"governance": {"escalate": false, "reason": null}
	}`)

	// Nivel 1 (Ollama) falla -> Nivel 2 (Groq) responde
	ollamaFail := ai.NewStubProvider("ollama", ai.StubResponse{Err: ai.ErrProviderTimeout}).WithTier(1)
	groqSuccess := ai.NewStubProvider("groq", ai.StubResponse{RawJSON: groqReply, InputTokens: 110, OutputTokens: 45}).WithTier(2)

	semCache := ai.NewSemanticCache(100, 1*time.Hour)
	cascade := ai.NewCascadeEngine(ollamaFail, groqSuccess, nil, semCache, nil)
	aiOrch := ai.NewCascadeOrchestrator(cascade, semCache)

	pipe := &Pipeline{
		Engine:   eng,
		InvCache: inventory.NewSeededStubCache(now),
		Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
		Guard:    guard.NewGuard([]string{"buyacar.com.co"}),
		AIOrch:   aiOrch,
		EscQueue: escalate.NewQueue(),
		CRM:      crm.NewMemoryManager(),
	}

	out, err := pipe.ProcessConversation(ctx, waStub, sess, conv, now)
	if err != nil {
		t.Fatalf("unexpected pipeline error: %v", err)
	}

	if !out.UsedAI || out.InputTokens != 110 || out.OutputTokens != 45 {
		t.Fatalf("expected Groq cascade execution: UsedAI=%v, in=%d, out=%d", out.UsedAI, out.InputTokens, out.OutputTokens)
	}

	// Segunda conversacion con la misma consulta -> Debe responder desde SemanticCache a 0 tokens
	conv2 := channel.Conversation{ID: "conv-cascade-2", ChannelID: "whatsapp", CustomerAlias: "Cliente_Cascade_2", UpdatedAt: now.Add(2 * time.Minute)}
	waStub.SeedThread(conv2, []channel.Message{
		{ID: "msg-2", Sender: "customer", Text: inquiry, Timestamp: now.Add(1 * time.Minute)},
	})

	out2, err2 := pipe.ProcessConversation(ctx, waStub, sess, conv2, now.Add(2*time.Minute))
	if err2 != nil {
		t.Fatalf("unexpected pipeline error on second run: %v", err2)
	}

	if out2.UsedAI || out2.InputTokens != 0 || out2.OutputTokens != 0 {
		t.Fatalf("expected semantic cache hit on second run, got UsedAI=%v in=%d", out2.UsedAI, out2.InputTokens)
	}
}
