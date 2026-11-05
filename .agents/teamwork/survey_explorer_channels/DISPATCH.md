## 2026-10-29T05:08:39Z
You are the Channels Explorer for the Survey Phase of BuyaCAR Omnicanal Evolution (R1, R2).
Your working directory is: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\survey_explorer_channels\
Your parent is Orchestrator (Conversation ID: 84a866f8-986b-4c92-a496-9363b3a17b7f).

MANDATORY FIRST STEP:
Read the authoritative request in: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\ORIGINAL_REQUEST.md

Investigate `app_build/internal/channel/` and related channel components:
- `rate_controller.go` (120/h, 250/d, deterministic jitter)
- `resilience.go` (versioned selectors, 3-consecutive-failures auto-pause)
- `channel.go` (Channel interface, ChannelConversation, SendResult)
- `email_pilot.go`, `stub.go`, etc.

Your goal:
1. Map existing channel architecture, interfaces, and rate limiting/resilience integration.
2. Detail implementation architecture for R2:
   - Official WhatsApp Cloud API adapter: webhook endpoint, verification token, HSM pre-approved templates catalog, media message handling, opt-in/opt-out status management.
   - Resilient fallback from Cloud API to Playwright browser automation with versioned selectors upon quota saturation or pending verification.
   - MercadoLibre push webhook connector: endpoint, HMAC/crypto signature verification, parsing of publication questions and post-sale messages.
   - Unified omnichannel integration for Facebook Messenger and Instagram Direct: webhook handlers, mapping to ChannelConversation, sharing triage engine and rate controller.
3. Identify how all these channels hook into `RateController` and `ResilienceTracker`.

Write detailed analysis to analysis.md and formal handoff to handoff.md in your working directory.
When done, send a summary message to your parent using send_message.
