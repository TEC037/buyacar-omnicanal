package skills

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"buyacar.com.co/omnicanal/internal/inventory"
	"buyacar.com.co/omnicanal/internal/rag"
	"buyacar.com.co/omnicanal/internal/triage"
)

const (
	MaxInputTokensBudget  = 1500
	MaxOutputTokensBudget = 300
	MaxVehiclesInContext  = 3
	DefaultMaxRAGTokens   = 150
)

// CompactContext contiene la intencion, 1 a 3 vehiculos relevantes, historial resumido y fragmentos RAG opcionales.
type CompactContext struct {
	Intent          triage.Intent
	Summary         string
	Vehicles        []inventory.Vehicle
	RAGChunks       []rag.Chunk
	RAGSummary      string
	EstimatedTokens int
}

// BuildCompactContext prefiltra hasta 3 vehiculos disponibles y comprime el historial para ahorrar tokens (retrocompatibilidad).
func BuildCompactContext(intent triage.Intent, historySummary string, candidates []inventory.Vehicle) CompactContext {
	return BuildCompactContextWithRAG(intent, historySummary, candidates, nil, 0)
}

// BuildCompactContextWithRAG enriquece el contexto con fragmentos RAG acotados por presupuesto estricto de tokens (~1 token / 4 caracteres).
func BuildCompactContextWithRAG(
	intent triage.Intent,
	historySummary string,
	candidates []inventory.Vehicle,
	ragChunks []rag.Chunk,
	maxRAGTokens int,
) CompactContext {
	if maxRAGTokens <= 0 {
		maxRAGTokens = DefaultMaxRAGTokens
	}

	var selected []inventory.Vehicle
	for _, v := range candidates {
		if v.Status == "available" {
			selected = append(selected, v)
			if len(selected) == MaxVehiclesInContext {
				break
			}
		}
	}

	summary := strings.TrimSpace(historySummary)
	if len(summary) > 350 {
		summary = summary[:350] + "..."
	}

	// Presupuesto determinista de caracteres para RAG: ~1 token cada 4 caracteres
	maxRAGChars := maxRAGTokens * 4
	var selectedChunks []rag.Chunk
	var ragParts []string
	currentRAGChars := 0

	for _, ch := range ragChunks {
		chunkTitle := strings.TrimSpace(ch.Title)
		chunkContent := strings.TrimSpace(ch.Content)
		if chunkContent == "" {
			continue
		}
		var entry string
		if chunkTitle != "" {
			entry = fmt.Sprintf("[%s] %s", chunkTitle, chunkContent)
		} else {
			entry = chunkContent
		}

		entryLen := len(entry)
		if currentRAGChars+entryLen <= maxRAGChars {
			selectedChunks = append(selectedChunks, ch)
			ragParts = append(ragParts, entry)
			currentRAGChars += entryLen + 1
		} else {
			remaining := maxRAGChars - currentRAGChars
			if remaining > 30 {
				truncated := entry[:remaining-3] + "..."
				chTruncated := ch
				chTruncated.Content = truncated
				selectedChunks = append(selectedChunks, chTruncated)
				ragParts = append(ragParts, truncated)
			}
			break
		}
	}

	ragSummary := strings.Join(ragParts, "\n")

	var b strings.Builder
	b.WriteString(fmt.Sprintf("intent:%s|summary:%s|", intent, summary))
	for _, v := range selected {
		b.WriteString(fmt.Sprintf("%s %s %d $%d %dkm %s|", v.Brand, v.Model, v.Year, v.PriceCOP, v.MileageKm, v.OfficialURL))
	}
	if ragSummary != "" {
		b.WriteString("rag:" + ragSummary + "|")
	}

	// Estimacion conservadora: ~1 token cada 4 caracteres
	estTokens := (len(b.String()) / 4) + 40

	return CompactContext{
		Intent:          intent,
		Summary:         summary,
		Vehicles:        selected,
		RAGChunks:       selectedChunks,
		RAGSummary:      ragSummary,
		EstimatedTokens: estTokens,
	}
}

// ApprovedCache almacena respuestas frecuentes validadas por humanos para evitar llamadas a IA.
type ApprovedCache struct {
	mu          sync.RWMutex
	entries     map[string]string
	hits        int64
	misses      int64
	tokensSaved int64
	tokenStatus string
}

func NewApprovedCache() *ApprovedCache {
	return &ApprovedCache{
		entries:     make(map[string]string),
		tokenStatus: "not_measured",
	}
}

func (c *ApprovedCache) Set(key, approvedReply string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[strings.ToLower(strings.TrimSpace(key))] = approvedReply
}

func (c *ApprovedCache) Get(key string) (string, bool) {
	c.mu.RLock()
	val, ok := c.entries[strings.ToLower(strings.TrimSpace(key))]
	c.mu.RUnlock()
	if ok {
		atomic.AddInt64(&c.hits, 1)
		return val, true
	}
	atomic.AddInt64(&c.misses, 1)
	return "", false
}

// Hits devuelve el conteo atomico de aciertos.
func (c *ApprovedCache) Hits() int64 {
	return atomic.LoadInt64(&c.hits)
}

// Misses devuelve el conteo atomico de fallos.
func (c *ApprovedCache) Misses() int64 {
	return atomic.LoadInt64(&c.misses)
}

// TokensSaved devuelve el conteo acumulado de tokens ahorrados por aciertos de cache.
func (c *ApprovedCache) TokensSaved() int64 {
	return atomic.LoadInt64(&c.tokensSaved)
}

// TokenStatus reporta si la medicion de tokens es real ("measured") o no medida ("not_measured").
func (c *ApprovedCache) TokenStatus() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.tokenStatus == "" {
		return "not_measured"
	}
	return c.tokenStatus
}

// RecordTokensSaved registra medicion real de tokens ahorrados por servir desde cache.
func (c *ApprovedCache) RecordTokensSaved(tokens int) {
	if tokens > 0 {
		atomic.AddInt64(&c.tokensSaved, int64(tokens))
		c.mu.Lock()
		c.tokenStatus = "measured"
		c.mu.Unlock()
	}
}

// Stats mantiene compatibilidad con la firma anterior (hits, misses int).
func (c *ApprovedCache) Stats() (hits, misses int) {
	return int(atomic.LoadInt64(&c.hits)), int(atomic.LoadInt64(&c.misses))
}
