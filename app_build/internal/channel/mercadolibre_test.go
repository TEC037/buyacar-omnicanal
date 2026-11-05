package channel

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"buyacar.com.co/omnicanal/internal/escalate"
)

func TestMercadoLibre_LimitsAndIdentity(t *testing.T) {
	ch := NewMercadoLibreChannel(MercadoLibreConfig{
		AppID:     "meli_app_1",
		SecretKey: "secret_meli_key",
	}, nil, nil)

	if ch.ID() != MercadoLibreChannelID {
		t.Fatalf("expected ID %s, got %s", MercadoLibreChannelID, ch.ID())
	}
	limits := ch.Limits()
	if limits.MaxCharacterLength != 2000 {
		t.Errorf("expected 2000 max characters, got %d", limits.MaxCharacterLength)
	}
	if limits.MaxPerHour != 120 || limits.MaxPerDay != 250 {
		t.Errorf("expected 120/h and 250/d, got %d/h and %d/d", limits.MaxPerHour, limits.MaxPerDay)
	}
}

func TestMercadoLibre_WebhookSignatureVerification(t *testing.T) {
	secret := "meli_secret_token_abcdef"
	ch := NewMercadoLibreChannel(MercadoLibreConfig{
		SecretKey: secret,
	}, nil, nil)

	payload := []byte(`{"resource":"/questions/12345","topic":"questions","user_id":555}`)

	// Formato 1: Simple sha256 hex
	mac1 := hmac.New(sha256.New, []byte(secret))
	mac1.Write(payload)
	sig1 := hex.EncodeToString(mac1.Sum(nil))

	if !ch.VerifySignature(payload, sig1) {
		t.Fatal("expected simple hex signature to verify successfully")
	}

	// Formato 2: X-Signature estructurado con ts y v1
	ts := "1730179000"
	mac2 := hmac.New(sha256.New, []byte(secret))
	mac2.Write([]byte(ts))
	mac2.Write([]byte(";"))
	mac2.Write(payload)
	v1Hash := hex.EncodeToString(mac2.Sum(nil))
	sig2 := fmt.Sprintf("ts=%s,v1=%s", ts, v1Hash)

	if !ch.VerifySignature(payload, sig2) {
		t.Fatal("expected structured ts/v1 signature to verify successfully")
	}

	// Firma invalida
	if ch.VerifySignature(payload, "invalid_signature_hash") {
		t.Fatal("expected invalid signature to fail")
	}

	// HTTP POST con firma valida
	req := httptest.NewRequest(http.MethodPost, "/webhook/meli", bytes.NewReader(payload))
	req.Header.Set("X-Signature", sig2)
	rec := httptest.NewRecorder()
	ch.HandleWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on valid MELI webhook, got %d: %s", rec.Code, rec.Body.String())
	}

	// HTTP POST con firma no autorizada
	reqBad := httptest.NewRequest(http.MethodPost, "/webhook/meli", bytes.NewReader(payload))
	reqBad.Header.Set("X-Signature", "ts=123,v1=badhash")
	recBad := httptest.NewRecorder()
	ch.HandleWebhook(recBad, reqBad)

	if recBad.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized on invalid signature, got %d", recBad.Code)
	}
}

func TestMercadoLibre_TopicRouting(t *testing.T) {
	secret := "secret123"
	ch := NewMercadoLibreChannel(MercadoLibreConfig{SecretKey: secret}, nil, nil)

	// Ingesta de pregunta
	qJSON := []byte(`{"resource":"/questions/998877","topic":"questions","user_id":4433,"received":"2026-10-29T05:00:00Z"}`)
	macQ := hmac.New(sha256.New, []byte(secret))
	macQ.Write(qJSON)
	sigQ := hex.EncodeToString(macQ.Sum(nil))

	reqQ := httptest.NewRequest(http.MethodPost, "/webhook/meli", bytes.NewReader(qJSON))
	reqQ.Header.Set("X-Signature", sigQ)
	recQ := httptest.NewRecorder()
	ch.HandleWebhook(recQ, reqQ)
	if recQ.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for question notification, got %d", recQ.Code)
	}

	// Ingesta de mensaje post-venta
	mJSON := []byte(`{"resource":"/messages/packs/776655/sellers/11","topic":"messages","user_id":8899,"received":"2026-10-29T05:05:00Z"}`)
	macM := hmac.New(sha256.New, []byte(secret))
	macM.Write(mJSON)
	sigM := hex.EncodeToString(macM.Sum(nil))

	reqM := httptest.NewRequest(http.MethodPost, "/webhook/meli", bytes.NewReader(mJSON))
	reqM.Header.Set("X-Signature", sigM)
	recM := httptest.NewRecorder()
	ch.HandleWebhook(recM, reqM)
	if recM.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for post-sale message notification, got %d", recM.Code)
	}

	ctx := context.Background()
	sess, err := ch.Open(ctx, Link{ID: "lnk_meli", Active: true})
	if err != nil {
		t.Fatalf("unexpected open error: %v", err)
	}

	pending, err := ch.Pending(ctx, sess)
	if err != nil {
		t.Fatalf("unexpected pending error: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 conversations routed, got %d", len(pending))
	}

	// Verificar clasificacion de topicos
	topicQ := ch.GetConversationTopic("meli-q-998877")
	if topicQ != TopicQuestions {
		t.Fatalf("expected topic %s, got %s", TopicQuestions, topicQ)
	}
	topicM := ch.GetConversationTopic("meli-m-776655")
	if topicM != TopicMessages {
		t.Fatalf("expected topic %s, got %s", TopicMessages, topicM)
	}
}

func TestMercadoLibre_PreSaleGuardrailPolicy(t *testing.T) {
	// 1. Caso con telefonos, correos y URLs
	dirtyText := "Hola amigo, llámame al 3151212223 o escríbeme a asesor@buyacar.com.co y visita https://buyacar.com.co"
	sanitized, modified := SanitizePreSaleReply(dirtyText)

	if !modified {
		t.Fatal("expected modified=true for pre-sale text with prohibited contact info")
	}
	if strings.Contains(sanitized, "3151212223") {
		t.Errorf("phone number was not stripped: %s", sanitized)
	}
	if strings.Contains(sanitized, "asesor@buyacar.com.co") {
		t.Errorf("email was not stripped: %s", sanitized)
	}
	if strings.Contains(sanitized, "https://buyacar.com.co") {
		t.Errorf("url was not stripped: %s", sanitized)
	}
	if !strings.Contains(sanitized, ContactRedactionPlaceholder) {
		t.Errorf("expected placeholder %s in sanitized text: %s", ContactRedactionPlaceholder, sanitized)
	}

	// 2. Caso con texto limpio y seguro de preventa
	cleanText := "Buenas tardes. El Renault Sandero 2021 cuenta con 45.000 km, único dueño y peritaje 100% aprobado."
	sanitizedClean, modifiedClean := SanitizePreSaleReply(cleanText)
	if modifiedClean {
		t.Fatalf("expected modified=false for clean text, got: %s", sanitizedClean)
	}
	if sanitizedClean != cleanText {
		t.Fatalf("expected identical clean text, got: %s", sanitizedClean)
	}

	// 3. Verificacion en Reply: en pregunta publica se purga; en post-venta privada no se purga
	ch := NewMercadoLibreChannel(MercadoLibreConfig{}, nil, nil)
	ctx := context.Background()
	sess := Session{ID: "s1", ChannelID: MercadoLibreChannelID}

	// Pregunta publica
	ch.IngestQuestion(MercadoLibreQuestion{
		ID:          "100",
		Text:        "¿Esta disponible?",
		DateCreated: time.Now().UTC(),
	})
	convQ := Conversation{ID: "meli-q-100", ChannelID: MercadoLibreChannelID}
	replyMsg := Message{ID: "r1", Text: "Si, disponible. Escribeme al 3022866969"}

	resDraftQ, errQ := ch.Reply(ctx, sess, convQ, replyMsg, ModeDraft)
	if errQ != nil || resDraftQ.Status != SendDrafted {
		t.Fatalf("expected SendDrafted, got err=%v, res=%+v", errQ, resDraftQ)
	}

	// Verificar que el borrador guardado fue sanitizado
	if len(ch.draftOutbox) != 1 {
		t.Fatalf("expected 1 draft in outbox, got %d", len(ch.draftOutbox))
	}
	if !ch.draftOutbox[0].Sanitized || strings.Contains(ch.draftOutbox[0].Text, "3022866969") {
		t.Fatalf("expected draft to be sanitized, got text: %s", ch.draftOutbox[0].Text)
	}

	// Mensaje privado post-venta
	ch.IngestPostSaleMessage("pack-900", 1234, "Mensaje comprador", time.Now().UTC())
	convM := Conversation{ID: "meli-m-pack-900", ChannelID: MercadoLibreChannelID}
	replyPostSale := Message{ID: "r2", Text: "Para entrega coordinar al 3160502717 o visitar https://buyacar.com.co"}

	resDraftM, errM := ch.Reply(ctx, sess, convM, replyPostSale, ModeDraft)
	if errM != nil || resDraftM.Status != SendDrafted {
		t.Fatalf("expected SendDrafted for post-sale, got err=%v, res=%+v", errM, resDraftM)
	}
	if ch.draftOutbox[1].Sanitized {
		t.Fatalf("post-sale messages should not be stripped by pre-sale guardrail")
	}
}

func TestMercadoLibre_ModesAndRateResilience(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message":"Too many requests","status":429}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success"}`))
	}))
	defer server.Close()

	escQueue := escalate.NewQueue()
	rt := NewResilienceTracker(escQueue)
	rc := NewRateController(RateConfig{MaxMessagesPerHour: 2, MaxMessagesPerDay: 5})

	ch := NewMercadoLibreChannel(MercadoLibreConfig{
		AppID:       "app_test",
		AccessToken: "token_test",
		BaseURL:     server.URL,
	}, rt, rc)

	ctx := context.Background()
	sess := Session{ID: "s1", ChannelID: MercadoLibreChannelID}
	conv := Conversation{ID: "meli-q-777", ChannelID: MercadoLibreChannelID}

	// 1. SIMULATION
	res1, err1 := ch.Reply(ctx, sess, conv, Message{ID: "m1", Text: "Respuesta 1"}, ModeSimulation)
	if err1 != nil || res1.Status != SendSimulated {
		t.Fatalf("expected SendSimulated, got err=%v, res=%+v", err1, res1)
	}

	res2, err2 := ch.Reply(ctx, sess, conv, Message{ID: "m2", Text: "Respuesta 2"}, ModeSimulation)
	if err2 != nil || res2.Status != SendSimulated {
		t.Fatalf("expected SendSimulated, got err=%v, res=%+v", err2, res2)
	}

	// 2. Rate limit agotado (MaxPerHour=2)
	_, errLimit := ch.Reply(ctx, sess, conv, Message{ID: "m3", Text: "Respuesta 3"}, ModeSimulation)
	if errLimit != ErrRateLimitExceeded {
		t.Fatalf("expected ErrRateLimitExceeded, got %v", errLimit)
	}

	// Reset de tasa para evaluar resiliencia
	rc.Reset(MercadoLibreChannelID)

	// 3. Fallos consecutivos en Autopilot disparan auto-pausa
	for i := 1; i <= 3; i++ {
		_, errAP := ch.Reply(ctx, sess, conv, Message{ID: "ap", Text: "Intento Autopilot"}, ModeAutopilot)
		if errAP != ErrQuotaExceeded {
			t.Fatalf("attempt %d: expected ErrQuotaExceeded, got %v", i, errAP)
		}
	}

	if !ch.IsPaused() {
		t.Fatal("expected MercadoLibre channel to be auto-paused after 3 consecutive failures")
	}

	// Operaciones posteriores deben ser rechazadas con ErrChannelPaused
	_, errPaused := ch.Reply(ctx, sess, conv, Message{ID: "post", Text: "Post pause"}, ModeSimulation)
	if errPaused != ErrChannelPaused {
		t.Fatalf("expected ErrChannelPaused, got %v", errPaused)
	}

	// Escalamiento registrado
	if len(escQueue.Pending()) == 0 {
		t.Fatal("expected escalation ticket in escalate.Queue")
	}
}
