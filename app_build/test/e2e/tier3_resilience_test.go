package e2e

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"buyacar.com.co/omnicanal/internal/channel"
	"buyacar.com.co/omnicanal/internal/escalate"
	"buyacar.com.co/omnicanal/internal/inventory"
	"buyacar.com.co/omnicanal/internal/store"
)

// TestTier3_Feature04_SendResultFailureEscalation verifies that upstream dispatch errors
// (e.g. network failure, API rejection) escalate immediately to escalate.Queue with
// reason 'send_failed' and do NOT seal idempotency as delivered.
func TestTier3_Feature04_SendResultFailureEscalation(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 11, 0, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	ch := h.Channels["whatsapp"]
	ch.SetFailSend(true, "simulated WhatsApp network drop")

	convID := "conv-tier3-sendfail-1"
	out, err := h.ExecuteFlow(ctx, "whatsapp", convID, "Buenos dias", now)

	// An error from ch.Reply should return non-nil error and escalate
	if err == nil {
		t.Fatalf("expected non-nil error when channel send fails")
	}
	if !out.Escalated || out.EscalationReason != "send_failed" {
		t.Fatalf("expected EscalationReason send_failed, got escalated=%v, reason=%s", out.Escalated, out.EscalationReason)
	}

	// Verify escalation ticket in human queue
	pending := h.EscQueue.Pending()
	found := false
	for _, ticket := range pending {
		if ticket.ChannelID == "whatsapp" && ticket.Reason == "send_failed" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected send_failed ticket in escalate.Queue")
	}

	// Verify idempotency was NOT sealed as processed: retry must be permitted
	ch.SetFailSend(false, "")
	outRetry, errRetry := h.ExecuteFlow(ctx, "whatsapp", convID, "Buenos dias", now.Add(1*time.Minute))
	if errRetry != nil {
		t.Fatalf("retry failed unexpectedly: %v", errRetry)
	}
	if outRetry.Skipped {
		t.Fatalf("retry thread should NOT have been skipped by idempotency since first attempt failed")
	}
}

// TestTier3_Feature14_VersionedSelectorFallbackCascade tests that when a Primary selector
// fails, the selector resolver falls back to candidate Fallback selectors sequentially.
func TestTier3_Feature14_VersionedSelectorFallbackCascade(t *testing.T) {
	selector := VersionedSelector{
		ID:        "wa-msg-input-v1",
		Version:   "1.2.0",
		Primary:   "div._input_broken_selector",
		Fallbacks: []string{"div.selectable-text[data-tab='10']", "footer div[contenteditable='true']"},
	}

	// Resolver simulation: primary fails, fallback 0 succeeds
	resolveSelector := func(s VersionedSelector, brokenMap map[string]bool) (string, int, bool) {
		if !brokenMap[s.Primary] {
			return s.Primary, -1, true
		}
		for i, fb := range s.Fallbacks {
			if !brokenMap[fb] {
				return fb, i, true
			}
		}
		return "", -1, false
	}

	// Case 1: Primary broken -> Fallback 0 selected
	broken1 := map[string]bool{selector.Primary: true}
	sel1, idx1, ok1 := resolveSelector(selector, broken1)
	if !ok1 || idx1 != 0 || sel1 != selector.Fallbacks[0] {
		t.Fatalf("expected fallback 0, got ok=%v, idx=%d, sel=%s", ok1, idx1, sel1)
	}

	// Case 2: Primary and Fallback 0 broken -> Fallback 1 selected
	broken2 := map[string]bool{selector.Primary: true, selector.Fallbacks[0]: true}
	sel2, idx2, ok2 := resolveSelector(selector, broken2)
	if !ok2 || idx2 != 1 || sel2 != selector.Fallbacks[1] {
		t.Fatalf("expected fallback 1, got ok=%v, idx=%d, sel=%s", ok2, idx2, sel2)
	}

	// Case 3: All selectors broken -> returns failure
	broken3 := map[string]bool{
		selector.Primary:      true,
		selector.Fallbacks[0]: true,
		selector.Fallbacks[1]: true,
	}
	_, _, ok3 := resolveSelector(selector, broken3)
	if ok3 {
		t.Fatalf("expected failure when all selector candidates broken")
	}
}

// TestTier3_Feature16_17_AutomaticChannelPauseOn3FailuresWithHumanAlert tests that
// exactly 3 consecutive failures trigger automatic channel pause AND a human escalation alert.
func TestTier3_Feature16_17_AutomaticChannelPauseOn3FailuresWithHumanAlert(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 11, 15, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	ch := h.Channels["whatsapp"]
	ch.SetFailSend(true, "upstream 503 service unavailable")

	// 1st failure
	_, _ = h.ExecuteFlow(ctx, "whatsapp", "conv-pause-1", "Hola", now)
	if h.Resilience.IsPaused("whatsapp") {
		t.Fatalf("channel paused prematurely after 1 failure")
	}

	// 2nd failure
	_, _ = h.ExecuteFlow(ctx, "whatsapp", "conv-pause-2", "Hola", now.Add(5*time.Second))
	if h.Resilience.IsPaused("whatsapp") {
		t.Fatalf("channel paused prematurely after 2 failures")
	}

	// 3rd failure: TRIGGER AUTO-PAUSE (Feature 16)
	_, _ = h.ExecuteFlow(ctx, "whatsapp", "conv-pause-3", "Hola", now.Add(10*time.Second))
	if !h.Resilience.IsPaused("whatsapp") {
		t.Fatalf("expected channel whatsapp to be auto-paused after 3 consecutive failures")
	}

	// Verify emergency alert ticket in escalate.Queue (Feature 17)
	pending := h.EscQueue.Pending()
	alertFound := false
	for _, ticket := range pending {
		if ticket.ChannelID == "whatsapp" && ticket.Reason == "channel_auto_paused_consecutive_failures" {
			alertFound = true
			break
		}
	}
	if !alertFound {
		t.Fatalf("expected human escalation ticket with reason channel_auto_paused_consecutive_failures")
	}

	// 4th incoming message on paused channel: must be skipped with 'channel_is_paused'
	out4, err4 := h.ExecuteFlow(ctx, "whatsapp", "conv-pause-4", "Hola", now.Add(15*time.Second))
	if err4 != nil {
		t.Fatalf("unexpected error on skipped conversation: %v", err4)
	}
	if !out4.Skipped || out4.SkipReason != "channel_is_paused" {
		t.Fatalf("expected skipped with channel_is_paused, got skipped=%v, reason=%s", out4.Skipped, out4.SkipReason)
	}
}

// TestTier3_Feature17_NoAlertStormOnRepeatedFailuresAfterPause validates that once a channel
// enters paused state, additional failures do NOT flood the human queue with duplicate alerts.
func TestTier3_Feature17_NoAlertStormOnRepeatedFailuresAfterPause(t *testing.T) {
	escQ := escalate.NewQueue()
	tracker := NewMemoryResilienceTracker(escQ)
	chID := "email_pilot"

	// Trigger 3 failures -> paused
	tracker.RecordFailure(chID, "err 1")
	tracker.RecordFailure(chID, "err 2")
	tracker.RecordFailure(chID, "err 3")

	if !tracker.IsPaused(chID) {
		t.Fatalf("expected channel to be paused")
	}
	if len(escQ.Pending()) != 1 {
		t.Fatalf("expected exactly 1 alert ticket after 3rd failure, got %d", len(escQ.Pending()))
	}

	// Trigger 4th and 5th failures on already-paused channel
	tracker.RecordFailure(chID, "err 4")
	tracker.RecordFailure(chID, "err 5")

	// Total alert count must REMAIN exactly 1 (no alert storm!)
	if len(escQ.Pending()) != 1 {
		t.Fatalf("alert storm detected! Expected exactly 1 alert ticket, got %d", len(escQ.Pending()))
	}
}

// TestTier3_Feature18_ChannelFaultIsolationUnderFailure validates that pausing Channel A
// leaves Channel B and Channel C operating at 100% capacity with 0 interference.
func TestTier3_Feature18_ChannelFaultIsolationUnderFailure(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 11, 30, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	// Simulate catastrophic failure on whatsapp channel
	wa := h.Channels["whatsapp"]
	wa.SetFailSend(true, "fatal whatsapp connection timeout")

	for i := 1; i <= 3; i++ {
		_, _ = h.ExecuteFlow(ctx, "whatsapp", "conv-isolate-wa-"+string(rune('0'+i)), "Hola", now.Add(time.Duration(i)*time.Second))
	}

	if !h.Resilience.IsPaused("whatsapp") {
		t.Fatalf("expected whatsapp to be paused")
	}

	// Channel B (email_pilot) and Channel C (web) must be completely unaffected
	if h.Resilience.IsPaused("email_pilot") {
		t.Fatalf("fault leak! email_pilot should NOT be paused")
	}
	if h.Resilience.IsPaused("web") {
		t.Fatalf("fault leak! web should NOT be paused")
	}

	// Execute 20 transactions on email_pilot and web
	for i := 1; i <= 10; i++ {
		emailConv := "conv-isolate-email-" + string(rune('a'+i))
		outEmail, errEmail := h.ExecuteFlow(ctx, "email_pilot", emailConv, "Donde quedan ubicados en Tunja?", now.Add(time.Duration(i)*time.Second))
		if errEmail != nil || outEmail.Skipped || outEmail.Escalated {
			t.Fatalf("email_pilot failed during whatsapp outage: err=%v, skipped=%v, escalated=%v", errEmail, outEmail.Skipped, outEmail.Escalated)
		}

		webConv := "conv-isolate-web-" + string(rune('a'+i))
		outWeb, errWeb := h.ExecuteFlow(ctx, "web", webConv, "Que horario de atencion tienen?", now.Add(time.Duration(i)*time.Second))
		if errWeb != nil || outWeb.Skipped || outWeb.Escalated {
			t.Fatalf("web failed during whatsapp outage: err=%v, skipped=%v, escalated=%v", errWeb, outWeb.Skipped, outWeb.Escalated)
		}
	}

	// Verify reply counts
	if len(h.Channels["email_pilot"].GetRepliesSent()) != 10 {
		t.Fatalf("expected 10 replies dispatched on email_pilot")
	}
	if len(h.Channels["web"].GetRepliesSent()) != 10 {
		t.Fatalf("expected 10 replies dispatched on web")
	}
}

// ============================================================================
// NEW EVOLUTION TIER 3 RESILIENCE TESTS (FEATURES 1–28)
// ============================================================================

// TestTier3_Feature06_WhatsAppCloud_FailoverToPlaywrightOn429OrPendingWABA verifies automatic
// failover to browser automation when Cloud API returns 429 quota or pending WABA (Feature 6).
func TestTier3_Feature06_WhatsAppCloud_FailoverToPlaywrightOn429OrPendingWABA(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 13, 0, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	wa := h.WhatsAppCloud

	// 1. Activate Quota 429 condition
	wa.SimulateQuota429 = true

	res, err := wa.SendMessage(ctx, "573151212223", "Hola desde contingencia", false)
	if err != nil {
		t.Fatalf("expected successful failover dispatch, got err: %v", err)
	}
	if !wa.FallbackActive {
		t.Fatalf("expected FallbackActive to be true after 429 error")
	}
	if res.Status != channel.SendSimulated {
		t.Fatalf("expected SendSimulated, got: %s", res.Status)
	}

	// Verify fallback adapter recorded message
	replies := wa.FallbackAdapter.GetRepliesSent()
	if len(replies) != 1 {
		t.Fatalf("expected 1 reply dispatched to Playwright fallback adapter, got %d", len(replies))
	}
}

// TestTier3_Feature10_ResilienceTracker_ThreeConsecutiveFailuresAutoPausesNewAdapters verifies
// that new omnichannel adapters (Messenger, Instagram, MercadoLibre) trigger auto-pause on 3rd failure (Feature 10).
func TestTier3_Feature10_ResilienceTracker_ThreeConsecutiveFailuresAutoPausesNewAdapters(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 13, 5, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	chID := "facebook_messenger"
	ch := h.Channels[chID]
	ch.SetFailSend(true, "Meta Graph API 500 internal server error")

	// 1st failure
	_, _ = h.ExecuteFlow(ctx, chID, "conv-res-fb-1", "Hola", now)
	if h.Resilience.IsPaused(chID) {
		t.Fatalf("unexpected early pause after 1 failure")
	}

	// 2nd failure
	_, _ = h.ExecuteFlow(ctx, chID, "conv-res-fb-2", "Hola", now.Add(2*time.Second))
	if h.Resilience.IsPaused(chID) {
		t.Fatalf("unexpected early pause after 2 failures")
	}

	// 3rd failure: TRIGGER AUTO-PAUSE (Mandament 1, Feature 10)
	_, _ = h.ExecuteFlow(ctx, chID, "conv-res-fb-3", "Hola", now.Add(4*time.Second))
	if !h.Resilience.IsPaused(chID) {
		t.Fatalf("expected channel %s to be auto-paused after 3 consecutive failures", chID)
	}

	// 4th message: skipped
	out4, _ := h.ExecuteFlow(ctx, chID, "conv-res-fb-4", "Hola", now.Add(6*time.Second))
	if !out4.Skipped || out4.SkipReason != "channel_is_paused" {
		t.Fatalf("expected 4th message to be skipped with channel_is_paused")
	}
}

// TestTier3_Feature12_ResilientStore_DatabaseFailureFallbackToFlatJSONLog verifies automatic
// failover to local flat JSON log on database failure, and subsequent replay on primary recovery (Feature 12).
func TestTier3_Feature12_ResilientStore_DatabaseFailureFallbackToFlatJSONLog(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 13, 10, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	rs := h.ResilientStore

	// 1. Force failure on primary SQLite/ACID store
	rs.ForceFallback()
	if !rs.IsUsingFallback() {
		t.Fatalf("expected IsUsingFallback to be true")
	}

	// 2. Persist record during outage (must save to flat JSON append log)
	rec := store.IdempotencyRecord{
		Hash:       "hash-fallback-001",
		ChannelID:  "whatsapp",
		ConvID:     "conv-fb-001",
		ReplyText:  "Respuesta en contingencia",
		CreatedAt:  now,
		SendStatus: "simulated_no_op",
	}
	if err := rs.SaveIdempotency(rec); err != nil {
		t.Fatalf("fallback SaveIdempotency failed: %v", err)
	}

	// Must be retrievable from fallback log
	saved, ok := rs.GetIdempotency("hash-fallback-001")
	if !ok || saved.ConvID != "conv-fb-001" {
		t.Fatalf("failed to retrieve record from fallback log")
	}

	// 3. Recover primary store and verify replay
	if err := rs.RecoverPrimary(ctx); err != nil {
		t.Fatalf("RecoverPrimary failed: %v", err)
	}
	if rs.IsUsingFallback() {
		t.Fatalf("expected IsUsingFallback to be false after recovery")
	}

	// Record must now exist in primary store!
	replayed, ok2 := rs.GetIdempotency("hash-fallback-001")
	if !ok2 || replayed.ConvID != "conv-fb-001" {
		t.Fatalf("replayed record missing from primary after recovery")
	}
}

// TestTier3_Feature13_DMSSyncWorker_NetworkFailureRetryAndStaleGrace verifies that when
// DMS sync fails, existing cache snapshot is preserved until 30m grace window expires (Feature 13).
func TestTier3_Feature13_DMSSyncWorker_NetworkFailureRetryAndStaleGrace(t *testing.T) {
	ctx := context.Background()
	t0 := time.Date(2026, 10, 29, 13, 15, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, t0)
	defer h.Close()

	dms := h.DMSWorker
	dms.failNetwork = true

	// Sync fails due to network outage
	_, err := dms.Sync(ctx, "etag-net-fail", h.InvWrapper, t0)
	if err == nil {
		t.Fatalf("expected network failure error from DMS worker")
	}

	// Within 30 min (e.g. +15 min): Cache remains fresh despite sync failure
	t15 := t0.Add(15 * time.Minute)
	searchFresh := h.InvWrapper.Cache().Search(ctx, inventory.Query{Brand: "Toyota"}, t15)
	if searchFresh.Status == inventory.StatusStale {
		t.Fatalf("cache marked stale prematurely during network outage grace period")
	}

	// Past 30 min (e.g. +31 min): Cache becomes stale
	t31 := t0.Add(31 * time.Minute)
	searchStale := h.InvWrapper.Cache().Search(ctx, inventory.Query{Brand: "Toyota"}, t31)
	if searchStale.Status != inventory.StatusStale {
		t.Fatalf("expected StatusStale after 31 min without successful sync, got: %s", searchStale.Status)
	}
}

// TestTier3_Feature17_OutboundCRM_ExponentialBackoffExhaustionToEscalateQueue verifies that
// CRM webhook retries 3 times with backoff, then enqueues to escalate.Queue (Feature 17).
func TestTier3_Feature17_OutboundCRM_ExponentialBackoffExhaustionToEscalateQueue(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 13, 20, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	dispatcher := h.CRMDispatcher
	dispatcher.FailUpstream = true // simulate upstream CRM 500 error

	task := CRMTask{
		ID:          "crm-task-fail-101",
		ConvID:      "conv-backoff-101",
		ChannelID:   "whatsapp",
		Type:        TaskCommercialFollowup,
		Description: "Lead de alto valor",
		CreatedAt:   now,
	}

	err := dispatcher.Dispatch(ctx, task)
	if err == nil {
		t.Fatalf("expected dispatch error when upstream CRM fails")
	}
	if dispatcher.AttemptsMade != 3 {
		t.Fatalf("expected exactly 3 retry attempts, made %d", dispatcher.AttemptsMade)
	}

	// Verify enqueued to escalate.Queue
	pending := h.EscQueue.Pending()
	found := false
	for _, ticket := range pending {
		if ticket.Reason == "crm_webhook_exhausted" && ticket.ConversationID == "conv-backoff-101" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected crm_webhook_exhausted ticket in escalate.Queue")
	}
}

// TestTier3_Feature20_FreeInferenceCascade_L1OllamaDown_L2GroqDown_L3FastPathFallback tests
// cascading failover from Tier 1 (Ollama) to Tier 2 (Groq) to Tier 3 (FastPath) (Feature 20).
func TestTier3_Feature20_FreeInferenceCascade_L1OllamaDown_L2GroqDown_L3FastPathFallback(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 13, 25, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	cascade := h.Cascade

	// 1. Both L1 and L2 available -> Tier 1 (Ollama)
	_, tier1, _ := cascade.Resolve(ctx, "Consulta general")
	if tier1 != 1 {
		t.Fatalf("step 1: expected tier 1, got %d", tier1)
	}

	// 2. L1 Ollama down -> Cascades to Tier 2 (Groq)
	cascade.OllamaAvailable = false
	_, tier2, _ := cascade.Resolve(ctx, "Consulta general")
	if tier2 != 2 {
		t.Fatalf("step 2: expected tier 2, got %d", tier2)
	}

	// 3. Both L1 Ollama and L2 Groq down -> Cascades to Tier 3 (FastPath determinista)
	cascade.GroqAvailable = false
	ans3, tier3, _ := cascade.Resolve(ctx, "ubicacion")
	if tier3 != 3 {
		t.Fatalf("step 3: expected tier 3 FastPath, got %d", tier3)
	}
	if !strings.Contains(ans3, "Cra. 6 #52-23") {
		t.Fatalf("step 3: expected FastPath business text reply, got: %s", ans3)
	}
}

// TestTier3_Feature26_ChannelUnpauseAPI_RestoresTraffic verifies that calling the unpause
// API clears the paused state and allows message processing to resume (Feature 26).
func TestTier3_Feature26_ChannelUnpauseAPI_RestoresTraffic(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 13, 30, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	chID := "whatsapp"

	// 1. Trigger 3 failures -> paused
	h.Resilience.RecordFailure(chID, "error 1")
	h.Resilience.RecordFailure(chID, "error 2")
	h.Resilience.RecordFailure(chID, "error 3")
	if !h.Resilience.IsPaused(chID) {
		t.Fatalf("expected channel to be paused")
	}

	// 2. Verify traffic skipped while paused
	outSkip, _ := h.ExecuteFlow(ctx, chID, "conv-paused-test", "Hola", now)
	if !outSkip.Skipped {
		t.Fatalf("expected message to be skipped while channel is paused")
	}

	// 3. Operator invokes unpause API
	unpauseURL := h.WebServer.Server.URL + "/api/channel/unpause?channel=" + chID
	resp, err := http.Post(unpauseURL, "application/json", nil)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("unpause API failed: err=%v, status=%d", err, resp.StatusCode)
	}

	// 4. Verify channel is no longer paused
	if h.Resilience.IsPaused(chID) {
		t.Fatalf("expected channel %s to be unpaused after API call", chID)
	}

	// 5. Subsequent message succeeds!
	outSuccess, errSuccess := h.ExecuteFlow(ctx, chID, "conv-resumed-test", "Donde quedan ubicados en Tunja?", now.Add(5*time.Second))
	if errSuccess != nil || outSuccess.Skipped || outSuccess.Escalated {
		t.Fatalf("expected successful flow after unpause: err=%v, skipped=%v, escalated=%v", errSuccess, outSuccess.Skipped, outSuccess.Escalated)
	}
}
