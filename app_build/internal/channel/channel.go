package channel

import (
	"context"
	"errors"
	"net/http"
	"time"
)

var (
	// ErrChannelPaused indica que el canal se encuentra pausado por resiliencia o accion de operador.
	ErrChannelPaused = errors.New("channel: channel is paused due to consecutive failures")
	// ErrRateLimitExceeded indica que se sobrepaso la cuota de mensajes por hora o por dia.
	ErrRateLimitExceeded = errors.New("channel: rate limit exceeded")
	// ErrOptInRevoked indica que el usuario revoco su consentimiento (opt-out) para recibir mensajes.
	ErrOptInRevoked = errors.New("channel: outbound message blocked due to revoked opt-in consent")
	// ErrHSMTemplateRequired indica que ventanas > 24 horas exigen plantillas HSM pre-aprobadas.
	ErrHSMTemplateRequired = errors.New("channel: window >24h requires pre-approved HSM template")
	// ErrQuotaExceeded indica saturacion de cuota de API (HTTP 429).
	ErrQuotaExceeded = errors.New("channel: API quota exceeded (HTTP 429)")
	// ErrPreSaleContactPolicy indica intento de enviar datos de contacto en preguntas publicas de preventa.
	ErrPreSaleContactPolicy = errors.New("channel: pre-sale question reply violates contact data policy")
	// ErrCharacterLimitExceeded indica que el mensaje supera el limite de caracteres del canal.
	ErrCharacterLimitExceeded = errors.New("channel: message exceeds maximum character length")
)

const (
	// MaxWebhookBodyBytes define el limite maximo de lectura en endpoints webhook (256 KB - Mandamiento 4).
	MaxWebhookBodyBytes = 256 * 1024
)

// WebhookReceiver define el contrato canonico para la recepcion e ingesta de webhooks firmados.
type WebhookReceiver interface {
	HandleWebhook(w http.ResponseWriter, r *http.Request)
	VerifySignature(payload []byte, signature string) bool
}

// OptInStatus representa el estado de consentimiento de un cliente para mensajeria directa.
type OptInStatus string

const (
	OptInGranted OptInStatus = "granted"
	OptInRevoked OptInStatus = "revoked"
	OptInUnknown OptInStatus = "unknown"
)

// OptInManager define la gestion de estado de consentimiento de clientes.
type OptInManager interface {
	GetStatus(customerID string) OptInStatus
	SetStatus(customerID string, status OptInStatus)
}

// TemplateParam representa un parametro componente para plantillas HSM.
type TemplateParam struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// HSMTemplate define una plantilla de alta estructura pre-aprobada para Meta WhatsApp.
type HSMTemplate struct {
	Name       string          `json:"name"`
	Language   string          `json:"language"`
	Components []TemplateParam `json:"components"`
}

// Mode representa el modo operativo del sistema.
type Mode string

const (
	ModeSimulation Mode = "SIMULATION"
	ModeDraft      Mode = "DRAFT"
	ModeAutopilot  Mode = "AUTOPILOT"
)

// Limits define las restricciones de tasa y longitud de un canal.
type Limits struct {
	MaxCharacterLength int
	MaxPerHour         int
	MaxPerDay          int
}

// Link representa un enlace editable a monitorear por el daemon.
type Link struct {
	ID        string
	URL       string
	ChannelID string
	Label     string
	Active    bool
	Priority  int
	Schedule  string
	LastVisit time.Time
}

// Session representa una sesion activa de canal (stub o real).
type Session struct {
	ID        string
	ChannelID string
	CreatedAt time.Time
}

// Conversation representa un hilo pendiente en un canal.
type Conversation struct {
	ID            string
	ChannelID     string
	CustomerAlias string
	UpdatedAt     time.Time
}

// Message representa un mensaje individual dentro de un hilo.
type Message struct {
	ID        string
	Sender    string // "customer" o "own"
	Text      string
	Timestamp time.Time
}

// Thread contiene el historial de mensajes de una conversacion.
type Thread struct {
	Conversation Conversation
	Messages     []Message
}

// SendStatus indica el resultado del intento de envio.
type SendStatus string

const (
	SendSimulated SendStatus = "simulated_no_op"
	SendDrafted   SendStatus = "draft_recorded"
	SendVerified  SendStatus = "sent_verified"
	SendFailed    SendStatus = "send_failed"
)

// SendResult encapsula la respuesta del canal tras Reply.
type SendResult struct {
	Status    SendStatus
	MessageID string
	Timestamp time.Time
}

// Channel es la interfaz minima obligatoria segun especificacion canonica.
type Channel interface {
	ID() string
	Limits() Limits
	Open(ctx context.Context, link Link) (Session, error)
	Pending(ctx context.Context, session Session) ([]Conversation, error)
	Read(ctx context.Context, session Session, conversation Conversation) (Thread, error)
	Reply(ctx context.Context, session Session, conversation Conversation, message Message, mode Mode) (SendResult, error)
}
