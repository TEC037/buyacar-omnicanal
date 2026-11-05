package channel

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"buyacar.com.co/omnicanal/internal/escalate"
)

func TestWhatsAppCloud_LimitsAndIdentity(t *testing.T) {
	ch := NewWhatsAppCloudChannel(WhatsAppCloudConfig{
		PhoneNumberID: "123456789",
		AccessToken:   "token_test",
		VerifyToken:   "secret_verify",
		AppSecret:     "secret_app",
	}, nil, nil)

	if ch.ID() != WhatsAppCloudChannelID {
		t.Fatalf("expected ID %s, got %s", WhatsAppCloudChannelID, ch.ID())
	}

	limits := ch.Limits()
	if limits.MaxCharacterLength != 650 {
		t.Errorf("expected MaxCharacterLength 650 (Mandamiento 2), got %d", limits.MaxCharacterLength)
	}
	if limits.MaxPerHour != 120 {
		t.Errorf("expected MaxPerHour 120, got %d", limits.MaxPerHour)
	}
	if limits.MaxPerDay != 250 {
		t.Errorf("expected MaxPerDay 250, got %d", limits.MaxPerDay)
	}
}

func TestWhatsAppCloud_WebhookVerificationHandshake(t *testing.T) {
	ch := NewWhatsAppCloudChannel(WhatsAppCloudConfig{
		VerifyToken: "buyacar_super_secret_token",
	}, nil, nil)

	// Caso 1: Handshake valido
	req := httptest.NewRequest(http.MethodGet, "/webhook?hub.mode=subscribe&hub.verify_token=buyacar_super_secret_token&hub.challenge=challenge_123456", nil)
	rec := httptest.NewRecorder()
	ch.HandleWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	if rec.Body.String() != "challenge_123456" {
		t.Fatalf("expected challenge 'challenge_123456', got %q", rec.Body.String())
	}

	// Caso 2: Token invalido
	reqBad := httptest.NewRequest(http.MethodGet, "/webhook?hub.mode=subscribe&hub.verify_token=wrong_token&hub.challenge=123", nil)
	recBad := httptest.NewRecorder()
	ch.HandleWebhook(recBad, reqBad)
	if recBad.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for bad token, got %d", recBad.Code)
	}

	// Caso 3: Mode incorrecto
	reqMode := httptest.NewRequest(http.MethodGet, "/webhook?hub.mode=other&hub.verify_token=buyacar_super_secret_token&hub.challenge=123", nil)
	recMode := httptest.NewRecorder()
	ch.HandleWebhook(recMode, reqMode)
	if recMode.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for wrong mode, got %d", recMode.Code)
	}
}

func TestWhatsAppCloud_WebhookSignatureVerification(t *testing.T) {
	appSecret := "app_secret_test_98765"
	ch := NewWhatsAppCloudChannel(WhatsAppCloudConfig{
		AppSecret: appSecret,
	}, nil, nil)

	payload := []byte(`{"object":"whatsapp_business_account","entry":[]}`)

	// Calculo de HMAC-SHA256 valido
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(payload)
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !ch.VerifySignature(payload, validSig) {
		t.Fatal("expected VerifySignature to return true for valid signature")
	}

	// Modificacion del payload debe invalidar
	tamperedPayload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"1"}]}`)
	if ch.VerifySignature(tamperedPayload, validSig) {
		t.Fatal("expected VerifySignature to fail for tampered payload")
	}

	// Firma con clave incorrecta
	badMac := hmac.New(sha256.New, []byte("wrong_secret"))
	badMac.Write(payload)
	badSig := "sha256=" + hex.EncodeToString(badMac.Sum(nil))
	if ch.VerifySignature(payload, badSig) {
		t.Fatal("expected VerifySignature to fail for incorrect secret")
	}

	// HTTP POST con firma valida
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(payload))
	req.Header.Set("X-Hub-Signature-256", validSig)
	rec := httptest.NewRecorder()
	ch.HandleWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on valid webhook post, got %d", rec.Code)
	}

	// HTTP POST con firma invalida
	reqInvalid := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(payload))
	reqInvalid.Header.Set("X-Hub-Signature-256", "sha256=abcdef123456")
	recInvalid := httptest.NewRecorder()
	ch.HandleWebhook(recInvalid, reqInvalid)
	if recInvalid.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized on invalid signature, got %d", recInvalid.Code)
	}
}

func TestWhatsAppCloud_MediaMessageParsing(t *testing.T) {
	appSecret := "sec123"
	ch := NewWhatsAppCloudChannel(WhatsAppCloudConfig{AppSecret: appSecret}, nil, nil)

	rawJSON := `{
		"object": "whatsapp_business_account",
		"entry": [{
			"changes": [{
				"value": {
					"messages": [
						{
							"id": "wamid.text1",
							"from": "573151212223",
							"timestamp": "1730178000",
							"type": "text",
							"text": {"body": "Hola, ¿tienen la Duster 2022?"}
						},
						{
							"id": "wamid.img1",
							"from": "573151212223",
							"timestamp": "1730178010",
							"type": "image",
							"image": {
								"id": "media_img_99",
								"mime_type": "image/jpeg",
								"sha256": "hash_img_abc",
								"caption": "Foto del carro que busco"
							}
						},
						{
							"id": "wamid.aud1",
							"from": "573151212223",
							"timestamp": "1730178020",
							"type": "audio",
							"audio": {
								"id": "media_aud_55",
								"mime_type": "audio/ogg",
								"voice": true
							}
						},
						{
							"id": "wamid.doc1",
							"from": "573151212223",
							"timestamp": "1730178030",
							"type": "document",
							"document": {
								"id": "media_doc_11",
								"mime_type": "application/pdf",
								"filename": "cedula_solicitante.pdf",
								"caption": "Adjunto documento"
							}
						}
					]
				}
			}]
		}]
	}`

	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write([]byte(rawJSON))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(rawJSON))
	req.Header.Set("X-Hub-Signature-256", sig)
	rec := httptest.NewRecorder()
	ch.HandleWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	ctx := context.Background()
	sess, err := ch.Open(ctx, Link{ID: "lnk1", URL: "https://buyacar.com.co", Active: true})
	if err != nil {
		t.Fatalf("unexpected open error: %v", err)
	}

	pending, err := ch.Pending(ctx, sess)
	if err != nil {
		t.Fatalf("unexpected pending error: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 conversation, got %d", len(pending))
	}
	if pending[0].ID != "573151212223" {
		t.Fatalf("expected conversation ID 573151212223, got %s", pending[0].ID)
	}

	thread, err := ch.Read(ctx, sess, pending[0])
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if len(thread.Messages) != 4 {
		t.Fatalf("expected 4 messages in thread, got %d", len(thread.Messages))
	}

	if thread.Messages[0].Text != "Hola, ¿tienen la Duster 2022?" {
		t.Errorf("unexpected message 0 text: %s", thread.Messages[0].Text)
	}
	if thread.Messages[1].Text != "Foto del carro que busco" {
		t.Errorf("unexpected message 1 text: %s", thread.Messages[1].Text)
	}
}

func TestWhatsAppCloud_OptInOptOut(t *testing.T) {
	mgr := NewMemoryOptInManager()

	customerID := "573100000000"
	if status := mgr.GetStatus(customerID); status != OptInUnknown {
		t.Fatalf("expected initial status unknown, got %s", status)
	}

	// Mensaje de baja / stop
	mgr.EvaluateConsent(customerID, "por favor STOP no quiero mas mensajes")
	if status := mgr.GetStatus(customerID); status != OptInRevoked {
		t.Fatalf("expected status revoked after STOP, got %s", status)
	}

	// Intentar envio a cliente revocado debe fallar
	ch := NewWhatsAppCloudChannel(WhatsAppCloudConfig{}, nil, nil)
	ch.optInManager.SetStatus(customerID, OptInRevoked)

	ctx := context.Background()
	sess := Session{ID: "sess1", ChannelID: WhatsAppCloudChannelID}
	conv := Conversation{ID: customerID, ChannelID: WhatsAppCloudChannelID}
	msg := Message{ID: "m1", Text: "Hola, tu vehiculo esta listo"}

	res, err := ch.Reply(ctx, sess, conv, msg, ModeSimulation)
	if err != ErrOptInRevoked {
		t.Fatalf("expected ErrOptInRevoked, got err=%v, res=%+v", err, res)
	}

	// Cliente vuelve a dar consentimiento con ALTA
	ch.optInManager.EvaluateConsent(customerID, "START deseo recibir informacion")
	if status := ch.optInManager.GetStatus(customerID); status != OptInGranted {
		t.Fatalf("expected status granted after START, got %s", status)
	}

	resGranted, errGranted := ch.Reply(ctx, sess, conv, msg, ModeSimulation)
	if errGranted != nil || resGranted.Status != SendSimulated {
		t.Fatalf("expected successful simulated send after opt-in grant, got %v, status=%s", errGranted, resGranted.Status)
	}
}

func TestWhatsAppCloud_HSMTemplateCatalog(t *testing.T) {
	catalog := NewHSMTemplateCatalog()

	tmpl, ok := catalog.Get("lead_followup_24h")
	if !ok {
		t.Fatal("expected lead_followup_24h template to be present")
	}
	if tmpl.Language != "es_CO" {
		t.Errorf("expected language es_CO, got %s", tmpl.Language)
	}

	rendered, err := catalog.Render("lead_followup_24h", "Carlos", "Renault Duster 2022")
	if err != nil {
		t.Fatalf("unexpected render error: %v", err)
	}
	if !strings.Contains(rendered, "Carlos") || !strings.Contains(rendered, "Renault Duster 2022") {
		t.Fatalf("expected rendered text with substitutions, got: %s", rendered)
	}

	_, errMissing := catalog.Render("template_inexistente", "param")
	if errMissing == nil {
		t.Fatal("expected error for non-existent template")
	}
}

func TestWhatsAppCloud_24HourWindowEnforcement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"messages": []map[string]string{{"id": "wamid.HBgL"}},
		})
	}))
	defer server.Close()

	ch := NewWhatsAppCloudChannel(WhatsAppCloudConfig{
		PhoneNumberID: "phone_123",
		AccessToken:   "token_abc",
		BaseURL:       server.URL,
	}, nil, nil)

	customerID := "573123456789"
	ctx := context.Background()
	sess := Session{ID: "sess1", ChannelID: WhatsAppCloudChannelID}
	conv := Conversation{ID: customerID, ChannelID: WhatsAppCloudChannelID}

	// Caso 1: Fuera de ventana de 24h con texto libre -> ErrHSMTemplateRequired
	res, err := ch.Reply(ctx, sess, conv, Message{ID: "m1", Text: "Hola, sigues ahi?"}, ModeAutopilot)
	if err != ErrHSMTemplateRequired {
		t.Fatalf("expected ErrHSMTemplateRequired for >24h window, got err=%v, res=%+v", err, res)
	}

	// Caso 2: Fuera de ventana pero usando plantilla HSM registrada -> Envio exitoso
	resHSM, errHSM := ch.Reply(ctx, sess, conv, Message{ID: "m2", Text: "HSM:lead_followup_24h"}, ModeAutopilot)
	if errHSM != nil || resHSM.Status != SendVerified {
		t.Fatalf("expected SendVerified using HSM outside window, got err=%v, res=%+v", errHSM, resHSM)
	}

	// Caso 3: Dentro de la ventana de 24h tras un inbound reciente
	ch.RecordInboundMessage(WhatsAppInboundMessage{
		ID:        "inbound1",
		From:      customerID,
		Timestamp: time.Now().UTC().Add(-2 * time.Hour), // 2 horas atras (< 24h)
		Type:      "text",
		Text:      "Precio del Onix?",
	})

	resInside, errInside := ch.Reply(ctx, sess, conv, Message{ID: "m3", Text: "El Chevrolet Onix cuesta $54.000.000"}, ModeAutopilot)
	if errInside != nil || resInside.Status != SendVerified {
		t.Fatalf("expected SendVerified inside 24h window, got err=%v, res=%+v", errInside, resInside)
	}
}

func TestWhatsAppCloud_OutboundModesAndRateResilience(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount <= 3 {
			// Simula saturacion de cuota 429
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"Rate limit exceeded","code":429}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"messages":[{"id":"wa-ok-123"}]}`))
	}))
	defer server.Close()

	escQueue := escalate.NewQueue()
	rt := NewResilienceTracker(escQueue)
	rc := NewRateController(RateConfig{
		MaxMessagesPerHour: 2,
		MaxMessagesPerDay:  10,
	})

	ch := NewWhatsAppCloudChannel(WhatsAppCloudConfig{
		PhoneNumberID: "phone_test",
		AccessToken:   "token_test",
		BaseURL:       server.URL,
	}, rt, rc)

	customerID := "573200000000"
	ctx := context.Background()
	sess := Session{ID: "s1", ChannelID: WhatsAppCloudChannelID}
	conv := Conversation{ID: customerID, ChannelID: WhatsAppCloudChannelID}

	// Activar ventana reciente
	ch.RecordInboundMessage(WhatsAppInboundMessage{
		ID:        "msg_init",
		From:      customerID,
		Timestamp: time.Now().UTC(),
		Text:      "start",
	})

	// 1. Validacion de presupuesto de caracteres (650 caracteres)
	longText := strings.Repeat("A", 651)
	_, errLong := ch.Reply(ctx, sess, conv, Message{ID: "long", Text: longText}, ModeSimulation)
	if errLong != ErrCharacterLimitExceeded {
		t.Fatalf("expected ErrCharacterLimitExceeded, got %v", errLong)
	}

	// 2. Modo SIMULATION
	resSim, errSim := ch.Reply(ctx, sess, conv, Message{ID: "sim1", Text: "Respuesta de simulacion 1"}, ModeSimulation)
	if errSim != nil || resSim.Status != SendSimulated {
		t.Fatalf("expected SendSimulated, got err=%v, res=%+v", errSim, resSim)
	}
	resSim2, errSim2 := ch.Reply(ctx, sess, conv, Message{ID: "sim2", Text: "Respuesta de simulacion 2"}, ModeSimulation)
	if errSim2 != nil || resSim2.Status != SendSimulated {
		t.Fatalf("expected SendSimulated for second message, got err=%v, res=%+v", errSim2, resSim2)
	}

	// 3. Modo DRAFT (no incrementa tasa saliente)
	resDraft, errDraft := ch.Reply(ctx, sess, conv, Message{ID: "d1", Text: "Borrador pendiente"}, ModeDraft)
	if errDraft != nil || resDraft.Status != SendDrafted {
		t.Fatalf("expected SendDrafted, got err=%v, res=%+v", errDraft, resDraft)
	}

	// 4. Rate Limit agotado (MaxPerHour = 2)
	resLimit, errLimit := ch.Reply(ctx, sess, conv, Message{ID: "d2", Text: "Excede tasa"}, ModeSimulation)
	if errLimit != ErrRateLimitExceeded {
		t.Fatalf("expected ErrRateLimitExceeded, got err=%v, res=%+v", errLimit, resLimit)
	}

	// Reset rate controller para probar resiliencia
	rc.Reset(WhatsAppCloudChannelID)

	// 5. 3 Fallos consecutivos en Autopilot disparan auto-pausa
	for i := 1; i <= 3; i++ {
		_, err429 := ch.Reply(ctx, sess, conv, Message{ID: "ap", Text: "Intento autopilot"}, ModeAutopilot)
		if err429 != ErrQuotaExceeded {
			t.Fatalf("attempt %d: expected ErrQuotaExceeded, got %v", i, err429)
		}
	}

	if !ch.IsPaused() {
		t.Fatal("expected WhatsApp Cloud channel to be auto-paused after 3 consecutive failures")
	}

	// Siguiente intento debe ser rechazado inmediatamente con ErrChannelPaused
	_, errPaused := ch.Reply(ctx, sess, conv, Message{ID: "ap4", Text: "Texto post pausa"}, ModeSimulation)
	if errPaused != ErrChannelPaused {
		t.Fatalf("expected ErrChannelPaused, got %v", errPaused)
	}

	// Verificar ticket en escalate.Queue
	tickets := escQueue.Pending()
	if len(tickets) == 0 {
		t.Fatal("expected ticket to be enqueued in escalate.Queue upon auto-pause")
	}
}
