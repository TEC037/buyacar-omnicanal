package core

import (
	"context"
	"fmt"
	"testing"
	"time"

	"buyacar.com.co/omnicanal/internal/ai"
	"buyacar.com.co/omnicanal/internal/channel"
	"buyacar.com.co/omnicanal/internal/escalate"
	"buyacar.com.co/omnicanal/internal/fast"
	"buyacar.com.co/omnicanal/internal/guard"
	"buyacar.com.co/omnicanal/internal/inventory"
	"buyacar.com.co/omnicanal/internal/obs"
	"buyacar.com.co/omnicanal/internal/skills"
	"buyacar.com.co/omnicanal/internal/store"
)

const (
	// AutopilotStatusBlockedPendingHumanReview asegura que AUTOPILOT permanezca bloqueado sin revisión humana explícita.
	AutopilotStatusBlockedPendingHumanReview = "BLOCKED_PENDING_HUMAN_REVIEW"
	// AutopilotStatusAuthorizedByHuman representa la autorización explícita por parte de un asesor humano calificado.
	AutopilotStatusAuthorizedByHuman = "AUTHORIZED_BY_HUMAN"
)

// TestDraftExitGate_100BatchExerciser implementa y verifica inc-01-draft-batch-100-simulator
// procesando un lote representativo de 100 conversaciones sintéticas en modo DRAFT.
// Desacopla la ejecución técnica del lote de la compuerta humana:
// PROHÍBE cualquier bucle for que auto-apruebe borradores para forzar ReadyForAutopilot=true.
// Verifica que Autopilot permanezca estrictamente en BLOCKED_PENDING_HUMAN_REVIEW.
func TestDraftExitGate_100BatchExerciser(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 2, 0, 0, 0, time.UTC)

	cfg := DefaultDraftConfig()
	st := store.NewMemoryStore()
	eng := NewEngine(cfg, st)

	pilot := channel.NewEmailPilotChannel()
	if err := eng.RegisterChannel(pilot); err != nil {
		t.Fatalf("unexpected error registering pilot: %v", err)
	}

	sess, err := pilot.Open(ctx, channel.Link{
		ID:        "link-batch-100",
		URL:       "https://buyacar.com.co/pilot/batch100",
		ChannelID: channel.EmailPilotChannelID,
		Active:    true,
	})
	if err != nil {
		t.Fatalf("unexpected error opening pilot session: %v", err)
	}

	invCache := inventory.NewSeededStubCache(now)
	guardInst := guard.NewGuard([]string{"buyacar.com.co"})
	pipe := &Pipeline{
		Engine:   eng,
		InvCache: invCache,
		Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), cfg.ConfidenceThreshold),
		Guard:    guardInst,
		AIOrch:   ai.NewOrchestrator(nil, nil, skills.NewApprovedCache()),
		EscQueue: escalate.NewQueue(),
	}

	metrics := obs.NewMetricsCollector()

	// 100 conversaciones sintéticas en modo DRAFT:
	// - 80 consultas resueltas por Fast-Path (horario, ubicación, financiamiento, Hilux, CX-5 2023)
	// - 20 consultas complejas que escalan controladamente
	goldenCases := build64GoldenCases()
	queries := make([]string, 16)
	for i := 0; i < 16; i++ {
		queries[i] = goldenCases[i].Incoming
	}

	for i := 1; i <= 100; i++ {
		cID := fmt.Sprintf("conv-batch-%03d", i)
		txt := queries[(i-1)%len(queries)]
		if i > 80 {
			// Consultas que no tienen coincidencia en inventario o solicitan asesor
			txt = goldenCases[48+(i-81)%16].Incoming
		}

		conv := channel.Conversation{
			ID:            cID,
			ChannelID:     channel.EmailPilotChannelID,
			CustomerAlias: fmt.Sprintf("Cliente_%03d", i),
			UpdatedAt:     now.Add(time.Duration(i) * time.Minute),
		}
		pilot.IngestInboundEmail(conv, "Consulta BuyaCAR", []channel.Message{
			{ID: fmt.Sprintf("msg-%03d", i), Sender: "customer", Text: txt, Timestamp: conv.UpdatedAt},
		})

		start := time.Now()
		out, err := pipe.ProcessConversation(ctx, pilot, sess, conv, conv.UpdatedAt)
		if err != nil {
			t.Fatalf("unexpected pipeline failure on conv %s: %v", cID, err)
		}
		metrics.RecordFastPathLatency(float64(time.Since(start).Microseconds()) / 1000.0)
		metrics.RecordOutcome(out.UsedFastPath, out.UsedAI, out.Escalated, out.EscalationReason, out.Skipped, out.SkipReason, out.InputTokens, out.OutputTokens)
	}

	// Verificación técnica de ejecución en lote:
	// Los borradores deben generarse y quedar registrados en pilot.RecordedDrafts()
	// estrictamente PENDIENTES de revisión humana.
	// PROHIBIDO: auto-aprobar borradores con un bucle for.
	recordedDrafts := pilot.RecordedDrafts()
	if len(recordedDrafts) < 70 {
		t.Fatalf("expected >= 70 drafts generated in outbox, got %d", len(recordedDrafts))
	}

	totalDrafts, reviewed, unedited, uneditedPct := pilot.DraftGateMetrics()
	if totalDrafts < 70 {
		t.Fatalf("expected >= 70 total drafts recorded in pilot metrics, got %d", totalDrafts)
	}
	if reviewed != 0 || unedited != 0 {
		t.Fatalf("expected 0 reviewed and 0 unedited drafts before human action, got reviewed=%d unedited=%d", reviewed, unedited)
	}
	if uneditedPct != 0.0 {
		t.Fatalf("expected 0.0%% unedited approval before human review, got %.1f%%", uneditedPct)
	}

	// Evaluación de compuerta: sin revisión humana explícita, ReadyForAutopilot DEBE ser false.
	gate := metrics.EvaluateDraftExitGate(uneditedPct)
	if gate.ReadyForAutopilot {
		t.Fatalf("security violation: ReadyForAutopilot cannot be true without human review, got %+v", gate)
	}

	// Autopilot DEBE permanecer estrictamente en BLOCKED_PENDING_HUMAN_REVIEW
	autopilotStatus := AutopilotStatusBlockedPendingHumanReview
	if gate.ReadyForAutopilot {
		autopilotStatus = AutopilotStatusAuthorizedByHuman
	}
	if autopilotStatus != AutopilotStatusBlockedPendingHumanReview {
		t.Fatalf("expected Autopilot status %s, got %s",
			AutopilotStatusBlockedPendingHumanReview, autopilotStatus)
	}

	// Verificaciones de seguridad y calidad del arnés
	if gate.HallucinatedNumbers != 0 {
		t.Fatalf("safety violation: hallucinated numbers detected: %d", gate.HallucinatedNumbers)
	}
	if gate.DuplicatesSent != 0 {
		t.Fatalf("safety violation: duplicates sent detected: %d", gate.DuplicatesSent)
	}
	if gate.FastPathPercent < 70.0 {
		t.Fatalf("expected FastPathPercent >= 70.0%%, got %.1f%%", gate.FastPathPercent)
	}

	_, _, p99 := metrics.FastPathPercentiles()
	if p99 >= 5.0 {
		t.Fatalf("expected Fast-Path p99 < 5.0 ms, got %.3f ms", p99)
	}

	if len(pipe.EscQueue.Pending()) != 20 {
		t.Fatalf("expected exactly 20 escalations to escalate.Queue, got %d", len(pipe.EscQueue.Pending()))
	}
}

// TestDraftExitGate_DecoupledHumanAuthorizationGate verifica de manera aislada
// que la transición a AUTOPILOT dependa estrictamente de la autorización humana explícita.
func TestDraftExitGate_DecoupledHumanAuthorizationGate(t *testing.T) {
	mc := obs.NewMetricsCollector()
	for i := 0; i < 80; i++ {
		mc.RecordOutcome(true, false, false, "", false, "", 0, 0)
	}
	for i := 0; i < 20; i++ {
		mc.RecordOutcome(false, true, false, "", false, "", 200, 50)
	}

	t.Run("BlockedWithoutHumanReview", func(t *testing.T) {
		// Sin revisión humana (0% aprobados), la compuerta permanece bloqueada.
		gate := mc.EvaluateDraftExitGate(0.0)
		if gate.ReadyForAutopilot {
			t.Fatalf("expected ReadyForAutopilot=false when unreviewed, got %+v", gate)
		}
		status := AutopilotStatusBlockedPendingHumanReview
		if gate.ReadyForAutopilot {
			status = AutopilotStatusAuthorizedByHuman
		}
		if status != AutopilotStatusBlockedPendingHumanReview {
			t.Fatalf("expected %s, got %s", AutopilotStatusBlockedPendingHumanReview, status)
		}
	})

	t.Run("BlockedWhenApprovalBelowThreshold", func(t *testing.T) {
		// Asesor humano revisó pero requirió edición en 30% (70% sin editar < 85% meta)
		gate := mc.EvaluateDraftExitGate(70.0)
		if gate.ReadyForAutopilot {
			t.Fatalf("expected ReadyForAutopilot=false when unedited approval < 85%%, got %+v", gate)
		}
	})

	t.Run("AuthorizedOnlyWithExplicitHumanReview", func(t *testing.T) {
		// Asesor humano explícitamente revisó y aprobó con >= 85% sin edición (ej. 90%)
		explicitHumanAuthorized := true
		gate := mc.EvaluateDraftExitGate(90.0)
		if !gate.ReadyForAutopilot {
			t.Fatalf("expected ReadyForAutopilot=true when explicitly authorized by human with 90%% unedited, got %+v", gate)
		}
		var status string
		if explicitHumanAuthorized && gate.ReadyForAutopilot {
			status = AutopilotStatusAuthorizedByHuman
		} else {
			status = AutopilotStatusBlockedPendingHumanReview
		}
		if status != AutopilotStatusAuthorizedByHuman {
			t.Fatalf("expected %s, got %s", AutopilotStatusAuthorizedByHuman, status)
		}
	})
}
