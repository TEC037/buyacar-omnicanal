package rag

import (
	"context"
	"sort"
	"testing"
	"time"
)

func TestKaizenEvaluation(t *testing.T) {
	ctx := context.Background()
	engine := NewEngine()

	// Seed 30 documents across warranty, credit, catalog, and maintenance
	docs := []struct {
		id       string
		category string
		title    string
		content  string
	}{
		{
			id:       "doc_warr_01",
			category: "warranty",
			title:    "Garantía Mecánica BuyaCAR",
			content:  "BuyaCAR Tunja ofrece garantía de 1 año o 20000 kilómetros para motor y transmisión en vehículos certificados. Contacto al 3151212223.",
		},
		{
			id:       "doc_warr_02",
			category: "warranty",
			title:    "Cobertura de Sistema Eléctrico",
			content:  "La garantía extendida cubre alternador, motor de arranque y batería por 6 meses. Válido en talleres autorizados de Boyacá.",
		},
		{
			id:       "doc_cred_01",
			category: "credit",
			title:    "Requisitos Crédito Persona Natural",
			content:  "Documentos requeridos para crédito de vehículo: cédula de ciudadanía original, extractos bancarios de 3 meses y carta laboral vigente.",
		},
		{
			id:       "doc_cred_02",
			category: "credit",
			title:    "Financiamiento y Cuota Inicial",
			content:  "Financiamos hasta el 90% del valor comercial. Cuota inicial desde $5000000 COP con tasas preferenciales desde 1.1% MV en Boyacá.",
		},
		{
			id:       "doc_cat_01",
			category: "catalog",
			title:    "Renault Duster 2021 4x4",
			content:  "Camioneta Renault Duster Zen 2021 4x4 gris cometa, 45000 km, precio de oferta $68000000 COP. Disponible en Tunja, celular 3022866969.",
		},
		{
			id:       "doc_cat_02",
			category: "catalog",
			title:    "Mazda 2 Touring 2020",
			content:  "Automóvil Mazda 2 Touring 2020 automático rojo diamante, 38000 km, único dueño, precio $56000000 COP en sede Boyacá.",
		},
		{
			id:       "doc_cat_03",
			category: "catalog",
			title:    "Chevrolet Tracker Turbo 2022",
			content:  "Camioneta Chevrolet Tracker Turbo Premier 2022 blanco polar, 28000 km, techo panorámico, precio $79000000 COP.",
		},
		{
			id:       "doc_maint_01",
			category: "maintenance",
			title:    "Revisión Técnico Mecánica y Alistamiento",
			content:  "Cada vehículo BuyaCAR cuenta con peritaje de 150 puntos, cambio de aceite sintético y alineación antes de entrega oficial.",
		},
	}

	for _, d := range docs {
		_, err := engine.IndexDocument(ctx, d.id, d.category, d.title, d.content)
		if err != nil {
			t.Fatalf("Failed to index doc %s: %v", d.id, err)
		}
	}

	// 1. Latency Measurement: 1000 iterations for BM25, Cosine, Hybrid
	const iterations = 1000

	// BM25 Latencies (batched to overcome Windows clock resolution)
	bm25Times := make([]time.Duration, iterations)
	bm25Query := "garantia motor transmision kilometros"
	for i := 0; i < iterations; i++ {
		start := time.Now()
		for j := 0; j < 50; j++ {
			_ = engine.bm25.Score(bm25Query)
		}
		bm25Times[i] = time.Since(start) / 50
	}
	sort.Slice(bm25Times, func(i, j int) bool { return bm25Times[i] < bm25Times[j] })
	bm25P99 := bm25Times[int(float64(iterations)*0.99)]
	bm25P50 := bm25Times[int(float64(iterations)*0.50)]

	// Cosine Similarity Latencies
	cosineTimes := make([]time.Duration, iterations)
	fb := NewFallbackEmbedder(DefaultVectorDim)
	vA, _ := fb.Embed(ctx, "requisitos credito vehiculo")
	vB, _ := fb.Embed(ctx, "financiamiento cuota inicial extractos")
	for i := 0; i < iterations; i++ {
		start := time.Now()
		for j := 0; j < 500; j++ {
			_ = CosineSimilarity(vA, vB)
		}
		cosineTimes[i] = time.Since(start) / 500
	}
	sort.Slice(cosineTimes, func(i, j int) bool { return cosineTimes[i] < cosineTimes[j] })
	cosineP99 := cosineTimes[int(float64(iterations)*0.99)]
	cosineP50 := cosineTimes[int(float64(iterations)*0.50)]

	// HybridRetrieve Latencies
	hybridTimes := make([]time.Duration, iterations)
	hQuery := Query{Text: "camioneta renault duster en Tunja precio", TopK: 3}
	for i := 0; i < iterations; i++ {
		start := time.Now()
		for j := 0; j < 10; j++ {
			_, _ = engine.Retrieve(ctx, hQuery)
		}
		hybridTimes[i] = time.Since(start) / 10
	}
	sort.Slice(hybridTimes, func(i, j int) bool { return hybridTimes[i] < hybridTimes[j] })
	hybridP99 := hybridTimes[int(float64(iterations)*0.99)]
	hybridP50 := hybridTimes[int(float64(iterations)*0.50)]

	t.Logf("KAIZEN_METRIC: BM25 p50=%.2fµs p99=%.2fµs", float64(bm25P50.Nanoseconds())/1000.0, float64(bm25P99.Nanoseconds())/1000.0)
	t.Logf("KAIZEN_METRIC: Cosine p50=%.2fµs p99=%.2fµs", float64(cosineP50.Nanoseconds())/1000.0, float64(cosineP99.Nanoseconds())/1000.0)
	t.Logf("KAIZEN_METRIC: HybridRetrieve p50=%.2fµs p99=%.2fµs", float64(hybridP50.Nanoseconds())/1000.0, float64(hybridP99.Nanoseconds())/1000.0)

	// 2. Hit-Rate Evaluation on Golden Queries
	evalCases := []struct {
		query            string
		expectedCategory string
		expectedDocID    string
	}{
		{"¿Qué cubre la garantía de motor y caja?", "warranty", "doc_warr_01"},
		{"¿Cuáles son los requisitos de crédito para carro?", "credit", "doc_cred_01"},
		{"¿Tienen camioneta Renault Duster 4x4 en Tunja?", "catalog", "doc_cat_01"},
		{"¿Cuánto es la cuota inicial mínima para financiar?", "credit", "doc_cred_02"},
		{"¿Tienen Mazda 2 automático en venta?", "catalog", "doc_cat_02"},
		{"¿Hacen peritaje técnico mecánico antes de la entrega?", "maintenance", "doc_maint_01"},
		{"¿La batería y alternador tienen garantía?", "warranty", "doc_warr_02"},
		{"¿Tienen Chevrolet Tracker Turbo?", "catalog", "doc_cat_03"},
	}

	hits := 0
	for _, tc := range evalCases {
		res, err := engine.Retrieve(ctx, Query{Text: tc.query, TopK: 2})
		if err != nil {
			t.Errorf("Retrieve error on %q: %v", tc.query, err)
			continue
		}
		found := false
		for _, r := range res {
			if r.Chunk.Category == tc.expectedCategory || r.Chunk.DocID == tc.expectedDocID {
				found = true
				break
			}
		}
		if found {
			hits++
		} else {
			t.Logf("Missed query: %q, top result was %s (%s)", tc.query, res[0].Chunk.DocID, res[0].Chunk.Category)
		}
	}
	hitRate := (float64(hits) / float64(len(evalCases))) * 100.0
	t.Logf("KAIZEN_METRIC: HitRate=%.1f%% (%d/%d hits)", hitRate, hits, len(evalCases))

	// 3. Tokens Injected vs Limit Evaluation
	tokenBudgets := []int{30, 60, 100, 200}
	for _, maxTok := range tokenBudgets {
		res, err := engine.Retrieve(ctx, Query{
			Text:      "requisitos credito garantia vehiculo duster",
			TopK:      5,
			MaxTokens: maxTok,
		})
		if err != nil {
			t.Fatalf("Retrieve failed: %v", err)
		}
		totalInjected := 0
		for _, r := range res {
			totalInjected += r.Chunk.Tokens
		}
		t.Logf("KAIZEN_METRIC: TokenBudget limit=%d injected=%d chunks=%d", maxTok, totalInjected, len(res))
	}
}
