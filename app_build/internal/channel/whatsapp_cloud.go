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
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// WhatsAppCloudChannelID es el identificador canonico del canal oficial WhatsApp Cloud API.
	WhatsAppCloudChannelID = "whatsapp_cloud"
	// WhatsAppAliasChannelID es el alias generico para interoperabilidad.
	WhatsAppAliasChannelID = "whatsapp"
	// WhatsAppMaxCharacters es el presupuesto de caracteres para WhatsApp (Mandamiento 2).
	WhatsAppMaxCharacters = 650
	// DefaultGraphAPIBaseURL es la URL base de Meta Graph API.
	DefaultGraphAPIBaseURL = "https://graph.facebook.com/v21.0"
	// DefaultGraphAPITimeout es el timeout para llamadas salientes a Meta Graph API (< 30s).
	DefaultGraphAPITimeout = 10 * time.Second
)

// WhatsAppMediaAttachment encapsula la metadata de adjuntos multimedia entrantes.
type WhatsAppMediaAttachment struct {
	ID       string `json:"id"`
	MIMEType string `json:"mime_type"`
	SHA256   string `json:"sha256"`
	Caption  string `json:"caption,omitempty"`
	Filename string `json:"filename,omitempty"`
	Voice    bool   `json:"voice,omitempty"`
}

// WhatsAppInboundMessage representa un mensaje entrante procesado del webhook de WhatsApp Cloud API.
type WhatsAppInboundMessage struct {
	ID        string                   `json:"id"`
	From      string                   `json:"from"`
	Timestamp time.Time                `json:"timestamp"`
	Type      string                   `json:"type"` // "text", "image", "audio", "document", "video", "sticker"
	Text      string                   `json:"text"`
	Media     *WhatsAppMediaAttachment `json:"media,omitempty"`
}

// WhatsAppDraftEntry registra un borrador de WhatsApp pendiente de aprobacion humana en DRAFT.
type WhatsAppDraftEntry struct {
	DraftID        string
	ConversationID string
	CustomerAlias  string
	Text           string
	CreatedAt      time.Time
	ApprovedBy     string
	Edited         bool
}

// WhatsAppCloudConfig define las credenciales y endpoints del adaptador oficial WhatsApp Cloud API.
type WhatsAppCloudConfig struct {
	PhoneNumberID string
	AccessToken   string
	VerifyToken   string
	AppSecret     string
	BaseURL       string
	HTTPClient    *http.Client
}

// MemoryOptInManager gestiona el consentimiento de clientes de forma segura en memoria y thread-safe.
type MemoryOptInManager struct {
	mu       sync.RWMutex
	statuses map[string]OptInStatus
}

// NewMemoryOptInManager construye un gestor de consentimiento en memoria.
func NewMemoryOptInManager() *MemoryOptInManager {
	return &MemoryOptInManager{
		statuses: make(map[string]OptInStatus),
	}
}

// GetStatus retorna el estado de opt-in para un cliente dado.
func (m *MemoryOptInManager) GetStatus(customerID string) OptInStatus {
	if customerID == "" {
		return OptInUnknown
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	status, exists := m.statuses[customerID]
	if !exists {
		return OptInUnknown
	}
	return status
}

// SetStatus fija el estado de opt-in para un cliente.
func (m *MemoryOptInManager) SetStatus(customerID string, status OptInStatus) {
	if customerID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statuses[customerID] = status
}

var (
	optOutKeywords = regexp.MustCompile(`(?i)\b(stop|baja|cancelar|no\s+deseo\s+recibir\s+mas|unsubscribe|no\s+mas|salir)\b`)
	optInKeywords  = regexp.MustCompile(`(?i)\b(start|alta|acepto|continuar|quiero\s+info|si|suscribir)\b`)
)

// EvaluateConsent evalua el texto del mensaje entrante para actualizar el consentimiento del cliente.
func (m *MemoryOptInManager) EvaluateConsent(customerID, text string) OptInStatus {
	if customerID == "" {
		return OptInUnknown
	}
	cleaned := strings.TrimSpace(text)
	if optOutKeywords.MatchString(cleaned) {
		m.SetStatus(customerID, OptInRevoked)
		return OptInRevoked
	}
	if optInKeywords.MatchString(cleaned) {
		m.SetStatus(customerID, OptInGranted)
		return OptInGranted
	}
	return m.GetStatus(customerID)
}

// HSMTemplateCatalog mantiene el catalogo de plantillas pre-aprobadas por Meta.
type HSMTemplateCatalog struct {
	mu        sync.RWMutex
	templates map[string]HSMTemplate
}

// NewHSMTemplateCatalog inicializa el catalogo con las plantillas oficiales pre-aprobadas para BuyaCAR Tunja.
func NewHSMTemplateCatalog() *HSMTemplateCatalog {
	catalog := &HSMTemplateCatalog{
		templates: make(map[string]HSMTemplate),
	}
	// Catalogo inicial pre-aprobado para >24h window re-engagement
	catalog.Register(HSMTemplate{
		Name:     "lead_followup_24h",
		Language: "es_CO",
		Components: []TemplateParam{
			{Type: "text", Text: "Hola {{1}}, tenemos novedades sobre el vehículo {{2}} que consultaste en BuyaCAR Tunja. ¿Deseas más información?"},
		},
	})
	catalog.Register(HSMTemplate{
		Name:     "price_drop_alert",
		Language: "es_CO",
		Components: []TemplateParam{
			{Type: "text", Text: "Hola {{1}}, el vehículo {{2}} en BuyaCAR Tunja tiene un nuevo precio especial de {{3}}. ¿Te interesa agendar una prueba?"},
		},
	})
	catalog.Register(HSMTemplate{
		Name:     "appointment_reminder",
		Language: "es_CO",
		Components: []TemplateParam{
			{Type: "text", Text: "Hola {{1}}, te recordamos tu cita para ver el {{2}} el día {{3}} en nuestro concesionario BuyaCAR Tunja."},
		},
	})
	catalog.Register(HSMTemplate{
		Name:     "welcome_optin",
		Language: "es_CO",
		Components: []TemplateParam{
			{Type: "text", Text: "Hola {{1}}, bienvenido a BuyaCAR Tunja. Contamos con vehículos certificados. ¿En qué modelo estás interesado?"},
		},
	})
	return catalog
}

// Register almacena una nueva plantilla HSM en el catalogo.
func (c *HSMTemplateCatalog) Register(tmpl HSMTemplate) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.templates[tmpl.Name] = tmpl
}

// Get obtiene una plantilla por nombre.
func (c *HSMTemplateCatalog) Get(name string) (HSMTemplate, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	tmpl, exists := c.templates[name]
	return tmpl, exists
}

// Render sustituye los parametros posicionales {{1}}, {{2}}, etc. en el componente de texto.
func (c *HSMTemplateCatalog) Render(name string, params ...string) (string, error) {
	tmpl, ok := c.Get(name)
	if !ok {
		return "", fmt.Errorf("whatsapp_cloud: hsm template %q not found", name)
	}
	if len(tmpl.Components) == 0 {
		return "", fmt.Errorf("whatsapp_cloud: hsm template %q has no components", name)
	}
	rendered := tmpl.Components[0].Text
	for i, param := range params {
		placeholder := fmt.Sprintf("{{%d}}", i+1)
		rendered = strings.ReplaceAll(rendered, placeholder, param)
	}
	return rendered, nil
}

// WhatsAppCloudChannel es el adaptador oficial de WhatsApp Business Cloud API.
type WhatsAppCloudChannel struct {
	mu           sync.RWMutex
	config       WhatsAppCloudConfig
	limits       Limits
	optInManager *MemoryOptInManager
	catalog      *HSMTemplateCatalog
	tracker      ResilienceTracker
	rateCtrl     RateController

	conversations []Conversation
	threads       map[string]Thread
	lastInbound   map[string]time.Time
	draftOutbox   []WhatsAppDraftEntry
	paused        bool
}

// NewWhatsAppCloudChannel inicializa el canal oficial WhatsApp Cloud API.
func NewWhatsAppCloudChannel(cfg WhatsAppCloudConfig, rt ResilienceTracker, rc RateController) *WhatsAppCloudChannel {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultGraphAPIBaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: DefaultGraphAPITimeout}
	}
	if rt == nil {
		rt = NewResilienceTracker(nil)
	}
	if rc == nil {
		rc = NewRateController(DefaultRateConfig())
	}
	return &WhatsAppCloudChannel{
		config: cfg,
		limits: Limits{
			MaxCharacterLength: WhatsAppMaxCharacters,
			MaxPerHour:         120,
			MaxPerDay:          250,
		},
		optInManager:  NewMemoryOptInManager(),
		catalog:       NewHSMTemplateCatalog(),
		tracker:       rt,
		rateCtrl:      rc,
		threads:       make(map[string]Thread),
		lastInbound:   make(map[string]time.Time),
		draftOutbox:   make([]WhatsAppDraftEntry, 0),
		conversations: make([]Conversation, 0),
	}
}

func (w *WhatsAppCloudChannel) ID() string {
	return WhatsAppCloudChannelID
}

func (w *WhatsAppCloudChannel) Limits() Limits {
	return w.limits
}

// OptInManager retorna el gestor de consentimiento.
func (w *WhatsAppCloudChannel) OptInManager() *MemoryOptInManager {
	return w.optInManager
}

// HSMTemplateCatalog retorna el catalogo de plantillas HSM.
func (w *WhatsAppCloudChannel) HSMTemplateCatalog() *HSMTemplateCatalog {
	return w.catalog
}

// ResilienceTracker retorna el tracker de resiliencia asociado.
func (w *WhatsAppCloudChannel) ResilienceTracker() ResilienceTracker {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.tracker
}

// SetResilienceTracker inyecta o actualiza el tracker de resiliencia.
func (w *WhatsAppCloudChannel) SetResilienceTracker(rt ResilienceTracker) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.tracker = rt
}

// RateController retorna el controlador de tasa asociado.
func (w *WhatsAppCloudChannel) RateController() RateController {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.rateCtrl
}

// SetRateController inyecta o actualiza el controlador de tasa.
func (w *WhatsAppCloudChannel) SetRateController(rc RateController) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.rateCtrl = rc
}

// IsPaused indica si el canal esta pausado por seguridad o intervencion humana.
func (w *WhatsAppCloudChannel) IsPaused() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.paused {
		return true
	}
	if w.tracker != nil && w.tracker.IsPaused(WhatsAppCloudChannelID) {
		return true
	}
	return false
}

// SetPaused actualiza el estado de pausa local y en el ResilienceTracker.
func (w *WhatsAppCloudChannel) SetPaused(paused bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.paused = paused
	if w.tracker != nil {
		w.tracker.SetPaused(WhatsAppCloudChannelID, paused)
	}
}

// IsWithin24HourWindow verifica si el cliente envio un mensaje en las ultimas 24 horas.
func (w *WhatsAppCloudChannel) IsWithin24HourWindow(customerID string, now time.Time) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	last, ok := w.lastInbound[customerID]
	if !ok {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	return now.Sub(last) <= 24*time.Hour
}

// RecordInboundMessage registra un mensaje entrante parsed en el estado del canal.
func (w *WhatsAppCloudChannel) RecordInboundMessage(inbound WhatsAppInboundMessage) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.lastInbound[inbound.From] = inbound.Timestamp
	w.optInManager.EvaluateConsent(inbound.From, inbound.Text)

	convID := inbound.From
	conv := Conversation{
		ID:            convID,
		ChannelID:     WhatsAppCloudChannelID,
		CustomerAlias: inbound.From,
		UpdatedAt:     inbound.Timestamp,
	}

	exists := false
	for i, c := range w.conversations {
		if c.ID == convID {
			w.conversations[i].UpdatedAt = inbound.Timestamp
			exists = true
			break
		}
	}
	if !exists {
		w.conversations = append(w.conversations, conv)
	}

	msg := Message{
		ID:        inbound.ID,
		Sender:    "customer",
		Text:      inbound.Text,
		Timestamp: inbound.Timestamp,
	}

	th, ok := w.threads[convID]
	if !ok {
		th = Thread{
			Conversation: conv,
			Messages:     []Message{msg},
		}
	} else {
		th.Messages = append(th.Messages, msg)
	}
	w.threads[convID] = th
}

// Open valida los parametros del canal y abre una sesion de trabajo.
func (w *WhatsAppCloudChannel) Open(ctx context.Context, link Link) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	if w.IsPaused() {
		return Session{}, ErrChannelPaused
	}
	if !link.Active {
		return Session{}, fmt.Errorf("channel %s: link %s is inactive", WhatsAppCloudChannelID, link.ID)
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(link.URL)), "https://") {
		return Session{}, fmt.Errorf("channel %s: non-https link rejected", WhatsAppCloudChannelID)
	}
	return Session{
		ID:        "sess-wacloud-" + link.ID,
		ChannelID: WhatsAppCloudChannelID,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// Pending retorna las conversaciones activas pendientes de atencion.
func (w *WhatsAppCloudChannel) Pending(ctx context.Context, session Session) ([]Conversation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if w.IsPaused() {
		return nil, ErrChannelPaused
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]Conversation, len(w.conversations))
	copy(out, w.conversations)
	return out, nil
}

// Read devuelve el hilo de mensajes de una conversacion.
func (w *WhatsAppCloudChannel) Read(ctx context.Context, session Session, conversation Conversation) (Thread, error) {
	if err := ctx.Err(); err != nil {
		return Thread{}, err
	}
	if w.IsPaused() {
		return Thread{}, ErrChannelPaused
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	th, ok := w.threads[conversation.ID]
	if !ok {
		return Thread{}, fmt.Errorf("channel %s: conversation %s not found", WhatsAppCloudChannelID, conversation.ID)
	}
	msgs := make([]Message, len(th.Messages))
	copy(msgs, th.Messages)
	return Thread{
		Conversation: th.Conversation,
		Messages:     msgs,
	}, nil
}

// Reply procesa el envio segun el modo operativo respetando rate limits, opt-in y la ventana de 24h.
func (w *WhatsAppCloudChannel) Reply(ctx context.Context, session Session, conversation Conversation, message Message, mode Mode) (SendResult, error) {
	if err := ctx.Err(); err != nil {
		return SendResult{Status: SendFailed}, err
	}
	if w.IsPaused() {
		return SendResult{Status: SendFailed}, ErrChannelPaused
	}

	// 1. Validacion de presupuesto de caracteres (650 caracteres para WhatsApp)
	if len(message.Text) > w.limits.MaxCharacterLength {
		return SendResult{Status: SendFailed}, ErrCharacterLimitExceeded
	}

	// 2. Verificacion estricta de consentimiento Opt-In
	optStatus := w.optInManager.GetStatus(conversation.ID)
	if optStatus == OptInRevoked {
		return SendResult{Status: SendFailed}, ErrOptInRevoked
	}

	// 3. Verificacion de tasa de envio (RateController solo para envios outbound)
	now := time.Now().UTC()
	if mode != ModeDraft && w.rateCtrl != nil {
		allowed, _ := w.rateCtrl.CanSend(WhatsAppCloudChannelID, now)
		if !allowed {
			return SendResult{Status: SendFailed}, ErrRateLimitExceeded
		}
	}

	switch mode {
	case ModeSimulation:
		if w.rateCtrl != nil {
			w.rateCtrl.RecordSend(WhatsAppCloudChannelID, now)
		}
		if w.tracker != nil {
			w.tracker.RecordSuccess(WhatsAppCloudChannelID)
		}
		return SendResult{
			Status:    SendSimulated,
			MessageID: message.ID,
			Timestamp: now,
		}, nil

	case ModeDraft:
		w.mu.Lock()
		draft := WhatsAppDraftEntry{
			DraftID:        fmt.Sprintf("draft-wa-%s-%d", conversation.ID, len(w.draftOutbox)+1),
			ConversationID: conversation.ID,
			CustomerAlias:  conversation.CustomerAlias,
			Text:           message.Text,
			CreatedAt:      now,
		}
		w.draftOutbox = append(w.draftOutbox, draft)
		w.mu.Unlock()

		return SendResult{
			Status:    SendDrafted,
			MessageID: draft.DraftID,
			Timestamp: now,
		}, nil

	case ModeAutopilot:
		// Verificacion de ventana de 24 horas: fuera de ventana exige plantilla HSM
		if !w.IsWithin24HourWindow(conversation.ID, now) {
			if !strings.HasPrefix(strings.TrimSpace(message.Text), "HSM:") {
				return SendResult{Status: SendFailed}, ErrHSMTemplateRequired
			}
		}

		result, err := w.sendViaGraphAPI(ctx, conversation.ID, message.Text)
		if err != nil {
			if w.tracker != nil {
				w.tracker.RecordFailure(WhatsAppCloudChannelID, err.Error())
			}
			return SendResult{Status: SendFailed}, err
		}

		if w.rateCtrl != nil {
			w.rateCtrl.RecordSend(WhatsAppCloudChannelID, now)
		}
		if w.tracker != nil {
			w.tracker.RecordSuccess(WhatsAppCloudChannelID)
		}
		return result, nil

	default:
		return SendResult{Status: SendFailed}, fmt.Errorf("channel %s: unknown mode %s", WhatsAppCloudChannelID, mode)
	}
}

// sendViaGraphAPI despacha un mensaje a traves de Meta Graph API.
func (w *WhatsAppCloudChannel) sendViaGraphAPI(ctx context.Context, to string, text string) (SendResult, error) {
	reqURL := fmt.Sprintf("%s/%s/messages", strings.TrimRight(w.config.BaseURL, "/"), w.config.PhoneNumberID)

	var payload map[string]interface{}
	if strings.HasPrefix(strings.TrimSpace(text), "HSM:") {
		// Despacho de plantilla HSM pre-aprobada
		templateName := strings.TrimPrefix(strings.TrimSpace(text), "HSM:")
		tmpl, exists := w.catalog.Get(templateName)
		if !exists {
			return SendResult{Status: SendFailed}, fmt.Errorf("whatsapp_cloud: template %s not found in catalog", templateName)
		}
		payload = map[string]interface{}{
			"messaging_product": "whatsapp",
			"to":                to,
			"type":              "template",
			"template": map[string]interface{}{
				"name": tmpl.Name,
				"language": map[string]string{
					"code": tmpl.Language,
				},
			},
		}
	} else {
		payload = map[string]interface{}{
			"messaging_product": "whatsapp",
			"to":                to,
			"type":              "text",
			"text": map[string]string{
				"body": text,
			},
		}
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return SendResult{Status: SendFailed}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return SendResult{Status: SendFailed}, err
	}
	req.Header.Set("Authorization", "Bearer "+w.config.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.config.HTTPClient.Do(req)
	if err != nil {
		return SendResult{Status: SendFailed}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return SendResult{Status: SendFailed}, ErrQuotaExceeded
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return SendResult{Status: SendFailed}, fmt.Errorf("whatsapp_cloud: graph api returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var graphResp struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&graphResp); err == nil && len(graphResp.Messages) > 0 {
		return SendResult{
			Status:    SendVerified,
			MessageID: graphResp.Messages[0].ID,
			Timestamp: time.Now().UTC(),
		}, nil
	}

	return SendResult{
		Status:    SendVerified,
		MessageID: fmt.Sprintf("wa-%d", time.Now().UnixNano()),
		Timestamp: time.Now().UTC(),
	}, nil
}

// VerifySignature comprueba la firma criptografica HMAC-SHA256 del payload entrante contra AppSecret.
func (w *WhatsAppCloudChannel) VerifySignature(payload []byte, signature string) bool {
	if w.config.AppSecret == "" || signature == "" {
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

	mac := hmac.New(sha256.New, []byte(w.config.AppSecret))
	mac.Write(payload)
	actualMAC := mac.Sum(nil)

	return hmac.Equal(actualMAC, expectedMAC)
}

// HandleWebhook implementa el contrato WebhookReceiver para verificar handshake GET y validar POSTs firmados.
func (w *WhatsAppCloudChannel) HandleWebhook(rw http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// Webhook Verification Handshake
		query := r.URL.Query()
		mode := query.Get("hub.mode")
		token := query.Get("hub.verify_token")
		challenge := query.Get("hub.challenge")

		if mode == "subscribe" && token == w.config.VerifyToken {
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
		if !w.VerifySignature(payload, sig) {
			http.Error(rw, "invalid signature", http.StatusUnauthorized)
			return
		}

		inboundMessages, err := ParseWhatsAppWebhookPayload(payload)
		if err != nil {
			http.Error(rw, "unparseable json payload", http.StatusBadRequest)
			return
		}

		for _, msg := range inboundMessages {
			w.RecordInboundMessage(msg)
		}

		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte(`{"status":"received"}`))
		return

	default:
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ParseWhatsAppWebhookPayload extrae mensajes de texto y multimedia desde el payload estructurado de Meta.
func ParseWhatsAppWebhookPayload(payload []byte) ([]WhatsAppInboundMessage, error) {
	var metaPayload struct {
		Entry []struct {
			Changes []struct {
				Value struct {
					Messages []struct {
						ID        string `json:"id"`
						From      string `json:"from"`
						Timestamp string `json:"timestamp"`
						Type      string `json:"type"`
						Text      struct {
							Body string `json:"body"`
						} `json:"text"`
						Image struct {
							ID       string `json:"id"`
							MIMEType string `json:"mime_type"`
							SHA256   string `json:"sha256"`
							Caption  string `json:"caption"`
						} `json:"image"`
						Audio struct {
							ID       string `json:"id"`
							MIMEType string `json:"mime_type"`
							Voice    bool   `json:"voice"`
						} `json:"audio"`
						Document struct {
							ID       string `json:"id"`
							MIMEType string `json:"mime_type"`
							Filename string `json:"filename"`
							Caption  string `json:"caption"`
						} `json:"document"`
					} `json:"messages"`
				} `json:"value"`
			} `json:"changes"`
		} `json:"entry"`
	}

	if err := json.Unmarshal(payload, &metaPayload); err != nil {
		return nil, err
	}

	var parsed []WhatsAppInboundMessage
	for _, entry := range metaPayload.Entry {
		for _, change := range entry.Changes {
			for _, m := range change.Value.Messages {
				ts := time.Now().UTC()
				if sec, err := strconv.ParseInt(m.Timestamp, 10, 64); err == nil {
					ts = time.Unix(sec, 0).UTC()
				}

				inbound := WhatsAppInboundMessage{
					ID:        m.ID,
					From:      m.From,
					Timestamp: ts,
					Type:      m.Type,
					Text:      m.Text.Body,
				}

				switch m.Type {
				case "image":
					inbound.Media = &WhatsAppMediaAttachment{
						ID:       m.Image.ID,
						MIMEType: m.Image.MIMEType,
						SHA256:   m.Image.SHA256,
						Caption:  m.Image.Caption,
					}
					if inbound.Text == "" {
						inbound.Text = m.Image.Caption
					}
				case "audio":
					inbound.Media = &WhatsAppMediaAttachment{
						ID:       m.Audio.ID,
						MIMEType: m.Audio.MIMEType,
						Voice:    m.Audio.Voice,
					}
				case "document":
					inbound.Media = &WhatsAppMediaAttachment{
						ID:       m.Document.ID,
						MIMEType: m.Document.MIMEType,
						Filename: m.Document.Filename,
						Caption:  m.Document.Caption,
					}
					if inbound.Text == "" {
						inbound.Text = m.Document.Caption
					}
				}

				parsed = append(parsed, inbound)
			}
		}
	}

	return parsed, nil
}
