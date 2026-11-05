package channel

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"buyacar.com.co/omnicanal/internal/escalate"
)

// TestVersionedSelector_DefaultsAndMethods valida la navegacion, failover y resolucion de VersionedSelector.
func TestVersionedSelector_DefaultsAndMethods(t *testing.T) {
	sel := NewVersionedSelector("btn-send-v1", "1.0.0", "button#send-primary", "button.btn-fallback-1", "button[data-action='send']")

	if sel.ID != "btn-send-v1" || sel.Version != "1.0.0" {
		t.Fatalf("unexpected metadata: id=%s version=%s", sel.ID, sel.Version)
	}

	// 1. Selector inicial es Primary
	if sel.GetActiveSelector() != "button#send-primary" {
		t.Fatalf("expected initial selector to be Primary, got: %s", sel.GetActiveSelector())
	}

	// 2. Failover a Fallback 0
	next1, ok1 := sel.FailoverToNext()
	if !ok1 || next1 != "button.btn-fallback-1" || sel.GetActiveSelector() != "button.btn-fallback-1" {
		t.Fatalf("step 2: expected fallback 0, got ok=%v, next=%s, active=%s", ok1, next1, sel.GetActiveSelector())
	}

	// 3. Failover a Fallback 1
	next2, ok2 := sel.FailoverToNext()
	if !ok2 || next2 != "button[data-action='send']" || sel.GetActiveSelector() != "button[data-action='send']" {
		t.Fatalf("step 3: expected fallback 1, got ok=%v, next=%s, active=%s", ok2, next2, sel.GetActiveSelector())
	}

	// 4. Failover cuando se agotan los fallbacks
	next3, ok3 := sel.FailoverToNext()
	if ok3 || next3 != "" {
		t.Fatalf("step 4: expected failover to fail when exhausted, got ok=%v, next=%s", ok3, next3)
	}

	// 5. Reset restaura al Primary
	sel.Reset()
	if sel.GetActiveSelector() != "button#send-primary" || sel.ActiveIndex != -1 {
		t.Fatalf("step 5: expected Reset() to return to Primary, got active=%s, idx=%d", sel.GetActiveSelector(), sel.ActiveIndex)
	}

	// 6. Resolve contra brokenMap
	broken := map[string]bool{
		"button#send-primary":   true,
		"button.btn-fallback-1": true,
	}
	activeSel, idx, ok := sel.Resolve(broken)
	if !ok || idx != 1 || activeSel != "button[data-action='send']" {
		t.Fatalf("step 6: expected fallback 1 resolved, got ok=%v, idx=%d, sel=%s", ok, idx, activeSel)
	}

	// 7. Resolve cuando todos estan rotos
	brokenAll := map[string]bool{
		"button#send-primary":        true,
		"button.btn-fallback-1":      true,
		"button[data-action='send']": true,
	}
	_, _, okAll := sel.Resolve(brokenAll)
	if okAll {
		t.Fatalf("step 7: expected resolve to fail when all candidates broken")
	}

	// 8. ResolveFunc
	pred := func(s string) bool {
		return s == "button#send-primary"
	}
	fSel, fIdx, fOk := sel.ResolveFunc(pred)
	if !fOk || fIdx != 0 || fSel != "button.btn-fallback-1" {
		t.Fatalf("step 8: expected fallback 0 from predicate, got ok=%v, idx=%d, sel=%s", fOk, fIdx, fSel)
	}

	// 9. Clone
	cloned := sel.Clone()
	if cloned.ID != sel.ID || len(cloned.Fallbacks) != len(sel.Fallbacks) {
		t.Fatalf("step 9: clone mismatch")
	}
}

// TestResilienceTracker_ConsecutiveFailuresAndAutoPause verifica la transicion de fallos
// consecutivos (1, 2, 3 -> pausa automatica y escalamiento).
func TestResilienceTracker_ConsecutiveFailuresAndAutoPause(t *testing.T) {
	escQ := escalate.NewQueue()
	tracker := NewMemoryResilienceTracker(escQ)
	chID := "whatsapp"

	// Fallo 1
	paused, count := tracker.RecordFailure(chID, "connection timeout")
	if paused || count != 1 {
		t.Fatalf("failure 1: expected paused=false, count=1, got paused=%v, count=%d", paused, count)
	}
	if tracker.IsPaused(chID) {
		t.Fatalf("channel should not be paused on failure 1")
	}
	if len(escQ.Pending()) != 0 {
		t.Fatalf("no escalation tickets expected on failure 1")
	}

	// Fallo 2
	paused, count = tracker.RecordFailure(chID, "dom element stale")
	if paused || count != 2 {
		t.Fatalf("failure 2: expected paused=false, count=2, got paused=%v, count=%d", paused, count)
	}
	if tracker.IsPaused(chID) {
		t.Fatalf("channel should not be paused on failure 2")
	}
	if len(escQ.Pending()) != 0 {
		t.Fatalf("no escalation tickets expected on failure 2")
	}

	// Fallo 3: AUTO-PAUSA ESTRICTA
	paused, count = tracker.RecordFailure(chID, "service unavailable 503")
	if !paused || count != 3 {
		t.Fatalf("failure 3: expected paused=true, count=3, got paused=%v, count=%d", paused, count)
	}
	if !tracker.IsPaused(chID) {
		t.Fatalf("expected IsPaused=true after 3rd failure")
	}
	if tracker.GetStatus(chID) != StatusPaused {
		t.Fatalf("expected StatusPaused, got: %s", tracker.GetStatus(chID))
	}

	// Alerta de escalamiento generada
	tickets := escQ.Pending()
	if len(tickets) != 1 {
		t.Fatalf("expected exactly 1 ticket in escalate.Queue, got %d", len(tickets))
	}
	if tickets[0].ChannelID != chID || tickets[0].Reason != EscalationReasonChannelAutoPaused {
		t.Fatalf("ticket mismatch: channel=%s, reason=%s", tickets[0].ChannelID, tickets[0].Reason)
	}
}

// TestResilienceTracker_SuccessReset valida que cualquier exito restablece inmediatamente
// el contador de fallos acumulados a 0.
func TestResilienceTracker_SuccessReset(t *testing.T) {
	escQ := escalate.NewQueue()
	tracker := NewMemoryResilienceTracker(escQ)
	chID := "mercadolibre"

	// 2 fallos consecutivos
	tracker.RecordFailure(chID, "transient error 1")
	tracker.RecordFailure(chID, "transient error 2")
	if tracker.GetConsecutiveFailures(chID) != 2 {
		t.Fatalf("expected 2 failures, got: %d", tracker.GetConsecutiveFailures(chID))
	}

	// Exito verificado
	tracker.RecordSuccess(chID)
	if tracker.GetConsecutiveFailures(chID) != 0 {
		t.Fatalf("expected counter reset to 0, got: %d", tracker.GetConsecutiveFailures(chID))
	}
	if tracker.IsPaused(chID) {
		t.Fatalf("channel should remain unpaused")
	}

	// Nuevo fallo arranca de nuevo desde 1
	paused, count := tracker.RecordFailure(chID, "new failure")
	if paused || count != 1 {
		t.Fatalf("expected count=1 after reset, got paused=%v, count=%d", paused, count)
	}
}

// TestResilienceTracker_AlertStormSuppression valida que una vez pausado el canal,
// fallos adicionales no generen una tormenta de alertas duplicadas.
func TestResilienceTracker_AlertStormSuppression(t *testing.T) {
	escQ := escalate.NewQueue()
	tracker := NewMemoryResilienceTracker(escQ)
	chID := "email"

	// Provocar 3 fallos -> auto-pausa
	tracker.RecordFailure(chID, "err 1")
	tracker.RecordFailure(chID, "err 2")
	tracker.RecordFailure(chID, "err 3")

	if !tracker.IsPaused(chID) {
		t.Fatalf("channel must be paused")
	}
	if len(escQ.Pending()) != 1 {
		t.Fatalf("expected 1 initial escalation ticket, got %d", len(escQ.Pending()))
	}

	// Fallos 4 a 10 mientras sigue pausado
	for i := 4; i <= 10; i++ {
		paused, count := tracker.RecordFailure(chID, fmt.Sprintf("repeated error %d", i))
		if !paused || count != i {
			t.Fatalf("failure %d: expected paused=true, count=%d", i, i)
		}
	}

	// Cantidad de tickets debe mantenerse estrictamente en 1
	if len(escQ.Pending()) != 1 {
		t.Fatalf("alert storm detected! expected 1 ticket, got %d", len(escQ.Pending()))
	}
}

// TestResilienceTracker_ChannelFaultIsolation valida que la caida de un canal no afecte a otros.
func TestResilienceTracker_ChannelFaultIsolation(t *testing.T) {
	escQ := escalate.NewQueue()
	tracker := NewMemoryResilienceTracker(escQ)

	// Pausar whatsapp
	for i := 1; i <= 3; i++ {
		tracker.RecordFailure("whatsapp", "whatsapp timeout")
	}
	if !tracker.IsPaused("whatsapp") {
		t.Fatalf("whatsapp should be paused")
	}

	// email y web deben operar con normalidad
	if tracker.IsPaused("email") {
		t.Fatalf("fault leak! email should not be paused")
	}
	if tracker.IsPaused("web") {
		t.Fatalf("fault leak! web should not be paused")
	}

	// Operar sobre email y web
	tracker.RecordSuccess("email")
	tracker.RecordFailure("web", "minor web glitch")

	if tracker.GetConsecutiveFailures("email") != 0 {
		t.Fatalf("email counter should be 0")
	}
	if tracker.GetConsecutiveFailures("web") != 1 {
		t.Fatalf("web counter should be 1")
	}
	if tracker.GetConsecutiveFailures("whatsapp") != 3 {
		t.Fatalf("whatsapp counter should remain 3")
	}
}

// TestResilienceTracker_ResumeAndManualControls valida Resume, SetPaused y Reset.
func TestResilienceTracker_ResumeAndManualControls(t *testing.T) {
	escQ := escalate.NewQueue()
	tracker := NewMemoryResilienceTracker(escQ)
	chID := "test-channel"

	// Pausar por 3 fallos
	tracker.RecordFailure(chID, "err 1")
	tracker.RecordFailure(chID, "err 2")
	tracker.RecordFailure(chID, "err 3")
	if !tracker.IsPaused(chID) {
		t.Fatalf("expected paused")
	}

	// Reanudar mediante Resume
	tracker.Resume(chID)
	if tracker.IsPaused(chID) {
		t.Fatalf("expected unpaused after Resume")
	}
	if tracker.GetConsecutiveFailures(chID) != 0 {
		t.Fatalf("expected failures reset to 0 after Resume")
	}

	// Pausar manualmente
	tracker.SetPaused(chID, true)
	if !tracker.IsPaused(chID) {
		t.Fatalf("expected paused after SetPaused(true)")
	}

	// Reset total
	tracker.Reset(chID)
	if tracker.IsPaused(chID) || tracker.GetConsecutiveFailures(chID) != 0 {
		t.Fatalf("expected full reset")
	}
}

// TestResilienceTracker_ConcurrencyStress_40Goroutines estresa el tracker con 40 goroutines
// simultaneas para descartar condiciones de carrera (0 data races con -race).
func TestResilienceTracker_ConcurrencyStress_40Goroutines(t *testing.T) {
	escQ := escalate.NewQueue()
	tracker := NewMemoryResilienceTracker(escQ)
	channels := []string{"ch-alpha", "ch-beta", "ch-gamma", "ch-delta"}

	const numWorkers = 40
	const opsPerWorker = 200

	var wg sync.WaitGroup
	var errorCount int64

	startSignal := make(chan struct{})

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			<-startSignal

			ch := channels[workerID%len(channels)]

			for j := 0; j < opsPerWorker; j++ {
				switch j % 5 {
				case 0:
					tracker.RecordFailure(ch, fmt.Sprintf("worker-%d-op-%d", workerID, j))
				case 1:
					tracker.RecordSuccess(ch)
				case 2:
					_ = tracker.IsPaused(ch)
				case 3:
					_ = tracker.GetConsecutiveFailures(ch)
				case 4:
					if workerID%10 == 0 {
						tracker.SetPaused(ch, false)
					}
				}
			}
		}(i)
	}

	close(startSignal)
	wg.Wait()

	if atomic.LoadInt64(&errorCount) > 0 {
		t.Fatalf("concurrency errors detected: %d", errorCount)
	}
}

// TestStubChannel_ResilienceIntegration verifica la integracion de StubChannel con ResilienceTracker.
func TestStubChannel_ResilienceIntegration(t *testing.T) {
	ctx := context.Background()
	escQ := escalate.NewQueue()
	tracker := NewMemoryResilienceTracker(escQ)

	stub := NewStubChannelWithTracker("stub-test", 500, tracker)
	link := Link{ID: "link-1", Active: true}

	// 1. Canal arranca operativo
	sess, err := stub.Open(ctx, link)
	if err != nil {
		t.Fatalf("expected Open ok, got: %v", err)
	}

	// 2. Simular 3 fallos a traves de RecordFailure
	stub.RecordFailure("upstream timeout 1")
	stub.RecordFailure("upstream timeout 2")
	stub.RecordFailure("upstream timeout 3")

	if !stub.IsPaused() {
		t.Fatalf("expected stub channel to be paused")
	}

	// 3. Open, Pending, Read y Reply deben retornar ErrChannelPaused
	if _, err := stub.Open(ctx, link); err != ErrChannelPaused {
		t.Fatalf("expected ErrChannelPaused on Open, got: %v", err)
	}
	if _, err := stub.Pending(ctx, sess); err != ErrChannelPaused {
		t.Fatalf("expected ErrChannelPaused on Pending, got: %v", err)
	}
	conv := Conversation{ID: "conv-1", ChannelID: "stub-test"}
	if _, err := stub.Read(ctx, sess, conv); err != ErrChannelPaused {
		t.Fatalf("expected ErrChannelPaused on Read, got: %v", err)
	}
	msg := Message{ID: "msg-1", Text: "hola"}
	if _, err := stub.Reply(ctx, sess, conv, msg, ModeSimulation); err != ErrChannelPaused {
		t.Fatalf("expected ErrChannelPaused on Reply, got: %v", err)
	}

	// 4. Reanudar canal
	stub.SetPaused(false)
	if stub.IsPaused() {
		t.Fatalf("expected stub channel unpaused")
	}

	// 5. Simular fallo de envio con SetFailSend
	stub.SetFailSend(true, "network drop")
	res, err := stub.Reply(ctx, sess, conv, msg, ModeSimulation)
	if err == nil || res.Status != SendFailed {
		t.Fatalf("expected fail send error and SendFailed status")
	}
}

// TestEmailPilotChannel_ResilienceIntegration verifica la integracion de EmailPilotChannel con ResilienceTracker.
func TestEmailPilotChannel_ResilienceIntegration(t *testing.T) {
	ctx := context.Background()
	escQ := escalate.NewQueue()
	tracker := NewMemoryResilienceTracker(escQ)

	pilot := NewEmailPilotChannelWithTracker(tracker)
	link := Link{ID: "link-email", URL: "https://buyacar.com.co/email", Active: true}

	sess, err := pilot.Open(ctx, link)
	if err != nil {
		t.Fatalf("expected Open ok, got: %v", err)
	}

	// 3 fallos consecutivos
	pilot.RecordFailure()
	pilot.RecordFailure()
	if !pilot.RecordFailure() || !pilot.IsPaused() {
		t.Fatalf("expected channel to pause after 3 failures")
	}

	// Alerta escalada en queue
	if len(escQ.Pending()) != 1 {
		t.Fatalf("expected 1 escalation ticket, got %d", len(escQ.Pending()))
	}

	// Operaciones rechazadas con ErrEmailChannelPaused
	if _, err := pilot.Open(ctx, link); err != ErrEmailChannelPaused {
		t.Fatalf("expected ErrEmailChannelPaused on Open, got %v", err)
	}
	if _, err := pilot.Pending(ctx, sess); err != ErrEmailChannelPaused {
		t.Fatalf("expected ErrEmailChannelPaused on Pending, got %v", err)
	}

	// Reanudar mediante RecordSuccess y SetPaused
	pilot.SetPaused(false)
	if pilot.IsPaused() {
		t.Fatalf("expected unpaused after SetPaused(false)")
	}
}
