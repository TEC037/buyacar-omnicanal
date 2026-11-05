# Plan: BuyaCAR Omnicanal Local RAG Architecture (Go 1.23)

## Objective
Planificar, diseñar e implementar un motor RAG local y gratuito en Go 1.23 estándar para producción en BuyaCAR Omnicanal, indexando catálogo vehicular y normativas (garantía, crédito, FAQ Tunja/Boyacá) e integrándolo en CP4 (IA Estructurada) bajo control estricto de tokens y mandamientos de seguridad.

## Architecture & Constraints
1. **Zero External Heavy Dependencies / Pre-built Frameworks**: Built entirely in Go 1.23 standard library (`math`, `strings`, `sync`, `net/http`, `encoding/json`, etc.).
2. **Hybrid In-Memory Retrieval**: Pure Go BM25 lexical scorer + pure Go vector cosine similarity, with seamless local Ollama (`nomic-embed-text`) embedding fallback.
3. **Deterministic Semantic Chunking**: Chunking fixtures for vehicle catalog, warranty policies, credit requirements, and FAQ Tunja/Boyacá with metadata preservation.
4. **CP4 Integration**: Injects top-k re-ranked chunks into CP4 prompt while strictly enforcing token limits and zero degradation of p95/p99 latency.
5. **Anti-Hallucination & Governance**: Preserves SIMULATION mode, PII redaction (Ley 1581), anti-hallucination filter (blocks unbacked 5+ digit numbers, enforces `https://buyacar.com.co` allowlist).
6. **Quality**: 100% test pass uncached (`go test -count=1 ./...`), zero data races (`go test -race ./...`), and programmatic golden test cases.

## Execution Phases
- **Phase 0: Survey & Codebase Exploration** (3 parallel subagents: 2 Explorers + 1 Spec Miner)
  - Survey Explorer 1: `internal/ai` mapping (cascade, prompts, tokens, Ollama client)
  - Survey Explorer 2: `internal/core` & `pipeline.go` mapping (CP1-CP7 flow, CP4 invocation, guardrails in CP5)
  - Survey Spec Miner: Existing documentation, fixtures, `internal/inventory`, and required Boyacá/Tunja RAG docs
- **Phase 1: Synthesis & Project Specification**
  - Consolidate findings into `PROJECT.md` with Feature Inventory and Interface Contracts
- **Phase 2: Milestone Execution** (Project Pattern iteration loop: Explorer -> Worker -> Reviewers -> Challengers -> Auditor -> Gate)
  - **Milestone 1**: Motor RAG Local & Recuperación Híbrida (`internal/rag`: BM25, TF-IDF, Cosine Vector, Ollama nomic-embed-text client, Hybrid Ranker)
  - **Milestone 2**: Chunking Semántico & Fixtures Normativas/Catálogo (`fixtures/rag/`: Markdown & JSON docs, Semantic Chunker, Metadata Extractor)
  - **Milestone 3**: Integración en CP4 (IA Estructurada) & Presupuesto de Tokens (`internal/ai`, `pipeline.go`: prompt injection, top-k context formatting, token budgeting)
  - **Milestone 4**: Gobernanza, Casos Dorados, E2E & Anti-Hallucination (`test/e2e`, golden test suite, PII validation, race testing, audit)
- **Phase 3: Final Verification & Hand-off**
  - Run full suite with race detector uncached, ensure green build, conduct forensic audit, and deliver handoff to parent.
