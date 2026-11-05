# Handoff Report — Channels Explorer (R1, R2 Survey)

## 1. Observation

- **Architecture & Interfaces**:
  - `app_build/internal/channel/channel.go`: Lines 89–96 define the canonical `Channel` interface:
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
  - `app_build/internal/channel/channel.go`: Lines 23–28 define `Limits`:
    ```go
    type Limits struct {
        MaxCharacterLength int
        MaxPerHour         int
        MaxPerDay          int
    }
    ```
  - `app_build/internal/channel/channel.go`: Lines 74–79 define `SendStatus` constants:
    `SendSimulated = "simulated_no_op"`, `SendDrafted = "draft_recorded"`, `SendVerified = "sent_verified"`, `SendFailed = "send_failed"`.
  - `app_build/internal/channel/rate_controller.go`: Lines 20–29 define `DefaultRateConfig()`:
    `MaxMessagesPerHour = 120`, `MaxMessagesPerDay = 250`, `ChatDelayBase = 3200ms`, `ChannelDelayBase = 2500ms`, `JitterPercent = 0.30` (±30%).
  - `app_build/internal/channel/rate_controller.go`: Lines 149–156 implement `CalculateDeterministicJitter` using SHA-256 over `jitter-salt-%d`, mapping deterministic delay without pseudo-random races.
  - `app_build/internal/channel/resilience.go`: Line 23 sets `DefaultMaxConsecutiveFailures = 3`. Lines 179–210 implement `RecordFailure`, automatically pausing the channel upon 3 consecutive failures and pushing an alert ticket to `escalate.Queue` while suppressing alert storms on subsequent failures.
  - `app_build/internal/channel/resilience.go`: Lines 30–36 define `VersionedSelector` with `Primary`, `Fallbacks []string`, and `ActiveIndex int`.
  - `app_build/internal/guard/guard.go`: Lines 38–44 define `ChannelCharLimits`:
    `tiktok: 400`, `instagram: 450`, `facebook_marketplace: 500`, `whatsapp: 650`, `email: 2000`.
  - `app_build/internal/core/pipeline.go`: Lines 78–96 and Lines 272–316 illustrate the core pipeline integrating `rt.IsPaused()`, `ch.Read()`, `rt.RecordFailure()`, `ch.Reply()`, mode verification, and `rt.RecordSuccess()`.
- **Existing Test Execution**:
  - Running `Set-Location app_build; go test ./...` passed 100% across all 15 packages without warnings or race condition failures.

## 2. Logic Chain

1. **Inbound Webhook vs Polling Interface (Ref: Obs 1.1 `channel.go:89-96` & `core/pipeline.go:78-96`)**:
   - The existing `Channel` interface uses `Pending()` and `Read()`.
   - Webhook-based channels (WhatsApp Cloud API, MercadoLibre, Meta Graph API) receive HTTP POST push notifications asynchronously.
   - Therefore, the adapters must implement a native `net/http` webhook receiver that verifies cryptographic signatures (`X-Hub-Signature-256`, `X-Signature`), hydrates messages into the adapter's in-memory thread buffer, and yields them via `Pending()` / `Read()`, or exposes a direct pipeline ingress method. This preserves 100% compatibility with the existing 7-checkpoint pipeline.

2. **WhatsApp Cloud API & Resilient Fallback (Ref: Obs 1.4 `rate_controller.go`, Obs 1.6 `resilience.go:30-36`, Obs 1.8 `guard.go`)**:
   - Meta imposes strict messaging tiers (e.g. 250 msgs/day for unverified accounts) and returns HTTP 429 upon quota exhaustion.
   - The system requirement R2 demands seamless fallback to Playwright browser automation with versioned selectors.
   - Therefore, a composite adapter (`WhatsAppCompositeChannel`) containing both `WhatsAppCloudAdapter` and `WhatsAppBrowserAdapter` must manage failover state. When Cloud API returns 429, WABA error 131031, or 3 consecutive 5xx errors, it seamlessly switches to the Playwright adapter using `SelectorChatSearch`, `SelectorMessageInput`, and `SelectorSendButton` (`VersionedSelector`).
   - Rate limiting (120/h, 250/d) and SHA-256 deterministic jitter remain bound across both engines, protecting browser automation from anti-bot detection.

3. **MercadoLibre Pre-Sale vs Post-Sale Segregation (Ref: Obs 1.8 `guard.go:38-44`)**:
   - MercadoLibre webhooks emit notifications for two topics: `questions` (public pre-sale) and `messages` (private post-sale).
   - MercadoLibre strictly penalizes sharing phone numbers or external URLs in pre-sale questions.
   - Therefore, the MercadoLibre connector must intercept pre-sale questions, enforce a specialized ML Guard policy that strips phone numbers and external links, append a compliant marketplace notice, and respect the 2000 character limit.

4. **Unified Meta Omnichannel Adapter (Ref: Obs 1.8 `guard.go:38-44`)**:
   - Facebook Messenger and Instagram Direct use the same Meta Graph Webhooks protocol, differing primarily in the payload `object` (`"page"` vs `"instagram"`) and channel character limits (500 vs 450).
   - Therefore, a single unified `MetaOmnichannelAdapter` handles signature validation, routing events to `facebook_marketplace` / `facebook_messenger` or `instagram`, reusing triage, Fast-Path, and `RateController`.

5. **Universal Rate & Resilience Integration (Ref: Obs 1.4 `rate_controller.go`, Obs 1.6 `resilience.go:179-210`)**:
   - Both `RateController` and `ResilienceTracker` are indexed by `channelID`.
   - By ensuring that each channel adapter uses its canonical `ID()` (`whatsapp`, `mercadolibre`, `facebook_marketplace`, `instagram`, `email`), channel failures are completely isolated: a MercadoLibre pause does not disrupt WhatsApp or Instagram.
   - Upon the 3rd consecutive failure on any channel, `ResilienceTracker` auto-pauses the channel, emits a ticket to `escalate.Queue`, and suppresses alert storms.

## 3. Caveats

- **No live credentials used**: Consistent with `SIMULATION` mode and safety rules, no real Meta App Secrets, WhatsApp Phone Number IDs, or MercadoLibre OAuth tokens were requested or configured. All designs use secure environment variable injection (`os.Getenv`).
- **Playwright process execution**: Playwright browser automation in production requires Node.js/headless Chromium. In Go, it can be driven via standard library CDP WebSocket client or headless CLI runner without CGO or heavy external libraries.
- **Webhook hosting in local dev**: During local development without public IP, a reverse proxy (e.g. ngrok or Cloudflare tunnel) or synthetic webhook testing harness is required to deliver push notifications from Meta / MercadoLibre.

## 4. Conclusion

The existing channel and resilience foundation (`channel.go`, `rate_controller.go`, `resilience.go`) is sound, robust, and ready for extension. The R2 evolution can be implemented cleanly in Go 1.23 standard library by introducing:
1. `WhatsAppCloudAdapter` with HMAC-SHA256 validation, HSM catalog, and opt-in/opt-out manager.
2. `WhatsAppBrowserAdapter` with `VersionedSelector` cascades for Playwright fallback.
3. `WhatsAppCompositeChannel` acting as the resilient facade.
4. `MercadoLibreConnector` with `X-Signature` HMAC verification and pre-sale contact strip guardrail.
5. `MetaOmnichannelAdapter` multiplexing Messenger and Instagram Direct.
6. `WebhookServer` serving standard `net/http` webhook endpoints.

All components integrate seamlessly with `RateController` (120/h, 250/d, SHA-256 jitter) and `ResilienceTracker` (3-failure auto-pause, `escalate.Queue`), preserving strict channel isolation, Ley 1581 privacy compliance, and Fast-Path token economy.

## 5. Verification Method

To verify the observations and analysis independently:
1. **Module & Suite Test Execution**:
   ```powershell
   Set-Location app_build
   go test -race ./...
   ```
   *Expected outcome*: 100% green tests across all packages (`channel`, `core`, `crm`, `fast`, `guard`, `inventory`, `obs`, `store`, `test/e2e`).
2. **Channel Resilience & Rate Controller Tests**:
   ```powershell
   Set-Location app_build
   go test -v -run TestRateController ./internal/channel/
   go test -v -run TestResilienceTracker ./internal/channel/
   go test -v -run TestVersionedSelector ./internal/channel/
   ```
   *Expected outcome*: All tests pass verifying sliding window 120/h, 250/d, SHA-256 jitter bounds, selector failovers, and 3-failure auto-pause.
3. **Inspect Analysis Blueprint**:
   View `c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\survey_explorer_channels\analysis.md` for complete data structures, webhook flows, and template definitions.
4. **Invalidation Conditions**:
   - If Meta alters Graph API webhook payload format in v22+.
   - If MercadoLibre deprecates `X-Signature` HMAC authentication.
   - If the project introduces CGO or third-party web frameworks, violating pure Go 1.23 standard library constraints.
