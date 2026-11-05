package rag

import (
	"context"
)

// Chunk represents a segmented document fragment with metadata and embeddings.
type Chunk struct {
	ID            string            `json:"id"`
	DocID         string            `json:"doc_id"`
	Category      string            `json:"category"` // "catalog", "warranty", "credit", "faq"
	Title         string            `json:"title"`
	Content       string            `json:"content"`
	Tokens        int               `json:"tokens"`
	BackedNumbers []string          `json:"backed_numbers"`
	Metadata      map[string]string `json:"metadata"`
	Vector        []float32         `json:"vector,omitempty"`
}

// Query defines search criteria including optional category filter and budget constraints.
type Query struct {
	Text      string `json:"text"`
	Category  string `json:"category"`
	TopK      int    `json:"top_k"`
	MaxTokens int    `json:"max_tokens"`
}

// ScoredChunk is a retrieved chunk with its computed relevance score.
type ScoredChunk struct {
	Chunk Chunk   `json:"chunk"`
	Score float64 `json:"score"`
}

// Engine defines the contract for local document indexing and hybrid retrieval.
type Engine interface {
	IndexDocument(ctx context.Context, docID, category, title, text string) ([]Chunk, error)
	Retrieve(ctx context.Context, q Query) ([]ScoredChunk, error)
	GetBackedNumbers() []string
}

// Embedder generates vector embeddings for text.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Dimension() int
}
