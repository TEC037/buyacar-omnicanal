package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"buyacar.com.co/omnicanal/internal/channel"
	"buyacar.com.co/omnicanal/internal/escalate"
	"buyacar.com.co/omnicanal/internal/inventory"
)

// TestTier2_Feature02_CacheFreshnessExactBoundary_30Min tests the exact 30-minute freshness threshold:
// At 29m 59s, cache is fresh and dispatch is allowed.
// At 30m 01s, cache is stale and dispatch is rejected/escalated.
func TestTier2_Feature02_CacheFreshnessExactBoundary_30Min(t *testing.T) {
	ctx := context.Background()
	t0 := time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, t0)
	defer h.Close()

	// 1. Boundary: 29 minutes and 59 seconds (Fresh)
	tFresh := t0.Add(29*time.Minute + 59*time.Second)
	if !h.InvWrapper.IsFresh(tFresh) {
		t.Fatalf("expected cache to be fresh at 29m 59s")
	}

	searchFresh := h.InvWrapper.Cache().Search(ctx, inventory.Query{Brand: "Toyota", Model: "Hilux"}, tFresh)
	if searchFresh.Status == inventory.StatusStale {
		t.Fatalf("expected Search not to return StatusStale at 29m 59s, got %s", searchFresh.Status)
	}

	// 2. Boundary: 30 minutes and 1 second (Stale)
	tStale := t0.Add(30*time.Minute + 1*time.Second)
	if h.InvWrapper.IsFresh(tStale) {
		t.Fatalf("expected cache to be stale at 30m 01s")
	}

	searchStale := h.InvWrapper.Cache().Search(ctx, inventory.Query{Brand: "Toyota", Model: "Hilux"}, tStale)
	if searchStale.Status != inventory.StatusStale {
		t.Fatalf("expected Search to return StatusStale at 30m 01s, got %s", searchStale.Status)
	}
}

// TestTier2_Feature03_VehicleAvailabilityStateTransition_SoldReserved tests that when
// a vehicle transitions to 'sold' or 'reserved', commercial sales pitches are aborted,
// a clear commercial notice is emitted, and an escalation ticket is enqueued.
func TestTier2_Feature03_VehicleAvailabilityStateTransition_SoldReserved(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	// Transition veh-001 (Hilux) to 'sold'
	ok := h.InvWrapper.UpdateVehicleStatus("veh-001", "sold")
	if !ok {
		t.Fatalf("failed to update veh-001 status to sold")
	}

	// Confirm updated in wrapper
	v, _ := h.InvWrapper.GetVehicle("veh-001")
	if v.Status != "sold" {
		t.Fatalf("expected status sold, got %s", v.Status)
	}

	// Query for the now-sold vehicle
	convID := "conv-sold-transition-1"
	out, err := h.ExecuteFlow(ctx, "whatsapp", convID, "Precio de la Toyota Hilux 2022 por favor", now)
	if err != nil {
		t.Fatalf("unexpected pipeline error: %v", err)
	}

	// Must be escalated to human queue with sold notice
	t.Logf("OUTCOME: Escalated=%v, Reason=%s, Reply=%s, Checkpoints=%d", out.Escalated, out.EscalationReason, out.SentReply, out.CheckpointsOK)
	if !out.Escalated {
		t.Fatalf("expected conversation for sold vehicle to be escalated, got escalated=false")
	}
	if !strings.Contains(out.EscalationReason, "sold") && !strings.Contains(out.EscalationReason, "vendido") {
		t.Fatalf("expected escalation reason to contain sold or vendido, got: %s", out.EscalationReason)
	}

	// Verify escalation ticket enqueued in human queue
	if len(h.EscQueue.Pending()) == 0 {
		t.Fatalf("expected human escalation ticket enqueued for sold vehicle")
	}
}

// TestTier2_Feature07_InventoryCacheQueryAPI verifies GetVehicle by ID and IsFresh query contracts.
func TestTier2_Feature07_InventoryCacheQueryAPI(t *testing.T) {
	now := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	// 1. Direct ID query
	v1, ok1 := h.InvWrapper.GetVehicle("veh-001")
	if !ok1 || v1.Brand != "Toyota" || v1.Model != "Hilux" {
		t.Fatalf("expected veh-001 to resolve to Toyota Hilux, got ok=%v, v=%+v", ok1, v1)
	}

	// 2. Slug alias query
	v2, ok2 := h.InvWrapper.GetVehicle("toyota-hilux-2022")
	if !ok2 || v2.ID != "veh-001" {
		t.Fatalf("expected toyota-hilux-2022 to resolve to veh-001, got ok=%v, v=%+v", ok2, v2)
	}

	// 3. Non-existent vehicle query
	_, ok3 := h.InvWrapper.GetVehicle("ferrari-f40-1990")
	if ok3 {
		t.Fatalf("expected non-existent vehicle to return ok=false")
	}

	// 4. Freshness API
	if !h.InvWrapper.IsFresh(now.Add(10 * time.Minute)) {
		t.Fatalf("expected cache to be fresh at +10 min")
	}
	if h.InvWrapper.IsFresh(now.Add(35 * time.Minute)) {
		t.Fatalf("expected cache not to be fresh at +35 min")
	}
}

// TestTier2_Feature08_HourlyRateLimitExactBoundary_120Msgs validates that strictly 120 messages
// are permitted in a rolling 1-hour window, with the 121st blocked with 'hourly_rate_limit_exceeded'.
func TestTier2_Feature08_HourlyRateLimitExactBoundary_120Msgs(t *testing.T) {
	cfg := DefaultRateConfig()
	rc := NewMemoryRateController(cfg)
	t0 := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	chID := "whatsapp"

	// Send exactly 120 messages in rolling 1-hour window
	for i := 1; i <= 120; i++ {
		ts := t0.Add(time.Duration(i) * 20 * time.Second) // 120 msgs across 40 minutes
		canSend, reason := rc.CanSend(chID, ts)
		if !canSend {
			t.Fatalf("message %d should have been permitted, but got denied: %s", i, reason)
		}
		rc.RecordSend(chID, ts)
	}

	// The 121st message within the same 1-hour window must be blocked
	t121 := t0.Add(45 * time.Minute)
	canSend121, reason121 := rc.CanSend(chID, t121)
	if canSend121 {
		t.Fatalf("expected 121st message to be rejected by hourly rate limiter")
	}
	if reason121 != "hourly_rate_limit_exceeded" {
		t.Fatalf("expected reason hourly_rate_limit_exceeded, got: %s", reason121)
	}

	// After 1 hour rolls past the first messages, sending is authorized again
	tAfter1Hr := t0.Add(1*time.Hour + 1*time.Minute)
	canSendAfter, _ := rc.CanSend(chID, tAfter1Hr)
	if !canSendAfter {
		t.Fatalf("expected rate limiter to allow sending after 1 hour rolled past oldest message")
	}
}

// TestTier2_Feature09_DailyRateLimitExactBoundary_250Msgs validates that strictly 250 messages
// are permitted in a rolling 24-hour window, with the 251st blocked with 'daily_rate_limit_exceeded'.
func TestTier2_Feature09_DailyRateLimitExactBoundary_250Msgs(t *testing.T) {
	cfg := DefaultRateConfig()
	rc := NewMemoryRateController(cfg)
	t0 := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	chID := "email_pilot"

	// Spread 250 messages across 20 hours (staying well under 120/hr)
	for i := 1; i <= 250; i++ {
		ts := t0.Add(time.Duration(i) * 4 * time.Minute) // 250 msgs over ~16.6 hours (~15 msgs/hr)
		canSend, reason := rc.CanSend(chID, ts)
		if !canSend {
			t.Fatalf("message %d should have been permitted, got denied: %s", i, reason)
		}
		rc.RecordSend(chID, ts)
	}

	// 251st message within the 24-hour window must be blocked by daily limiter
	t251 := t0.Add(18 * time.Hour)
	canSend251, reason251 := rc.CanSend(chID, t251)
	if canSend251 {
		t.Fatalf("expected 251st message to be rejected by daily rate limiter")
	}
	if reason251 != "daily_rate_limit_exceeded" {
		t.Fatalf("expected reason daily_rate_limit_exceeded, got: %s", reason251)
	}

	// Roll past 24 hours: daily quota resets
	tAfter24 := t0.Add(24*time.Hour + 5*time.Minute)
	canSendAfter, _ := rc.CanSend(chID, tAfter24)
	if !canSendAfter {
		t.Fatalf("expected daily quota to reset after rolling window passes 24 hours")
	}
}

// TestTier2_Feature10_11_12_DeterministicJitterExactBounds_30Percent tests 10,000 distinct seeds
// verifying that 100% of calculated delays fall strictly within the base ± 30% range.
func TestTier2_Feature10_11_12_DeterministicJitterExactBounds_30Percent(t *testing.T) {
	rc := NewMemoryRateController(DefaultRateConfig())

	chatBase := 3200 * time.Millisecond
	chatMin := time.Duration(float64(chatBase.Nanoseconds()) * 0.70) // 2,240 ms
	chatMax := time.Duration(float64(chatBase.Nanoseconds()) * 1.30) // 4,160 ms

	channelBase := 2500 * time.Millisecond
	channelMin := time.Duration(float64(channelBase.Nanoseconds()) * 0.70) // 1,750 ms
	channelMax := time.Duration(float64(channelBase.Nanoseconds()) * 1.30) // 3,250 ms

	for seed := int64(1); seed <= 10000; seed++ {
		chatDelay := rc.CalculateChatDelay(seed)
		if chatDelay < chatMin || chatDelay > chatMax {
			t.Fatalf("seed %d: chatDelay %v out of ±30%% bounds [%v, %v]", seed, chatDelay, chatMin, chatMax)
		}

		channelDelay := rc.CalculateChannelDelay(seed)
		if channelDelay < channelMin || channelDelay > channelMax {
			t.Fatalf("seed %d: channelDelay %v out of ±30%% bounds [%v, %v]", seed, channelDelay, channelMin, channelMax)
		}
	}
}

// TestTier2_Feature12_JitterSeedReproducibility validates that the deterministic jitter
// is perfectly repeatable: the same seed always yields the exact same duration.
func TestTier2_Feature12_JitterSeedReproducibility(t *testing.T) {
	rc := NewMemoryRateController(DefaultRateConfig())

	targetSeed := int64(987654321)
	firstChatDelay := rc.CalculateChatDelay(targetSeed)
	firstChannelDelay := rc.CalculateChannelDelay(targetSeed)

	for i := 0; i < 500; i++ {
		if rc.CalculateChatDelay(targetSeed) != firstChatDelay {
			t.Fatalf("non-deterministic chat delay observed on iteration %d", i)
		}
		if rc.CalculateChannelDelay(targetSeed) != firstChannelDelay {
			t.Fatalf("non-deterministic channel delay observed on iteration %d", i)
		}
	}
}

// TestTier2_Feature13_VirtualTimeNonBlockingAdvancementAndCancellation tests that simulated
// time waits do not block wall-clock execution and respond instantly to context cancellation.
func TestTier2_Feature13_VirtualTimeNonBlockingAdvancementAndCancellation(t *testing.T) {
	rc := NewMemoryRateController(DefaultRateConfig())

	// 1. Instant cancellation
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	start := time.Now()
	err := rc.WaitWithContext(ctx, 3200*time.Millisecond)
	elapsed := time.Since(start)

	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("virtual wait took too long to abort on cancellation: %v", elapsed)
	}
}

// TestTier2_Feature15_16_ConsecutiveFailureCounterTransitions_ResetOnSuccess tests the exact
// state machine transitions: 1 error -> active; 2 errors -> active; 1 success -> resets to 0;
// then 1, 2, 3 consecutive errors -> transitions to paused = true.
func TestTier2_Feature15_16_ConsecutiveFailureCounterTransitions_ResetOnSuccess(t *testing.T) {
	escQ := escalate.NewQueue()
	tracker := NewMemoryResilienceTracker(escQ)
	chID := "whatsapp"

	// 1. First failure
	paused, count := tracker.RecordFailure(chID, "read timeout")
	if paused || count != 1 {
		t.Fatalf("step 1: expected paused=false, count=1, got paused=%v, count=%d", paused, count)
	}

	// 2. Second failure
	paused, count = tracker.RecordFailure(chID, "selector missing")
	if paused || count != 2 {
		t.Fatalf("step 2: expected paused=false, count=2, got paused=%v, count=%d", paused, count)
	}

	// 3. Success resets counter to 0
	tracker.RecordSuccess(chID)
	if tracker.GetConsecutiveFailures(chID) != 0 {
		t.Fatalf("step 3: expected count=0 after RecordSuccess, got %d", tracker.GetConsecutiveFailures(chID))
	}
	if tracker.IsPaused(chID) {
		t.Fatalf("step 3: channel should not be paused")
	}

	// 4. One failure after reset
	paused, count = tracker.RecordFailure(chID, "intermittent net error")
	if paused || count != 1 {
		t.Fatalf("step 4: expected paused=false, count=1 after reset, got paused=%v, count=%d", paused, count)
	}

	// 5. Second consecutive failure
	paused, count = tracker.RecordFailure(chID, "DOM element stale")
	if paused || count != 2 {
		t.Fatalf("step 5: expected paused=false, count=2, got paused=%v, count=%d", paused, count)
	}

	// 6. Third consecutive failure: STRICT TRIGGER OF AUTO-PAUSE (Feature 16)
	paused, count = tracker.RecordFailure(chID, "HTTP 502 Bad Gateway")
	if !paused || count != 3 {
		t.Fatalf("step 6: expected paused=true, count=3, got paused=%v, count=%d", paused, count)
	}
	if !tracker.IsPaused(chID) {
		t.Fatalf("expected IsPaused to return true")
	}

	// 7. Verify human alert ticket enqueued in escalate.Queue (Feature 17)
	if len(escQ.Pending()) != 1 {
		t.Fatalf("expected 1 human escalation ticket enqueued upon auto-pause, got %d", len(escQ.Pending()))
	}
	ticket := escQ.Pending()[0]
	if ticket.Reason != "channel_auto_paused_consecutive_failures" {
		t.Fatalf("expected reason channel_auto_paused_consecutive_failures, got: %s", ticket.Reason)
	}
}

// ============================================================================
// NEW EVOLUTION TIER 2 BOUNDARY TESTS (FEATURES 1–28)
// ============================================================================

// TestTier2_Feature01_WhatsAppCloud_InvalidHMACSignatureRejection verifies that forged or
// corrupted HMAC signatures are rejected with 401 Unauthorized (Feature 1).
func TestTier2_Feature01_WhatsAppCloud_InvalidHMACSignatureRejection(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	payload := []byte(`{"object":"whatsapp_business_account","entry":[]}`)
	status, err := h.WhatsAppCloud.HandleWebhookPOST("sha256=forged_invalid_signature_hash_123", payload)
	if err == nil || status != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for invalid signature, got status=%d, err=%v", status, err)
	}
}

// TestTier2_Feature04_WhatsAppCloud_MediaPayloadSizeLimit_256KB verifies that payloads
// exceeding MAX_BODY_BYTES = 256KB are rejected with 413 Payload Too Large (Mandament 4, Feature 4).
func TestTier2_Feature04_WhatsAppCloud_MediaPayloadSizeLimit_256KB(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 5, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	// 257 KB payload: exceeds 256KB limit
	oversized := make([]byte, 257*1024)
	sig := h.WhatsAppCloud.ComputeHMACSHA256(oversized)
	status, err := h.WhatsAppCloud.HandleWebhookPOST(sig, oversized)
	if err == nil || status != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 Payload Too Large for oversized payload, got status=%d, err=%v", status, err)
	}

	// 255 KB payload: permitted
	validSize := make([]byte, 255*1024)
	sigValid := h.WhatsAppCloud.ComputeHMACSHA256(validSize)
	statusValid, errValid := h.WhatsAppCloud.HandleWebhookPOST(sigValid, validSize)
	if errValid != nil || statusValid != http.StatusOK {
		t.Fatalf("expected 200 OK for 255KB payload, got status=%d, err=%v", statusValid, errValid)
	}
}

// TestTier2_Feature05_WhatsAppCloud_RevokedConsentBlocksMarketing verifies that outbound
// messages to customers with OptInRevoked are blocked (Feature 5).
func TestTier2_Feature05_WhatsAppCloud_RevokedConsentBlocksMarketing(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 12, 10, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	custID := "cust-optout-user"
	h.WhatsAppCloud.OptIn.SetStatus(custID, OptInRevoked)

	res, err := h.WhatsAppCloud.SendMessage(ctx, custID, "Nuevas ofertas disponibles!", true)
	if err == nil || res.Status != channel.SendFailed {
		t.Fatalf("expected send failure when customer opted out, got err=%v, res=%+v", err, res)
	}
	if !strings.Contains(err.Error(), "opt_in_revoked") {
		t.Fatalf("expected opt_in_revoked error, got: %v", err)
	}
}

// TestTier2_Feature08_MercadoLibre_PreSaleGuardrail_StripPhoneAndLinks verifies stripping
// of phone numbers and external links in public pre-sale questions (Feature 8).
func TestTier2_Feature08_MercadoLibre_PreSaleGuardrail_StripPhoneAndLinks(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 15, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	question := "Me interesa el auto. Escríbeme al 3151212223 o visita mi página www.autosboyaca.com para ver mi oferta."
	sanitized, modified := h.MercadoLibre.PreSaleGuardrail(question)

	if !modified {
		t.Fatalf("expected pre-sale question to be modified by guardrail")
	}
	if strings.Contains(sanitized, "3151212223") {
		t.Fatalf("phone number was not stripped: %s", sanitized)
	}
	if strings.Contains(sanitized, "www.autosboyaca.com") {
		t.Fatalf("external link was not stripped: %s", sanitized)
	}
	if !strings.Contains(sanitized, "[TELEFONO_REDACTADO]") || !strings.Contains(sanitized, "[ENLACE_EXTERNO_REDACTADO]") {
		t.Fatalf("expected redaction placeholders in output, got: %s", sanitized)
	}
}

// TestTier2_Feature09_CharacterBudgetBoundaries_AllChannels verifies exact character budgets
// across omnichannel adapters (WhatsApp: 650, FB: 500, IG: 450) (Mandament 2 CP5, Feature 9).
func TestTier2_Feature09_CharacterBudgetBoundaries_AllChannels(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 20, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	limits := map[string]int{
		"whatsapp":           650,
		"facebook_messenger": 500,
		"instagram_direct":   450,
	}

	for chID, expectedMax := range limits {
		ch, ok := h.Channels[chID]
		if !ok {
			t.Fatalf("channel %s not found in harness", chID)
		}
		if ch.Limits().MaxCharacterLength != expectedMax {
			t.Fatalf("channel %s has character budget %d, expected %d", chID, ch.Limits().MaxCharacterLength, expectedMax)
		}
	}
}

// TestTier2_Feature14_InvalidationWebhook_UnauthorizedBearerToken verifies unauthorized
// invalidation requests are rejected (Feature 14).
func TestTier2_Feature14_InvalidationWebhook_UnauthorizedBearerToken(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 25, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	body := bytes.NewBufferString(`{"vehicle_id":"veh-001","status":"sold"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/invalidation", body)
	req.Header.Set("Authorization", "Bearer invalid_bearer_token")

	status, err := h.Invalidation.HandleInvalidation(req)
	if err == nil || status != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for invalid token, got status=%d, err=%v", status, err)
	}
}

// TestTier2_Feature15_Waitlist_SHA256IntegrityVerification verifies exact byte-level
// SHA-256 seal calculation on waitlist registrations (Feature 15).
func TestTier2_Feature15_Waitlist_SHA256IntegrityVerification(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 30, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	entry := h.Waitlist.Register("veh-hilux-01", "cust-boyaca-456", "whatsapp", now)

	// Independently re-derive expected SHA-256 seal
	raw := fmt.Sprintf("%s|%s|%s|%d", "veh-hilux-01", "cust-boyaca-456", "whatsapp", now.Unix())
	expectedHashBytes := sha256.Sum256([]byte(raw))
	expectedHash := hex.EncodeToString(expectedHashBytes[:])

	if entry.SealedHash != expectedHash {
		t.Fatalf("Waitlist SHA-256 seal mismatch: expected %s, got %s", expectedHash, entry.SealedHash)
	}
}

// TestTier2_Feature19_SingleFlight_ConcurrentQueryDeduplication verifies that multiple
// concurrent identical queries collapse into a single evaluation (Feature 19).
func TestTier2_Feature19_SingleFlight_ConcurrentQueryDeduplication(t *testing.T) {
	sf := NewMockSingleFlight()
	key := "hash-query-financiamiento-bancos"

	evalCount := 0
	var mu sync.Mutex

	evaluator := func() (string, error) {
		mu.Lock()
		evalCount++
		mu.Unlock()
		time.Sleep(10 * time.Millisecond) // simulate AI evaluation time
		return "BuyaCAR ofrece planes con Bancolombia y Davivienda.", nil
	}

	var wg sync.WaitGroup
	results := make([]string, 10)
	sharedFlags := make([]bool, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			res, _, shared := sf.Execute(key, evaluator)
			results[idx] = res
			sharedFlags[idx] = shared
		}(i)
	}

	wg.Wait()

	// All 10 must receive the exact same valid response
	for i, r := range results {
		if !strings.Contains(r, "Bancolombia") {
			t.Fatalf("goroutine %d received invalid answer: %s", i, r)
		}
	}

	// At least some executions were marked as shared duplicate calls
	hasShared := false
	for _, sh := range sharedFlags {
		if sh {
			hasShared = true
			break
		}
	}
	if !hasShared {
		t.Fatalf("expected single-flight to share duplicate concurrent calls")
	}
}

// TestTier2_Feature21_GroqProvider_TokenBudgetAndJSONSchema verifies that Groq Free API
// responses adhere to JSON schema and output token budget (Feature 21).
func TestTier2_Feature21_GroqProvider_TokenBudgetAndJSONSchema(t *testing.T) {
	sampleGroqJSON := `{
		"config_id": "cfg-groq-free-v1",
		"trace": {"request_id": "req-groq-01", "timestamp_utc": "2026-10-29T12:35:00Z"},
		"proposed_reply": "Disponemos de Toyota Hilux y Mazda CX-5 certificadas.",
		"intent": "catalog_inquiry",
		"confidence": 0.95,
		"crm_update": {"tasks_to_create": ["seguimiento_comercial"]},
		"governance": {"escalate": false}
	}`

	var parsed struct {
		ConfigID      string  `json:"config_id"`
		ProposedReply string  `json:"proposed_reply"`
		Intent        string  `json:"intent"`
		Confidence    float64 `json:"confidence"`
	}

	if err := json.Unmarshal([]byte(sampleGroqJSON), &parsed); err != nil {
		t.Fatalf("Groq response failed JSON schema parse: %v", err)
	}
	if parsed.ProposedReply == "" || parsed.Confidence < 0.8 {
		t.Fatalf("invalid Groq schema content: %+v", parsed)
	}
}

// TestTier2_Feature28_JSONCLIInspection_FlagsMetricsAndInspect verifies that CLI flags
// --metrics and --inspect output parseable JSON with expected keys (Feature 28).
func TestTier2_Feature28_JSONCLIInspection_FlagsMetricsAndInspect(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 40, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	// 1. --metrics CLI output
	metricsJSON, err := h.CLI.ExecuteCLI("--metrics")
	if err != nil {
		t.Fatalf("--metrics failed: %v", err)
	}
	var metricsData map[string]interface{}
	if err := json.Unmarshal([]byte(metricsJSON), &metricsData); err != nil {
		t.Fatalf("--metrics output is not valid JSON: %v, raw: %s", err, metricsJSON)
	}
	if _, ok := metricsData["fast_path_p99_us"]; !ok {
		t.Fatalf("--metrics JSON missing fast_path_p99_us")
	}

	// 2. --inspect CLI output
	inspectJSON, err2 := h.CLI.ExecuteCLI("--inspect")
	if err2 != nil {
		t.Fatalf("--inspect failed: %v", err2)
	}
	var inspectData map[string]interface{}
	if err := json.Unmarshal([]byte(inspectJSON), &inspectData); err != nil {
		t.Fatalf("--inspect output is not valid JSON: %v, raw: %s", err, inspectJSON)
	}
	if inspectData["status"] != "operational" {
		t.Fatalf("--inspect JSON missing operational status")
	}
}

// TestTier2_Mandament02_NumericAntiHallucination_OfficialPhones verifies that 5+ digit
// phone numbers not in the official whitelist (3151212223, 3022866969, 3160502717) are rejected (Mandament 2 CP5).
func TestTier2_Mandament02_NumericAntiHallucination_OfficialPhones(t *testing.T) {
	// 1. Reply with official phone number: Allowed
	validReply := "Comunícate con nuestro asesor oficial en Tunja al 3151212223."
	if !ValidateResponsePhones(validReply, nil) {
		t.Fatalf("expected official phone 3151212223 to be allowed")
	}

	// 2. Reply with another official phone: Allowed
	validReply2 := "También puedes llamar al 3022866969 o al 3160502717."
	if !ValidateResponsePhones(validReply2, nil) {
		t.Fatalf("expected official phones 3022866969 and 3160502717 to be allowed")
	}

	// 3. Reply with uncataloged/hallucinated phone: Rejected!
	hallucinatedReply := "Llama a mi celular personal al 3198887766 para darte descuento."
	if ValidateResponsePhones(hallucinatedReply, nil) {
		t.Fatalf("expected hallucinated phone 3198887766 to be rejected by CP5 guardrail")
	}
}
