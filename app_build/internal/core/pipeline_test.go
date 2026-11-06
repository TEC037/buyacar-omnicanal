package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"buyacar.com.co/omnicanal/internal/ai"
	"buyacar.com.co/omnicanal/internal/channel"
	"buyacar.com.co/omnicanal/internal/crm"
	"buyacar.com.co/omnicanal/internal/escalate"
	"buyacar.com.co/omnicanal/internal/fast"
	"buyacar.com.co/omnicanal/internal/guard"
	"buyacar.com.co/omnicanal/internal/inventory"
	"buyacar.com.co/omnicanal/internal/obs"
	"buyacar.com.co/omnicanal/internal/rag"
	"buyacar.com.co/omnicanal/internal/skills"
	"buyacar.com.co/omnicanal/internal/store"
)

// mockChannelForSendStatus permite simular estados arbitrarios de SendResult y fallos de envio
type mockChannelForSendStatus struct {
	*channel.StubChannel
	failError  error
	sendResult channel.SendResult
}

func (m *mockChannelForSendStatus) Reply(ctx context.Context, session channel.Session, conv channel.Conversation, message channel.Message, mode channel.Mode) (channel.SendResult, error) {
	if m.failError != nil {
		return channel.SendResult{Status: channel.SendFailed}, m.failError
	}
	if m.sendResult.Status != "" {
		return m.sendResult, nil
	}
	return m.StubChannel.Reply(ctx, session, conv, message, mode)
}

func TestPipeline_EndToEndIdempotencyAndResume(t *testing.T) {
	now := time.Date(2026, 10, 26, 13, 55, 0, 0, time.UTC)
	st := store.NewMemoryStore()
	eng := NewEngine(DefaultSafeConfig(), st)

	waStub := channel.NewStubChannel("whatsapp", 650)
	_ = eng.RegisterChannel(waStub)

	conv := channel.Conversation{ID: "conv-100", ChannelID: "whatsapp", CustomerAlias: "Cliente_1", UpdatedAt: now}
	waStub.SeedThread(conv, []channel.Message{
		{ID: "m1", Sender: "customer", Text: "¿Qué precio tiene la Toyota Hilux 2022?", Timestamp: now.Add(-1 * time.Minute)},
	})

	sess, err := waStub.Open(context.Background(), channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})
	if err != nil {
		t.Fatalf("unexpected open error: %v", err)
	}

	crmMgr := crm.NewMemoryManager()
	pipe := &Pipeline{
		Engine:   eng,
		InvCache: inventory.NewSeededStubCache(now),
		Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
		Guard:    guard.NewGuard([]string{"buyacar.com.co"}),
		AIOrch:   ai.NewOrchestrator(nil, nil, skills.NewApprovedCache()),
		EscQueue: escalate.NewQueue(),
		CRM:      crmMgr,
	}

	// Primera ejecucion -> Resuelve por Fast-Path con 0 tokens de IA en SIMULATION
	out1, err := pipe.ProcessConversation(context.Background(), waStub, sess, conv, now)
	if err != nil {
		t.Fatalf("unexpected pipeline error: %v", err)
	}
	if !out1.UsedFastPath || out1.SendStatus != channel.SendSimulated || out1.InputTokens != 0 {
		t.Fatalf("expected Fast-Path simulated send with 0 tokens, got %+v", out1)
	}
	if !strings.Contains(out1.SentReply, "185000000") {
		t.Fatalf("expected verified Hilux price in reply, got %s", out1.SentReply)
	}

	// Verificar CRM task asentada
	tasks := crmMgr.GetTasksByConv("conv-100")
	if len(tasks) == 0 {
		t.Fatalf("expected settled CRM task for conv-100")
	}

	// Verificar idempotency record sellado en store
	has, rec := st.HasProcessed(st.ComputeHash("whatsapp", "conv-100", "¿Qué precio tiene la Toyota Hilux 2022?"))
	if !has || !rec.IsSealed() {
		t.Fatalf("expected sealed idempotency record, got has=%v rec=%+v", has, rec)
	}

	// Verificar checkpoint persistido (cp7_crm:ok) para reanudacion
	cp, ok := st.GetCheckpointState("conv-100")
	if !ok || cp != "cp7_crm:ok" {
		t.Fatalf("expected persisted checkpoint cp7_crm:ok, got %s", cp)
	}

	// Segunda ejecucion tras reinicio con el mismo hilo -> Omitido por idempotencia (0 duplicados)
	out2, err := pipe.ProcessConversation(context.Background(), waStub, sess, conv, now)
	if err != nil {
		t.Fatalf("unexpected error on second run: %v", err)
	}
	if !out2.Skipped || !strings.Contains(out2.SkipReason, "already processed") {
		t.Fatalf("expected duplicate to be skipped by idempotency, got %+v", out2)
	}
}

func TestPipeline_RevalidatePreSend_AvailableVehicle(t *testing.T) {
	now := time.Date(2026, 10, 26, 13, 35, 0, 0, time.UTC)
	cache := inventory.NewSeededStubCache(now)
	pipe := &Pipeline{InvCache: cache}

	conv := channel.Conversation{ID: "c1", ChannelID: "whatsapp"}
	ok, reason := pipe.RevalidatePreSend(context.Background(), conv, "veh-001", now)
	if !ok || reason != "" {
		t.Fatalf("expected (true, \"\"), got (%v, %q)", ok, reason)
	}

	// Consulta sin vehiculo especifico debe pasar siempre
	okEmpty, reasonEmpty := pipe.RevalidatePreSend(context.Background(), conv, "", now)
	if !okEmpty || reasonEmpty != "" {
		t.Fatalf("expected (true, \"\") for empty vehicleID, got (%v, %q)", okEmpty, reasonEmpty)
	}
}

func TestPipeline_RevalidatePreSend_SoldVehicle(t *testing.T) {
	now := time.Date(2026, 10, 26, 13, 35, 0, 0, time.UTC)
	cache := inventory.NewSeededStubCache(now)
	pipe := &Pipeline{InvCache: cache}

	conv := channel.Conversation{ID: "c1", ChannelID: "whatsapp"}
	ok, reason := pipe.RevalidatePreSend(context.Background(), conv, "veh-004", now)
	if ok || reason != "vehicle_sold_or_reserved" {
		t.Fatalf("expected (false, \"vehicle_sold_or_reserved\"), got (%v, %q)", ok, reason)
	}
}

func TestPipeline_RevalidatePreSend_StaleInventory(t *testing.T) {
	now := time.Date(2026, 10, 26, 13, 35, 0, 0, time.UTC)
	cache := inventory.NewSeededStubCache(now)
	pipe := &Pipeline{InvCache: cache}

	conv := channel.Conversation{ID: "c1", ChannelID: "whatsapp"}
	staleTime := now.Add(31 * time.Minute)
	ok, reason := pipe.RevalidatePreSend(context.Background(), conv, "veh-001", staleTime)
	if ok || reason != "inventory_stale" {
		t.Fatalf("expected (false, \"inventory_stale\"), got (%v, %q)", ok, reason)
	}
}

func TestPipeline_RevalidatePreSend_UnavailableCache(t *testing.T) {
	now := time.Date(2026, 10, 26, 13, 35, 0, 0, time.UTC)
	cache := inventory.NewSeededStubCache(now)
	cache.SetUnavailable()
	pipe := &Pipeline{InvCache: cache}

	conv := channel.Conversation{ID: "c1", ChannelID: "whatsapp"}
	ok, reason := pipe.RevalidatePreSend(context.Background(), conv, "veh-001", now)
	if ok || reason != "inventory_unavailable" {
		t.Fatalf("expected (false, \"inventory_unavailable\"), got (%v, %q)", ok, reason)
	}

	// Nil cache
	nilPipe := &Pipeline{InvCache: nil}
	okNil, reasonNil := nilPipe.RevalidatePreSend(context.Background(), conv, "veh-001", now)
	if okNil || reasonNil != "inventory_unavailable" {
		t.Fatalf("expected (false, \"inventory_unavailable\") for nil cache, got (%v, %q)", okNil, reasonNil)
	}
}

func TestPipeline_ProcessConversation_VehicleSoldTransition(t *testing.T) {
	now := time.Date(2026, 10, 26, 13, 35, 0, 0, time.UTC)
	st := store.NewMemoryStore()
	eng := NewEngine(DefaultSafeConfig(), st)

	waStub := channel.NewStubChannel("whatsapp", 650)
	_ = eng.RegisterChannel(waStub)

	// Inicia consulta con veh-001 (Hilux)
	conv := channel.Conversation{ID: "conv-sold-transition", ChannelID: "whatsapp", CustomerAlias: "Cliente_Sold"}
	waStub.SeedThread(conv, []channel.Message{
		{ID: "m1", Sender: "customer", Text: "¿Qué precio tiene la Toyota Hilux 2022?", Timestamp: now},
	})
	sess, _ := waStub.Open(context.Background(), channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})

	// Creamos catalogo donde veh-001 esta marcado como "sold"
	vehicles := inventory.DefaultFixtureVehicles()
	for i := range vehicles {
		if vehicles[i].ID == "veh-001" {
			vehicles[i].Status = "sold"
		}
	}
	cache := inventory.NewCache(15, 30)
	cache.LoadSnapshot(vehicles, `W/"test-etag"`, now)

	escQ := escalate.NewQueue()
	pipe := &Pipeline{
		Engine:   eng,
		InvCache: cache,
		Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
		Guard:    guard.NewGuard([]string{"buyacar.com.co"}),
		AIOrch:   ai.NewOrchestrator(nil, nil, skills.NewApprovedCache()),
		EscQueue: escQ,
	}

	out, err := pipe.ProcessConversation(context.Background(), waStub, sess, conv, now)
	if err != nil {
		t.Fatalf("unexpected pipeline error: %v", err)
	}
	if !out.Escalated || out.EscalationReason != "vehicle_sold_or_reserved" {
		t.Fatalf("expected escalation with reason vehicle_sold_or_reserved, got escalated=%v reason=%s", out.Escalated, out.EscalationReason)
	}
	// Debe haber reemplazado el pitch comercial por el aviso oficial SoldVehicleNotice
	if !strings.Contains(out.SentReply, "vendido/separado") {
		t.Fatalf("expected reply to contain polite sold notice, got %s", out.SentReply)
	}
	// Y la alerta debe existir en la cola humana
	if len(escQ.Pending()) == 0 {
		t.Fatalf("expected escalation ticket in queue")
	}
}

func TestPipeline_ProcessConversation_SendFailedEscalation(t *testing.T) {
	now := time.Date(2026, 10, 26, 13, 35, 0, 0, time.UTC)
	st := store.NewMemoryStore()
	eng := NewEngine(DefaultSafeConfig(), st)

	baseStub := channel.NewStubChannel("whatsapp", 650)
	mockCh := &mockChannelForSendStatus{
		StubChannel: baseStub,
		failError:   errors.New("mock network timeout"),
	}
	_ = eng.RegisterChannel(mockCh)

	conv := channel.Conversation{ID: "conv-fail-send", ChannelID: "whatsapp"}
	baseStub.SeedThread(conv, []channel.Message{
		{ID: "m1", Sender: "customer", Text: "Donde quedan?", Timestamp: now},
	})
	sess, _ := baseStub.Open(context.Background(), channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})

	escQ := escalate.NewQueue()
	pipe := &Pipeline{
		Engine:   eng,
		InvCache: inventory.NewSeededStubCache(now),
		Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
		Guard:    guard.NewGuard([]string{"buyacar.com.co"}),
		AIOrch:   ai.NewOrchestrator(nil, nil, skills.NewApprovedCache()),
		EscQueue: escQ,
	}

	out, err := pipe.ProcessConversation(context.Background(), mockCh, sess, conv, now)
	if err == nil {
		t.Fatalf("expected error from failed send, got nil")
	}
	if !out.Escalated || out.EscalationReason != "send_failed" {
		t.Fatalf("expected escalation send_failed, got %+v", out)
	}
	// Checkpoint cp6_envio no debe haberse registrado
	cp, _ := st.GetCheckpointState("conv-fail-send")
	if cp == "cp6_envio:ok" || cp == "cp7_crm:ok" {
		t.Fatalf("expected cp6 not recorded on failure, got %s", cp)
	}
}

func TestPipeline_ProcessConversation_AutopilotVerification(t *testing.T) {
	now := time.Date(2026, 10, 26, 13, 35, 0, 0, time.UTC)
	st := store.NewMemoryStore()
	cfg := DefaultSafeConfig()
	cfg.OperatingMode = channel.ModeAutopilot
	eng := NewEngine(cfg, st)

	// 1. Channel returns SendVerified -> OK
	baseStub := channel.NewStubChannel("whatsapp", 650)
	mockCh := &mockChannelForSendStatus{
		StubChannel: baseStub,
		sendResult: channel.SendResult{
			Status:    channel.SendVerified,
			MessageID: "msg-verified-1",
			Timestamp: now,
		},
	}
	_ = eng.RegisterChannel(mockCh)

	conv := channel.Conversation{ID: "conv-autopilot-ok", ChannelID: "whatsapp"}
	baseStub.SeedThread(conv, []channel.Message{
		{ID: "m1", Sender: "customer", Text: "Donde quedan?", Timestamp: now},
	})
	sess, _ := baseStub.Open(context.Background(), channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})

	pipe := &Pipeline{
		Engine:   eng,
		InvCache: inventory.NewSeededStubCache(now),
		Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
		Guard:    guard.NewGuard([]string{"buyacar.com.co"}),
		AIOrch:   ai.NewOrchestrator(nil, nil, skills.NewApprovedCache()),
		EscQueue: escalate.NewQueue(),
	}

	out, err := pipe.ProcessConversation(context.Background(), mockCh, sess, conv, now)
	if err != nil {
		t.Fatalf("unexpected error in autopilot: %v", err)
	}
	if out.SendStatus != channel.SendVerified {
		t.Fatalf("expected SendVerified, got %s", out.SendStatus)
	}

	// 2. Channel returns SendDrafted in Autopilot -> Mismatch error & escalation
	conv2 := channel.Conversation{ID: "conv-autopilot-mismatch", ChannelID: "whatsapp"}
	baseStub.SeedThread(conv2, []channel.Message{
		{ID: "m2", Sender: "customer", Text: "Donde quedan?", Timestamp: now},
	})
	mockCh.sendResult = channel.SendResult{
		Status:    channel.SendDrafted,
		MessageID: "msg-mismatch",
		Timestamp: now,
	}

	out2, err2 := pipe.ProcessConversation(context.Background(), mockCh, sess, conv2, now)
	if err2 == nil {
		t.Fatalf("expected error on status mismatch in autopilot mode")
	}
	if !out2.Escalated || out2.EscalationReason != "send_unverified" {
		t.Fatalf("expected escalation send_unverified, got %+v", out2)
	}
}

func TestPipeline_ResilienceTracker_SendSuccessAndAutoPause(t *testing.T) {
	now := time.Date(2026, 10, 26, 13, 35, 0, 0, time.UTC)
	st := store.NewMemoryStore()
	eng := NewEngine(DefaultSafeConfig(), st)

	baseStub := channel.NewStubChannel("whatsapp", 650)
	mockCh := &mockChannelForSendStatus{
		StubChannel: baseStub,
	}
	_ = eng.RegisterChannel(mockCh)

	escQ := escalate.NewQueue()
	tracker := channel.NewMemoryResilienceTracker(escQ)

	pipe := &Pipeline{
		Engine:     eng,
		InvCache:   inventory.NewSeededStubCache(now),
		Fast:       fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
		Guard:      guard.NewGuard([]string{"buyacar.com.co"}),
		AIOrch:     ai.NewOrchestrator(nil, nil, skills.NewApprovedCache()),
		EscQueue:   escQ,
		Resilience: tracker,
	}

	sess, _ := baseStub.Open(context.Background(), channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})

	// 1. Pre-seed 2 failures on whatsapp
	tracker.RecordFailure("whatsapp", "prior error 1")
	tracker.RecordFailure("whatsapp", "prior error 2")
	if tracker.GetConsecutiveFailures("whatsapp") != 2 {
		t.Fatalf("expected 2 pre-seeded failures, got %d", tracker.GetConsecutiveFailures("whatsapp"))
	}

	// 2. Successful execution through pipeline -> triggers RecordSuccess() and resets failures to 0
	conv1 := channel.Conversation{ID: "conv-resilience-1", ChannelID: "whatsapp"}
	baseStub.SeedThread(conv1, []channel.Message{
		{ID: "m1", Sender: "customer", Text: "Donde quedan?", Timestamp: now},
	})
	out1, err1 := pipe.ProcessConversation(context.Background(), mockCh, sess, conv1, now)
	if err1 != nil {
		t.Fatalf("unexpected error on successful flow: %v", err1)
	}
	if out1.SendStatus != channel.SendSimulated {
		t.Fatalf("expected SendSimulated, got %s", out1.SendStatus)
	}
	if tracker.GetConsecutiveFailures("whatsapp") != 0 {
		t.Fatalf("expected failure count reset to 0 after send success, got %d", tracker.GetConsecutiveFailures("whatsapp"))
	}
	if tracker.IsPaused("whatsapp") {
		t.Fatalf("channel should not be paused")
	}

	// 3. Simulate send failures: exactly 3 consecutive failures trigger auto-pause
	mockCh.failError = errors.New("upstream gateway timeout")

	// Failure 1
	convF1 := channel.Conversation{ID: "conv-resilience-f1", ChannelID: "whatsapp"}
	baseStub.SeedThread(convF1, []channel.Message{
		{ID: "mf1", Sender: "customer", Text: "Hola?", Timestamp: now.Add(1 * time.Second)},
	})
	_, _ = pipe.ProcessConversation(context.Background(), mockCh, sess, convF1, now.Add(1*time.Second))
	if tracker.GetConsecutiveFailures("whatsapp") != 1 || tracker.IsPaused("whatsapp") {
		t.Fatalf("expected 1 failure and unpaused after 1st failure")
	}

	// Failure 2
	convF2 := channel.Conversation{ID: "conv-resilience-f2", ChannelID: "whatsapp"}
	baseStub.SeedThread(convF2, []channel.Message{
		{ID: "mf2", Sender: "customer", Text: "Donde quedan?", Timestamp: now.Add(2 * time.Second)},
	})
	outF2, errF2 := pipe.ProcessConversation(context.Background(), mockCh, sess, convF2, now.Add(2*time.Second))
	if errF2 == nil || tracker.GetConsecutiveFailures("whatsapp") != 2 || tracker.IsPaused("whatsapp") {
		t.Fatalf("expected 2 failures and unpaused after 2nd failure, got count=%d, paused=%v, err=%v, out=%+v", tracker.GetConsecutiveFailures("whatsapp"), tracker.IsPaused("whatsapp"), errF2, outF2)
	}

	// Failure 3 -> AUTO-PAUSE
	convF3 := channel.Conversation{ID: "conv-resilience-f3", ChannelID: "whatsapp"}
	baseStub.SeedThread(convF3, []channel.Message{
		{ID: "mf3", Sender: "customer", Text: "Que horario tienen?", Timestamp: now.Add(3 * time.Second)},
	})
	outF3, errF3 := pipe.ProcessConversation(context.Background(), mockCh, sess, convF3, now.Add(3*time.Second))
	if errF3 == nil {
		t.Fatalf("expected error on 3rd failure")
	}
	if tracker.GetConsecutiveFailures("whatsapp") != 3 {
		t.Fatalf("expected 3 failures, got %d", tracker.GetConsecutiveFailures("whatsapp"))
	}
	if !tracker.IsPaused("whatsapp") {
		t.Fatalf("expected channel to be auto-paused after 3rd failure")
	}
	if outF3.EscalationReason != "channel_auto_paused_consecutive_failures" {
		t.Fatalf("expected escalation reason channel_auto_paused_consecutive_failures, got %s", outF3.EscalationReason)
	}

	// Check escalation ticket in queue
	tickets := escQ.Pending()
	foundAutoPauseTicket := false
	for _, tk := range tickets {
		if tk.ChannelID == "whatsapp" && tk.Reason == channel.EscalationReasonChannelAutoPaused {
			foundAutoPauseTicket = true
			break
		}
	}
	if !foundAutoPauseTicket {
		t.Fatalf("expected auto-pause ticket in escalate.Queue")
	}

	// 4. Next conversation on paused channel -> immediately skipped with channel_is_paused and escalated
	convPaused := channel.Conversation{ID: "conv-resilience-paused", ChannelID: "whatsapp"}
	baseStub.SeedThread(convPaused, []channel.Message{
		{ID: "mp1", Sender: "customer", Text: "Intento en canal pausado", Timestamp: now.Add(4 * time.Second)},
	})
	outPaused, errPaused := pipe.ProcessConversation(context.Background(), mockCh, sess, convPaused, now.Add(4*time.Second))
	if errPaused != nil {
		t.Fatalf("expected nil error on skipped conversation, got %v", errPaused)
	}
	if !outPaused.Skipped || outPaused.SkipReason != "channel_is_paused" {
		t.Fatalf("expected skipped with channel_is_paused, got %+v", outPaused)
	}
	if !outPaused.Escalated || outPaused.EscalationReason != "channel_paused_skip" {
		t.Fatalf("expected escalation channel_paused_skip, got %+v", outPaused)
	}

	// 5. Resume channel -> resumes normal operation
	tracker.Resume("whatsapp")
	if tracker.IsPaused("whatsapp") {
		t.Fatalf("expected channel unpaused after Resume")
	}
	mockCh.failError = nil

	convResumed := channel.Conversation{ID: "conv-resilience-resumed", ChannelID: "whatsapp"}
	baseStub.SeedThread(convResumed, []channel.Message{
		{ID: "mr1", Sender: "customer", Text: "Donde quedan?", Timestamp: now.Add(5 * time.Second)},
	})
	outResumed, errResumed := pipe.ProcessConversation(context.Background(), mockCh, sess, convResumed, now.Add(5*time.Second))
	if errResumed != nil || outResumed.Skipped || outResumed.SendStatus != channel.SendSimulated {
		t.Fatalf("expected normal processing after resume, got err=%v out=%+v", errResumed, outResumed)
	}
	if tracker.GetConsecutiveFailures("whatsapp") != 0 {
		t.Fatalf("expected failure count 0, got %d", tracker.GetConsecutiveFailures("whatsapp"))
	}
}

func BenchmarkPipeline_RevalidatePreSend(b *testing.B) {
	now := time.Date(2026, 10, 26, 13, 35, 0, 0, time.UTC)
	cache := inventory.NewSeededStubCache(now)
	pipe := &Pipeline{InvCache: cache}
	conv := channel.Conversation{ID: "conv-bench", ChannelID: "whatsapp"}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ok, _ := pipe.RevalidatePreSend(ctx, conv, "veh-001", now)
		if !ok {
			b.Fatal("expected ok")
		}
	}
}

func TestPipeline_TelemetryTraceHookAndTraceDirExport(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	tmpDir := t.TempDir()

	st := store.NewMemoryStore()
	eng := NewEngine(DefaultSafeConfig(), st)

	waStub := channel.NewStubChannel("whatsapp", 650)
	_ = eng.RegisterChannel(waStub)

	conv := channel.Conversation{ID: "conv-telemetry-1", ChannelID: "whatsapp", CustomerAlias: "Cliente_Tel", UpdatedAt: now}
	waStub.SeedThread(conv, []channel.Message{
		{ID: "m1", Sender: "customer", Text: "¿Qué precio tiene la Toyota Hilux 2022?", Timestamp: now.Add(-1 * time.Minute)},
	})

	sess, err := waStub.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})
	if err != nil {
		t.Fatalf("unexpected open error: %v", err)
	}

	var hookTrace obs.ConversationTrace
	hookCalled := false

	pipe := &Pipeline{
		Engine:   eng,
		InvCache: inventory.NewSeededStubCache(now),
		Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
		Guard:    guard.NewGuard([]string{"buyacar.com.co"}),
		AIOrch:   ai.NewOrchestrator(nil, nil, skills.NewApprovedCache()),
		EscQueue: escalate.NewQueue(),
		CRM:      crm.NewMemoryManager(),
		TraceHook: func(tr obs.ConversationTrace) {
			hookCalled = true
			hookTrace = tr
		},
		TraceDir: tmpDir,
	}

	out, err := pipe.ProcessConversation(ctx, waStub, sess, conv, now)
	if err != nil {
		t.Fatalf("unexpected process conversation error: %v", err)
	}

	// 1. Verify TraceHook called with genuine trace
	if !hookCalled {
		t.Fatalf("expected TraceHook to be called")
	}
	if hookTrace.ConversationID != "conv-telemetry-1" {
		t.Fatalf("expected hook trace for conv-telemetry-1, got %s", hookTrace.ConversationID)
	}
	if len(hookTrace.Checkpoints) != 7 {
		t.Fatalf("expected all 7 checkpoints in trace, got %d", len(hookTrace.Checkpoints))
	}

	// 2. Verify all 7 checkpoints have valid, strictly monotonic timestamps
	for i := 1; i < len(hookTrace.Checkpoints); i++ {
		tPrev, err1 := time.Parse(time.RFC3339Nano, hookTrace.Checkpoints[i-1].TimestampUTC)
		tCurr, err2 := time.Parse(time.RFC3339Nano, hookTrace.Checkpoints[i].TimestampUTC)
		if err1 != nil || err2 != nil {
			t.Fatalf("timestamp parsing error: %v, %v", err1, err2)
		}
		if !tCurr.After(tPrev) {
			t.Fatalf("checkpoint %d (%s at %s) is not strictly after checkpoint %d (%s at %s)",
				i, hookTrace.Checkpoints[i].Name, hookTrace.Checkpoints[i].TimestampUTC,
				i-1, hookTrace.Checkpoints[i-1].Name, hookTrace.Checkpoints[i-1].TimestampUTC)
		}
	}

	// 3. Verify TraceDir produced a genuine JSON trace file
	files, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to read tmpDir: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("expected trace JSON file in tmpDir, found 0 files")
	}
	traceContent, err := os.ReadFile(filepath.Join(tmpDir, files[0].Name()))
	if err != nil {
		t.Fatalf("failed to read trace file: %v", err)
	}
	if !strings.Contains(string(traceContent), "conv-telemetry-1") || !strings.Contains(string(traceContent), "lectura") {
		t.Fatalf("trace file content missing expected trace fields: %s", string(traceContent))
	}

	// 4. Verify outcome contains trace
	if out.Trace.ConversationID != "conv-telemetry-1" {
		t.Fatalf("expected outcome.Trace to be populated")
	}
}

type errorCheckpointStore struct {
	store.Store
	saveErr error
}

func (e *errorCheckpointStore) SaveCheckpointState(convID, state string) error {
	if e.saveErr != nil {
		return e.saveErr
	}
	return e.Store.SaveCheckpointState(convID, state)
}

func TestPipeline_SaveCheckpointErrorCapturedAndLogged(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)

	baseStore := store.NewMemoryStore()
	errStore := &errorCheckpointStore{
		Store:   baseStore,
		saveErr: errors.New("simulated disk failure on save checkpoint"),
	}
	eng := NewEngine(DefaultSafeConfig(), errStore)

	waStub := channel.NewStubChannel("whatsapp", 650)
	_ = eng.RegisterChannel(waStub)

	conv := channel.Conversation{ID: "conv-err-store", ChannelID: "whatsapp", CustomerAlias: "Cliente_Err", UpdatedAt: now}
	waStub.SeedThread(conv, []channel.Message{
		{ID: "m1", Sender: "customer", Text: "¿Horario de atención?", Timestamp: now.Add(-1 * time.Minute)},
	})

	sess, _ := waStub.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})

	pipe := &Pipeline{
		Engine:   eng,
		InvCache: inventory.NewSeededStubCache(now),
		Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
		Guard:    guard.NewGuard([]string{"buyacar.com.co"}),
		AIOrch:   ai.NewOrchestrator(nil, nil, skills.NewApprovedCache()),
		EscQueue: escalate.NewQueue(),
		CRM:      crm.NewMemoryManager(),
	}

	// Pipeline should execute without panicking and log structured error
	out, err := pipe.ProcessConversation(ctx, waStub, sess, conv, now)
	if err != nil {
		t.Fatalf("unexpected pipeline failure: %v", err)
	}
	if out.ConversationID != "conv-err-store" {
		t.Fatalf("expected processed outcome for conv-err-store")
	}
}

type capturingAIProvider struct {
	capturedCompact skills.CompactContext
	replyJSON       []byte
}

func (c *capturingAIProvider) Name() string { return "ollama" }
func (c *capturingAIProvider) GenerateJSON(ctx context.Context, compact skills.CompactContext) ([]byte, int, int, error) {
	c.capturedCompact = compact
	return c.replyJSON, compact.EstimatedTokens, 60, nil
}

func TestPipeline_CP4_RAGContextEnrichment(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 11, 0, 0, 0, time.UTC)

	st := store.NewMemoryStore()
	eng := NewEngine(DefaultSafeConfig(), st)

	waStub := channel.NewStubChannel("whatsapp", 650)
	_ = eng.RegisterChannel(waStub)

	// Consulta que no coincide con Fast-Path (compleja / garantías específicas)
	conv := channel.Conversation{ID: "conv-rag-01", ChannelID: "whatsapp", CustomerAlias: "Cliente_RAG", UpdatedAt: now}
	waStub.SeedThread(conv, []channel.Message{
		{ID: "m1", Sender: "customer", Text: "¿Qué respaldo de garantía y cobertura legal tienen los vehículos usados certificados?", Timestamp: now.Add(-1 * time.Minute)},
	})

	sess, err := waStub.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})
	if err != nil {
		t.Fatalf("unexpected open error: %v", err)
	}

	// Inicializar motor RAG local e indexar política
	ragEngine := rag.NewEngine()
	_, err = ragEngine.IndexDocument(ctx, "warranty_policy", "warranty", "Garantía BuyaCAR Tunja",
		"En BuyaCAR todos los vehículos usados certificados cuentan con 3 meses de garantía legal en caja y motor según Ley 1480 de 2011.")
	if err != nil {
		t.Fatalf("failed to index test document: %v", err)
	}

	validReplyJSON := []byte(`{
		"config_id": "cfg-sim-rag-01",
		"trace": {
			"timestamp_utc": "2026-10-29T11:00:00Z",
			"request_id": "req-rag-101"
		},
		"proposed_reply": "En BuyaCAR Tunja todos nuestros vehículos cuentan con 3 meses de garantía legal en caja y motor según Ley 1480.",
		"intent": "warranty",
		"confidence": 0.95,
		"conversation_summary": "Cliente consulta políticas de garantía BuyaCAR.",
		"crm_update": {
			"tasks_to_create": ["seguimiento_garantia"]
		},
		"governance": {
			"escalate": false,
			"reason": null
		}
	}`)

	capturer := &capturingAIProvider{replyJSON: validReplyJSON}
	aiOrch := ai.NewOrchestrator(nil, capturer, skills.NewApprovedCache())

	pipe := NewPipeline(PipelineConfig{
		Engine:    eng,
		InvCache:  inventory.NewSeededStubCache(now),
		Fast:      fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
		Guard:     guard.NewGuard([]string{"buyacar.com.co"}),
		AIOrch:    aiOrch,
		EscQueue:  escalate.NewQueue(),
		CRM:       crm.NewMemoryManager(),
		RAGEngine: ragEngine,
	})

	out, err := pipe.ProcessConversation(ctx, waStub, sess, conv, now)
	if err != nil {
		t.Fatalf("unexpected pipeline error: %v", err)
	}
	if !out.UsedAI {
		t.Fatalf("expected query to be handled by AI, got UsedFastPath=%v UsedAI=%v", out.UsedFastPath, out.UsedAI)
	}
	if out.SendStatus != channel.SendSimulated {
		t.Fatalf("expected SIMULATION send status, got %s", out.SendStatus)
	}

	// Verificar que CP4 enriqueció el contexto con RAG
	if len(capturer.capturedCompact.RAGChunks) == 0 {
		t.Fatal("expected RAGChunks to be populated in captured CompactContext")
	}
	if capturer.capturedCompact.RAGChunks[0].Title != "Garantía BuyaCAR Tunja" {
		t.Fatalf("expected title 'Garantía BuyaCAR Tunja', got %q", capturer.capturedCompact.RAGChunks[0].Title)
	}
	if !strings.Contains(capturer.capturedCompact.RAGSummary, "Garantía BuyaCAR Tunja") {
		t.Fatalf("expected RAGSummary to contain title, got %q", capturer.capturedCompact.RAGSummary)
	}
	if capturer.capturedCompact.EstimatedTokens <= 0 {
		t.Fatalf("expected estimated tokens > 0, got %d", capturer.capturedCompact.EstimatedTokens)
	}
}

func TestPipeline_CP4_RAGTokenBudgetCompliance(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 11, 30, 0, 0, time.UTC)

	st := store.NewMemoryStore()
	eng := NewEngine(DefaultSafeConfig(), st)

	waStub := channel.NewStubChannel("whatsapp", 650)
	_ = eng.RegisterChannel(waStub)

	conv := channel.Conversation{ID: "conv-rag-budget", ChannelID: "whatsapp", CustomerAlias: "Cliente_Budget", UpdatedAt: now}
	waStub.SeedThread(conv, []channel.Message{
		{ID: "m1", Sender: "customer", Text: "¿Cuáles son las políticas de peritaje y requisitos de revisión técnica en detalle?", Timestamp: now.Add(-1 * time.Minute)},
	})

	sess, _ := waStub.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})

	// Indexar documento intencionalmente largo para verificar que el tope de tokens previene overflow
	ragEngine := rag.NewEngine()
	longText := strings.Repeat("Requisito de financiamiento para vehículos certificados en Boyacá con bancos aliados. ", 50)
	_, err := ragEngine.IndexDocument(ctx, "long_credit_doc", "credit", "Crédito Extenso BuyaCAR", longText)
	if err != nil {
		t.Fatalf("failed to index long document: %v", err)
	}

	validReplyJSON := []byte(`{
		"config_id": "cfg-sim-rag-02",
		"trace": {
			"timestamp_utc": "2026-10-29T11:30:00Z",
			"request_id": "req-rag-102"
		},
		"proposed_reply": "Manejamos convenios con Bancolombia, Banco de Bogotá y Davivienda con crédito ágil en Tunja.",
		"intent": "credit_or_risk_central",
		"confidence": 0.90,
		"conversation_summary": "Consulta crédito vehicular.",
		"crm_update": {"tasks_to_create": []},
		"governance": {"escalate": false, "reason": null}
	}`)

	capturer := &capturingAIProvider{replyJSON: validReplyJSON}
	aiOrch := ai.NewOrchestrator(nil, capturer, skills.NewApprovedCache())

	pipe := NewPipeline(PipelineConfig{
		Engine:    eng,
		InvCache:  inventory.NewSeededStubCache(now),
		Fast:      fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
		Guard:     guard.NewGuard([]string{"buyacar.com.co"}),
		AIOrch:    aiOrch,
		EscQueue:  escalate.NewQueue(),
		CRM:       crm.NewMemoryManager(),
		RAGEngine: ragEngine,
	})

	out, err := pipe.ProcessConversation(ctx, waStub, sess, conv, now)
	if err != nil {
		t.Fatalf("unexpected pipeline failure: %v", err)
	}
	if !out.UsedAI {
		t.Fatalf("expected AI usage")
	}

	// Presupuesto estricto: RAGSummary debe respetar el tope de 150 tokens (~600 caracteres)
	maxAllowedChars := 150 * 4
	if len(capturer.capturedCompact.RAGSummary) > maxAllowedChars {
		t.Fatalf("expected RAGSummary length <= %d, got %d", maxAllowedChars, len(capturer.capturedCompact.RAGSummary))
	}
	if capturer.capturedCompact.EstimatedTokens > skills.MaxInputTokensBudget {
		t.Fatalf("expected estimated tokens <= %d, got %d", skills.MaxInputTokensBudget, capturer.capturedCompact.EstimatedTokens)
	}
}

func TestPipeline_CP4_WithoutRAG_BackwardCompatibility(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

	st := store.NewMemoryStore()
	eng := NewEngine(DefaultSafeConfig(), st)

	waStub := channel.NewStubChannel("whatsapp", 650)
	_ = eng.RegisterChannel(waStub)

	conv := channel.Conversation{ID: "conv-no-rag", ChannelID: "whatsapp", CustomerAlias: "Cliente_NoRAG", UpdatedAt: now}
	waStub.SeedThread(conv, []channel.Message{
		{ID: "m1", Sender: "customer", Text: "¿Qué diferencias de peritaje y revisión aplican al comparar modelos?", Timestamp: now.Add(-1 * time.Minute)},
	})

	sess, _ := waStub.Open(ctx, channel.Link{ID: "l1", URL: "https://buyacar.com.co/wa", Active: true})

	validReplyJSON := []byte(`{
		"config_id": "cfg-sim-no-rag",
		"trace": {
			"timestamp_utc": "2026-10-29T12:00:00Z",
			"request_id": "req-no-rag-103"
		},
		"proposed_reply": "Contamos con la Toyota Hilux 2022 y Mazda CX-5 certificadas en Tunja.",
		"intent": "complex_query",
		"confidence": 0.90,
		"conversation_summary": "Consulta camionetas familiares.",
		"crm_update": {"tasks_to_create": []},
		"governance": {"escalate": false, "reason": null}
	}`)

	capturer := &capturingAIProvider{replyJSON: validReplyJSON}
	aiOrch := ai.NewOrchestrator(nil, capturer, skills.NewApprovedCache())

	// RAGEngine: nil (sin RAG configurado)
	pipe := NewPipeline(PipelineConfig{
		Engine:    eng,
		InvCache:  inventory.NewSeededStubCache(now),
		Fast:      fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
		Guard:     guard.NewGuard([]string{"buyacar.com.co"}),
		AIOrch:    aiOrch,
		EscQueue:  escalate.NewQueue(),
		CRM:       crm.NewMemoryManager(),
		RAGEngine: nil,
	})

	out, err := pipe.ProcessConversation(ctx, waStub, sess, conv, now)
	if err != nil {
		t.Fatalf("unexpected pipeline failure: %v", err)
	}
	if !out.UsedAI {
		t.Fatalf("expected AI usage")
	}

	// Verificar retrocompatibilidad
	if len(capturer.capturedCompact.RAGChunks) != 0 || capturer.capturedCompact.RAGSummary != "" {
		t.Fatalf("expected empty RAG fields when RAGEngine is nil")
	}
}
