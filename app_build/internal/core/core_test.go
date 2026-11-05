package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"buyacar.com.co/omnicanal/internal/ai"
	"buyacar.com.co/omnicanal/internal/channel"
	"buyacar.com.co/omnicanal/internal/escalate"
	"buyacar.com.co/omnicanal/internal/fast"
	"buyacar.com.co/omnicanal/internal/guard"
	"buyacar.com.co/omnicanal/internal/inventory"
	"buyacar.com.co/omnicanal/internal/links"
	"buyacar.com.co/omnicanal/internal/skills"
	"buyacar.com.co/omnicanal/internal/store"
)

func TestEngine_DefaultSimulationAndKillSwitch(t *testing.T) {
	cfg := DefaultSafeConfig()
	if cfg.OperatingMode != channel.ModeSimulation {
		t.Fatalf("expected SIMULATION mode, got %s", cfg.OperatingMode)
	}

	st := store.NewMemoryStore()
	eng := NewEngine(cfg, st)

	if eng.IsKillSwitchActive() {
		t.Fatal("expected kill switch to be false initially")
	}

	eng.TriggerKillSwitch()
	if !eng.IsKillSwitchActive() {
		t.Fatal("expected kill switch to be active after TriggerKillSwitch")
	}
}

func TestEngine_ShouldProcessThread_Rules(t *testing.T) {
	cfg := DefaultSafeConfig()
	st := store.NewMemoryStore()
	eng := NewEngine(cfg, st)
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

	// 1. Omitir cuando el ultimo mensaje es propio
	ownThread := channel.Thread{
		Conversation: channel.Conversation{ID: "c1", ChannelID: "whatsapp"},
		Messages: []channel.Message{
			{ID: "m1", Sender: "customer", Text: "Hola", Timestamp: now.Add(-10 * time.Minute)},
			{ID: "m2", Sender: "own", Text: "Hola, bienvenido a BuyaCAR", Timestamp: now.Add(-5 * time.Minute)},
		},
	}
	if _, err := eng.ShouldProcessThread(context.Background(), ownThread, now); !errors.Is(err, ErrOwnLastMessage) {
		t.Fatalf("expected ErrOwnLastMessage, got %v", err)
	}

	// 2. Omitir conversacion de mas de 9 semanas
	oldThread := channel.Thread{
		Conversation: channel.Conversation{ID: "c2", ChannelID: "whatsapp"},
		Messages: []channel.Message{
			{ID: "m3", Sender: "customer", Text: "Sigue disponible?", Timestamp: now.Add(-10 * 7 * 24 * time.Hour)},
		},
	}
	if _, err := eng.ShouldProcessThread(context.Background(), oldThread, now); !errors.Is(err, ErrOlderThan9Weeks) {
		t.Fatalf("expected ErrOlderThan9Weeks, got %v", err)
	}

	// 3. Procesar conversacion valida y verificar idempotencia
	validThread := channel.Thread{
		Conversation: channel.Conversation{ID: "c3", ChannelID: "whatsapp"},
		Messages: []channel.Message{
			{ID: "m4", Sender: "customer", Text: "Donde estan ubicados?", Timestamp: now.Add(-5 * time.Minute)},
		},
	}
	hash, err := eng.ShouldProcessThread(context.Background(), validThread, now)
	if err != nil {
		t.Fatalf("unexpected error for valid thread: %v", err)
	}

	_ = st.MarkProcessed(store.IdempotencyRecord{
		Hash:      hash,
		ChannelID: "whatsapp",
		ConvID:    "c3",
		ReplyText: "Av. Oriental - Cra. 6 #52-23",
	})

	if _, err := eng.ShouldProcessThread(context.Background(), validThread, now); !errors.Is(err, ErrAlreadyProcessed) {
		t.Fatalf("expected ErrAlreadyProcessed on duplicate message, got %v", err)
	}
}

func TestLinks_AllowlistAndActiveFilter(t *testing.T) {
	repo := links.NewRepository([]string{"buyacar.com.co"})

	// Rechazar HTTP no seguro
	if err := repo.Upsert(channel.Link{ID: "1", URL: "http://buyacar.com.co/cars", Active: true}); err == nil {
		t.Fatal("expected error for non-HTTPS link")
	}

	// Rechazar dominio fuera de allowlist
	if err := repo.Upsert(channel.Link{ID: "2", URL: "https://malicious.example.com/cars", Active: true}); err == nil {
		t.Fatal("expected error for unauthorized domain")
	}

	// Aceptar HTTPS en allowlist (uno activo y uno inactivo)
	if err := repo.Upsert(channel.Link{ID: "3", URL: "https://buyacar.com.co/wp-json/vehica/v1/cars", Active: true}); err != nil {
		t.Fatalf("unexpected error for valid link: %v", err)
	}
	if err := repo.Upsert(channel.Link{ID: "4", URL: "https://buyacar.com.co/inactive", Active: false}); err != nil {
		t.Fatalf("unexpected error for valid inactive link: %v", err)
	}

	active := repo.ActiveLinks()
	if len(active) != 1 || active[0].ID != "3" {
		t.Fatalf("expected exactly 1 active link (ID 3), got %+v", active)
	}
}

func TestPipeline_EmailPilotDraftModeEndToEnd(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 28, 17, 25, 0, 0, time.UTC)

	cfg := DefaultSafeConfig()
	cfg.OperatingMode = channel.ModeDraft // Prueba aislada de transicion Etapa B en modo DRAFT
	st := store.NewMemoryStore()
	eng := NewEngine(cfg, st)

	emailPilot := channel.NewEmailPilotChannel()
	if err := eng.RegisterChannel(emailPilot); err != nil {
		t.Fatalf("unexpected error registering email pilot channel: %v", err)
	}

	sess, err := emailPilot.Open(ctx, channel.Link{
		ID:        "link-email-pilot-1",
		URL:       "https://buyacar.com.co/pilot/email",
		ChannelID: channel.EmailPilotChannelID,
		Active:    true,
	})
	if err != nil {
		t.Fatalf("unexpected error opening email pilot session: %v", err)
	}

	conv := channel.Conversation{
		ID:            "email-draft-001",
		ChannelID:     channel.EmailPilotChannelID,
		CustomerAlias: "Lead_Email_Tunja",
		UpdatedAt:     now,
	}
	emailPilot.IngestInboundEmail(conv, "Disponibilidad Toyota Hilux 2022", []channel.Message{
		{
			ID:        "em-1",
			Sender:    "customer",
			Text:      "¿Qué precio tiene la Toyota Hilux 2022?",
			Timestamp: now.Add(-1 * time.Minute),
		},
	})

	pipe := &Pipeline{
		Engine:   eng,
		InvCache: inventory.NewSeededStubCache(now),
		Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), cfg.ConfidenceThreshold),
		Guard:    guard.NewGuard([]string{"buyacar.com.co"}),
		AIOrch:   ai.NewOrchestrator(nil, nil, skills.NewApprovedCache()),
		EscQueue: escalate.NewQueue(),
	}

	out, err := pipe.ProcessConversation(ctx, emailPilot, sess, conv, now)
	if err != nil {
		t.Fatalf("unexpected pipeline error: %v", err)
	}
	if !out.UsedFastPath || out.SendStatus != channel.SendDrafted || out.CheckpointsOK != 7 {
		t.Fatalf("expected Fast-Path + draft_recorded + 7 checkpoints OK, got %+v", out)
	}

	drafts := emailPilot.RecordedDrafts()
	if len(drafts) != 1 || drafts[0].ConversationID != "email-draft-001" {
		t.Fatalf("expected 1 recorded email draft, got %+v", drafts)
	}
}
