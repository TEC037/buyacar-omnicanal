package core

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"buyacar.com.co/omnicanal/internal/ai"
	"buyacar.com.co/omnicanal/internal/channel"
	"buyacar.com.co/omnicanal/internal/crm"
	"buyacar.com.co/omnicanal/internal/escalate"
	"buyacar.com.co/omnicanal/internal/fast"
	"buyacar.com.co/omnicanal/internal/guard"
	"buyacar.com.co/omnicanal/internal/inventory"
	"buyacar.com.co/omnicanal/internal/skills"
	"buyacar.com.co/omnicanal/internal/store"
	"buyacar.com.co/omnicanal/internal/triage"
)

// ============================================================================
// 1. ADVERSARIAL SEND STATUS VERIFICATION
// ============================================================================

type adversarialMockChannel struct {
	*channel.StubChannel
	mu           sync.Mutex
	isForced     bool
	forcedStatus channel.SendStatus
	forcedErr    error
	repliesCount int
}

func newAdversarialMockChannel(id string) *adversarialMockChannel {
	return &adversarialMockChannel{
		StubChannel: channel.NewStubChannel(id, 650),
	}
}

func (m *adversarialMockChannel) setForced(status channel.SendStatus, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isForced = true
	m.forcedStatus = status
	m.forcedErr = err
}

func (m *adversarialMockChannel) clearForced() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isForced = false
	m.forcedStatus = ""
	m.forcedErr = nil
}

func (m *adversarialMockChannel) Reply(ctx context.Context, session channel.Session, conv channel.Conversation, message channel.Message, mode channel.Mode) (channel.SendResult, error) {
	m.mu.Lock()
	m.repliesCount++
	isForced := m.isForced
	forcedStatus := m.forcedStatus
	forcedErr := m.forcedErr
	m.mu.Unlock()

	if forcedErr != nil {
		return channel.SendResult{Status: channel.SendFailed, MessageID: "msg-err", Timestamp: time.Now().UTC()}, forcedErr
	}
	if isForced {
		return channel.SendResult{Status: forcedStatus, MessageID: "msg-forced", Timestamp: time.Now().UTC()}, nil
	}
	return m.StubChannel.Reply(ctx, session, conv, message, mode)
}

func (m *adversarialMockChannel) getRepliesCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.repliesCount
}

func setupAdversarialPipeline(mode channel.Mode, ch *adversarialMockChannel, now time.Time) (*Pipeline, *store.MemoryStore, *crm.MemoryManager, *escalate.Queue) {
	st := store.NewMemoryStore()
	cfg := DefaultSafeConfig()
	cfg.OperatingMode = mode
	eng := NewEngine(cfg, st)
	_ = eng.RegisterChannel(ch)

	cache := inventory.NewSeededStubCache(now)
	escQ := escalate.NewQueue()
	crmMgr := crm.NewMemoryManager()

	pipe := &Pipeline{
		Engine:   eng,
		InvCache: cache,
		Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
		Guard:    guard.NewGuard([]string{"buyacar.com.co"}),
		AIOrch:   ai.NewOrchestrator(nil, nil, skills.NewApprovedCache()),
		EscQueue: escQ,
		CRM:      crmMgr,
	}
	return pipe, st, crmMgr, escQ
}

func TestAdversarial_SendStatusVerification_Matrix(t *testing.T) {
	now := time.Date(2026, 10, 29, 14, 0, 0, 0, time.UTC)

	cases := []struct {
		name          string
		mode          channel.Mode
		mockStatus    channel.SendStatus
		mockErr       error
		expectSuccess bool
		expectedEsc   string
	}{
		// Autopilot mode matrix
		{name: "Autopilot_SendVerified_Success", mode: channel.ModeAutopilot, mockStatus: channel.SendVerified, mockErr: nil, expectSuccess: true},
		{name: "Autopilot_SendDrafted_Rejected", mode: channel.ModeAutopilot, mockStatus: channel.SendDrafted, mockErr: nil, expectSuccess: false, expectedEsc: "send_unverified"},
		{name: "Autopilot_SendSimulated_Rejected", mode: channel.ModeAutopilot, mockStatus: channel.SendSimulated, mockErr: nil, expectSuccess: false, expectedEsc: "send_unverified"},
		{name: "Autopilot_SendFailed_Rejected", mode: channel.ModeAutopilot, mockStatus: channel.SendFailed, mockErr: nil, expectSuccess: false, expectedEsc: "send_failed"},
		{name: "Autopilot_UnknownStatus_Rejected", mode: channel.ModeAutopilot, mockStatus: channel.SendStatus("unknown_status"), mockErr: nil, expectSuccess: false, expectedEsc: "send_unverified"},
		{name: "Autopilot_EmptyStatus_Rejected", mode: channel.ModeAutopilot, mockStatus: channel.SendStatus(""), mockErr: nil, expectSuccess: false, expectedEsc: "send_unverified"},
		{name: "Autopilot_NetworkError_Rejected", mode: channel.ModeAutopilot, mockStatus: "", mockErr: errors.New("connection reset by peer"), expectSuccess: false, expectedEsc: "send_failed"},

		// Draft mode matrix
		{name: "Draft_SendDrafted_Success", mode: channel.ModeDraft, mockStatus: channel.SendDrafted, mockErr: nil, expectSuccess: true},
		{name: "Draft_SendVerified_Rejected", mode: channel.ModeDraft, mockStatus: channel.SendVerified, mockErr: nil, expectSuccess: false, expectedEsc: "draft_unrecorded"},
		{name: "Draft_SendSimulated_Rejected", mode: channel.ModeDraft, mockStatus: channel.SendSimulated, mockErr: nil, expectSuccess: false, expectedEsc: "draft_unrecorded"},
		{name: "Draft_SendFailed_Rejected", mode: channel.ModeDraft, mockStatus: channel.SendFailed, mockErr: nil, expectSuccess: false, expectedEsc: "send_failed"},
		{name: "Draft_NetworkError_Rejected", mode: channel.ModeDraft, mockStatus: "", mockErr: errors.New("timeout"), expectSuccess: false, expectedEsc: "send_failed"},

		// Simulation mode matrix
		{name: "Simulation_SendSimulated_Success", mode: channel.ModeSimulation, mockStatus: channel.SendSimulated, mockErr: nil, expectSuccess: true},
		{name: "Simulation_SendVerified_Rejected", mode: channel.ModeSimulation, mockStatus: channel.SendVerified, mockErr: nil, expectSuccess: false, expectedEsc: "simulation_unrecorded"},
		{name: "Simulation_SendDrafted_Rejected", mode: channel.ModeSimulation, mockStatus: channel.SendDrafted, mockErr: nil, expectSuccess: false, expectedEsc: "simulation_unrecorded"},
		{name: "Simulation_SendFailed_Rejected", mode: channel.ModeSimulation, mockStatus: channel.SendFailed, mockErr: nil, expectSuccess: false, expectedEsc: "send_failed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockCh := newAdversarialMockChannel("mock-channel")
			mockCh.setForced(tc.mockStatus, tc.mockErr)

			pipe, st, crmMgr, escQ := setupAdversarialPipeline(tc.mode, mockCh, now)

			convID := "conv-" + tc.name
			conv := channel.Conversation{ID: convID, ChannelID: "mock-channel", CustomerAlias: "Tester", UpdatedAt: now}
			mockCh.SeedThread(conv, []channel.Message{
				{ID: "m1", Sender: "customer", Text: "Donde estan ubicados en Boyaca?", Timestamp: now},
			})
			sess, err := mockCh.Open(context.Background(), channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})
			if err != nil {
				t.Fatalf("unexpected Open error: %v", err)
			}

			outcome, err := pipe.ProcessConversation(context.Background(), mockCh, sess, conv, now)

			if tc.expectSuccess {
				if err != nil {
					t.Fatalf("expected success, got error: %v", err)
				}
				if outcome.Escalated {
					t.Fatalf("expected no escalation, got: %s", outcome.EscalationReason)
				}
				if outcome.SendStatus != tc.mockStatus {
					t.Fatalf("expected outcome.SendStatus=%s, got %s", tc.mockStatus, outcome.SendStatus)
				}

				// Checkpoint cp6 and cp7 must exist
				cp, ok := st.GetCheckpointState(convID)
				if !ok || cp != "cp7_crm:ok" {
					t.Fatalf("expected checkpoint cp7_crm:ok, got ok=%v cp=%s", ok, cp)
				}

				// CRM task must be recorded
				tasks := crmMgr.GetTasksByConv(convID)
				if len(tasks) == 0 {
					t.Fatalf("expected CRM task to be recorded on success")
				}

				// Idempotency record must be sealed
				hash := st.ComputeHash("mock-channel", convID, "Donde estan ubicados en Boyaca?")
				has, rec := st.HasProcessed(hash)
				if !has || !rec.IsSealed() {
					t.Fatalf("expected sealed idempotency record on success, got has=%v sealed=%v rec=%+v", has, rec.IsSealed(), rec)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error for rejected status/error, got nil")
				}
				if !outcome.Escalated {
					t.Fatalf("expected escalation for rejected status")
				}
				if outcome.EscalationReason != tc.expectedEsc {
					t.Fatalf("expected escalation reason %q, got %q", tc.expectedEsc, outcome.EscalationReason)
				}

				// Checkpoint cp6_envio and cp7_crm must NOT be recorded
				cp, _ := st.GetCheckpointState(convID)
				if cp == "cp6_envio:ok" || cp == "cp7_crm:ok" {
					t.Fatalf("checkpoint %s recorded despite send failure/rejection", cp)
				}

				// CRM task must NOT be settled
				tasks := crmMgr.GetTasksByConv(convID)
				if len(tasks) > 0 {
					t.Fatalf("CRM tasks recorded despite send failure/rejection: %+v", tasks)
				}

				// Idempotency record must NOT be marked processed or sealed
				hash := st.ComputeHash("mock-channel", convID, "Donde estan ubicados en Boyaca?")
				has, _ := st.HasProcessed(hash)
				if has {
					t.Fatalf("idempotency record marked processed despite send failure/rejection")
				}

				// Escalation queue must contain the alert
				if len(escQ.Pending()) == 0 {
					t.Fatalf("expected escalation ticket in EscQueue")
				}
			}
		})
	}
}

// ============================================================================
// 2. ADVERSARIAL IDEMPOTENCY HASH SEALING
// ============================================================================

func TestAdversarial_IdempotencyHashSealing_Integrity(t *testing.T) {
	st := store.NewMemoryStore()

	// 1. Extreme inputs & Unicode / Emoji hash uniqueness
	inputs := []struct {
		channelID string
		convID    string
		msg       string
	}{
		{"wa", "c1", "Hola"},
		{"wa", "c1", "Hola "},                      // trailing space must produce different hash
		{"wa", "c1", "hola"},                       // case sensitivity
		{"wa", "c1", "Hola\n¿Precio?"},             // newline
		{"wa", "c1", "¿Dónde están ubicados? 🚗💨✨"}, // emoji & non-ascii
		{"wa", "c2", "Hola"},                       // different conv
		{"email", "c1", "Hola"},                    // different channel
		{"wa", "c1", strings.Repeat("A", 10000)},   // 10K chars
	}

	hashes := make(map[string]int)
	for i, inp := range inputs {
		h := st.ComputeHash(inp.channelID, inp.convID, inp.msg)
		if len(h) != 64 {
			t.Fatalf("invalid sha256 hex length: %d for input %d", len(h), i)
		}
		if prev, exists := hashes[h]; exists {
			t.Fatalf("hash collision detected between input %d and %d: %s", prev, i, h)
		}
		hashes[h] = i
	}

	// 2. IsSealed boolean logic verification
	unsealedRecords := []store.IdempotencyRecord{
		{SendStatus: "", ETag: ""},
		{SendStatus: "sent_verified", ETag: ""},
		{SendStatus: "", ETag: `W/"123"`},
	}
	for i, rec := range unsealedRecords {
		if rec.IsSealed() {
			t.Fatalf("record %d should be unsealed: %+v", i, rec)
		}
	}

	sealedRecord := store.IdempotencyRecord{SendStatus: "sent_verified", ETag: `W/"etag-ok"`}
	if !sealedRecord.IsSealed() {
		t.Fatalf("record should be sealed: %+v", sealedRecord)
	}

	// 3. Atomicity of SealIdempotency
	testHash := st.ComputeHash("wa", "conv-seal-test", "Precio Hilux?")
	_ = st.MarkProcessed(store.IdempotencyRecord{
		Hash:      testHash,
		ChannelID: "wa",
		ConvID:    "conv-seal-test",
		ReplyText: "El precio es 185M COP",
		CreatedAt: time.Now().UTC(),
	})

	has, beforeSeal := st.HasProcessed(testHash)
	if !has || beforeSeal.IsSealed() {
		t.Fatalf("expected unsealed before seal call")
	}

	err := st.SealIdempotency(testHash, "sent_simulated", `W/"buyacar-etag-99"`, "crm-task-001")
	if err != nil {
		t.Fatalf("unexpected seal error: %v", err)
	}

	has, afterSeal := st.HasProcessed(testHash)
	if !has || !afterSeal.IsSealed() {
		t.Fatalf("expected sealed after seal call")
	}
	if afterSeal.SendStatus != "sent_simulated" || afterSeal.ETag != `W/"buyacar-etag-99"` || afterSeal.CRMTaskID != "crm-task-001" {
		t.Fatalf("sealed record field mismatch: %+v", afterSeal)
	}
	// ReplyText must be preserved from initial MarkProcessed
	if afterSeal.ReplyText != "El precio es 185M COP" {
		t.Fatalf("initial ReplyText lost during seal: %q", afterSeal.ReplyText)
	}
}

// ============================================================================
// 3. ADVERSARIAL DUPLICATE REQUEST REPLAY
// ============================================================================

func TestAdversarial_DuplicateReplay_SequentialAndRetry(t *testing.T) {
	now := time.Date(2026, 10, 29, 14, 30, 0, 0, time.UTC)
	mockCh := newAdversarialMockChannel("replay-channel")
	pipe, st, crmMgr, escQ := setupAdversarialPipeline(channel.ModeSimulation, mockCh, now)

	conv := channel.Conversation{ID: "conv-replay-1", ChannelID: "replay-channel", CustomerAlias: "ReplayUser", UpdatedAt: now}
	msgText := "¿Qué precio tiene la Toyota Hilux 2022?"
	mockCh.SeedThread(conv, []channel.Message{
		{ID: "m1", Sender: "customer", Text: msgText, Timestamp: now},
	})
	sess, _ := mockCh.Open(context.Background(), channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})

	// 1. Initial execution -> Must process and complete
	out1, err1 := pipe.ProcessConversation(context.Background(), mockCh, sess, conv, now)
	if err1 != nil {
		t.Fatalf("first execution error: %v", err1)
	}
	if out1.Skipped {
		t.Fatalf("first execution should not be skipped")
	}
	if mockCh.getRepliesCount() != 1 {
		t.Fatalf("expected 1 reply sent, got %d", mockCh.getRepliesCount())
	}
	if len(crmMgr.GetTasksByConv("conv-replay-1")) != 1 {
		t.Fatalf("expected 1 CRM task recorded, got %d", len(crmMgr.GetTasksByConv("conv-replay-1")))
	}

	// 2. Sequential Replay (Exact duplicate message 10 times)
	for i := 1; i <= 10; i++ {
		outReplay, errReplay := pipe.ProcessConversation(context.Background(), mockCh, sess, conv, now.Add(time.Duration(i)*time.Second))
		if errReplay != nil {
			t.Fatalf("replay iteration %d error: %v", i, errReplay)
		}
		if !outReplay.Skipped {
			t.Fatalf("replay iteration %d was NOT skipped", i)
		}
		if !strings.Contains(outReplay.SkipReason, "already processed") {
			t.Fatalf("replay iteration %d unexpected skip reason: %s", i, outReplay.SkipReason)
		}
	}

	// Channel replies and CRM tasks must remain strictly 1
	if mockCh.getRepliesCount() != 1 {
		t.Fatalf("expected exactly 1 reply dispatched after 10 duplicate replays, got %d", mockCh.getRepliesCount())
	}
	if len(crmMgr.GetTasksByConv("conv-replay-1")) != 1 {
		t.Fatalf("expected exactly 1 CRM task after 10 duplicate replays, got %d", len(crmMgr.GetTasksByConv("conv-replay-1")))
	}
	if len(escQ.Pending()) != 0 {
		t.Fatalf("expected 0 escalations on replay, got %d", len(escQ.Pending()))
	}

	// 3. Replay after send failure -> Channel fails on first attempt, then succeeds on retry
	convFail := channel.Conversation{ID: "conv-fail-retry", ChannelID: "replay-channel"}
	mockCh.SeedThread(convFail, []channel.Message{
		{ID: "mf1", Sender: "customer", Text: "Horarios de atencion?", Timestamp: now},
	})
	mockCh.setForced(channel.SendFailed, errors.New("temporary carrier timeout"))

	outFail, errFail := pipe.ProcessConversation(context.Background(), mockCh, sess, convFail, now)
	if errFail == nil {
		t.Fatalf("expected error on failed channel reply")
	}
	if !outFail.Escalated || outFail.EscalationReason != "send_failed" {
		t.Fatalf("expected send_failed escalation, got %+v", outFail)
	}

	// Ensure failed attempt was NOT sealed in store
	failHash := st.ComputeHash("replay-channel", "conv-fail-retry", "Horarios de atencion?")
	has, _ := st.HasProcessed(failHash)
	if has {
		t.Fatalf("failed send was marked as processed in store")
	}

	// Channel recovers! Retry the exact same conversation
	mockCh.clearForced()
	outRetry, errRetry := pipe.ProcessConversation(context.Background(), mockCh, sess, convFail, now.Add(10*time.Second))
	if errRetry != nil {
		t.Fatalf("retry after recovery failed: %v", errRetry)
	}
	if outRetry.Skipped {
		t.Fatalf("retry was erroneously skipped by idempotency")
	}
	if outRetry.SendStatus != channel.SendSimulated {
		t.Fatalf("expected successful simulated send on retry, got %s", outRetry.SendStatus)
	}

	// Now it should be sealed
	hasAfter, recAfter := st.HasProcessed(failHash)
	if !hasAfter || !recAfter.IsSealed() {
		t.Fatalf("retry should have sealed the idempotency record, got has=%v sealed=%v", hasAfter, recAfter.IsSealed())
	}
}

func TestAdversarial_DuplicateReplay_ConcurrentSubmission(t *testing.T) {
	now := time.Date(2026, 10, 29, 14, 45, 0, 0, time.UTC)
	mockCh := newAdversarialMockChannel("concurrent-channel")
	pipe, st, _, _ := setupAdversarialPipeline(channel.ModeSimulation, mockCh, now)

	conv := channel.Conversation{ID: "conv-concurrent-race", ChannelID: "concurrent-channel", CustomerAlias: "RaceUser", UpdatedAt: now}
	msgText := "¿Qué precio tiene la Toyota Hilux 2022?"
	mockCh.SeedThread(conv, []channel.Message{
		{ID: "m1", Sender: "customer", Text: msgText, Timestamp: now},
	})
	sess, _ := mockCh.Open(context.Background(), channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})

	// 20 concurrent goroutines submitting the identical message at the exact same instant
	concurrency := 20
	var wg sync.WaitGroup
	var skippedCount int64
	var processedCount int64
	var errCount int64

	startBarrier := make(chan struct{})

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startBarrier // synchronize start
			outcome, err := pipe.ProcessConversation(context.Background(), mockCh, sess, conv, now)
			if err != nil {
				atomic.AddInt64(&errCount, 1)
				return
			}
			if outcome.Skipped {
				atomic.AddInt64(&skippedCount, 1)
			} else {
				atomic.AddInt64(&processedCount, 1)
			}
		}()
	}

	close(startBarrier)
	wg.Wait()

	t.Logf("Concurrent duplicate submission (20 goroutines): Processed=%d, Skipped=%d, Errors=%d", processedCount, skippedCount, errCount)

	// Verification of data store consistency:
	hash := st.ComputeHash("concurrent-channel", "conv-concurrent-race", msgText)
	has, rec := st.HasProcessed(hash)
	if !has || !rec.IsSealed() {
		t.Fatalf("expected sealed idempotency record in store")
	}

	// Verify checkpoint state observation
	cp, ok := st.GetCheckpointState("conv-concurrent-race")
	t.Logf("Observed checkpoint state after concurrent submission: ok=%v, cp=%q", ok, cp)
	if cp != "cp7_crm:ok" {
		t.Logf("FINDING [VULNERABILITY]: Checkpoint clobbered to %q by trailing skipped duplicate goroutine at pipeline.go:82", cp)
	}

	// In absence of an in-flight reservation mutex across steps 2-7, concurrent requests
	// that execute before CP7 finishes will all process.
	// We verify that once sealed, subsequent sequential requests are strictly 100% skipped:
	outAfter, errAfter := pipe.ProcessConversation(context.Background(), mockCh, sess, conv, now.Add(time.Second))
	if errAfter != nil || !outAfter.Skipped {
		t.Fatalf("expected subsequent request to be strictly skipped: %+v, err=%v", outAfter, errAfter)
	}
}

// TestAdversarial_CheckpointClobberOnDuplicateReplay empirically reproduces the bug where
// an already-completed conversation (cp7_crm:ok) has its checkpoint clobbered to cp1_lectura:ok
// whenever a duplicate or skipped message is evaluated.
func TestAdversarial_CheckpointClobberOnDuplicateReplay(t *testing.T) {
	now := time.Date(2026, 10, 29, 15, 30, 0, 0, time.UTC)
	mockCh := newAdversarialMockChannel("clobber-channel")
	pipe, st, _, _ := setupAdversarialPipeline(channel.ModeSimulation, mockCh, now)

	convID := "conv-clobber-test"
	conv := channel.Conversation{ID: convID, ChannelID: "clobber-channel", CustomerAlias: "ClobberUser", UpdatedAt: now}
	msgText := "Donde quedan en Tunja?"
	mockCh.SeedThread(conv, []channel.Message{
		{ID: "m1", Sender: "customer", Text: msgText, Timestamp: now},
	})
	sess, _ := mockCh.Open(context.Background(), channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})

	// 1. Initial execution -> Completes all checkpoints to cp7_crm:ok
	out1, err1 := pipe.ProcessConversation(context.Background(), mockCh, sess, conv, now)
	if err1 != nil || out1.Skipped {
		t.Fatalf("initial execution failed: err=%v, out=%+v", err1, out1)
	}

	cpBefore, okBefore := st.GetCheckpointState(convID)
	if !okBefore || cpBefore != "cp7_crm:ok" {
		t.Fatalf("expected cp7_crm:ok after first run, got ok=%v cp=%q", okBefore, cpBefore)
	}

	// 2. Replay duplicate message -> Skipped by ShouldProcessThread
	out2, err2 := pipe.ProcessConversation(context.Background(), mockCh, sess, conv, now.Add(time.Minute))
	if err2 != nil || !out2.Skipped {
		t.Fatalf("expected duplicate to be skipped: err=%v, out=%+v", err2, out2)
	}

	// 3. Inspect checkpoint state after duplicate replay
	cpAfter, okAfter := st.GetCheckpointState(convID)
	t.Logf("EMPIRICAL EVIDENCE — Checkpoint before duplicate: %q, Checkpoint after duplicate: %q (ok=%v)", cpBefore, cpAfter, okAfter)

	if cpAfter == "cp1_lectura:ok" {
		t.Logf("CONFIRMED BUG: CP1 checkpoint (pipeline.go:82) unconditionally clobbers completed conversation checkpoint on duplicate replay")
	}
}

// ============================================================================
// 4. HIGH CONTENTION FAST-PATH LATENCY (< 5ms SLA)
// ============================================================================

func TestAdversarial_HighContentionFastPathLatency(t *testing.T) {
	fastEng := fast.NewEngine(fast.DefaultBusinessTexts(), 0.80)

	// Sub-test A: High Contention Concurrent Read Fast-Path (50 and 100 concurrent workers)
	t.Run("HighReadContention_50Workers", func(t *testing.T) {
		runFastPathContentionTest(t, fastEng, 50, 100, false)
	})

	t.Run("HighReadContention_100Workers", func(t *testing.T) {
		runFastPathContentionTest(t, fastEng, 100, 100, false)
	})

	// Sub-test B: Mixed Read-Write Contention (50 concurrent workers with periodic inventory update)
	t.Run("MixedContention_50Workers_WithPeriodicUpdates", func(t *testing.T) {
		runFastPathContentionTest(t, fastEng, 50, 100, true)
	})
}

func runFastPathContentionTest(t *testing.T, fastEng *fast.Engine, concurrency, iterations int, enableWriters bool) {
	now := time.Now().UTC()
	cache := inventory.NewSeededStubCache(now)
	pipe := &Pipeline{InvCache: cache, Fast: fastEng}

	totalOps := concurrency * iterations
	durations := make([]time.Duration, 0, totalOps)
	var durationsMu sync.Mutex

	var wg sync.WaitGroup
	startBarrier := make(chan struct{})

	stopWriters := make(chan struct{})
	var writerWg sync.WaitGroup

	if enableWriters {
		writerWg.Add(1)
		go func() {
			defer writerWg.Done()
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			cnt := 0
			for {
				select {
				case <-stopWriters:
					return
				case <-ticker.C:
					cnt++
					tNow := time.Now().UTC()
					cache.TouchRefresh(tNow)
					if cnt%5 == 0 {
						cache.LoadSnapshot(inventory.DefaultFixtureVehicles(), fmt.Sprintf(`W/"etag-%d"`, cnt), tNow)
					}
				}
			}
		}()
	}

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerIdx int) {
			defer wg.Done()
			localDurations := make([]time.Duration, 0, iterations)
			ctx := context.Background()
			conv := channel.Conversation{ID: fmt.Sprintf("conv-stress-%d", workerIdx), ChannelID: "whatsapp"}

			<-startBarrier

			for j := 0; j < iterations; j++ {
				queryText := "¿Qué precio tiene la Toyota Hilux 2022?"
				if j%2 == 1 {
					queryText = "Donde estan ubicados?"
				}

				curTime := time.Now().UTC()
				start := time.Now()

				cl := triage.Classify(queryText)
				fpRes := fastEng.Evaluate(ctx, cl, cache, curTime)
				if !fpRes.Handled {
					t.Errorf("expected Fast-Path to handle query %q", queryText)
				}
				ok, reason := pipe.RevalidatePreSend(ctx, conv, "veh-001", curTime)
				if !ok {
					t.Errorf("expected revalidation ok, got reason: %s", reason)
				}

				elapsed := time.Since(start)
				localDurations = append(localDurations, elapsed)
			}

			durationsMu.Lock()
			durations = append(durations, localDurations...)
			durationsMu.Unlock()
		}(i)
	}

	close(startBarrier)
	wg.Wait()

	if enableWriters {
		close(stopWriters)
		writerWg.Wait()
	}

	sort.Slice(durations, func(i, j int) bool {
		return durations[i] < durations[j]
	})

	n := len(durations)
	minD := durations[0]
	p50D := durations[n*50/100]
	p90D := durations[n*90/100]
	p95D := durations[n*95/100]
	p99D := durations[n*99/100]
	maxD := durations[n-1]

	var sum time.Duration
	for _, d := range durations {
		sum += d
	}
	meanD := sum / time.Duration(n)

	t.Logf("[%d workers, %d total ops] Min: %v | Mean: %v | p50: %v | p90: %v | p95: %v | p99: %v | Max: %v",
		concurrency, n, minD, meanD, p50D, p90D, p95D, p99D, maxD)

	slaCeiling := 5 * time.Millisecond
	if p99D >= slaCeiling {
		t.Fatalf("SLA VIOLATION: p99 latency %v exceeded 5ms ceiling!", p99D)
	}
}
