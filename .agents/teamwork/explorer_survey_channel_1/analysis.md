# Survey & Gap Analysis Report: Channels, Rate Limiting & Selector Resilience (R2 & R3)

**Author**: Explorer Subagent (`explorer_survey_channel_1`)  
**Target Milestone**: Pre-requisites R2 and R3 (Etapa C AUTOPILOT)  
**Workspace**: `c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace`  
**Date**: 2026-10-29  

---

## 1. Executive Summary

This investigation surveys the existing Go codebase in `app_build/` to evaluate readiness and identify technical gaps for:
1. **R2**: Rate controller with strictly max 120 msgs/hour, 250 msgs/day, pauses of 3,200 ms between conversations, 2,500 ms between channels with ±30% deterministic jitter, without race conditions or blocking the main scheduler.
2. **R3**: Selector resilience with automatic pause after 3 consecutive failures, human escalation alert, and per-channel isolation.

### Key Findings
- **Baseline Health**: The existing suite `go test ./...` passes 100% in green (64 golden cases, 100 DRAFT batch conversations, Fast-Path p99 < 5 ms).
- **Current Rate Limiting**: Only nominal fields exist (`Limits.MaxPerHour = 120`, `Limits.MaxPerDay = 250`, `Config.MaxMessagesPerHour = 120`). **Zero rate limiting logic, zero quota tracking, and zero pause/jitter mechanisms are implemented.** The scheduler loop in `cmd/buyacar/main.go` runs synchronously and without delays.
- **Current Error Handling & Resilience**: `EmailPilotChannel` defines `MaxConsecutiveEmailFailures = 3` and has an internal `RecordFailure()` method, but **it is never called by the pipeline**, has no `RecordSuccess()` reset, does not notify human operators when pausing, and has no selector resilience or versioning abstraction.
- **Channel Isolation**: Channel instances are stored in maps, but there is no centralized channel manager, health tracker, or per-channel execution isolation.

---

## 2. Current Architecture Mapping

### 2.1 Channel Interface & Data Model (`internal/channel/channel.go`)
The canonical `Channel` interface (lines 82–90) defines:
```go
type Channel interface {
    ID() string
    Limits() Limits
    Open(ctx context.Context, link Link) (Session, error)
    Pending(ctx context.Context, session Session) ([]Conversation, error)
    Read(ctx context.Context, session Session, conversation Conversation) (Thread, error)
    Reply(ctx context.Context, session Session, conversation Conversation, message Message, mode Mode) (SendResult, error)
}
```
Relevant data structures:
- `Limits` (`channel.go:18-22`):
  ```go
  type Limits struct {
      MaxCharacterLength int
      MaxPerHour         int
      MaxPerDay          int
  }
  ```
- `SendStatus` (`channel.go:66-73`):
  `SendSimulated = "simulated_no_op"`, `SendDrafted = "draft_recorded"`, `SendVerified = "sent_verified"`, `SendFailed = "send_failed"`.

### 2.2 Channel Adapter Implementations
1. **`StubChannel` (`internal/channel/stub.go`)**:
   - In-memory mock adapter used for SIMULATION and unit testing.
   - Initialized with `Limits{MaxPerHour: 120, MaxPerDay: 250}` (`stub.go:26-27`).
   - Does not implement failure tracking, pausing, or rate limits.
2. **`EmailPilotChannel` (`internal/channel/email_pilot.go`)**:
   - Implements Pilot Email Channel (Step 6 of `instructivo.md`).
   - Restricts `Reply`: blocks `ModeAutopilot` with `ErrEmailAutopilotBlocked` (`email_pilot.go:214`).
   - Contains prototype failure tracking (`email_pilot.go:89-105`):
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
   - **Crucial limitation**: Neither `Pipeline` nor `main.go` invokes `RecordFailure()`. There is no success reset method, so any 3 cumulative errors over the lifetime would permanently pause the channel.
3. **Other Channels**:
   - WhatsApp is mocked via `StubChannel("whatsapp", 650)`.
   - Character limits are hardcoded in `internal/guard/guard.go:38-44` (`tiktok:400`, `instagram:450`, `facebook_marketplace:500`, `whatsapp:650`, `email:2000`).
   - No browser/CDP adapters exist (`internal/browser` is absent).

### 2.3 Scheduler & Concurrency Model (`cmd/buyacar/main.go`)
In `cmd/buyacar/main.go:115-132`, the dispatch loop is strictly synchronous:
```go
for _, lnk := range activeLinks {
    ch, ok := channelsByID[lnk.ChannelID]
    if !ok { continue }
    sess, err := ch.Open(ctx, lnk)
    if err != nil { continue }
    pending, _ := ch.Pending(ctx, sess)
    for _, conv := range pending {
        start := time.Now()
        out, _ := pipe.ProcessConversation(ctx, ch, sess, conv, now)
        elapsedMs := float64(time.Since(start).Microseconds()) / 1000.0
        metrics.RecordFastPathLatency(elapsedMs)
        metrics.RecordOutcome(out.UsedFastPath, out.UsedAI, out.Escalated, ...)
    }
}
```
**Concurrency Observations**:
- The execution is single-threaded and sequential.
- There is no background dispatcher or channel queue.
- If a blocking `time.Sleep` was placed inside this loop, the entire process would stall, preventing prompt response to OS signals, emergency kill switch activation, or processing other channels.

---

## 3. Gap Analysis for R2: Rate Controller & Jitter

### 3.1 Requirements for R2
1. **Quota Enforcement**:
   - Maximum 120 messages/hour.
   - Maximum 250 messages/day.
2. **Pausas Humanas con Jitter Determinista**:
   - 3,200 ms between conversations (chats).
   - 2,500 ms between channels.
   - ±30% deterministic jitter:
     - Chat delay range: `[2,240 ms, 4,160 ms]` ($3200 \times 0.70$ to $3200 \times 1.30$).
     - Channel delay range: `[1,750 ms, 3,250 ms]` ($2500 \times 0.70$ to $2500 \times 1.30$).
3. **Non-blocking Architecture**:
   - Must not block the main scheduler or pipeline thread.
   - Must be free of race conditions (`0 condiciones de carrera`).
4. **Deterministic Testing**:
   - Must accept virtual/mock time (`time.Time`) so existing tests (64 golden cases and 100 batch conversations) do not stall or become flaky.

### 3.2 Identified Gaps for R2
| Requirement Component | Current State in Codebase | Identified Gap | Severity |
|---|---|---|---|
| Hourly Quota (120/hr) | `Limits.MaxPerHour = 120` in struct definition only | No windowing, counting, or blocking logic exists. | High |
| Daily Quota (250/day) | `Limits.MaxPerDay = 250` in struct definition only | No daily tracking or reset mechanism exists. | High |
| Inter-chat Pause (3,200 ms) | Completely absent; 0 ms delay in all loops | No pause calculation or scheduling logic. | High |
| Inter-channel Pause (2,500 ms) | Completely absent; 0 ms delay between channels | No channel-level throttling. | High |
| Deterministic Jitter (±30%) | Mentioned in docs; zero implementations | No deterministic algorithm or PRNG with reproducible seed. | High |
| Non-blocking Execution | Synchronous nested loops in `main.go` | Any synchronous sleep would block the daemon and kill switch. | Critical |

---

## 4. Gap Analysis for R3: Selector Resilience & Auto-Pause

### 4.1 Requirements for R3
1. **Selector Breakage & Anomaly Detection**:
   - Detect failures in channel adapters (DOM/CSS selector missing, payload rejection, network/API failure).
2. **Automatic Immediate Pause**:
   - Automatically pause the affected channel after **3 consecutive failures**.
3. **Human Escalation & Alert**:
   - Immediately raise an alert / enqueue a ticket in `escalate.Queue` upon auto-pause.
4. **Per-Channel Isolation**:
   - Pausing one channel must NOT disrupt or block other operational channels.

### 4.2 Identified Gaps for R3
| Requirement Component | Current State in Codebase | Identified Gap | Severity |
|---|---|---|---|
| Selector Abstraction | None; no selector types exist | No `VersionedSelector` or selector registry with fallback chains. | High |
| Consecutive Failure Counter | Only in `EmailPilotChannel` (disconnected) | `RecordFailure()` is not called by `Pipeline`. `StubChannel` lacks it completely. | High |
| Reset on Success | Missing in all adapters | Consecutive failure counter never resets to 0 upon success. | Medium |
| Automatic Alert on Pause | `EmailPilotChannel` sets `paused = true` silently | No ticket is enqueued in `escalate.Queue` when channel enters paused state. | High |
| Channel Isolation | Loops over links sequentially in `main.go` | If an unhandled error occurs, there is no formal channel manager isolating state. | Medium |

---

## 5. Proposed Modular Design & Signatures

To implement R2 and R3 cleanly in standard Go without breaking existing pipeline behavior, we recommend creating a dedicated resilience and rate package (`internal/channel/rate` or within `internal/channel`):

### 5.1 R2: `RateController` Specification
```go
package channel

import (
    "context"
    "sync"
    "time"
)

// RateConfig defines limits and delay parameters.
type RateConfig struct {
    MaxPerHour             int           // 120
    MaxPerDay              int           // 250
    DelayBetweenChats      time.Duration // 3200ms
    DelayBetweenChannels   time.Duration // 2500ms
    JitterPercent          float64       // 0.30
}

// WindowTracker tracks rolling hourly and daily quotas per channel.
type WindowTracker struct {
    mu           sync.Mutex
    hourlyEvents []time.Time
    dailyEvents  []time.Time
    lastChatSent time.Time
}

// RateController controls sending rates, pauses, and deterministic jitter.
type RateController struct {
    mu              sync.RWMutex
    cfg             RateConfig
    channelTrackers map[string]*WindowTracker
    lastChannelSent time.Time
}

// CalculateDeterministicJitter calculates ±jitterPercent deterministically based on seed/hash.
func CalculateDeterministicJitter(base time.Duration, jitterPercent float64, seedKey string, seq uint64) time.Duration

// CheckRateLimit checks if sending is permitted at time `now`. Returns wait time if quota reached.
func (rc *RateController) CheckRateLimit(channelID string, now time.Time) (allowed bool, waitDuration time.Duration, reason string)

// RecordSend commits a successful send event to the quota windows.
func (rc *RateController) RecordSend(channelID string, now time.Time)

// NextChatDelay returns the deterministic pause before the next conversation.
func (rc *RateController) NextChatDelay(channelID, convID string, seq uint64) time.Duration

// NextChannelDelay returns the deterministic pause before switching channels.
func (rc *RateController) NextChannelDelay(channelID string, seq uint64) time.Duration
```

### 5.2 R3: Selector Resilience & Auto-Pause Specification
```go
package channel

import (
    "sync"
    "time"
    "buyacar.com.co/omnicanal/internal/escalate"
)

// VersionedSelector supports primary and fallback selectors for resilience.
type VersionedSelector struct {
    ID        string   // e.g. "chat_input_box", "send_button"
    Version   string   // e.g. "v1.2"
    ChannelID string   // e.g. "whatsapp", "email"
    Primary   string   // primary CSS/DOM/XPath or payload selector
    Fallbacks []string // candidate fallbacks if primary breaks
}

// ChannelHealthStatus represents channel availability.
type ChannelHealthStatus string

const (
    StatusHealthy ChannelHealthStatus = "healthy"
    StatusPaused  ChannelHealthStatus = "paused"
)

// ResilienceTracker monitors consecutive failures, auto-pause, and human alert escalation.
type ResilienceTracker struct {
    mu                  sync.RWMutex
    channelID           string
    consecutiveFailures int
    maxFailures         int // strictly 3
    status              ChannelHealthStatus
    lastError           string
    pausedAt            time.Time
    escQueue            *escalate.Queue
}

// RecordSuccess resets consecutive failures to 0 if channel is healthy.
func (rt *ResilienceTracker) RecordSuccess()

// RecordFailure increments failures. If count reaches 3, transitions to StatusPaused
// and immediately enqueues a human alert in escalate.Queue.
func (rt *ResilienceTracker) RecordFailure(convID, reason string, err error, now time.Time) bool

// IsPaused returns true if channel is currently paused.
func (rt *ResilienceTracker) IsPaused() bool

// Resume manually resets pause state after human inspection.
func (rt *ResilienceTracker) Resume(advisorID string)
```

---

## 6. Non-Regression & Concurrency Protection Strategy

1. **Virtual Time Injection**:
   - `RateController.CheckRateLimit` and `RecordSend` must accept `now time.Time`.
   - In unit tests (such as `TestGoldenSuite_64Cases` and `TestDraftExitGate_100BatchExerciser`), test code passes virtual timestamps `now.Add(...)` without calling `time.Sleep`.
   - This keeps `go test ./...` execution time under 1 second.
2. **Scheduler Non-Blocking Guarantee**:
   - In the live daemon, rather than a monolithic blocking sleep in the outer loop, a worker or timer-driven queue (`select { case <-ctx.Done(): return; case <-timer.C: ... }`) ensures that the daemon remains responsive to:
     - Emergency Kill Switch (`< 1s` shutdown).
     - Inbound HTTP / Webhook events.
     - Active channels when one channel is paused or sleeping.
3. **Per-Channel Isolation Guarantee**:
   - Each channel holds its own `WindowTracker` and `ResilienceTracker`.
   - If channel `email` fails 3 times and is paused, channel `whatsapp` proceeds with 0 degradation.

---

## 7. Actionable Implementation Plan (For `@go-engineer`)

1. **Phase 1: R2 Rate Controller Implementation**:
   - Create `internal/channel/rate_controller.go` and `internal/channel/rate_controller_test.go`.
   - Implement rolling window quota tracker (120/hr, 250/day).
   - Implement deterministic jitter formula for 3,200 ms and 2,500 ms (±30%).
   - Verify with 0 race conditions via unit tests.
2. **Phase 2: R3 Resilience & Auto-Pause Implementation**:
   - Create `internal/channel/resilience.go` and `internal/channel/resilience_test.go`.
   - Implement `ResilienceTracker` with strictly 3 consecutive failures threshold.
   - Wire auto-pause directly to `escalate.Queue.Enqueue`.
   - Implement `VersionedSelector` struct with primary and fallback candidate resolution.
3. **Phase 3: Integration & Non-Regression**:
   - Connect `Pipeline.ProcessConversation` to update `ResilienceTracker` (RecordSuccess on ok, RecordFailure on error).
   - Update `cmd/buyacar/main.go` to use `RateController` and verify per-channel isolation.
   - Run full verification: `go test -v ./...`, benchmark Fast-Path p99 (< 5 ms).
