package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestTokenizeAndSpanishStopwords(t *testing.T) {
	input := "¿Cuál es la garantía y requisitos de crédito para un vehículo en BuyaCAR Boyacá?!"
	tokens := Tokenize(input)

	// Stop words like "cual", "es", "la", "y", "de", "para", "un", "en" should be stripped
	for _, tok := range tokens {
		if tok == "cual" || tok == "es" || tok == "de" || tok == "la" || tok == "para" || tok == "un" || tok == "en" {
			t.Errorf("Stopword %q was not removed", tok)
		}
	}

	expectedPresent := []string{"garantia", "requisitos", "credito", "vehiculo", "buyacar", "boyaca"}
	for _, exp := range expectedPresent {
		found := false
		for _, tok := range tokens {
			if tok == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected token %q not found in token list: %v", exp, tokens)
		}
	}
}

func TestBM25Scoring(t *testing.T) {
	idx := NewBM25Index()

	idx.AddDocument("doc1", "Garantía de caja y motor por 1 año en BuyaCAR Tunja")
	idx.AddDocument("doc2", "Requisitos de crédito para comprar carro usado en Boyacá")
	idx.AddDocument("doc3", "Mantenimiento preventivo y cambio de aceite para vehículos")

	// Query for garantía
	scores := idx.Score("garantía motor")
	if len(scores) == 0 {
		t.Fatalf("Expected non-empty scores for 'garantía motor'")
	}
	if scores[0].ID != "doc1" {
		t.Errorf("Expected doc1 as top result, got %s", scores[0].ID)
	}

	// Query for crédito
	creditScores := idx.Score("crédito requisitos")
	if len(creditScores) == 0 || creditScores[0].ID != "doc2" {
		t.Errorf("Expected doc2 as top result for credit query, got %v", creditScores)
	}

	// Irrelevant query
	noneScores := idx.Score("palabraInexistenteTotalmenteXYZ")
	if len(noneScores) != 0 {
		t.Errorf("Expected 0 scores for unknown word, got %v", noneScores)
	}
}

func TestVectorOperations(t *testing.T) {
	a := []float32{1.0, 2.0, 3.0}
	b := []float32{4.0, 5.0, 6.0}

	dot := DotProduct(a, b)
	expectedDot := float32(1*4 + 2*5 + 3*6) // 4 + 10 + 18 = 32
	if math.Abs(float64(dot-expectedDot)) > 1e-5 {
		t.Errorf("DotProduct = %f, expected %f", dot, expectedDot)
	}

	// 3-4-0 norm
	c := []float32{3.0, 4.0, 0.0}
	normC := Norm(c)
	if math.Abs(float64(normC-5.0)) > 1e-5 {
		t.Errorf("Norm = %f, expected 5.0", normC)
	}

	// Normalize
	normed := Normalize(c)
	if math.Abs(float64(Norm(normed)-1.0)) > 1e-5 {
		t.Errorf("Norm of normalized vector = %f, expected 1.0", Norm(normed))
	}

	// Cosine Similarity: Identical
	simIdentical := CosineSimilarity(c, c)
	if math.Abs(simIdentical-1.0) > 1e-5 {
		t.Errorf("CosineSimilarity of identical vectors = %f, expected 1.0", simIdentical)
	}

	// Cosine Similarity: Orthogonal
	x := []float32{1.0, 0.0}
	y := []float32{0.0, 1.0}
	simOrtho := CosineSimilarity(x, y)
	if math.Abs(simOrtho-0.0) > 1e-5 {
		t.Errorf("CosineSimilarity of orthogonal vectors = %f, expected 0.0", simOrtho)
	}

	// Cosine Similarity: Opposite
	negX := []float32{-1.0, 0.0}
	simOpp := CosineSimilarity(x, negX)
	if math.Abs(simOpp - -1.0) > 1e-5 {
		t.Errorf("CosineSimilarity of opposite vectors = %f, expected -1.0", simOpp)
	}
}

func TestFallbackEmbedder(t *testing.T) {
	fb := NewFallbackEmbedder(256)
	if fb.Dimension() != 256 {
		t.Errorf("Dimension = %d, expected 256", fb.Dimension())
	}

	ctx := context.Background()
	v1, err := fb.Embed(ctx, "requisitos para credito vehicular en Boyaca")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	v2, err := fb.Embed(ctx, "solicitud y requisitos de financiamiento de credito")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	v3, err := fb.Embed(ctx, "receta de sopa de cebolla con queso")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	simRelated := CosineSimilarity(v1, v2)
	simUnrelated := CosineSimilarity(v1, v3)

	if simRelated <= simUnrelated {
		t.Errorf("Expected related sim (%f) > unrelated sim (%f)", simRelated, simUnrelated)
	}
}

func TestOllamaEmbedder_SuccessAndFallback(t *testing.T) {
	ctx := context.Background()

	// 1. Mock Ollama Server returning valid 768-dim embedding
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollamaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		dummyVec := make([]float64, 768)
		for i := range dummyVec {
			dummyVec[i] = 0.05
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ollamaResponse{Embedding: dummyVec})
	}))
	defer server.Close()

	client, err := NewOllamaEmbedder(
		WithOllamaEndpoint(server.URL),
		WithOllamaTimeout(2*time.Second),
	)
	if err != nil {
		t.Fatalf("Failed to create OllamaEmbedder: %v", err)
	}

	vec, err := client.Embed(ctx, "Prueba de embedder ollama")
	if err != nil {
		t.Fatalf("Embed call failed: %v", err)
	}
	if len(vec) != 768 {
		t.Errorf("Expected 768 dimensions from mock Ollama, got %d", len(vec))
	}

	// 2. Fallback when server returns 500 error
	errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errServer.Close()

	fallbackClient, err := NewOllamaEmbedder(
		WithOllamaEndpoint(errServer.URL),
		WithOllamaTimeout(500*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("Failed to create fallback client: %v", err)
	}

	fbVec, err := fallbackClient.Embed(ctx, "Texto con fallback automático")
	if err != nil {
		t.Fatalf("Fallback should not error, got: %v", err)
	}
	if len(fbVec) != DefaultVectorDim {
		t.Errorf("Expected fallback dimension %d, got %d", DefaultVectorDim, len(fbVec))
	}

	// 3. Security validation: rejecting non-localhost endpoint
	_, errBadHost := NewOllamaEmbedder(WithOllamaEndpoint("http://malicious-external-api.com:11434/api/embeddings"))
	if errBadHost == nil {
		t.Errorf("Expected security error for non-localhost endpoint, got nil")
	}
}

func TestHybridRanker(t *testing.T) {
	bm25Scores := []DocScore{
		{ID: "docA", Score: 4.5},
		{ID: "docB", Score: 2.1},
		{ID: "docC", Score: 1.0},
	}
	vectorScores := []DocScore{
		{ID: "docB", Score: 0.85},
		{ID: "docA", Score: 0.80},
		{ID: "docD", Score: 0.90},
	}

	// RRF Fusion
	rrfRanker := NewHybridRanker(WithFusionStrategy(FusionRRF), WithRRFConstant(60))
	fusedRRF := rrfRanker.Fuse(bm25Scores, vectorScores)
	if len(fusedRRF) != 4 {
		t.Fatalf("Expected 4 fused docs, got %d", len(fusedRRF))
	}
	// docA (rank 1 in BM25, rank 2 in Vec) and docB (rank 2 in BM25, rank 1 in Vec) should be top
	if fusedRRF[0].ID != "docA" && fusedRRF[0].ID != "docB" {
		t.Errorf("Expected docA or docB top in RRF, got %s", fusedRRF[0].ID)
	}

	// Weighted Fusion
	weightedRanker := NewHybridRanker(WithFusionStrategy(FusionWeighted), WithWeights(0.6, 0.4))
	fusedWeighted := weightedRanker.Fuse(bm25Scores, vectorScores)
	if len(fusedWeighted) != 4 {
		t.Fatalf("Expected 4 fused docs in weighted, got %d", len(fusedWeighted))
	}
	if fusedWeighted[0].Score < fusedWeighted[1].Score {
		t.Errorf("Weighted fusion results not sorted descending")
	}
}

func TestEngine_IndexAndRetrieve(t *testing.T) {
	ctx := context.Background()
	engine := NewEngine()

	// 1. Index warranty doc
	warrantyText := `BuyaCAR Tunja ofrece garantía de 1 año o 20000 kilómetros para motor y caja de cambios en todos los vehículos usados certificados.
Comunícate con asesor oficial al 3151212223 para activar tu cobertura.`
	_, err := engine.IndexDocument(ctx, "garantia_01", "warranty", "Política de Garantía BuyaCAR", warrantyText)
	if err != nil {
		t.Fatalf("IndexDocument warranty failed: %v", err)
	}

	// 2. Index credit doc
	creditText := `Requisitos para crédito vehicular en Boyacá: cédula de ciudadanía, comprobante de ingresos de los últimos 3 meses y no tener reportes negativos.
Tasa preferencial desde el 1.2% mensual. Cuota inicial desde $15000000 COP.`
	_, err = engine.IndexDocument(ctx, "credito_01", "credit", "Requisitos de Crédito", creditText)
	if err != nil {
		t.Fatalf("IndexDocument credit failed: %v", err)
	}

	// 3. Index vehicle catalog
	catalogText := `Renault Duster 2021 4x4 color gris, kilometraje 45000 km, precio de contado $68000000 COP en la sede Tunja.
Línea directa de contacto 3022866969.`
	_, err = engine.IndexDocument(ctx, "catalog_duster", "catalog", "Renault Duster 2021", catalogText)
	if err != nil {
		t.Fatalf("IndexDocument catalog failed: %v", err)
	}

	if engine.DocumentCount() != 3 {
		t.Errorf("Expected 3 documents, got %d", engine.DocumentCount())
	}

	// Query: Credit
	results, err := engine.Retrieve(ctx, Query{
		Text: "requisitos para solicitar credito vehicular",
		TopK: 2,
	})
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("Expected results for credit query")
	}
	if results[0].Chunk.Category != "credit" {
		t.Errorf("Expected top result category 'credit', got %s", results[0].Chunk.Category)
	}

	// Query with Category filter
	catResults, err := engine.Retrieve(ctx, Query{
		Text:     "Tunja vehiculo garantia",
		Category: "catalog",
		TopK:     2,
	})
	if err != nil {
		t.Fatalf("Retrieve with category filter failed: %v", err)
	}
	for _, r := range catResults {
		if r.Chunk.Category != "catalog" {
			t.Errorf("Expected only 'catalog' chunks, got %s", r.Chunk.Category)
		}
	}

	// MaxTokens Budgeting
	budgetResults, err := engine.Retrieve(ctx, Query{
		Text:      "credito y garantia",
		TopK:      5,
		MaxTokens: 25,
	})
	if err != nil {
		t.Fatalf("Retrieve with MaxTokens failed: %v", err)
	}
	totalTokens := 0
	for _, r := range budgetResults {
		totalTokens += r.Chunk.Tokens
	}
	if totalTokens > 50 { // with first chunk allowance, should not exceed significantly
		t.Errorf("Total tokens %d exceeded expected budget", totalTokens)
	}
}

func TestEngine_BackedNumbers(t *testing.T) {
	ctx := context.Background()
	engine := NewEngine()

	docText := `Contacto oficial: 3151212223 y WhatsApp 3022866969.
Precio de venta: 48000000 COP. Kilometraje certificado: 45000 km. Código 202401.`

	_, err := engine.IndexDocument(ctx, "doc_numbers", "catalog", "Ficha Mazda 2", docText)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	backed := engine.GetBackedNumbers()
	expectedNums := []string{"202401", "3022866969", "3151212223", "45000", "48000000"}

	for _, exp := range expectedNums {
		found := false
		for _, b := range backed {
			if b == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected backed number %s not found in %v", exp, backed)
		}
	}
}

func TestEngine_ConcurrencyAndRace(t *testing.T) {
	ctx := context.Background()
	engine := NewEngine()

	var wg sync.WaitGroup
	workers := 8
	iterations := 25

	// Concurrently index documents
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				docID := fmt.Sprintf("doc_w%d_i%d", workerID, i)
				title := fmt.Sprintf("Vehículo Modelo %d", i)
				text := fmt.Sprintf("Descripción para vehículo %d con precio %d COP y teléfono 3151212223.", i, 50000000+i*1000)
				_, _ = engine.IndexDocument(ctx, docID, "catalog", title, text)
			}
		}(w)
	}

	// Concurrently retrieve documents
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_, _ = engine.Retrieve(ctx, Query{
					Text: fmt.Sprintf("vehiculo modelo %d", i%5),
					TopK: 3,
				})
				_ = engine.GetBackedNumbers()
			}
		}(w)
	}

	wg.Wait()

	if engine.ChunkCount() == 0 {
		t.Errorf("Expected chunks after concurrent indexing, got 0")
	}
}

func BenchmarkBM25Search(b *testing.B) {
	idx := NewBM25Index()
	for i := 0; i < 100; i++ {
		idx.AddDocument(
			fmt.Sprintf("doc_%d", i),
			fmt.Sprintf("Vehículo usado certificado modelo %d con garantía extendida en Tunja Boyacá precio %d", 2015+i%8, 30000000+i*1000000),
		)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = idx.Score("garantía extendida vehículo Tunja")
	}
}

func BenchmarkCosineSimilarity(b *testing.B) {
	fb := NewFallbackEmbedder(256)
	ctx := context.Background()
	v1, _ := fb.Embed(ctx, "requisitos de credito y financiamiento vehicular en Boyaca")
	v2, _ := fb.Embed(ctx, "politica de garantia y cobertura para motor y caja")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CosineSimilarity(v1, v2)
	}
}

func BenchmarkHybridRetrieve(b *testing.B) {
	ctx := context.Background()
	engine := NewEngine()

	for i := 0; i < 20; i++ {
		_, _ = engine.IndexDocument(
			ctx,
			fmt.Sprintf("doc_%d", i),
			"catalog",
			fmt.Sprintf("Camioneta Renault Duster %d", 2018+i%6),
			fmt.Sprintf("Camioneta 4x4 en excelente estado en Tunja Boyacá. Precio %d COP. Garantía 1 año. Teléfono 3151212223.", 45000000+i*2000000),
		)
	}

	q := Query{
		Text: "camioneta duster en Tunja con garantia",
		TopK: 3,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = engine.Retrieve(ctx, q)
	}
}
