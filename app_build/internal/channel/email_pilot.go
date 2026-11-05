package channel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	// EmailPilotChannelID es el identificador canonico del canal piloto de correo (Orden #6).
	EmailPilotChannelID = "email"
	// DefaultEmailMaxChars define el limite maximo seguro para borradores del canal piloto de correo.
	DefaultEmailMaxChars = 2000
	// MaxConsecutiveEmailFailures pausa el canal automaticamente tras fallos consecutivos.
	MaxConsecutiveEmailFailures = 3
)

var (
	// ErrEmailAutopilotBlocked impide que el adaptador piloto de correo envie mensajes reales sin aprobacion humana.
	ErrEmailAutopilotBlocked = errors.New("channel/email: AUTOPILOT is strictly blocked on pilot adapter; only SIMULATION and DRAFT are allowed")
	// ErrEmailChannelPaused indica que el canal fue pausado tras fallos consecutivos.
	ErrEmailChannelPaused = errors.New("channel/email: pilot channel is paused due to consecutive failures")
)

// EmailDraftEntry registra un borrador generado para revision de un asesor humano en Etapa B (DRAFT).
type EmailDraftEntry struct {
	DraftID        string
	ConversationID string
	CustomerAlias  string
	Subject        string
	DraftBody      string
	CreatedAt      time.Time
	ApprovedBy     string
	Edited         bool
}

// EmailPilotChannel implementa el Canal Piloto de Correo (Orden de implementacion #6 en instructivo.md).
// Opera exclusivamente en lectura y generacion de borradores (SIMULATION / DRAFT), bloqueando envios autonomos.
type EmailPilotChannel struct {
	mu                  sync.RWMutex
	limits              Limits
	conversations       []Conversation
	threads             map[string]Thread
	subjects            map[string]string
	draftOutbox         []EmailDraftEntry
	consecutiveFailures int
	paused              bool
	tracker             ResilienceTracker
}

// NewEmailPilotChannel construye el adaptador piloto de correo en modo seguro lectura/borrador.
func NewEmailPilotChannel() *EmailPilotChannel {
	return &EmailPilotChannel{
		limits: Limits{
			MaxCharacterLength: DefaultEmailMaxChars,
			MaxPerHour:         120,
			MaxPerDay:          250,
		},
		threads:  make(map[string]Thread),
		subjects: make(map[string]string),
	}
}

// NewEmailPilotChannelWithTracker construye el adaptador piloto con un ResilienceTracker inyectado.
func NewEmailPilotChannelWithTracker(rt ResilienceTracker) *EmailPilotChannel {
	ch := NewEmailPilotChannel()
	ch.tracker = rt
	return ch
}

// ResilienceTracker retorna el tracker de resiliencia asociado al canal.
func (e *EmailPilotChannel) ResilienceTracker() ResilienceTracker {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.tracker
}

// SetTracker inyecta o actualiza el tracker de resiliencia del canal.
func (e *EmailPilotChannel) SetTracker(rt ResilienceTracker) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tracker = rt
}

// RecordSuccess restablece el contador de fallos consecutivos a 0 y notifica al tracker.
func (e *EmailPilotChannel) RecordSuccess() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.consecutiveFailures = 0
	if e.tracker != nil {
		e.tracker.RecordSuccess(EmailPilotChannelID)
	}
}

// SetPaused actualiza el estado de pausa del canal y sincroniza con el tracker.
func (e *EmailPilotChannel) SetPaused(paused bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.paused = paused
	if !paused {
		e.consecutiveFailures = 0
	}
	if e.tracker != nil {
		e.tracker.SetPaused(EmailPilotChannelID, paused)
	}
}

func (e *EmailPilotChannel) ID() string {
	return EmailPilotChannelID
}

func (e *EmailPilotChannel) Limits() Limits {
	return e.limits
}

// IngestInboundEmail registra un hilo entrante de correo en la bandeja de solo lectura.
func (e *EmailPilotChannel) IngestInboundEmail(conv Conversation, subject string, messages []Message) {
	e.mu.Lock()
	defer e.mu.Unlock()
	conv.ChannelID = EmailPilotChannelID
	e.conversations = append(e.conversations, conv)
	copied := make([]Message, len(messages))
	copy(copied, messages)
	e.threads[conv.ID] = Thread{
		Conversation: conv,
		Messages:     copied,
	}
	e.subjects[conv.ID] = strings.TrimSpace(subject)
}

// RecordFailure incrementa el contador de fallos consecutivos y pausa el canal al alcanzar el umbral.
func (e *EmailPilotChannel) RecordFailure() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.consecutiveFailures++
	if e.consecutiveFailures >= MaxConsecutiveEmailFailures {
		e.paused = true
	}
	if e.tracker != nil {
		e.tracker.RecordFailure(EmailPilotChannelID, "consecutive failure in email pilot channel")
	}
	return e.paused
}

// isPausedLocked comprueba el estado de pausa bajo cerrojo existente.
func (e *EmailPilotChannel) isPausedLocked() bool {
	if e.paused {
		return true
	}
	if e.tracker != nil && e.tracker.IsPaused(EmailPilotChannelID) {
		return true
	}
	return false
}

// IsPaused indica si el canal fue pausado por seguridad tras fallos consecutivos o accion de operador.
func (e *EmailPilotChannel) IsPaused() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.isPausedLocked()
}

func (e *EmailPilotChannel) Open(ctx context.Context, link Link) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	e.mu.RLock()
	paused := e.isPausedLocked()
	e.mu.RUnlock()
	if paused {
		return Session{}, ErrEmailChannelPaused
	}
	if !link.Active {
		return Session{}, fmt.Errorf("channel %s: link %s is inactive", EmailPilotChannelID, link.ID)
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(link.URL)), "https://") {
		return Session{}, fmt.Errorf("channel %s: non-https link rejected", EmailPilotChannelID)
	}
	return Session{
		ID:        "sess-email-pilot-" + link.ID,
		ChannelID: EmailPilotChannelID,
		CreatedAt: time.Now().UTC(),
	}, nil
}

func (e *EmailPilotChannel) Pending(ctx context.Context, session Session) ([]Conversation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.isPausedLocked() {
		return nil, ErrEmailChannelPaused
	}
	out := make([]Conversation, len(e.conversations))
	copy(out, e.conversations)
	return out, nil
}

func (e *EmailPilotChannel) Read(ctx context.Context, session Session, conversation Conversation) (Thread, error) {
	if err := ctx.Err(); err != nil {
		return Thread{}, err
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.isPausedLocked() {
		return Thread{}, ErrEmailChannelPaused
	}
	th, ok := e.threads[conversation.ID]
	if !ok {
		return Thread{}, fmt.Errorf("channel %s: conversation %s not found", EmailPilotChannelID, conversation.ID)
	}
	copiedMsgs := make([]Message, len(th.Messages))
	copy(copiedMsgs, th.Messages)
	return Thread{
		Conversation: th.Conversation,
		Messages:     copiedMsgs,
	}, nil
}

// Reply procesa la respuesta exclusivamente en SIMULATION (no-op) o DRAFT (guarda borrador para revision humana).
// Si recibe ModeAutopilot, bloquea inmediatamente el envio y devuelve ErrEmailAutopilotBlocked.
func (e *EmailPilotChannel) Reply(ctx context.Context, session Session, conversation Conversation, message Message, mode Mode) (SendResult, error) {
	if err := ctx.Err(); err != nil {
		return SendResult{Status: SendFailed}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.isPausedLocked() {
		return SendResult{Status: SendFailed}, ErrEmailChannelPaused
	}

	switch mode {
	case ModeSimulation:
		return SendResult{
			Status:    SendSimulated,
			MessageID: message.ID,
			Timestamp: message.Timestamp,
		}, nil

	case ModeDraft:
		subj := e.subjects[conversation.ID]
		if subj == "" {
			subj = "Información de vehículos certificados BuyaCAR Tunja"
		} else if !strings.HasPrefix(strings.ToLower(subj), "re:") {
			subj = "Re: " + subj
		}
		draft := EmailDraftEntry{
			DraftID:        fmt.Sprintf("draft-email-%s-%d", conversation.ID, len(e.draftOutbox)+1),
			ConversationID: conversation.ID,
			CustomerAlias:  conversation.CustomerAlias,
			Subject:        subj,
			DraftBody:      message.Text,
			CreatedAt:      message.Timestamp,
		}
		e.draftOutbox = append(e.draftOutbox, draft)
		return SendResult{
			Status:    SendDrafted,
			MessageID: draft.DraftID,
			Timestamp: message.Timestamp,
		}, nil

	default:
		// Guardrail estricto Etapa B: el adaptador piloto jamas envia correos al cliente final por cuenta propia.
		return SendResult{
			Status:    SendFailed,
			MessageID: message.ID,
			Timestamp: message.Timestamp,
		}, ErrEmailAutopilotBlocked
	}
}

// RecordedDrafts devuelve una copia de los borradores pendientes de revision humana.
func (e *EmailPilotChannel) RecordedDrafts() []EmailDraftEntry {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]EmailDraftEntry, len(e.draftOutbox))
	copy(out, e.draftOutbox)
	return out
}

// ReviewDraft registra la aprobacion humana de un borrador generado en Etapa B (con o sin edicion).
func (e *EmailPilotChannel) ReviewDraft(draftID, advisorID string, edited bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.draftOutbox {
		if e.draftOutbox[i].DraftID == draftID {
			e.draftOutbox[i].ApprovedBy = strings.TrimSpace(advisorID)
			e.draftOutbox[i].Edited = edited
			return nil
		}
	}
	return fmt.Errorf("channel/email: draft %s not found", draftID)
}

// DraftGateMetrics calcula el porcentaje de borradores aprobados sin requerir edicion (meta >= 85%).
func (e *EmailPilotChannel) DraftGateMetrics() (totalDrafts, reviewedDrafts, approvedUnedited int, uneditedPercent float64) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	totalDrafts = len(e.draftOutbox)
	for _, d := range e.draftOutbox {
		if d.ApprovedBy != "" {
			reviewedDrafts++
			if !d.Edited {
				approvedUnedited++
			}
		}
	}
	if reviewedDrafts > 0 {
		uneditedPercent = (float64(approvedUnedited) / float64(reviewedDrafts)) * 100.0
	}
	return totalDrafts, reviewedDrafts, approvedUnedited, uneditedPercent
}
