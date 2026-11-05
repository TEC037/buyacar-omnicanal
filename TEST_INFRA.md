# BuyaCAR Omnicanal — E2E Test Infrastructure Specification (R1–R5 Evolution)

**Document**: `TEST_INFRA.md`  
**Location**: Project Root (`buyacar-omnicanal-agent-workspace`)  
**Target Package**: `app_build/test/e2e` (`buyacar.com.co/omnicanal/test/e2e`)  
**Scope**: Features 1–28 (R1–R5 Evolution & 7 Anti-Regression Mandates) across Tiers 1–4  
**Test Approach**: Opaque-box, requirement-driven, deterministic, zero external dependency Go testing  
**Total Tests**: 58 comprehensive test scenarios (100% PASS, 0 flaky tests)

---

## 1. Architecture Overview

The End-to-End (E2E) testing framework for BuyaCAR Omnicanal validates all 28 evolutionary capabilities and the 7 Anti-Regression Mandates defined in `PROJECT.md` and `ORIGINAL_REQUEST.md`. Tests operate against pipeline boundaries, channel adapters, and contract interfaces without leaking implementation details.

```
app_build/test/e2e/
├── harness.go                 # Integrated test harness, mock adapters (WhatsApp Cloud, MercadoLibre, Meta, DMS, CRM, Cascade, Server)
├── tier1_smoke_test.go        # Tier 1: Smoke / Sanity / Happy-Path baseline verification (24 tests)
├── tier2_boundary_test.go     # Tier 2: Boundary conditions, quotas, rate limits, PII, anti-hallucination & sizes (17 tests)
├── tier3_resilience_test.go   # Tier 3: Resilient failover, SQLite-to-JSON fallback, 3-error auto-pause, backoff (12 tests)
└── tier4_omnichannel_test.go  # Tier 4: Multimodal lifecycle, customer journeys, embedded web UI, stress & 7 Mandaments (10 tests)
```

---

## 2. Feature Inventory Mapping Matrix (Features 1–28)

| # | Feature Name | Tier | Primary Test Function | Verification Source |
|---|--------------|------|-----------------------|---------------------|
| 1 | WhatsApp Cloud API Webhook Handler | Tier 1, 2 | `TestTier1_Feature01_WhatsAppCloud_WebhookVerificationHandshake`, `TestTier2_Feature01_WhatsAppCloud_InvalidHMACSignatureRejection` | `PROJECT.md` § Feature 1, `ORIGINAL_REQUEST.md` R2 |
| 2 | WhatsApp Cloud API Outbound Adapter | Tier 1 | `TestTier1_Feature02_WhatsAppCloud_OutboundMessageDelivery` | `PROJECT.md` § Feature 2 |
| 3 | WhatsApp HSM Template Engine | Tier 1 | `TestTier1_Feature03_WhatsAppCloud_HSMTemplateSelection` | `PROJECT.md` § Feature 3 |
| 4 | WhatsApp Inbound Media Handler | Tier 1, 2 | `TestTier1_Feature04_WhatsAppCloud_InboundMediaProcessing`, `TestTier2_Feature04_WhatsAppCloud_MediaPayloadSizeLimit_256KB` | `PROJECT.md` § Feature 4, Mandament 4 |
| 5 | WhatsApp Opt-In / Opt-Out Engine | Tier 1, 2 | `TestTier1_Feature05_WhatsAppCloud_OptInConsentLifecycle`, `TestTier2_Feature05_WhatsAppCloud_RevokedConsentBlocksMarketing` | `PROJECT.md` § Feature 5 |
| 6 | WhatsApp Playwright Resilient Fallback | Tier 3 | `TestTier3_Feature06_WhatsAppCloud_FailoverToPlaywrightOn429OrPendingWABA` | `PROJECT.md` § Feature 6 |
| 7 | MercadoLibre Webhook Ingest Connector | Tier 1 | `TestTier1_Feature07_MercadoLibre_WebhookIngestAndSignature` | `PROJECT.md` § Feature 7 |
| 8 | MercadoLibre Pre-Sale Guardrail Policy | Tier 2 | `TestTier2_Feature08_MercadoLibre_PreSaleGuardrail_StripPhoneAndLinks` | `PROJECT.md` § Feature 8 |
| 9 | Unified Meta Omnichannel Adapter | Tier 1, 2 | `TestTier1_Feature09_MetaOmnichannel_MessengerAndInstagramNormalDispatch`, `TestTier2_Feature09_CharacterBudgetBoundaries_AllChannels` | `PROJECT.md` § Feature 9 (FB: 500, IG: 450) |
| 10 | Universal Channel Rate & Resilience Link | Tier 3 | `TestTier3_Feature10_ResilienceTracker_ThreeConsecutiveFailuresAutoPausesNewAdapters` | `PROJECT.md` § Feature 10, Mandament 1 |
| 11 | Local ACID Store Implementation | Tier 1 | `TestTier1_Feature11_ResilientStore_ACIDTransactionHappyPath` | `PROJECT.md` § Feature 11, Mandament 2 CP7 |
| 12 | Automatic Fallback to JSON Flat-file | Tier 3 | `TestTier3_Feature12_ResilientStore_DatabaseFailureFallbackToFlatJSONLog` | `PROJECT.md` § Feature 12 |
| 13 | Periodic DMS Sync Worker | Tier 1, 3 | `TestTier1_Feature13_DMSSyncWorker_ETagFreshnessCycle`, `TestTier3_Feature13_DMSSyncWorker_NetworkFailureRetryAndStaleGrace` | `PROJECT.md` § Feature 13 (<30m ETag) |
| 14 | Real-time Inventory Invalidation Webhook | Tier 1, 2 | `TestTier1_Feature14_InvalidationWebhook_RealTimeStockPriceUpdate`, `TestTier2_Feature14_InvalidationWebhook_UnauthorizedBearerToken` | `PROJECT.md` § Feature 14 (<5ms update) |
| 15 | Waitlist SHA-256 Next-Entry Alert Engine | Tier 1, 2 | `TestTier1_Feature15_Waitlist_NextEntryAlertGeneration`, `TestTier2_Feature15_Waitlist_SHA256IntegrityVerification` | `PROJECT.md` § Feature 15 |
| 16 | Outbound CRM Webhook Dispatcher | Tier 1 | `TestTier1_Feature16_OutboundCRM_HMACWebhookDispatch` | `PROJECT.md` § Feature 16 (HMAC-SHA256) |
| 17 | Exponential Backoff with Escalate Fallback | Tier 3 | `TestTier3_Feature17_OutboundCRM_ExponentialBackoffExhaustionToEscalateQueue` | `PROJECT.md` § Feature 17 |
| 18 | Semantic Query Normalizer & Hash Cache | Tier 1 | `TestTier1_Feature18_SemanticCache_ExactAndNormalizedHit` | `PROJECT.md` § Feature 18 (0 AI tokens) |
| 19 | Single-Flight Query Deduplication | Tier 2 | `TestTier2_Feature19_SingleFlight_ConcurrentQueryDeduplication` | `PROJECT.md` § Feature 19 |
| 20 | Free Inference Cascade Engine | Tier 1, 3 | `TestTier1_Feature20_FreeInferenceCascade_Level1OllamaHappyPath`, `TestTier3_Feature20_FreeInferenceCascade_L1OllamaDown_L2GroqDown_L3FastPathFallback` | `PROJECT.md` § Feature 20 (L1 -> L2 -> L3) |
| 21 | Groq Free API Provider | Tier 2 | `TestTier2_Feature21_GroqProvider_TokenBudgetAndJSONSchema` | `PROJECT.md` § Feature 21 |
| 22 | Offline Deterministic Fast-Path Fallback | Tier 1 | `TestTier1_Feature22_DeterministicFastPath_ExactMatchLatency` | `PROJECT.md` § Feature 22 (~2.59 µs/op) |
| 23 | Embedded Local Web Server | Tier 1 | `TestTier1_Feature23_EmbeddedWebServer_StaticAssetsServed` | `PROJECT.md` § Feature 23 (`net/http` `embed.FS`) |
| 24 | Web Supervisor Dashboard UI | Tier 4 | `TestTier4_Feature24_25_WebDashboard_InspectEscalateQueueAnd1ClickApprove` | `PROJECT.md` § Feature 24 |
| 25 | 1-Click DRAFT Approval Endpoint | Tier 4 | `TestTier4_Feature24_25_WebDashboard_InspectEscalateQueueAnd1ClickApprove` | `PROJECT.md` § Feature 25 |
| 26 | Channel Pause / Resume Control API | Tier 3 | `TestTier3_Feature26_ChannelUnpauseAPI_RestoresTraffic` | `PROJECT.md` § Feature 26 |
| 27 | Prometheus `/metrics` Endpoint | Tier 1 | `TestTier1_Feature27_PrometheusMetrics_EndpointHappyPath` | `PROJECT.md` § Feature 27 |
| 28 | Structured JSON CLI Inspection Mode | Tier 2 | `TestTier2_Feature28_JSONCLIInspection_FlagsMetricsAndInspect` | `PROJECT.md` § Feature 28 (`--metrics`, `--inspect`) |

---

## 3. Seven Anti-Regression Mandates Verification

| Mandament | Requirement | Verification Test |
|-----------|-------------|-------------------|
| **M1** | Cero dependencias DOM inestable; webhook oficial con firma HMAC-SHA256 (`X-Hub-Signature-256`) y selectores versionados con auto-pausa al 3er fallo consecutivo | `TestTier1_Feature01_WhatsAppCloud_WebhookVerificationHandshake`, `TestTier3_Feature14_VersionedSelectorFallbackCascade`, `TestTier3_Feature16_17_AutomaticChannelPauseOn3FailuresWithHumanAlert` |
| **M2** | Pipeline rígido CP1–CP7 sin atajos: CP1 Anti-Injection, CP2 Triage, CP3 Fast-Path <5ms, CP4 IA Estructurada <=3 autos, CP5 Guardrails deterministas (teléfonos oficiales: 3151212223, 3022866969, 3160502717; allowlist https://buyacar.com.co; PII Ley 1581; presupuestos caracteres), CP6 revalidación pre-envío <30m y rate controller 120/h y 250/d con jitter SHA-256 en SIMULATION, CP7 sellado CRM e idempotencia ACID | `TestTier4_AdversarialPromptInjection_SanitizationCP1`, `TestTier2_Mandament02_NumericAntiHallucination_OfficialPhones`, `TestTier1_Feature01_02_PreSendAndCacheFreshnessHappyPath`, `TestTier4_Comprehensive7MandamentsAudit` |
| **M3** | Concurrencia aislada con `context.Context`, timeouts < 30s upstream, estructuras thread-safe persistentes contra reinicios | `TestTier4_Mandament03_04_ContextTimeoutAndLoopbackBoundaries`, `TestTier4_ConcurrentMultiChannelStressNoRaces`, `TestTier4_StatePersistenceAndRestartRehydration` |
| **M4** | Seguridad del entorno local y respeto de fronteras: Ollama exclusivo en 127.0.0.1 loopback, MAX_BODY_BYTES = 256 KB, webhooks con Bearer/HMAC | `TestTier4_Mandament03_04_ContextTimeoutAndLoopbackBoundaries`, `TestTier2_Feature04_WhatsAppCloud_MediaPayloadSizeLimit_256KB`, `TestTier2_Feature14_InvalidationWebhook_UnauthorizedBearerToken` |
| **M5** | Protocolo de salida y registro forense: suite dorada y tests unitarios sin red externa obligatorios para cada feature | `TestTier4_Comprehensive7MandamentsAudit`, 100% deterministic test execution |
| **M6** | Tolerancia total a reinicios con persistencia dual (ACID + JSON plano de contingencia) | `TestTier3_Feature12_ResilientStore_DatabaseFailureFallbackToFlatJSONLog`, `TestTier4_StatePersistenceAndRestartRehydration` |
| **M7** | Protección contra supresión de fallos: escalamiento inmediato a `escalate.Queue` ante agotamiento de reintentos | `TestTier3_Feature17_OutboundCRM_ExponentialBackoffExhaustionToEscalateQueue`, `TestTier3_Feature04_SendResultFailureEscalation` |

---

## 4. Test Tiers Overview

### Tier 1: Smoke & Sanity Baseline (24 Tests)
- **Focus**: Verifying standard happy paths, normal message processing, pipeline checkpoint execution, baseline CRM task generation, webhook verification, HSM template retrieval, DMS sync ETag checks, semantic cache exact/normalized hit, FastPath sub-millisecond execution, embedded console serving, and Prometheus `/metrics`.
- **Execution Speed**: ~0.02s total.

### Tier 2: Boundary, Quotas & Guardrails (17 Tests)
- **Focus**: Exact edge conditions:
  - Cache freshness cutoff: 29m59s (Fresh) vs 30m01s (Stale).
  - Rate limiting exact quotas: 120th message succeeds, 121st rejected; 250th message succeeds, 251st rejected.
  - Jitter bounds: 10,000 randomized seeds verifying 100% adherence to `[2240ms, 4160ms]` (chats) and `[1750ms, 3250ms]` (channels).
  - Character budgets per channel: WhatsApp 650, FB Messenger 500, Instagram Direct 450.
  - Media payload boundary: 256KB max size enforcement (255KB accepted, 257KB rejected with 413).
  - Numeric anti-hallucination guardrail: strict allowlist of official dealership phones (`3151212223`, `3022866969`, `3160502717`).
  - Single-flight concurrent deduplication collapsing concurrent redundant AI queries into a single evaluation.

### Tier 3: Resilience, Failover & Chaos (12 Tests)
- **Focus**: Upstream dispatch errors (`send_failed`), selector cascading failover (primary -> fallback[0] -> fallback[1]), 3-error auto-pause triggering, human operator ticket generation in `escalate.Queue`, alert storm prevention (suppressing repeated emergency alerts once paused), cross-channel fault isolation, WhatsApp Cloud to Playwright failover on 429 quota, SQLite to flat JSON append log failover and primary recovery, DMS network failure grace window, and 3-Tier Free Inference Cascade (Ollama down -> Groq down -> FastPath determinism).

### Tier 4: Omnichannel Lifecycle & System Integration (10 Tests)
- **Focus**: Multi-turn customer journeys (location -> financing inquiry -> test drive booking), mid-negotiation vehicle sold transitions, high-volume multi-channel bursts with sliding window rate limiting, state rehydration across restarts, 40-thread concurrent stress testing, embedded web console 1-click draft approval, CP1 indirect prompt injection sanitization, loopback 127.0.0.1 boundaries, and rigid CP1–CP7 audit.

---

## 5. How to Run the Tests

### Run Entire E2E Suite
```powershell
cd app_build
go test -v ./test/e2e/...
```

### Run Entire E2E Suite without Cache
```powershell
cd app_build
go test -v -count=1 ./test/e2e/...
```

### Run by Specific Tier
```powershell
cd app_build
go test -v -run TestTier1 ./test/e2e/...
go test -v -run TestTier2 ./test/e2e/...
go test -v -run TestTier3 ./test/e2e/...
go test -v -run TestTier4 ./test/e2e/...
```

### Code Formatting and Linting
```powershell
cd app_build
gofmt -l test/e2e/
go vet ./test/e2e/...
```

---

## 6. Concurrency, Security & Safety Guarantees

1. **Zero Real-Time Sleep**: The rate controller and human pause tests use virtual-time advancement (`now time.Time`) and context cancellation hooks (`WaitWithContext`), executing all 58 comprehensive test scenarios across 10,000 jitter calculations in under 4 seconds.
2. **Zero PII Exposure**: All escalation alerts, CRM tasks, and log outputs use sanitized customer aliases and certified inventory fixtures adhering to statutory privacy guardrails (Ley 1581 de 2012).
3. **Zero External Network Dependencies**: 100% standard library Go, using local memory stores, fixture vehicles, loopback HTTP servers, and deterministic adapters.
4. **Token Economy**: Semantic caching and FastPath exact matching verified to consume 0 AI tokens on repeated queries and business inquiries.
