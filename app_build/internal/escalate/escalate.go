package escalate

import (
	"sync"
	"time"

	"buyacar.com.co/omnicanal/internal/guard"
)

// Ticket representa una conversacion derivada a un asesor humano con PII redactada.
type Ticket struct {
	ID              string
	ChannelID       string
	ConversationID  string
	Reason          string
	RedactedContext string
	CreatedAt       time.Time
}

// Queue gestiona la cola de escalamiento humano y notificaciones seguras.
type Queue struct {
	mu      sync.RWMutex
	tickets []Ticket
}

func NewQueue() *Queue {
	return &Queue{}
}

// Enqueue redacta cualquier PII residual y registra el ticket en la cola humana.
func (q *Queue) Enqueue(channelID, convID, reason, rawContext string, now time.Time) Ticket {
	redacted, _ := guard.RedactPII(rawContext)
	t := Ticket{
		ID:              "esc-" + channelID + "-" + convID,
		ChannelID:       channelID,
		ConversationID:  convID,
		Reason:          reason,
		RedactedContext: redacted,
		CreatedAt:       now.UTC(),
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.tickets = append(q.tickets, t)
	return t
}

// Pending devuelve la lista de tickets pendientes de atencion humana.
func (q *Queue) Pending() []Ticket {
	q.mu.RLock()
	defer q.mu.RUnlock()
	out := make([]Ticket, len(q.tickets))
	copy(out, q.tickets)
	return out
}
