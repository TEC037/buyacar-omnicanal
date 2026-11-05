# Task Assignment — Worker M3: Selector Resilience & Automatic Channel Pause (R3)

**Role**: teamwork_preview_worker
**Working Directory**: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\worker_m3_1
**Authoritative Request**: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\ORIGINAL_REQUEST.md
**Project Plan**: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\PROJECT.md
**Design Reference**: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\explorer_survey_channel_1\analysis.md

## Exclusive File Ownership
You exclusively own and may edit or create:
- `app_build/internal/channel/resilience.go`
- `app_build/internal/channel/resilience_test.go`
- `app_build/internal/channel/email_pilot.go`
- `app_build/internal/channel/stub.go`
- `app_build/internal/channel/channel.go`
- `app_build/internal/core/pipeline.go` (to invoke RecordSuccess / RecordFailure on channel resilience tracker)

## Requirements & Implementation Steps
1. Implement in `app_build/internal/channel/resilience.go`:
   - `VersionedSelector` struct (`ID`, `Version`, `Primary`, `Fallbacks`, `ActiveIndex`) and selector resolution methods (`GetActiveSelector()`, `FailoverToNext()`, `Reset()`).
   - `ResilienceTracker` interface and thread-safe implementation (`sync.RWMutex`):
     - `RecordSuccess(channelID string)`: resets consecutive failures counter to 0.
     - `RecordFailure(channelID string, reason string, err error, now time.Time) (paused bool, consecutiveCount int)`: increments failure counter.
     - Strictly on the 3rd consecutive failure (`consecutiveFailures == 3`), enters paused state (`paused = true`) and immediately enqueues an emergency human escalation ticket in `escalate.Queue` (with reason `"channel_auto_paused_consecutive_failures"`).
     - Suppress alert storms: subsequent failures while already paused do not flood the escalation queue.
     - `IsPaused(channelID string) bool` and `SetPaused(channelID string, paused bool)`.
     - Strict per-channel fault isolation: pausing one channel has zero effect on any other channel.
2. Integrate with Channel Adapters & Pipeline:
   - Wire `ResilienceTracker` into `EmailPilotChannel`, `StubChannel`, and `core.Pipeline`.
   - In `pipeline.go`: when `ch.Reply()` succeeds, call `RecordSuccess()`; when `ch.Reply()` or `ch.Read()` fails, call `RecordFailure()`.
   - If channel `IsPaused()`, refuse outbound dispatch and escalate appropriately.
3. Unit & Integration Testing in `app_build/internal/channel/resilience_test.go`:
   - Test versioned selector primary and fallback cascading.
   - Test consecutive failure counter (1st, 2nd, 3rd triggers pause + escalation).
   - Test success reset (failures 1, 2, success -> counter resets to 0).
   - Test alert storm suppression on 4th and 5th failures.
   - Test per-channel fault isolation (channel A paused, channel B unaffected).
   - Concurrency stress test with 40 parallel goroutines.
4. Validation across all suites:
   - `cd app_build; go test -v ./internal/channel/...`
   - `cd app_build; go test -v ./internal/core/...`
   - `cd app_build; go test -v ./test/e2e/...` (specifically all Tier 3 resilience tests)
   - `cd app_build; go test ./...` (100% green across entire project)


## 2026-10-29T03:38:10Z
You are Worker M3 (Implementation of R3 - Selector Resilience and Automatic Pause) for BuyaCAR Omnicanal.
Your working directory is: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\worker_m3_1
Read your DISPATCH.md, c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\ORIGINAL_REQUEST.md, c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\PROJECT.md, and the design specification in .agents/teamwork/explorer_survey_channel_1/analysis.md.
Implement Selector Resilience and Auto-Pause in app_build/:
1. Create internal/channel/resilience.go: VersionedSelector with fallback cascading, and thread-safe ResilienceTracker enforcing auto-pause strictly after 3 consecutive failures, human alert escalation via escalate.Queue, reset to 0 on success, alert storm suppression, and per-channel fault isolation.
2. Wire ResilienceTracker into EmailPilotChannel, StubChannel, and pipeline.go (record success on verified send, record failure on error).
3. Create comprehensive tests in internal/channel/resilience_test.go.
4. Verify with `go test -v ./internal/channel/...`, `go test -v ./internal/core/...`, `go test -v ./test/e2e/...`, and `go test ./...` in app_build/ (100% green across entire project).
Deliver handoff.md in your working directory and notify caller via send_message.

