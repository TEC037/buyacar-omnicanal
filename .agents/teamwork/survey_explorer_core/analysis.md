# BuyaCAR Omnicanal Evolution — Core Infrastructure Survey Analysis (R1, R3, R4, R5)

**Date**: 2026-10-29  
**Agent**: Core Infrastructure Explorer (`survey_explorer_core`)  
**Scope**: `internal/core/`, `internal/store/`, `internal/crm/`, `internal/inventory/`, `internal/ai/`, `cmd/buyacar/`, `internal/obs/`, `internal/channel/`, `internal/guard/`, `internal/fast/`, `internal/escalate/`  
**Working Directory**: `c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\survey_explorer_core\`

---

## 1. Executive Summary & Baseline Health

A comprehensive technical survey of the `app_build/` codebase was conducted to evaluate the current architecture and identify the exact integration points for requirements **R1** (Core Governance & Safety), **R3** (Persistence, DMS Sync & CRM Integration), **R4** (Cognitive Layer & Free Inference Cascade), and **R5** (Embedded Web Console, Prometheus Metrics & CLI).

### Key Findings
1. **100% Test Suite Health**: The existing Go test suite passes completely without cached results (`go test -count=1 ./...` executes in ~12 seconds across all 15 packages).
2. **Standard Library Purity**: `app_build/go.mod` currently defines `module buyacar.com.co/omnicanal` under Go 1.23 with **zero** external dependencies.
3. **Toolchain & CGO Constraint**: The Windows host environment runs `go1.27.0` (with `go 1.23` language semantics) with `CGO_ENABLED=0` and **no C compiler** (`gcc` or `clang` are not in `PATH`). As a result, standard CGO-based SQLite drivers (`github.com/mattn/go-sqlite3`) **cannot compile**. Any SQLite implementation must use a 100% pure Go driver (e.g., `modernc.org/sqlite` via `database/sql`) or a pure Go transactional file/WAL storage engine, backed by an automated fallback to memory + flat JSON.
4. **Well-Defined Interfaces**: `store.Store`, `crm.Manager`, `ai.Provider`, `channel.Channel`, `channel.RateController`, `channel.ResilienceTracker`, and `inventory.Cache` are cleanly separated by Go interfaces and mutex-protected in-memory reference implementations.

---

## 2. Current Implementation Mapping

### 2.1 Core Pipeline & Governance (`internal/core/`)
- **`core.go`**:
  - `Config`: Controls `OperatingMode` (`SIMULATION`, `DRAFT`, `AUTOPILOT`), `EmergencyKillSwitch` (`bool`), `EnableFastPath` (`bool`), `ConfidenceThreshold` (`0.80`), `SkipOlderThanWeeks` (`9`), `LocalBindAddress` (`127.0.0.1`), `MaxMessagesPerHour` (`120`), `MaxMessagesPerDay` (`250`).
  - `DefaultSafeConfig()`: Enforces `ModeSimulation`.
  - `DefaultDraftConfig()`: Authorized for Etapa B human-supervised drafts.
  - `Engine`: Manages registered channels (`map[string]channel.Channel`), global kill switch (`sync/atomic.Bool`), and thread skipping rules via `ShouldProcessThread()`.
  - `ShouldProcessThread()` checks:
    1. Context cancellation.
    2. Emergency kill switch.
    3. Empty message threads.
    4. Outgoing loopback (`lastMsg.Sender == "own"`).
    5. Conversation age (> 9 weeks).
    6. Idempotency hash duplicate (`store.HasProcessed(hash)`).
- **`pipeline.go`**:
  - `PipelineOutcome`: Tracks conversation result, token usage, fast-path flag, escalation status, CRM task ID, checkpoints passed (0..7).
  - `Pipeline.ProcessConversation()` executes the 7 canonical checkpoints (CP1..CP7):
    - **CP1 (Lectura)**: `ch.Read()` + omission rules check (`cp1_lectura:ok`).
    - **CP2 (Triage)**: `triage.Classify(cleanedText)` (`cp2_triage:ok`).
    - **CP3 & CP4 (Inventario + Fast-Path)**: `Fast.Evaluate()` queries cache. If handled, generates deterministic reply (0 tokens, ~2.59 µs). If vehicle sold/reserved, emits commercial notice and escalates (`cp3_inventario:ok`, `cp4_respuesta:ok`). If unhandled, queries inventory and calls `AIOrch.Resolve()`.
    - **CP5 (Guard)**: `Guard.ValidateOutbox()` checks forbidden phrases, HTTPS allowlist, 5+ digit hallucinated numbers, and channel character limits (`cp5_guard:ok`).
    - **Step 11 (Revalidation Pre-Send)**: `RevalidatePreSend()` confirms inventory cache freshness (< 30 min) and vehicle availability (`available`).
    - **CP6 (Envio)**: `ch.Reply()` sends reply according to operating mode and asserts expected status (`SendSimulated`, `SendDrafted`, `SendVerified`). Updates `ResilienceTracker` (`cp6_envio:ok`).
    - **CP7 (CRM & Idempotencia)**: `settleCRMTasks()` creates CRM tasks, updates store with `store.MarkProcessed()` containing `currentETag` and `CRMTaskID` (`cp7_crm:ok`).

### 2.2 Persistence Layer (`internal/store/`)
- **`store.go`**:
  - `IdempotencyRecord`:
    ```go
    type IdempotencyRecord struct {
        Hash       string    `json:"hash"`
        ChannelID  string    `json:"channel_id"`
        ConvID     string    `json:"conv_id"`
        ReplyText  string    `json:"reply_text"`
        CreatedAt  time.Time `json:"created_at"`
        SendStatus string    `json:"send_status,omitempty"`
        ETag       string    `json:"etag,omitempty"`
        CRMTaskID  string    `json:"crm_task_id,omitempty"`
    }
    ```
  - `Store` Interface:
    ```go
    type Store interface {
        ComputeHash(channelID, convID, lastIncomingMessage string) string
        HasProcessed(hash string) (bool, IdempotencyRecord)
        MarkProcessed(record IdempotencyRecord) error
        SealIdempotency(hash string, sendStatus string, etag string, crmTaskID string) error
        SaveCheckpointState(convID string, checkpoint string) error
        GetCheckpointState(convID string) (string, bool)
    }
    ```
  - `MemoryStore`: In-memory implementation protected by `sync.RWMutex`.
  - **Current Gap**: Lacks on-disk ACID persistence (SQLite) and local flat JSON fallback.

### 2.3 CRM Management (`internal/crm/`)
- **`crm.go`**:
  - `TaskType`: `schedule_test_drive`, `commercial_followup`, `credit_application`, `general_inquiry`.
  - `TaskStatus`: `pending`, `completed`, `escalated`, `cancelled`.
  - `Task`:
    ```go
    type Task struct {
        ID          string     `json:"id"`
        ConvID      string     `json:"conv_id"`
        ChannelID   string     `json:"channel_id"`
        Type        TaskType   `json:"type"`
        Status      TaskStatus `json:"status"`
        Description string     `json:"description"`
        VehicleID   string     `json:"vehicle_id,omitempty"`
        CreatedAt   time.Time  `json:"created_at"`
        UpdatedAt   time.Time  `json:"updated_at,omitempty"`
    }
    ```
  - `Manager` Interface:
    ```go
    type Manager interface {
        RecordTask(ctx context.Context, task Task) error
        GetTasksByConv(convID string) []Task
        GetTask(ctx context.Context, id string) (Task, error)
        ListTasks(ctx context.Context) []Task
        UpdateTaskStatus(ctx context.Context, id string, status TaskStatus) error
    }
    ```
  - `MemoryManager`: In-memory implementation with index by ID and conversation.
  - Deterministic helpers: `NormalizeTaskType(raw string)`, `InferTaskFromTriage(intent, hasVehicle)`.
  - **Current Gap**: Lacks persistent storage (SQLite/JSON) and outgoing HMAC SHA-256 webhook exporter for external CRMs.

### 2.4 Inventory & DMS Synchronization (`internal/inventory/`)
- **`inventory.go`**:
  - `Vehicle`: Comprehensive struct containing ID, Brand, Model, Year, PriceCOP, MileageKm, Transmission, Engine, Traction, Color, Status ("available", "sold", "reserved"), OfficialURL.
  - `Cache`:
    - Manages in-memory snapshot with `sync.RWMutex`.
    - `CheckVehicleAvailability(id, now)`: Enforces freshness (< 30 min) and availability status.
    - `IsFresh(now)`: Validates `now.Sub(lastUpdated) <= 30 * time.Minute`.
    - `Search(ctx, q, now)`: Deterministic filtering returning `StatusExactMatch`, `StatusMultipleMatches`, `StatusNoMatch`, `StatusStale`, `StatusUnavailable`, `StatusVehicleNotRequired`.
- **`http_fetcher.go`**:
  - `HTTPFetcher`: Connects to `OfficialInventoryEndpoint` (`https://buyacar.com.co/wp-json/vehica/v1/cars`).
  - Supports conditional HTTP headers: `If-None-Match` (ETag) and `If-Modified-Since`.
  - Handles `304 Not Modified` via `cache.TouchRefresh(now)`.
  - Handles `200 OK` via `cache.LoadSnapshot(vehicles, etag, now)`.
  - Enforces HTTPS allowlist validation (`buyacar.com.co`) and 5MB payload limit.
  - **Current Gap**: Operates only on-demand; lacks a continuous background sync worker (< 30 min), a real-time invalidation webhook endpoint, and an automated SHA-256 sealed waitlist.

### 2.5 Cognitive Layer & AI Providers (`internal/ai/`)
- **`ai.go`**:
  - `StructuredOutput`: Strict schema with `config_id`, `trace`, `proposed_reply`, `intent`, `confidence`, `conversation_summary`, `crm_update`, `governance`.
  - `ParseStrictJSON(raw []byte)`: Strict parser rejecting unknown JSON fields and verifying RFC3339 timestamps and confidence bounds [0.0, 1.0].
  - `Provider` Interface:
    ```go
    type Provider interface {
        Name() string
        GenerateJSON(ctx context.Context, prompt skills.CompactContext) ([]byte, int, int, error)
    }
    ```
  - `Orchestrator`: Implements single-retry on invalid JSON and circuit breaker (opens at 3 consecutive failures).
- **`providers.go`**:
  - `BuildDelimitedPrompt(compact)`: Delimits system instructions, redacts PII, and bounds vehicle context (1..3 vehicles).
  - `OllamaProvider`: Connects to `http://127.0.0.1:11434/api/generate` with model `llama3.2:3b`. Enforces loopback host check.
  - `GeminiProvider`: Connects to `https://generativelanguage.googleapis.com` using `GEMINI_API_KEY`.
- **`skills/skills.go`**:
  - `BuildCompactContext()`: Compresses history and limits vehicles to max 3 (budget: 1500 in / 300 out).
  - `ApprovedCache`: Simple key-value store with hit/miss counters.
- **`fast/fast.go`**:
  - Fast-Path Engine: Resolves general intents (hours, location, greeting, contact) and exact-match inventory queries at ~2.59 µs/op without calling AI.
- **Current Gap**:
  - In `pipeline.go:160`, `AIOrch.Resolve(ctx, "", compact)` passes an empty string `""` as cacheKey.
  - Lacks semantic hashing for query deduplication.
  - Provider cascade order needs alignment with R4: Level 1 (Local Ollama), Level 2 (Cloud Free: Groq/Gemini), Level 3 (Deterministic Fast-Path).

### 2.6 Server & Command Line (`cmd/buyacar/main.go`, `internal/obs/`)
- **`cmd/buyacar/main.go`**:
  - Runs a synchronous batch script for `ModeDraft` or `ModeSimulation`, processes stub conversations, and logs output.
  - **Current Gap**: Does not run an HTTP server; lacks embedded web console (`embed.FS`), `/metrics` endpoint, and JSON CLI inspection mode.
- **`internal/obs/obs.go`**:
  - `MetricsCollector`: Tracks total conversations, resolutions without AI/human, escalations by reason, token counts, and fast-path latencies (p50, p95, p99).
  - `ConversationTrace`: Formats the 7 checkpoints into Markdown tables with zero PII.
  - **Current Gap**: Lacks Prometheus text format exporter (`text/plain; version=0.0.4`) and JSON serialization for CLI output.

---

## 3. Deep-Dive Gap Analysis & Integration Points

### 3.1 Requirement R1: Core Rules, Governance & Safety
| Aspect | Current Status | Integration / Enforcement Point |
|---|---|---|
| **Operating Modes** | `SIMULATION`, `DRAFT`, `AUTOPILOT` defined in `internal/channel/channel.go:18`. Verified in `pipeline.go:272`. | Must remain default `SIMULATION`. Mode transition to `DRAFT` conditioned on human authorization. |
| **PII Redaction (Ley 1581)** | `guard.RedactPII()` masks emails and IDs/phones (excluding official lines). | Active in `escalate.Queue.Enqueue()`, `providers.BuildDelimitedPrompt()`, and `obs.RecordCheckpoint()`. Ensure active in CRM webhooks and web console. |
| **Anti-Hallucination** | `guard.ValidateOutbox()` blocks 5+ digit numbers not in inventory or official lines, and validates HTTPS allowlist to `buyacar.com.co`. | Enforced at Checkpoint CP5 (`pipeline.go:177`). |
| **Rate Controller** | Sliding window limits (120 msg/h, 250 msg/d) and SHA-256 deterministic jitter (`±30%`) in `channel/rate_controller.go`. | Verified at boundary conditions. |
| **Consecutive Failures** | `channel/resilience.go` pauses channel on 3 consecutive failures and enqueues alert to `escalate.Queue`. Suppresses alert storms. | Enforced in `pipeline.go:91, 243, 258`. |

### 3.2 Requirement R3: Persistence, DMS Synchronization & CRM Integration
#### A. SQLite Persistence with Automatic Fallback
1. **Compilation Environment**:
   - `CGO_ENABLED=0` on Windows AMD64.
   - `github.com/mattn/go-sqlite3` will fail compilation due to lack of CGO.
   - **Recommended Pure Go Solution**: Use `modernc.org/sqlite` via Go standard library `database/sql`, OR implement a resilient storage adapter that uses `database/sql` when a pure Go driver is registered, combined with an automatic failover to `store.JSONFileStore` and `crm.JSONFileManager` (local flat JSON append/snapshot log with file locking).
2. **Database Schemas**:
   ```sql
   -- Idempotency Records
   CREATE TABLE IF NOT EXISTS idempotency_records (
       hash TEXT PRIMARY KEY,
       channel_id TEXT NOT NULL,
       conv_id TEXT NOT NULL,
       reply_text TEXT NOT NULL,
       created_at DATETIME NOT NULL,
       send_status TEXT,
       etag TEXT,
       crm_task_id TEXT
   );

   -- Checkpoints
   CREATE TABLE IF NOT EXISTS checkpoints (
       conv_id TEXT PRIMARY KEY,
       checkpoint TEXT NOT NULL,
       updated_at DATETIME NOT NULL
   );

   -- CRM Tasks
   CREATE TABLE IF NOT EXISTS crm_tasks (
       id TEXT PRIMARY KEY,
       conv_id TEXT NOT NULL,
       channel_id TEXT NOT NULL,
       type TEXT NOT NULL,
       status TEXT NOT NULL,
       description TEXT NOT NULL,
       vehicle_id TEXT,
       created_at DATETIME NOT NULL,
       updated_at DATETIME
   );
   ```
3. **Resilience & Fallback Architecture**:
   - `ResilientStore` wraps `SQLiteStore` and `MemoryStore`/`JSONStore`.
   - On database error (disk full, lock timeout, corruption), automatically fall back to memory + JSON flat file and enqueue a ticket in `escalate.Queue` (`reason: "persistence_fallback_to_json"`).

#### B. DMS Synchronization with ETag (< 30 min) & Invalidation Webhook
1. **Periodic DMS Worker**:
   - Background ticker (`time.NewTicker(15 * time.Minute)`) running `http_fetcher.Sync(ctx, cache, time.Now())`.
   - Sends `If-None-Match: <etag>` and `If-Modified-Since`.
   - On `304 Not Modified`, calls `cache.TouchRefresh()`.
   - On `200 OK`, updates snapshot with new ETag.
2. **Real-Time Invalidation Webhook**:
   - HTTP POST endpoint `/api/v1/inventory/invalidate` in the embedded server.
   - Accepts payload specifying updated/depleted vehicle IDs or general inventory invalidation signal.
   - Immediately triggers `fetcher.Sync()` or updates vehicle status to `"sold"`/`"reserved"` in real time.
   - Triggers waitlist evaluation for newly available vehicles.

#### C. Waitlist Management with SHA-256 Sealing
1. **Waitlist Structure**:
   ```go
   type WaitlistEntry struct {
       ID              string    `json:"id"`
       CustomerContact string    `json:"customer_contact"` // PII-redacted or hashed in logs
       DesiredBrand    string    `json:"desired_brand"`
       DesiredModel    string    `json:"desired_model"`
       MaxBudgetCOP    int64     `json:"max_budget_cop"`
       CreatedAt       time.Time `json:"created_at"`
       SealedHash      string    `json:"sealed_hash"` // SHA-256(contact + brand + model + budget + salt)
   }
   ```
2. **Workflow**:
   - When a vehicle query results in `vehicle_sold_or_reserved` or `no_match`, customer interest is captured into `WaitlistEntry`.
   - Sealed using `crypto/sha256`.
   - When DMS sync or invalidation webhook detects a vehicle transitioning to `available`, or a newly added vehicle matching the criteria:
     - Match is verified against the SHA-256 sealed hash.
     - A high-priority commercial CRM task is recorded (`TaskCommercialFollowup`).

#### D. Outgoing CRM Webhook with HMAC SHA-256 & Exponential Backoff
1. **Webhook Exporter (`crm.WebhookExporter`)**:
   - Emits outgoing HTTP POST to external CRM endpoint.
   - Generates signature: `X-BuyaCAR-Signature: hex(HMAC_SHA256(secret, payload))`.
   - Payload includes task metadata, conversation ID, channel ID, and lead context (PII-handled according to CRM contract).
2. **Exponential Backoff**:
   - Attempts: 1 to 4 with intervals 500ms, 1s, 2s, 4s.
   - If all retries fail, automatically enqueues to `escalate.Queue` (`reason: "crm_export_webhook_failed"`) for human intervention.

---

### 3.3 Requirement R4: Cognitive Layer & Free Inference Cascade
#### A. Semantic Hash Cache
1. **Semantic Hashing**:
   - Input normalization: trim, lowercase, remove diacritics, strip punctuation, clean conversational filler.
   - Compute `hash = sha256(normalized_query + "|" + extracted_intent)`.
2. **Integration into Pipeline**:
   - Replace the `""` cacheKey in `pipeline.go:160` with `semanticHash`.
   - Check `ApprovedCache` or `SemanticCache` before LLM invocation.
   - If match found, return cached response with `0 tokens`, latency `< 10 µs`, and increment cache hit metric.

#### B. 3-Tier Free Inference Cascade
```
[ Incoming Query ]
        │
        ▼
[ Level 3 Pre-Filter: Fast-Path ] ────────► Exact-match / Company Info (0 tokens, ~2.59 µs)
        │ (Complex query / Unresolved)
        ▼
[ Semantic Hash Cache ] ──────────────────► Verified Cache Hit (0 tokens, < 10 µs)
        │ (Cache Miss)
        ▼
[ Level 1: Free Local (Ollama) ] ─────────► Ollama llama3.2:3b @ 127.0.0.1:11434 (0 cloud cost)
        │ (Ollama down / timeout)
        ▼
[ Level 2: Free Cloud (Groq / Gemini) ] ──► Groq llama-3.1-8b / Gemini 2.5 Flash Free Tier
        │ (Cloud quota exceeded / down)
        ▼
[ Level 3 Fallback / Human Escalation ] ──► Fallback to Fast-Path safe message or escalate.Queue
```
1. **Provider Implementations**:
   - **Ollama**: Exists in `internal/ai/providers.go:70` (`llama3.2:3b`, loopback check).
   - **Gemini Free**: Exists in `internal/ai/providers.go:173`.
   - **Groq Free**: Add `GroqProvider` using OpenAI-compatible API (`https://api.groq.com/openai/v1/chat/completions`) with free tier model `llama-3.1-8b-instant`.
2. **Schema & Token Budgets**:
   - All providers must conform to `ParseStrictJSON()` and respect token budgets (max 1500 input / max 300 output).

---

### 3.4 Requirement R5: Embedded Web Console, Observability & CLI
#### A. Embedded Web Console (`net/http` + `embed.FS`)
1. **Asset Embedding**:
   - Use standard library `//go:embed web/*` to embed HTML, CSS, and JS directly into the compiled Go binary.
2. **Endpoints**:
   - `GET /`: Serves embedded single-page dashboard.
   - `GET /api/v1/health`: Returns system status, operating mode, kill switch state.
   - `GET /api/v1/tickets`: Returns pending tickets from `escalate.Queue` (with PII redacted).
   - `POST /api/v1/drafts/{id}/approve`: 1-click human approval of recorded DRAFT responses.
   - `GET /api/v1/channels`: Returns channel statuses, consecutive failure counts, paused flags.
   - `POST /api/v1/channels/{id}/resume`: Unpauses channel and resets failure counters.
   - `GET /api/v1/inventory`: Returns current inventory count, freshness, ETag.
   - `POST /api/v1/inventory/invalidate`: Webhook for DMS real-time invalidation.

#### B. Prometheus-Compatible Metrics Endpoint (`/metrics`)
- Standard text exposition format (`text/plain; version=0.0.4`):
  ```prometheus
  # HELP buyacar_fast_path_latency_seconds Latency of Fast-Path evaluations
  # TYPE buyacar_fast_path_latency_seconds summary
  buyacar_fast_path_latency_seconds{quantile="0.5"} 0.0000026
  buyacar_fast_path_latency_seconds{quantile="0.95"} 0.0000050
  buyacar_fast_path_latency_seconds{quantile="0.99"} 0.0007150

  # HELP buyacar_cache_hits_total Number of semantic cache hits
  # TYPE buyacar_cache_hits_total counter
  buyacar_cache_hits_total 42

  # HELP buyacar_cache_misses_total Number of semantic cache misses
  # TYPE buyacar_cache_misses_total counter
  buyacar_cache_misses_total 18

  # HELP buyacar_channel_paused Channel pause state (1=paused, 0=healthy)
  # TYPE buyacar_channel_paused gauge
  buyacar_channel_paused{channel="whatsapp"} 0
  buyacar_channel_paused{channel="email_pilot"} 0

  # HELP buyacar_channel_consecutive_failures Consecutive failure count
  # TYPE buyacar_channel_consecutive_failures gauge
  buyacar_channel_consecutive_failures{channel="whatsapp"} 0

  # HELP buyacar_conversations_total Total conversations processed
  # TYPE buyacar_conversations_total counter
  buyacar_conversations_total{status="resolved_without_ai"} 150
  buyacar_conversations_total{status="escalated"} 12

  # HELP buyacar_tokens_total Token usage
  # TYPE buyacar_tokens_total counter
  buyacar_tokens_total{direction="input"} 12450
  buyacar_tokens_total{direction="output"} 2100
  ```

#### C. CLI Mode Structured in JSON
- Extend `cmd/buyacar/main.go` with flag handling (`flag` package from stdlib):
  - `buyacar --mode=[SIMULATION|DRAFT|AUTOPILOT]`
  - `buyacar --server --port=8080 --bind=127.0.0.1` (starts web console, DMS worker, and channel listener)
  - `buyacar --metrics` (outputs current Prometheus or JSON metrics to stdout and exits)
  - `buyacar --inspect` (outputs JSON status including health, channels, queue, and inventory)

---

## 4. Build Requirements & Toolchain Feasibility

| Item | Status | Evaluation |
|---|---|---|
| **Go Version** | Go 1.23 standard | Fully supported (`go.mod` specifies `go 1.23`; environment has `go1.27.0`). |
| **CGO** | `CGO_ENABLED=0` | CGO is disabled; `gcc`/`clang` are missing. **Do NOT use `mattn/go-sqlite3`**. |
| **Pure Go SQLite** | Feasible | `modernc.org/sqlite` can be introduced via ADR, or a pure Go transactional file/WAL storage engine with `database/sql` driver abstraction can be used. |
| **External Dependencies** | Currently 0 | All core components operate with Go standard library (`net/http`, `crypto/sha256`, `crypto/hmac`, `embed`, `sync`, `log/slog`, `encoding/json`). |
| **Race Detector** | Requires CGO | `go test -race` cannot run without a C compiler on Windows; however, thread safety is verified via concurrency stress tests with mutex-locked state. |

---

## 5. Architectural Recommendations for Implementation Squads

1. **Squad 2 (Persistence & DMS/CRM - R3)**:
   - Create `internal/store/sqlite.go` implementing `store.Store` using `database/sql` + pure Go SQLite driver or JSON WAL, wrapped in `ResilientStore` with automated fallback to `MemoryStore` + flat JSON.
   - Create `internal/crm/sqlite.go` and `internal/crm/exporter.go` (HMAC SHA-256 webhook with exponential backoff).
   - Implement `internal/inventory/sync_worker.go` for background DMS sync (< 30 min) and `internal/inventory/waitlist.go` for SHA-256 sealed waitlist.
2. **Squad 3 (Cognitive Layer & Free Inference - R4)**:
   - Create `internal/ai/semantic_cache.go` with deterministic hashing.
   - Create `internal/ai/groq.go` for free-tier Groq inference.
   - Create `internal/ai/cascade.go` orchestrating L1 (Ollama) -> L2 (Groq/Gemini) -> L3 (Fast-path).
   - Wire `semanticHash` into `pipeline.go:160`.
3. **Squad 4 (Embedded Console & Observability - R5)**:
   - Create `internal/web/console.go` using `embed.FS` with clean, modern HTML/Tailwind assets.
   - Implement HTTP handlers for tickets, 1-click draft approvals, channel resume, and inventory invalidation.
   - Implement `internal/obs/prometheus.go` for `/metrics` exposition.
   - Refactor `cmd/buyacar/main.go` to support `--server`, `--metrics`, and `--inspect` JSON CLI modes.
4. **Squad 1 (Gateways & Webhooks - R2)**:
   - Implement WhatsApp Business Cloud API webhook handler with signature verification.
   - Implement MercadoLibre webhook handler with cryptographic signature verification.
   - Integrate with the unified `RateController` and `ResilienceTracker`.
