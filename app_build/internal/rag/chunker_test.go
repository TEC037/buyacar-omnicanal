package rag

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestChunkMarkdown_StructuralSections(t *testing.T) {
	md := `# Guía de Prueba BuyaCAR

Introducción general al catálogo vehicular en Boyacá.

## Vehículo Renault Duster 2021
Detalles del Renault Duster 2021 con motor 1.6L y precio de $72.000.000 COP (72000000).
Kilometraje certificado de 45.000 km (45000).

## Vehículo Mazda CX-30 2023
Detalles del Mazda CX-30 2023 con motor 2.0L Skyactiv-G y precio de $105.000.000 COP (105000000).
Kilometraje certificado de 22.000 km (22000).

### Opciones de Financiación
Planes con Bancolombia y Davivienda con cuota inicial desde el 10%.
`

	chunker := NewSemanticChunker(DefaultChunkingConfig())
	chunks, err := chunker.ChunkMarkdown("test_doc", "catalog", []byte(md))
	if err != nil {
		t.Fatalf("unexpected error chunking markdown: %v", err)
	}

	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
	}

	// Verify IDs, doc_id, category
	for i, c := range chunks {
		expectedID := fmt.Sprintf("test_doc_c%03d", i)
		if c.ID != expectedID {
			t.Errorf("chunk %d has ID %s, expected %s", i, c.ID, expectedID)
		}
		if c.DocID != "test_doc" {
			t.Errorf("chunk %d has DocID %s, expected test_doc", i, c.DocID)
		}
		if c.Category != "catalog" {
			t.Errorf("chunk %d has Category %s, expected catalog", i, c.Category)
		}
		if c.Tokens <= 0 {
			t.Errorf("chunk %d has invalid token count: %d", i, c.Tokens)
		}
	}

	// Check Duster chunk backed numbers
	var dusterFound, cx30Found bool
	for _, c := range chunks {
		if strings.Contains(c.Title, "Duster") {
			dusterFound = true
			if len(c.BackedNumbers) == 0 {
				t.Errorf("expected backed numbers in Duster chunk, got none")
			}
			has72M := false
			for _, num := range c.BackedNumbers {
				if num == "72000000" {
					has72M = true
				}
			}
			if !has72M {
				t.Errorf("expected 72000000 in Duster backed numbers, got %v", c.BackedNumbers)
			}
		}
		if strings.Contains(c.Title, "CX-30") {
			cx30Found = true
			has105M := false
			for _, num := range c.BackedNumbers {
				if num == "105000000" {
					has105M = true
				}
			}
			if !has105M {
				t.Errorf("expected 105000000 in CX-30 backed numbers, got %v", c.BackedNumbers)
			}
		}
	}

	if !dusterFound || !cx30Found {
		t.Errorf("expected both Duster and CX-30 chunks to be identified, duster=%v, cx30=%v", dusterFound, cx30Found)
	}
}

func TestChunkMarkdown_BoundaryEnforcement(t *testing.T) {
	// Construct a markdown section with multiple long paragraphs
	var sb strings.Builder
	sb.WriteString("# Documento Extenso con Límites\n\n## Sección Principal Extensa\n")
	for i := 1; i <= 20; i++ {
		sb.WriteString(fmt.Sprintf("Párrafo número %d de prueba detallada. Contiene información técnica relevante para evaluar la partición determinista por párrafos en el chunker de BuyaCAR en Tunja Boyacá. El valor comercial es de 5000000%d.\n\n", i, i))
	}

	cfg := ChunkingConfig{
		MaxTokensPerChunk: 50,
		MinTokensPerChunk: 10,
		MaxCharsPerChunk:  300,
	}

	chunker := NewSemanticChunker(cfg)
	chunks, err := chunker.ChunkMarkdown("boundary_doc", "test", []byte(sb.String()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(chunks) <= 1 {
		t.Fatalf("expected document to be split into multiple chunks, got %d", len(chunks))
	}

	for i, c := range chunks {
		if len(c.Content) > cfg.MaxCharsPerChunk {
			t.Errorf("chunk %d exceeded MaxCharsPerChunk: got %d, max %d", i, len(c.Content), cfg.MaxCharsPerChunk)
		}
		if c.Tokens > cfg.MaxTokensPerChunk {
			t.Errorf("chunk %d exceeded MaxTokensPerChunk: got %d, max %d", i, c.Tokens, cfg.MaxTokensPerChunk)
		}
	}
}

func TestChunkJSON_Vehicles(t *testing.T) {
	jsonContent := `[
  {
    "id": "veh-duster-2021",
    "brand": "Renault",
    "model": "Duster Zen 1.6",
    "year": 2021,
    "price_cop": 72000000,
    "mileage_km": 45000,
    "transmission": "Mecánica 5 velocidades",
    "engine": "1.6L 16V 114 HP",
    "traction": "4x2",
    "color": "Gris Estrella",
    "body_type": "SUV",
    "plate_last_char": "5",
    "city": "Tunja",
    "department": "Boyacá",
    "features": [
      "Frenos ABS con repartidor electrónico EBD",
      "Control electrónico de estabilidad ESP"
    ],
    "soat_valid": true,
    "tecno_valid": true,
    "status": "available",
    "official_url": "https://buyacar.com.co/vehiculos/renault-duster-zen-2021"
  },
  {
    "id": "veh-tracker-2022",
    "brand": "Chevrolet",
    "model": "Tracker Premier Turbo",
    "year": 2022,
    "price_cop": 86000000,
    "mileage_km": 28000,
    "transmission": "Automática secuencial 6 velocidades",
    "engine": "1.2L Turbo 132 HP 190 Nm",
    "traction": "4x2",
    "color": "Azul Lunar",
    "body_type": "SUV",
    "plate_last_char": "1",
    "city": "Tunja",
    "department": "Boyacá",
    "features": [
      "6 airbags",
      "Frenado autónomo de emergencia"
    ],
    "soat_valid": true,
    "tecno_valid": true,
    "status": "available",
    "official_url": "https://buyacar.com.co/vehiculos/chevrolet-tracker-premier-2022"
  }
]`

	chunker := NewSemanticChunker()
	chunks, err := chunker.ChunkJSON("vehicles_doc", "catalog", []byte(jsonContent))
	if err != nil {
		t.Fatalf("unexpected error chunking JSON: %v", err)
	}

	if len(chunks) != 2 {
		t.Fatalf("expected 2 vehicle chunks, got %d", len(chunks))
	}

	// Verify first chunk
	c0 := chunks[0]
	if c0.ID != "vehicles_doc_veh-duster-2021" {
		t.Errorf("expected chunk ID vehicles_doc_veh-duster-2021, got %s", c0.ID)
	}
	if c0.Metadata["brand"] != "Renault" || c0.Metadata["price_cop"] != "72000000" {
		t.Errorf("unexpected metadata in c0: %v", c0.Metadata)
	}
	has72M := false
	has45k := false
	for _, n := range c0.BackedNumbers {
		if n == "72000000" {
			has72M = true
		}
		if n == "45000" {
			has45k = true
		}
	}
	if !has72M || !has45k {
		t.Errorf("expected 72000000 and 45000 in backed numbers, got %v", c0.BackedNumbers)
	}

	// Verify second chunk
	c1 := chunks[1]
	if c1.ID != "vehicles_doc_veh-tracker-2022" {
		t.Errorf("expected chunk ID vehicles_doc_veh-tracker-2022, got %s", c1.ID)
	}
	if c1.Metadata["brand"] != "Chevrolet" || c1.Metadata["mileage_km"] != "28000" {
		t.Errorf("unexpected metadata in c1: %v", c1.Metadata)
	}
}

func TestChunkJSON_GenericFallback(t *testing.T) {
	// Generic array
	arrJSON := `[
		{"title": "Políticas", "desc": "Detalle normativo 1", "num": 12345},
		{"title": "Garantías", "desc": "Detalle normativo 2", "num": 67890}
	]`
	chunker := NewSemanticChunker()
	chunks, err := chunker.ChunkJSON("gen_doc", "policy", []byte(arrJSON))
	if err != nil {
		t.Fatalf("unexpected error chunking generic json: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Category != "policy" {
		t.Errorf("expected category policy, got %s", chunks[0].Category)
	}

	// Generic object with sections
	objJSON := `{
		"seccion_a": {"regla": "cumplimiento", "valor": 98765},
		"seccion_b": {"regla": "seguridad", "valor": 54321}
	}`
	objChunks, err := chunker.ChunkJSON("obj_doc", "safety", []byte(objJSON))
	if err != nil {
		t.Fatalf("unexpected error chunking object json: %v", err)
	}
	if len(objChunks) != 2 {
		t.Fatalf("expected 2 chunks for object json, got %d", len(objChunks))
	}
}

func TestMetadataAndTagsExtraction(t *testing.T) {
	title := "Garantía de Motor y Transmisión en Tunja"
	content := "Detalles sobre la cobertura de motor y caja de cambios en Boyacá con talleres autorizados en Duitama."
	category := "warranty"

	tags := extractDeterministicTags(title, content, category)
	if len(tags) == 0 {
		t.Fatalf("expected non-empty tags")
	}

	// Verify sorted and deduplicated
	for i := 1; i < len(tags); i++ {
		if tags[i] <= tags[i-1] {
			t.Errorf("tags are not strictly ascending: %s <= %s", tags[i], tags[i-1])
		}
	}

	expectedKeywords := []string{"warranty", "garantia", "motor", "caja", "tunja", "duitama", "boyaca"}
	for _, ek := range expectedKeywords {
		found := false
		for _, tag := range tags {
			if tag == ek {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected tag %s in extracted tags: %v", ek, tags)
		}
	}
}

func TestBackedNumbersExtraction(t *testing.T) {
	text := "Vehículo con precio de $86.000.000 COP (86000000), 28.000 km (28000), teléfono de contacto 3151212223 y código 1234 (menos de 5 dígitos)."
	numbers := extractFiveDigitNumbers(text)

	expected := map[string]bool{
		"86000000":   false,
		"28000":      false,
		"3151212223": false,
	}

	for _, n := range numbers {
		if _, ok := expected[n]; ok {
			expected[n] = true
		}
		if n == "1234" {
			t.Errorf("1234 should not be extracted as it has < 5 digits")
		}
	}

	for k, found := range expected {
		if !found {
			t.Errorf("expected number %s was not extracted", k)
		}
	}
}

func TestFixturesLoader_Embedded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	engine := NewEngine()
	chunks, err := LoadEmbeddedFixtures(ctx, engine)
	if err != nil {
		t.Fatalf("failed to load embedded fixtures: %v", err)
	}

	if len(chunks) == 0 {
		t.Fatalf("expected loaded chunks, got 0")
	}

	// We have vehicles.json (5 chunks), vehicles.md (~6 chunks), warranty_policy.md (~4 chunks),
	// credit_requirements.md (~4 chunks), faq_boyaca.md (~5 chunks) -> Total > 20 chunks
	t.Logf("Total chunks loaded: %d", len(chunks))
	if len(chunks) < 15 {
		t.Errorf("expected at least 15 chunks from embedded fixtures, got %d", len(chunks))
	}

	if engine.ChunkCount() != len(chunks) {
		t.Errorf("engine chunk count mismatch: engine=%d, chunks=%d", engine.ChunkCount(), len(chunks))
	}

	backed := engine.GetBackedNumbers()
	if len(backed) < 5 {
		t.Errorf("expected at least 5 backed numbers in engine, got %d (%v)", len(backed), backed)
	}

	// Verify key numbers present in engine
	keyNumbers := []string{"72000000", "54000000", "48500000", "86000000", "105000000", "20000", "3151212223"}
	for _, kn := range keyNumbers {
		found := false
		for _, b := range backed {
			if b == kn {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected key backed number %s in engine, not found in %v", kn, backed)
		}
	}
}

func TestFixturesLoader_Disk(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	engine := NewEngine()
	chunks, err := LoadFixturesFromDisk(ctx, engine, "")
	if err != nil {
		t.Fatalf("failed to load fixtures from disk: %v", err)
	}

	if len(chunks) < 15 {
		t.Errorf("expected at least 15 chunks from disk fixtures, got %d", len(chunks))
	}
}

func TestEndToEnd_FixturesRetrieval(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	engine := NewEngine()
	_, err := LoadEmbeddedFixtures(ctx, engine)
	if err != nil {
		t.Fatalf("failed to load fixtures: %v", err)
	}

	tests := []struct {
		name             string
		query            Query
		expectedContains string
		expectedCategory string
	}{
		{
			name: "Retrieve Duster in catalog",
			query: Query{
				Text:      "Renault Duster camioneta 2021",
				Category:  "catalog",
				TopK:      3,
				MaxTokens: 500,
			},
			expectedContains: "Duster",
			expectedCategory: "catalog",
		},
		{
			name: "Retrieve Mazda CX-30 in catalog",
			query: Query{
				Text:      "Mazda CX-30 Grand Touring",
				Category:  "catalog",
				TopK:      3,
				MaxTokens: 500,
			},
			expectedContains: "CX-30",
			expectedCategory: "catalog",
		},
		{
			name: "Retrieve Warranty Policy",
			query: Query{
				Text:      "garantía mecánica cobertura motor y caja de cambios Boyacá",
				Category:  "warranty",
				TopK:      3,
				MaxTokens: 500,
			},
			expectedContains: "Garantía",
			expectedCategory: "warranty",
		},
		{
			name: "Retrieve Credit Requirements",
			query: Query{
				Text:      "requisitos crédito empleados independientes cuota inicial Bancolombia",
				Category:  "credit",
				TopK:      3,
				MaxTokens: 500,
			},
			expectedContains: "Crédito",
			expectedCategory: "credit",
		},
		{
			name: "Retrieve FAQ Tunja",
			query: Query{
				Text:      "horarios de atención y dirección vitrina sede Tunja Unicentro",
				Category:  "faq",
				TopK:      3,
				MaxTokens: 500,
			},
			expectedContains: "Tunja",
			expectedCategory: "faq",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := engine.Retrieve(ctx, tt.query)
			if err != nil {
				t.Fatalf("Retrieve failed: %v", err)
			}
			if len(results) == 0 {
				t.Fatalf("expected at least 1 retrieved chunk, got 0")
			}

			// Verify category filter strictly respected
			for _, res := range results {
				if tt.query.Category != "" && res.Chunk.Category != tt.query.Category {
					t.Errorf("chunk category mismatch: expected %s, got %s", tt.query.Category, res.Chunk.Category)
				}
			}

			// Verify top result relevance
			top := results[0]
			fullText := top.Chunk.Title + " " + top.Chunk.Content
			if !strings.Contains(fullText, tt.expectedContains) {
				t.Errorf("top chunk does not contain '%s': title='%s'", tt.expectedContains, top.Chunk.Title)
			}
		})
	}
}

func TestRetrieval_TokenBudgetConstraint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	engine := NewEngine()
	_, err := LoadEmbeddedFixtures(ctx, engine)
	if err != nil {
		t.Fatalf("failed to load fixtures: %v", err)
	}

	// Query with large budget
	qLarge := Query{
		Text:      "vehículo camioneta garantía crédito Boyacá",
		TopK:      5,
		MaxTokens: 1000,
	}
	resLarge, err := engine.Retrieve(ctx, qLarge)
	if err != nil {
		t.Fatalf("query large failed: %v", err)
	}

	// Query with small budget
	qSmall := Query{
		Text:      "vehículo camioneta garantía crédito Boyacá",
		TopK:      5,
		MaxTokens: 40,
	}
	resSmall, err := engine.Retrieve(ctx, qSmall)
	if err != nil {
		t.Fatalf("query small failed: %v", err)
	}

	tokensLarge := 0
	for _, r := range resLarge {
		tokensLarge += r.Chunk.Tokens
	}

	tokensSmall := 0
	for _, r := range resSmall {
		tokensSmall += r.Chunk.Tokens
	}

	t.Logf("Tokens large budget: %d (chunks: %d), Tokens small budget: %d (chunks: %d)",
		tokensLarge, len(resLarge), tokensSmall, len(resSmall))

	if len(resSmall) > len(resLarge) {
		t.Errorf("small budget returned more chunks than large budget: %d vs %d", len(resSmall), len(resLarge))
	}
}

func TestChunker_DeterministicReproducibility(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	engineA := NewEngine()
	chunksA, err := LoadEmbeddedFixtures(ctx, engineA)
	if err != nil {
		t.Fatalf("run A failed: %v", err)
	}

	engineB := NewEngine()
	chunksB, err := LoadEmbeddedFixtures(ctx, engineB)
	if err != nil {
		t.Fatalf("run B failed: %v", err)
	}

	if len(chunksA) != len(chunksB) {
		t.Fatalf("chunk count differs between runs: %d vs %d", len(chunksA), len(chunksB))
	}

	for i := range chunksA {
		cA := chunksA[i]
		cB := chunksB[i]

		if cA.ID != cB.ID {
			t.Errorf("chunk %d ID mismatch: %s vs %s", i, cA.ID, cB.ID)
		}
		if cA.Title != cB.Title {
			t.Errorf("chunk %d Title mismatch: %s vs %s", i, cA.Title, cB.Title)
		}
		if cA.Tokens != cB.Tokens {
			t.Errorf("chunk %d Tokens mismatch: %d vs %d", i, cA.Tokens, cB.Tokens)
		}
		if cA.Content != cB.Content {
			t.Errorf("chunk %d Content mismatch", i)
		}
		if len(cA.BackedNumbers) != len(cB.BackedNumbers) {
			t.Errorf("chunk %d BackedNumbers length mismatch: %d vs %d", i, len(cA.BackedNumbers), len(cB.BackedNumbers))
		}
	}
}

func TestChunker_ConcurrencyAndRace(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	engine := NewEngine()
	_, err := LoadEmbeddedFixtures(ctx, engine)
	if err != nil {
		t.Fatalf("failed to seed engine: %v", err)
	}

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			q := Query{
				Text:      "Duster garantía cuota crédito Tunja",
				TopK:      3,
				MaxTokens: 200,
			}
			res, err := engine.Retrieve(ctx, q)
			if err != nil {
				t.Errorf("concurrent retrieve error in worker %d: %v", id, err)
			}
			if len(res) == 0 {
				t.Errorf("concurrent retrieve returned 0 results in worker %d", id)
			}

			// Index additional chunk concurrently
			newChunk := Chunk{
				ID:            fmt.Sprintf("concur_chunk_%d", id),
				DocID:         "concur_doc",
				Category:      "faq",
				Title:         fmt.Sprintf("Concurrent Test Chunk %d", id),
				Content:       fmt.Sprintf("Contenido concurrente de prueba número %d con cifra 99999%d", id, id),
				Tokens:        15,
				BackedNumbers: []string{fmt.Sprintf("99999%d", id)},
			}
			if err := engine.IndexChunk(ctx, newChunk); err != nil {
				t.Errorf("concurrent index error in worker %d: %v", id, err)
			}
		}(g)
	}

	wg.Wait()
}
