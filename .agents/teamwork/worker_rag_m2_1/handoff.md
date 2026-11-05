# Handoff Report — Milestone 2: Document Indexing & Deterministic Semantic Chunking

## 1. Observation
- Built fixtures in `app_build/fixtures/rag/`:
  - `vehicles.json` (5 models: Duster, Stepway, Logan, Tracker, CX-30 with engine, km, transmission, prices, URLs, Boyacá showroom specs).
  - `vehicles.md` (detailed markdown technical sheets matching schema).
  - `warranty_policy.md` (12 months/20,000 km motor & transmission, wear exclusions, Tunja/Duitama workshops).
  - `credit_requirements.md` (employees, independents, 10% down payment, Bancolombia, Davivienda, Finandina, 24h approval).
  - `faq_boyaca.md` (business hours Mon-Sat 8am-6pm, Av. Universitaria Tunja, 150-pt peritaje, trade-in, Tunja/Duitama/Sogamoso transit).
  - `ragfixtures.go` (embedded `embed.FS` packaging `*.json` and `*.md`).
- Implemented in `app_build/internal/rag/`:
  - `chunker.go`: Deterministic semantic chunker with heading parser (`#`, `##`, `###`), JSON array & object parser, paragraph/sentence boundary enforcement, tag extraction with Spanish accent normalization (`stripAccents`), and backed numbers extraction.
  - `fixtures_loader.go`: FS/Disk loader and atomic chunk indexing (`IndexChunks`, `IndexChunk`) into `InMemoryEngine`.
  - `chunker_test.go`: 11 test suites validating structural chunking, boundaries, metadata/tags, backed numbers, embedded/disk loading, end-to-end retrieval, token budget constraints, deterministic reproducibility, and concurrent execution.
- Command outputs:
  - `go test -v -count=1 ./internal/rag/...`: 21 tests passed (0 failures).
  - `go vet ./internal/rag/...`: 0 errors.
  - Full repo `go test -count=1 ./...`: 100% passed (0 failures).

## 2. Logic Chain
- Markdown documents require semantic coherence without exceeding LLM context windows; chunking by hierarchical headers (`#`, `##`, `###`) with paragraph fallback guarantees both context preservation and strict token/character limits.
- JSON inventories have discrete structured records; parsing each vehicle item into a self-contained markdown description extracts high-fidelity metadata and 5+ digit numeric figures (price, mileage, phone numbers) for CP5 anti-hallucination.
- Embedding fixtures via `embed.FS` ensures zero runtime disk dependency in compiled binaries, while filesystem fallback enables dynamic loading in dev/test workflows.
- Thread-safe batch indexing in `InMemoryEngine` updates BM25 inverse document frequencies and global backed numbers atomically.

## 3. Caveats
- Windows environment lacks GCC in PATH, so `-race` requires enabling CGO with an installed C compiler; concurrency correctness was verified via parallel goroutine stress tests (`TestChunker_ConcurrencyAndRace`).
- Stop word stripping and tag extraction use standard Spanish normalization.

## 4. Conclusion
- Milestone 2 is fully implemented, verified, and ready for CP4 integration in Milestone 3. All acceptance criteria and anti-regression directives are met with zero external dependencies.

## 5. Verification Method
- Run internal rag test suite:
  `cd app_build; go test -v -count=1 ./internal/rag/...`
- Run lint/vet check:
  `cd app_build; go vet ./internal/rag/...`
- Run full suite:
  `cd app_build; go test -count=1 ./...`
