package rag

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var reFiveOrMoreDigits = regexp.MustCompile(`\b\d{5,}\b`)

// InMemoryEngine implements thread-safe local RAG engine.
type InMemoryEngine struct {
	mu            sync.RWMutex
	bm25          *BM25Index
	embedder      Embedder
	ranker        *HybridRanker
	chunks        map[string]Chunk
	docChunks     map[string][]string // docID -> []chunkID
	backedNumbers map[string]struct{}
}

// EngineOption configures InMemoryEngine.
type EngineOption func(*InMemoryEngine)

// WithEngineEmbedder sets the embedder used for vectorization.
func WithEngineEmbedder(emb Embedder) EngineOption {
	return func(e *InMemoryEngine) {
		if emb != nil {
			e.embedder = emb
		}
	}
}

// WithEngineRanker sets the hybrid ranker.
func WithEngineRanker(r *HybridRanker) EngineOption {
	return func(e *InMemoryEngine) {
		if r != nil {
			e.ranker = r
		}
	}
}

// NewEngine creates a new in-memory RAG engine.
func NewEngine(opts ...EngineOption) *InMemoryEngine {
	e := &InMemoryEngine{
		bm25:          NewBM25Index(),
		embedder:      NewFallbackEmbedder(DefaultVectorDim),
		ranker:        NewHybridRanker(WithFusionStrategy(FusionWeighted), WithWeights(0.80, 0.20)),
		chunks:        make(map[string]Chunk),
		docChunks:     make(map[string][]string),
		backedNumbers: make(map[string]struct{}),
	}

	for _, opt := range opts {
		opt(e)
	}
	return e
}

// estimateTokens returns an approximate token count for Spanish/English text.
func estimateTokens(text string) int {
	words := len(strings.Fields(text))
	if words == 0 {
		return 0
	}
	// Typically 1 word ≈ 1.3 tokens in multilingual/Spanish models
	return int(math.Ceil(float64(words) * 1.3))
}

// extractFiveDigitNumbers finds all distinct 5+ digit numbers in text.
func extractFiveDigitNumbers(text string) []string {
	matches := reFiveOrMoreDigits.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	var res []string
	for _, m := range matches {
		if _, exists := seen[m]; !exists {
			seen[m] = struct{}{}
			res = append(res, m)
		}
	}
	sort.Strings(res)
	return res
}

// splitIntoParagraphChunks partitions text into coherent semantic paragraphs.
func splitIntoParagraphChunks(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	rawParas := strings.Split(text, "\n\n")
	var chunks []string
	var current strings.Builder

	for _, p := range rawParas {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		// If current paragraph is markdown header alone, attach to next paragraph
		if strings.HasPrefix(p, "#") && !strings.Contains(p, "\n") {
			if current.Len() > 0 {
				chunks = append(chunks, strings.TrimSpace(current.String()))
				current.Reset()
			}
			current.WriteString(p)
			current.WriteString("\n")
			continue
		}

		if current.Len() > 0 {
			current.WriteString("\n")
			current.WriteString(p)
			// Target chunk size ~120 words
			if len(strings.Fields(current.String())) >= 100 {
				chunks = append(chunks, strings.TrimSpace(current.String()))
				current.Reset()
			}
		} else {
			if len(strings.Fields(p)) >= 100 {
				chunks = append(chunks, p)
			} else {
				current.WriteString(p)
			}
		}
	}

	if current.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(current.String()))
	}

	return chunks
}

// IndexDocument segments, embeds, and indexes a document in BM25 and vector stores.
func (e *InMemoryEngine) IndexDocument(ctx context.Context, docID, category, title, text string) ([]Chunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	paragraphs := splitIntoParagraphChunks(text)
	if len(paragraphs) == 0 {
		paragraphs = []string{text}
	}

	chunks := make([]Chunk, len(paragraphs))
	for i, para := range paragraphs {
		chunkID := fmt.Sprintf("%s_c%d", docID, i)
		tokens := estimateTokens(para)
		backed := extractFiveDigitNumbers(para + " " + title)

		// Vector embedding with context
		vec, err := e.embedder.Embed(ctx, title+"\n"+para)
		if err != nil {
			return nil, fmt.Errorf("failed to embed chunk %s: %w", chunkID, err)
		}

		chunk := Chunk{
			ID:            chunkID,
			DocID:         docID,
			Category:      category,
			Title:         title,
			Content:       para,
			Tokens:        tokens,
			BackedNumbers: backed,
			Metadata: map[string]string{
				"doc_id":   docID,
				"category": category,
				"title":    title,
				"index":    fmt.Sprintf("%d", i),
			},
			Vector: vec,
		}
		chunks[i] = chunk
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Clear any previous chunks for this docID
	if prevChunkIDs, exists := e.docChunks[docID]; exists {
		for _, cid := range prevChunkIDs {
			delete(e.chunks, cid)
		}
	}

	chunkIDs := make([]string, len(chunks))
	for i, chunk := range chunks {
		e.chunks[chunk.ID] = chunk
		chunkIDs[i] = chunk.ID

		// Add to BM25 index (with 3x title weight for heading relevance)
		e.bm25.AddDocument(chunk.ID, chunk.Title+" "+chunk.Title+" "+chunk.Title+" "+chunk.Content)

		// Add backed numbers to global set
		for _, num := range chunk.BackedNumbers {
			e.backedNumbers[num] = struct{}{}
		}
	}
	e.docChunks[docID] = chunkIDs

	return chunks, nil
}

// Retrieve performs hybrid retrieval with BM25 + Vector fusion, category filter and token budget.
func (e *InMemoryEngine) Retrieve(ctx context.Context, q Query) ([]ScoredChunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	qText := strings.TrimSpace(q.Text)
	if qText == "" {
		return nil, nil
	}

	topK := q.TopK
	if topK <= 0 {
		topK = 3
	}

	// Embed query
	qVec, err := e.embedder.Embed(ctx, qText)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	if len(e.chunks) == 0 {
		return nil, nil
	}

	// 1. BM25 scores
	rawBM25 := e.bm25.Score(qText)
	bm25Map := make(map[string]float64, len(rawBM25))
	for _, s := range rawBM25 {
		bm25Map[s.ID] = s.Score
	}

	queriedModels := extractQueriedCatalogModels(qText)

	// 2. Vector scores & category filtering
	bm25Scores := make([]DocScore, 0, len(e.chunks))
	vectorScores := make([]DocScore, 0, len(e.chunks))

	for id, chunk := range e.chunks {
		// Category filter
		if q.Category != "" && chunk.Category != q.Category {
			continue
		}

		// Catalog brand/model guard: never substitute a different vehicle when a specific brand/model is queried
		if chunk.Category == "catalog" && len(queriedModels) > 0 {
			if !chunkMatchesCatalogModels(chunk, queriedModels) {
				continue
			}
		}

		bm25Score, hasBM25 := bm25Map[id]
		if !hasBM25 || bm25Score <= 0 {
			continue
		}
		bm25Scores = append(bm25Scores, DocScore{ID: id, Score: bm25Score})

		if len(chunk.Vector) > 0 && len(qVec) > 0 {
			sim := CosineSimilarity(qVec, chunk.Vector)
			if sim > 0 {
				vectorScores = append(vectorScores, DocScore{ID: id, Score: sim})
			}
		}
	}

	// 3. Hybrid fusion
	fused := e.ranker.Fuse(bm25Scores, vectorScores)
	if len(fused) == 0 {
		return nil, nil
	}

	// 4. Assemble ScoredChunks respecting TopK and MaxTokens
	results := make([]ScoredChunk, 0, topK)
	totalTokens := 0

	for _, item := range fused {
		chunk, exists := e.chunks[item.ID]
		if !exists {
			continue
		}

		// Token budget check
		if q.MaxTokens > 0 && len(results) > 0 && totalTokens+chunk.Tokens > q.MaxTokens {
			// Budget exhausted, stop adding
			break
		}

		results = append(results, ScoredChunk{
			Chunk: chunk,
			Score: item.Score,
		})
		totalTokens += chunk.Tokens

		if len(results) >= topK {
			break
		}
	}

	return results, nil
}

// GetBackedNumbers returns a deduplicated, sorted list of all 5+ digit numbers in indexed documents.
func (e *InMemoryEngine) GetBackedNumbers() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	out := make([]string, 0, len(e.backedNumbers))
	for num := range e.backedNumbers {
		out = append(out, num)
	}
	sort.Strings(out)
	return out
}

// ChunkCount returns current number of indexed chunks.
func (e *InMemoryEngine) ChunkCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.chunks)
}

// DocumentCount returns current number of indexed documents.
func (e *InMemoryEngine) DocumentCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.docChunks)
}

var knownCatalogTokens = map[string]struct{}{
	"toyota": {}, "hilux": {}, "fortuner": {}, "mazda": {}, "cx5": {}, "cx30": {}, "mazda3": {},
	"renault": {}, "duster": {}, "logan": {}, "stepway": {}, "sandero": {}, "captur": {},
	"chevrolet": {}, "tracker": {}, "captiva": {}, "spark": {}, "sonic": {},
	"nissan": {}, "kicks": {}, "xtrail": {}, "qashqai": {},
	"volkswagen": {}, "tiguan": {}, "kia": {}, "picanto": {}, "sportage": {},
	"ford": {}, "escape": {}, "edge": {}, "focus": {}, "fiesta": {},
	"hyundai": {}, "accent": {}, "santafe": {}, "honda": {}, "crv": {},
	"mercedes": {}, "c200": {}, "b180": {}, "audi": {}, "a3": {}, "peugeot": {}, "207": {},
	"ferrari": {}, "testarossa": {},
}

func extractQueriedCatalogModels(qText string) []string {
	tokens := Tokenize(qText)
	var matched []string
	for _, tok := range tokens {
		if _, ok := knownCatalogTokens[tok]; ok {
			matched = append(matched, tok)
		}
	}
	return matched
}

func chunkMatchesCatalogModels(chunk Chunk, queried []string) bool {
	docTokens := Tokenize(chunk.Title + " " + chunk.Content)
	docSet := make(map[string]struct{}, len(docTokens))
	for _, dt := range docTokens {
		docSet[dt] = struct{}{}
	}
	for _, qTok := range queried {
		if _, ok := docSet[qTok]; ok {
			return true
		}
	}
	return false
}
