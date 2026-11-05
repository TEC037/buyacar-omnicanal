package rag

import (
	"context"
	"io/fs"
	"sort"
	"strings"
	"testing"
	"time"

	ragfixtures "buyacar.com.co/omnicanal/fixtures/rag"
)

type FixtureMetrics struct {
	Filename    string
	DocID       string
	Category    string
	ByteSize    int
	ChunkCount  int
	TotalTokens int
	AvgTokens   float64
	Duration    time.Duration
}

func TestMilestone2_Evaluation(t *testing.T) {
	ctx := context.Background()
	chunker := NewSemanticChunker()

	entries, err := fs.ReadDir(ragfixtures.Files, ".")
	if err != nil {
		t.Fatalf("failed to read embedded fixtures: %v", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	var metrics []FixtureMetrics
	totalBytes := 0
	totalChunks := 0
	totalTokens := 0
	var totalChunkingTime time.Duration

	engine := NewEngine()

	t.Log("=== 1. CHUNK & TOKEN METRICS PER FIXTURE ===")
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".md") {
			continue
		}

		data, err := fs.ReadFile(ragfixtures.Files, name)
		if err != nil {
			t.Fatalf("failed to read fixture %s: %v", name, err)
		}

		docID, cat := InferCategoryAndDocID(name)
		start := time.Now()
		chunks, err := chunker.ChunkDocument(docID, cat, name, data)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("chunking failed for %s: %v", name, err)
		}

		if err := engine.IndexChunks(ctx, chunks); err != nil {
			t.Fatalf("indexing failed for %s: %v", name, err)
		}

		docTokens := 0
		for _, c := range chunks {
			docTokens += c.Tokens
		}
		avgTok := 0.0
		if len(chunks) > 0 {
			avgTok = float64(docTokens) / float64(len(chunks))
		}

		fm := FixtureMetrics{
			Filename:    name,
			DocID:       docID,
			Category:    cat,
			ByteSize:    len(data),
			ChunkCount:  len(chunks),
			TotalTokens: docTokens,
			AvgTokens:   avgTok,
			Duration:    elapsed,
		}
		metrics = append(metrics, fm)

		totalBytes += len(data)
		totalChunks += len(chunks)
		totalTokens += docTokens
		totalChunkingTime += elapsed

		t.Logf("FIXTURE_EVAL: file=%-22s doc_id=%-15s cat=%-8s chunks=%2d total_tokens=%4d avg_tokens=%5.1f size=%5d B latency=%v",
			fm.Filename, fm.DocID, fm.Category, fm.ChunkCount, fm.TotalTokens, fm.AvgTokens, fm.ByteSize, fm.Duration)
	}

	overallAvgTokens := 0.0
	if totalChunks > 0 {
		overallAvgTokens = float64(totalTokens) / float64(totalChunks)
	}
	t.Logf("FIXTURE_SUMMARY: total_files=%d total_bytes=%d total_chunks=%d total_tokens=%d overall_avg_tokens=%.1f",
		len(metrics), totalBytes, totalChunks, totalTokens, overallAvgTokens)

	// 2. LATENCY & THROUGHPUT (averaged across 500 repeated passes)
	const benchRounds = 500
	benchStart := time.Now()
	for r := 0; r < benchRounds; r++ {
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".md") {
				continue
			}
			data, _ := fs.ReadFile(ragfixtures.Files, name)
			docID, cat := InferCategoryAndDocID(name)
			_, _ = chunker.ChunkDocument(docID, cat, name, data)
		}
	}
	benchElapsed := time.Since(benchStart)
	avgPassDuration := benchElapsed / benchRounds
	throughputMBs := (float64(totalBytes*benchRounds) / (1024.0 * 1024.0)) / benchElapsed.Seconds()
	chunksPerSec := float64(totalChunks*benchRounds) / benchElapsed.Seconds()

	t.Log("=== 2. CHUNKING LATENCY & THROUGHPUT ===")
	t.Logf("CHUNKING_PERF: pass_latency=%v throughput=%.2f MB/s rate=%.0f chunks/sec total_corpus_size=%d B",
		avgPassDuration, throughputMBs, chunksPerSec, totalBytes)

	// 3. RETRIEVAL HIT RATE ON REAL FIXTURE QUERIES
	goldenQueries := []struct {
		name        string
		query       Query
		targetDocID string
		targetCat   string
		mustContain string
	}{
		{
			name:        "Credit: Requisitos de crédito Tunja",
			query:       Query{Text: "requisitos credito vehiculo Boyaca Boyaca Tunja", TopK: 3},
			targetCat:   "credit",
			mustContain: "crédito",
		},
		{
			name:        "Credit: Empleados nómina e independientes",
			query:       Query{Text: "desprendibles de nomina extractos bancarios independientes", TopK: 3},
			targetCat:   "credit",
			mustContain: "empleados",
		},
		{
			name:        "Credit: Cuota inicial mínima",
			query:       Query{Text: "cuota inicial minima diez por ciento financiamiento", TopK: 3},
			targetCat:   "credit",
			mustContain: "10%",
		},
		{
			name:        "Credit: Bancos aliados Tunja",
			query:       Query{Text: "bancos aliados Bancolombia Davivienda Finandina", TopK: 3},
			targetCat:   "credit",
			mustContain: "Bancolombia",
		},
		{
			name:        "Warranty: Cobertura mecánica 12 meses motor caja",
			query:       Query{Text: "garantia mecanica motor caja de cambios 12 meses 20000 km", TopK: 3},
			targetCat:   "warranty",
			mustContain: "garantía",
		},
		{
			name:        "Warranty: Exclusiones piezas de desgaste frenos embrague",
			query:       Query{Text: "exclusiones garantia pastillas embrague llantas desgaste", TopK: 3},
			targetCat:   "warranty",
			mustContain: "exclusiones",
		},
		{
			name:        "Warranty: Talleres autorizados Tunja Duitama",
			query:       Query{Text: "talleres autorizados garantia Tunja Duitama direccion", TopK: 3},
			targetCat:   "warranty",
			mustContain: "Tunja",
		},
		{
			name:        "Warranty: Proceso radicación 48 horas",
			query:       Query{Text: "proceso radicacion novedad mecanica cita pericial 48 horas", TopK: 3},
			targetCat:   "warranty",
			mustContain: "48 horas",
		},
		{
			name:        "Vehicle: Renault Duster Zen 2021",
			query:       Query{Text: "camioneta Renault Duster Zen 2021 1.6 72000000 Tunja", TopK: 3},
			targetCat:   "catalog",
			mustContain: "Duster",
		},
		{
			name:        "Vehicle: Renault Sandero Stepway Intens 2020",
			query:       Query{Text: "Renault Sandero Stepway Intens 2020 54000000 naranja ocre", TopK: 3},
			targetCat:   "catalog",
			mustContain: "Sandero",
		},
		{
			name:        "Vehicle: Renault Logan Life 2022",
			query:       Query{Text: "Renault Logan Life 2022 baul 510 litros blanco", TopK: 3},
			targetCat:   "catalog",
			mustContain: "Logan",
		},
		{
			name:        "Vehicle: Chevrolet Tracker Premier Turbo 2022",
			query:       Query{Text: "Chevrolet Tracker Premier Turbo 2022 azul lunar techo panoramico", TopK: 3},
			targetCat:   "catalog",
			mustContain: "Tracker",
		},
		{
			name:        "Vehicle: Mazda CX-30 Grand Touring 2023",
			query:       Query{Text: "Mazda CX-30 Grand Touring 2023 Skyactiv rojo diamante Tunja", TopK: 3},
			targetCat:   "catalog",
			mustContain: "CX-30",
		},
		{
			name:        "FAQ: Horarios y ubicación vitrina Unicentro",
			query:       Query{Text: "horarios de atencion vitrina sede principal Unicentro Tunja", TopK: 3},
			targetCat:   "faq",
			mustContain: "Unicentro",
		},
		{
			name:        "FAQ: Peritaje 150 puntos RUNT SIMIT",
			query:       Query{Text: "peritaje tecnico certificado 150 puntos revision RUNT SIMIT", TopK: 3},
			targetCat:   "faq",
			mustContain: "150 puntos",
		},
		{
			name:        "FAQ: Retoma de usado parte de pago",
			query:       Query{Text: "reciben vehiculo usado retoma parte de pago avaluo", TopK: 3},
			targetCat:   "faq",
			mustContain: "retoma",
		},
		{
			name:        "FAQ: Traspaso secretarias Boyaca",
			query:       Query{Text: "tramite de traspaso de propiedad transito Tunja Duitama Sogamoso", TopK: 3},
			targetCat:   "faq",
			mustContain: "traspaso",
		},
	}

	t.Log("=== 3. RETRIEVAL HIT RATE ON REAL FIXTURE QUERIES ===")
	top1Hits := 0
	top3Hits := 0

	for _, gq := range goldenQueries {
		res, err := engine.Retrieve(ctx, gq.query)
		if err != nil {
			t.Errorf("query %s failed: %v", gq.name, err)
			continue
		}
		if len(res) == 0 {
			t.Errorf("query %s returned zero results", gq.name)
			continue
		}

		matchedTop1 := false
		matchedTop3 := false

		for rank, r := range res {
			full := strings.ToLower(r.Chunk.Title + " " + r.Chunk.Content)
			catMatch := (gq.targetCat == "" || r.Chunk.Category == gq.targetCat)
			contentMatch := (gq.mustContain == "" || strings.Contains(full, strings.ToLower(gq.mustContain)))

			if catMatch && contentMatch {
				if rank == 0 {
					matchedTop1 = true
				}
				matchedTop3 = true
				break
			}
		}

		if matchedTop1 {
			top1Hits++
		}
		if matchedTop3 {
			top3Hits++
			t.Logf("HIT: query=%-35s top1_title=%-35s score=%.4f rank_ok=true",
				gq.name, res[0].Chunk.Title, res[0].Score)
		} else {
			t.Logf("MISS: query=%-35s top1_title=%-35s score=%.4f expected=%s/%s",
				gq.name, res[0].Chunk.Title, res[0].Score, gq.targetCat, gq.mustContain)
		}
	}

	totalQueries := len(goldenQueries)
	top1Rate := (float64(top1Hits) / float64(totalQueries)) * 100.0
	top3Rate := (float64(top3Hits) / float64(totalQueries)) * 100.0

	t.Logf("HIT_RATE_SUMMARY: queries=%d top1_hits=%d (%.1f%%) top3_hits=%d (%.1f%%)",
		totalQueries, top1Hits, top1Rate, top3Hits, top3Rate)

	if top3Rate < 90.0 {
		t.Fatalf("expected top3 hit rate >= 90%%, got %.1f%%", top3Rate)
	}
}

func BenchmarkMilestone2_Chunking(b *testing.B) {
	chunker := NewSemanticChunker()
	entries, err := fs.ReadDir(ragfixtures.Files, ".")
	if err != nil {
		b.Fatalf("failed to read embedded fixtures: %v", err)
	}

	type fixtureItem struct {
		docID    string
		category string
		name     string
		data     []byte
	}
	var items []fixtureItem
	totalBytes := int64(0)

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".md") {
			continue
		}
		data, err := fs.ReadFile(ragfixtures.Files, name)
		if err != nil {
			b.Fatalf("failed to read %s: %v", name, err)
		}
		docID, cat := InferCategoryAndDocID(name)
		items = append(items, fixtureItem{
			docID:    docID,
			category: cat,
			name:     name,
			data:     data,
		})
		totalBytes += int64(len(data))
	}

	b.SetBytes(totalBytes)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, it := range items {
			_, _ = chunker.ChunkDocument(it.docID, it.category, it.name, it.data)
		}
	}
}
