## 2026-10-29T13:51:05Z
You are teamwork_worker implementing Milestone 4: Governance, CP5 Anti-Hallucination Backed Numbers, Golden Tests & E2E Validation.
Working directory: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\worker_rag_m4_1
Original request path: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\ORIGINAL_REQUEST.md
Scope document: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\PROJECT.md
Domain skill: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\skills\go-quality\SKILL.md
Exclusive write ownership: app_build/internal/guard/, app_build/internal/core/pipeline.go, and app_build/test/rag_golden_test.go.

Tasks:
1. `app_build/internal/guard/guard.go`:
   - Enhance `Guard` with `ValidateOutboxWithBackedNumbers(reply string, channelID string, relevantVehicles []inventory.Vehicle, extraAllowedNumbers []string) ValidationResult`.
   - Ensure existing `ValidateOutbox` maintains 100% backward compatibility by calling `ValidateOutboxWithBackedNumbers(..., nil)`.
   - Allow 5+ digit numbers backed by RAG fixtures (e.g. 20000 km warranty, official prices, approved phones) while strictly blocking unbacked numbers (anti-hallucination).
   - Enforce HTTPS allowlist to `https://buyacar.com.co` and PII redaction (Ley 1581 de 2012).
2. `app_build/internal/core/pipeline.go`:
   - In CP5 (step 280-295), pass backed numbers from `p.RAGEngine.GetBackedNumbers()` to `p.Guard.ValidateOutboxWithBackedNumbers`.
3. `app_build/test/rag_golden_test.go`:
   - Implement programmatic golden test suite covering representative user queries:
     * Vehicle catalog query (specs, km, prices in Boyacá).
     * Warranty policy query (12m/20k km, coverage, workshops in Tunja/Duitama).
     * Credit requirements query (employees, independents, 10% down payment, allied banks).
     * FAQ Boyacá/Tunja query (hours, address Av. Universitaria, peritaje, retoma).
     * Anti-hallucination test: backed numbers pass, unbacked 5+ digit numbers are blocked and escalated.
     * PII redaction test: customer cédula/phones redacted under Ley 1581.
     * Concurrency test: parallel queries to Pipeline with RAGEngine.
4. Verification:
   Run `go test -v -count=1 ./test/...`, `go test -v -count=1 ./internal/guard/...`, and full repo `go test -count=1 ./...`.
