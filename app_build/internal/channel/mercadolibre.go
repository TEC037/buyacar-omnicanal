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
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	// MercadoLibreChannelID es el identificador canonico de canal para MercadoLibre.
	MercadoLibreChannelID = "mercadolibre"
	// MercadoLibreAliasChannelID es el alias corto.
	MercadoLibreAliasChannelID = "meli"
	// MercadoLibreMaxCharacters es la longitud maxima de respuesta en MercadoLibre.
	MercadoLibreMaxCharacters = 2000
	// DefaultMELIBaseURL es la URL base del API de MercadoLibre.
	DefaultMELIBaseURL = "https://api.mercadolibre.com"
	// ContactRedactionPlaceholder es el token insertado al purgar datos de contacto en preventa.
	ContactRedactionPlaceholder = "[CONTACTO_ELIMINADO_POR_POLITICA_MELI]"
	// TopicQuestions identifica el flujo publico de preguntas de preventa.
	TopicQuestions = "questions"
	// TopicMessages identifica la mensajeria privada post-venta entre comprador y vendedor.
	TopicMessages = "messages"
)

var (
	// Expresiones regulares para la politica de guardrail de preventa MercadoLibre
	reMeliPhoneColombia = regexp.MustCompile(`(?i)(?:\+?57\s*)?(?:3\d{2}[\s.-]?\d{3}[\s.-]?\d{4}|\b3\d{9}\b)`)
	reMeliGeneralPhone  = regexp.MustCompile(`\b(?:\+?\d{1,3}[\s.-]?)?\(?\d{2,4}\)?[\s.-]?\d{3,4}[\s.-]?\d{3,4}\b`)
	reMeliURL           = regexp.MustCompile(`(?i)(?:https?://[^\s)\]"']+|www\.[^\s)\]"']+|\b[a-zA-Z0-9.-]+\.(?:com|co|com\.co|net|org|io|app)[^\s]*\b)`)
	reMeliEmail         = regexp.MustCompile(`(?i)[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
)

// MercadoLibreNotification representa el payload de notificaciones push de webhooks de MercadoLibre.
type MercadoLibreNotification struct {
	Resource      string    `json:"resource"`
	UserID        int64     `json:"user_id"`
	Topic         string    `json:"topic"`
	ApplicationID int64     `json:"application_id"`
	Attempts      int       `json:"attempts"`
	Sent          time.Time `json:"sent"`
	Received      time.Time `json:"received"`
}

// MercadoLibreQuestion representa una pregunta de preventa en un item/vehiculo publicado.
type MercadoLibreQuestion struct {
	ID          string    `json:"id"`
	ItemID      string    `json:"item_id"`
	SellerID    int64     `json:"seller_id"`
	BuyerID     int64     `json:"buyer_id"`
	Text        string    `json:"text"`
	Status      string    `json:"status"` // "UNANSWERED", "ANSWERED"
	DateCreated time.Time `json:"date_created"`
}

// MercadoLibreDraftEntry almacena un borrador generado para revision humana.
type MercadoLibreDraftEntry struct {
	DraftID        string
	ConversationID string
	Topic          string
	Text           string
	Sanitized      bool
	CreatedAt      time.Time
}

// MercadoLibreConfig almacena las credenciales y ajustes del conector de MercadoLibre.
type MercadoLibreConfig struct {
	AppID       string
	SecretKey   string
	AccessToken string
	BaseURL     string
	HTTPClient  *http.Client
}

// MercadoLibreChannel implementa el conector de canal para MercadoLibre con politicas de preventa.
type MercadoLibreChannel struct {
	mu       sync.RWMutex
	config   MercadoLibreConfig
	limits   Limits
	tracker  ResilienceTracker
	rateCtrl RateController

	conversations []Conversation
	threads       map[string]Thread
	convTopics    map[string]string // ConversationID -> Topic ("questions" vs "messages")
	draftOutbox   []MercadoLibreDraftEntry
	paused        bool
}

// NewMercadoLibreChannel construye el conector oficial de MercadoLibre.
func NewMercadoLibreChannel(cfg MercadoLibreConfig, rt ResilienceTracker, rc RateController) *MercadoLibreChannel {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultMELIBaseURL
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

	return &MercadoLibreChannel{
		config: cfg,
		limits: Limits{
			MaxCharacterLength: MercadoLibreMaxCharacters,
			MaxPerHour:         120,
			MaxPerDay:          250,
		},
		tracker:       rt,
		rateCtrl:      rc,
		conversations: make([]Conversation, 0),
		threads:       make(map[string]Thread),
		convTopics:    make(map[string]string),
		draftOutbox:   make([]MercadoLibreDraftEntry, 0),
	}
}

func (m *MercadoLibreChannel) ID() string {
	return MercadoLibreChannelID
}

func (m *MercadoLibreChannel) Limits() Limits {
	return m.limits
}

// ResilienceTracker retorna el tracker de resiliencia.
func (m *MercadoLibreChannel) ResilienceTracker() ResilienceTracker {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tracker
}

// RateController retorna el controlador de tasa.
func (m *MercadoLibreChannel) RateController() RateController {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rateCtrl
}

// IsPaused indica si el canal esta pausado.
func (m *MercadoLibreChannel) IsPaused() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.paused {
		return true
	}
	if m.tracker != nil && m.tracker.IsPaused(MercadoLibreChannelID) {
		return true
	}
	return false
}

// SetPaused actualiza el estado de pausa.
func (m *MercadoLibreChannel) SetPaused(paused bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.paused = paused
	if m.tracker != nil {
		m.tracker.SetPaused(MercadoLibreChannelID, paused)
	}
}

// SanitizePreSaleReply aplica la politica estricta de preventa eliminando telefonos, correos y enlaces externos.
// Retorna el texto sanitizado y un booleano indicando si se detectaron y purgaron infracciones.
func SanitizePreSaleReply(text string) (string, bool) {
	modified := false
	out := text

	// 1. Purgar telefonos celulares colombianos o secuencias numericas de contacto
	if reMeliPhoneColombia.MatchString(out) {
		out = reMeliPhoneColombia.ReplaceAllString(out, ContactRedactionPlaceholder)
		modified = true
	}
	if reMeliGeneralPhone.MatchString(out) {
		// Purgar secuencias telefonicas restantes
		out = reMeliGeneralPhone.ReplaceAllStringFunc(out, func(match string) string {
			digits := 0
			for _, r := range match {
				if r >= '0' && r <= '9' {
					digits++
				}
			}
			if digits >= 7 {
				modified = true
				return ContactRedactionPlaceholder
			}
			return match
		})
	}

	// 2. Purgar correos electronicos
	if reMeliEmail.MatchString(out) {
		out = reMeliEmail.ReplaceAllString(out, ContactRedactionPlaceholder)
		modified = true
	}

	// 3. Purgar enlaces web externos (URLs, dominios, www)
	if reMeliURL.MatchString(out) {
		out = reMeliURL.ReplaceAllString(out, ContactRedactionPlaceholder)
		modified = true
	}

	return strings.TrimSpace(out), modified
}

// IngestQuestion registra una pregunta de preventa en el buzon del canal.
func (m *MercadoLibreChannel) IngestQuestion(q MercadoLibreQuestion) {
	m.mu.Lock()
	defer m.mu.Unlock()

	convID := "meli-q-" + q.ID
	conv := Conversation{
		ID:            convID,
		ChannelID:     MercadoLibreChannelID,
		CustomerAlias: fmt.Sprintf("buyer-%d", q.BuyerID),
		UpdatedAt:     q.DateCreated,
	}

	m.convTopics[convID] = TopicQuestions

	exists := false
	for i, c := range m.conversations {
		if c.ID == convID {
			m.conversations[i].UpdatedAt = q.DateCreated
			exists = true
			break
		}
	}
	if !exists {
		m.conversations = append(m.conversations, conv)
	}

	msg := Message{
		ID:        "msg-q-" + q.ID,
		Sender:    "customer",
		Text:      q.Text,
		Timestamp: q.DateCreated,
	}

	th, ok := m.threads[convID]
	if !ok {
		th = Thread{
			Conversation: conv,
			Messages:     []Message{msg},
		}
	} else {
		th.Messages = append(th.Messages, msg)
	}
	m.threads[convID] = th
}

// IngestPostSaleMessage registra un mensaje privado de orden post-venta.
func (m *MercadoLibreChannel) IngestPostSaleMessage(packID string, senderID int64, text string, ts time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	convID := "meli-m-" + packID
	conv := Conversation{
		ID:            convID,
		ChannelID:     MercadoLibreChannelID,
		CustomerAlias: fmt.Sprintf("buyer-%d", senderID),
		UpdatedAt:     ts,
	}

	m.convTopics[convID] = TopicMessages

	exists := false
	for i, c := range m.conversations {
		if c.ID == convID {
			m.conversations[i].UpdatedAt = ts
			exists = true
			break
		}
	}
	if !exists {
		m.conversations = append(m.conversations, conv)
	}

	msg := Message{
		ID:        fmt.Sprintf("msg-m-%s-%d", packID, ts.UnixNano()),
		Sender:    "customer",
		Text:      text,
		Timestamp: ts,
	}

	th, ok := m.threads[convID]
	if !ok {
		th = Thread{
			Conversation: conv,
			Messages:     []Message{msg},
		}
	} else {
		th.Messages = append(th.Messages, msg)
	}
	m.threads[convID] = th
}

// GetConversationTopic consulta si una conversacion pertenece a preventa o postventa.
func (m *MercadoLibreChannel) GetConversationTopic(convID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	topic, ok := m.convTopics[convID]
	if !ok {
		if strings.HasPrefix(convID, "meli-q-") {
			return TopicQuestions
		}
		return TopicMessages
	}
	return topic
}

func (m *MercadoLibreChannel) Open(ctx context.Context, link Link) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	if m.IsPaused() {
		return Session{}, ErrChannelPaused
	}
	return Session{
		ID:        "sess-meli-" + link.ID,
		ChannelID: MercadoLibreChannelID,
		CreatedAt: time.Now().UTC(),
	}, nil
}

func (m *MercadoLibreChannel) Pending(ctx context.Context, session Session) ([]Conversation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.IsPaused() {
		return nil, ErrChannelPaused
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Conversation, len(m.conversations))
	copy(out, m.conversations)
	return out, nil
}

func (m *MercadoLibreChannel) Read(ctx context.Context, session Session, conversation Conversation) (Thread, error) {
	if err := ctx.Err(); err != nil {
		return Thread{}, err
	}
	if m.IsPaused() {
		return Thread{}, ErrChannelPaused
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	th, ok := m.threads[conversation.ID]
	if !ok {
		return Thread{}, fmt.Errorf("channel %s: conversation %s not found", MercadoLibreChannelID, conversation.ID)
	}
	msgs := make([]Message, len(th.Messages))
	copy(msgs, th.Messages)
	return Thread{
		Conversation: th.Conversation,
		Messages:     msgs,
	}, nil
}

// Reply procesa la respuesta aplicando obligatoriamente el guardrail de preventa si la conversacion es publica.
func (m *MercadoLibreChannel) Reply(ctx context.Context, session Session, conversation Conversation, message Message, mode Mode) (SendResult, error) {
	if err := ctx.Err(); err != nil {
		return SendResult{Status: SendFailed}, err
	}
	if m.IsPaused() {
		return SendResult{Status: SendFailed}, ErrChannelPaused
	}

	if len(message.Text) > m.limits.MaxCharacterLength {
		return SendResult{Status: SendFailed}, ErrCharacterLimitExceeded
	}

	topic := m.GetConversationTopic(conversation.ID)
	sanitizedText := message.Text
	wasSanitized := false

	// Aplicacion estricta de politica de preventa en preguntas publicas
	if topic == TopicQuestions {
		sanitizedText, wasSanitized = SanitizePreSaleReply(message.Text)
	}

	now := time.Now().UTC()
	if mode != ModeDraft && m.rateCtrl != nil {
		allowed, _ := m.rateCtrl.CanSend(MercadoLibreChannelID, now)
		if !allowed {
			return SendResult{Status: SendFailed}, ErrRateLimitExceeded
		}
	}

	switch mode {
	case ModeSimulation:
		if m.rateCtrl != nil {
			m.rateCtrl.RecordSend(MercadoLibreChannelID, now)
		}
		if m.tracker != nil {
			m.tracker.RecordSuccess(MercadoLibreChannelID)
		}
		return SendResult{
			Status:    SendSimulated,
			MessageID: message.ID,
			Timestamp: now,
		}, nil

	case ModeDraft:
		m.mu.Lock()
		draft := MercadoLibreDraftEntry{
			DraftID:        fmt.Sprintf("draft-meli-%s-%d", conversation.ID, len(m.draftOutbox)+1),
			ConversationID: conversation.ID,
			Topic:          topic,
			Text:           sanitizedText,
			Sanitized:      wasSanitized,
			CreatedAt:      now,
		}
		m.draftOutbox = append(m.draftOutbox, draft)
		m.mu.Unlock()

		return SendResult{
			Status:    SendDrafted,
			MessageID: draft.DraftID,
			Timestamp: now,
		}, nil

	case ModeAutopilot:
		result, err := m.sendViaMELIAPI(ctx, conversation.ID, topic, sanitizedText)
		if err != nil {
			if m.tracker != nil {
				m.tracker.RecordFailure(MercadoLibreChannelID, err.Error())
			}
			return SendResult{Status: SendFailed}, err
		}

		if m.rateCtrl != nil {
			m.rateCtrl.RecordSend(MercadoLibreChannelID, now)
		}
		if m.tracker != nil {
			m.tracker.RecordSuccess(MercadoLibreChannelID)
		}
		return result, nil

	default:
		return SendResult{Status: SendFailed}, fmt.Errorf("channel %s: unknown mode %s", MercadoLibreChannelID, mode)
	}
}

// sendViaMELIAPI despacha la respuesta a la API de MercadoLibre.
func (m *MercadoLibreChannel) sendViaMELIAPI(ctx context.Context, convID, topic, text string) (SendResult, error) {
	var endpoint string
	var payload map[string]interface{}

	if topic == TopicQuestions {
		// Endpoint oficial de respuestas a preguntas de preventa: POST /answers
		questionID := strings.TrimPrefix(convID, "meli-q-")
		endpoint = fmt.Sprintf("%s/answers", strings.TrimRight(m.config.BaseURL, "/"))
		payload = map[string]interface{}{
			"question_id": questionID,
			"text":        text,
		}
	} else {
		// Endpoint oficial de mensajes post-venta
		packID := strings.TrimPrefix(convID, "meli-m-")
		endpoint = fmt.Sprintf("%s/messages/packs/%s/sellers/%s", strings.TrimRight(m.config.BaseURL, "/"), packID, m.config.AppID)
		payload = map[string]interface{}{
			"text": text,
		}
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return SendResult{Status: SendFailed}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return SendResult{Status: SendFailed}, err
	}
	req.Header.Set("Authorization", "Bearer "+m.config.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.config.HTTPClient.Do(req)
	if err != nil {
		return SendResult{Status: SendFailed}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return SendResult{Status: SendFailed}, ErrQuotaExceeded
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return SendResult{Status: SendFailed}, fmt.Errorf("mercadolibre: api returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return SendResult{
		Status:    SendVerified,
		MessageID: fmt.Sprintf("meli-%d", time.Now().UnixNano()),
		Timestamp: time.Now().UTC(),
	}, nil
}

// VerifySignature verifica la firma criptografica HMAC-SHA256 del header X-Signature.
// Soporta tanto formato simple hex sha256 como el formato estructurado ts=...,v1=... de MercadoLibre.
func (m *MercadoLibreChannel) VerifySignature(payload []byte, signature string) bool {
	if m.config.SecretKey == "" || signature == "" {
		return false
	}

	targetHash := signature
	var tsPart string

	// Si contiene ts= y v1=, extraemos v1
	if strings.Contains(signature, "v1=") {
		parts := strings.Split(signature, ",")
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if strings.HasPrefix(trimmed, "v1=") {
				targetHash = strings.TrimPrefix(trimmed, "v1=")
			} else if strings.HasPrefix(trimmed, "ts=") {
				tsPart = strings.TrimPrefix(trimmed, "ts=")
			}
		}
	} else if strings.HasPrefix(signature, "sha256=") {
		targetHash = strings.TrimPrefix(signature, "sha256=")
	}

	expectedMAC, err := hex.DecodeString(targetHash)
	if err != nil {
		return false
	}

	// Calculo de HMAC: si tsPart esta presente, el mensaje firmado es "ts:{ts};payload:{payload}" o simplemente payload
	mac := hmac.New(sha256.New, []byte(m.config.SecretKey))
	if tsPart != "" {
		// Validacion con manifest de timestamp
		mac.Write([]byte(tsPart))
		mac.Write([]byte(";"))
	}
	mac.Write(payload)
	actualMAC := mac.Sum(nil)

	if hmac.Equal(actualMAC, expectedMAC) {
		return true
	}

	// Fallback de comparacion directa sobre payload sin prefijo
	macDirect := hmac.New(sha256.New, []byte(m.config.SecretKey))
	macDirect.Write(payload)
	return hmac.Equal(macDirect.Sum(nil), expectedMAC)
}

// HandleWebhook procesa las notificaciones push de MercadoLibre y enruta segun topic ("questions" vs "messages").
func (m *MercadoLibreChannel) HandleWebhook(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(rw, r.Body, MaxWebhookBodyBytes)
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(rw, "payload too large or read error", http.StatusBadRequest)
		return
	}

	sig := r.Header.Get("X-Signature")
	if sig == "" {
		sig = r.Header.Get("X-MELI-Signature")
	}

	if !m.VerifySignature(payload, sig) {
		http.Error(rw, "invalid cryptographic signature", http.StatusUnauthorized)
		return
	}

	// Intento 1: parsear como notificacion push de evento MercadoLibre
	var notif MercadoLibreNotification
	if err := json.Unmarshal(payload, &notif); err == nil && notif.Resource != "" {
		m.routeNotification(notif)
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte(`{"status":"acknowledged"}`))
		return
	}

	// Intento 2: parsear como pregunta directa de preventa
	var q MercadoLibreQuestion
	if err := json.Unmarshal(payload, &q); err == nil && q.Text != "" {
		if q.DateCreated.IsZero() {
			q.DateCreated = time.Now().UTC()
		}
		m.IngestQuestion(q)
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte(`{"status":"question_ingested"}`))
		return
	}

	http.Error(rw, "unrecognized mercadolibre payload", http.StatusBadRequest)
}

// routeNotification enruta la notificacion segun su topico oficial.
func (m *MercadoLibreChannel) routeNotification(n MercadoLibreNotification) {
	switch n.Topic {
	case TopicQuestions:
		// Notificacion de nueva pregunta de preventa
		parts := strings.Split(n.Resource, "/")
		questionID := parts[len(parts)-1]
		m.IngestQuestion(MercadoLibreQuestion{
			ID:          questionID,
			SellerID:    n.UserID,
			Text:        fmt.Sprintf("Consulta de preventa recibida para recurso %s", n.Resource),
			DateCreated: n.Received,
		})
	case TopicMessages, "orders_v2":
		// Notificacion de mensaje post-venta
		parts := strings.Split(n.Resource, "/")
		packID := parts[len(parts)-1]
		m.IngestPostSaleMessage(packID, n.UserID, fmt.Sprintf("Mensaje post-venta recibido para recurso %s", n.Resource), n.Received)
	}
}
