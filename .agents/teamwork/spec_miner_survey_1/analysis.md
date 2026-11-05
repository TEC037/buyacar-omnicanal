# SPECIFICATION SURVEY REPORT: R1, R2, R3 FOR AUTOPILOT TRANSITION

- **Author**: Specification Investigator / Spec Miner (`teamwork_preview_spec_miner`)
- **Workspace**: `buyacar-omnicanal-agent-workspace`
- **Working Directory**: `.agents/teamwork/spec_miner_survey_1`
- **Date**: 2026-10-29T02:40:00Z
- **Status**: Comprehensive Analysis Completed

---

## 1. Executive Summary & Authoritative Specification Hierarchy

This document details the exhaustive technical requirements, mathematical bounds, state transitions, interface contracts, error modes, and edge cases for the three mandatory technical prerequisites required for transitioning **BuyaCAR Omnicanal** towards controlled autonomous operation (`AUTOPILOT`).

### Authoritative Document Hierarchy
1. `instructivo.md`: Master transition guide (`SIMULATION` -> `DRAFT` -> `AUTOPILOT`), specifically Stage C ("Transición al Modo AUTOPILOT") and Step 11 of the 12-step conversation lifecycle.
2. `.agents/prompts/BUYACAR_FINAL_INTEGRATED.md`: Canonical system prompt and architecture specification (Sections: `<OBJETIVOS_MEDIBLES>`, `<FLUJO_POR_CONVERSACION>`, `<PARAMETROS_INICIALES>`, `<CONTRATO_IA_OPERATIVA>`, `<GUARD>`, `<LECCIONES_CONOCIDAS>`).
3. `.agents/rules/30-channels.md`: Channel governance, selector versioning, and consecutive failure pausing.
4. `docs/SPEC.md` & `docs/MVP_DOCUMENTATION.md`: Architecture map, quality gates, and data contracts.
5. `ORIGINAL_REQUEST.md`: Direct user prompt establishing prerequisites R1, R2, and R3.
6. `app_build/`: Existing reference implementation in standard Go (`buyacar.com.co/omnicanal`).

---

## 2. Deep Dive Specification: R1 — Pre-Send Revalidator & CRM Sealing

### 2.1 Context and Placement in Conversation Lifecycle
Step 11 of `instructivo.md` and `<FLUJO_POR_CONVERSACION>` defines the pre-send revalidation immediately prior to channel dispatch (`ch.Reply`):
```text
Pipeline Step 11:
1. Revalidate vehicle availability against fresh in-memory inventory cache.
2. Confirm vehicle status is "available". If transitioned to "sold" or "reserved", halt outbound sales pitch and escalate.
3. Validate channel dispatch result (must be "sent_verified" in AUTOPILOT, "draft_recorded" in DRAFT, "simulated_no_op" in SIMULATION).
4. Seal idempotency hash into persistence store.
5. Seal CRM task/record in CRM layer.
```

### 2.2 Technical Invariants & Rules for R1
- **Freshness Window Check**:
  - The cache must not be stale (`now - lastUpdated <= 30m`). If stale, revalidation returns `inventory_stale` and escalates immediately without sending unconfirmed vehicle data.
  - If cache is unavailable (`available == false`), revalidation returns `inventory_unavailable` and escalates.
- **Vehicle Status Verification**:
  - For any inquiry referencing a specific vehicle (by ID, Brand/Model/Year), query the fresh cache.
  - If status == `"available"`: revalidation succeeds.
  - If status == `"sold"` or `"reserved"`:
    - Never stay silent (Lesson #4: "Vehículos vendidos o no disponibles requieren respuesta comercial y escalamiento, nunca silencio").
    - Replace the proposed reply with `BusinessTexts.SoldVehicleNotice` ("Te contamos que el vehículo consultado ya fue vendido/separado, pero un asesor te contactará para ofrecerte opciones similares de nuestro inventario certificado.").
    - Enqueue human escalation ticket with reason `"vehicle_sold_or_reserved"`.
  - If vehicle is missing/deleted from inventory:
    - Abort dispatch, escalate with reason `"vehicle_no_longer_in_inventory"`.
- **Dispatch Verification**:
  - Execute `ch.Reply(...)`.
  - If `err != nil` or `SendResult.Status == SendFailed`:
    - Enqueue escalation ticket with reason `"send_failed"`.
    - Checkpoint CP6 marked as `"failed"`.
    - Return error; do not seal idempotency as successful message.
  - If `SendResult.Status == SendVerified` (in `AUTOPILOT`): mark CP6 `"ok"`.
- **Idempotency Sealing**:
  - SHA-256 hash: `channelID|conversationID|lastIncomingMessage`.
  - Write record to `store.Store`: `store.MarkProcessed(record)`.
- **CRM Sealing (CP7)**:
  - If AI returned `crm_update.tasks_to_create`, or if standard triage identified an action (e.g., test drive scheduling, credit assessment referral, sold vehicle follow-up), record the CRM task entry.
  - Checkpoint CP7 recorded as `"cp7_crm:ok"`.

---

## 3. Deep Dive Specification: R2 — Rate Controller with Human Pauses & Deterministic Jitter

### 3.1 Rate Limits & Sliding Windows
- **Hourly Quota**: Maximum **120 messages/hour**.
  - Sliding 3600-second window across all outbound dispatches per channel (or engine-wide).
  - Attempting message 121 within 60 minutes blocks or rejects with `ErrHourlyQuotaExceeded`.
- **Daily Quota**: Maximum **250 messages/day**.
  - Sliding 24-hour (86,400-second) window.
  - Attempting message 251 within 24 hours blocks or rejects with `ErrDailyQuotaExceeded`.

### 3.2 Human Delay Parameters & Deterministic Jitter Formula
- **Inter-Conversation Delay ($T_{\text{chat}}$)**:
  - Base delay: $3,200\text{ ms}$ (3.2 seconds).
  - Jitter range: $\pm 30\%$.
  - Minimum delay: $3,200 \times (1 - 0.30) = 2,240\text{ ms}$.
  - Maximum delay: $3,200 \times (1 + 0.30) = 4,160\text{ ms}$.
- **Inter-Channel Delay ($T_{\text{channel}}$)**:
  - Base delay: $2,500\text{ ms}$ (2.5 seconds).
  - Jitter range: $\pm 30\%$.
  - Minimum delay: $2,500 \times (1 - 0.30) = 1,750\text{ ms}$.
  - Maximum delay: $2,500 \times (1 + 0.30) = 3,250\text{ ms}$.
- **Deterministic Jitter Computation**:
  - To prevent non-deterministic test flakes and maintain repeatable simulation:
    $$\text{jitterFraction} = \frac{(\text{DeterministicHash}(\text{seed}) \pmod{6001}) - 3000}{10000.0} \in [-0.30, +0.30]$$
    $$\text{ActualDelay} = \text{round}(T_{\text{base}} \times (1.0 + \text{jitterFraction}))$$
  - The `seed` can be derived from conversation ID, message timestamp, or sequence counter. In unit tests, a fixed seed yields identical millisecond timing every execution.

### 3.3 Concurrency & Non-Blocking Scheduler Rules
- All state updates (sliding window counters, last sent timestamps) must be protected by a `sync.Mutex` or lock-free atomic constructs.
- **Context Awareness**: Any sleep or wait MUST listen to `ctx.Done()`. If the context is canceled or the Kill Switch is triggered, delay terminates immediately ($< 1\text{ ms}$), satisfying the $< 1\text{ s}$ emergency kill switch constraint.

---

## 4. Deep Dive Specification: R3 — Selector Resilience & Automatic Pause

### 4.1 DOM Selector Resilience
- **Selector Versioning**:
  - UI selectors for web-based channels (WhatsApp Web, Meta Messenger, Instagram Direct) change frequently.
  - Each interaction element must define versioned candidates:
    - Primary (`v1`)
    - Fallback (`v2`)
    - Structural/Resilient (`v3`)
  - When locating an element, try `v1` $\rightarrow$ `v2` $\rightarrow$ `v3`.
  - If all fail, the operation fails with `ErrDOMSelectorBroken`.

### 4.2 Consecutive Failure Circuit Breaker
- **Failure Counting**:
  - Maintain an atomic/synchronized `consecutiveFailures` counter per channel.
  - Any successful operation (`Open`, `Read`, `Reply`, element lookup) resets `consecutiveFailures = 0`.
  - Any fatal error or broken selector increments `consecutiveFailures++`.
- **Auto-Pause Threshold**:
  - Threshold = **3 consecutive failures**.
  - Upon reaching 3 consecutive failures:
    1. Set channel state to `PAUSED` (`paused = true`).
    2. Enqueue an urgent human escalation ticket to `internal/escalate.Queue`:
       - Reason: `"channel_paused_consecutive_failures"`
       - Context: Sanitized channel ID, failed selector key, error message.
    3. Emit telemetry / metric event: `channel_paused`.
    4. Subsequent calls to that channel immediately fail fast with `ErrChannelPaused`.

### 4.3 Channel Isolation Invariant
- Each channel adapter manages its own independent health and pause state.
- Pausing `whatsapp` does NOT affect `email`, `facebook_marketplace`, `instagram`, or `tiktok`. Other channels continue processing unhindered.
- **Manual/Administrative Resume**: Provide an administrative method `Resume()` or `Reset()` to reset `consecutiveFailures = 0` and `paused = false` once human operators resolve the issue.

---

## 5. Features Discovered

| # | Category | Feature | Description | Inputs | Outputs | Error Behavior | Discovered Via |
|---|----------|---------|-------------|--------|---------|----------------|----------------|
| 1 | R1 — Pre-Send Revalidation | Fresh Cache Validation | Verify inventory cache is fresh (< 30 min) and available prior to reply dispatch | `cache *inventory.Cache`, `now time.Time` | `bool (isFresh)` | Returns `StatusStale` or `StatusUnavailable`; aborts send and escalates to human | `instructivo.md` §4 Etapa C #3; `app_build/internal/inventory` |
| 2 | R1 — Pre-Send Revalidation | Vehicle Status Recheck | Check that referenced vehicle is still `"available"` immediately before dispatch | `vehicleID string`, `cache *inventory.Cache` | `status string ("available", "sold", "reserved")` | If `"sold"` or `"reserved"`, halts sales pitch, sends sold notice, escalates | `instructivo.md` §4 Etapa C #3; `BUYACAR_FINAL_INTEGRATED.md` #11 |
| 3 | R1 — Pre-Send Revalidation | Sold Vehicle Transition Handler | Switches response to commercial sold notice and enqueues human escalation | `candidateReply string`, `vehicle inventory.Vehicle` | `sanitizedReply string`, `mustEscalate bool` | Reason: `"vehicle_sold_or_reserved"`; ensures zero silence per Lesson #4 | `app_build/internal/fast/fast.go:91-100`; `BUYACAR_FINAL_INTEGRATED.md` |
| 4 | R1 — CRM Sealing | Outbound Status Verification | Confirms `ch.Reply` returns `sent_verified` (in AUTOPILOT) before sealing | `SendResult.Status` | `verified bool` | If `send_failed` or error, escalates with `"send_failed"`, marks CP6 failed, does not mark processed | `instructivo.md` §4 Etapa C #3; `app_build/internal/channel` |
| 5 | R1 — CRM Sealing | Idempotency Hash Sealing | Calculates SHA-256 (`channel\|conv\|lastMsg`) and stores in `store.Store` | `channelID`, `convID`, `lastIncomingText` | `hash string`, `persisted error` | If already processed, prevents duplicate sending; logs duplicate avoided | `app_build/internal/core/pipeline.go:157-163`; `store.Store` |
| 6 | R1 — CRM Sealing | CRM Task Settlement (CP7) | Records CRM follow-up task (from AI `crm_update` or standard lead workflow) | `tasks []string`, `convID string` | `checkpointStatus ("cp7_crm:ok")` | Redacts PII before saving; records status in checkpoint tracker | `BUYACAR_FINAL_INTEGRATED.md:266-268,303`; `instructivo.md` |
| 7 | R2 — Rate Control | Hourly Quota Enforcement | Limits outbound messages to 120 per sliding 3600-second window | `timestamp time.Time` | `allowed bool` | If counter >= 120, returns `ErrHourlyQuotaExceeded` and blocks send | `instructivo.md` §4 Etapa C #2; `ORIGINAL_REQUEST.md` R2 |
| 8 | R2 — Rate Control | Daily Quota Enforcement | Limits outbound messages to 250 per sliding 86,400-second window | `timestamp time.Time` | `allowed bool` | If counter >= 250, returns `ErrDailyQuotaExceeded` and blocks send | `instructivo.md` §4 Etapa C #2; `ORIGINAL_REQUEST.md` R2 |
| 9 | R2 — Rate Control | Chat Pause with Jitter | Imposes 3,200 ms base delay between chats with deterministic $\pm 30\%$ jitter | `baseMs=3200`, `seed int64/string` | `delay time.Duration [2240ms, 4160ms]` | Returns early if `ctx.Done()` or Kill Switch triggered (< 1s) | `instructivo.md` §4 Etapa C #2; `BUYACAR_FINAL_INTEGRATED.md:234,236` |
| 10 | R2 — Rate Control | Channel Pause with Jitter | Imposes 2,500 ms base delay between channels with deterministic $\pm 30\%$ jitter | `baseMs=2500`, `seed int64/string` | `delay time.Duration [1750ms, 3250ms]` | Returns early if `ctx.Done()` or Kill Switch triggered (< 1s) | `instructivo.md` §4 Etapa C #2; `BUYACAR_FINAL_INTEGRATED.md:235,236` |
| 11 | R2 — Rate Control | Deterministic Jitter Generator | Calculates repeatable pseudo-random jitter factor in $[-0.30, +0.30]$ from seed | `seed string/int64` | `jitterFactor float64` | Pure function; produces identical delay for identical inputs | `ORIGINAL_REQUEST.md` R2; `.agents/rules/50-token-economy.md` |
| 12 | R3 — Selector Resilience | Versioned Selector Resolver | Resolves UI elements via versioned cascade (`v1` -> `v2` -> `v3`) | `elementName string`, `DOMSnapshot` | `matchedSelector string, err` | Returns `ErrDOMSelectorBroken` if all versioned candidates fail | `instructivo.md` §4 Etapa C #4; `.agents/rules/30-channels.md` |
| 13 | R3 — Selector Resilience | Consecutive Failure Counter | Tracks consecutive failures per channel, resetting to 0 on any success | `success bool` | `consecutiveCount int` | Increments on error; resets on success | `app_build/internal/channel/email_pilot.go:89-98`; `.agents/rules/30-channels.md` |
| 14 | R3 — Selector Resilience | Automatic Channel Pauser | Automatically pauses channel when consecutive failures reach 3 | `consecutiveCount >= 3` | `paused bool (true)` | Subsequent calls fail fast with `ErrChannelPaused` | `ORIGINAL_REQUEST.md` R3; `email_pilot.go:94` |
| 15 | R3 — Selector Resilience | Emergency Human Escalation | Enqueues high-priority ticket when channel auto-pauses due to broken selectors | `channelID`, `failureReason`, `details` | `Ticket (ID, ChannelID, Reason)` | Redacts any PII before enqueuing ticket | `ORIGINAL_REQUEST.md` R3; `app_build/internal/escalate` |
| 16 | R3 — Selector Resilience | Channel Fault Isolation | Ensures pausing one channel has zero effect on other registered channels | `channelID string` | Independent state per channel | Other channels continue uninterrupted | `ORIGINAL_REQUEST.md` R3; `instructivo.md` |
| 17 | R3 — Selector Resilience | Channel Reset & Resume | Administrative method allowing operators to unpause channel after fix | `channelID string` | `paused=false, failures=0` | Resumes normal operations | `app_build/internal/channel` |

---

## 6. Edge Cases

| # | Feature | Input | Observed / Required Behavior |
|---|---------|-------|------------------------------|
| 1 | R1 — Vehicle Revalidation | Vehicle sold between triage and reply | Pre-send revalidator detects status changed from `available` to `sold`; replaces reply with `SoldVehicleNotice`, enqueues escalation ticket with `vehicle_sold_or_reserved`, prevents dispatch of stale availability. |
| 2 | R1 — Vehicle Revalidation | Cache expires (> 30 min) during LLM inference | Pre-send revalidator detects `now - lastUpdated > 30m`; aborts outbound message, enqueues escalation with `inventory_stale`, prevents sending unverified data. |
| 3 | R1 — Vehicle Revalidation | Cache marked unavailable (`available=false`) | Pre-send revalidator detects cache offline; aborts outbound message, enqueues escalation with `inventory_unavailable`. |
| 4 | R1 — Vehicle Revalidation | Inquiry does not reference any specific vehicle (e.g. location, hours) | Pre-send revalidator checks `relevantVehicles` is empty; verifies cache is not stale/unavailable, allows dispatch without vehicle-specific lookup. |
| 5 | R1 — Dispatch Verification | Channel returns `send_failed` or transport error | Pre-send revalidator catches failure, enqueues escalation ticket with `send_failed`, marks CP6 `failed`, aborts without sealing successful idempotency. |
| 6 | R2 — Quota Enforcement | 121st message attempted at 59m 59s | Rate controller checks sliding 1-hour window; finds 120 messages; rejects with `ErrHourlyQuotaExceeded`; counter does not increment. |
| 7 | R2 — Quota Enforcement | 251st message attempted within 24 hours | Rate controller checks sliding 24-hour window; finds 250 messages; rejects with `ErrDailyQuotaExceeded`. |
| 8 | R2 — Human Pause | Context canceled while waiting for 4,160 ms pause | Pause loop listens to `select { case <-time.After(delay): ... case <-ctx.Done(): return ctx.Err() }`; terminates within 1 ms, satisfying the < 1s emergency stop. |
| 9 | R2 — Deterministic Jitter | Same conversation ID and seed evaluated multiple times | Jitter formula produces exact same millisecond value every execution, ensuring reproducible tests without flaky timing. |
| 10 | R3 — Selector Resilience | Selector `v1` fails but fallback `v2` succeeds | Selector resolver uses `v2`; operation succeeds; consecutive failure counter resets to 0; channel remains unpaused. |
| 11 | R3 — Selector Resilience | Channel encounters 2 failures, then 1 success, then 1 failure | Failure counter increments to 2, resets to 0 on success, increments to 1 on subsequent failure; channel does NOT pause. |
| 12 | R3 — Selector Resilience | Channel encounters 3 consecutive failures | Failure counter reaches 3; channel transitions to `PAUSED`; escalation ticket enqueued to `Queue`; all future requests return `ErrChannelPaused`. |
| 13 | R3 — Channel Isolation | Channel `whatsapp` paused after 3 failures; `email` receives message | `whatsapp` returns `ErrChannelPaused`; `email` processes message normally with zero crosstalk or blocking. |
| 14 | R3 — Channel Reset | Admin calls `Resume()` on paused channel | `consecutiveFailures` set to 0; `paused` set to `false`; channel immediately resumes accepting traffic. |

---

## 7. Concrete Interface Designs for Implementation

### 7.1 Pre-Send Revalidator Contract (`internal/core`)
```go
type PreSendRevalidator interface {
    Revalidate(ctx context.Context, reply string, vehicles []inventory.Vehicle, cache *inventory.Cache, now time.Time) (RevalidationResult, error)
}

type RevalidationResult struct {
    Approved         bool
    SanitizedReply   string
    MustEscalate     bool
    EscalationReason string
    InventoryStatus  inventory.Status
}
```

### 7.2 Rate Controller Contract (`internal/channel`)
```go
type RateController struct {
    mu                  sync.Mutex
    maxPerHour          int           // 120
    maxPerDay           int           // 250
    delayBetweenChats   time.Duration // 3200ms
    delayBetweenChannels time.Duration // 2500ms
    jitterPercent       float64       // 0.30
    sentTimestamps      []time.Time
}

func (rc *RateController) CheckQuota(now time.Time) error
func (rc *RateController) RecordSent(now time.Time)
func (rc *RateController) CalculateChatDelay(seed string) time.Duration
func (rc *RateController) CalculateChannelDelay(seed string) time.Duration
func (rc *RateController) WaitChatPause(ctx context.Context, seed string) error
func (rc *RateController) WaitChannelPause(ctx context.Context, seed string) error
```

### 7.3 Selector Health & Channel Resilience Guard (`internal/channel`)
```go
type SelectorCatalog struct {
    Key       string
    Selectors []string // v1, v2, v3
}

type ChannelHealthTracker struct {
    mu                  sync.RWMutex
    channelID           string
    consecutiveFailures int
    maxFailuresBeforePause int // 3
    paused              bool
}

func (h *ChannelHealthTracker) RecordSuccess()
func (h *ChannelHealthTracker) RecordFailure() (newFailures int, paused bool)
func (h *ChannelHealthTracker) IsPaused() bool
func (h *ChannelHealthTracker) Resume()
```

---

## 8. Verification and Compliance Checklist

- [x] All parameters from `instructivo.md` Stage C and Step 11 extracted.
- [x] All parameters from `BUYACAR_FINAL_INTEGRATED.md` verified.
- [x] Both standard tables (`## Features Discovered` and `## Edge Cases`) fully populated.
- [x] Mathematical ranges and formulas for deterministic jitter formalized ($[-30\%, +30\%]$).
- [x] State transitions for inventory, quotas, and selector circuit breaker mapped.
- [x] Zero code implementation performed (specification mining only).
