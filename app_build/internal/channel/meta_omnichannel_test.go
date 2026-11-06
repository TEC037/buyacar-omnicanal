package channel

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"buyacar.com.co/omnicanal/internal/escalate"
)

func TestMetaOmnichannel_MultiplexingAndCharacterLimits(t *testing.T) {
	adapter := NewMetaOmnichannelAdapter(MetaOmnichannelConfig{
		PageID:      "page_111",
		InstagramID: "ig_222",
	}, nil, nil)

	messenger := adapter.MessengerSubChannel()
	instagram := adapter.InstagramSubChannel()

	if messenger.ID() != ChannelIDMessenger {
		t.Fatalf("expected ID %s, got %s", ChannelIDMessenger, messenger.ID())
	}
	if instagram.ID() != ChannelIDInstagram {
		t.Fatalf("expected ID %s, got %s", ChannelIDInstagram, instagram.ID())
	}

	// Limites de caracteres estrictos segun Mandamiento 2
	if messenger.Limits().MaxCharacterLength != 500 {
		t.Errorf("expected 500 max characters for Facebook Messenger, got %d", messenger.Limits().MaxCharacterLength)
	}
	if instagram.Limits().MaxCharacterLength != 450 {
		t.Errorf("expected 450 max characters for Instagram Direct, got %d", instagram.Limits().MaxCharacterLength)
	}

	ctx := context.Background()
	sessM := Session{ID: "s-m", ChannelID: ChannelIDMessenger}
	convM := Conversation{ID: "messenger-user1", ChannelID: ChannelIDMessenger}

	sessIG := Session{ID: "s-ig", ChannelID: ChannelIDInstagram}
	convIG := Conversation{ID: "instagram-user2", ChannelID: ChannelIDInstagram}

	// 501 caracteres en Messenger debe fallar
	overMessenger := strings.Repeat("M", 501)
	_, errM := messenger.Reply(ctx, sessM, convM, Message{ID: "m1", Text: overMessenger}, ModeSimulation)
	if errM != ErrCharacterLimitExceeded {
		t.Fatalf("expected ErrCharacterLimitExceeded on Messenger >500 chars, got %v", errM)
	}

	// 451 caracteres en Instagram debe fallar
	overIG := strings.Repeat("I", 451)
	_, errIG := instagram.Reply(ctx, sessIG, convIG, Message{ID: "m2", Text: overIG}, ModeSimulation)
	if errIG != ErrCharacterLimitExceeded {
		t.Fatalf("expected ErrCharacterLimitExceeded on Instagram >450 chars, got %v", errIG)
	}

	// 500 caracteres exactos en Messenger es valido
	validM := strings.Repeat("M", 500)
	resM, errValidM := messenger.Reply(ctx, sessM, convM, Message{ID: "m3", Text: validM}, ModeSimulation)
	if errValidM != nil || resM.Status != SendSimulated {
		t.Fatalf("expected SendSimulated for exact 500 chars on Messenger, got %v", errValidM)
	}

	// 450 caracteres exactos en Instagram es valido
	validIG := strings.Repeat("I", 450)
	resIG, errValidIG := instagram.Reply(ctx, sessIG, convIG, Message{ID: "m4", Text: validIG}, ModeSimulation)
	if errValidIG != nil || resIG.Status != SendSimulated {
		t.Fatalf("expected SendSimulated for exact 450 chars on Instagram, got %v", errValidIG)
	}
}

func TestMetaOmnichannel_WebhookVerificationHandshake(t *testing.T) {
	adapter := NewMetaOmnichannelAdapter(MetaOmnichannelConfig{
		VerifyToken: "token_meta_omnichannel",
	}, nil, nil)

	// Handshake valido
	req := httptest.NewRequest(http.MethodGet, "/webhook/meta?hub.mode=subscribe&hub.verify_token=token_meta_omnichannel&hub.challenge=test_meta_challenge", nil)
	rec := httptest.NewRecorder()
	adapter.HandleWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	if rec.Body.String() != "test_meta_challenge" {
		t.Fatalf("expected challenge 'test_meta_challenge', got %s", rec.Body.String())
	}

	// Token incorrecto
	reqBad := httptest.NewRequest(http.MethodGet, "/webhook/meta?hub.mode=subscribe&hub.verify_token=wrong&hub.challenge=test", nil)
	recBad := httptest.NewRecorder()
	adapter.HandleWebhook(recBad, reqBad)
	if recBad.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden, got %d", recBad.Code)
	}
}

func TestMetaOmnichannel_WebhookSignatureVerification(t *testing.T) {
	appSecret := "app_sec_meta_54321"
	adapter := NewMetaOmnichannelAdapter(MetaOmnichannelConfig{
		AppSecret: appSecret,
	}, nil, nil)

	payload := []byte(`{"object":"page","entry":[]}`)

	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(payload)
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !adapter.VerifySignature(payload, validSig) {
		t.Fatal("expected valid signature to pass")
	}

	tampered := []byte(`{"object":"page","entry":[{"id":"x"}]}`)
	if adapter.VerifySignature(tampered, validSig) {
		t.Fatal("expected tampered payload to fail")
	}

	req := httptest.NewRequest(http.MethodPost, "/webhook/meta", bytes.NewReader(payload))
	req.Header.Set("X-Hub-Signature-256", validSig)
	rec := httptest.NewRecorder()
	adapter.HandleWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on valid signature, got %d", rec.Code)
	}

	reqBad := httptest.NewRequest(http.MethodPost, "/webhook/meta", bytes.NewReader(payload))
	reqBad.Header.Set("X-Hub-Signature-256", "sha256=11223344")
	recBad := httptest.NewRecorder()
	adapter.HandleWebhook(recBad, reqBad)

	if recBad.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized on invalid signature, got %d", recBad.Code)
	}
}

func TestMetaOmnichannel_SharedTriage(t *testing.T) {
	appSecret := "secret_triage"
	pageID := "page_official_1"
	adapter := NewMetaOmnichannelAdapter(MetaOmnichannelConfig{
		PageID:      pageID,
		InstagramID: "ig_official_2",
		AppSecret:   appSecret,
	}, nil, nil)

	// JSON con mezcla de eventos: echo, delivery receipt, read receipt, spam vacio, mensaje repetido y mensaje valido
	jsonPayload := `{
		"object": "page",
		"entry": [{
			"id": "entry_1",
			"time": 1730179500000,
			"messaging": [
				{
					"sender": {"id": "page_official_1"},
					"recipient": {"id": "customer_1"},
					"timestamp": 1730179500000,
					"message": {"mid": "mid.echo1", "text": "Respuesta enviada", "is_echo": true}
				},
				{
					"sender": {"id": "customer_1"},
					"recipient": {"id": "page_official_1"},
					"timestamp": 1730179500010,
					"delivery": {"mids": ["mid.del1"]}
				},
				{
					"sender": {"id": "customer_1"},
					"recipient": {"id": "page_official_1"},
					"timestamp": 1730179500020,
					"read": {"watermark": 1730179500000}
				},
				{
					"sender": {"id": "customer_1"},
					"recipient": {"id": "page_official_1"},
					"timestamp": 1730179500030,
					"message": {"mid": "mid.empty", "text": "   "}
				},
				{
					"sender": {"id": "customer_1"},
					"recipient": {"id": "page_official_1"},
					"timestamp": 1730179500040,
					"message": {"mid": "mid.valid1", "text": "Hola, ¿que kilometraje tiene el Sandero?"}
				},
				{
					"sender": {"id": "customer_1"},
					"recipient": {"id": "page_official_1"},
					"timestamp": 1730179500050,
					"message": {"mid": "mid.valid1", "text": "Hola, ¿que kilometraje tiene el Sandero?"}
				}
			]
		}]
	}`

	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write([]byte(jsonPayload))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/webhook/meta", strings.NewReader(jsonPayload))
	req.Header.Set("X-Hub-Signature-256", sig)
	rec := httptest.NewRecorder()
	adapter.HandleWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	// Metricas de triage
	dropped, processed := adapter.TriageMetrics()
	// Eventos esperados descartados (CP2):
	// 1. is_echo / sender == pageID
	// 2. delivery receipt
	// 3. read receipt
	// 4. empty text
	// 5. duplicate mid.valid1
	// Total descartados = 5. Total procesados = 1.
	if dropped != 5 {
		t.Fatalf("expected 5 dropped events in triage, got %d", dropped)
	}
	if processed != 1 {
		t.Fatalf("expected 1 valid processed event in triage, got %d", processed)
	}

	// Verificar hilo en el buzon de Messenger
	messenger := adapter.MessengerSubChannel()
	ctx := context.Background()
	sess, err := messenger.Open(ctx, Link{ID: "lnk_m", Active: true})
	if err != nil {
		t.Fatalf("unexpected open error: %v", err)
	}

	pending, err := messenger.Pending(ctx, sess)
	if err != nil {
		t.Fatalf("unexpected pending error: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 conversation, got %d", len(pending))
	}

	thread, err := messenger.Read(ctx, sess, pending[0])
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if len(thread.Messages) != 1 {
		t.Fatalf("expected 1 message in thread, got %d", len(thread.Messages))
	}
	if thread.Messages[0].Text != "Hola, ¿que kilometraje tiene el Sandero?" {
		t.Errorf("unexpected thread message: %s", thread.Messages[0].Text)
	}
}

func TestMetaOmnichannel_ModesAndRateResilience(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"Rate limit reached","code":429}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"recipient_id":"cust_1","message_id":"mid.resp"}`))
	}))
	defer server.Close()

	escQueue := escalate.NewQueue()
	rt := NewResilienceTracker(escQueue)
	rc := NewRateController(RateConfig{MaxMessagesPerHour: 2, MaxMessagesPerDay: 5})

	adapter := NewMetaOmnichannelAdapter(MetaOmnichannelConfig{
		PageID:      "page_test",
		AccessToken: "token_test",
		BaseURL:     server.URL,
	}, rt, rc)

	messenger := adapter.MessengerSubChannel()
	instagram := adapter.InstagramSubChannel()

	ctx := context.Background()
	sessM := Session{ID: "s-m", ChannelID: ChannelIDMessenger}
	convM := Conversation{ID: "messenger-c1", ChannelID: ChannelIDMessenger}

	// 1. SIMULATION
	res1, err1 := messenger.Reply(ctx, sessM, convM, Message{ID: "m1", Text: "Simulado 1"}, ModeSimulation)
	if err1 != nil || res1.Status != SendSimulated {
		t.Fatalf("expected SendSimulated, got err=%v, res=%+v", err1, res1)
	}
	res2, err2 := messenger.Reply(ctx, sessM, convM, Message{ID: "m2", Text: "Simulado 2"}, ModeSimulation)
	if err2 != nil || res2.Status != SendSimulated {
		t.Fatalf("expected SendSimulated, got err=%v, res=%+v", err2, res2)
	}

	// 2. Limite de tasa agotado en Messenger (MaxPerHour=2)
	_, errLimit := messenger.Reply(ctx, sessM, convM, Message{ID: "m3", Text: "Simulado 3"}, ModeSimulation)
	if errLimit != ErrRateLimitExceeded {
		t.Fatalf("expected ErrRateLimitExceeded, got %v", errLimit)
	}

	rc.Reset(ChannelIDMessenger)

	// 3. 3 Fallos consecutivos en Messenger provocan auto-pausa
	for i := 1; i <= 3; i++ {
		_, errAP := messenger.Reply(ctx, sessM, convM, Message{ID: "ap", Text: "Autopilot test"}, ModeAutopilot)
		if errAP != ErrQuotaExceeded {
			t.Fatalf("attempt %d: expected ErrQuotaExceeded, got %v", i, errAP)
		}
	}

	if !adapter.IsPaused(ChannelIDMessenger) {
		t.Fatal("expected Messenger to be auto-paused after 3 failures")
	}

	// 4. Aislamiento de fallos: Instagram debe seguir saludable y no estar pausado
	if adapter.IsPaused(ChannelIDInstagram) {
		t.Fatal("expected Instagram to remain healthy despite Messenger pause")
	}

	sessIG := Session{ID: "s-ig", ChannelID: ChannelIDInstagram}
	convIG := Conversation{ID: "instagram-c2", ChannelID: ChannelIDInstagram}

	resIG, errIG := instagram.Reply(ctx, sessIG, convIG, Message{ID: "ig1", Text: "Hola IG"}, ModeSimulation)
	if errIG != nil || resIG.Status != SendSimulated {
		t.Fatalf("expected Instagram send to succeed independently, got err=%v, res=%+v", errIG, resIG)
	}

	// Escalamiento registrado para Messenger
	tickets := escQueue.Pending()
	if len(tickets) == 0 {
		t.Fatal("expected escalation ticket in escalate.Queue for auto-paused Messenger")
	}
}

func TestMetaOmnichannel_FIFOBacklogSorting(t *testing.T) {
	adapter := NewMetaOmnichannelAdapter(MetaOmnichannelConfig{
		PageID:      "page_fifo",
		VerifyToken: "token_fifo",
		AppSecret:   "secret_fifo",
	}, nil, nil)

	var receivedOrder []string
	var wg sync.WaitGroup
	wg.Add(3)

	adapter.SetOnInboundConversation(func(channelID string, conv Conversation) {
		receivedOrder = append(receivedOrder, conv.CustomerAlias)
		wg.Done()
	})

	// Enviamos webhook payload con 3 mensajes fuera de orden temporal (t3, t1, t2)
	// El procesador FIFO debe despacharlos en orden ascendente (t1, t2, t3)
	payload := `{
		"object": "page",
		"entry": [
			{
				"id": "page_fifo",
				"time": 3000,
				"messaging": [
					{
						"sender": {"id": "cliente_t3"},
						"recipient": {"id": "page_fifo"},
						"timestamp": 3000,
						"message": {"mid": "mid_3", "text": "Mensaje 3 (más nuevo)"}
					},
					{
						"sender": {"id": "cliente_t1"},
						"recipient": {"id": "page_fifo"},
						"timestamp": 1000,
						"message": {"mid": "mid_1", "text": "Mensaje 1 (más antiguo)"}
					},
					{
						"sender": {"id": "cliente_t2"},
						"recipient": {"id": "page_fifo"},
						"timestamp": 2000,
						"message": {"mid": "mid_2", "text": "Mensaje 2 (intermedio)"}
					}
				]
			}
		]
	}`

	mac := hmac.New(sha256.New, []byte("secret_fifo"))
	mac.Write([]byte(payload))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/webhooks/meta", bytes.NewReader([]byte(payload)))
	req.Header.Set("X-Hub-Signature-256", sig)
	w := httptest.NewRecorder()

	adapter.HandleWebhook(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	wg.Wait()

	if len(receivedOrder) != 3 {
		t.Fatalf("expected 3 processed messages, got %d", len(receivedOrder))
	}
	if receivedOrder[0] != "cliente_t1" || receivedOrder[1] != "cliente_t2" || receivedOrder[2] != "cliente_t3" {
		t.Fatalf("expected FIFO order [cliente_t1, cliente_t2, cliente_t3], got %+v", receivedOrder)
	}
}
