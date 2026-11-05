package skills

import (
	"strings"
	"sync"
	"testing"

	"buyacar.com.co/omnicanal/internal/inventory"
	"buyacar.com.co/omnicanal/internal/rag"
	"buyacar.com.co/omnicanal/internal/triage"
)

func TestApprovedCache_HitsMissesAndTokenTracking(t *testing.T) {
	cache := NewApprovedCache()

	// Initial cold start state
	if cache.Hits() != 0 {
		t.Fatalf("expected 0 hits on cold start, got %d", cache.Hits())
	}
	if cache.Misses() != 0 {
		t.Fatalf("expected 0 misses on cold start, got %d", cache.Misses())
	}
	if cache.TokenStatus() != "not_measured" {
		t.Fatalf("expected token status 'not_measured' on cold start, got %s", cache.TokenStatus())
	}

	// 1. Cache Miss on unseeded key
	val, ok := cache.Get("inquiry-unseen")
	if ok || val != "" {
		t.Fatalf("expected cache miss, got ok=%v, val=%q", ok, val)
	}
	if cache.Misses() != 1 {
		t.Fatalf("expected 1 miss, got %d", cache.Misses())
	}

	// 2. Set an entry and test Hit
	cache.Set("¿Precio Hilux?", "La Hilux 2022 cuesta $185000000 COP")
	val2, ok2 := cache.Get("  ¿precio hilux?  ")
	if !ok2 || val2 != "La Hilux 2022 cuesta $185000000 COP" {
		t.Fatalf("expected cache hit with normalized key, got ok=%v val=%q", ok2, val2)
	}
	if cache.Hits() != 1 {
		t.Fatalf("expected 1 hit, got %d", cache.Hits())
	}

	// 3. Record real measured token savings
	cache.RecordTokensSaved(320)
	if cache.TokensSaved() != 320 {
		t.Fatalf("expected 320 tokens saved, got %d", cache.TokensSaved())
	}
	if cache.TokenStatus() != "measured" {
		t.Fatalf("expected token status 'measured', got %s", cache.TokenStatus())
	}

	// 4. Stats backwards compatibility
	hits, misses := cache.Stats()
	if hits != 1 || misses != 1 {
		t.Fatalf("expected stats (1, 1), got (%d, %d)", hits, misses)
	}
}

func TestApprovedCache_ConcurrentAccessRace(t *testing.T) {
	cache := NewApprovedCache()
	cache.Set("key1", "reply 1")
	cache.Set("key2", "reply 2")

	var wg sync.WaitGroup
	// 50 readers
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if id%2 == 0 {
				cache.Get("key1")
			} else {
				cache.Get("key-unknown")
			}
		}(i)
	}

	// 20 writers
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			cache.RecordTokensSaved(10)
		}(i)
	}

	wg.Wait()

	if cache.Hits()+cache.Misses() != 50 {
		t.Fatalf("expected 50 total operations, got %d hits + %d misses", cache.Hits(), cache.Misses())
	}
}

func TestBuildCompactContext_LimitsVehiclesAndSummary(t *testing.T) {
	candidates := []inventory.Vehicle{
		{ID: "v1", Brand: "Toyota", Model: "Hilux", Year: 2022, PriceCOP: 185000000, Status: "available", OfficialURL: "https://buyacar.com.co/v1"},
		{ID: "v2", Brand: "Mazda", Model: "CX-5", Year: 2021, PriceCOP: 115000000, Status: "available", OfficialURL: "https://buyacar.com.co/v2"},
		{ID: "v3", Brand: "Renault", Model: "Duster", Year: 2020, PriceCOP: 68000000, Status: "available", OfficialURL: "https://buyacar.com.co/v3"},
		{ID: "v4", Brand: "Chevrolet", Model: "Tracker", Year: 2023, PriceCOP: 95000000, Status: "available", OfficialURL: "https://buyacar.com.co/v4"},
	}

	ctx := BuildCompactContext(triage.IntentPrice, "Consulta precios de camionetas", candidates)
	if len(ctx.Vehicles) != MaxVehiclesInContext {
		t.Fatalf("expected max %d vehicles, got %d", MaxVehiclesInContext, len(ctx.Vehicles))
	}
	if ctx.EstimatedTokens <= 0 {
		t.Fatalf("expected estimated tokens > 0, got %d", ctx.EstimatedTokens)
	}
	if len(ctx.RAGChunks) != 0 || ctx.RAGSummary != "" {
		t.Fatalf("expected empty RAG chunks and summary for BuildCompactContext, got %d chunks, %q", len(ctx.RAGChunks), ctx.RAGSummary)
	}
}

func TestBuildCompactContextWithRAG_TokenCappingAndTruncation(t *testing.T) {
	candidates := []inventory.Vehicle{
		{ID: "v1", Brand: "Toyota", Model: "Hilux", Year: 2022, PriceCOP: 185000000, Status: "available", OfficialURL: "https://buyacar.com.co/v1"},
	}

	chunks := []rag.Chunk{
		{
			ID:       "w1",
			Title:    "Garantía BuyaCAR",
			Content:  "Todos los vehículos cuentan con garantía legal de 3 meses en caja y motor según Ley 1480 de 2011.",
			Category: "warranty",
		},
		{
			ID:       "c1",
			Title:    "Crédito Bancario",
			Content:  "Convenio con Bancolombia, Banco de Bogotá, Davivienda y Finandina con tasas preferenciales desde 1.2% MV.",
			Category: "credit",
		},
		{
			ID:       "f1",
			Title:    "Preguntas Frecuentes",
			Content:  "Recibimos su vehículo usado como parte de pago previa valoración técnica en nuestra sede de Tunja.",
			Category: "faq",
		},
	}

	// 1. With sufficient budget (e.g. 150 tokens = 600 chars), all chunks fit
	ctxFit := BuildCompactContextWithRAG(triage.IntentLegalOrClaim, "Consulta sobre garantía", candidates, chunks, 150)
	if len(ctxFit.RAGChunks) != 3 {
		t.Fatalf("expected 3 chunks included, got %d", len(ctxFit.RAGChunks))
	}
	if !strings.Contains(ctxFit.RAGSummary, "Garantía BuyaCAR") || !strings.Contains(ctxFit.RAGSummary, "Crédito Bancario") {
		t.Fatalf("expected both warranty and credit in summary, got %s", ctxFit.RAGSummary)
	}
	if ctxFit.EstimatedTokens <= 0 {
		t.Fatalf("expected estimated tokens > 0, got %d", ctxFit.EstimatedTokens)
	}

	// 2. With tight budget (e.g. 35 tokens = 140 chars), chunks are capped and/or truncated
	ctxTight := BuildCompactContextWithRAG(triage.IntentLegalOrClaim, "Consulta garantía", candidates, chunks, 35)
	maxCharsAllowed := 35 * 4
	if len(ctxTight.RAGSummary) > maxCharsAllowed {
		t.Fatalf("expected RAGSummary length <= %d chars, got %d chars: %s", maxCharsAllowed, len(ctxTight.RAGSummary), ctxTight.RAGSummary)
	}
	if len(ctxTight.RAGChunks) == 0 {
		t.Fatal("expected at least one chunk to be included or truncated")
	}

	// 3. Backward compatibility and default max tokens when maxRAGTokens <= 0
	ctxDefault := BuildCompactContextWithRAG(triage.IntentLegalOrClaim, "Consulta", candidates, chunks, 0)
	defaultMaxChars := DefaultMaxRAGTokens * 4
	if len(ctxDefault.RAGSummary) > defaultMaxChars {
		t.Fatalf("expected default capped length <= %d chars, got %d", defaultMaxChars, len(ctxDefault.RAGSummary))
	}
}
