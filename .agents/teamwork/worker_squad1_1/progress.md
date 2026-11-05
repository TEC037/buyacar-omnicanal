# Progress — Squad 1: Gateways & Canales

**Last visited**: 2026-10-29T05:32:00Z

## Status
- [x] Initialized workspace and reviewed DISPATCH, ORIGINAL_REQUEST, PROJECT.md, and `internal/channel` current code.
- [x] Step 1: Design and implement WhatsApp Cloud API adapter (`whatsapp_cloud.go`): webhook handshake GET, HMAC-SHA256 POST verification, Graph API outbound, HSM templates catalog, media parsing, opt-in/opt-out manager.
- [x] Step 2: Design and implement WhatsApp Playwright resilient composite fallback (`whatsapp_composite.go`): HTTP 429 quota auto-failover, pending WABA auto-failover, VersionedSelectors.
- [x] Step 3: Design and implement MercadoLibre push webhook connector & pre-sale guardrail policy (`mercadolibre.go`): `X-Signature` HMAC verification, topic routing (`questions` vs `messages`), stripping phone numbers and external links in pre-sale.
- [x] Step 4: Design and implement Unified Meta Omnichannel adapter (`meta_omnichannel.go`): multiplexing Facebook Messenger (500 chars) and Instagram Direct (450 chars) with shared triage and rate controlling.
- [x] Step 5: RateController & ResilienceTracker integration across all adapters (120 msg/h, 250 msg/d, SHA-256 jitter, 3 consecutive failures auto-pause).
- [x] Step 6: Write comprehensive unit and integration tests (`whatsapp_cloud_test.go`, `whatsapp_composite_test.go`, `mercadolibre_test.go`, `meta_omnichannel_test.go`).
- [x] Step 7: Verify with `go test -v ./internal/channel/...`, `go vet ./internal/channel/...`, `gofmt -s -l ./internal/channel`, and full repo `go test ./...` (100% PASS).
- [x] Step 8: Produce `handoff.md` and send completion message to parent orchestrator.
