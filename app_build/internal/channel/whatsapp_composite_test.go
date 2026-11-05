package channel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"buyacar.com.co/omnicanal/internal/escalate"
)

func TestWhatsAppComposite_PendingWABAFallover(t *testing.T) {
	driver := NewSimulatedPlaywrightDriver()
	rt := NewResilienceTracker(nil)
	rc := NewRateController(DefaultRateConfig())

	cloud := NewWhatsAppCloudChannel(WhatsAppCloudConfig{
		PhoneNumberID: "cloud123",
		AccessToken:   "token123",
	}, rt, rc)

	playwright := NewWhatsAppPlaywrightChannel(driver, rt, rc)

	// Inicialmente WABA sin verificar (wabaVerified = false)
	composite := NewWhatsAppCompositeChannel(cloud, playwright, false, rt, rc)

	active, reason := composite.IsFallbackActive()
	if !active || reason != "waba_verification_pending" {
		t.Fatalf("expected fallback active due to waba_verification_pending, got active=%v, reason=%s", active, reason)
	}

	ctx := context.Background()
	sess := Session{ID: "sess-comp", ChannelID: WhatsAppCompositeChannelID}
	conv := Conversation{ID: "573111111111", ChannelID: WhatsAppCompositeChannelID}

	// Reply debe canalizarse a Playwright
	res, err := composite.Reply(ctx, sess, conv, Message{ID: "m1", Text: "Mensaje via fallback"}, ModeSimulation)
	if err != nil || res.Status != SendSimulated {
		t.Fatalf("expected SendSimulated via Playwright fallback, got err=%v, res=%+v", err, res)
	}

	// Meta aprueba la verificacion WABA
	composite.SetWABAVerified(true)
	activeAfter, reasonAfter := composite.IsFallbackActive()
	if activeAfter {
		t.Fatalf("expected fallback inactive after WABA verification, got reason=%s", reasonAfter)
	}
}

func TestWhatsAppComposite_HTTP429AutoFailover(t *testing.T) {
	// Servidor mock que simula saturacion 429 en Cloud API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"Rate limit reached","code":429}}`))
	}))
	defer server.Close()

	driver := NewSimulatedPlaywrightDriver()
	rt := NewResilienceTracker(nil)
	rc := NewRateController(DefaultRateConfig())

	cloud := NewWhatsAppCloudChannel(WhatsAppCloudConfig{
		PhoneNumberID: "cloud429",
		AccessToken:   "token429",
		BaseURL:       server.URL,
	}, rt, rc)

	// Simular mensaje inbound reciente para que no sea bloqueado por la regla de 24h
	customerID := "573222222222"
	cloud.RecordInboundMessage(WhatsAppInboundMessage{
		ID:        "in1",
		From:      customerID,
		Timestamp: time.Now().UTC(),
		Text:      "start",
	})

	playwright := NewWhatsAppPlaywrightChannel(driver, rt, rc)
	composite := NewWhatsAppCompositeChannel(cloud, playwright, true, rt, rc)

	ctx := context.Background()
	sess := Session{ID: "s-comp", ChannelID: WhatsAppCompositeChannelID}
	conv := Conversation{ID: customerID, ChannelID: WhatsAppCompositeChannelID}

	// Al intentar Autopilot, Cloud retorna 429 -> Conmuta automaticamente a Playwright
	res, err := composite.Reply(ctx, sess, conv, Message{ID: "m_auto", Text: "Prueba failover 429"}, ModeAutopilot)
	if err != nil {
		t.Fatalf("expected successful failover to playwright on 429, got err=%v", err)
	}
	if res.Status != SendVerified {
		t.Fatalf("expected SendVerified from Playwright fallback, got status=%s", res.Status)
	}

	// El estado de contingencia debe permanecer activo durante el cooldown
	active, reason := composite.IsFallbackActive()
	if !active || reason != "http_429_quota_saturation" {
		t.Fatalf("expected quota cooldown active, got active=%v, reason=%s", active, reason)
	}

	// Verificamos que el mensaje fue efectivamente despachado por el driver de Playwright
	if len(driver.RecordedMsgs) != 1 {
		t.Fatalf("expected 1 recorded message in Playwright driver, got %d", len(driver.RecordedMsgs))
	}
}

func TestWhatsAppPlaywright_VersionedSelectorsResolution(t *testing.T) {
	driver := NewSimulatedPlaywrightDriver()
	ch := NewWhatsAppPlaywrightChannel(driver, nil, nil)

	sSel, iSel, bSel := ch.Selectors()
	if sSel.Primary != "div[contenteditable='true'][data-tab='3']" {
		t.Fatalf("unexpected primary search selector: %s", sSel.Primary)
	}
	if iSel.Primary != "footer div[contenteditable='true'][data-tab='10']" {
		t.Fatalf("unexpected primary input selector: %s", iSel.Primary)
	}
	if bSel.Primary != "button[data-tab='11']" {
		t.Fatalf("unexpected primary send selector: %s", bSel.Primary)
	}

	// Simular rotura del selector primario de busqueda en el DOM
	driver.BrokenMap[sSel.Primary] = true

	ctx := context.Background()
	sess := Session{ID: "s1", ChannelID: WhatsAppPlaywrightChannelID}
	conv := Conversation{ID: "573333333333", ChannelID: WhatsAppPlaywrightChannelID}

	// Debe auto-rotar al primer fallback disponible ("[data-testid='chat-list-search']")
	res, err := ch.Reply(ctx, sess, conv, Message{ID: "m1", Text: "Hola con selector rotado"}, ModeAutopilot)
	if err != nil || res.Status != SendVerified {
		t.Fatalf("expected successful send after selector failover, got err=%v, res=%+v", err, res)
	}

	// Romper todos los selectores de busqueda
	for _, fb := range sSel.Fallbacks {
		driver.BrokenMap[fb] = true
	}

	// Ahora debe agotar los selectores y retornar ErrBrowserSelectorsExhausted
	_, errExhausted := ch.Reply(ctx, sess, conv, Message{ID: "m2", Text: "Intento agotado"}, ModeAutopilot)
	if errExhausted != ErrBrowserSelectorsExhausted {
		t.Fatalf("expected ErrBrowserSelectorsExhausted, got %v", errExhausted)
	}
}

func TestWhatsAppPlaywright_AutoPauseOn3Failures(t *testing.T) {
	driver := NewSimulatedPlaywrightDriver()
	driver.FailSend = true // Simula fallo general del navegador

	escQueue := escalate.NewQueue()
	rt := NewResilienceTracker(escQueue)
	ch := NewWhatsAppPlaywrightChannel(driver, rt, nil)

	ctx := context.Background()
	sess := Session{ID: "s1", ChannelID: WhatsAppPlaywrightChannelID}
	conv := Conversation{ID: "573444444444", ChannelID: WhatsAppPlaywrightChannelID}

	// 3 fallos consecutivos
	for i := 1; i <= 3; i++ {
		_, err := ch.Reply(ctx, sess, conv, Message{ID: "m", Text: "Test"}, ModeAutopilot)
		if err == nil {
			t.Fatalf("attempt %d: expected error on failed driver", i)
		}
	}

	if !ch.IsPaused() {
		t.Fatal("expected Playwright channel to be paused after 3 consecutive failures")
	}

	// Intento subsecuente debe ser rechazado inmediatamente
	_, errPaused := ch.Reply(ctx, sess, conv, Message{ID: "m4", Text: "Test post-pause"}, ModeSimulation)
	if errPaused != ErrChannelPaused {
		t.Fatalf("expected ErrChannelPaused, got %v", errPaused)
	}

	// Escalamiento humano encolado
	if len(escQueue.Pending()) == 0 {
		t.Fatal("expected ticket in escalate.Queue upon auto-pause")
	}
}

func TestWhatsAppComposite_WebhookDelegation(t *testing.T) {
	cloud := NewWhatsAppCloudChannel(WhatsAppCloudConfig{
		VerifyToken: "token_composite_webhook",
	}, nil, nil)
	playwright := NewWhatsAppPlaywrightChannel(nil, nil, nil)
	composite := NewWhatsAppCompositeChannel(cloud, playwright, true, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/webhook?hub.mode=subscribe&hub.verify_token=token_composite_webhook&hub.challenge=abc_xyz", nil)
	rec := httptest.NewRecorder()

	composite.HandleWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from composite webhook, got %d", rec.Code)
	}
	if rec.Body.String() != "abc_xyz" {
		t.Fatalf("expected challenge 'abc_xyz', got %s", rec.Body.String())
	}
}
