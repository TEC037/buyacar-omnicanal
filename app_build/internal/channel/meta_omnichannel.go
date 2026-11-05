package channel

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// ChannelIDMessenger es el identificador del canal Facebook Messenger.
	ChannelIDMessenger = "messenger"
	// ChannelIDFacebookAlias es el alias compatible con configuraciones existentes.
	ChannelIDFacebookAlias = "facebook_marketplace"
	// ChannelIDInstagram es el identificador del canal Instagram Direct.
	ChannelIDInstagram = "instagram"
	// MessengerMaxCharacters es el presupuesto de caracteres para Facebook Messenger (500 chars - Mandamiento 2).
	MessengerMaxCharacters = 500
	// InstagramMaxCharacters es el presupuesto de caracteres para Instagram Direct (450 chars - Mandamiento 2).
	InstagramMaxCharacters = 450
	// DefaultMetaGraphBaseURL es la URL base de Meta Graph API.
	DefaultMetaGraphBaseURL = "https://graph.facebook.com/v21.0"
)

// MetaTriageResult encapsula la decision del filtro de descarte temprano sin consumo de memoria/tokens.
type MetaTriageResult struct {
	ShouldProcess bool
	DropReason    string
	Platform      string
	SenderID      string
	MessageID     string
	Text          string
	Timestamp     time.Time
}

// MetaDraftEntry almacena un borrador generado para revision humana.
type MetaDraftEntry struct {
	DraftID        string
	ChannelID      string
	ConversationID string
	Text           string
	CreatedAt      time.Time
}

// MetaOmnichannelConfig contiene las credenciales y llaves para Messenger e Instagram Direct.
type MetaOmnichannelConfig struct {
	PageID      string
	InstagramID string
	AccessToken string
	VerifyToken string
	AppSecret   string
	BaseURL     string
	HTTPClient  *http.Client
}

// MetaOmnichannelAdapter multiplexa Facebook Messenger e Instagram Direct compartiendo triage y rate limiters.
type MetaOmnichannelAdapter struct {
	mu       sync.RWMutex
	config   MetaOmnichannelConfig
	tracker  ResilienceTracker
	rateCtrl RateController

	conversations map[string][]Conversation // ChannelID -> Conversations
	threads       map[string]Thread         // ConversationID -> Thread
	draftOutbox   []MetaDraftEntry
	seenMIDs      map[string]bool
	triagedDrops  int
	processedMsgs int
	paused        map[string]bool

	onInbound func(channelID string, conv Conversation)
}

// SetOnInboundConversation configura un callback disparado automáticamente cuando el webhook ingesta un mensaje válido.
func (a *MetaOmnichannelAdapter) SetOnInboundConversation(fn func(channelID string, conv Conversation)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onInbound = fn
}

// UpdateConfig actualiza en caliente las credenciales y URL base de Meta Graph API de forma thread-safe.
func (a *MetaOmnichannelAdapter) UpdateConfig(pageID, accessToken, verifyToken, appSecret, baseURL string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if strings.TrimSpace(pageID) != "" {
		a.config.PageID = strings.TrimSpace(pageID)
	}
	if strings.TrimSpace(accessToken) != "" {
		a.config.AccessToken = strings.TrimSpace(accessToken)
	}
	if strings.TrimSpace(verifyToken) != "" {
		a.config.VerifyToken = strings.TrimSpace(verifyToken)
	}
	if strings.TrimSpace(appSecret) != "" {
		a.config.AppSecret = strings.TrimSpace(appSecret)
	}
	if strings.TrimSpace(baseURL) != "" {
		a.config.BaseURL = strings.TrimSpace(baseURL)
	}
}

// ConfigSnapshot retorna una copia segura de la configuración actual de Meta.
func (a *MetaOmnichannelAdapter) ConfigSnapshot() MetaOmnichannelConfig {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.config
}

// NewMetaOmnichannelAdapter inicializa el adaptador omnicanal de Meta.
func NewMetaOmnichannelAdapter(cfg MetaOmnichannelConfig, rt ResilienceTracker, rc RateController) *MetaOmnichannelAdapter {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultMetaGraphBaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if rt == nil {
		rt = NewResilienceTracker(nil)
	}
	if rc == nil {
		rc = NewRateController(DefaultRateConfig())
	}

	return &MetaOmnichannelAdapter{
		config:        cfg,
		tracker:       rt,
		rateCtrl:      rc,
		conversations: make(map[string][]Conversation),
		threads:       make(map[string]Thread),
		draftOutbox:   make([]MetaDraftEntry, 0),
		seenMIDs:      make(map[string]bool),
		paused:        make(map[string]bool),
	}
}

// ResilienceTracker retorna el tracker de resiliencia asociado.
func (a *MetaOmnichannelAdapter) ResilienceTracker() ResilienceTracker {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.tracker
}

// RateController retorna el controlador de tasa asociado.
func (a *MetaOmnichannelAdapter) RateController() RateController {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.rateCtrl
}

// IsPaused verifica el estado de pausa para una plataforma especifica.
func (a *MetaOmnichannelAdapter) IsPaused(channelID string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.paused[channelID] {
		return true
	}
	if a.tracker != nil && a.tracker.IsPaused(channelID) {
		return true
	}
	return false
}

// SetPaused fija el estado de pausa de una plataforma.
func (a *MetaOmnichannelAdapter) SetPaused(channelID string, paused bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.paused[channelID] = paused
	if a.tracker != nil {
		a.tracker.SetPaused(channelID, paused)
	}
}

// TriageMetrics retorna estadisticas de triage (mensajes descartados vs procesados).
func (a *MetaOmnichannelAdapter) TriageMetrics() (dropped int, processed int) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.triagedDrops, a.processedMsgs
}

// LimitsForChannel retorna los limites oficiales segun la plataforma.
func (a *MetaOmnichannelAdapter) LimitsForChannel(channelID string) Limits {
	norm := strings.ToLower(channelID)
	if norm == ChannelIDInstagram {
		return Limits{
			MaxCharacterLength: InstagramMaxCharacters,
			MaxPerHour:         120,
			MaxPerDay:          250,
		}
	}
	// Por defecto presupone Facebook Messenger (500 chars)
	return Limits{
		MaxCharacterLength: MessengerMaxCharacters,
		MaxPerHour:         120,
		MaxPerDay:          250,
	}
}

// TriageInboundEvent filtra eventos del webhook descartando echos, receipts, mensajes repetidos y spam (CP2).
func (a *MetaOmnichannelAdapter) TriageInboundEvent(platform, senderID, recipientID, mid, text string, isEcho, isDelivery, isRead bool, ts time.Time) MetaTriageResult {
	a.mu.Lock()
	defer a.mu.Unlock()

	// 1. Descarte de echos (mensajes emitidos por la pagina/bot)
	if isEcho || senderID == a.config.PageID || (a.config.InstagramID != "" && senderID == a.config.InstagramID) {
		a.triagedDrops++
		return MetaTriageResult{ShouldProcess: false, DropReason: "echo_or_own_message"}
	}

	// 2. Descarte de recibos de entrega o lectura
	if isDelivery || isRead {
		a.triagedDrops++
		return MetaTriageResult{ShouldProcess: false, DropReason: "delivery_or_read_receipt"}
	}

	// 3. Descarte de mensajes vacios o spam sin texto
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		a.triagedDrops++
		return MetaTriageResult{ShouldProcess: false, DropReason: "empty_text_or_system_event"}
	}

	// 4. Deduplicacion de mensajes por MID
	if mid != "" {
		if a.seenMIDs[mid] {
			a.triagedDrops++
			return MetaTriageResult{ShouldProcess: false, DropReason: "duplicate_mid"}
		}
		a.seenMIDs[mid] = true
	}

	a.processedMsgs++
	return MetaTriageResult{
		ShouldProcess: true,
		Platform:      platform,
		SenderID:      senderID,
		MessageID:     mid,
		Text:          trimmed,
		Timestamp:     ts,
	}
}

// IngestTriageResult asienta una conversacion en el buzon correspondiente.
func (a *MetaOmnichannelAdapter) IngestTriageResult(tr MetaTriageResult) {
	if !tr.ShouldProcess {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	chID := ChannelIDMessenger
	if tr.Platform == "instagram" || tr.Platform == ChannelIDInstagram {
		chID = ChannelIDInstagram
	}

	convID := fmt.Sprintf("%s-%s", chID, tr.SenderID)
	conv := Conversation{
		ID:            convID,
		ChannelID:     chID,
		CustomerAlias: tr.SenderID,
		UpdatedAt:     tr.Timestamp,
	}

	convs := a.conversations[chID]
	exists := false
	for i, c := range convs {
		if c.ID == convID {
			convs[i].UpdatedAt = tr.Timestamp
			exists = true
			break
		}
	}
	if !exists {
		a.conversations[chID] = append(convs, conv)
	}

	msg := Message{
		ID:        tr.MessageID,
		Sender:    "customer",
		Text:      tr.Text,
		Timestamp: tr.Timestamp,
	}

	th, ok := a.threads[convID]
	if !ok {
		th = Thread{
			Conversation: conv,
			Messages:     []Message{msg},
		}
	} else {
		th.Messages = append(th.Messages, msg)
	}
	a.threads[convID] = th
}

// VerifySignature verifica la firma criptografica HMAC-SHA256 del webhook de Meta.
func (a *MetaOmnichannelAdapter) VerifySignature(payload []byte, signature string) bool {
	if a.config.AppSecret == "" || signature == "" {
		return false
	}
	const prefix = "sha256="
	if !strings.HasPrefix(signature, prefix) {
		return false
	}
	expectedHashHex := strings.TrimPrefix(signature, prefix)
	expectedMAC, err := hex.DecodeString(expectedHashHex)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(a.config.AppSecret))
	mac.Write(payload)
	actualMAC := mac.Sum(nil)

	return hmac.Equal(actualMAC, expectedMAC)
}

// HandleWebhook procesa las notificaciones unificadas de Messenger e Instagram Direct.
func (a *MetaOmnichannelAdapter) HandleWebhook(rw http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// Webhook verification handshake
		query := r.URL.Query()
		mode := query.Get("hub.mode")
		token := query.Get("hub.verify_token")
		challenge := query.Get("hub.challenge")

		if mode == "subscribe" && token == a.config.VerifyToken {
			rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
			rw.WriteHeader(http.StatusOK)
			_, _ = rw.Write([]byte(challenge))
			return
		}
		http.Error(rw, "forbidden", http.StatusForbidden)
		return

	case http.MethodPost:
		// Ingesta de notificaciones webhook
		r.Body = http.MaxBytesReader(rw, r.Body, MaxWebhookBodyBytes)
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(rw, "payload too large or read error", http.StatusBadRequest)
			return
		}

		sig := r.Header.Get("X-Hub-Signature-256")
		if !a.VerifySignature(payload, sig) {
			http.Error(rw, "invalid signature", http.StatusUnauthorized)
			return
		}

		var metaPayload struct {
			Object string `json:"object"` // "page" o "instagram"
			Entry  []struct {
				ID        string `json:"id"`
				Time      int64  `json:"time"`
				Messaging []struct {
					Sender struct {
						ID string `json:"id"`
					} `json:"sender"`
					Recipient struct {
						ID string `json:"id"`
					} `json:"recipient"`
					Timestamp int64 `json:"timestamp"`
					Message   struct {
						MID      string `json:"mid"`
						Text     string `json:"text"`
						IsEcho   bool   `json:"is_echo"`
						Referral struct {
							AdTitle string `json:"ad_title"`
							Ref     string `json:"ref"`
						} `json:"referral"`
					} `json:"message"`
					Referral struct {
						AdTitle string `json:"ad_title"`
						Ref     string `json:"ref"`
					} `json:"referral"`
					Delivery struct {
						MIDs []string `json:"mids"`
					} `json:"delivery"`
					Read struct {
						Watermark int64 `json:"watermark"`
					} `json:"read"`
				} `json:"messaging"`
			} `json:"entry"`
		}

		if err := json.Unmarshal(payload, &metaPayload); err != nil {
			http.Error(rw, "invalid json payload", http.StatusBadRequest)
			return
		}

		platform := "messenger"
		if metaPayload.Object == "instagram" {
			platform = "instagram"
		}

		for _, entry := range metaPayload.Entry {
			for _, m := range entry.Messaging {
				ts := time.Now().UTC()
				if m.Timestamp > 0 {
					ts = time.UnixMilli(m.Timestamp).UTC()
				}
				isDelivery := len(m.Delivery.MIDs) > 0
				isRead := m.Read.Watermark > 0

				msgText := m.Message.Text
				adTitle := strings.TrimSpace(m.Message.Referral.AdTitle)
				if adTitle == "" {
					adTitle = strings.TrimSpace(m.Referral.AdTitle)
				}
				if adTitle != "" && msgText != "" {
					msgText = fmt.Sprintf("[Anuncio Marketplace: %s] %s", adTitle, msgText)
				}

				triageRes := a.TriageInboundEvent(
					platform,
					m.Sender.ID,
					m.Recipient.ID,
					m.Message.MID,
					msgText,
					m.Message.IsEcho,
					isDelivery,
					isRead,
					ts,
				)

				if triageRes.ShouldProcess {
					a.IngestTriageResult(triageRes)
					a.mu.RLock()
					hook := a.onInbound
					chID := ChannelIDMessenger
					if platform == "instagram" {
						chID = ChannelIDInstagram
					}
					conv := Conversation{
						ID:            fmt.Sprintf("%s-%s", chID, m.Sender.ID),
						ChannelID:     chID,
						CustomerAlias: m.Sender.ID,
						UpdatedAt:     ts,
					}
					a.mu.RUnlock()
					if hook != nil {
						hook(chID, conv)
					}
				}
			}
		}

		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte(`{"status":"received"}`))
		return

	default:
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// MetaSubChannel es un envoltorio que implementa la interfaz Channel para una plataforma especifica (FB o IG).
type MetaSubChannel struct {
	adapter   *MetaOmnichannelAdapter
	channelID string
}

// NewMetaSubChannel instancia la interfaz Channel para Messenger o Instagram.
func (a *MetaOmnichannelAdapter) NewSubChannel(channelID string) *MetaSubChannel {
	return &MetaSubChannel{
		adapter:   a,
		channelID: channelID,
	}
}

// MessengerSubChannel retorna la vista Channel para Facebook Messenger.
func (a *MetaOmnichannelAdapter) MessengerSubChannel() *MetaSubChannel {
	return a.NewSubChannel(ChannelIDMessenger)
}

// InstagramSubChannel retorna la vista Channel para Instagram Direct.
func (a *MetaOmnichannelAdapter) InstagramSubChannel() *MetaSubChannel {
	return a.NewSubChannel(ChannelIDInstagram)
}

func (s *MetaSubChannel) ID() string {
	return s.channelID
}

func (s *MetaSubChannel) Limits() Limits {
	return s.adapter.LimitsForChannel(s.channelID)
}

func (s *MetaSubChannel) Open(ctx context.Context, link Link) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	if s.adapter.IsPaused(s.channelID) {
		return Session{}, ErrChannelPaused
	}
	return Session{
		ID:        fmt.Sprintf("sess-%s-%s", s.channelID, link.ID),
		ChannelID: s.channelID,
		CreatedAt: time.Now().UTC(),
	}, nil
}

func (s *MetaSubChannel) Pending(ctx context.Context, session Session) ([]Conversation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.adapter.IsPaused(s.channelID) {
		return nil, ErrChannelPaused
	}
	s.adapter.mu.RLock()
	defer s.adapter.mu.RUnlock()
	convs := s.adapter.conversations[s.channelID]
	out := make([]Conversation, len(convs))
	copy(out, convs)
	return out, nil
}

func (s *MetaSubChannel) Read(ctx context.Context, session Session, conversation Conversation) (Thread, error) {
	if err := ctx.Err(); err != nil {
		return Thread{}, err
	}
	if s.adapter.IsPaused(s.channelID) {
		return Thread{}, ErrChannelPaused
	}
	s.adapter.mu.RLock()
	defer s.adapter.mu.RUnlock()
	th, ok := s.adapter.threads[conversation.ID]
	if !ok {
		return Thread{}, fmt.Errorf("channel %s: conversation %s not found", s.channelID, conversation.ID)
	}
	msgs := make([]Message, len(th.Messages))
	copy(msgs, th.Messages)
	return Thread{
		Conversation: th.Conversation,
		Messages:     msgs,
	}, nil
}

func (s *MetaSubChannel) Reply(ctx context.Context, session Session, conversation Conversation, message Message, mode Mode) (SendResult, error) {
	if err := ctx.Err(); err != nil {
		return SendResult{Status: SendFailed}, err
	}
	if s.adapter.IsPaused(s.channelID) {
		return SendResult{Status: SendFailed}, ErrChannelPaused
	}

	limits := s.Limits()
	if len(message.Text) > limits.MaxCharacterLength {
		return SendResult{Status: SendFailed}, ErrCharacterLimitExceeded
	}

	now := time.Now().UTC()
	if mode != ModeDraft && s.adapter.rateCtrl != nil {
		allowed, _ := s.adapter.rateCtrl.CanSend(s.channelID, now)
		if !allowed {
			return SendResult{Status: SendFailed}, ErrRateLimitExceeded
		}
	}

	switch mode {
	case ModeSimulation:
		if s.adapter.rateCtrl != nil {
			s.adapter.rateCtrl.RecordSend(s.channelID, now)
		}
		if s.adapter.tracker != nil {
			s.adapter.tracker.RecordSuccess(s.channelID)
		}
		return SendResult{
			Status:    SendSimulated,
			MessageID: message.ID,
			Timestamp: now,
		}, nil

	case ModeDraft:
		s.adapter.mu.Lock()
		draft := MetaDraftEntry{
			DraftID:        fmt.Sprintf("draft-%s-%s-%d", s.channelID, conversation.ID, len(s.adapter.draftOutbox)+1),
			ChannelID:      s.channelID,
			ConversationID: conversation.ID,
			Text:           message.Text,
			CreatedAt:      now,
		}
		s.adapter.draftOutbox = append(s.adapter.draftOutbox, draft)
		s.adapter.mu.Unlock()

		return SendResult{
			Status:    SendDrafted,
			MessageID: draft.DraftID,
			Timestamp: now,
		}, nil

	case ModeAutopilot:
		result, err := s.sendViaMetaGraphAPI(ctx, conversation.ID, message.Text)
		if err != nil {
			if s.adapter.tracker != nil {
				s.adapter.tracker.RecordFailure(s.channelID, err.Error())
			}
			return SendResult{Status: SendFailed}, err
		}

		if s.adapter.rateCtrl != nil {
			s.adapter.rateCtrl.RecordSend(s.channelID, now)
		}
		if s.adapter.tracker != nil {
			s.adapter.tracker.RecordSuccess(s.channelID)
		}
		return result, nil

	default:
		return SendResult{Status: SendFailed}, fmt.Errorf("channel %s: unknown mode %s", s.channelID, mode)
	}
}

// sendViaMetaGraphAPI despacha a Meta Graph API para Messenger o Instagram Direct.
func (s *MetaSubChannel) sendViaMetaGraphAPI(ctx context.Context, convID, text string) (SendResult, error) {
	cfg := s.adapter.ConfigSnapshot()
	recipientID := strings.TrimPrefix(convID, s.channelID+"-")
	endpoint := fmt.Sprintf("%s/me/messages", strings.TrimRight(cfg.BaseURL, "/"))

	payload := map[string]interface{}{
		"recipient": map[string]string{
			"id": recipientID,
		},
		"message": map[string]string{
			"text": text,
		},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return SendResult{Status: SendFailed}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return SendResult{Status: SendFailed}, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return SendResult{Status: SendFailed}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return SendResult{Status: SendFailed}, ErrQuotaExceeded
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return SendResult{Status: SendFailed}, fmt.Errorf("meta_omnichannel: api returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return SendResult{
		Status:    SendVerified,
		MessageID: fmt.Sprintf("%s-%d", s.channelID, time.Now().UnixNano()),
		Timestamp: time.Now().UTC(),
	}, nil
}
