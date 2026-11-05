## 2026-10-29T05:17:10Z
You are the Worker for Milestone 1 (Squad 1: Gateways & Canales) of BuyaCAR Omnicanal Evolution.
Your working directory is: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\worker_squad1_1\
Your parent is Orchestrator (Conversation ID: 84a866f8-986b-4c92-a496-9363b3a17b7f).

MANDATORY FIRST STEP:
Read the authoritative request in: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\ORIGINAL_REQUEST.md
Also read PROJECT.md in workspace root.
Follow the domain skill `go-quality` at: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\skills\go-quality\SKILL.md

Write ownership:
You own EXCLUSIVELY:
- `app_build/internal/channel/`
DO NOT modify any other directory.

Mission (Features 1–10):
1. Implement official WhatsApp Cloud API adapter (`whatsapp_cloud.go`): webhook verification handshake, HMAC-SHA256 signature verification (`X-Hub-Signature-256`), Graph API outbound, HSM templates catalog (>24h window), media message parsing, opt-in/opt-out manager.
2. Implement WhatsApp Playwright resilient fallback (`whatsapp_composite.go`): auto-failover upon HTTP 429 quota saturation or pending WABA verification, using VersionedSelectors.
3. Implement MercadoLibre push webhook connector (`mercadolibre.go`): `X-Signature` HMAC verification, topic routing (`questions` vs `messages`), and pre-sale guardrail policy stripping phone numbers and external links.
4. Implement Unified Meta Omnichannel adapter (`meta_omnichannel.go`): multiplexing Facebook Messenger (500 chars) and Instagram Direct (450 chars) with shared triage and rate controlling.
5. Bind all adapters to `RateController` (120 msg/h, 250 msg/d, deterministic SHA-256 jitter) and `ResilienceTracker` (3 consecutive failures auto-pause).
6. Write comprehensive unit tests in `internal/channel/*_test.go` and verify 100% pass via `go test -v ./internal/channel/...`.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Deliver your handoff report to handoff.md and send completion message to parent orchestrator.


## 2026-10-29T05:18:28Z
**Context**: Directiva Suprema Vinculante (7 Mandamientos Anti-Regresión) para Squad 1
**Content**: Se ha emitido una directiva suprema inmutable que debes incorporar en tu implementación:
1. Ingesta oficial vía Webhook HTTP con validación HMAC-SHA256 (X-Hub-Signature-256 / X-Signature). Si hay contingencia gráfica, usar VersionedSelector con auto-pausa obligatoria al 3er fallo consecutivo y emisión de ticket a escalate.Queue. Prohibido reintento infinito.
2. Respetar presupuestos de caracteres en adaptadores (WhatsApp: 650, FB: 500, IG: 450, TikTok: 400).
3. Concurrencia aislada con context.Context y timeouts <30s. RateController (120/h, 250/d, jitter SHA-256) activo.
4. MAX_BODY_BYTES = 256 KB en endpoints de webhook.
5. Todo debe compilar en Go 1.23 estándar y pasar 100% de tests unitarios sin carreras.
Revisa PROJECT.md para más detalles.
**Action**: Implementa asegurando cumplimiento estricto de estos 7 mandamientos.
