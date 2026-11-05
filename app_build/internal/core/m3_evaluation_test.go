package core

import (
	"context"
	"fmt"
	"sort"
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
	"buyacar.com.co/omnicanal/internal/rag"
	"buyacar.com.co/omnicanal/internal/skills"
	"buyacar.com.co/omnicanal/internal/store"
)

func setupEvaluationFixtures(t testing.TB) (rag.Engine, []inventory.Vehicle) {
	ctx := context.Background()
	ragEng := rag.NewEngine()

	// Documentos representativos del corpus BuyaCAR Tunja
	_, err := ragEng.IndexDocument(ctx, "pol_garantia", "warranty", "Garantía Legal BuyaCAR Tunja",
		"Todos los vehículos usados certificados cuentan con 3 meses de garantía en motor y caja según Ley 1480 de 2011.")
	if err != nil {
		t.Fatalf("failed to index warranty doc: %v", err)
	}

	_, err = ragEng.IndexDocument(ctx, "pol_credito", "credit", "Políticas de Crédito y Financiación",
		"Financiación hasta el 90% con Bancolombia, Banco de Bogotá y Davivienda en vitrina Tunja.")
	if err != nil {
		t.Fatalf("failed to index credit doc: %v", err)
	}

	_, err = ragEng.IndexDocument(ctx, "faq_horarios", "faq", "Sedes y Horarios de Atención",
		"Vitrina Cra 6 #52-23 frente a Toyota Tunja. Lunes a sábado de 8am a 6pm jornada continua.")
	if err != nil {
		t.Fatalf("failed to index faq doc: %v", err)
	}

	vehicles := inventory.DefaultFixtureVehicles()
	return ragEng, vehicles
}

func TestMilestone3_KaizenEvaluation(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 14, 0, 0, 0, time.UTC)
	ragEng, _ := setupEvaluationFixtures(t)

	validReplyJSON := []byte(`{
		"config_id": "cfg-eval-m3",
		"trace": {"timestamp_utc": "2026-10-29T14:00:00Z", "request_id": "req-m3"},
		"proposed_reply": "En BuyaCAR Tunja ofrecemos garantía legal de 3 meses en caja y motor.",
		"intent": "warranty",
		"confidence": 0.95,
		"conversation_summary": "Evaluación M3",
		"crm_update": {"tasks_to_create": []},
		"governance": {"escalate": false, "reason": null}
	}`)

	// Configurar pipeline SIN RAG
	capturerNoRAG := &capturingAIProvider{replyJSON: validReplyJSON}
	stNoRAG := store.NewMemoryStore()
	engNoRAG := NewEngine(DefaultSafeConfig(), stNoRAG)
	waNoRAG := channel.NewStubChannel("whatsapp", 650)
	_ = engNoRAG.RegisterChannel(waNoRAG)
	pipeNoRAG := NewPipeline(PipelineConfig{
		Engine:    engNoRAG,
		InvCache:  inventory.NewSeededStubCache(now),
		Fast:      fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
		Guard:     guard.NewGuard([]string{"buyacar.com.co"}),
		AIOrch:    ai.NewOrchestrator(nil, capturerNoRAG, skills.NewApprovedCache()),
		EscQueue:  escalate.NewQueue(),
		CRM:       crm.NewMemoryManager(),
		RAGEngine: nil,
	})

	// Configurar pipeline CON RAG
	capturerWithRAG := &capturingAIProvider{replyJSON: validReplyJSON}
	stWithRAG := store.NewMemoryStore()
	engWithRAG := NewEngine(DefaultSafeConfig(), stWithRAG)
	waWithRAG := channel.NewStubChannel("whatsapp", 650)
	_ = engWithRAG.RegisterChannel(waWithRAG)
	pipeWithRAG := NewPipeline(PipelineConfig{
		Engine:    engWithRAG,
		InvCache:  inventory.NewSeededStubCache(now),
		Fast:      fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
		Guard:     guard.NewGuard([]string{"buyacar.com.co"}),
		AIOrch:    ai.NewOrchestrator(nil, capturerWithRAG, skills.NewApprovedCache()),
		EscQueue:  escalate.NewQueue(),
		CRM:       crm.NewMemoryManager(),
		RAGEngine: ragEng,
	})

	queryText := "¿Qué respaldo de garantía y cobertura legal tienen los vehículos usados certificados?"
	conv := channel.Conversation{ID: "conv-eval-01", ChannelID: "whatsapp", CustomerAlias: "Cliente_Eval", UpdatedAt: now}
	waNoRAG.SeedThread(conv, []channel.Message{{ID: "m1", Sender: "customer", Text: queryText, Timestamp: now.Add(-1 * time.Minute)}})
	waWithRAG.SeedThread(conv, []channel.Message{{ID: "m1", Sender: "customer", Text: queryText, Timestamp: now.Add(-1 * time.Minute)}})

	sessNoRAG, _ := waNoRAG.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})
	sessWithRAG, _ := waWithRAG.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})

	// Ejecutar CP4 SIN RAG
	outNoRAG, err := pipeNoRAG.ProcessConversation(ctx, waNoRAG, sessNoRAG, conv, now)
	if err != nil {
		t.Fatalf("pipeNoRAG failed: %v", err)
	}
	promptNoRAG := ai.BuildDelimitedPrompt(capturerNoRAG.capturedCompact)
	tokensNoRAG := capturerNoRAG.capturedCompact.EstimatedTokens
	charsNoRAG := len(promptNoRAG)

	// Ejecutar CP4 CON RAG
	outWithRAG, err := pipeWithRAG.ProcessConversation(ctx, waWithRAG, sessWithRAG, conv, now)
	if err != nil {
		t.Fatalf("pipeWithRAG failed: %v", err)
	}
	promptWithRAG := ai.BuildDelimitedPrompt(capturerWithRAG.capturedCompact)
	tokensWithRAG := capturerWithRAG.capturedCompact.EstimatedTokens
	charsWithRAG := len(promptWithRAG)

	ragInjectedTokens := tokensWithRAG - tokensNoRAG
	tokenCap := skills.DefaultMaxRAGTokens
	capUtilization := (float64(ragInjectedTokens) / float64(tokenCap)) * 100.0

	t.Log("================================================================================")
	t.Log("                    KAIZEN EVALUATION: MILESTONE 3 CP4 RAG                      ")
	t.Log("================================================================================")
	t.Logf("METRIC 1 [PROMPT TOKENS]:")
	t.Logf("  - CP4 Without RAG: %d compact tokens | %d prompt chars (~%d prompt tokens)",
		tokensNoRAG, charsNoRAG, charsNoRAG/4)
	t.Logf("  - CP4 With RAG:    %d compact tokens | %d prompt chars (~%d prompt tokens)",
		tokensWithRAG, charsWithRAG, charsWithRAG/4)
	t.Logf("  - Injected Delta:  +%d compact tokens (+%d prompt chars)",
		ragInjectedTokens, charsWithRAG-charsNoRAG)

	t.Logf("METRIC 2 [TOKEN CAP UTILIZATION]:")
	t.Logf("  - RAG Enforced Cap:      %d tokens (max %d chars)", tokenCap, tokenCap*4)
	t.Logf("  - RAG Injected Tokens:   %d tokens", ragInjectedTokens)
	t.Logf("  - Cap Utilization Rate:  %.1f%%", capUtilization)
	t.Logf("  - Global Budget Cap:     %d tokens (max allowed by skills)", skills.MaxInputTokensBudget)
	t.Logf("  - Total Context Budget:  %d / %d (%.1f%% of max safe budget)",
		tokensWithRAG, skills.MaxInputTokensBudget, (float64(tokensWithRAG)/float64(skills.MaxInputTokensBudget))*100.0)

	// Validar stress cap con documento extralargo
	stressRAGEng := rag.NewEngine()
	hugeDoc := strings.Repeat("Texto de garantía de prueba extremadamente largo para verificar límite estricto de tokens. ", 60)
	_, _ = stressRAGEng.IndexDocument(ctx, "huge_doc", "warranty", "Huge Title", hugeDoc)
	capturerStress := &capturingAIProvider{replyJSON: validReplyJSON}
	stStress := store.NewMemoryStore()
	engStress := NewEngine(DefaultSafeConfig(), stStress)
	waStress := channel.NewStubChannel("whatsapp", 650)
	_ = engStress.RegisterChannel(waStress)
	pipeStress := NewPipeline(PipelineConfig{
		Engine:    engStress,
		InvCache:  inventory.NewSeededStubCache(now),
		Fast:      fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
		Guard:     guard.NewGuard([]string{"buyacar.com.co"}),
		AIOrch:    ai.NewOrchestrator(nil, capturerStress, skills.NewApprovedCache()),
		EscQueue:  escalate.NewQueue(),
		CRM:       crm.NewMemoryManager(),
		RAGEngine: stressRAGEng,
	})
	convStress := channel.Conversation{ID: "conv-stress-01", ChannelID: "whatsapp", CustomerAlias: "Cliente_Stress", UpdatedAt: now}
	waStress.SeedThread(convStress, []channel.Message{{ID: "m1", Sender: "customer", Text: "¿Qué respaldo de garantía y cobertura legal tienen los vehículos usados certificados?", Timestamp: now.Add(-1 * time.Minute)}})
	sessStress, _ := waStress.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})
	_, _ = pipeStress.ProcessConversation(ctx, waStress, sessStress, convStress, now)
	stressRAGChars := len(capturerStress.capturedCompact.RAGSummary)
	stressRAGTokens := (stressRAGChars / 4)
	t.Logf("  - Stress Cap Test:       %d chars (~%d tokens) injected (cap: 600 chars / 150 tokens) - ENFORCED: %v",
		stressRAGChars, stressRAGTokens, stressRAGChars <= tokenCap*4)

	// METRIC 3: LATENCY BENCHMARKING (500 iterations each with fresh non-duplicated conversations)
	const rounds = 500
	var latenciesNoRAG []time.Duration
	var latenciesWithRAG []time.Duration

	for i := 0; i < rounds; i++ {
		cid := fmt.Sprintf("conv-norag-%d", i)
		cIter := channel.Conversation{ID: cid, ChannelID: "whatsapp", CustomerAlias: "Bench", UpdatedAt: now}
		waNoRAG.SeedThread(cIter, []channel.Message{{ID: "m1", Sender: "customer", Text: queryText, Timestamp: now.Add(-1 * time.Minute)}})
		start := time.Now()
		_, _ = pipeNoRAG.ProcessConversation(ctx, waNoRAG, sessNoRAG, cIter, now)
		latenciesNoRAG = append(latenciesNoRAG, time.Since(start))
	}

	for i := 0; i < rounds; i++ {
		cid := fmt.Sprintf("conv-withrag-%d", i)
		cIter := channel.Conversation{ID: cid, ChannelID: "whatsapp", CustomerAlias: "Bench", UpdatedAt: now}
		waWithRAG.SeedThread(cIter, []channel.Message{{ID: "m1", Sender: "customer", Text: queryText, Timestamp: now.Add(-1 * time.Minute)}})
		start := time.Now()
		_, _ = pipeWithRAG.ProcessConversation(ctx, waWithRAG, sessWithRAG, cIter, now)
		latenciesWithRAG = append(latenciesWithRAG, time.Since(start))
	}

	sort.Slice(latenciesNoRAG, func(i, j int) bool { return latenciesNoRAG[i] < latenciesNoRAG[j] })
	sort.Slice(latenciesWithRAG, func(i, j int) bool { return latenciesWithRAG[i] < latenciesWithRAG[j] })

	avgNoRAG := calcAvgDuration(latenciesNoRAG)
	avgWithRAG := calcAvgDuration(latenciesWithRAG)
	p50NoRAG := latenciesNoRAG[int(float64(rounds)*0.50)]
	p50WithRAG := latenciesWithRAG[int(float64(rounds)*0.50)]
	p95NoRAG := latenciesNoRAG[int(float64(rounds)*0.95)]
	p95WithRAG := latenciesWithRAG[int(float64(rounds)*0.95)]
	p99NoRAG := latenciesNoRAG[int(float64(rounds)*0.99)]
	p99WithRAG := latenciesWithRAG[int(float64(rounds)*0.99)]

	latencyOverheadAvg := avgWithRAG - avgNoRAG
	latencyOverheadP95 := p95WithRAG - p95NoRAG

	t.Logf("METRIC 3 [CP4 PIPELINE LATENCY IMPACT (n=%d)]:", rounds)
	t.Logf("  - Pipeline Without RAG: avg=%v | p50=%v | p95=%v | p99=%v",
		avgNoRAG, p50NoRAG, p95NoRAG, p99NoRAG)
	t.Logf("  - Pipeline With RAG:    avg=%v | p50=%v | p95=%v | p99=%v",
		avgWithRAG, p50WithRAG, p95WithRAG, p99WithRAG)
	t.Logf("  - Retrieval Overhead:   avg delta=+%v | p95 delta=+%v",
		latencyOverheadAvg, latencyOverheadP95)

	// METRIC 4: FAST-PATH PRESERVATION
	fastConv := channel.Conversation{ID: "conv-fast-01", ChannelID: "whatsapp", CustomerAlias: "Cliente_Fast", UpdatedAt: now}
	waWithRAG.SeedThread(fastConv, []channel.Message{{ID: "m2", Sender: "customer", Text: "buenos dias", Timestamp: now.Add(-1 * time.Minute)}})
	startFast := time.Now()
	fastOut, _ := pipeWithRAG.ProcessConversation(ctx, waWithRAG, sessWithRAG, fastConv, now)
	fastDuration := time.Since(startFast)
	t.Logf("FAST-PATH PRESERVATION:")
	t.Logf("  - Greeting query ('buenos dias'): UsedFastPath=%v UsedAI=%v Latency=%v",
		fastOut.UsedFastPath, fastOut.UsedAI, fastDuration)

	t.Log("================================================================================")

	// Assertions for Quality Gates
	if !outWithRAG.UsedAI {
		t.Fatalf("expected outWithRAG to use AI")
	}
	if !outNoRAG.UsedAI {
		t.Fatalf("expected outNoRAG to use AI")
	}
	if tokensWithRAG <= tokensNoRAG {
		t.Fatalf("expected tokensWithRAG (%d) > tokensNoRAG (%d)", tokensWithRAG, tokensNoRAG)
	}
	if stressRAGChars > tokenCap*4 {
		t.Fatalf("stress RAG chars (%d) exceeded cap (%d)", stressRAGChars, tokenCap*4)
	}
	if !fastOut.UsedFastPath {
		t.Fatalf("expected fast path to handle greeting")
	}
}

func calcAvgDuration(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	var sum int64
	for _, d := range durations {
		sum += int64(d)
	}
	return time.Duration(sum / int64(len(durations)))
}

func BenchmarkPipeline_WithoutRAG(b *testing.B) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 14, 0, 0, 0, time.UTC)
	validReplyJSON := []byte(`{"proposed_reply":"Garantía BuyaCAR","intent":"warranty","confidence":0.95}`)
	capturer := &capturingAIProvider{replyJSON: validReplyJSON}
	st := store.NewMemoryStore()
	eng := NewEngine(DefaultSafeConfig(), st)
	wa := channel.NewStubChannel("whatsapp", 650)
	_ = eng.RegisterChannel(wa)
	pipe := NewPipeline(PipelineConfig{
		Engine:    eng,
		InvCache:  inventory.NewSeededStubCache(now),
		Fast:      fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
		Guard:     guard.NewGuard([]string{"buyacar.com.co"}),
		AIOrch:    ai.NewOrchestrator(nil, capturer, skills.NewApprovedCache()),
		EscQueue:  escalate.NewQueue(),
		CRM:       crm.NewMemoryManager(),
		RAGEngine: nil,
	})
	sess, _ := wa.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		conv := channel.Conversation{ID: fmt.Sprintf("conv-norag-%d", i), ChannelID: "whatsapp", CustomerAlias: "Bench", UpdatedAt: now}
		wa.SeedThread(conv, []channel.Message{{ID: "m1", Sender: "customer", Text: "¿Qué respaldo de garantía legal tienen los usados certificados?", Timestamp: now.Add(-1 * time.Minute)}})
		b.StartTimer()
		_, _ = pipe.ProcessConversation(ctx, wa, sess, conv, now)
	}
}

func BenchmarkPipeline_WithRAG(b *testing.B) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 14, 0, 0, 0, time.UTC)
	ragEng, _ := setupEvaluationFixtures(b)
	validReplyJSON := []byte(`{"proposed_reply":"Garantía BuyaCAR","intent":"warranty","confidence":0.95}`)
	capturer := &capturingAIProvider{replyJSON: validReplyJSON}
	st := store.NewMemoryStore()
	eng := NewEngine(DefaultSafeConfig(), st)
	wa := channel.NewStubChannel("whatsapp", 650)
	_ = eng.RegisterChannel(wa)
	pipe := NewPipeline(PipelineConfig{
		Engine:    eng,
		InvCache:  inventory.NewSeededStubCache(now),
		Fast:      fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
		Guard:     guard.NewGuard([]string{"buyacar.com.co"}),
		AIOrch:    ai.NewOrchestrator(nil, capturer, skills.NewApprovedCache()),
		EscQueue:  escalate.NewQueue(),
		CRM:       crm.NewMemoryManager(),
		RAGEngine: ragEng,
	})
	sess, _ := wa.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		conv := channel.Conversation{ID: fmt.Sprintf("conv-withrag-%d", i), ChannelID: "whatsapp", CustomerAlias: "Bench", UpdatedAt: now}
		wa.SeedThread(conv, []channel.Message{{ID: "m1", Sender: "customer", Text: "¿Qué respaldo de garantía legal tienen los usados certificados?", Timestamp: now.Add(-1 * time.Minute)}})
		b.StartTimer()
		_, _ = pipe.ProcessConversation(ctx, wa, sess, conv, now)
	}
}
