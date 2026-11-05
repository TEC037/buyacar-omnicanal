## 2026-10-29T05:35:29Z
You are the Worker for Milestone 4 (Squad 4: Consola Web Embebida y Observabilidad) of BuyaCAR Omnicanal Evolution.
Your working directory is: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\worker_squad4_1\
Your parent is Orchestrator (Conversation ID: 84a866f8-986b-4c92-a496-9363b3a17b7f).

MANDATORY FIRST STEP:
Read the authoritative request in: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\ORIGINAL_REQUEST.md
Also read PROJECT.md, TEST_INFRA.md, and TEST_READY.md in workspace root.
Follow the domain skill `go-quality` at: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\skills\go-quality\SKILL.md

Write ownership:
You own EXCLUSIVELY:
- `app_build/internal/server/`
- `app_build/cmd/buyacar/`
DO NOT modify any other files.

Mission (Features 23–28):
1. Implement Embedded Local Web Server (`internal/server/server.go`): `net/http` native server using `embed.FS` for assets (`internal/server/assets/`), graceful shutdown with context.
2. Implement Web Supervisor Dashboard UI (`internal/server/assets/index.html` & `dashboard_handler.go`): single-page UI for escalate.Queue review, 1-click DRAFT approval endpoint (`POST /api/draft/approve`), channel unpause endpoint (`POST /api/channel/unpause`).
3. Implement Prometheus `/metrics` Endpoint (`internal/server/metrics_exporter.go`): latencies p95/p99, cache hit/miss, channel msg count, paused channels, consecutive failures, queue size.
4. Implement Structured JSON CLI Inspection Mode in `cmd/buyacar/main.go`: `--server`, `--port`, `--metrics` (stdout), `--inspect` (structured JSON dump).
5. Write unit tests in `internal/server/*_test.go` and `cmd/buyacar/*_test.go` and verify 100% pass via `go test -v ./internal/server/... ./cmd/buyacar/...`.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Deliver your handoff report to handoff.md and send completion message to parent orchestrator.
