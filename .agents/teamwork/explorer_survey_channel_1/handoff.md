# Handoff Report: Survey of Channels, Rate Limiting & Selector Resilience (R2 & R3)

**Author**: Explorer Subagent (`explorer_survey_channel_1`)  
**Recipient**: Parent / Lead Orchestrator (`41bf3eb6-8d36-4b70-8f73-4cb69f505710`)  
**Workspace**: `c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\explorer_survey_channel_1`  
**Date**: 2026-10-29  
**Handoff Type**: Hard (Task Complete)  

---

## 1. Observation

1. **Test Suite Baseline**:
   - Tool Command: `cd app_build; go test ./...`
   - Result:
     ```
     ok  	buyacar.com.co/omnicanal/internal/ai	(cached)
     ok  	buyacar.com.co/omnicanal/internal/channel	(cached)
     ok  	buyacar.com.co/omnicanal/internal/core	(cached)
     ok  	buyacar.com.co/omnicanal/internal/fast	(cached)
     ok  	buyacar.com.co/omnicanal/internal/guard	(cached)
     ok  	buyacar.com.co/omnicanal/internal/inventory	(cached)
     ok  	buyacar.com.co/omnicanal/internal/obs	(cached)
     ```
   - Race Detector: Tool command `cd app_build; go test -race ./...` returned:
     `go: -race requires cgo; enable cgo by setting CGO_ENABLED=1`. Command `where gcc; go env CGO_ENABLED` verified that no GCC is present on the Windows host and `CGO_ENABLED=0`.

2. **Existing Channel Definitions**:
   - `internal/channel/channel.go:82-90`: Interface `Channel` contains `ID()`, `Limits()`, `Open()`, `Pending()`, `Read()`, `Reply()`.
   - `internal/channel/channel.go:18-22`: `Limits` defines `MaxCharacterLength int`, `MaxPerHour int`, `MaxPerDay int`.
   - `internal/channel/stub.go:24-28`: `NewStubChannel` sets `MaxPerHour: 120, MaxPerDay: 250`, but these limits are never checked or decremented.
   - `internal/channel/email_pilot.go:56-60`: `NewEmailPilotChannel` sets `MaxPerHour: 120, MaxPerDay: 250`, never referenced in `Reply`.

3. **Absence of Rate Limiting, Sleep, and Jitter in Core**:
   - Search across `app_build/`: Zero occurrences of `time.Sleep`, zero occurrences of `delayBetweenChatsMs`, `delayBetweenChannelsMs`, or `jitterPercent`.
   - `cmd/buyacar/main.go:115-132`: The channel dispatch loop is a tight, synchronous nested loop iterating over `activeLinks` and `pending` conversations with zero pauses between conversations or channels.
   - `internal/core/golden64_test.go:129-149` and `internal/core/draft_batch_exerciser_test.go:69-94`: Test suites execute 64 and 100 conversations sequentially within milliseconds; introducing a synchronous real-time sleep (e.g. 3,200 ms) inside `ProcessConversation` would cause tests to take 204.8s and 320s respectively.

4. **Status of Failure Tracking and Resilience**:
   - `internal/channel/email_pilot.go:18-19`: `MaxConsecutiveEmailFailures = 3`.
   - `internal/channel/email_pilot.go:89-98`:
     ```go
     func (e *EmailPilotChannel) RecordFailure() bool {
         e.mu.Lock()
         defer e.mu.Unlock()
         e.consecutiveFailures++
         if e.consecutiveFailures >= MaxConsecutiveEmailFailures {
             e.paused = true
         }
         return e.paused
     }
     ```
   - In `internal/core/pipeline.go:147-152`:
     ```go
     sendRes, err := ch.Reply(ctx, sess, conv, ...)
     if err != nil {
         p.EscQueue.Enqueue(ch.ID(), conv.ID, "send_failed", err.Error(), now)
         outcome.Escalated = true
         outcome.EscalationReason = "send_failed"
         return outcome, err
     }
     ```
     `p.EscQueue.Enqueue` is called, but `RecordFailure()` on the channel is NEVER invoked.
   - There is no `RecordSuccess()` method in `EmailPilotChannel` or `StubChannel` to reset the consecutive failures counter.
   - When `e.paused` becomes `true`, `EmailPilotChannel` does NOT notify or enqueue a ticket for human alert.
   - There is no selector abstraction (`VersionedSelector` or DOM selector candidate registry) anywhere in `app_build/`.

---

## 2. Logic Chain

1. **From Observation 2 & 3 to R2 Gap Assessment**:
   - Because `Limits.MaxPerHour` and `Limits.MaxPerDay` exist only as struct fields without any sliding window or bucket counting logic, and because there are no inter-chat or inter-channel delays, the system currently allows unlimited burst transmissions if conversations are available.
   - Therefore, a dedicated `RateController` is required to track rolling hourly (120/hr) and daily (250/day) windows.
   - Because `cmd/buyacar/main.go` runs synchronously, any naive `time.Sleep` would block the process, degrading responsiveness to kill-switch signals and stalling all channels. Thus, the rate controller must be non-blocking (e.g. calculating reservations / delays, or using timer channels with context cancellation).
   - Because test suites (64 golden cases and 100 batch conversations) run synchronously, the rate controller must decouple time calculation by accepting `now time.Time`, ensuring tests can verify quota limits and deterministic jitter formulas without wall-clock sleep.

2. **From Observation 4 to R3 Gap Assessment**:
   - `EmailPilotChannel` has a prototype `RecordFailure()` method, but it is orphaned: `Pipeline` does not call it, and errors during `ch.Reply` or `ch.Read` do not affect the failure counter.
   - Because there is no `RecordSuccess()`, intermittent, non-consecutive errors would eventually trigger a false-positive channel pause.
   - Because no human alert is triggered when the channel pauses, operators in Tunja would not be alerted to broken channels.
   - Because no selector representation exists, DOM or API selector versioning and fallback resolution cannot be tested or evaluated.
   - Therefore, a generalized `ResilienceTracker` and `VersionedSelector` abstraction must be added to `internal/channel`, integrated with `escalate.Queue` for human alerts, and hooked into `Pipeline.ProcessConversation`.

---

## 3. Caveats

- **Host Compiler Constraint**: CGO is disabled (`CGO_ENABLED=0`) and GCC is not installed on this Windows environment. As a result, `go test -race ./...` cannot run natively without installing a MinGW/GCC toolchain. Race safety must therefore be verified by strict code inspection and standard concurrency patterns (`sync.RWMutex`, atomic types).
- **Scope Limit**: As an Explorer subagent, no modifications were made to `app_build/` or project configuration files; all proposals and architectural specs are documented in `.agents/teamwork/explorer_survey_channel_1/analysis.md`.
- **Browser/CDP Scope**: External browser automation (`internal/browser`) is not yet implemented in `app_build/`. The selector resilience component for R3 should therefore be designed to validate both current API/email adapters and future browser automation adapters.

---

## 4. Conclusion

1. **R2 (Rate Controller & Jitter)** requires:
   - A thread-safe `RateController` in `internal/channel` enforcing max 120 msgs/hr and max 250 msgs/day per channel.
   - Deterministic jitter calculation for 3,200 ms (chats) and 2,500 ms (channels) within the exact ±30% range (`[2240ms, 4160ms]` and `[1750ms, 3250ms]`).
   - Virtual time parameterization so test suites verify quotas instantly without sleeping.
2. **R3 (Selector Resilience & Auto-Pause)** requires:
   - A `ResilienceTracker` in `internal/channel` enforcing the strict threshold of **3 consecutive failures**.
   - Automatic escalation to `escalate.Queue` with reason `"channel_auto_paused_consecutive_failures"` upon entering paused state.
   - Automatic counter reset to 0 upon any successful conversation processing (`RecordSuccess`).
   - `VersionedSelector` struct supporting primary and fallback selectors.
   - Per-channel isolation ensuring a paused channel never degrades or blocks other registered channels.

All specific Go structs, function signatures, and implementation blueprints have been written to:
`c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\explorer_survey_channel_1\analysis.md`

---

## 5. Verification Method

To independently reproduce and verify the findings of this survey:

1. **Check Test Suite Baseline**:
   ```powershell
   cd c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\app_build
   go test -v ./...
   ```
   *Expected*: All packages (`ai`, `channel`, `core`, `fast`, `guard`, `inventory`, `obs`) pass in green.

2. **Verify Absence of Rate Limiting and Sleep Logic**:
   ```powershell
   rg "time\.Sleep" app_build/
   rg "delayBetween" app_build/
   ```
   *Expected*: Zero matches returned in `app_build/`.

3. **Verify Disconnected Failure Tracker in Email Pilot**:
   - Inspect `app_build/internal/channel/email_pilot.go` lines 89–98.
   - Inspect `app_build/internal/core/pipeline.go` lines 147–152.
   *Expected*: Notice that `RecordFailure` is never called when `ch.Reply` fails.

4. **Review Technical Architecture & Proposed Signatures**:
   - View `c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\explorer_survey_channel_1\analysis.md`.
