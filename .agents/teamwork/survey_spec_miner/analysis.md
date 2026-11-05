# BuyaCAR Omnicanal Evolution — Specification Mining Report (R1–R5)

**File**: `analysis.md`  
**Author**: Specification Miner (`survey_spec_miner`)  
**Parent Agent**: Orchestrator (Conversation ID: `84a866f8-986b-4c92-a496-9363b3a17b7f`)  
**Workspace**: `c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace`  
**Date**: 2026-10-29  
**Version**: 2.0.0-PROPOSED  

---

## 1. Executive Summary & Authoritative Specification Sources

This document specifies the evolutionary expansion and production hardening for the BuyaCAR Omnicanal Control Plane across modules R1 through R5.

Authoritative specification sources probed and synthesized:
1. `ORIGINAL_REQUEST.md`: Initial mandate, squad allocations, acceptance criteria for R1–R5.
2. `TODO.md` (§2 "Evoluciones Naturales"): Operational scope, future architectural features.
3. `instructivo.md`: Canonical business invariants, transition stages (`SIMULATION` -> `DRAFT` -> `AUTOPILOT`), token budgets.
4. `docs/SPEC.md` & `docs/MVP_DOCUMENTATION.md`: Existing 15 Go standard library packages, 7-checkpoint pipeline, golden suites.
5. `.agents/rules/` (`00-safety.md`, `10-go.md`, `20-ai-contracts.md`, `30-channels.md`, `50-token-economy.md`).
6. `app_build/`: Direct codebase inspection of `internal/channel`, `internal/guard`, `internal/inventory`, `internal/store`, `internal/crm`, `internal/ai`, `internal/obs`, and `test/e2e`.

---

## 2. Features Discovered

| # | Category | Feature | Description | Inputs | Outputs | Error Behavior | Discovered Via |
|---|----------|---------|-------------|--------|---------|----------------|----------------|
| 1 | R1: Core Governance | Initial Safe Mode Boot | Daemon boots strictly in `SIMULATION`, with transitions to `DRAFT` and `AUTOPILOT` gated by human policy and quantitative criteria. | CLI invocation, runtime configuration struct | Running engine bound to `operatingMode=SIMULATION` | Invalid config aborts with structured log | `ORIGINAL_REQUEST.md`, `instructivo.md` §4 |
| 2 | R1: PII Protection | Ley 1581 PII Masking | Deterministic regex masking of emails (`reEmail`), IDs, cards, and tax IDs (`reCardOrID`), preserving verified official lines. | Raw user messages, log strings, error messages | Sanitized string replacing PII with `[PII_EMAIL_REDACTED]` and `[PII_NUM_REDACTED]` | Malformed input returned as empty/sanitized string | `app_build/internal/guard/guard.go`, `ORIGINAL_REQUEST.md` |
| 3 | R1: Anti-Hallucination | 5+ Digit Number Guard | Blocks any unconfirmed number >= 5 digits unless matching relevant vehicle price, mileage, or official dealership phones. | Proposed reply text, relevant vehicle list, official phone set | Validation result (`Approved=true/false`, `MustEscalate=true/false`) | Rejection triggers `MustEscalate=true` and routes to `escalate.Queue` | `app_build/internal/guard/guard.go:151`, `instructivo.md` §3 |
| 4 | R1: Anti-Hallucination | Strict HTTPS Allowlist | Enforces HTTPS scheme and domain allowlist strictly matching `buyacar.com.co` or subdomains. | URLs embedded in outgoing messages | Approved/rejected flag | Non-HTTPS or unknown domains trigger immediate escalation | `app_build/internal/guard/guard.go:130` |
| 5 | R1: Rate Limiting | Sliding Quota Controller | Sliding-window message limiter enforcing max 120 msgs/hour and max 250 msgs/day per channel. | Channel ID, timestamp | Allowed (`bool`), rejection reason (`string`) | Rejects send, retains queue, prevents channel bans | `app_build/internal/channel/rate_controller.go` |
| 6 | R1: Human Pauses | Deterministic SHA-256 Jitter | Computes ±30% human jitter using SHA-256 seed for reproducible, race-free delays between chats (3.2s) and channels (2.5s). | Base duration, jitter percent (0.30), seed int64 | `time.Duration` in [2240ms, 4160ms] or [1750ms, 3250ms] | Context cancellation aborts wait immediately (<1ms) | `app_build/internal/channel/rate_controller.go:149` |
| 7 | R1: Channel Resilience | 3-Failure Auto-Pause | Automatically pauses a channel upon 3 consecutive dispatch failures, enqueuing an emergency ticket to `escalate.Queue`. | Consecutive failure count, error reason, channel ID | Paused status (`bool`), ticket ID | Suppresses duplicate alert storms while channel remains paused | `app_build/internal/channel/resilience.go:179` |
| 8 | R2: Gateways (Meta) | WhatsApp Cloud API Webhook | Ingests Meta Cloud API webhooks with GET verification challenge and POST payload signature validation (`X-Hub-Signature-256`). | HTTP GET (`hub.challenge`), HTTP POST payload + header | Verification echo or incoming conversation struct | Invalid HMAC-SHA256 returns HTTP 401 Unauthorized | `ORIGINAL_REQUEST.md` §R2, `TODO.md` §2 |
| 9 | R2: Gateways (Meta) | WhatsApp HSM Templates | Catalog of Meta pre-approved HSM templates for initiating outreach or re-engaging users beyond 24h window. | Template ID, recipient phone, parameter list | JSON payload for Graph API `/messages` | Missing required parameters aborts send with error | `ORIGINAL_REQUEST.md` §R2, `TODO.md` §2 |
| 10 | R2: Gateways (Meta) | Inbound/Outbound Media | Handles WhatsApp Cloud API media (voice notes, car photos, documents) via Meta Graph Media IDs. | Media ID, MIME type, storage path | Downloaded attachment metadata / outgoing media link | Oversized or unallowlisted MIME type escalated | `ORIGINAL_REQUEST.md` §R2 |
| 11 | R2: Gateways (Meta) | User Opt-In / Opt-Out Engine | Manages user communication consent (`STOP`, `CANCELAR`, `SALIR`, `OPTOUT` vs `START`, `CONTINUAR`). | Inbound message text, user phone | Consent status flag in store/database | Opted-out user immediately blocks all outbound messages | `ORIGINAL_REQUEST.md` §R2 |
| 12 | R2: Gateways (Meta) | Playwright Resilient Fallback | Fallback from Meta Cloud API to browser automation (Playwright/CDP) with `VersionedSelector` when API quota is exhausted. | HTTP 429 status, account suspension signal | Browser-driven reply dispatch | Broken DOM selectors cascade to fallbacks; escalates if all fail | `ORIGINAL_REQUEST.md` §R2, `app_build/internal/channel/resilience.go` |
| 13 | R2: Gateways (MELI) | MercadoLibre Webhook Ingest | Push webhook receiver for vehicle publication questions and post-sale messages. | HTTP POST with JSON event, header `X-Signature` | Standardized `Conversation` and `Message` | Signature mismatch or replay timestamp drift (>5m) rejected | `ORIGINAL_REQUEST.md` §R2, `TODO.md` §2 |
| 14 | R2: Gateways (Omni) | Messenger & IG Direct Adapter | Shared adapter for Facebook Messenger and Instagram Direct utilizing unified triage and rate controller. | Meta Graph messaging webhook events | Uniform incoming message struct | Character limit violations (IG:450, FB:500) safely trimmed | `app_build/internal/guard/guard.go:38`, `ORIGINAL_REQUEST.md` §R2 |
| 15 | R3: State & Sync | SQLite ACID Persistence | Local ACID persistence for `IdempotencyRecord`, `crm.Task`, checkpoints, and waitlists. | Database connection string, transactions | Persisted rows in SQLite tables with WAL mode | SQL error/disk failure fails over to memory/JSON fallback | `ORIGINAL_REQUEST.md` §R3, `TODO.md` §2 |
| 16 | R3: State & Sync | Memory + Flat JSON Fallback | Automatic fallback when SQLite is unavailable, keeping state in memory and syncing to flat local JSON files. | State records, JSON backup path | Serialized JSON file on disk, in-memory lookup | Write errors logged and reported to `escalate.Queue` | `ORIGINAL_REQUEST.md` §R3 |
| 17 | R3: State & Sync | Periodic DMS Sync (<30m) | Syncs local inventory cache with dealership DMS/ERP using HTTP conditional GET with ETag. | Upstream URL, existing ETag, timeout (30s) | HTTP 304 (touch freshness) or HTTP 200 (updated inventory snapshot) | Upstream failure marks cache as unavailable (0 hallucination) | `app_build/internal/inventory/http_fetcher.go`, `ORIGINAL_REQUEST.md` §R3 |
| 18 | R3: State & Sync | Real-Time Stock Invalidation | Secure webhook endpoint (`/api/v1/inventory/invalidate`) to invalidate or update stock/price immediately. | Signed JSON event (sold, reserved, available, price) | Updated memory cache, regenerated ETag, HTTP 200 | Invalid signature returns HTTP 403; bad body HTTP 400 | `ORIGINAL_REQUEST.md` §R3 |
| 19 | R3: State & Sync | Waitlist Alerts (SHA-256) | Generates waitlist alerts sealed with SHA-256 when a vehicle transitions from unavailable/reserved to available. | Customer contact, vehicle query, state transition | High-priority CRM task `commercial_followup` | Hash tampering or missing contact escalates to human | `ORIGINAL_REQUEST.md` §R3, `TODO.md` §2 |
| 20 | R3: State & Sync | Outbound CRM Webhook | Emits HTTP POST webhook with HMAC SHA-256 signature to export leads/tasks to external CRM (HubSpot/Salesforce). | CRM Task struct, endpoint URL, secret key | HTTP 200/201 response | HTTP 5xx triggers exponential backoff (1s, 2s, 4s, 8s); fallback to `escalate.Queue` | `ORIGINAL_REQUEST.md` §R3, `TODO.md` §2 |
| 21 | R4: Cognitive & Cost | Semantic Hash Cache | Hash store of semantically identical queries to reuse validated responses without invoking LLMs. | Normalized query string, vehicle context ID | Cached validated reply (0 tokens) | Cache miss triggers inference cascade | `ORIGINAL_REQUEST.md` §R4, `TODO.md` §2 |
| 22 | R4: Cognitive & Cost | Free Cascade: L1 Local Ollama | Quantized local model (`llama3.2:3b` / `mistral:7b`) on `127.0.0.1:11434` for zero-token-cost inference. | System prompt, compact context, JSON schema | Structured JSON reply conforming to schema | Timeout (>20s) or invalid JSON triggers single retry, then L2 fallback | `app_build/internal/ai/ai.go`, `ORIGINAL_REQUEST.md` §R4 |
| 23 | R4: Cognitive & Cost | Free Cascade: L2 Cloud Free | Cloud-based free tier inference using Groq Cloud API or Gemini Free Tier. | JSON prompt with strict schema | Structured JSON reply, token counts | Rate limit (HTTP 429) or circuit trip triggers L3 fallback | `ORIGINAL_REQUEST.md` §R4 |
| 24 | R4: Cognitive & Cost | Free Cascade: L3 Fast-Path | Deterministic rules engine executing exact match on inventory without LLM invocation (~2.59 µs/op). | Triage classification, inventory cache | Formatted commercial reply, 0 tokens | Unhandled intent or confidence < 0.80 delegates to human escalation | `app_build/internal/fast/fast.go`, `TODO.md` §1 |
| 25 | R5: Ops & Metrics | Embedded Web Console | Local net/http server embedding HTML/Tailwind assets via Go `embed.FS` to supervise system on localhost. | HTTP requests from advisor browser | Rendered dashboard, JSON API responses | Unauthenticated or remote access denied by binding | `ORIGINAL_REQUEST.md` §R5, `TODO.md` §2 |
| 26 | R5: Ops & Metrics | 1-Click DRAFT Approval | Web console action allowing human advisors to review and approve draft messages in 1 click. | Draft message ID, optional human edit | Approved state update, dispatch execution if in Autopilot | Non-existent draft returns HTTP 404 | `ORIGINAL_REQUEST.md` §R5 |
| 27 | R5: Ops & Metrics | 1-Click Channel Unpause | Web console action to reset consecutive failure counters and unpause blocked channels. | Channel ID | Channel status set to `healthy`, failures reset to 0 | Unknown channel returns HTTP 400 | `app_build/internal/channel/resilience.go:224`, `ORIGINAL_REQUEST.md` §R5 |
| 28 | R5: Ops & Metrics | Prometheus `/metrics` | HTTP endpoint exposing Prometheus-formatted metrics (latencies, token counts, cache hit ratio, error counts). | HTTP GET `/metrics` | Prometheus text representation (UTF-8) | Endpoint never panics, returns HTTP 200 with current metrics | `ORIGINAL_REQUEST.md` §R5, `TODO.md` §2 |
| 29 | R5: Ops & Metrics | Structured JSON CLI Export | CLI flag to dump complete operational metrics snapshot in structured JSON. | `./buyacar --export-metrics --format=json` | Formatted JSON stdout with exit code 0 | Internal collector error exits with code 1 | `ORIGINAL_REQUEST.md` §R5 |

---

## 3. Edge Cases Discovered

| # | Feature | Input / Condition | Observed / Documented Behavior |
|---|---------|-------------------|--------------------------------|
| 1 | PII Masking | Text contains official phone number `3151212223` alongside user ID `1049608123`. | `RedactPII` checks `OfficialPhoneNumbers[num]`: preserves `3151212223`, redacts user ID to `[PII_NUM_REDACTED]`. |
| 2 | Anti-Hallucination | LLM proposes price `COP 55.000.000` when vehicle price is `54.000.000`. | `reFiveOrMoreDigits` extracts `55000000`, not in `allowedNumbers`. `ValidateOutbox` fails with `hallucinated_number_blocked:55000000` and escalates. |
| 3 | Anti-Hallucination | Message exceeds channel limit (e.g. 520 runes for Instagram limit 450) and contains price. | `safeTrimWithoutCuttingNumbers` checks if trimming removes price: if cutting truncates number, trimming aborts and escalates to human rather than sending broken price. |
| 4 | Rate Controller | 120 messages dispatched in first 10 minutes of the hour. | `CanSend` returns `(false, "hourly_rate_limit_exceeded")`. Messages queued until sliding window moves past 60 minutes. |
| 5 | Jitter Calculation | Negative seed or seed = 0. | `CalculateDeterministicJitter` computes `sha256("jitter-salt-0")`: deterministic salt produces stable, non-zero bounded duration within exact `[1 - jitter, 1 + jitter]` limits. |
| 6 | Channel Auto-Pause | 2 failures followed by 1 successful delivery, followed by 1 failure. | `RecordSuccess` resets failure count to 0. Subsequent single failure brings count to 1; channel remains `healthy`. |
| 7 | Channel Auto-Pause | Channel already paused receives 5 more failed attempts. | `pausedState` check suppresses redundant alerts: ticket is enqueued ONLY on the transition to paused (count == 3). |
| 8 | WhatsApp Webhook | Verification GET contains wrong `hub.verify_token`. | Webhook handler returns HTTP 403 Forbidden without echoing `hub.challenge`. |
| 9 | WhatsApp Webhook | Incoming message contains user keyword `NO MAS`. | Opt-Out engine sets consent to `false`. Engine acknowledges cancellation and blocks any commercial reply. |
| 10 | WhatsApp Media | Voice message received via WhatsApp Cloud API without text. | Adapter flags `has_audio=true`, extracts media URL, and creates ticket in `escalate.Queue` for voice review. |
| 11 | MercadoLibre Webhook | Webhook arrives with timestamp drift > 5 minutes (replay attack). | Verification rejects event with `ErrReplayAttackPrevented`, HTTP 400. |
| 12 | SQLite Failure | SQLite file locked by external process or disk full error. | Store layer catches SQLite error, automatically falls back to in-memory store + writes `crm_backup.json`, and records warning in `escalate.Queue`. |
| 13 | DMS Sync | Dealership DMS server returns HTTP 304 Not Modified. | `HTTPFetcher.Sync` leaves cached vehicles intact, updates `lastUpdated = now`, and returns `(notModified=true, nil)`. |
| 14 | DMS Sync | Dealership DMS server goes offline (connection refused / HTTP 500). | `HTTPFetcher` marks `cache.SetUnavailable()`. Pre-send checks reject dispatch with `inventory_unavailable`, preventing hallucination. |
| 15 | Waitlist Sealed Hash | Vehicle state changes from `sold` to `available` due to financing cancellation. | Invalidation triggers waitlist scan. Matching buyer's sealed hash is validated against stored record; CRM generates `TaskCommercialFollowup`. |
| 16 | Outbound CRM Webhook | External CRM returns HTTP 503 Service Unavailable. | Webhook dispatcher initiates exponential backoff (1s, 2s, 4s, 8s). If 4th retry fails, lead payload is enqueued into `escalate.Queue`. |
| 17 | Semantic Cache | User asks *"cuanto vale el mazda 3?"* and another asks *"precio del mazda 3?"*. | Triage maps both to `(Intent: price, Model: mazda 3)`. Semantic query hash matches, returning cached validated response in 2.59 µs (0 tokens). |
| 18 | Inference Cascade L1 | Local Ollama daemon is offline (`connection refused: 11434`). | L1 fails immediately -> cascade seamlessly promotes request to L2 (Groq/Gemini Free Cloud API). |
| 19 | Inference Cascade L2 | Gemini Free Tier returns HTTP 429 Quota Exceeded. | Circuit breaker logs failure, promotes request to L3 Fast-Path if intent allows, or safely escalates to human if complex. |
| 20 | Web Console Access | Request to web console arrives from non-loopback IP (e.g. `192.168.1.50`). | Security middleware rejects request with HTTP 403 Forbidden (localhost-only binding: `127.0.0.1:8080`). |

---

## 4. Deep Dive Specification by Evolution Requirement (R1–R5)

### R1. Core Rules, Governance, PII & Rate Limits

#### 4.1.1 Operating Modes & Transition Gates
The system operates across three strictly hierarchical modes:
1. `SIMULATION`: Default immutable boot mode. No external network dispatches, all channel replies recorded as `simulated_no_op`.
2. `DRAFT`: Human-supervised mode. Inbound reading operates against live channels/DMS, but outbound replies are stored as `draft_recorded` for advisor 1-click approval.
3. `AUTOPILOT`: Fully autonomous mode with active dispatch (`sent_verified`). Activated **only** per channel when the Quantitative Exit Gate is satisfied:
   - $\ge 100$ real conversations processed in DRAFT.
   - $0$ hallucinated numbers.
   - $0$ duplicate messages.
   - $\ge 70\%$ resolved via Fast-Path (0 tokens).
   - $\ge 85\%$ approved without human editing.

#### 4.1.2 PII Redaction Specification (Ley 1581 de 2012)
- **Email Regex**: `(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}` $\rightarrow$ `[PII_EMAIL_REDACTED]`
- **ID & Card Regex**: `\b\d{7,16}\b` (Colombian Cédula: 7–10 digits; NIT: 9–10 digits; Credit Card: 13–16 digits).
  - Exception Allowlist:
    - Dealership WhatsApp: `3151212223`
    - Dealership Landline 1: `3022866969`
    - Dealership Landline 2: `3160502717`
  - All non-allowlisted matches $\rightarrow$ `[PII_NUM_REDACTED]`

#### 4.1.3 Anti-Hallucination Guardrails
- **5+ Digit Rule**: Any sequence of $\ge 5$ consecutive digits (`\b\d{5,}\b`) must exist in the active set:
  $$\mathcal{S}_{\text{allowed}} = \mathcal{S}_{\text{phones}} \cup \{ \text{PriceCOP}(v), \text{MileageKm}(v) \mid v \in \mathcal{V}_{\text{relevant}} \}$$
  If $n \notin \mathcal{S}_{\text{allowed}}$, message is rejected with `hallucinated_number_blocked:<number>` and routed to `escalate.Queue`.
- **URL Allowlist**: Scheme must be `https`. Host must equal `buyacar.com.co` or end with `.buyacar.com.co`.
- **Prohibited Marketing Expressions**: Exact normalized matches of forbidden claims trigger immediate escalation:
  - *"crédito aprobado sin requisitos"*
  - *"aprobación garantizada sin estudio"*
  - *"el más barato del mercado"*
  - *"compramos tu carro sin revisarlo"*
  - *"descuento garantizado"*

#### 4.1.4 Rate Limiting & Deterministic Jitter Formula
- **Rate Windows**:
  - $W_{\text{hour}} = 120\text{ messages} / 3600\text{ seconds}$
  - $W_{\text{day}} = 250\text{ messages} / 86400\text{ seconds}$
- **Sliding Window Pruning**: On each evaluation, entries older than 24 hours are pruned from memory.
- **Deterministic SHA-256 Jitter**:
  Given base duration $D_{\text{base}}$, jitter percentage $J = 0.30$, and integer seed $S$:
  $$H = \text{SHA256}(\text{"jitter-salt-"} \mathbin{\Vert} \text{string}(S))$$
  $$V = (H[0] \ll 8) \mid H[1] \quad \in [0, 65535]$$
  $$N = \left( \frac{V}{65535.0} \right) \times 2.0 - 1.0 \quad \in [-1.0, 1.0]$$
  $$\Delta = N \times J$$
  $$D_{\text{actual}} = D_{\text{base}} \times (1.0 + \Delta)$$
  - For Chat Base ($3200\text{ ms}$): $D_{\text{chat}} \in [2240\text{ ms}, 4160\text{ ms}]$
  - For Channel Base ($2500\text{ ms}$): $D_{\text{channel}} \in [1750\text{ ms}, 3250\text{ ms}]$

#### 4.1.5 Auto-Pausa State Machine
```
[HEALTHY] ──(failure 1, 2)──> [DEGRADING (count=1,2)]
   │                                  │
(success)                         (failure 3)
   │                                  ▼
[RESET count=0] <──(unpause)──── [PAUSED (count=3)]
                                      │
                                      ├── Enqueue Ticket to escalate.Queue
                                      └── Suppress future duplicate alerts
```

---

### R2. Channels and Communication Gateways (Meta & MercadoLibre)

#### 4.2.1 WhatsApp Business Cloud API Adapter
- **Base Endpoint**: `https://graph.facebook.com/v20.0/{phone_number_id}/messages`
- **Inbound Webhook Verification (`GET /webhook/whatsapp`)**:
  - Query parameters: `hub.mode`, `hub.verify_token`, `hub.challenge`.
  - Condition: `hub.mode == "subscribe"` && `hub.verify_token == WHATSAPP_VERIFY_TOKEN`.
  - Response: HTTP 200 with body = `hub.challenge`.
- **Inbound Message Event (`POST /webhook/whatsapp`)**:
  - Signature validation: Header `X-Hub-Signature-256: sha256=<hex_hmac>`.
  - Computed via: $\text{HMAC-SHA256}(\text{body}, \text{META\_APP\_SECRET})$.
  - Payload Schema:
    ```json
    {
      "object": "whatsapp_business_account",
      "entry": [{
        "id": "WABA_ID",
        "changes": [{
          "value": {
            "messaging_product": "whatsapp",
            "metadata": { "phone_number_id": "PHONE_ID" },
            "messages": [{
              "id": "wamid.HBgL...",
              "from": "573151212223",
              "timestamp": "1730178000",
              "type": "text | image | audio | interactive",
              "text": { "body": "¿Está disponible la Duster?" },
              "image": { "id": "MEDIA_ID", "mime_type": "image/jpeg" },
              "audio": { "id": "MEDIA_ID", "mime_type": "audio/ogg" }
            }]
          },
          "field": "messages"
        }]
      }]
    }
    ```
- **Pre-Approved HSM Templates (Post-24h Window)**:
  - Supported templates: `car_followup_v1`, `waitlist_notification_v1`, `appointment_reminder_v1`.
  - Payload format:
    ```json
    {
      "messaging_product": "whatsapp",
      "to": "573001234567",
      "type": "template",
      "template": {
        "name": "car_followup_v1",
        "language": { "code": "es_CO" },
        "components": [
          { "type": "body", "parameters": [{ "type": "text", "text": "Renault Duster" }] }
        ]
      }
    }
    ```
- **Opt-In / Opt-Out Engine**:
  - Unsubscribe tokens: `["stop", "cancelar", "salir", "baja", "no mas", "optout"]`.
  - Resubscribe tokens: `["start", "activar", "continuar", "alta", "optin"]`.
- **Playwright / Browser Resilient Fallback**:
  - Triggered when: WhatsApp Cloud API returns HTTP 429, HTTP 500, or account verification alert.
  - Failover cascade uses `VersionedSelector` for DOM elements (`chat_input`, `send_button`, `message_bubble`).

#### 4.2.2 MercadoLibre Webhook Ingest
- **Base Endpoint**: `POST /webhook/mercadolibre`
- **HMAC Verification**: Header `X-Signature: ts=<ts>,v1=<hmac_sha256>`.
  - Validation: Verify timestamp $| \text{now} - \text{ts} | \le 300\text{ s}$ (anti-replay).
  - Signature: $\text{HMAC-SHA256}(\text{"ts="} + \text{ts} + \text{body}, \text{MELI\_CLIENT\_SECRET})$.
- **Payload Schema**:
  ```json
  {
    "resource": "/questions/123456789",
    "user_id": 98765432,
    "topic": "questions",
    "application_id": 11223344,
    "attempts": 1,
    "sent": "2026-10-29T05:00:00Z",
    "received": "2026-10-29T05:00:01Z"
  }
  ```
- **Triage Ingestion**: Fetches question details via authenticated MELI API stub or payload, cleans marketplace boilerplate, routes to `triage.Classify`.

#### 4.2.3 Facebook Messenger & Instagram Direct
- Webhook endpoints sharing Meta Graph verification infrastructure.
- Character limits: Instagram Direct = 450 runes, Facebook = 500 runes.
- Reuses shared `MemoryRateController` and `triage.Classify`.

---

### R3. Persistence, DMS Synchronization & CRM Connectors

#### 4.3.1 SQLite Local ACID Persistence
- **Engine**: SQLite 3 with WAL Mode (`PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;`).
- **DDL Schema**:
  ```sql
  CREATE TABLE IF NOT EXISTS idempotency_records (
      hash TEXT PRIMARY KEY,
      channel_id TEXT NOT NULL,
      conv_id TEXT NOT NULL,
      reply_text TEXT,
      created_at TIMESTAMP NOT NULL,
      send_status TEXT,
      etag TEXT,
      crm_task_id TEXT
  );

  CREATE TABLE IF NOT EXISTS crm_tasks (
      id TEXT PRIMARY KEY,
      conv_id TEXT NOT NULL,
      channel_id TEXT NOT NULL,
      type TEXT NOT NULL,
      status TEXT NOT NULL,
      description TEXT,
      vehicle_id TEXT,
      created_at TIMESTAMP NOT NULL,
      updated_at TIMESTAMP
  );

  CREATE TABLE IF NOT EXISTS checkpoints (
      conv_id TEXT NOT NULL,
      checkpoint TEXT NOT NULL,
      updated_at TIMESTAMP NOT NULL,
      PRIMARY KEY (conv_id, checkpoint)
  );

  CREATE TABLE IF NOT EXISTS waitlist (
      id TEXT PRIMARY KEY,
      client_contact TEXT NOT NULL,
      brand TEXT NOT NULL,
      model TEXT NOT NULL,
      max_budget_cop INTEGER,
      status TEXT NOT NULL, -- 'pending', 'notified', 'cancelled'
      created_at TIMESTAMP NOT NULL,
      sealed_hash TEXT NOT NULL
  );

  CREATE INDEX IF NOT EXISTS idx_crm_tasks_conv ON crm_tasks(conv_id);
  CREATE INDEX IF NOT EXISTS idx_waitlist_status ON waitlist(status);
  ```

#### 4.3.2 Fallback: In-Memory + Flat JSON Backup
- If SQLite initialization or queries fail (e.g. disk read-only or lock timeout):
  1. Failover seamlessly to `MemoryStore` and `MemoryCRMManager`.
  2. Write state snapshots asynchronously to `production_artifacts/persistence/idempotency_backup.json` and `crm_backup.json`.
  3. Register alert in `escalate.Queue` under reason `sqlite_failover_to_json`.

#### 4.3.3 Dealership DMS Synchronization (<30m) with ETag
- **Periodic Timer**: 15 minutes (stale threshold = 30 minutes).
- **Conditional GET**:
  - Request headers: `If-None-Match: <current_etag>`, `If-Modified-Since: <last_rfc1123>`.
  - HTTP 304: Calls `cache.TouchRefresh(now)`, 0 vehicle parsing overhead.
  - HTTP 200: Parses JSON vehicle array, updates memory cache, records new ETag.
  - Upstream Failure: Calls `cache.SetUnavailable()`.

#### 4.3.4 Real-Time Stock/Price Invalidation Webhook
- **Endpoint**: `POST /api/v1/inventory/invalidate`
- **Security**: Bearer token or `X-BuyaCAR-Signature: sha256=<hmac>`.
- **Payload Schema**:
  ```json
  {
    "event": "stock_status_changed",
    "vehicle_id": "veh-duster-2021",
    "status": "sold | reserved | available",
    "price_cop": 68000000,
    "timestamp_utc": "2026-10-29T05:00:00Z"
  }
  ```
- **Handler Action**:
  1. Atomically updates `Vehicle` in cache under mutex lock.
  2. Recomputes cache ETag: $\text{ETag} = \text{MD5}(\text{snapshot\_timestamp} + \text{vehicle\_count})$.
  3. Scans waitlist for matching buyers if status transitioned to `available`.

#### 4.3.5 Waitlist Sealing & Transition Alerts
- **Sealing Formula**:
  $$\text{SealedHash} = \text{SHA256}(\text{client\_contact} \mathbin{\Vert} \text{brand} \mathbin{\Vert} \text{model} \mathbin{\Vert} \text{created\_at\_rfc3339})$$
- **Trigger**: When vehicle transitions $\text{status} \in \{\text{"sold"}, \text{"reserved"}\} \to \text{"available"}$.
- **Action**: Generates `crm.Task` with `Type: TaskCommercialFollowup`, `Description: "Vehículo en lista de espera ahora disponible: <Model>"` and marks waitlist entry `status: notified`.

#### 4.3.6 Outbound CRM Webhook (HMAC SHA-256)
- **Endpoint**: External configured CRM URL (e.g. `https://crm.buyacar.internal/webhook/leads`).
- **Signature Header**: `X-BuyaCAR-Signature: sha256=<hex_hmac>`.
- **Payload Schema**:
  ```json
  {
    "event": "crm_task_created",
    "task_id": "task-conv-01-1730178000",
    "conversation_id": "conv-01",
    "channel_id": "whatsapp",
    "task_type": "schedule_test_drive",
    "vehicle_id": "veh-duster-2021",
    "description": "Cliente solicita agendar visita o prueba de ruta",
    "created_at_utc": "2026-10-29T05:00:00Z"
  }
  ```
- **Retry Policy**: Exponential backoff $T_r = 1\text{s} \times 2^{r-1}$ for $r \in [1, 4]$ ($1\text{s}, 2\text{s}, 4\text{s}, 8\text{s}$). If all 4 retries fail, ticket is logged to `escalate.Queue` under reason `crm_export_failed`.

---

### R4. Cognitive Layer and Token Economy with Free Inference Cascade

#### 4.4.1 Semantic Hash Cache & Deduplication
- **Query Normalization**:
  $$\text{NormText} = \text{stripAccents}(\text{toLower}(\text{trim}(\text{incomingText})))$$
- **Semantic Hash Formula**:
  $$\text{QueryHash} = \text{SHA256}(\text{Intent} \mathbin{\Vert} \text{NormText} \mathbin{\Vert} \text{VehicleID})$$
- **Cache Hit SLA**: $< 5\text{ µs}$ retrieval, $0$ tokens consumed.

#### 4.4.2 Free Inference Cascade Architecture
```
                  Incoming User Query
                           │
                           ▼
          [Exact-Match Deterministic Fast-Path] ──(Confidence >= 0.80)──> 0 Tokens Reply (~2.59 µs)
                           │
                     (Confidence < 0.80)
                           ▼
          [L1: Local Ollama (llama3.2:3b / 127.0.0.1)] ──(Success)─────> 0 Network Tokens (Local GPU/CPU)
                           │
                     (Timeout / Offline)
                           ▼
          [L2: Free Cloud API (Groq / Gemini Free)] ────(Success)─────> Free Tier Token Quota
                           │
                 (Rate Limit / Circuit Open)
                           ▼
          [L3: Fallback Offline Fast-Path / Human] ────────────────────> escalate.Queue Ticket
```

- **Tier 1 (Ollama Local)**:
  - Host: `http://127.0.0.1:11434/api/generate`
  - Model: `llama3.2:3b` or `mistral:7b-instruct-q4`
  - Timeout: 20 seconds.
- **Tier 2 (Free Cloud API)**:
  - Groq Cloud API: `llama-3.1-8b-instant` (free tier rate limits: 30 RPM, 14.4k RPD).
  - Gemini Free Tier: `gemini-1.5-flash` (free tier: 15 RPM).
  - Timeout: 8 seconds (p95 SLA).
- **Tier 3 (Offline Fast-Path Exact-Match)**:
  - 100% deterministic rules in `internal/fast`.
  - Latency SLA: p99 $< 5\text{ ms}$ (measured `2.59 µs/op`).
  - Strict Token Economy Budget: Max 1,500 input tokens / 300 output tokens when LLM is invoked.

---

### R5. Embedded Local Web Console and Observability

#### 4.5.1 Embedded Local Web Console (`embed.FS` + `net/http`)
- **Technology**: Go 1.23 standard library `embed.FS` + `net/http`.
- **Zero External Runtime**: All CSS/JS inlined or bundled into binary.
- **Security Binding**: Binds strictly to `127.0.0.1:8080` (rejects external network interfaces).
- **Endpoints**:
  - `GET /`: Serves embedded single-page dashboard.
  - `GET /api/v1/queue`: Returns JSON list of pending tickets from `escalate.Queue`.
  - `GET /api/v1/channels`: Returns JSON status of all channels (`healthy`, `paused`, consecutive failure count).
  - `POST /api/v1/drafts/{id}/approve`: Approves draft message, dispatches if permitted.
  - `POST /api/v1/channels/{id}/unpause`: Unpauses channel, resets failure count to 0.

#### 4.5.2 Prometheus `/metrics` Endpoint
- **Endpoint**: `GET /metrics`
- **Exposed Metrics**:
  ```prometheus
  # HELP buyacar_conversations_total Total conversations processed by channel and status
  # TYPE buyacar_conversations_total counter
  buyacar_conversations_total{channel="whatsapp",status="sent_verified"} 42
  buyacar_conversations_total{channel="whatsapp",status="draft_recorded"} 12
  buyacar_conversations_total{channel="whatsapp",status="escalated"} 3

  # HELP buyacar_fastpath_latency_seconds Latency of deterministic fast-path
  # TYPE buyacar_fastpath_latency_seconds summary
  buyacar_fastpath_latency_seconds{quantile="0.5"} 0.00000259
  buyacar_fastpath_latency_seconds{quantile="0.95"} 0.00000410
  buyacar_fastpath_latency_seconds{quantile="0.99"} 0.00000520

  # HELP buyacar_tokens_total Token consumption by provider and direction
  # TYPE buyacar_tokens_total counter
  buyacar_tokens_total{provider="ollama",direction="input"} 1420
  buyacar_tokens_total{provider="ollama",direction="output"} 280
  buyacar_tokens_total{provider="groq",direction="input"} 0

  # HELP buyacar_cache_hits_total Number of cache hits
  # TYPE buyacar_cache_hits_total counter
  buyacar_cache_hits_total{type="semantic"} 28
  buyacar_cache_hits_total{type="inventory_etag"} 14

  # HELP buyacar_channel_consecutive_failures Current consecutive failure count
  # TYPE buyacar_channel_consecutive_failures gauge
  buyacar_channel_consecutive_failures{channel="whatsapp"} 0
  buyacar_channel_consecutive_failures{channel="mercadolibre"} 0

  # HELP buyacar_channel_paused Channel pause state (1=paused, 0=healthy)
  # TYPE buyacar_channel_paused gauge
  buyacar_channel_paused{channel="whatsapp"} 0
  buyacar_channel_paused{channel="mercadolibre"} 0

  # HELP buyacar_draft_exit_gate_ratio Progress toward Autopilot quality gate
  # TYPE buyacar_draft_exit_gate_ratio gauge
  buyacar_draft_exit_gate_ratio{metric="without_ai"} 76.5
  buyacar_draft_exit_gate_ratio{metric="without_human"} 88.0
  buyacar_draft_exit_gate_ratio{metric="approved_unedited"} 91.2
  ```

#### 4.5.3 Structured JSON CLI Export
- **CLI Flag**: `./buyacar --export-metrics --format=json`
- **Output JSON Schema**:
  ```json
  {
    "timestamp_utc": "2026-10-29T05:00:00Z",
    "operating_mode": "DRAFT",
    "conversations": {
      "total": 120,
      "resolved_without_ai": 92,
      "resolved_without_human": 105,
      "sent_verified": 0,
      "draft_recorded": 105,
      "escalated": 15
    },
    "latencies_ms": {
      "p50": 0.0026,
      "p95": 0.0041,
      "p99": 0.0052
    },
    "tokens": {
      "total_input": 2450,
      "total_output": 510,
      "by_provider": {
        "ollama": { "input": 2450, "output": 510 },
        "groq": { "input": 0, "output": 0 }
      }
    },
    "channels": {
      "whatsapp": { "status": "healthy", "consecutive_failures": 0, "hourly_count": 14, "daily_count": 32 },
      "mercadolibre": { "status": "healthy", "consecutive_failures": 0, "hourly_count": 8, "daily_count": 19 }
    },
    "draft_exit_gate": {
      "conversations_processed": 120,
      "hallucinated_numbers": 0,
      "duplicates_sent": 0,
      "fast_path_percent": 76.67,
      "approved_unedited_pct": 91.2,
      "ready_for_autopilot": true
    }
  }
  ```

---

## 5. Acceptance Criteria & Test Verification Matrix

| Area | Requirement | Test Case / Verification Command | Expected Outcome |
|---|---|---|---|
| Core / R1 | Go 1.23 standard library | `go vet ./...` & `go build -o buyacar.exe ./cmd/buyacar` | Clean build, 0 external heavy modules, exit code 0 |
| Core / R1 | PII Protection (Ley 1581) | `go test -v -run TestGuardRedactPII ./internal/guard` | Cédulas, NITs, credit cards redacted; dealership phones preserved |
| Core / R1 | Anti-Hallucination Guard | `go test -v -run TestGuardValidateOutbox ./internal/guard` | Blocks unconfirmed 5+ digits, non-HTTPS URLs, and forbidden phrases |
| Core / R1 | Rate Limits & Jitter | `go test -v -run TestRateController ./internal/channel` | 120/h and 250/d limits enforced; jitter within ±30% deterministic bounds |
| Core / R1 | 3-Failure Auto-Pause | `go test -v -run TestResilienceAutoPause ./internal/channel` | Pauses on 3rd failure; enqueues ticket; suppresses duplicate alerts |
| Gateways / R2 | WhatsApp Cloud Webhook | `go test -v -run TestWhatsAppWebhook ./internal/channel` | Challenge echo on GET; HMAC-SHA256 signature verified on POST |
| Gateways / R2 | MercadoLibre Webhook | `go test -v -run TestMercadoLibreWebhook ./internal/channel` | `X-Signature` validated; replay attacks > 5m rejected |
| Gateways / R2 | Messenger / Instagram | `go test -v -run TestOmnichannelAdapter ./internal/channel` | Char limits respected (IG:450, FB:500); rate limits shared |
| State / R3 | SQLite ACID Persistence | `go test -v -run TestSQLiteStore ./internal/store` | Idempotency and CRM tasks persisted under WAL; ACID verified |
| State / R3 | SQLite Failover to JSON | `go test -v -run TestStoreFailover ./internal/store` | Disk failure smoothly falls back to memory + flat JSON backup |
| State / R3 | DMS ETag Sync | `go test -v -run TestHTTPFetcherSync ./internal/inventory` | HTTP 304 touches refresh; HTTP 200 refreshes vehicles and ETag |
| State / R3 | Stock Invalidation Webhook | `go test -v -run TestInventoryInvalidation ./internal/inventory` | POST `/api/v1/inventory/invalidate` updates vehicle and ETag |
| State / R3 | Waitlist SHA-256 Alerts | `go test -v -run TestWaitlistAlerts ./internal/crm` | Transition to available generates sealed CRM follow-up task |
| State / R3 | Outbound CRM Webhook | `go test -v -run TestCRMOutboundWebhook ./internal/crm` | HMAC-SHA256 signed; exponential backoff on HTTP 5xx; fallback to queue |
| Cognitive / R4 | Semantic Cache | `go test -v -run TestSemanticCache ./internal/skills` | Deduplicates identical semantic queries; 0 tokens consumed |
| Cognitive / R4 | Free Inference Cascade | `go test -v -run TestInferenceCascade ./internal/ai` | L1 Ollama -> L2 Groq/Gemini -> L3 Fast-path fallback verified |
| Ops / R5 | Embedded Web Console | `go test -v -run TestWebConsoleRoutes ./internal/console` | `embed.FS` serves dashboard; queue review and unpause respond |
| Ops / R5 | Prometheus Metrics | `go test -v -run TestPrometheusMetrics ./internal/obs` | `/metrics` exports valid Prometheus format; counters accurate |
| Ops / R5 | Structured JSON CLI | `go test -v -run TestCLIExportMetrics ./cmd/buyacar` | `--export-metrics --format=json` outputs valid schema |
| Quality | Race Detector | `go test -race ./...` | 0 race conditions across all concurrent modules |
