# BuyaCAR Omnicanal — Channels Architecture & R2 Evolution Analysis

- **Document Version**: 1.0.0
- **Author**: Channels Explorer (Survey Phase R1, R2)
- **Status**: Completed Survey & Architectural Blueprint
- **Target Component**: `app_build/internal/channel/` and Omnichannel Gateways

---

## 1. Executive Summary

BuyaCAR Omnicanal is an autonomous customer inquiry processing system built in pure standard Go 1.23, operating under strict Colombian privacy regulations (Ley 1581 de 2012) and certified dealership parameters in Tunja, Boyacá. The baseline MVP implements core concurrency, a 7-checkpoint pipeline (`CP1` to `CP7`), an ultra-low latency Fast-Path engine (~2.59 µs/op exact-match), an in-memory RateController (120 msg/h, 250 msg/d with deterministic SHA-256 jitter), a ResilienceTracker with automatic 3-consecutive-failure auto-pause and alert escalation, and an Email Pilot adapter for Stage B (`DRAFT`) supervision.

This survey establishes the complete architectural specification and design for **Requirement R2 (Channels and Communication Connectors)**, enabling:
1. **Official WhatsApp Business Cloud API Adapter**: Native webhook endpoint with verification handshake, HMAC-SHA256 signature validation, HSM (Highly Structured Message) pre-approved templates catalog, inbound/outbound media handling, and opt-in/opt-out consent management.
2. **Resilient Fallback to Playwright Browser Automation**: Automatic failover upon Cloud API quota saturation (HTTP 429), tier limits, or pending WABA verification, leveraging `VersionedSelector` cascades and humanized delays.
3. **MercadoLibre Push Webhook Connector**: Push notification receiver (`/webhooks/mercadolibre`) with cryptographic `X-Signature` verification, topic routing for pre-sale publication questions (`questions`) and post-sale messages (`messages`), and enforcement of ML zero-contact-data policies in pre-sale.
4. **Unified Meta Omnichannel Adapter (Facebook Messenger & Instagram Direct)**: Unified webhook listener for `object: "page"` and `object: "instagram"`, sharing triage, fast-path, and rate controlling while respecting individual channel limits (450 chars for Instagram, 500 for Facebook).
5. **Universal Rate & Resilience Integration**: Standardized integration lifecycle tying all channels into `RateController` and `ResilienceTracker` with zero race conditions, strict channel isolation, and automated escalation to `escalate.Queue`.

---

## 2. Mapping Existing Channel Architecture & Contracts

### 2.1 The Canonical `Channel` Interface (`channel.go`)

The core abstraction is defined in `app_build/internal/channel/channel.go`:

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

#### Core Data Entities:
- **`Limits`**: Defines `MaxCharacterLength`, `MaxPerHour` (default: 120), and `MaxPerDay` (default: 250).
- **`Session`**: Represents an authenticated session (`ID`, `ChannelID`, `CreatedAt`).
- **`Conversation`**: Identifies a thread pending action (`ID`, `ChannelID`, `CustomerAlias`, `UpdatedAt`).
- **`Message`**: Atomic message (`ID`, `Sender` ["customer" | "own"], `Text`, `Timestamp`).
- **`Thread`**: `Conversation` metadata plus sequential message history `[]Message`.
- **`SendStatus`**:
  - `SendSimulated`: `"simulated_no_op"` (used in `SIMULATION`).
  - `SendDrafted`: `"draft_recorded"` (used in `DRAFT`).
  - `SendVerified`: `"sent_verified"` (used in `AUTOPILOT`).
  - `SendFailed`: `"send_failed"` (triggers escalation).
- **`SendResult`**: Encapsulates `Status`, `MessageID`, and `Timestamp`.

### 2.2 Rate Controller (`rate_controller.go`)

The `RateController` contract and `MemoryRateController` implementation provide sliding-window throttling and human delay simulation:

1. **Sliding Window Counters**:
   - Hourly limit: 120 messages per channel per sliding 1-hour window.
   - Daily limit: 250 messages per channel per sliding 24-hour window.
   - Automatic memory pruning: Records older than 24 hours are evicted during `CanSend()`.
2. **Deterministic SHA-256 Jitter**:
   - Pausa entre conversaciones (`ChatDelayBase` = 3200 ms, ±30%): strictly in `[2240 ms, 4160 ms]`.
   - Pausa entre canales (`ChannelDelayBase` = 2500 ms, ±30%): strictly in `[1750 ms, 3250 ms]`.
   - Formula:
     ```go
     h := sha256.Sum256([]byte(fmt.Sprintf("jitter-salt-%d", seed)))
     val := int(h[0])<<8 | int(h[1]) // 0..65535
     normalized := (float64(val)/65535.0)*2.0 - 1.0 // [-1.0, +1.0]
     delay := base * (1.0 + normalized * jitterPercent)
     ```
   - 100% reproducible across test runs, free of global PRNG state locks or race conditions.
3. **Cancelable Context Waits**:
   - `WaitWithContext(ctx, delay)` uses `time.NewTimer` with `select` on `ctx.Done()`, enabling sub-millisecond emergency shutdown (< 1s SLA).

### 2.3 Resilience Tracker & Selectors (`resilience.go`)

1. **`VersionedSelector`**:
   - Implements DOM selector resilience with `Primary` string and `Fallbacks []string`.
   - Methods: `GetActiveSelector()`, `FailoverToNext()`, `Reset()`, `Resolve(brokenMap)`.
   - Allows browser automation to adapt dynamically to minor UI modifications.
2. **`MemoryResilienceTracker`**:
   - Monitored threshold: `DefaultMaxConsecutiveFailures = 3`.
   - `RecordFailure(channelID, reason)`:
     - Increments consecutive failure counter.
     - When `failures >= 3` and channel is not yet paused: sets `pausedState[channelID] = true` and pushes an urgent ticket to `escalate.Queue` with reason `"channel_auto_paused_consecutive_failures"`.
     - Suppresses alert storms: subsequent errors while paused do not re-enqueue tickets.
   - `RecordSuccess(channelID)`:
     - Resets `consecutiveFailures[channelID] = 0`.
   - `Resume(channelID)` / `SetPaused(channelID, false)`:
     - Clears pause state and resets error counters to 0.

### 2.4 Existing Adapters (`email_pilot.go` & `stub.go`)

- **`StubChannel`**: In-memory fixture supporting synthetic conversations, error simulation flags (`failSend`, `failRead`), and `ResilienceTracker` integration.
- **`EmailPilotChannel`**: Production pilot adapter for Stage B (`DRAFT`). Strictly blocks `AUTOPILOT` via `ErrEmailAutopilotBlocked`. Collects generated drafts in `draftOutbox`, supports human approval via `ReviewDraft()`, and evaluates the draft exit gate (requires >= 85% unedited human approvals to satisfy readiness criteria).

### 2.5 Pipeline Integration Flow (`core/pipeline.go`)

The core execution loop executes across 7 checkpoints:
1. `CP1 (Lectura)`: `ch.Read()` fetches thread.
2. `Paso 2 (Omisión)`: Verifies last message is from customer, checks SHA-256 deduplication, verifies age <= 9 weeks, validates kill switch.
3. `CP2 (Triage)`: Deterministic classification (`triage.Classify`) into intent, query, and escalation triggers (prompt injection, trade-in, credit, legal, discounts).
4. `CP3 & CP4 (Inventario + Respuesta)`: Evaluates Fast-Path (`fast.Engine`) first (~2.59 µs). If unhandled, delegates to AI Orchestrator (`ai.Orchestrator`).
5. `CP5 (Guardrails)`: `guard.Guard` checks 5+ digit hallucination, allowlisted URLs (`https://buyacar.com.co`), PII redaction, forbidden phrases, and character limits.
6. `CP6 (Envio/Reply)`: Invokes `ch.Reply()`. In `SIMULATION` expects `SendSimulated`; in `DRAFT` expects `SendDrafted`; in `AUTOPILOT` expects `SendVerified`. Upon failure, triggers `rt.RecordFailure()` and escalates. Upon success, calls `rt.RecordSuccess()`.
7. `CP7 (CRM & Idempotencia)`: Persists idempotency record with `ETag`, `SendStatus`, and CRM task assignment.

---

## 3. Official WhatsApp Business Cloud API Adapter

### 3.1 Architecture Overview

The WhatsApp Cloud API adapter connects directly to Meta's Graph API (`graph.facebook.com/v21.0`). Unlike the polling model of browser scrapers, Meta operates on an asynchronous push model (Webhooks) for inbound traffic and REST calls for outbound traffic.

```
                    ┌──────────────────────────────────────────────┐
                    │            Meta Graph API / Webhook          │
                    └──────────────────────┬───────────────────────┘
                                           │
                        Inbound POST /webhooks/whatsapp
                        (Header: X-Hub-Signature-256)
                                           ▼
             ┌─────────────────────────────────────────────────────────────┐
             │                  WhatsAppCloudAdapter                       │
             │  1. HMAC-SHA256 Signature Validation (App Secret)           │
             │  2. Webhook Event De-serialization (entry.changes.messages) │
             │  3. Opt-in / Opt-out Filtering (Ley 1581 / STOP keywords)   │
             │  4. Enqueue into Inbound Buffer / Conversation Store        │
             └─────────────────────────────┬───────────────────────────────┘
                                           │
                                  Pending() / Read()
                                           ▼
             ┌─────────────────────────────────────────────────────────────┐
             │                Core Pipeline (CP1 -> CP7)                   │
             └─────────────────────────────┬───────────────────────────────┘
                                           │
                                     Reply() Call
                                           ▼
             ┌─────────────────────────────────────────────────────────────┐
             │                  Outbound Delivery Engine                   │
             │  1. Check RateController (CanSend: 120/h, 250/d)            │
             │  2. Window Evaluation (< 24h: Free text; > 24h: HSM)        │
             │  3. If Mode == SIMULATION -> Return SendSimulated           │
             │  4. If Mode == DRAFT -> Store Draft in Outbox               │
             │  5. If Mode == AUTOPILOT -> POST /messages to Graph API     │
             │     (On 429 / Tier Limit -> Trigger Playwright Fallback)    │
             └─────────────────────────────────────────────────────────────┘
```

### 3.2 Webhook Handshake & Signature Validation

1. **GET Verification Handshake**:
   - Meta sends: `hub.mode`, `hub.verify_token`, `hub.challenge`.
   - Verification logic:
     ```go
     if r.URL.Query().Get("hub.mode") == "subscribe" &&
        r.URL.Query().Get("hub.verify_token") == a.verifyToken {
         w.WriteHeader(http.StatusOK)
         w.Write([]byte(r.URL.Query().Get("hub.challenge")))
         return
     }
     http.Error(w, "forbidden", http.StatusForbidden)
     ```
2. **POST Event Verification**:
   - Header: `X-Hub-Signature-256: sha256=<hex_digest>`.
   - Validation implementation using `crypto/hmac` and `crypto/sha256`:
     ```go
     mac := hmac.New(sha256.New, []byte(a.appSecret))
     mac.Write(bodyBytes)
     expectedMAC := hex.EncodeToString(mac.Sum(nil))
     actualMAC := strings.TrimPrefix(r.Header.Get("X-Hub-Signature-256"), "sha256=")
     if !hmac.Equal([]byte(expectedMAC), []byte(actualMAC)) {
         http.Error(w, "invalid signature", http.StatusUnauthorized)
         return
     }
     ```
   - Immediate HTTP 200: Handlers return `200 OK` within 2 seconds to prevent Meta retries.

### 3.3 Data Models & Inbound Normalization

WhatsApp Cloud API payload structure:
```json
{
  "object": "whatsapp_business_account",
  "entry": [{
    "id": "WABA_ID",
    "changes": [{
      "value": {
        "messaging_product": "whatsapp",
        "metadata": {
          "display_phone_number": "573151212223",
          "phone_number_id": "PHONE_NUMBER_ID"
        },
        "contacts": [{
          "profile": {"name": "Carlos Gomez"},
          "wa_id": "573001234567"
        }],
        "messages": [{
          "from": "573001234567",
          "id": "wamid.HBgL...",
          "timestamp": "1730160000",
          "type": "text",
          "text": {"body": "¿Tienen la Hilux 2022 en Boyacá?"}
        }]
      },
      "field": "messages"
    }]
  }]
}
```

- Mapping to `channel.Conversation`:
  - `ID`: `"wa-" + from` (e.g. `wa-573001234567`).
  - `ChannelID`: `"whatsapp"`.
  - `CustomerAlias`: `contacts[0].profile.name` (sanitized through `guard.RedactPII`).
  - `UpdatedAt`: converted from UNIX timestamp.
- Mapping to `channel.Message`:
  - `ID`: `wamid`.
  - `Sender`: `"customer"`.
  - `Text`: extracted from `text.body`, or placeholder `[media:image]` for non-text.
  - `Timestamp`: message timestamp.

### 3.4 HSM (Highly Structured Message) Pre-Approved Templates Catalog

Under Meta policies, if the customer's last message is older than 24 hours, free-form text cannot be dispatched. Only registered HSM templates can be delivered.

#### BuyaCAR HSM Catalog (`es_CO`):
1. **`buyacar_inventory_followup_v1`**:
   - Category: `UTILITY`.
   - Body: `"Hola {{1}}, te contactamos de BuyaCAR Tunja para informarte sobre la disponibilidad del vehículo {{2}}. ¿Deseas recibir la ficha técnica o agendar una prueba de manejo?"`
   - Buttons: Quick Replies: `["Ver Ficha Técnica", "Hablar con Asesor"]`.
2. **`buyacar_waitlist_alert_v1`**:
   - Category: `MARKETING`.
   - Body: `"¡Hola {{1}}! El vehículo {{2}} que estabas buscando en BuyaCAR Tunja ya ingresó a nuestro inventario certificado. Visítanos en la Av. Oriental - Cra. 6 #52-23."`
   - Buttons: `["Ver Vehículo", "No me interesa"]`.
3. **`buyacar_test_drive_confirmation_v1`**:
   - Category: `UTILITY`.
   - Body: `"Hola {{1}}, confirmamos tu cita de prueba de manejo para el vehículo {{2}} en BuyaCAR Tunja el día {{3}}. Si requieres reprogramar, responde a este mensaje."`

#### HSM Dispatch Handler:
When `Reply()` is called:
- Check `time.Since(lastCustomerMessageTimestamp) > 24*time.Hour`.
- If > 24h, format message payload with `type: "template"` referencing the pre-approved template name, language code `es_CO`, and dynamic parameters.

### 3.5 Media Message Handling

1. **Inbound Attachments**:
   - WhatsApp delivers an attachment ID (e.g. `image.id`).
   - Resolution process:
     1. Query Meta endpoint: `GET https://graph.facebook.com/v21.0/{media_id}`.
     2. Extract temporary download URL.
     3. Verify destination URL scheme (`https://`) and store media reference safely.
   - PII Guardrail: Media files are never stored in plain text directories or logged in traces.
2. **Outbound Media (Vehicle Photos / Inspection Certificates)**:
   - Vehicle photos are sent via `type: "image"` with `link: "https://buyacar.com.co/cdn/vehicles/{id}.jpg"`.
   - Domain Allowlist: The Guard enforces that media links strictly belong to `buyacar.com.co`.

### 3.6 Opt-In / Opt-Out Status Management (Ley 1581 & Meta Compliance)

1. **Keywords for Opt-Out**:
   - Customer messages matching `STOP`, `BAJA`, `NO MAS`, `CANCELAR`, `NO ENVIAR`, `DESUSCRIBIR` immediately trigger opt-out processing.
2. **Consent State Machine**:
   - States: `OptedIn`, `OptedOut`, `PendingConfirmation`.
   - Storage: Tracked in `store.Store` / SQLite with timestamp and source.
3. **Guard Enforcement**:
   - Before any outbound dispatch (`Reply`), the adapter verifies customer opt-in status.
   - If `OptedOut`, delivery is aborted, marked as `skipped: customer_opted_out`, and a final confirmation message is emitted if legally required.

---

## 4. Resilient Fallback to Playwright Browser Automation

### 4.1 Trigger Conditions & Transition Matrix

The WhatsApp Cloud API adapter must gracefully degrade to browser automation (Playwright/CDP) under specific conditions:

| Trigger Condition | Detection Mechanism | Action |
|---|---|---|
| **Meta Quota Exhaustion (429)** | HTTP Status 429 Too Many Requests | Failover to Playwright for current batch |
| **WABA Verification Pending** | HTTP Status 403 / Error code 131031 | Run in Playwright until WABA approved |
| **Tier Limit Reached (Tier 250)** | HTTP Error: "Daily message limit reached" | Failover to Playwright for remaining day |
| **Meta Graph API Outage** | 3 consecutive HTTP 5xx responses | Failover to Playwright before auto-pausing |
| **DOM Selector Failure** | VersionedSelector fallbacks exhausted | Pause channel and enqueue to `escalate.Queue` |

```
                       ┌────────────────────────────┐
                       │   Inbound/Outbound Request │
                       └─────────────┬──────────────┘
                                     │
                                     ▼
                      /──────────────────────────────\
                     <  Cloud API Healthy & Tier OK?  >
                      \──────────────────────────────/
                                     │
                            YES      │      NO (429, WABA Unverified, 5xx)
                   ┌─────────────────┴─────────────────┐
                   ▼                                   ▼
        ┌─────────────────────┐             ┌─────────────────────┐
        │ WhatsApp Cloud API  │             │ Playwright Browser  │
        │      Adapter        │             │     Automation      │
        └──────────┬──────────┘             └──────────┬──────────┘
                   │                                   │
                   │                                   ▼
                   │                        /──────────────────────\
                   │                       < VersionedSelector OK?  >
                   │                        \──────────────────────/
                   │                                   │
                   │                          YES      │      NO (exhausted)
                   │                                   ▼
                   │                        ┌─────────────────────┐
                   │                        │ Auto-Pause Channel  │
                   │                        │ Ticket to escalate  │
                   │                        └─────────────────────┘
                   │                                   │
                   ▼                                   ▼
        ┌─────────────────────────────────────────────────────────┐
        │        Shared RateController & ResilienceTracker        │
        └─────────────────────────────────────────────────────────┘
```

### 4.2 Browser Automation via `VersionedSelector`

WhatsApp Web's DOM layout evolves periodically. The adapter employs `VersionedSelector` defined in `resilience.go` for all critical user actions:

```go
var (
    // Search Box for selecting chat
    SelectorChatSearch = NewVersionedSelector(
        "wa-web-search-box",
        "2026.1",
        "div[contenteditable='true'][data-tab='3']",
        "div[role='textbox'][title='Buscar o iniciar un chat']",
        "div[data-testid='chat-list-search']",
        "#side div[contenteditable='true']",
    )

    // Message Input Field
    SelectorMessageInput = NewVersionedSelector(
        "wa-web-message-input",
        "2026.1",
        "footer div[contenteditable='true'][data-tab='10']",
        "div[data-testid='conversation-compose-box-input']",
        "footer div[role='textbox']",
        "div[title='Escribe un mensaje aquí']",
    )

    // Send Button
    SelectorSendButton = NewVersionedSelector(
        "wa-web-send-button",
        "2026.1",
        "button[data-testid='compose-btn-send']",
        "span[data-icon='send']",
        "footer button[aria-label='Enviar']",
        "span[data-testid='send']",
    )

    // Chat Message Bubbles
    SelectorMessageContainer = NewVersionedSelector(
        "wa-web-message-container",
        "2026.1",
        "div[data-testid='msg-container']",
        "div.message-in, div.message-out",
        "div[role='row']",
    )
)
```

### 4.3 Human Delay Simulation & Browser Safety

When operating in browser mode, anti-bot detection mitigation is paramount:
1. Employs `RateController.CalculateChatDelay(seed)` (2.2s to 4.1s).
2. Employs `RateController.CalculateChannelDelay(seed)` (1.7s to 3.2s).
3. Uses Playwright typing emulation with keystroke jitter (50ms - 120ms per character).
4. Monitors DOM readiness before typing and waits for the message bubble to appear with a checkmark before confirming `SendVerified`.

---

## 5. MercadoLibre Push Webhook Connector

### 5.1 Push Architecture & Topic Segregation

MercadoLibre's API features two completely distinct communication contexts with opposing policy requirements:

```
                            MercadoLibre Webhook
                         POST /webhooks/mercadolibre
                         Header: X-Signature (ts, v1)
                                      │
                                      ▼
                      ┌───────────────────────────────┐
                      │    HMAC-SHA256 Signature      │
                      │          Validation           │
                      └───────────────┬───────────────┘
                                      │
                                      ▼
                      /───────────────────────────────\
                     <      Notification Topic?        >
                      \───────────────────────────────/
                                      │
                   ┌──────────────────┴──────────────────┐
                   ▼                                     ▼
        Topic: "questions"                       Topic: "messages"
     (Pre-Sale on Publications)              (Post-Sale Orders/Packs)
                   │                                     │
                   ▼                                     ▼
   ┌──────────────────────────────┐      ┌──────────────────────────────┐
   │ Query GET /questions/{id}    │      │ Query GET /messages/{pack_id}│
   │ Fetch question text & item   │      │ Fetch conversation thread    │
   └──────────────┬───────────────┘      └──────────────┬───────────────┘
                  │                                     │
                  ▼                                     ▼
   ┌──────────────────────────────┐      ┌──────────────────────────────┐
   │ MercadoLibre Zero-Contact    │      │ Standard Guardrails Check    │
   │ Policy: Strip Phones/URLs    │      │ Allow Official Contact Info  │
   └──────────────┬───────────────┘      └──────────────┬───────────────┘
                  │                                     │
                  └──────────────────┬──────────────────┘
                                     │
                                     ▼
                        Core Omnichannel Pipeline
```

### 5.2 Cryptographic Signature Verification (`X-Signature`)

MercadoLibre signs webhooks using an `X-Signature` header formatted as:
`ts=1730160000,v1=9e8c3...`

#### Verification Protocol:
1. Extract `ts` and `v1` components.
2. Verify timestamp freshness: `|now - ts| <= 300 seconds` (prevents replay attacks).
3. Build template string: `id:{resource_id};request-id:{x-request-id};ts:{ts};`.
4. Calculate HMAC-SHA256 using MercadoLibre App Secret.
5. Perform constant-time comparison via `subtle.ConstantTimeCompare()`.

### 5.3 Policy Compliance & Pre-Sale Contact Filter

- **Strict ML Regulation**: MercadoLibre strictly forbids sharing direct contact information (phone numbers, WhatsApp numbers, email addresses, external URLs) in pre-sale question responses. Doing so results in listing cancellation or seller account suspension.
- **Custom ML Guard Policy**:
  When processing pre-sale questions (`topic == "questions"`):
  - Strip official dealership phone numbers (`3151212223`, `3022866969`, `3160502717`).
  - Strip external URLs (even `https://buyacar.com.co`).
  - Append standard MercadoLibre compliant footer:
    `"Por políticas de MercadoLibre no podemos compartir datos de contacto directos por esta sección. Te invitamos a dar clic en 'Contactar' en la publicación o visitarnos en nuestra sede en Tunja."`
  - Max Character Limit: 2000 characters.

### 5.4 Data Mapping to `channel.Conversation`

- **Pre-Sale Questions**:
  - `Conversation.ID`: `"ml-q-" + question_id`.
  - `Conversation.ChannelID`: `"mercadolibre"`.
  - `Conversation.CustomerAlias`: `"ML-Buyer-" + from.id`.
  - `Message.Text`: question text.
  - Outbound reply: calls `POST https://api.mercadolibre.com/answers` with `{"question_id": id, "text": replyText}`.
- **Post-Sale Messages**:
  - `Conversation.ID`: `"ml-m-" + pack_id`.
  - `Conversation.ChannelID`: `"mercadolibre"`.
  - Outbound reply: calls `POST https://api.mercadolibre.com/messages/packs/{pack_id}/sellers/{seller_id}`.

---

## 6. Unified Meta Omnichannel Adapter (Facebook Messenger & Instagram Direct)

### 6.1 Multi-Tenant Webhook Dispatcher

Facebook Messenger and Instagram Direct Messaging share Meta's Graph Webhook API. A single endpoint (`/webhooks/meta`) dispatches events based on the top-level `object` field:

```json
{
  "object": "page", // for Facebook Messenger
  "entry": [{
    "id": "PAGE_ID",
    "time": 1730160000,
    "messaging": [{
      "sender": {"id": "USER_PSID"},
      "recipient": {"id": "PAGE_ID"},
      "timestamp": 1730160000,
      "message": {
        "mid": "m_123456",
        "text": "¿El Renault Duster sigue en venta en Tunja?"
      }
    }]
  }]
}
```

```json
{
  "object": "instagram", // for Instagram Direct
  "entry": [{
    "id": "IG_ACCOUNT_ID",
    "time": 1730160000,
    "messaging": [{
      "sender": {"id": "IG_USER_ID"},
      "recipient": {"id": "IG_ACCOUNT_ID"},
      "timestamp": 1730160000,
      "message": {
        "mid": "m_ig789",
        "text": "¿Cuánto vale el Mazda CX-5?"
      }
    }]
  }]
}
```

### 6.2 Channel Identity & Limits Matrix

| Parameter | Facebook Messenger / Marketplace | Instagram Direct |
|---|---|---|
| **Channel ID** | `"facebook_marketplace"` / `"facebook_messenger"` | `"instagram"` |
| **Max Characters** | 500 characters | 450 characters |
| **Noise Cleaning** | Removes `MarketplaceNoise` ("¿sigue disponible?", etc.) | Removes story mention noise |
| **Rate Limits** | 120/h, 250/d | 120/h, 250/d |
| **Outbound Endpoint** | `POST https://graph.facebook.com/v21.0/me/messages` | `POST https://graph.facebook.com/v21.0/me/messages` |
| **Token Type** | Page Access Token | Instagram Page Access Token |

Both channels route through the identical triage pipeline (`triage.Classify`), inventory cache, Fast-Path, and AI orchestrator, ensuring uniform answers and zero duplicate code.

---

## 7. Universal RateController & ResilienceTracker Integration

### 7.1 Lifecycle Hook Integration Matrix

Every channel adapter connects to `RateController` and `ResilienceTracker` at specific lifecycle checkpoints:

```
 Inbound Webhook Event
           │
           ▼
 [ Checkpoint A: Verification ]
  HMAC Signature / Token Valid? ──(NO)──> Return HTTP 401/403
           │ (YES)
           ▼
 [ Checkpoint B: Channel Health ]
  ResilienceTracker.IsPaused(channelID)? ──(YES)──> Skip, Enqueue escalate ticket
           │ (NO)
           ▼
 [ Checkpoint C: Ingest & De-noise ]
  CleanIncomingNoise -> Map to Conversation -> Mark CP1
           │
           ▼
 [ Checkpoint D: Core Triage & Resolution ]
  Classify -> Fast-Path / AI Orchestrator -> Guardrails (CP2 - CP5)
           │
           ▼
 [ Checkpoint E: Pre-Send Rate Limit ]
  RateController.CanSend(channelID, now)? ──(NO)──> Log limit exceeded, Escalate
           │ (YES)
           ▼
 [ Checkpoint F: Mode Dispatch (Reply) ]
  SIMULATION: Record Simulated
  DRAFT:      Record in Draft Outbox
  AUTOPILOT:  Execute HTTP Call to Channel API
           │
           ├────────────────────────────┐
       (Success)                     (Failure)
           ▼                            ▼
 [ Checkpoint G: Success ]    [ Checkpoint H: Failure ]
  RateController.RecordSend    ResilienceTracker.RecordFailure(channelID, err)
  ResilienceTracker.Record     Is auto-paused (>= 3)?
  Success(channelID)            └──(YES)──> Escalate to escalate.Queue
  Persist CP7 Idempotency
```

### 7.2 Strict Channel Isolation

The `MemoryResilienceTracker` and `MemoryRateController` maintain per-channel state dictionaries keyed by `channelID`. Consequently:
- If `mercadolibre` suffers 3 consecutive API failures and auto-pauses, `whatsapp`, `instagram`, `facebook_marketplace`, and `email` continue running without interruption.
- Quotas are metered independently: 120 msg/h on WhatsApp does not consume quota from Instagram or MercadoLibre.

### 7.3 Human Unpause & Recovery SLA

When an auto-pause occurs:
1. `ResilienceTracker` sets `pausedState[channelID] = true`.
2. Emits an urgent ticket to `escalate.Queue`:
   - `ChannelID`: e.g. `"mercadolibre"`.
   - `Reason`: `"channel_auto_paused_consecutive_failures"`.
   - `Context`: contains sanitized failure message.
3. In the Web Console / Management API, a human supervisor reviews the error and invokes `tracker.Resume(channelID)`.
4. `Resume()` sets `paused = false` and resets `consecutiveFailures = 0`, immediately restoring message processing.

---

## 8. Proposed Architecture & Package Structure for R2

To preserve clean architecture and Go 1.23 standard library discipline without bloating dependencies, the implementation files in `app_build/internal/channel/` are structured as follows:

```
app_build/internal/channel/
├── channel.go              # Canonical Channel interface, Session, Conversation, Message, Thread
├── rate_controller.go      # MemoryRateController, sliding windows, SHA-256 jitter
├── resilience.go           # VersionedSelector, MemoryResilienceTracker, auto-pause
├── email_pilot.go          # Email Pilot for Stage B (DRAFT)
├── stub.go                 # In-memory testing stub
│
├── whatsapp_cloud.go       # WhatsApp Cloud API adapter (Meta Graph API)
├── whatsapp_templates.go   # Pre-approved HSM template catalog and parameter binding
├── whatsapp_browser.go     # Playwright / CDP fallback with VersionedSelector
├── whatsapp_composite.go   # Resilient facade switching Cloud API <-> Browser
│
├── mercadolibre.go         # MercadoLibre push webhook connector & pre-sale filter
│
├── meta_omnichannel.go     # Unified Facebook Messenger & Instagram Direct adapter
│
├── webhook_server.go       # Lightweight net/http server multiplexing webhook routes
│
├── channel_test.go         # Shared compliance test harness
├── whatsapp_cloud_test.go  # Unit & contract tests for WhatsApp Cloud
├── mercadolibre_test.go    # Unit & signature tests for MercadoLibre
└── meta_omnichannel_test.go# Unit tests for Messenger & Instagram
```

### 8.1 Sketch of the Webhook Multiplexer (`webhook_server.go`)

```go
package channel

import (
    "context"
    "net/http"
    "log/slog"
)

// WebhookServer manages HTTP endpoints for incoming provider notifications.
type WebhookServer struct {
    server *http.Server
    logger *slog.Logger
}

func NewWebhookServer(addr string, logger *slog.Logger) *WebhookServer {
    mux := http.NewServeMux()
    return &WebhookServer{
        server: &http.Server{
            Addr:    addr,
            Handler: mux,
        },
        logger: logger,
    }
}

func (s *WebhookServer) RegisterWebhook(path string, handler http.HandlerFunc) {
    if mux, ok := s.server.Handler.(*http.ServeMux); ok {
        mux.HandleFunc(path, handler)
    }
}

func (s *WebhookServer) Start(ctx context.Context) error {
    go func() {
        <-ctx.Done()
        _ = s.server.Shutdown(context.Background())
    }()
    if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        return err
    }
    return nil
}
```

---

## 9. Security, Privacy & Token Economy Compliance

1. **Ley 1581 de 2012 (PII Redaction)**:
   - Webhook logging is strictly sanitized. Customer telephone numbers (`wa_id`), names, and national identity numbers (cédulas) are redacted via `guard.RedactPII()` before logging or saving to `obs`.
2. **Anti-Hallucination & Allowlist**:
   - Outbound messages across all new channels (WhatsApp, ML, Messenger, Instagram) pass through `guard.Guard`.
   - Any figure with 5+ digits not present in the inventory cache is rejected.
   - Any external URL other than `https://buyacar.com.co` is stripped.
3. **Token Economy**:
   - The Fast-Path exact-match engine resolves ~70-80% of incoming inquiries (location, hours, contact, price, mileage, specs) in **2.59 µs** without making calls to LLMs (0 tokens).
   - Only complex queries pass to the AI orchestrator, maximizing token efficiency across WhatsApp, MercadoLibre, and Meta channels.

---

## 10. Summary Matrix for Implementers (Squad 1)

| Requirement | Implementation Component | Key Methods / Handlers | Primary Safety Rule |
|---|---|---|---|
| **WhatsApp Cloud API** | `whatsapp_cloud.go` | `HandleWebhook`, `SendTemplate`, `SendFreeText` | Reject unverified HMAC; 24h window for HSM |
| **WhatsApp Fallback** | `whatsapp_browser.go` | `FailoverToBrowser`, `ResolveSelector` | Use `VersionedSelector`; human jitter typing |
| **MercadoLibre Push** | `mercadolibre.go` | `HandleMLWebhook`, `AnswerQuestion` | Zero contact info in pre-sale questions |
| **Messenger / Instagram** | `meta_omnichannel.go` | `HandleMetaWebhook`, `ReplyMessenger` | Channel-specific length limit (450 vs 500) |
| **Rate Controller** | `rate_controller.go` | `CanSend`, `RecordSend`, `WaitWithContext` | 120/h, 250/d; SHA-256 jitter |
| **Resilience Tracker** | `resilience.go` | `RecordSuccess`, `RecordFailure`, `Resume` | Auto-pause at 3 failures; alert escalation |
