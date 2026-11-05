# Project: BuyaCAR Omnicanal — Local RAG Architecture (Go 1.23)

## Architecture
Local, free, in-memory Retrieval-Augmented Generation (RAG) architecture implemented in pure Go 1.23 standard library without external frameworks or heavy libraries.

```
┌────────────────────────────────────────────────────────────────────────┐
│                        RAG Engine (internal/rag)                       │
│                                                                        │
│  Fixtures (Markdown / JSON)                                            │
│  - Fichas Técnicas (Vehículos)                                         │
│  - Políticas de Garantía                                               │
│  - Requisitos de Crédito                                               │
│  - FAQ BuyaCAR Tunja/Boyacá                                            │
│                 │                                                      │
│                 ▼                                                      │
│  Deterministic Semantic Chunker & Metadata Extractor                   │
│                 │                                                      │
│                 ▼                                                      │
│  Hybrid In-Memory Index                                                │
│  ├── BM25 / TF-IDF Lexical Index (Pure Go)                             │
│  └── Cosine Vector Index (Pure Go) ◄──► Ollama (nomic-embed-text)      │
│                 │                                                      │
│                 ▼                                                      │
│  Hybrid Ranker (Lexical + Vector Fusion)                               │
└─────────────────┬──────────────────────────────────────────────────────┘
                  │ Top-k Chunks (Bounded by Token Budget)
                  ▼
┌────────────────────────────────────────────────────────────────────────┐
│                        CP4: Structured AI Layer                        │
│  Prompt Injection with Token Budget & Fast-Path Re-ranking             │
└─────────────────┬──────────────────────────────────────────────────────┘
                  │
                  ▼
┌────────────────────────────────────────────────────────────────────────┐
│                        CP5: Security Guardrails                        │
│  - Anti-Hallucination Filter (5+ digits validated against indexed docs)│
│  - Strict Allowlist (https://buyacar.com.co)                           │
│  - PII Redaction (Ley 1581 de 2012)                                    │
│  - SIMULATION Mode Enforcement                                         │
└────────────────────────────────────────────────────────────────────────┘
```

## Feature Inventory
| # | Feature | Description | Milestone | Source |
|---|---------|-------------|-----------|--------|
| 1 | Pure Go BM25 / TF-IDF Lexical Scorer | In-memory tokenization and BM25 scoring | M1 | ORIGINAL_REQUEST R1 |
| 2 | Pure Go Vector Cosine Similarity | Vector dot-product and cosine distance in standard Go | M1 | ORIGINAL_REQUEST R1 |
| 3 | Local Ollama Embedding Client | HTTP client for `nomic-embed-text` with automatic fallback | M1 | ORIGINAL_REQUEST R1 |
| 4 | Hybrid Ranker Fusion | Reciprocal rank / score fusion combining BM25 and Vector | M1 | ORIGINAL_REQUEST R1 |
| 5 | BuyaCAR Boyacá/Tunja Fixtures | Markdown & JSON fixtures for catalog, warranty, credit, FAQ | M2 | ORIGINAL_REQUEST R2 |
| 6 | Deterministic Semantic Chunker | Boundary-aware chunker with metadata & backed-numbers extraction | M2 | ORIGINAL_REQUEST R2 |
| 7 | In-Memory Document Indexer | Thread-safe concurrent indexing of chunks | M2 | ORIGINAL_REQUEST R2 |
| 8 | CP4 Context Injector & Token Budget | Injecting top-k chunks into CP4 within strict token caps | M3 | ORIGINAL_REQUEST R3 |
| 9 | RAG Fast-Path & Cache Integration | Skip/cache integration to preserve sub-5ms latency | M3 | ORIGINAL_REQUEST R3 |
| 10 | CP5 Anti-Hallucination Extension | Backed-number whitelist for indexed document figures | M4 | ORIGINAL_REQUEST R4 |
| 11 | PII & Allowlist Compliance | Redaction and URL filtering across retrieved fragments | M4 | ORIGINAL_REQUEST R4 |
| 12 | Programmatic Golden Test Suite | Precision and relevance evaluation for user queries | M4 | ORIGINAL_REQUEST AC |

## Milestones
| # | Name | Scope | Dependencies | Status |
|---|------|-------|-------------|--------|
| M1 | Local RAG Engine & Hybrid Retrieval | Pure Go BM25, Cosine, Ollama client, Hybrid Ranker (`internal/rag`) | none | DONE |
| M2 | Document Fixtures & Semantic Chunking | Fixtures (vehicles, warranty, credit, FAQ), Chunker & Indexer | M1 | DONE |
| M3 | CP4 Context Injection & Token Budget | CP4 prompt enrichment, token budgeting, latency preservation | M1, M2 | DONE |
| M4 | Governance, Golden Tests & E2E Validation | CP5 backed numbers, PII check, golden tests, `go test -race` | M3 | IN_PROGRESS |

## Interface Contracts

### `internal/rag`
```go
package rag

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

type Query struct {
    Text       string
    Category   string
    TopK       int
    MaxTokens  int
}

type ScoredChunk struct {
    Chunk Chunk
    Score float64
}

type Engine interface {
    IndexDocument(ctx context.Context, docID, category, title, text string) ([]Chunk, error)
    Retrieve(ctx context.Context, q Query) ([]ScoredChunk, error)
    GetBackedNumbers() []string
}
```

## Code Layout
- `app_build/internal/rag/`: RAG engine, BM25, vector similarity, chunker, indexer, Ollama embedder.
- `app_build/fixtures/rag/`: Markdown and JSON document fixtures for BuyaCAR Boyacá/Tunja.
- `app_build/internal/ai/`: CP4 prompt augmentation, token budget enforcement.
- `app_build/internal/core/` / `app_build/pipeline.go`: Pipeline integration, CP5 anti-hallucination backed numbers.
- `app_build/test/`: Golden tests and E2E verification.
