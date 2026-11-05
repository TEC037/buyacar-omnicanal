package channel

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestEmailPilotChannel_DraftRecordedAndAutopilotBlocked(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 28, 17, 25, 0, 0, time.UTC)

	pilot := NewEmailPilotChannel()
	if pilot.ID() != EmailPilotChannelID {
		t.Fatalf("expected channel ID %q, got %q", EmailPilotChannelID, pilot.ID())
	}

	// Enlace HTTPS valido para abrir sesion
	sess, err := pilot.Open(ctx, Link{
		ID:        "link-email-pilot",
		URL:       "https://buyacar.com.co/pilot/email",
		ChannelID: EmailPilotChannelID,
		Active:    true,
	})
	if err != nil {
		t.Fatalf("expected valid session on HTTPS link, got %v", err)
	}

	conv := Conversation{
		ID:            "email-conv-001",
		ChannelID:     EmailPilotChannelID,
		CustomerAlias: "Cliente_Correo_Tunja",
		UpdatedAt:     now,
	}
	pilot.IngestInboundEmail(conv, "Consulta Toyota Hilux 2022", []Message{
		{
			ID:        "em-msg-1",
			Sender:    "customer",
			Text:      "Buenas tardes, ¿qué precio tiene la Toyota Hilux 2022 y dónde están ubicados?",
			Timestamp: now.Add(-2 * time.Minute),
		},
	})

	pending, err := pilot.Pending(ctx, sess)
	if err != nil || len(pending) != 1 {
		t.Fatalf("expected 1 pending email conversation, got %d (err=%v)", len(pending), err)
	}

	// 1. Modo SIMULATION: no-op sin registrar borrador externo
	simRes, err := pilot.Reply(ctx, sess, conv, Message{
		ID:        "reply-sim",
		Sender:    "own",
		Text:      "Respuesta simulada",
		Timestamp: now,
	}, ModeSimulation)
	if err != nil || simRes.Status != SendSimulated {
		t.Fatalf("expected simulated_no_op in SIMULATION mode, got %+v (err=%v)", simRes, err)
	}
	if len(pilot.RecordedDrafts()) != 0 {
		t.Fatalf("expected 0 recorded drafts in SIMULATION mode")
	}

	// 2. Modo DRAFT (Etapa B): guarda borrador con prefijo Re: y status draft_recorded
	draftRes, err := pilot.Reply(ctx, sess, conv, Message{
		ID:        "reply-draft",
		Sender:    "own",
		Text:      "Hola, la Toyota Hilux 2022 certificada tiene un precio de $185.000.000 COP. Estamos en Av. Oriental - Cra. 6 #52-23, frente a la Toyota, Tunja.",
		Timestamp: now,
	}, ModeDraft)
	if err != nil || draftRes.Status != SendDrafted {
		t.Fatalf("expected draft_recorded in DRAFT mode, got %+v (err=%v)", draftRes, err)
	}
	drafts := pilot.RecordedDrafts()
	if len(drafts) != 1 || drafts[0].Subject != "Re: Consulta Toyota Hilux 2022" {
		t.Fatalf("unexpected recorded draft: %+v", drafts)
	}

	// 3. Modo AUTOPILOT: estrictamente bloqueado en el adaptador piloto de Etapa B
	autoRes, err := pilot.Reply(ctx, sess, conv, Message{
		ID:        "reply-auto",
		Sender:    "own",
		Text:      "Intento de envio autonomo",
		Timestamp: now,
	}, ModeAutopilot)
	if !errors.Is(err, ErrEmailAutopilotBlocked) || autoRes.Status != SendFailed {
		t.Fatalf("expected ErrEmailAutopilotBlocked in AUTOPILOT mode, got res=%+v err=%v", autoRes, err)
	}

	// 4. Pausa automatica tras 3 fallos consecutivos (Regla 30-channels.md)
	pilot.RecordFailure()
	pilot.RecordFailure()
	if !pilot.RecordFailure() || !pilot.IsPaused() {
		t.Fatalf("expected channel to pause after %d consecutive failures", MaxConsecutiveEmailFailures)
	}
	if _, err := pilot.Pending(ctx, sess); !errors.Is(err, ErrEmailChannelPaused) {
		t.Fatalf("expected ErrEmailChannelPaused when paused, got %v", err)
	}
}
