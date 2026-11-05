# Codebase Survey & Gap Analysis: Core & Pipeline (R1)

- **Date**: 2026-10-29T02:43:00Z
- **Explorer**: `explorer_survey_core_1`
- **Component**: Core Engine, Pipeline Execution, Inventory Freshness, Idempotency Sealing & CRM Task Registration
- **Target Milestone**: R1 — Pre-Send Revalidator & CRM Sealing (Step 11 of `instructivo.md`)

---

## 1. Executive Summary

This investigation surveys the Go codebase in `app_build/` to evaluate readiness and identify technical gaps for implementing **Requirement R1 (Pre-Send Revalidator & CRM Sealing)**.
The codebase is healthy, modular, and compiles cleanly with 100% green tests across all packages, passing the 64 golden cases (`golden64_test.go`) and the 100-batch DRAFT exerciser (`draft_batch_exerciser_test.go`).

However, the current pipeline (`app_build/internal/core/pipeline.go`) lacks the critical pre-send revalidation safeguards mandated by Step 11 of `instructivo.md` and `.agents/prompts/BUYACAR_FINAL_INTEGRATED.md`:
1. It does not revalidate vehicle availability or inventory freshness against the cache immediately before invoking `channel.Reply()`.
2. It does not check if `sendRes.Status == channel.SendFailed` (or enforce `sent_verified` in `AUTOPILOT`).
3. It marks the message hash as processed regardless of whether the send actually succeeded.
4. There is no dedicated CRM abstraction or package (`internal/crm` does not exist); the pipeline merely records a string `"cp7_crm:ok"` without registering any customer task or tracking CRM status.

Below is the complete architectural map, line-by-line evidence, and recommended interface designs to implement R1 cleanly without breaking existing tests.

---

## 2. Architecture Mapping

### 2.1 The 12-Step Conversation Lifecycle

The canonical specification (`.agents/prompts/BUYACAR_FINAL_INTEGRATED.md`, lines 203–219) defines a strict 12-step flow per conversation:

| Step # | Canonical Definition | Current Implementation Location | Current Status |
|:---|:---|:---|:---|
| **Step 1** | Open active link, navigate, list pending | `cmd/buyacar/main.go:115-125`, `channel.Pending()` | Implemented |
| **Step 2** | Skip evaluation (own message, duplicate hash, >9 weeks, kill switch) | `internal/core/core.go:100-127` (`ShouldProcessThread`) | Implemented |
| **Step 3** | Triage intent classification (CP2) | `internal/core/pipeline.go:68`, `internal/triage/triage.go` | Implemented |
| **Step 4** | Fast-Path if exact match & confidence >= 0.80 | `internal/core/pipeline.go:76-101`, `internal/fast/fast.go` | Implemented |
| **Step 5** | 6 Inventory statuses (`exact_match`, `multiple_matches`, `no_match`, etc.) | `internal/inventory/inventory.go:10-20` | Implemented |
| **Step 6** | Multiple matches / no match / stale -> ask or escalate | `internal/fast/fast.go:107-124` | Implemented |
| **Step 7** | Complex queries -> Gemini / Ollama / Human fallback | `internal/core/pipeline.go:104-127`, `internal/ai/ai.go` | Implemented |
| **Step 8** | Prefilter catalog (1–3 vehicles + intent + summary) | `internal/skills/skills.go:BuildCompactContext` | Implemented |
| **Step 9** | Outbox Guard validation (forbidden phrases, HTTPS URLs, 5+ digits, limits) | `internal/core/pipeline.go:130-138`, `internal/guard/guard.go` | Implemented |
| **Step 10**| Commercial escalation triggers (discounts, trade-in, credit, etc.) | `internal/triage/triage.go`, `internal/escalate/escalate.go` | Implemented |
| **Step 11**| **Pre-send revalidation in fresh cache, verify `sent_verified`/`send_failed`, seal idempotency & record CRM** | `internal/core/pipeline.go:141-165` | **GAP: Missing Pre-Send Revalidation, SendStatus checks, and CRM task registration** |
| **Step 12**| Persist state to resume after restart, zero duplicate sends | `internal/store/store.go:SaveCheckpointState`, `MarkProcessed` | Partially implemented |

---

### 2.2 Deep Dive: Existing Pipeline Flow (`app_build/internal/core/pipeline.go`)

In `internal/core/pipeline.go`, `ProcessConversation` connects all subsystems:

```go
// Line 45: ProcessConversation execution flow
func (p *Pipeline) ProcessConversation(ctx context.Context, ch channel.Channel, sess channel.Session, conv channel.Conversation, now time.Time) (PipelineOutcome, error) {
    // CP1: Lectura (lines 49-54)
    th, err := ch.Read(ctx, sess, conv)
    ...
    // Step 2: Skip Rules (lines 57-62)
    hash, err := p.Engine.ShouldProcessThread(ctx, th, now)
    ...
    // CP2: Triage (lines 68-70)
    cl := triage.Classify(cleanedText)
    ...
    // CP3 & CP4: Fast-Path / Inventory / Response (lines 76-127)
    ...
    // CP5: Guard (lines 130-138)
    valRes := p.Guard.ValidateOutbox(candidateReply, ch.ID(), relevantVehicles)
    ...
    // CP6: Envio (lines 141-154)
    sendRes, err := ch.Reply(ctx, sess, conv, channel.Message{
        ID:        "out-" + conv.ID,
        Sender:    "own",
        Text:      valRes.SanitizedReply,
        Timestamp: now,
    }, p.Engine.cfg.OperatingMode)
    if err != nil {
        p.EscQueue.Enqueue(ch.ID(), conv.ID, "send_failed", err.Error(), now)
        outcome.Escalated = true
        outcome.EscalationReason = "send_failed"
        return outcome, err
    }
    _ = p.Engine.store.SaveCheckpointState(conv.ID, "cp6_envio:ok")
    outcome.CheckpointsOK++

    // CP7: Persistir idempotencia y estado CRM (lines 157-165)
    _ = p.Engine.store.MarkProcessed(store.IdempotencyRecord{
        Hash:      hash,
        ChannelID: ch.ID(),
        ConvID:    conv.ID,
        ReplyText: valRes.SanitizedReply,
        CreatedAt: now,
    })
    _ = p.Engine.store.SaveCheckpointState(conv.ID, "cp7_crm:ok")
    outcome.CheckpointsOK++

    outcome.SentReply = valRes.SanitizedReply
    outcome.SendStatus = sendRes.Status
    return outcome, nil
}
```

---

## 3. Detailed Component Analysis & R1 Gaps

### 3.1 Gap A: Missing Pre-Send Revalidation (Inventory Freshness & Vehicle Availability)

#### Current State
- `internal/inventory/inventory.go` provides:
  - `Cache`:
    - `vehicles []Vehicle` (in-memory snapshot)
    - `etag string`
    - `lastUpdated time.Time`
    - `refreshWindow time.Duration` (default 15 minutes)
    - `staleThreshold time.Duration` (default 30 minutes)
    - `available bool`
  - Methods:
    - `LoadSnapshot(vehicles []Vehicle, etag string, now time.Time)` (line 86)
    - `SetUnavailable()` (line 98)
    - `TouchRefresh(now time.Time)` (line 105)
    - `NeedsRefresh(now time.Time) bool` (line 115)
    - `Search(ctx context.Context, q Query, now time.Time) MatchResult` (line 125)

#### What is Missing for R1
1. **No direct vehicle lookup by ID**: `Cache` does not have a `GetVehicle(id string) (Vehicle, bool)` or `FindByID(id string) (Vehicle, bool)`.
2. **No dedicated freshness validator**: Freshness check is embedded inside `Search()`, which requires building a `Query`. There is no standalone `CheckFreshness(now time.Time) error` or `IsFresh(now time.Time) (bool, Status)`.
3. **No pre-send vehicle status check**:
   - In `pipeline.go`, after `Guard.ValidateOutbox()` approves the candidate reply (line 130), execution proceeds directly to `ch.Reply()` (line 141).
   - If a vehicle was available during CP3/CP4, but changed status to `"sold"` or `"reserved"` in the fresh cache (or if the cache expired past 30 minutes) before the message is dispatched, the pipeline currently sends the obsolete reply anyway!
4. **Behavior when vehicle is sold/reserved**:
   - Requirement: Revalidate against fresh inventory that the vehicle remains in `available` state. If status changed to `sold` or `reserved`, the pipeline must abort normal sending, escalate to human advisors (`vehicle_sold_or_reserved`), or emit the appropriate sold vehicle notification.

---

### 3.2 Gap B: Unchecked Channel Send Result (`sent_verified` vs `send_failed`)

#### Current State
In `internal/channel/channel.go`:
```go
// Line 66: SendStatus definitions
const (
    SendSimulated SendStatus = "simulated_no_op"
    SendDrafted   SendStatus = "draft_recorded"
    SendVerified  SendStatus = "sent_verified"
    SendFailed    SendStatus = "send_failed"
)

type SendResult struct {
    Status    SendStatus
    MessageID string
    Timestamp time.Time
}
```

In `internal/channel/stub.go` (lines 97-104):
- In `ModeSimulation`: returns `SendSimulated`.
- In `ModeDraft`: returns `SendDrafted`.
- In `ModeAutopilot`: returns `SendVerified`.

In `internal/channel/email_pilot.go` (lines 178-216):
- In `ModeSimulation`: returns `SendSimulated`.
- In `ModeDraft`: returns `SendDrafted`.
- In `ModeAutopilot`: returns `SendFailed` with `ErrEmailAutopilotBlocked`.

In `internal/core/pipeline.go` (lines 147-152):
```go
sendRes, err := ch.Reply(...)
if err != nil {
    p.EscQueue.Enqueue(ch.ID(), conv.ID, "send_failed", err.Error(), now)
    outcome.Escalated = true
    outcome.EscalationReason = "send_failed"
    return outcome, err
}
_ = p.Engine.store.SaveCheckpointState(conv.ID, "cp6_envio:ok")
```

#### What is Missing for R1
1. **Unchecked `sendRes.Status`**: If `ch.Reply()` returns `err == nil` but `sendRes.Status == channel.SendFailed`, `pipeline.go` currently records `cp6_envio:ok`, seals the idempotency hash, and considers the message successfully processed!
2. **Autopilot delivery verification**: In `ModeAutopilot`, Step 11 requires verifying `sent_verified`. If `sendRes.Status != channel.SendVerified`, the delivery was not verified and must be flagged as `send_failed`, preventing idempotency sealing and triggering human escalation.
3. **Draft / Simulation verification**: In `ModeDraft`, expected status is `SendDrafted`; in `ModeSimulation`, expected status is `SendSimulated`. Any other status indicates an unexpected channel state.

---

### 3.3 Gap C: Idempotency Hash Sealing Incompleteness

#### Current State
In `internal/store/store.go` (lines 10-26):
```go
type IdempotencyRecord struct {
    Hash      string
    ChannelID string
    ConvID    string
    ReplyText string
    CreatedAt time.Time
}

type Store interface {
    ComputeHash(channelID, convID, lastIncomingMessage string) string
    HasProcessed(hash string) (bool, IdempotencyRecord)
    MarkProcessed(record IdempotencyRecord) error
    SaveCheckpointState(convID string, checkpoint string) error
    GetCheckpointState(convID string) (string, bool)
}
```

#### What is Missing for R1
1. **Premature / Unconditional Sealing**:
   - `MarkProcessed` is invoked with just `Hash`, `ChannelID`, `ConvID`, `ReplyText`, `CreatedAt`.
   - If sending failed (`send_failed`), marking the hash as processed prevents future retries for that message, causing permanent dropped replies.
2. **Missing Metadata in `IdempotencyRecord`**:
   - The sealed record lacks:
     - `SendStatus channel.SendStatus`: Proof of whether it was `sent_verified`, `draft_recorded`, or `simulated_no_op`.
     - `ETag string`: Inventory snapshot version at the time of sealing.
     - `VehicleID string`: Verified vehicle ID.
     - `CRMTaskID string`: Reference to the registered CRM task.
3. **Atomic Sealing Contract**:
   - Step 11 requires "sealing the idempotency hash" as the terminal transaction tying together the verified send result and the CRM task.

---

### 3.4 Gap D: Missing CRM Integration / Task Tracking Architecture

#### Current State
- **There is no `internal/crm` package anywhere in `app_build/`**.
- References to CRM in existing codebase:
  1. `internal/ai/ai.go:30`:
     ```go
     type CRMUpdate struct {
         TasksToCreate []string `json:"tasks_to_create"`
     }
     type StructuredOutput struct {
         ...
         CRMUpdate CRMUpdate `json:"crm_update"`
     }
     ```
  2. `internal/core/pipeline.go:164`:
     `_ = p.Engine.store.SaveCheckpointState(conv.ID, "cp7_crm:ok")`
     (This is a pure string literal with no actual CRM action or data storage).
  3. `internal/obs/obs.go:23`:
     `CPCRM CheckpointName = "crm"`

#### What is Missing for R1
1. **CRM Contract & Service**:
   - Need an interface/struct for CRM tasks, e.g.:
     ```go
     type CRMTask struct {
         ID             string
         ConversationID string
         ChannelID      string
         CustomerAlias  string
         VehicleID      string
         TaskType       string // e.g. "quote_sent", "follow_up_test_drive", "vehicle_sold_notice", "human_escalation"
         Description    string
         CreatedAt      time.Time
         Status         string // "pending", "completed"
     }
     ```
   - Need a thread-safe in-memory CRM store/registry (following clean architecture with interface and memory stub).
2. **Task Generation Logic in Pipeline**:
   - For Fast-Path: When a vehicle quote or info is sent, register a corresponding CRM task (e.g. `quote_sent:Toyota_Hilux_2022`).
   - For AI: Ingest `aiRes.Output.CRMUpdate.TasksToCreate` and register them into the CRM service.
   - For Escalations: When escalated (e.g. `vehicle_sold_or_reserved`, `credit_study`, `human_takeover`), register an urgent advisor CRM task.
3. **CRM Checkpoint Evidence**:
   - CP7 must reflect real CRM task registration and idempotency sealing, recording `ok=true` only when the task was recorded and hash sealed.

---

## 4. Test Harness Survey & Compatibility

### 4.1 Canonical Golden 64 Suite (`app_build/internal/core/golden64_test.go`)
- Verifies 64 deterministic conversations:
  - 16 Fast-Path general queries (location, hours, contact, greeting) -> Expect Fast-Path=true, Escalate=false.
  - 16 Inventory queries ->
    - Exact matches (Hilux 2022, CX-5 2021, CX-5 2023) -> Fast-Path=true, Escalate=false.
    - Multiple matches (CX-5 without year) -> Fast-Path=false, Escalate=true.
    - Sold vehicle (Renault Duster 2020) -> Fast-Path=true, Escalate=true (Sends sold notice AND escalates).
    - No match (Ferrari Testarossa) -> Fast-Path=false, Escalate=true.
  - 16 Security & Guard queries -> Fast-Path=false, Escalate=true.
  - 16 Commercial & Credit escalation queries -> Fast-Path=false, Escalate=true.
- **Key Compatibility Requirement for R1**:
  - In Case 31 (Renault Duster 2020), `Fast.Evaluate()` returns `MustEscalate: true`, `EscalationReason: "vehicle_sold_or_reserved"`, and `Reply: SoldVehicleNotice`.
  - The pre-send revalidator must distinguish between:
    - A reply that is *already* an official sold notice (which should be permitted to inform the customer while escalating), vs.
    - A reply that *promises* availability of a vehicle that suddenly became sold or reserved in the fresh cache (which must be blocked and prevented from claiming availability).

### 4.2 Draft Batch Exerciser (`app_build/internal/core/draft_batch_exerciser_test.go`)
- Processes 100 conversations in `ModeDraft` through `EmailPilotChannel`.
- Validates `DraftExitGateReport` (`EvaluateDraftExitGate`):
  - `>= 100` conversations processed.
  - `0` hallucinated numbers.
  - `0` duplicate sends.
  - `>= 70%` without AI (Fast-Path).
  - `>= 85%` approved unedited.
- **Key Compatibility Requirement for R1**:
  - The pre-send revalidator, send verification, and CRM registration must execute seamlessly in `ModeDraft` without adding blocking overhead or failing `EvaluateDraftExitGate`.

---

## 5. Architectural Proposal for R1 Implementation

To satisfy R1 without introducing breaking changes or bloat, the following modular extensions are recommended:

```
app_build/
├── internal/
│   ├── crm/                    <-- NEW PACKAGE
│   │   ├── crm.go              // Task, Service, MemoryStore, TaskType
│   │   └── crm_test.go         // Unit tests for CRM task registration & thread-safety
│   ├── inventory/
│   │   └── inventory.go        // Add: GetVehicleByID, IsFresh, RevalidateVehicle
│   ├── store/
│   │   └── store.go            // Enrich IdempotencyRecord with SendStatus, ETag, CRMTaskID
│   └── core/
│       ├── revalidator.go      <-- NEW FILE: PreSendRevalidator logic (Step 11)
│       ├── pipeline.go         // Wire pre-send check, SendStatus validation & CRM sealing
│       ├── revalidator_test.go <-- NEW FILE: Tests for cache stale, sold transition, send_failed
│       ├── pipeline_test.go    // Verify updated CP7 & sealing
│       └── golden64_test.go    // Regress 64 cases
```

### Proposed Interface Sketches

#### 1. `inventory.Cache` Extensions
```go
// In internal/inventory/inventory.go
func (c *Cache) GetVehicle(id string) (Vehicle, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    for _, v := range c.vehicles {
        if v.ID == id {
            return v, true
        }
    }
    return Vehicle{}, false
}

func (c *Cache) IsFresh(now time.Time) (bool, Status) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    if !c.available || c.lastUpdated.IsZero() {
        return false, StatusUnavailable
    }
    if now.Sub(c.lastUpdated) > c.staleThreshold {
        return false, StatusStale
    }
    return true, StatusExactMatch
}
```

#### 2. `internal/crm` Package
```go
// In internal/crm/crm.go
type TaskType string

const (
    TaskQuoteSent        TaskType = "quote_sent"
    TaskVehicleInquiry   TaskType = "vehicle_inquiry"
    TaskSoldNoticeSent   TaskType = "sold_notice_sent"
    TaskHumanEscalation  TaskType = "human_escalation"
    TaskGeneralInquiry   TaskType = "general_inquiry"
)

type Task struct {
    ID             string
    ConversationID string
    ChannelID      string
    CustomerAlias  string
    VehicleID      string
    Type           TaskType
    Details        string
    CreatedAt      time.Time
}

type Manager interface {
    RecordTask(ctx context.Context, task Task) (string, error)
    ListTasks(convID string) []Task
}
```

#### 3. Pre-Send Revalidator in Pipeline (`internal/core/pipeline.go` & `revalidator.go`)
```go
type RevalidationResult struct {
    Approved         bool
    MustEscalate     bool
    EscalationReason string
    FreshETag        string
    ValidatedVehicle *inventory.Vehicle
}

func (p *Pipeline) RevalidatePreSend(ctx context.Context, reply string, vehicles []inventory.Vehicle, now time.Time) RevalidationResult {
    // 1. Freshness check
    isFresh, status := p.InvCache.IsFresh(now)
    if !isFresh {
        return RevalidationResult{
            Approved:         false,
            MustEscalate:     true,
            EscalationReason: string(status),
        }
    }

    // 2. If vehicles are referenced, verify availability
    for _, v := range vehicles {
        freshV, ok := p.InvCache.GetVehicle(v.ID)
        if !ok {
            return RevalidationResult{
                Approved:         false,
                MustEscalate:     true,
                EscalationReason: "vehicle_not_in_inventory",
            }
        }
        if freshV.Status != "available" {
            // If the reply is not already the official sold notice, block and escalate
            if !strings.Contains(reply, "ya fue vendido/separado") {
                return RevalidationResult{
                    Approved:         false,
                    MustEscalate:     true,
                    EscalationReason: "vehicle_sold_or_reserved",
                }
            }
        }
    }

    return RevalidationResult{Approved: true}
}
```

#### 4. Validating `SendStatus` and Sealing Idempotency & CRM
```go
// Inside ProcessConversation right before and after ch.Reply:
// 1. Revalidate Pre-Send
revRes := p.RevalidatePreSend(ctx, valRes.SanitizedReply, relevantVehicles, now)
if !revRes.Approved {
    p.EscQueue.Enqueue(ch.ID(), conv.ID, revRes.EscalationReason, valRes.SanitizedReply, now)
    outcome.Escalated = true
    outcome.EscalationReason = revRes.EscalationReason
    return outcome, nil
}

// 2. Dispatch Reply
sendRes, err := ch.Reply(ctx, sess, conv, msg, p.Engine.cfg.OperatingMode)
if err != nil || sendRes.Status == channel.SendFailed {
    p.EscQueue.Enqueue(ch.ID(), conv.ID, "send_failed", ..., now)
    outcome.Escalated = true
    outcome.EscalationReason = "send_failed"
    return outcome, err
}
if p.Engine.cfg.OperatingMode == channel.ModeAutopilot && sendRes.Status != channel.SendVerified {
    p.EscQueue.Enqueue(ch.ID(), conv.ID, "send_not_verified", ..., now)
    outcome.Escalated = true
    outcome.EscalationReason = "send_not_verified"
    return outcome, nil
}

// 3. Register CRM Task & Seal Idempotency Record
taskID, _ := p.CRM.RecordTask(...)
_ = p.Engine.store.MarkProcessed(store.IdempotencyRecord{
    Hash:       hash,
    ChannelID:  ch.ID(),
    ConvID:     conv.ID,
    ReplyText:  valRes.SanitizedReply,
    SendStatus: sendRes.Status,
    ETag:       p.InvCache.ETag(),
    CRMTaskID:  taskID,
    CreatedAt:  now,
})
_ = p.Engine.store.SaveCheckpointState(conv.ID, "cp7_crm:ok")
```

---

## 6. Conclusion

1. **Architecture is solid**: The current 7-checkpoint structure, thread-safety, fast-path engine, and deterministic guardrails provide a clean, robust foundation.
2. **Gaps for R1 are clear and localized**:
   - Lack of pre-send freshness & vehicle availability revalidator.
   - Missing verification of `sendRes.Status` (`sent_verified` vs `send_failed`).
   - Absence of an `internal/crm` package for task tracking and sealing.
   - Incomplete `IdempotencyRecord` metadata.
3. **Zero Risk of Regressions**: By encapsulating the pre-send revalidation and adding `internal/crm` as a dedicated clean module, existing tests (`golden64_test.go`, `draft_batch_exerciser_test.go`) can be preserved and extended with complete regression coverage.
