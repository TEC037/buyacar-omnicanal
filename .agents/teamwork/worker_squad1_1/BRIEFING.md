# BRIEFING — 2026-10-29T05:18:00Z

## Mission
Implement official communication gateways and channels (WhatsApp Cloud API, WhatsApp Playwright composite fallback, MercadoLibre webhook connector, and Unified Meta Omnichannel FB/IG adapter) with rate and resilience controls under SIMULATION/DRAFT modes in `app_build/internal/channel/`.

## 🔒 My Identity
- Archetype: worker
- Roles: implementer, qa, specialist
- Working directory: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\worker_squad1_1\
- Original parent: 84a866f8-986b-4c92-a496-9363b3a17b7f
- Milestone: Milestone 1 (Squad 1: Gateways & Canales)

## 🔒 Key Constraints
- Write ownership EXCLUSIVELY: `app_build/internal/channel/` and `.agents/teamwork/worker_squad1_1/`.
- Core in Go 1.23 standard library without heavy external dependencies.
- Operating mode starts in SIMULATION, draft gated by human approval, AUTOPILOT strictly prohibited on unapproved adapters.
- Preserve PII redaction (Ley 1581) and anti-hallucination rules.
- Rate limits (120 msg/h, 250 msg/d, SHA-256 jitter) and resilience (3 consecutive failures auto-pause, escalate.Queue).
- 100% test pass with `go test -race ./internal/channel/...`.
- No cheats, no hardcoded results, no dummy facades.
- Ingesta oficial vía Webhook HTTP con validación HMAC-SHA256 (X-Hub-Signature-256 / X-Signature).
- Si hay contingencia gráfica, usar VersionedSelector con auto-pausa obligatoria al 3er fallo consecutivo y emisión de ticket a escalate.Queue. Prohibido reintento infinito.
- Respetar presupuestos de caracteres en adaptadores (WhatsApp: 650, FB: 500, IG: 450, TikTok: 400).
- Concurrencia aislada con context.Context y timeouts <30s.
- MAX_BODY_BYTES = 256 KB en endpoints de webhook.

## Current Parent
- Conversation ID: 84a866f8-986b-4c92-a496-9363b3a17b7f
- Updated: not yet

## Task Summary
- **What to build**: 
  1. WhatsApp Cloud API adapter (`whatsapp_cloud.go`): webhook handshake GET, HMAC-SHA256 POST verification (`X-Hub-Signature-256`), Graph API outbound, HSM templates catalog (>24h window), media message parsing, opt-in/opt-out manager.
  2. WhatsApp Playwright resilient fallback (`whatsapp_composite.go`): auto-failover upon HTTP 429 quota saturation or pending WABA verification, using VersionedSelectors.
  3. MercadoLibre push webhook connector (`mercadolibre.go`): `X-Signature` HMAC verification, topic routing (`questions` vs `messages`), pre-sale guardrail policy stripping phone numbers and external links.
  4. Unified Meta Omnichannel adapter (`meta_omnichannel.go`): multiplexing Facebook Messenger (500 chars) and Instagram Direct (450 chars) with shared triage and rate controlling.
  5. Binding all adapters to RateController and ResilienceTracker.
  6. Comprehensive unit tests in `internal/channel/*_test.go` passing 100%.
- **Success criteria**: 100% tests pass on `go test ./internal/channel/...`, all 10 features implemented genuinely with interfaces, stubs, and tests.
- **Interface contracts**: PROJECT.md § Interface Contracts (M1: Gateways & Canales)
- **Code layout**: `app_build/internal/channel/`

## Key Decisions Made
- Used standard Go `crypto/hmac`, `crypto/sha256`, `net/http`, `net/url`, `regexp`, `encoding/json` standard library for zero heavy external dependencies.
- Canonical interfaces `WebhookReceiver`, `OptInManager`, `HSMTemplate` placed in `channel.go`.
- WhatsApp Cloud: implements `Channel` and `WebhookReceiver` interfaces, plus `MemoryOptInManager` and `HSMTemplateCatalog`.
- WhatsApp Composite: wraps Cloud API and Playwright fallback, monitoring 429 quota exhaustion or pending verification to trigger selector-driven fallback with `VersionedSelector`.
- MercadoLibre: implements `Channel` and `WebhookReceiver`, with public question pre-sale sanitizer stripping Colombian phone patterns, general phones, email addresses, and external links.
- Meta Omnichannel: single adapter supporting both Messenger (500 chars) and Instagram Direct (450 chars) channels with shared triage.
- RateController & ResilienceTracker bound across all adapters (120/hr, 250/day, 3 failures auto-pause).

## Artifact Index
- `app_build/internal/channel/channel.go` — Canonical interfaces and error contracts
- `app_build/internal/channel/whatsapp_cloud.go` — WhatsApp Cloud API adapter
- `app_build/internal/channel/whatsapp_cloud_test.go` — Unit tests for WhatsApp Cloud
- `app_build/internal/channel/whatsapp_composite.go` — WhatsApp Composite with Playwright fallback
- `app_build/internal/channel/whatsapp_composite_test.go` — Unit tests for WhatsApp Composite
- `app_build/internal/channel/mercadolibre.go` — MercadoLibre webhook & pre-sale guardrails
- `app_build/internal/channel/mercadolibre_test.go` — Unit tests for MercadoLibre
- `app_build/internal/channel/meta_omnichannel.go` — Meta Omnichannel FB/IG multiplexer
- `app_build/internal/channel/meta_omnichannel_test.go` — Unit tests for Meta Omnichannel
- `.agents/teamwork/worker_squad1_1/DISPATCH.md` — Assignment instructions
- `.agents/teamwork/worker_squad1_1/BRIEFING.md` — Situational awareness
- `.agents/teamwork/worker_squad1_1/progress.md` — Execution progress
- `.agents/teamwork/worker_squad1_1/handoff.md` — Final handoff report

## Change Tracker
- **Files modified**:
  - `app_build/internal/channel/channel.go`: added canonical interfaces (WebhookReceiver, OptInManager, HSMTemplate), constants (MaxWebhookBodyBytes), and common errors.
  - `app_build/internal/channel/whatsapp_cloud.go`: implemented official WhatsApp Cloud API adapter with webhooks, HMAC, media, opt-in/opt-out, HSM templates, Graph API outbound.
  - `app_build/internal/channel/whatsapp_cloud_test.go`: comprehensive unit tests (8 tests).
  - `app_build/internal/channel/whatsapp_composite.go`: implemented WhatsApp Composite channel and Playwright browser fallback with VersionedSelectors.
  - `app_build/internal/channel/whatsapp_composite_test.go`: comprehensive unit tests (5 tests).
  - `app_build/internal/channel/mercadolibre.go`: implemented MercadoLibre push webhook connector, topic routing, and pre-sale guardrail policy.
  - `app_build/internal/channel/mercadolibre_test.go`: comprehensive unit tests (5 tests).
  - `app_build/internal/channel/meta_omnichannel.go`: implemented Unified Meta Omnichannel adapter multiplexing Messenger (500 chars) and Instagram Direct (450 chars).
  - `app_build/internal/channel/meta_omnichannel_test.go`: comprehensive unit tests (5 tests).
- **Build status**: PASS (100% test pass on `go test -v ./internal/channel/...` and full repo `go test ./...`)
- **Pending issues**: None

## Quality Status
- **Build/test result**: PASS (42/42 tests passing in `internal/channel`)
- **Lint status**: 0 violations (`go vet` and `gofmt` completely clean)
- **Tests added/modified**: 23 new test cases added across 4 test suites

## Loaded Skills
- **Source**: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\skills\go-quality\SKILL.md
- **Local copy**: N/A (listed in system prompt)
- **Core methodology**: Modular Go architecture, interfaces & adapters, comprehensive tests, race detector, no panic for control flow.
