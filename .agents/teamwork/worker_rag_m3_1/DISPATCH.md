## 2026-10-29T13:24:38Z
You are teamwork_worker implementing Milestone 3: CP4 Structured AI RAG Integration & Strict Token Budgeting.
Working directory: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\worker_rag_m3_1
Original request path: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\ORIGINAL_REQUEST.md
Scope document: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\PROJECT.md
Domain skill: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\skills\go-quality\SKILL.md
Exclusive write ownership: app_build/internal/skills/, app_build/internal/ai/, app_build/internal/core/pipeline.go (and pipeline tests).

Tasks:
1. `app_build/internal/skills/`:
   - Enhance `CompactContext` to include `RAGChunks []rag.Chunk` and/or `RAGSummary string`.
   - Implement `BuildCompactContextWithRAG(intent, historySummary, candidates, ragChunks, maxRAGTokens)`.
   - Ensure deterministic token estimation (~1 token / 4 chars) and enforce token cap on RAG fragments (default max 150-200 tokens) so CP4 prompt never overflows.
   - Maintain full backward compatibility for `BuildCompactContext`.
2. `app_build/internal/ai/`:
   - Include RAG context in prompt generation for providers (Gemini, Ollama, Groq) formatted concisely under `[DOCUMENTOS BUyACAR BOYACA/TUNJA]`.
   - Update Orchestrator / Cascade to handle RAG context seamlessly.
3. `app_build/internal/core/pipeline.go`:
   - Integrate `rag.Engine` into `Pipeline` (e.g. `RAGEngine rag.Engine` in `Pipeline` and `PipelineConfig`).
   - In CP4 (step 234-250), if `p.RAGEngine != nil`: retrieve top-k chunks with query token budget (`rag.Query{Text: cleanedText, TopK: 3, MaxTokens: 150}`), build context with `BuildCompactContextWithRAG`, and pass to `p.AIOrch.Resolve`.
   - Add unit tests verifying CP4 RAG context enrichment, token budget compliance, and zero regression across existing pipeline tests.
4. Verification:
   Run `go test -v -count=1 ./internal/skills/...`, `go test -v -count=1 ./internal/ai/...`, `go test -v -count=1 ./internal/core/...`, and full repo `go test -count=1 ./...`.
