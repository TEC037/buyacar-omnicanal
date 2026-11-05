# Task Assignment — Worker M1: Pre-send Revalidator & CRM Sealing (R1)

**Role**: teamwork_preview_worker
**Working Directory**: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\worker_m1_1
**Authoritative Request**: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\ORIGINAL_REQUEST.md
**Project Plan**: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\PROJECT.md

## Reference Blueprints from Explorers:
- CRM Module: `c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\explorer_m1_1\analysis.md` and `handoff.md`
- Inventory & Store: `c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\explorer_m1_2\analysis.md` and `handoff.md`
- Pipeline Revalidator: `c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\explorer_m1_3\analysis.md` and `handoff.md`

## Exclusive File Ownership
You exclusively own and may edit or create:
- `app_build/internal/crm/crm.go`
- `app_build/internal/crm/crm_test.go`
- `app_build/internal/inventory/inventory.go`
- `app_build/internal/inventory/inventory_test.go`
- `app_build/internal/store/store.go`
- `app_build/internal/store/store_test.go`
- `app_build/internal/core/pipeline.go`
- `app_build/internal/core/pipeline_test.go`
- `app_build/cmd/buyacar/main.go`

## Objective & Implementation Steps
1. Create `internal/crm`: Implement `Task`, `TaskType`, `TaskStatus`, `Manager` interface, and thread-safe `MemoryManager` (`sync.RWMutex`). Include unit tests in `crm_test.go`.
2. Enhance `internal/inventory`: Add `GetVehicle(id string) (Vehicle, bool)`, `IsFresh(now time.Time) bool`, and `GetETag() string` / `ETag() string` to `inventory.Cache`.
3. Enhance `internal/store`: Add `SendStatus`, `ETag`, and `CRMTaskID` to `IdempotencyRecord`. Add `SealIdempotency()` to `Store` and `MemoryStore`. Add unit tests in `store_test.go`.
4. Update `internal/core/pipeline.go`:
   - Add `CRM crm.Manager` to `Pipeline`.
   - Implement `RevalidatePreSend(ctx context.Context, conv channel.Conversation, vehicleID string, now time.Time) (bool, string)`.
   - Between CP5 and CP6, revalidate vehicle availability and cache freshness. If sold/reserved, abort sales pitch, emit `SoldVehicleNotice`, enqueue escalation alert, and dispatch notice to customer. If stale/unavailable, abort dispatch immediately.
   - After CP6 `ch.Reply()`, verify `sendRes.Status`. In AUTOPILOT require `sent_verified`; in DRAFT accept `draft_recorded`; in SIMULATION accept `simulated_no_op`. On failure or status mismatch, enqueue escalation alert, refuse `cp6_envio:ok`, and return error.
   - At CP7, settle CRM tasks (`aiRes.Output.CRMUpdate.TasksToCreate` or inferred from Fast-Path) and seal `IdempotencyRecord` with `SendStatus`, `ETag`, and `CRMTaskID`.
5. Update `cmd/buyacar/main.go` to wire `crm.NewMemoryManager()` into `pipe`.
6. Run `go test -v ./...` in `app_build/`. Ensure:
   - 100% tests pass (including 64 golden cases and 100 draft batch exerciser).
   - Fast-Path latency p99 < 5 ms.
   - 0 race conditions.
   - 0 PII leaks and 0 credentials.
7. Deliver `handoff.md` with complete 5-component report including test and benchmark execution output. Notify orchestrator via `send_message`.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

## 2026-10-29T02:54:06Z
You are Worker M1 (Implementation of R1 - Pre-send Revalidator, CRM Sealing & Idempotency) for BuyaCAR Omnicanal.
Your working directory is: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\worker_m1_1
Read your DISPATCH.md, c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\ORIGINAL_REQUEST.md, c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\PROJECT.md, and the explorer reports in .agents/teamwork/explorer_m1_1, explorer_m1_2, explorer_m1_3.
Implement all components modularly in app_build/:
1. Create internal/crm/ (Task, TaskType, Manager, MemoryManager, tests).
2. Enhance internal/inventory/ (GetVehicle, IsFresh, GetETag, tests).
3. Enhance internal/store/ (IdempotencyRecord with SendStatus, ETag, CRMTaskID; SealIdempotency; tests).
4. Update internal/core/pipeline.go (pre-send revalidator, sold/reserved escalation, send status verification, CRM settlement, idempotency sealing).
5. Update cmd/buyacar/main.go to wire CRM manager.
6. Verify with `go test -v ./...` in app_build/ that all packages pass 100% (including 64 golden cases and 100 batch exerciser) and Fast-Path latency benchmark is < 5 ms.
Write handoff.md in your working directory and notify the caller via send_message.

## 2026-10-29T03:05:30Z
**Context**: Milestone 1 Implementation (internal/core/pipeline.go)
**Content**: Note from E2E Test Writer: in `internal/ai/ai.go`, the execution result type is `ai.ExecutionResult` (not `ai.ResolutionResult`).
**Action**: Please ensure you use `ai.ExecutionResult` when referencing AI resolution results in `internal/core/pipeline.go`.


