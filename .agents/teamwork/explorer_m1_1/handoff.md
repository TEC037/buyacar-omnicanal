# Handoff Report: Explorer M1.1 — CRM Module Design & CP7 Integration

**Agent**: Explorer M1.1 (`explorer_m1_1`)  
**Parent**: Orchestrator (`41bf3eb6-8d36-4b70-8f73-4cb69f505710`)  
**Target Package**: `app_build/internal/crm/`  
**Milestone**: Milestone 1 (R1 — Pre-send Revalidator & CRM Sealing)  
**Date**: 2026-10-29  

---

## 1. Observation

1. **CRM Contract Definition (`PROJECT.md:74-100`)**:
   `PROJECT.md` establishes the interface requirements for CRM:
   ```go
   package crm

   type TaskType string
   const (
       TaskScheduleTestDrive TaskType = "schedule_test_drive"
       TaskCommercialFollowup TaskType = "commercial_followup"
       TaskCreditApplication TaskType = "credit_application"
       TaskGeneralInquiry    TaskType = "general_inquiry"
   )

   type Task struct {
       ID          string    `json:"id"`
       ConvID      string    `json:"conv_id"`
       ChannelID   string    `json:"channel_id"`
       Type        TaskType  `json:"type"`
       Description string    `json:"description"`
       VehicleID   string    `json:"vehicle_id,omitempty"`
       CreatedAt   time.Time `json:"created_at"`
   }

   type Manager interface {
       RecordTask(ctx context.Context, task Task) error
       GetTasksByConv(convID string) []Task
   }
   ```

2. **Current CP7 Implementation (`app_build/internal/core/pipeline.go:156-166`)**:
   ```go
   // CP7: Persistir idempotencia y estado CRM para reanudar tras reinicio
   _ = p.Engine.store.MarkProcessed(store.IdempotencyRecord{
       Hash:      hash,
       ChannelID: ch.ID(),
       ConvID:    conv.ID,
       ReplyText: valRes.SanitizedReply,
       CreatedAt: now,
   })
   _ = p.Engine.store.SaveCheckpointState(conv.ID, "cp7_crm:ok")
   outcome.CheckpointsOK++
   ```
   The existing `Pipeline` struct (`pipeline.go:35-42`) lacks a `CRM` reference, and `IdempotencyRecord` does not yet seal `SendStatus`, `ETag`, or `CRMTaskID`.

3. **AI Task Proposals (`app_build/internal/ai/ai.go:29-33`, `ai_test.go:22-24`)**:
   In `ai.go`:
   ```go
   type CRMUpdate struct {
       TasksToCreate []string `json:"tasks_to_create"`
   }
   ```
   In `ai_test.go:23`, sample output is `"tasks_to_create": ["agendar_prueba_ruta"]`.

4. **Store Idempotency Schema (`app_build/internal/store/store.go:10-17`)**:
   ```go
   type IdempotencyRecord struct {
       Hash      string
       ChannelID string
       ConvID    string
       ReplyText string
       CreatedAt time.Time
   }
   ```
   Requires addition of `SendStatus`, `ETag`, and `CRMTaskID` to fulfill R1 / Step 11.

5. **Inventory Cache ETag Visibility (`app_build/internal/inventory/inventory.go:60-68, 92`)**:
   `Cache` holds `etag string` internally, set via `LoadSnapshot`, but lacks a thread-safe `GetETag()` getter method.

6. **Toolchain & Test Baseline**:
   Running `cd app_build; go test ./...` passes cleanly across all packages with exit code 0. Running `go test -race` on Windows reports `go: -race requires cgo; enable cgo by setting CGO_ENABLED=1`.

---

## 2. Logic Chain

1. **Contract Alignment**:
   `PROJECT.md` and `DISPATCH.md` require a thread-safe `MemoryManager` supporting `Task`, `TaskType`, `TaskStatus`, and `Manager`. Adding `Status TaskStatus` and `UpdatedAt time.Time` to `Task` enables task lifecycle management while preserving compatibility with `PROJECT.md` (Observation 1).

2. **Deterministic Dual-Path Settlement (AI + Fast-Path)**:
   - When AI is used (`outcome.UsedAI`), `aiRes.Output.CRMUpdate.TasksToCreate` provides task names like `"agendar_prueba_ruta"`. These must be mapped to `crm.TaskType` via `crm.NormalizeTaskType()` (Observation 3).
   - When Fast-Path is used (`outcome.UsedFastPath`), 0 tokens are spent on LLMs. The pipeline must infer the task deterministically from `cl.Intent` and vehicle presence via `crm.InferTaskFromTriage()`.
   - If an escalation occurred (e.g. `vehicle_sold_or_reserved`), the settled task is marked with `TaskStatusEscalated`.

3. **Non-Breaking Pipeline Integration**:
   Adding `CRM crm.Manager` to `core.Pipeline` and checking `if p.CRM != nil` ensures existing tests (`golden64_test.go`, `draft_batch_exerciser_test.go`, `core_test.go`) continue to pass without modification (Observation 2). In `ProcessConversation()`, moving the declaration of `var aiRes ai.ResolutionResult` outside the `candidateReply == ""` block makes `aiRes` available during CP7.

4. **Idempotency Sealing (R1 / Step 11)**:
   By extending `store.IdempotencyRecord` with `SendStatus string`, `ETag string`, and `CRMTaskID string` and adding `GetETag()` to `inventory.Cache`, CP7 can record all 3 sealed attributes directly in `MarkProcessed` (Observations 2, 4, 5).

5. **Concurrency & Zero Data Race**:
   `MemoryManager` utilizes `sync.RWMutex` where `RecordTask` and `UpdateTaskStatus` acquire exclusive `mu.Lock()`, while `GetTasksByConv`, `GetTask`, and `ListTasks` acquire shared `mu.RLock()`. All read queries return newly allocated slices to ensure external mutation cannot cause data races (Observation 1).

---

## 3. Caveats

1. **Race Detector on Windows**:
   `go test -race` requires a C compiler (MinGW/GCC) when running on Windows (`go: -race requires cgo`). The code design guarantees race-free operation by strictly guarding all read and write accesses behind `sync.RWMutex`, which can be validated with CGO in Linux CI or with MinGW.
2. **Backward-Compatibility with Nil `p.CRM`**:
   The implementer must ensure `p.CRM` is nil-checked before dereferencing in `pipeline.go` so tests constructing `&Pipeline{...}` without `CRM` do not panic.
3. **No External Dependencies**:
   The CRM package relies strictly on Go's standard library (`context`, `errors`, `fmt`, `strings`, `sync`, `time`) and internal packages (`buyacar.com.co/omnicanal/internal/triage`).

---

## 4. Conclusion

The CRM technical specification, data models, memory manager, concurrency protections, unit testing suite, and CP7 pipeline integration are fully designed and documented in:
`c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\explorer_m1_1\analysis.md`.

### Implementation Action Plan for Implementer Agent:
1. **Create `app_build/internal/crm/crm.go`**: Implement `Task`, `TaskType`, `TaskStatus`, `Manager`, `MemoryManager`, `NormalizeTaskType`, and `InferTaskFromTriage`.
2. **Create `app_build/internal/crm/crm_test.go`**: Add the 8 unit and concurrency tests designed in `analysis.md §5.2`.
3. **Update `app_build/internal/store/store.go`**: Add `SendStatus`, `ETag`, and `CRMTaskID` to `IdempotencyRecord`.
4. **Update `app_build/internal/inventory/inventory.go`**: Add `func (c *Cache) GetETag() string`.
5. **Update `app_build/internal/core/pipeline.go`**: Add `CRM crm.Manager` to `Pipeline`, hoist `aiRes` scope, and implement `settleCRMTasks` in CP7.
6. **Update `app_build/cmd/buyacar/main.go`**: Wire `crmMgr := crm.NewMemoryManager()` into `pipe`.

---

## 5. Verification Method

1. **Unit Test CRM Package**:
   ```powershell
   cd c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\app_build
   go test -v ./internal/crm
   ```
   *Expected*: PASS for all 8 test cases (`RecordAndGetByConv`, `ValidationAndDefaults`, `GetTaskAndListTasks`, `UpdateTaskStatus`, `NormalizeTaskType`, `InferTaskFromTriage`, `ConcurrentAccess`, `ContextCancellation`).

2. **Full Test Suite & Non-Regression**:
   ```powershell
   cd c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\app_build
   go test ./...
   ```
   *Expected*: PASS across all packages, with 0 failures in Golden 64 and Draft 100 batch exercisers.

3. **Idempotency Record Sealing Verification**:
   Verify in `internal/core/pipeline_test.go` that `st.HasProcessed(hash)` returns an `IdempotencyRecord` where `CRMTaskID != ""` and `SendStatus == "sent_simulated"`.
