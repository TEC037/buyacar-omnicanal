## 2026-10-29T12:45:45Z
You are teamwork_worker implementing Milestone 1: Local RAG Engine & Hybrid Retrieval in pure Go 1.23 standard library.
Working directory: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\worker_rag_m1_1
Original request path: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\ORIGINAL_REQUEST.md
Scope document: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\PROJECT.md
Domain skill: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\skills\go-quality\SKILL.md
Exclusive write ownership: app_build/internal/rag/ (create package rag)

Task:
Implement the core local in-memory RAG engine in `app_build/internal/rag/` in standard Go 1.23:
1. `types.go`: Define `Chunk`, `Query`, `ScoredChunk`, `Engine` interface per `PROJECT.md § Interface Contracts`.
2. `bm25.go`: Lexical tokenizer (lowercase, Spanish punctuation strip, stop words filter), term frequency, document frequency, BM25 scoring algorithm (k1=1.2, b=0.75).
3. `vector.go`: Vector normalization, pure Go cosine similarity, dot product.
4. `ollama_embed.go`: Local Ollama embedder client for `nomic-embed-text` on `http://127.0.0.1:11434/api/embeddings` with timeout, fallback to pure in-memory lexical/TF-IDF vector when Ollama is unavailable.
5. `hybrid_ranker.go`: Hybrid ranking combining BM25 and vector cosine scores (Reciprocal Rank Fusion or normalized weighted score).
6. `engine.go`: Thread-safe in-memory `Engine` implementation with `sync.RWMutex`, indexing, querying with `TopK` and `MaxTokens`, and `GetBackedNumbers()` extractor.
7. `rag_test.go`: Unit tests and benchmarks testing BM25, Cosine, hybrid retrieval, concurrency (`-race`), and Ollama fallback.

Verification:
Execute `go test -v -count=1 -race ./internal/rag/...` inside `app_build` and ensure all tests pass cleanly.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Follow token economy: Keep reports ultra-compact. Write results to `handoff.md` in your working directory and notify with send_message.
