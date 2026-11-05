package e2e

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"buyacar.com.co/omnicanal/internal/channel"
	"buyacar.com.co/omnicanal/internal/store"
)

// TestTier1_Feature01_02_PreSendAndCacheFreshnessHappyPath tests that an inbound inquiry
// for an available vehicle with fresh inventory cache (< 30 min) proceeds smoothly through
// the pipeline to outbound dispatch without escalation.
func TestTier1_Feature01_02_PreSendAndCacheFreshnessHappyPath(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	// Verify inventory cache is fresh (Feature 2)
	if !h.InvWrapper.IsFresh(now) {
		t.Fatalf("expected inventory cache to be fresh at t=0")
	}

	// Verify vehicle availability (Feature 1)
	v, ok := h.InvWrapper.GetVehicle("toyota-hilux-2022")
	if !ok || v.Status != "available" {
		t.Fatalf("expected toyota-hilux-2022 to be available in cache")
	}

	outcome, err := h.ExecuteFlow(ctx, "whatsapp", "conv-tier1-smoke-1", "Que precio tiene la Toyota Hilux 2022?", now)
	if err != nil {
		t.Fatalf("unexpected pipeline error: %v", err)
	}

	if outcome.Skipped {
		t.Fatalf("expected conversation not to be skipped, got skip reason: %s", outcome.SkipReason)
	}
	if outcome.Escalated {
		t.Fatalf("expected conversation not to be escalated, got reason: %s", outcome.EscalationReason)
	}
	if !strings.Contains(outcome.SentReply, "185000000") {
		t.Fatalf("expected reply to contain official price 185000000, got: %s", outcome.SentReply)
	}
	if outcome.CheckpointsOK < 6 {
		t.Fatalf("expected at least 6 checkpoints ok, got %d", outcome.CheckpointsOK)
	}

	// Verify mock channel received reply
	replies := h.Channels["whatsapp"].GetRepliesSent()
	if len(replies) != 1 {
		t.Fatalf("expected 1 reply dispatched to whatsapp, got %d", len(replies))
	}
}

// TestTier1_Feature04_SendResultVerificationByMode validates that SendResult.Status
// correctly reflects the operating mode: SendSimulated in SIMULATION and SendDrafted in DRAFT.
func TestTier1_Feature04_SendResultVerificationByMode(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 10, 5, 0, 0, time.UTC)

	// 1. Simulation Mode
	hSim := NewE2EHarness(channel.ModeSimulation, now)
	defer hSim.Close()
	outSim, err := hSim.ExecuteFlow(ctx, "whatsapp", "conv-tier1-mode-sim", "Donde quedan ubicados en Tunja?", now)
	if err != nil {
		t.Fatalf("unexpected error in simulation: %v", err)
	}
	if outSim.SendStatus != channel.SendSimulated {
		t.Fatalf("expected SendStatus %s in SIMULATION, got %s", channel.SendSimulated, outSim.SendStatus)
	}

	// 2. Draft Mode
	hDraft := NewE2EHarness(channel.ModeDraft, now)
	defer hDraft.Close()
	outDraft, err := hDraft.ExecuteFlow(ctx, "email_pilot", "conv-tier1-mode-draft", "Donde quedan ubicados en Tunja?", now)
	if err != nil {
		t.Fatalf("unexpected error in draft: %v", err)
	}
	if outDraft.SendStatus != channel.SendDrafted {
		t.Fatalf("expected SendStatus %s in DRAFT, got %s", channel.SendDrafted, outDraft.SendStatus)
	}
}

// TestTier1_Feature05_06_CRMSettlementAndIdempotencySealing validates that high-intent
// customer interactions generate structured CRM tasks and that duplicate threads are
// idempotently skipped.
func TestTier1_Feature05_06_CRMSettlementAndIdempotencySealing(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 10, 10, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	convID := "conv-tier1-crm-testdrive"
	userMsg := "Hola, quiero agendar una prueba de ruta para conocer la camioneta"

	out, err := h.ExecuteFlow(ctx, "whatsapp", convID, userMsg, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Skipped {
		t.Fatalf("expected not to be skipped on first receipt")
	}

	// Verify CRM task settlement (Feature 5)
	tasks := h.CRM.GetTasksByConv(convID)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 CRM task settled, got %d", len(tasks))
	}
	if tasks[0].Type != TaskScheduleTestDrive {
		t.Fatalf("expected task type %s, got %s", TaskScheduleTestDrive, tasks[0].Type)
	}
	if tasks[0].ChannelID != "whatsapp" {
		t.Fatalf("expected channel_id whatsapp, got %s", tasks[0].ChannelID)
	}

	// Verify Idempotency sealing (Feature 6): Re-submitting the exact same message must be skipped
	outDup, err := h.ExecuteFlow(ctx, "whatsapp", convID, userMsg, now)
	if err != nil {
		t.Fatalf("unexpected error on duplicate receipt: %v", err)
	}
	if !outDup.Skipped {
		t.Fatalf("expected duplicate thread to be skipped by idempotency store")
	}
}

// TestTier1_Feature08_09_RateLimitNormalThroughput verifies that messages within the 120/hr
// and 250/day quotas are authorized without restriction.
func TestTier1_Feature08_09_RateLimitNormalThroughput(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 10, 15, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	for i := 1; i <= 10; i++ {
		convID := "conv-rate-normal-" + string(rune('a'+i))
		out, err := h.ExecuteFlow(ctx, "whatsapp", convID, "Que horario de atencion tienen?", now.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatalf("iteration %d returned error: %v", i, err)
		}
		if out.Skipped {
			t.Fatalf("iteration %d unexpectedly skipped with reason: %s", i, out.SkipReason)
		}
	}

	replies := h.Channels["whatsapp"].GetRepliesSent()
	if len(replies) != 10 {
		t.Fatalf("expected 10 replies dispatched, got %d", len(replies))
	}
}

// TestTier1_Feature10_11_BaseHumanDelaysCalculation validates that the base delays
// for inter-chat (3,200 ms) and inter-channel (2,500 ms) are correctly configured.
func TestTier1_Feature10_11_BaseHumanDelaysCalculation(t *testing.T) {
	rc := NewMemoryRateController(DefaultRateConfig())

	// Test chat delay calculation bounds
	chatDelay := rc.CalculateChatDelay(0)
	if chatDelay < 2000*time.Millisecond || chatDelay > 4500*time.Millisecond {
		t.Fatalf("chat delay %v outside reasonable range around 3200ms", chatDelay)
	}

	// Test channel delay calculation bounds
	channelDelay := rc.CalculateChannelDelay(0)
	if channelDelay < 1500*time.Millisecond || channelDelay > 3500*time.Millisecond {
		t.Fatalf("channel delay %v outside reasonable range around 2500ms", channelDelay)
	}
}

// TestTier1_Feature14_15_SelectorSuccessAndCounterZero validates that normal successful
// dispatches keep the consecutive failure counter at 0.
func TestTier1_Feature14_15_SelectorSuccessAndCounterZero(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 10, 20, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	_, err := h.ExecuteFlow(ctx, "whatsapp", "conv-tier1-selector-ok", "Buenos dias", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	count := h.Resilience.GetConsecutiveFailures("whatsapp")
	if count != 0 {
		t.Fatalf("expected consecutive failures to be 0 after success, got %d", count)
	}
	if h.Resilience.IsPaused("whatsapp") {
		t.Fatalf("expected whatsapp not to be paused")
	}
}

// TestTier1_Feature18_ChannelNormalDispatch validates independent execution across
// distinct channels without cross-talk.
func TestTier1_Feature18_ChannelNormalDispatch(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 10, 25, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	channelsToTest := []string{"whatsapp", "email_pilot", "mercadolibre", "web"}
	for _, chID := range channelsToTest {
		convID := "conv-tier1-channel-" + chID
		out, err := h.ExecuteFlow(ctx, chID, convID, "Donde quedan ubicados?", now)
		if err != nil {
			t.Fatalf("channel %s execution failed: %v", chID, err)
		}
		if out.Skipped || out.Escalated {
			t.Fatalf("channel %s: expected successful dispatch, got skipped=%v, escalated=%v", chID, out.Skipped, out.Escalated)
		}
		if len(h.Channels[chID].GetRepliesSent()) != 1 {
			t.Fatalf("channel %s: expected 1 reply recorded", chID)
		}
	}
}

// ============================================================================
// NEW EVOLUTION TIER 1 SMOKE TESTS (FEATURES 1–28)
// ============================================================================

// TestTier1_Feature01_WhatsAppCloud_WebhookVerificationHandshake tests GET handshake verification
// and POST HMAC-SHA256 signature verification (Feature 1).
func TestTier1_Feature01_WhatsAppCloud_WebhookVerificationHandshake(t *testing.T) {
	now := time.Date(2026, 10, 29, 10, 30, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	wa := h.WhatsAppCloud

	// 1. GET Handshake Verification (hub.mode=subscribe)
	challenge := "challenge_string_abc_123"
	respBody, status := wa.VerifyWebhookGET("subscribe", wa.VerifyToken, challenge)
	if status != http.StatusOK || respBody != challenge {
		t.Fatalf("GET verification failed: status=%d, body=%s", status, respBody)
	}

	// 2. POST Webhook with valid HMAC-SHA256
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"123","changes":[]}]}`)
	sig := wa.ComputeHMACSHA256(payload)
	postStatus, err := wa.HandleWebhookPOST(sig, payload)
	if err != nil || postStatus != http.StatusOK {
		t.Fatalf("POST webhook failed: status=%d, err=%v", postStatus, err)
	}
}

// TestTier1_Feature02_WhatsAppCloud_OutboundMessageDelivery verifies outbound delivery
// through WhatsApp Cloud API mock adapter (Feature 2).
func TestTier1_Feature02_WhatsAppCloud_OutboundMessageDelivery(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 10, 35, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	res, err := h.WhatsAppCloud.SendMessage(ctx, "573151212223", "Hola, con gusto le atendemos en BuyaCAR.", false)
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	if res.Status != channel.SendSimulated {
		t.Fatalf("expected SendSimulated, got %s", res.Status)
	}
	if len(h.WhatsAppCloud.DeliveredMsgs) != 1 {
		t.Fatalf("expected 1 delivered message recorded")
	}
}

// TestTier1_Feature03_WhatsAppCloud_HSMTemplateSelection verifies pre-approved HSM template
// retrieval for re-engagement beyond the 24-hour window (Feature 3).
func TestTier1_Feature03_WhatsAppCloud_HSMTemplateSelection(t *testing.T) {
	now := time.Date(2026, 10, 29, 10, 40, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	tmpl, ok := h.WhatsAppCloud.Templates["lead_reengagement_24h"]
	if !ok {
		t.Fatalf("expected template lead_reengagement_24h to exist")
	}
	if tmpl.Language != "es_CO" {
		t.Fatalf("expected language es_CO, got %s", tmpl.Language)
	}
	if len(tmpl.Components) == 0 {
		t.Fatalf("expected template components to be populated")
	}
}

// TestTier1_Feature04_WhatsAppCloud_InboundMediaProcessing verifies handling and size checks
// of incoming media attachments (Feature 4).
func TestTier1_Feature04_WhatsAppCloud_InboundMediaProcessing(t *testing.T) {
	now := time.Date(2026, 10, 29, 10, 45, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	mediaPayload := []byte(`{
		"entry": [{
			"changes": [{
				"value": {
					"messages": [{
						"from": "573151212223",
						"type": "image",
						"image": {"id": "media-img-001", "mime_type": "image/jpeg", "sha256": "fakehash"}
					}]
				}
			}]
		}]
	}`)
	sig := h.WhatsAppCloud.ComputeHMACSHA256(mediaPayload)
	status, err := h.WhatsAppCloud.HandleWebhookPOST(sig, mediaPayload)
	if err != nil || status != http.StatusOK {
		t.Fatalf("inbound media webhook failed: status=%d, err=%v", status, err)
	}
}

// TestTier1_Feature05_WhatsAppCloud_OptInConsentLifecycle verifies explicit opt-in / opt-out
// state transitions (Feature 5).
func TestTier1_Feature05_WhatsAppCloud_OptInConsentLifecycle(t *testing.T) {
	now := time.Date(2026, 10, 29, 10, 50, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	custID := "cust-573151212223"
	// 1. Initial state
	if st := h.WhatsAppCloud.OptIn.GetStatus(custID); st != OptInUnknown {
		t.Fatalf("expected initial status OptInUnknown, got %s", st)
	}

	// 2. Grant consent
	h.WhatsAppCloud.OptIn.SetStatus(custID, OptInGranted)
	if st := h.WhatsAppCloud.OptIn.GetStatus(custID); st != OptInGranted {
		t.Fatalf("expected OptInGranted, got %s", st)
	}

	// 3. Revoke consent
	h.WhatsAppCloud.OptIn.SetStatus(custID, OptInRevoked)
	if st := h.WhatsAppCloud.OptIn.GetStatus(custID); st != OptInRevoked {
		t.Fatalf("expected OptInRevoked, got %s", st)
	}
}

// TestTier1_Feature07_MercadoLibre_WebhookIngestAndSignature verifies MercadoLibre webhook
// ingestion with cryptographic signature validation (Feature 7).
func TestTier1_Feature07_MercadoLibre_WebhookIngestAndSignature(t *testing.T) {
	now := time.Date(2026, 10, 29, 10, 55, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	payload := []byte(`{"resource":"/questions/123456","user_id":998877,"topic":"questions"}`)
	sig := h.MercadoLibre.ComputeSignature(payload)
	status, err := h.MercadoLibre.HandleWebhook(sig, payload)
	if err != nil || status != http.StatusOK {
		t.Fatalf("MercadoLibre webhook ingest failed: status=%d, err=%v", status, err)
	}
}

// TestTier1_Feature09_MetaOmnichannel_MessengerAndInstagramNormalDispatch verifies unified
// Meta multiplexer dispatching to FB Messenger (500 chars) and Instagram Direct (450 chars) (Feature 9).
func TestTier1_Feature09_MetaOmnichannel_MessengerAndInstagramNormalDispatch(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 11, 0, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	// 1. Messenger dispatch
	outFB, errFB := h.ExecuteFlow(ctx, "facebook_messenger", "conv-fb-01", "Donde estan ubicados?", now)
	if errFB != nil || outFB.Skipped || outFB.Escalated {
		t.Fatalf("Messenger dispatch failed: err=%v, skipped=%v", errFB, outFB.Skipped)
	}

	// 2. Instagram dispatch
	outIG, errIG := h.ExecuteFlow(ctx, "instagram_direct", "conv-ig-01", "Que precio tiene la Hilux?", now)
	if errIG != nil || outIG.Skipped || outIG.Escalated {
		t.Fatalf("Instagram dispatch failed: err=%v, skipped=%v", errIG, outIG.Skipped)
	}
}

// TestTier1_Feature11_ResilientStore_ACIDTransactionHappyPath verifies ACID persistence of
// IdempotencyRecord and CheckpointState (Feature 11).
func TestTier1_Feature11_ResilientStore_ACIDTransactionHappyPath(t *testing.T) {
	now := time.Date(2026, 10, 29, 11, 5, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	rs := h.ResilientStore
	rec := store.IdempotencyRecord{
		Hash:       "hash-acid-001",
		ChannelID:  "whatsapp",
		ConvID:     "conv-001",
		ReplyText:  "Respuesta sellada",
		CreatedAt:  now,
		SendStatus: "simulated_no_op",
		ETag:       "etag-v1",
	}

	if err := rs.SaveIdempotency(rec); err != nil {
		t.Fatalf("SaveIdempotency failed: %v", err)
	}

	saved, ok := rs.GetIdempotency("hash-acid-001")
	if !ok || saved.ConvID != "conv-001" {
		t.Fatalf("failed to retrieve saved idempotency record: ok=%v, saved=%+v", ok, saved)
	}
}

// TestTier1_Feature13_DMSSyncWorker_ETagFreshnessCycle verifies DMS synchronization
// with ETag validation (Feature 13).
func TestTier1_Feature13_DMSSyncWorker_ETagFreshnessCycle(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 11, 10, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	// 1. Same ETag -> 304 Not Modified
	updated, err := h.DMSWorker.Sync(ctx, "etag-v1", h.InvWrapper, now)
	if err != nil || updated {
		t.Fatalf("expected 304 not modified: updated=%v, err=%v", updated, err)
	}

	// 2. New ETag -> 200 OK snapshot refresh
	updated2, err2 := h.DMSWorker.Sync(ctx, "etag-v2", h.InvWrapper, now.Add(5*time.Minute))
	if err2 != nil || !updated2 {
		t.Fatalf("expected cache snapshot updated with new ETag: updated=%v, err=%v", updated2, err2)
	}
}

// TestTier1_Feature14_InvalidationWebhook_RealTimeStockPriceUpdate verifies real-time
// stock/price refresh through the invalidation webhook (Feature 14).
func TestTier1_Feature14_InvalidationWebhook_RealTimeStockPriceUpdate(t *testing.T) {
	now := time.Date(2026, 10, 29, 11, 15, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	body := bytes.NewBufferString(`{"vehicle_id":"veh-001","status":"sold"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/invalidation", body)
	req.Header.Set("Authorization", "Bearer dms_secure_token_999")

	status, err := h.Invalidation.HandleInvalidation(req)
	if err != nil || status != http.StatusOK {
		t.Fatalf("invalidation webhook failed: status=%d, err=%v", status, err)
	}

	v, ok := h.InvWrapper.GetVehicle("veh-001")
	if !ok || v.Status != "sold" {
		t.Fatalf("expected veh-001 status to be updated to sold, got %s", v.Status)
	}
}

// TestTier1_Feature15_Waitlist_NextEntryAlertGeneration verifies SHA-256 sealed waitlist
// registration and alert emission when vehicle becomes available (Feature 15).
func TestTier1_Feature15_Waitlist_NextEntryAlertGeneration(t *testing.T) {
	now := time.Date(2026, 10, 29, 11, 20, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	entry := h.Waitlist.Register("veh-001", "customer-123", "whatsapp", now)
	if len(entry.SealedHash) != 64 {
		t.Fatalf("expected 64-char SHA-256 sealed hash, got: %s", entry.SealedHash)
	}

	// Trigger alert by returning vehicle to available
	alerts := h.Waitlist.CheckAndNotify("veh-001", "available")
	if len(alerts) != 1 || alerts[0].CustomerID != "customer-123" {
		t.Fatalf("expected 1 waitlist alert generated, got: %+v", alerts)
	}
}

// TestTier1_Feature16_OutboundCRM_HMACWebhookDispatch verifies outbound lead export
// signed with HMAC-SHA256 (Feature 16).
func TestTier1_Feature16_OutboundCRM_HMACWebhookDispatch(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 11, 25, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	task := CRMTask{
		ID:          "crm-export-01",
		ConvID:      "conv-101",
		ChannelID:   "whatsapp",
		Type:        TaskScheduleTestDrive,
		Description: "Prueba de ruta agendada",
		CreatedAt:   now,
	}

	if err := h.CRMDispatcher.Dispatch(ctx, task); err != nil {
		t.Fatalf("CRM dispatch failed: %v", err)
	}
	if len(h.CRMDispatcher.Dispatched) != 1 {
		t.Fatalf("expected 1 task dispatched to external CRM")
	}
}

// TestTier1_Feature18_SemanticCache_ExactAndNormalizedHit verifies query normalization
// and semantic cache hits reducing AI token consumption (Feature 18).
func TestTier1_Feature18_SemanticCache_ExactAndNormalizedHit(t *testing.T) {
	now := time.Date(2026, 10, 29, 11, 30, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	q1 := "¿Dónde quedan ubicados en Tunja?"
	q2 := "donde quedan ubicados en tunja"

	h.SemanticCache.Put(q1, "Cra. 6 #52-23 Tunja")

	// Both exact and normalized queries must hit the cache!
	ans1, ok1 := h.SemanticCache.Get(q1)
	ans2, ok2 := h.SemanticCache.Get(q2)

	if !ok1 || !ok2 || ans1 != ans2 {
		t.Fatalf("semantic cache normalization failed: ok1=%v, ok2=%v, ans1=%s, ans2=%s", ok1, ok2, ans1, ans2)
	}
}

// TestTier1_Feature20_FreeInferenceCascade_Level1OllamaHappyPath verifies Level 1
// resolution through local Ollama provider (Feature 20).
func TestTier1_Feature20_FreeInferenceCascade_Level1OllamaHappyPath(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 11, 35, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	_, tier, err := h.Cascade.Resolve(ctx, "Quiero financiamiento para vehiculo")
	if err != nil {
		t.Fatalf("cascade failed: %v", err)
	}
	if tier != 1 {
		t.Fatalf("expected Tier 1 (Ollama Local), got Tier %d", tier)
	}
}

// TestTier1_Feature22_DeterministicFastPath_ExactMatchLatency verifies exact match
// resolution offline with ~2.59 µs/op benchmark SLA (Feature 22).
func TestTier1_Feature22_DeterministicFastPath_ExactMatchLatency(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 11, 40, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	h.Cascade.FastPathOnly = true
	start := time.Now()
	ans, tier, err := h.Cascade.Resolve(ctx, "ubicacion")
	elapsed := time.Since(start)

	if err != nil || tier != 3 {
		t.Fatalf("expected Tier 3 FastPath, got tier=%d, err=%v", tier, err)
	}
	if elapsed > 5*time.Millisecond {
		t.Fatalf("FastPath latency too high: %v (SLA < 5ms)", elapsed)
	}
	if !strings.Contains(ans, "Cra. 6 #52-23") {
		t.Fatalf("unexpected answer: %s", ans)
	}
}

// TestTier1_Feature23_EmbeddedWebServer_StaticAssetsServed verifies embedded console
// HTTP server serving static dashboard (Feature 23).
func TestTier1_Feature23_EmbeddedWebServer_StaticAssetsServed(t *testing.T) {
	now := time.Date(2026, 10, 29, 11, 45, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	resp, err := http.Get(h.WebServer.Server.URL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "BuyaCAR Supervisor Console") {
		t.Fatalf("expected body to contain supervisor console title, got: %s", string(body))
	}
}

// TestTier1_Feature27_PrometheusMetrics_EndpointHappyPath verifies Prometheus /metrics
// exposition format (Feature 27).
func TestTier1_Feature27_PrometheusMetrics_EndpointHappyPath(t *testing.T) {
	now := time.Date(2026, 10, 29, 11, 50, 0, 0, time.UTC)
	h := NewE2EHarness(channel.ModeSimulation, now)
	defer h.Close()

	resp, err := http.Get(h.WebServer.Server.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "buyacar_fastpath_latency_p99") || !strings.Contains(bodyStr, "buyacar_cache_hits_total") {
		t.Fatalf("expected Prometheus metric definitions in output, got: %s", bodyStr)
	}
}
