package ai

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestQueryNormalizer_AccentsAndPunctuation(t *testing.T) {
	norm := NewQueryNormalizer()

	cases := []struct {
		input    string
		expected string
	}{
		{
			input:    "¿Qué precio tiene la Toyota Hilux 2022?",
			expected: "que precio tiene la toyota hilux 2022",
		},
		{
			input:    "  ¡¡HOLA!!   ¿Horario   de ATENCIÓN en Tunja??? ",
			expected: "hola horario de atencion en tunja",
		},
		{
			input:    "Mazda CX-5 (2023) - ¿Financiación / Bancos?",
			expected: "mazda cx 5 2023 financiacion bancos",
		},
		{
			input:    "",
			expected: "",
		},
	}

	for _, c := range cases {
		got := norm.Normalize(c.input)
		if got != c.expected {
			t.Errorf("Normalize(%q) = %q; want %q", c.input, got, c.expected)
		}
	}
}

func TestSemanticHasher_DeterministicHash(t *testing.T) {
	norm := NewQueryNormalizer()

	h1 := norm.HashQuery("¿Qué precio tiene la Toyota Hilux 2022?")
	h2 := norm.HashQuery("   que precio tiene la toyota hilux 2022   ")
	h3 := norm.HashQuery("¡Que precio tiene la Toyota Hilux 2022!")

	if h1 != h2 || h2 != h3 {
		t.Fatalf("expected deterministic hash across normalizations, got h1=%s, h2=%s, h3=%s", h1, h2, h3)
	}
	if len(h1) != 64 {
		t.Fatalf("expected 64-char sha256 hex string, got len=%d", len(h1))
	}
}

func TestSemanticCache_LRUAndTTL(t *testing.T) {
	cache := NewSemanticCache(2, 50*time.Millisecond)

	out1 := StructuredOutput{ConfigID: "1", ProposedReply: "Reply 1", Intent: "intent1"}
	out2 := StructuredOutput{ConfigID: "2", ProposedReply: "Reply 2", Intent: "intent2"}
	out3 := StructuredOutput{ConfigID: "3", ProposedReply: "Reply 3", Intent: "intent3"}

	h1 := cache.SetByQuery("query 1", out1, 50*time.Millisecond)
	h2 := cache.SetByQuery("query 2", out2, 50*time.Millisecond)

	// Verificar hits
	got1, ok := cache.Get(h1)
	if !ok || got1.ProposedReply != "Reply 1" {
		t.Fatalf("expected hit for key 1")
	}

	// Insertar key 3, debe desalojar key 2 (ya que key 1 fue accedida recientemente y es MRU)
	h3 := cache.SetByQuery("query 3", out3, 50*time.Millisecond)

	if _, ok := cache.Get(h2); ok {
		t.Fatalf("expected key 2 to be evicted by LRU capacity limit")
	}
	if _, ok := cache.Get(h1); !ok {
		t.Fatalf("expected key 1 to remain in cache")
	}
	if _, ok := cache.Get(h3); !ok {
		t.Fatalf("expected key 3 to be present in cache")
	}

	// Probar TTL
	time.Sleep(60 * time.Millisecond)
	if _, ok := cache.Get(h1); ok {
		t.Fatalf("expected key 1 to be expired by TTL")
	}
	if _, ok := cache.Get(h3); ok {
		t.Fatalf("expected key 3 to be expired by TTL")
	}

	hits, misses, evictions, size := cache.Stats()
	if hits < 2 || misses < 3 || evictions < 1 {
		t.Fatalf("unexpected cache stats: hits=%d, misses=%d, evictions=%d, size=%d", hits, misses, evictions, size)
	}
}

func TestSemanticCache_ConcurrentAccess(t *testing.T) {
	cache := NewSemanticCache(50, 1*time.Minute)

	var wg sync.WaitGroup
	// 50 escritores concurrentes
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			q := fmt.Sprintf("consulta vehiculo %d", id%10)
			out := StructuredOutput{ConfigID: fmt.Sprintf("cfg-%d", id), ProposedReply: fmt.Sprintf("Respuesta %d", id)}
			cache.SetByQuery(q, out, 1*time.Minute)
		}(i)
	}

	// 50 lectores concurrentes
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			q := fmt.Sprintf("consulta vehiculo %d", id%10)
			cache.GetByQuery(q)
		}(i)
	}

	wg.Wait()
}

func BenchmarkSemanticCache_Get(b *testing.B) {
	cache := NewSemanticCache(1000, 1*time.Hour)
	out := StructuredOutput{
		ConfigID:      "cfg-bm",
		ProposedReply: "Vehiculo certificado BuyaCAR Tunja",
		Intent:        "price",
	}
	hash := cache.SetByQuery("¿Qué precio tiene la Toyota Hilux 2022?", out, 1*time.Hour)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, ok := cache.Get(hash)
		if !ok || res.ConfigID != "cfg-bm" {
			b.Fatal("unexpected cache miss during benchmark")
		}
	}
}

func TestSemanticCache_HitsMissesAndTokensTracking(t *testing.T) {
	cache := NewSemanticCache(10, 1*time.Hour)

	// Cold start: 0 hits, 0 misses initially
	if cache.Hits() != 0 || cache.Misses() != 0 {
		t.Fatalf("expected 0 hits and 0 misses initially, got hits=%d, misses=%d", cache.Hits(), cache.Misses())
	}

	// 1. Cold start query miss
	_, ok := cache.GetByQuery("consulta no existente en frio")
	if ok {
		t.Fatalf("expected miss on cold start query")
	}
	if cache.Misses() != 1 {
		t.Fatalf("expected 1 miss recorded, got %d", cache.Misses())
	}

	// 2. Set with tokens and test hit
	out := StructuredOutput{
		ConfigID:      "cfg-tokens",
		ProposedReply: "Respuesta con tokens medidos",
		Intent:        "price",
	}
	h := cache.HashQuery("consulta con tokens")
	cache.SetWithTokens(h, out, 120, 45, 1*time.Hour)

	got, hit := cache.Get(h)
	if !hit || got.ProposedReply != "Respuesta con tokens medidos" {
		t.Fatalf("expected hit on cached query")
	}
	if cache.Hits() != 1 {
		t.Fatalf("expected 1 hit, got %d", cache.Hits())
	}
	if cache.TokensSaved() != 165 {
		t.Fatalf("expected 165 tokens saved (120+45), got %d", cache.TokensSaved())
	}
}
