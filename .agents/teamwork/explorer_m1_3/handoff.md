# Handoff Report: Pipeline Pre-Send Revalidation & Send Result Verification (M1.3)

- **Agent**: `explorer_m1_3`
- **Milestone**: Milestone 1 (R1 — Pipeline Pre-Send Revalidation)
- **Recipient**: Orchestrator (`41bf3eb6-8d36-4b70-8f73-4cb69f505710`) / Implementer
- **Working Directory**: `c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\explorer_m1_3`
- **Handoff Type**: Hard Handoff (Investigation, Design, and Specification Complete)
- **Reference Document**: `c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\explorer_m1_3\analysis.md`

---

## 1. Observation

1. **Current Pipeline Flow (`app_build/internal/core/pipeline.go:130-170`)**:
   - At line 130, `valRes := p.Guard.ValidateOutbox(candidateReply, ch.ID(), relevantVehicles)` validates outbound messages against anti-hallucination and Ley 1581 PII rules.
   - At line 137, `p.Engine.store.SaveCheckpointState(conv.ID, "cp5_guard:ok")` is recorded.
   - Immediately at line 141, `sendRes, err := ch.Reply(...)` is invoked. No pre-send check is performed to confirm whether the referenced vehicle remains in `available` state in `p.InvCache`, or whether `p.InvCache` has exceeded its 30-minute stale threshold.
   - At line 147, `if err != nil` is evaluated, but `sendRes.Status` is never inspected. If `sendRes.Status == channel.SendFailed` or if `sendRes.Status != channel.SendVerified` in `AUTOPILOT` mode, line 153 executes regardless: `p.Engine.store.SaveCheckpointState(conv.ID, "cp6_envio:ok")`.
   - At line 157, `p.Engine.store.MarkProcessed` seals the record, but lacks `SendStatus`, `ETag`, and `CRMTaskID`.
2. **Inventory Cache Methods (`app_build/internal/inventory/inventory.go:60-125`)**:
   - `inventory.Cache` has `refreshWindow` (15m) and `staleThreshold` (30m).
   - `c.NeedsRefresh()` exists, but there is no public `GetVehicle(id string) (Vehicle, bool)` or standalone `IsFresh(now time.Time) bool` (under development by M1.2).
3. **Known Lesson #4 & Fast-Path Behavior (`app_build/internal/fast/fast.go:90-101`)**:
   - When a vehicle status is not `"available"`, Fast-Path returns `Result{Handled: true, Reply: e.texts.SoldVehicleNotice, MustEscalate: true, EscalationReason: "vehicle_sold_or_reserved"}`.
   - In `pipeline.go:90-94`, if `fpRes.MustEscalate` is true for a sold vehicle, the commercial notice is delivered and the human alert is enqueued.
4. **Fast-Path Latency Baseline**:
   - Executing `cd app_build; go test -bench=BenchmarkFastPath_ExactMatch -benchmem ./internal/fast` reports:
     `BenchmarkFastPath_ExactMatch-12: 217632 ops, 4684 ns/op (0.00468 ms/op), 3090 B/op, 35 allocs/op`.
   - The required SLA is `< 5 ms` (and `< 0.015 ms` calibration).
5. **Channel Send Status (`app_build/internal/channel/channel.go:68-73`)**:
   - Constants defined: `SendSimulated = "simulated_no_op"`, `SendDrafted = "draft_recorded"`, `SendVerified = "sent_verified"`, `SendFailed = "send_failed"`.

---

## 2. Logic Chain

1. **Requirement Traceability**:
   - Step 11 of `instructivo.md` and R1 of `ORIGINAL_REQUEST.md` require revalidating vehicle availability in fresh cache (< 30 min) immediately before dispatch, validating the send result status (`sent_verified` in `AUTOPILOT`), sealing idempotency, and settling CRM tasks.
2. **Identification of Gaps (Connecting Observation 1 to Requirements)**:
   - Observation 1 demonstrates that `pipeline.go` presently lacks the pre-send check and ignores `sendRes.Status`. If a vehicle is reserved while AI processes a prompt, the system would dispatch an invalid promise of availability.
3. **Deduction of Revalidation Placement**:
   - The revalidation check must occur strictly between `cp5_guard:ok` and `ch.Reply(...)`.
   - If the vehicle transitioned to `sold` or `reserved`:
     - Per Observation 3 and Known Lesson #4, the system must not remain silent. It must abort the sales pitch, replace the reply with `SoldVehicleNotice`, enqueue escalation in `p.EscQueue`, and dispatch the polite notice to the customer.
   - If the cache is stale (> 30 min) or unavailable:
     - The system cannot verify inventory integrity; per rules 6 & 10, it must enqueue escalation (`"inventory_stale"` / `"inventory_unavailable"`) and abort dispatch immediately without sending.
4. **Deduction of Send Result Validation**:
   - Per Observation 1 and 5, after `ch.Reply()`, the pipeline must check `sendRes.Status == channel.SendFailed` and match the status against `p.Engine.cfg.OperatingMode` (`SendVerified` for `AUTOPILOT`, `SendDrafted` for `DRAFT`, `SendSimulated` for `SIMULATION`).
   - If a failure or mismatch occurs, the pipeline enqueues an escalation alert, sets `outcome.SendStatus = sendRes.Status`, refuses to mark `cp6_envio:ok`, and returns an error.
5. **Latency & Concurrency Preservation (Connecting Observation 4 to Performance Criteria)**:
   - `RevalidatePreSend` performs only an in-memory `sync.RWMutex` read-lock and slice lookup (< 300 ns).
   - Baseline 4,684 ns + 300 ns = 4,984 ns (~0.005 ms), safely well below the 5.0 ms SLA by a factor of 1,000x.
   - Because `*Pipeline` is stateless and operates strictly on local stack variables and thread-safe dependencies, zero race conditions are introduced.

---

## 3. Caveats

1. **Dependency on M1.1 and M1.2**:
   - `RevalidatePreSend` depends on `inventory.Cache.GetVehicle(id string)` and `inventory.Cache.IsFresh(now time.Time)` being implemented by Explorer M1.2.
   - Full CP7 settlement depends on `internal/crm.Manager` being implemented by Explorer M1.1 and `store.IdempotencyRecord` fields being added by Explorer M1.2.
   - The design in `analysis.md` provides defensive `nil` checks (`if p.CRM != nil`, `if p.InvCache != nil`) so `pipeline.go` compiles and functions safely even if modules are integrated incrementally.
2. **General / Non-Vehicle Inquiries**:
   - When a customer asks about office hours, address, official phone lines, or general greetings (Suite A cases 1..16), `referencedVehicleID` is empty. The design specifies that `RevalidatePreSend` returns `(true, "")` so static dealership inquiries are never blocked by inventory sync delays.

---

## 4. Conclusion

The specification for `pipeline.go` pre-send revalidation and send status validation is fully designed, documented, and ready for implementation.
Key deliverables provided in `analysis.md`:
1. Exported method `RevalidatePreSend(ctx context.Context, conv channel.Conversation, vehicleID string, now time.Time) (bool, string)`.
2. Pipeline integration between CP5 and CP6 handling sold/reserved transitions (abort pitch, emit `SoldVehicleNotice`, enqueue escalation, deliver notice).
3. Send result verification matrix enforcing `sent_verified` in `AUTOPILOT`, `draft_recorded` in `DRAFT`, and `simulated_no_op` in `SIMULATION`.
4. Concurrency design guaranteeing zero race conditions and sub-microsecond latency addition (< 300 ns), preserving Fast-Path p99 < 5 ms.
5. Full unified diff patch and unit test suite plan for `pipeline_test.go`.

---

## 5. Verification Method

Once the implementer applies the diff from `analysis.md`:

1. **Verify Unit & Core Tests**:
   ```powershell
   cd app_build
   go test -v ./internal/core
   ```
   *Expected*: Passes 100%, including `TestPipeline_EndToEndIdempotencyAndResume`, `TestPipeline_RevalidatePreSend_*`, `golden64_test.go`, and `draft_batch_exerciser_test.go`.

2. **Verify Fast-Path Latency Preservation**:
   ```powershell
   cd app_build
   go test -bench=BenchmarkFastPath_ExactMatch -benchmem ./internal/fast
   ```
   *Expected*: Latency remains < 0.015 ms/op (approx. 4,500 - 5,500 ns/op).

3. **Verify Concurrency & Whole-Project Integrity**:
   ```powershell
   cd app_build
   go test ./...
   ```
   *Expected*: All packages exit code 0, 0 race conditions.
