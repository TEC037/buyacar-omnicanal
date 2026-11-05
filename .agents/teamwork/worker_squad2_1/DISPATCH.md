## 2026-10-29T05:17:19Z
You are the Worker for Milestone 2 (Squad 2: Persistencia, DMS & CRM) of BuyaCAR Omnicanal Evolution.
Your working directory is: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\worker_squad2_1\
Your parent is Orchestrator (Conversation ID: 84a866f8-986b-4c92-a496-9363b3a17b7f).

MANDATORY FIRST STEP:
Read the authoritative request in: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\ORIGINAL_REQUEST.md
Also read PROJECT.md in workspace root.
Follow the domain skill `go-quality` at: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\skills\go-quality\SKILL.md

Write ownership:
You own EXCLUSIVELY:
- `app_build/internal/store/`
- `app_build/internal/crm/`
- `app_build/internal/inventory/`
DO NOT modify any other directory.

Mission (Features 11–17):
1. Implement Resilient ACID Store & fallback (`internal/store/`): pure Go transactional engine with WAL semantics/atomic persistence for `IdempotencyRecord` and `CheckpointState`, with automatic failover to in-memory + local flat JSON append log (`json_store.go`, `resilient_store.go`) upon disk errors, enqueuing alert to `escalate.Queue`. Implement `ResilientStore` interface.
2. Implement Periodic DMS Inventory Synchronization (`internal/inventory/dms_sync.go`): background worker loop checking inventory with ETag validation (< 30 min loop), `304 Not Modified` handling.
3. Implement Real-time Inventory Invalidation Webhook (`internal/inventory/invalidation_webhook.go`): HTTP POST webhook refreshing cache entry and recalculating ETag upon external ERP events.
4. Implement Waitlist SHA-256 Next-Entry Alert Engine (`internal/inventory/waitlist.go`): customer reservation manager with SHA-256 sealing, generating priority alert / CRM task when vehicle returns to `available`.
5. Implement Outbound CRM Webhook Dispatcher (`internal/crm/webhook_exporter.go`): HTTP POST exporting tasks/leads signed with HMAC-SHA256 (`X-BuyaCAR-Signature-256`), with exponential backoff and fallback to `escalate.Queue` on retry exhaustion.
6. Write unit tests in `internal/store/*_test.go`, `internal/crm/*_test.go`, `internal/inventory/*_test.go` and verify 100% pass via `go test -v ./internal/store/... ./internal/crm/... ./internal/inventory/...`.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Deliver your handoff report to handoff.md and send completion message to parent orchestrator.


## 2026-10-29T05:18:35Z
**Context**: Directiva Suprema Vinculante (7 Mandamientos Anti-Regresión) para Squad 2
**Content**: Se ha emitido una directiva suprema inmutable que debes incorporar en tu implementación:
1. Resistencia ACID de datos y sellado de idempotencia (hash SHA-256 único, inferencia y registro de tarea comercial con persistencia tolerante a reinicios). Si la base principal falla, fallback automático a memoria + JSON plano sin bloquear la operación, emitiendo alerta a escalate.Queue.
2. DMS sync con TTL < 30m y ETag; webhook de invalidación de inventario con MAX_BODY_BYTES = 256 KB y timeouts < 30s.
3. Concurrencia con sync.RWMutex en inventario y store, sin data races.
4. Exportación CRM saliente firmada con HMAC-SHA256, backoff exponencial y escalamiento a escalate.Queue tras agotamiento de reintentos.
Revisa PROJECT.md para más detalles.
**Action**: Implementa asegurando cumplimiento estricto de estos 7 mandamientos.


## 2026-10-29T05:24:31Z
**Context**: URGENT: Break import cycle in `app_build/internal/inventory/waitlist.go`
**Content**: An import cycle has been detected:
`internal/inventory` imports `internal/crm` (waitlist.go) -> `internal/crm` imports `internal/triage` -> `internal/triage` imports `internal/inventory`.
Go compilation fails with `import cycle not allowed`.
**Action**: Immediately decouple `internal/inventory/waitlist.go` from `internal/crm`. DO NOT import `buyacar.com.co/omnicanal/internal/crm` in `internal/inventory`. Instead:
1. Define a minimal local interface in `internal/inventory` (e.g. `type WaitlistNotifier interface { ... }` or `type TaskRecorder interface { ... }`) or define local waitlist task structures, or have `CheckAndNotify` return the generated alert tasks to the caller without referencing `crm.Task`.
2. Ensure `cd app_build; go test ./internal/inventory/...` compiles cleanly without import cycles.
