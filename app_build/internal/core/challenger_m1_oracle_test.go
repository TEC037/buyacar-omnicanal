package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
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
)

// hookStore wraps store.Store to trigger dynamic callbacks when specific checkpoints are saved
type hookStore struct {
	store.Store
	onCheckpoint func(convID string, checkpoint string)
}

func (h *hookStore) SaveCheckpointState(convID string, checkpoint string) error {
	if h.onCheckpoint != nil {
		h.onCheckpoint(convID, checkpoint)
	}
	return h.Store.SaveCheckpointState(convID, checkpoint)
}

// ============================================================================
// CHALLENGE 1: Inventory Cache Freshness Boundary (29m59s vs 30m00s vs 30m01s)
// ============================================================================

func TestChallenger_CacheFreshnessBoundaries_29m59s_vs_30m01s(t *testing.T) {
	ctx := context.Background()
	t0 := time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC)

	// Subtest 1.1: Exact unit-level boundary checks on Cache & RevalidatePreSend
	t.Run("UnitBoundaryChecks", func(t *testing.T) {
		cache := inventory.NewCache(15, 30)
		vehicles := inventory.DefaultFixtureVehicles()
		cache.LoadSnapshot(vehicles, `W/"boundary-test"`, t0)
		pipe := &Pipeline{InvCache: cache}
		conv := channel.Conversation{ID: "c-boundary", ChannelID: "whatsapp"}

		// 1. Exactly 29 minutes and 59 seconds (diff = 29m59s <= 30m -> Fresh)
		t29m59s := t0.Add(29*time.Minute + 59*time.Second)
		if !cache.IsFresh(t29m59s) {
			t.Fatalf("expected cache.IsFresh to be true at 29m 59s")
		}
		okPre29, reasonPre29 := pipe.RevalidatePreSend(ctx, conv, "veh-001", t29m59s)
		if !okPre29 || reasonPre29 != "" {
			t.Fatalf("expected RevalidatePreSend to pass at 29m 59s, got ok=%v reason=%q", okPre29, reasonPre29)
		}

		// 2. Exactly 30 minutes 00 seconds (diff = 30m00s == 30m -> Fresh boundary inclusive)
		t30m00s := t0.Add(30 * time.Minute)
		if !cache.IsFresh(t30m00s) {
			t.Fatalf("expected cache.IsFresh to be true at exact 30m 00s boundary")
		}
		okPre30, reasonPre30 := pipe.RevalidatePreSend(ctx, conv, "veh-001", t30m00s)
		if !okPre30 || reasonPre30 != "" {
			t.Fatalf("expected RevalidatePreSend to pass at exact 30m 00s boundary, got ok=%v reason=%q", okPre30, reasonPre30)
		}

		// 3. Exactly 30 minutes 01 seconds (diff = 30m01s > 30m -> Stale)
		t30m01s := t0.Add(30*time.Minute + 1*time.Second)
		if cache.IsFresh(t30m01s) {
			t.Fatalf("expected cache.IsFresh to be false at 30m 01s")
		}
		okPre31, reasonPre31 := pipe.RevalidatePreSend(ctx, conv, "veh-001", t30m01s)
		if okPre31 || reasonPre31 != "inventory_stale" {
			t.Fatalf("expected RevalidatePreSend to fail with inventory_stale at 30m 01s, got ok=%v reason=%q", okPre31, reasonPre31)
		}
	})

	// Subtest 1.2: Full Pipeline Execution at 29m59s (Should Dispatch & Settle CRM)
	t.Run("Pipeline_At_29m59s_DispatchesSuccessfully", func(t *testing.T) {
		t29m59s := t0.Add(29*time.Minute + 59*time.Second)
		st := store.NewMemoryStore()
		eng := NewEngine(DefaultSafeConfig(), st)
		crmMgr := crm.NewMemoryManager()
		escQ := escalate.NewQueue()

		cache := inventory.NewCache(15, 30)
		cache.LoadSnapshot(inventory.DefaultFixtureVehicles(), `W/"boundary-etag"`, t0)

		waStub := channel.NewStubChannel("whatsapp", 650)
		_ = eng.RegisterChannel(waStub)

		conv := channel.Conversation{ID: "conv-fresh-29m59s", ChannelID: "whatsapp"}
		waStub.SeedThread(conv, []channel.Message{
			{ID: "m1", Sender: "customer", Text: "¿Qué precio tiene la Toyota Hilux?", Timestamp: t29m59s},
		})
		sess, _ := waStub.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})

		pipe := &Pipeline{
			Engine:   eng,
			InvCache: cache,
			Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
			Guard:    guard.NewGuard([]string{"buyacar.com.co"}),
			AIOrch:   ai.NewOrchestrator(nil, nil, skills.NewApprovedCache()),
			EscQueue: escQ,
			CRM:      crmMgr,
		}

		out, err := pipe.ProcessConversation(ctx, waStub, sess, conv, t29m59s)
		if err != nil {
			t.Fatalf("unexpected pipeline error: %v", err)
		}
		if out.Escalated {
			t.Fatalf("expected no escalation at 29m 59s, got escalated=true reason=%s", out.EscalationReason)
		}
		if out.CheckpointsOK != 7 {
			t.Fatalf("expected all 7 checkpoints completed at 29m 59s, got %d", out.CheckpointsOK)
		}
		if !strings.Contains(out.SentReply, "185000000") {
			t.Fatalf("expected commercial reply sent, got %q", out.SentReply)
		}

		// CRM task settled and idempotency sealed
		tasks := crmMgr.GetTasksByConv("conv-fresh-29m59s")
		if len(tasks) != 1 {
			t.Fatalf("expected 1 CRM task settled, got %d", len(tasks))
		}
		has, rec := st.HasProcessed(st.ComputeHash("whatsapp", "conv-fresh-29m59s", "¿Qué precio tiene la Toyota Hilux?"))
		if !has || !rec.IsSealed() {
			t.Fatalf("expected sealed idempotency record at 29m 59s")
		}
	})

	// Subtest 1.3: Full Pipeline Execution at 30m01s (Caught early by Fast-Path CP3 or Pre-Send Revalidator)
	t.Run("Pipeline_At_30m01s_HaltsImmediatelyAndEscalates", func(t *testing.T) {
		t30m01s := t0.Add(30*time.Minute + 1*time.Second)
		st := store.NewMemoryStore()
		eng := NewEngine(DefaultSafeConfig(), st)
		crmMgr := crm.NewMemoryManager()
		escQ := escalate.NewQueue()

		cache := inventory.NewCache(15, 30)
		cache.LoadSnapshot(inventory.DefaultFixtureVehicles(), `W/"boundary-etag"`, t0)

		waStub := channel.NewStubChannel("whatsapp", 650)
		_ = eng.RegisterChannel(waStub)

		conv := channel.Conversation{ID: "conv-stale-30m01s", ChannelID: "whatsapp"}
		waStub.SeedThread(conv, []channel.Message{
			{ID: "m1", Sender: "customer", Text: "¿Qué precio tiene la Toyota Hilux?", Timestamp: t30m01s},
		})
		sess, _ := waStub.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})

		pipe := &Pipeline{
			Engine:   eng,
			InvCache: cache,
			Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
			Guard:    guard.NewGuard([]string{"buyacar.com.co"}),
			AIOrch:   ai.NewOrchestrator(nil, nil, skills.NewApprovedCache()),
			EscQueue: escQ,
			CRM:      crmMgr,
		}

		out, err := pipe.ProcessConversation(ctx, waStub, sess, conv, t30m01s)
		if err != nil {
			t.Fatalf("unexpected pipeline error: %v", err)
		}
		if !out.Escalated || out.EscalationReason != "inventory_stale" {
			t.Fatalf("expected escalation with inventory_stale at 30m 01s, got escalated=%v reason=%s", out.Escalated, out.EscalationReason)
		}
		// Fast-Path detects stale cache at CP3 and halts before any AI or dispatch
		if out.CheckpointsOK != 3 {
			t.Fatalf("expected pipeline to halt at CP3 on stale inventory, got CheckpointsOK=%d", out.CheckpointsOK)
		}
		if out.SentReply != "" {
			t.Fatalf("expected no reply sent to customer on stale cache, got %q", out.SentReply)
		}

		// Verify ticket enqueued in human queue
		pending := escQ.Pending()
		if len(pending) != 1 || pending[0].Reason != "inventory_stale" {
			t.Fatalf("expected human escalation ticket with reason inventory_stale, got %+v", pending)
		}
	})

	// Subtest 1.4: Cache goes stale mid-flight (at CP5) before Pre-Send Revalidation
	t.Run("Pipeline_StaleThreshold_TriggeredAtPreSendRevalidation", func(t *testing.T) {
		baseStore := store.NewMemoryStore()
		cache := inventory.NewCache(15, 30)
		cache.LoadSnapshot(inventory.DefaultFixtureVehicles(), `W/"pre-send-etag"`, t0)

		// Hook store: when cp5_guard:ok is recorded, advance cache timestamp or set unavailable
		hStore := &hookStore{
			Store: baseStore,
			onCheckpoint: func(convID string, cp string) {
				if cp == "cp5_guard:ok" {
					// Cache becomes unavailable or stale right at pre-send gate
					cache.SetUnavailable()
				}
			},
		}

		eng := NewEngine(DefaultSafeConfig(), hStore)
		crmMgr := crm.NewMemoryManager()
		escQ := escalate.NewQueue()

		waStub := channel.NewStubChannel("whatsapp", 650)
		_ = eng.RegisterChannel(waStub)

		conv := channel.Conversation{ID: "conv-presend-stale", ChannelID: "whatsapp"}
		waStub.SeedThread(conv, []channel.Message{
			{ID: "m1", Sender: "customer", Text: "¿Qué precio tiene la Toyota Hilux?", Timestamp: t0.Add(5 * time.Minute)},
		})
		sess, _ := waStub.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})

		pipe := &Pipeline{
			Engine:   eng,
			InvCache: cache,
			Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
			Guard:    guard.NewGuard([]string{"buyacar.com.co"}),
			AIOrch:   ai.NewOrchestrator(nil, nil, skills.NewApprovedCache()),
			EscQueue: escQ,
			CRM:      crmMgr,
		}

		out, err := pipe.ProcessConversation(ctx, waStub, sess, conv, t0.Add(5*time.Minute))
		if err != nil {
			t.Fatalf("unexpected pipeline error: %v", err)
		}
		if !out.Escalated || out.EscalationReason != "inventory_unavailable" {
			t.Fatalf("expected pre-send revalidator to halt with inventory_unavailable, got %+v", out)
		}
		if out.CheckpointsOK != 5 {
			t.Fatalf("expected pipeline to halt strictly at CP5 without reaching CP6/CP7, got %d", out.CheckpointsOK)
		}
		if out.SentReply != "" {
			t.Fatalf("expected zero dispatch to customer, got %q", out.SentReply)
		}
	})
}

// ============================================================================
// CHALLENGE 2: Vehicle Marked Sold / Reserved WHILE Pipeline Is Executing
// ============================================================================

func TestChallenger_VehicleSoldTransition_DuringPipelineExecution(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)

	// Subtest 2.1: Vehicle transitions to "sold" right between Guard (CP5) and Pre-Send Revalidation
	t.Run("TransitionToSold_MidExecution_SuppressesPitchAndDispatchesNotice", func(t *testing.T) {
		baseStore := store.NewMemoryStore()
		cache := inventory.NewCache(15, 30)
		cache.LoadSnapshot(inventory.DefaultFixtureVehicles(), `W/"initial-etag"`, now)

		// Hook store: when cp5_guard:ok is saved, dynamically mutate veh-001 status to "sold"
		hStore := &hookStore{
			Store: baseStore,
			onCheckpoint: func(convID string, cp string) {
				if cp == "cp5_guard:ok" {
					// Simulate concurrent stock management update marking Hilux as sold
					updatedVehicles := inventory.DefaultFixtureVehicles()
					for i := range updatedVehicles {
						if updatedVehicles[i].ID == "veh-001" {
							updatedVehicles[i].Status = "sold"
						}
					}
					cache.LoadSnapshot(updatedVehicles, `W/"updated-sold-etag"`, now)
				}
			},
		}

		eng := NewEngine(DefaultSafeConfig(), hStore)
		crmMgr := crm.NewMemoryManager()
		escQ := escalate.NewQueue()

		waStub := channel.NewStubChannel("whatsapp", 650)
		_ = eng.RegisterChannel(waStub)

		conv := channel.Conversation{ID: "conv-mid-sold", ChannelID: "whatsapp"}
		waStub.SeedThread(conv, []channel.Message{
			{ID: "m1", Sender: "customer", Text: "¿Qué precio tiene la Toyota Hilux?", Timestamp: now},
		})
		sess, _ := waStub.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})

		pipe := &Pipeline{
			Engine:   eng,
			InvCache: cache,
			Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
			Guard:    guard.NewGuard([]string{"buyacar.com.co"}),
			AIOrch:   ai.NewOrchestrator(nil, nil, skills.NewApprovedCache()),
			EscQueue: escQ,
			CRM:      crmMgr,
		}

		out, err := pipe.ProcessConversation(ctx, waStub, sess, conv, now)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Must detect the sold transition at pre-send
		if !out.Escalated || out.EscalationReason != "vehicle_sold_or_reserved" {
			t.Fatalf("expected escalation with reason vehicle_sold_or_reserved, got escalated=%v reason=%s", out.Escalated, out.EscalationReason)
		}

		// Commercial sales pitch MUST be suppressed; SoldVehicleNotice dispatched instead
		if strings.Contains(out.SentReply, "185000000") {
			t.Fatalf("CRITICAL BUG: Stale sales pitch was dispatched despite mid-flight sold transition!")
		}
		if !strings.Contains(out.SentReply, "vendido/separado") {
			t.Fatalf("expected polite SoldVehicleNotice dispatched to customer, got %q", out.SentReply)
		}

		// Escalation ticket in human queue
		pending := escQ.Pending()
		if len(pending) == 0 {
			t.Fatalf("expected human escalation ticket enqueued")
		}

		// CRM task must be marked as escalated
		tasks := crmMgr.GetTasksByConv("conv-mid-sold")
		if len(tasks) != 1 || tasks[0].Status != crm.TaskStatusEscalated {
			t.Fatalf("expected CRM task with status escalated, got %+v", tasks)
		}
	})

	// Subtest 2.2: Vehicle transitions to "reserved" mid-execution
	t.Run("TransitionToReserved_MidExecution_SuppressesPitch", func(t *testing.T) {
		baseStore := store.NewMemoryStore()
		cache := inventory.NewCache(15, 30)
		cache.LoadSnapshot(inventory.DefaultFixtureVehicles(), `W/"initial-etag"`, now)

		hStore := &hookStore{
			Store: baseStore,
			onCheckpoint: func(convID string, cp string) {
				if cp == "cp5_guard:ok" {
					updatedVehicles := inventory.DefaultFixtureVehicles()
					for i := range updatedVehicles {
						if updatedVehicles[i].ID == "veh-001" {
							updatedVehicles[i].Status = "reserved"
						}
					}
					cache.LoadSnapshot(updatedVehicles, `W/"updated-reserved-etag"`, now)
				}
			},
		}

		eng := NewEngine(DefaultSafeConfig(), hStore)
		crmMgr := crm.NewMemoryManager()
		escQ := escalate.NewQueue()

		waStub := channel.NewStubChannel("whatsapp", 650)
		_ = eng.RegisterChannel(waStub)

		conv := channel.Conversation{ID: "conv-mid-reserved", ChannelID: "whatsapp"}
		waStub.SeedThread(conv, []channel.Message{
			{ID: "m1", Sender: "customer", Text: "¿Qué precio tiene la Toyota Hilux?", Timestamp: now},
		})
		sess, _ := waStub.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})

		pipe := &Pipeline{
			Engine:   eng,
			InvCache: cache,
			Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
			Guard:    guard.NewGuard([]string{"buyacar.com.co"}),
			AIOrch:   ai.NewOrchestrator(nil, nil, skills.NewApprovedCache()),
			EscQueue: escQ,
			CRM:      crmMgr,
		}

		out, err := pipe.ProcessConversation(ctx, waStub, sess, conv, now)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !out.Escalated || out.EscalationReason != "vehicle_sold_or_reserved" {
			t.Fatalf("expected escalation for reserved vehicle, got %+v", out)
		}
		if strings.Contains(out.SentReply, "185000000") {
			t.Fatalf("CRITICAL BUG: Price quote sent for reserved vehicle!")
		}
	})

	// Subtest 2.3: Vehicle deleted from catalog entirely mid-execution
	t.Run("VehicleDeleted_MidExecution_AbortsDispatch", func(t *testing.T) {
		baseStore := store.NewMemoryStore()
		cache := inventory.NewCache(15, 30)
		cache.LoadSnapshot(inventory.DefaultFixtureVehicles(), `W/"initial-etag"`, now)

		hStore := &hookStore{
			Store: baseStore,
			onCheckpoint: func(convID string, cp string) {
				if cp == "cp5_guard:ok" {
					// Catalog reloaded without veh-001
					var remaining []inventory.Vehicle
					for _, v := range inventory.DefaultFixtureVehicles() {
						if v.ID != "veh-001" {
							remaining = append(remaining, v)
						}
					}
					cache.LoadSnapshot(remaining, `W/"deleted-etag"`, now)
				}
			},
		}

		eng := NewEngine(DefaultSafeConfig(), hStore)
		crmMgr := crm.NewMemoryManager()
		escQ := escalate.NewQueue()

		waStub := channel.NewStubChannel("whatsapp", 650)
		_ = eng.RegisterChannel(waStub)

		conv := channel.Conversation{ID: "conv-mid-deleted", ChannelID: "whatsapp"}
		waStub.SeedThread(conv, []channel.Message{
			{ID: "m1", Sender: "customer", Text: "¿Qué precio tiene la Toyota Hilux?", Timestamp: now},
		})
		sess, _ := waStub.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})

		pipe := &Pipeline{
			Engine:   eng,
			InvCache: cache,
			Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
			Guard:    guard.NewGuard([]string{"buyacar.com.co"}),
			AIOrch:   ai.NewOrchestrator(nil, nil, skills.NewApprovedCache()),
			EscQueue: escQ,
			CRM:      crmMgr,
		}

		out, err := pipe.ProcessConversation(ctx, waStub, sess, conv, now)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !out.Escalated || out.EscalationReason != "vehicle_not_found" {
			t.Fatalf("expected escalation with vehicle_not_found, got escalated=%v reason=%s", out.Escalated, out.EscalationReason)
		}
		if out.CheckpointsOK != 5 {
			t.Fatalf("expected pipeline to stop at CP5, got %d", out.CheckpointsOK)
		}
		if out.SentReply != "" {
			t.Fatalf("expected no reply sent when vehicle not found, got %q", out.SentReply)
		}
	})
}

// ============================================================================
// CHALLENGE 3: Concurrency Stress — 100 Concurrent Pipelines Accessing Store & CRM
// ============================================================================

func TestChallenger_ConcurrencyStress_100Pipelines_SharedStoreAndCRM(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)

	st := store.NewMemoryStore()
	eng := NewEngine(DefaultSafeConfig(), st)
	crmMgr := crm.NewMemoryManager()
	escQ := escalate.NewQueue()
	cache := inventory.NewSeededStubCache(now)

	// Register 3 omnichannel stubs
	channels := []*channel.StubChannel{
		channel.NewStubChannel("whatsapp", 650),
		channel.NewStubChannel("mercadolibre", 1000),
		channel.NewStubChannel("web", 2000),
	}
	for _, ch := range channels {
		_ = eng.RegisterChannel(ch)
	}

	pipe := &Pipeline{
		Engine:   eng,
		InvCache: cache,
		Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
		Guard:    guard.NewGuard([]string{"buyacar.com.co"}),
		AIOrch:   ai.NewOrchestrator(nil, nil, skills.NewApprovedCache()),
		EscQueue: escQ,
		CRM:      crmMgr,
	}

	const totalConns = 100
	var wg sync.WaitGroup
	errCh := make(chan error, totalConns)
	outcomes := make([]PipelineOutcome, totalConns)

	// Pre-seed 100 distinct threads across the 3 channels with diverse Fast-Path intents
	for i := 0; i < totalConns; i++ {
		ch := channels[i%len(channels)]
		convID := fmt.Sprintf("conv-stress-%03d", i)
		conv := channel.Conversation{
			ID:            convID,
			ChannelID:     ch.ID(),
			CustomerAlias: fmt.Sprintf("Lead_%03d", i),
			UpdatedAt:     now,
		}
		msgText := "¿Qué precio tiene la Toyota Hilux 2022?"
		if i%4 == 1 {
			msgText = "¿Dónde están ubicados?"
		} else if i%4 == 2 {
			msgText = "¿Cuál es su horario de atención?"
		} else if i%4 == 3 {
			msgText = "¿Qué kilometraje tiene la Toyota Hilux?"
		}
		ch.SeedThread(conv, []channel.Message{
			{ID: fmt.Sprintf("m-%03d", i), Sender: "customer", Text: msgText, Timestamp: now},
		})
	}

	// Launch 100 concurrent workers
	wg.Add(totalConns)
	for i := 0; i < totalConns; i++ {
		idx := i
		go func() {
			defer wg.Done()
			ch := channels[idx%len(channels)]
			convID := fmt.Sprintf("conv-stress-%03d", idx)
			conv := channel.Conversation{ID: convID, ChannelID: ch.ID()}
			sess, err := ch.Open(ctx, channel.Link{ID: fmt.Sprintf("l-%s", ch.ID()), URL: "https://buyacar.com.co", Active: true})
			if err != nil {
				errCh <- fmt.Errorf("worker %d: open session failed: %w", idx, err)
				return
			}
			out, pErr := pipe.ProcessConversation(ctx, ch, sess, conv, now)
			if pErr != nil {
				errCh <- fmt.Errorf("worker %d: process conversation failed: %w", idx, pErr)
				return
			}
			outcomes[idx] = out
		}()
	}

	wg.Wait()
	close(errCh)

	// Verify zero worker errors
	for err := range errCh {
		t.Fatalf("concurrency stress failed with error: %v", err)
	}

	// Verify all 100 pipeline outcomes completed all 7 checkpoints
	for i, out := range outcomes {
		if out.CheckpointsOK != 7 {
			t.Fatalf("worker %d: expected 7 checkpoints, got %d", i, out.CheckpointsOK)
		}
		if out.CRMTaskID == "" {
			t.Fatalf("worker %d: expected CRMTaskID settled, got empty", i)
		}
		if out.SendStatus != channel.SendSimulated {
			t.Fatalf("worker %d: expected SendSimulated, got %s", i, out.SendStatus)
		}
	}

	// Verify CRM Manager thread-safe state consistency
	allTasks := crmMgr.ListTasks(ctx)
	if len(allTasks) != totalConns {
		t.Fatalf("expected strictly %d tasks in CRM, got %d", totalConns, len(allTasks))
	}
	for i := 0; i < totalConns; i++ {
		convID := fmt.Sprintf("conv-stress-%03d", i)
		tasks := crmMgr.GetTasksByConv(convID)
		if len(tasks) != 1 {
			t.Fatalf("expected 1 task for %s, got %d", convID, len(tasks))
		}
	}

	// Verify Store thread-safe state consistency & idempotency sealing
	for i := 0; i < totalConns; i++ {
		ch := channels[i%len(channels)]
		convID := fmt.Sprintf("conv-stress-%03d", i)
		msgText := "¿Qué precio tiene la Toyota Hilux 2022?"
		if i%4 == 1 {
			msgText = "¿Dónde están ubicados?"
		} else if i%4 == 2 {
			msgText = "¿Cuál es su horario de atención?"
		} else if i%4 == 3 {
			msgText = "¿Qué kilometraje tiene la Toyota Hilux?"
		}
		hash := st.ComputeHash(ch.ID(), convID, msgText)
		has, rec := st.HasProcessed(hash)
		if !has {
			t.Fatalf("store missing record for %s", convID)
		}
		if !rec.IsSealed() {
			t.Fatalf("store record for %s is not sealed: %+v", convID, rec)
		}
		cp, ok := st.GetCheckpointState(convID)
		if !ok || cp != "cp7_crm:ok" {
			t.Fatalf("store checkpoint for %s is not cp7_crm:ok, got %s", convID, cp)
		}
	}
}

// ============================================================================
// CHALLENGE 4: Concurrency Race on Exact Same Thread Hash (100 Concurrent Workers)
// ============================================================================

func TestChallenger_ConcurrencyRace_IdenticalThreadHash_100Workers(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 10, 30, 0, 0, time.UTC)

	st := store.NewMemoryStore()
	eng := NewEngine(DefaultSafeConfig(), st)
	crmMgr := crm.NewMemoryManager()
	escQ := escalate.NewQueue()
	cache := inventory.NewSeededStubCache(now)

	waStub := channel.NewStubChannel("whatsapp", 650)
	_ = eng.RegisterChannel(waStub)

	// Seed ONE single conversation
	conv := channel.Conversation{ID: "conv-duplicate-race", ChannelID: "whatsapp"}
	waStub.SeedThread(conv, []channel.Message{
		{ID: "m1", Sender: "customer", Text: "¿Qué precio tiene la Toyota Hilux 2022?", Timestamp: now},
	})
	sess, _ := waStub.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co", Active: true})

	pipe := &Pipeline{
		Engine:   eng,
		InvCache: cache,
		Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
		Guard:    guard.NewGuard([]string{"buyacar.com.co"}),
		AIOrch:   ai.NewOrchestrator(nil, nil, skills.NewApprovedCache()),
		EscQueue: escQ,
		CRM:      crmMgr,
	}

	const workers = 100
	var wg sync.WaitGroup
	var processedCount int64
	var skippedCount int64
	var mu sync.Mutex

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			out, err := pipe.ProcessConversation(ctx, waStub, sess, conv, now)
			if err != nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if out.Skipped && strings.Contains(out.SkipReason, "already processed") {
				skippedCount++
			} else if out.CheckpointsOK == 7 {
				processedCount++
			}
		}()
	}

	wg.Wait()

	// Under 100 racing threads on the exact same message:
	// At least 1 must process, and subsequent threads must skip via idempotency
	t.Logf("Race outcome: processed=%d, skipped=%d, total=%d", processedCount, skippedCount, workers)
	if processedCount < 1 {
		t.Fatalf("expected at least 1 worker to successfully process the thread")
	}

	// In the store, the final idempotency record must exist and be sealed
	hash := st.ComputeHash("whatsapp", "conv-duplicate-race", "¿Qué precio tiene la Toyota Hilux 2022?")
	has, rec := st.HasProcessed(hash)
	if !has || !rec.IsSealed() {
		t.Fatalf("expected sealed idempotency record after race")
	}

	// Sequential run after race must strictly skip
	outSeq, errSeq := pipe.ProcessConversation(ctx, waStub, sess, conv, now)
	if errSeq != nil {
		t.Fatalf("unexpected error on sequential run: %v", errSeq)
	}
	if !outSeq.Skipped || !strings.Contains(outSeq.SkipReason, "already processed") {
		t.Fatalf("expected subsequent run to be strictly skipped, got %+v", outSeq)
	}
}

// ============================================================================
// CHALLENGE 5: Mixed Load — Concurrently Reloading Inventory While Pipelines Run
// ============================================================================

func TestChallenger_ConcurrencyMixed_BackgroundInventoryUpdatesAndPipelines(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 11, 0, 0, 0, time.UTC)

	st := store.NewMemoryStore()
	eng := NewEngine(DefaultSafeConfig(), st)
	crmMgr := crm.NewMemoryManager()
	escQ := escalate.NewQueue()
	cache := inventory.NewCache(15, 30)
	cache.LoadSnapshot(inventory.DefaultFixtureVehicles(), `W/"v1"`, now)

	waStub := channel.NewStubChannel("whatsapp", 650)
	_ = eng.RegisterChannel(waStub)

	pipe := &Pipeline{
		Engine:   eng,
		InvCache: cache,
		Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
		Guard:    guard.NewGuard([]string{"buyacar.com.co"}),
		AIOrch:   ai.NewOrchestrator(nil, nil, skills.NewApprovedCache()),
		EscQueue: escQ,
		CRM:      crmMgr,
	}

	const pipelineRuns = 50
	const updaterRuns = 10
	var wg sync.WaitGroup

	// Background updater routines modifying inventory cache
	wg.Add(updaterRuns)
	for u := 0; u < updaterRuns; u++ {
		uIdx := u
		go func() {
			defer wg.Done()
			for step := 0; step < 5; step++ {
				snap := inventory.DefaultFixtureVehicles()
				if uIdx%2 == 0 {
					snap[0].Status = "available"
				} else {
					snap[0].Status = "sold"
				}
				cache.LoadSnapshot(snap, fmt.Sprintf(`W/"u-%d-%d"`, uIdx, step), now)
				cache.TouchRefresh(now)
			}
		}()
	}

	// Parallel pipeline executions
	wg.Add(pipelineRuns)
	for i := 0; i < pipelineRuns; i++ {
		idx := i
		convID := fmt.Sprintf("conv-mixed-%02d", idx)
		conv := channel.Conversation{ID: convID, ChannelID: "whatsapp"}
		waStub.SeedThread(conv, []channel.Message{
			{ID: fmt.Sprintf("m-%d", idx), Sender: "customer", Text: "¿Qué precio tiene la Toyota Hilux 2022?", Timestamp: now},
		})
		go func() {
			defer wg.Done()
			sess, _ := waStub.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co", Active: true})
			out, err := pipe.ProcessConversation(ctx, waStub, sess, conv, now)
			if err != nil {
				t.Errorf("mixed run failed: %v", err)
				return
			}
			// Each execution must either complete cleanly or escalate cleanly (never panic or corrupt state)
			if out.CheckpointsOK < 3 {
				t.Errorf("worker %d: unexpected check-out %d", idx, out.CheckpointsOK)
			}
		}()
	}

	wg.Wait()
}

// ============================================================================
// CHALLENGE 6: Exit Codes, Error Messages, and Escalation Triggers
// ============================================================================

func TestChallenger_ExitCodes_ErrorMessages_EscalationTriggers(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 11, 30, 0, 0, time.UTC)

	// Subtest 6.1: Channel reply error returns error and triggers "send_failed" escalation
	t.Run("ChannelReplyNetworkError", func(t *testing.T) {
		st := store.NewMemoryStore()
		eng := NewEngine(DefaultSafeConfig(), st)
		baseStub := channel.NewStubChannel("whatsapp", 650)
		mockCh := &mockChannelForSendStatus{
			StubChannel: baseStub,
			failError:   errors.New("connection reset by peer"),
		}
		_ = eng.RegisterChannel(mockCh)

		conv := channel.Conversation{ID: "c-send-err", ChannelID: "whatsapp"}
		baseStub.SeedThread(conv, []channel.Message{
			{ID: "m1", Sender: "customer", Text: "¿Dónde quedan?", Timestamp: now},
		})
		sess, _ := baseStub.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co", Active: true})
		escQ := escalate.NewQueue()

		pipe := &Pipeline{
			Engine:   eng,
			InvCache: inventory.NewSeededStubCache(now),
			Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
			Guard:    guard.NewGuard([]string{"buyacar.com.co"}),
			AIOrch:   ai.NewOrchestrator(nil, nil, skills.NewApprovedCache()),
			EscQueue: escQ,
		}

		out, err := pipe.ProcessConversation(ctx, mockCh, sess, conv, now)
		if err == nil || !strings.Contains(err.Error(), "connection reset by peer") {
			t.Fatalf("expected connection reset error, got: %v", err)
		}
		if !out.Escalated || out.EscalationReason != "send_failed" || out.SendStatus != channel.SendFailed {
			t.Fatalf("expected send_failed escalation, got %+v", out)
		}
		if len(escQ.Pending()) != 1 {
			t.Fatalf("expected 1 escalation ticket in queue")
		}
	})

	// Subtest 6.2: Channel reports send_failed status without Go error
	t.Run("ChannelReportsSendFailedStatus", func(t *testing.T) {
		st := store.NewMemoryStore()
		eng := NewEngine(DefaultSafeConfig(), st)
		baseStub := channel.NewStubChannel("whatsapp", 650)
		mockCh := &mockChannelForSendStatus{
			StubChannel: baseStub,
			sendResult:  channel.SendResult{Status: channel.SendFailed},
		}
		_ = eng.RegisterChannel(mockCh)

		conv := channel.Conversation{ID: "c-send-failed-status", ChannelID: "whatsapp"}
		baseStub.SeedThread(conv, []channel.Message{
			{ID: "m1", Sender: "customer", Text: "¿Dónde quedan?", Timestamp: now},
		})
		sess, _ := baseStub.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co", Active: true})
		escQ := escalate.NewQueue()

		pipe := &Pipeline{
			Engine:   eng,
			InvCache: inventory.NewSeededStubCache(now),
			Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
			Guard:    guard.NewGuard([]string{"buyacar.com.co"}),
			AIOrch:   ai.NewOrchestrator(nil, nil, skills.NewApprovedCache()),
			EscQueue: escQ,
		}

		out, err := pipe.ProcessConversation(ctx, mockCh, sess, conv, now)
		if err == nil || !strings.Contains(err.Error(), "reported send_failed") {
			t.Fatalf("expected reported send_failed error, got %v", err)
		}
		if !out.Escalated || out.EscalationReason != "send_failed" {
			t.Fatalf("expected send_failed escalation, got %+v", out)
		}
	})

	// Subtest 6.3: ModeAutopilot status mismatch (got SendDrafted instead of SendVerified)
	t.Run("AutopilotMode_UnverifiedStatusMismatch", func(t *testing.T) {
		cfg := DefaultSafeConfig()
		cfg.OperatingMode = channel.ModeAutopilot
		st := store.NewMemoryStore()
		eng := NewEngine(cfg, st)

		baseStub := channel.NewStubChannel("whatsapp", 650)
		mockCh := &mockChannelForSendStatus{
			StubChannel: baseStub,
			sendResult:  channel.SendResult{Status: channel.SendDrafted},
		}
		_ = eng.RegisterChannel(mockCh)

		conv := channel.Conversation{ID: "c-autopilot-mismatch", ChannelID: "whatsapp"}
		baseStub.SeedThread(conv, []channel.Message{
			{ID: "m1", Sender: "customer", Text: "¿Dónde quedan?", Timestamp: now},
		})
		sess, _ := baseStub.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co", Active: true})
		escQ := escalate.NewQueue()

		pipe := &Pipeline{
			Engine:   eng,
			InvCache: inventory.NewSeededStubCache(now),
			Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
			Guard:    guard.NewGuard([]string{"buyacar.com.co"}),
			AIOrch:   ai.NewOrchestrator(nil, nil, skills.NewApprovedCache()),
			EscQueue: escQ,
		}

		out, err := pipe.ProcessConversation(ctx, mockCh, sess, conv, now)
		if err == nil || !strings.Contains(err.Error(), "unverified send in autopilot mode") {
			t.Fatalf("expected unverified send error, got: %v", err)
		}
		if !out.Escalated || out.EscalationReason != "send_unverified" {
			t.Fatalf("expected send_unverified escalation, got %+v", out)
		}
	})

	// Subtest 6.4: ModeDraft status mismatch (got SendSimulated instead of SendDrafted)
	t.Run("DraftMode_UnrecordedStatusMismatch", func(t *testing.T) {
		cfg := DefaultDraftConfig()
		st := store.NewMemoryStore()
		eng := NewEngine(cfg, st)

		baseStub := channel.NewStubChannel("whatsapp", 650)
		mockCh := &mockChannelForSendStatus{
			StubChannel: baseStub,
			sendResult:  channel.SendResult{Status: channel.SendSimulated},
		}
		_ = eng.RegisterChannel(mockCh)

		conv := channel.Conversation{ID: "c-draft-mismatch", ChannelID: "whatsapp"}
		baseStub.SeedThread(conv, []channel.Message{
			{ID: "m1", Sender: "customer", Text: "¿Dónde quedan?", Timestamp: now},
		})
		sess, _ := baseStub.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co", Active: true})
		escQ := escalate.NewQueue()

		pipe := &Pipeline{
			Engine:   eng,
			InvCache: inventory.NewSeededStubCache(now),
			Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
			Guard:    guard.NewGuard([]string{"buyacar.com.co"}),
			AIOrch:   ai.NewOrchestrator(nil, nil, skills.NewApprovedCache()),
			EscQueue: escQ,
		}

		out, err := pipe.ProcessConversation(ctx, mockCh, sess, conv, now)
		if err == nil || !strings.Contains(err.Error(), "draft not recorded in draft mode") {
			t.Fatalf("expected draft not recorded error, got: %v", err)
		}
		if !out.Escalated || out.EscalationReason != "draft_unrecorded" {
			t.Fatalf("expected draft_unrecorded escalation, got %+v", out)
		}
	})

	// Subtest 6.5: Emergency Kill Switch active triggers immediate skip
	t.Run("EmergencyKillSwitchActive", func(t *testing.T) {
		st := store.NewMemoryStore()
		eng := NewEngine(DefaultSafeConfig(), st)
		eng.TriggerKillSwitch()

		waStub := channel.NewStubChannel("whatsapp", 650)
		_ = eng.RegisterChannel(waStub)

		conv := channel.Conversation{ID: "c-killswitch", ChannelID: "whatsapp"}
		waStub.SeedThread(conv, []channel.Message{
			{ID: "m1", Sender: "customer", Text: "¿Dónde quedan?", Timestamp: now},
		})
		sess, _ := waStub.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co", Active: true})

		pipe := &Pipeline{
			Engine:   eng,
			InvCache: inventory.NewSeededStubCache(now),
			Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
			Guard:    guard.NewGuard([]string{"buyacar.com.co"}),
			AIOrch:   ai.NewOrchestrator(nil, nil, skills.NewApprovedCache()),
			EscQueue: escalate.NewQueue(),
		}

		out, err := pipe.ProcessConversation(ctx, waStub, sess, conv, now)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !out.Skipped || out.SkipReason != "core: emergency kill switch is active" {
			t.Fatalf("expected skipped due to kill switch, got %+v", out)
		}
	})

	// Subtest 6.6: Context Cancellation cleanly returns context.Canceled
	t.Run("ContextCancellationCleanExit", func(t *testing.T) {
		cancCtx, cancel := context.WithCancel(ctx)
		cancel() // immediately cancel

		pipe := &Pipeline{
			InvCache: inventory.NewSeededStubCache(now),
		}
		conv := channel.Conversation{ID: "c-canceled", ChannelID: "whatsapp"}
		ok, reason := pipe.RevalidatePreSend(cancCtx, conv, "veh-001", now)
		if ok || reason != "context canceled" {
			t.Fatalf("expected (false, \"context canceled\"), got (%v, %q)", ok, reason)
		}
	})
}
