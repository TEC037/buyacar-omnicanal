package channel

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// StubChannel implementa Channel de forma segura en memoria para SIMULATION.
type StubChannel struct {
	mu            sync.RWMutex
	id            string
	limits        Limits
	conversations []Conversation
	threads       map[string]Thread
	sentMessages  []Message
	tracker       ResilienceTracker
	paused        bool
	failSend      bool
	failRead      bool
	sendErrorMsg  string
	readErrorMsg  string
}

// NewStubChannel crea un adaptador stub seguro para pruebas sin tocar cuentas reales.
func NewStubChannel(id string, maxChars int) *StubChannel {
	return &StubChannel{
		id: id,
		limits: Limits{
			MaxCharacterLength: maxChars,
			MaxPerHour:         120,
			MaxPerDay:          250,
		},
		threads: make(map[string]Thread),
	}
}

// NewStubChannelWithTracker crea un StubChannel con ResilienceTracker inyectado.
func NewStubChannelWithTracker(id string, maxChars int, rt ResilienceTracker) *StubChannel {
	s := NewStubChannel(id, maxChars)
	s.tracker = rt
	return s
}

func (s *StubChannel) ID() string {
	return s.id
}

func (s *StubChannel) Limits() Limits {
	return s.limits
}

// ResilienceTracker retorna el ResilienceTracker configurado.
func (s *StubChannel) ResilienceTracker() ResilienceTracker {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tracker
}

// SetTracker inyecta o actualiza el ResilienceTracker.
func (s *StubChannel) SetTracker(rt ResilienceTracker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tracker = rt
}

// isPausedLocked comprueba el estado de pausa bajo cerrojo existente.
func (s *StubChannel) isPausedLocked() bool {
	if s.paused {
		return true
	}
	if s.tracker != nil && s.tracker.IsPaused(s.id) {
		return true
	}
	return false
}

// IsPaused indica si el canal esta pausado.
func (s *StubChannel) IsPaused() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isPausedLocked()
}

// SetPaused actualiza el estado de pausa del canal y sincroniza con el tracker.
func (s *StubChannel) SetPaused(paused bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = paused
	if s.tracker != nil {
		s.tracker.SetPaused(s.id, paused)
	}
}

// RecordSuccess registra exito en el tracker de resiliencia si esta configurado.
func (s *StubChannel) RecordSuccess() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tracker != nil {
		s.tracker.RecordSuccess(s.id)
	}
}

// RecordFailure registra un fallo en el tracker de resiliencia si esta configurado.
func (s *StubChannel) RecordFailure(reason string) (bool, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tracker != nil {
		return s.tracker.RecordFailure(s.id, reason)
	}
	return false, 0
}

// SetFailSend configura simulacion de error al responder.
func (s *StubChannel) SetFailSend(fail bool, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failSend = fail
	s.sendErrorMsg = msg
}

// SetFailRead configura simulacion de error al leer.
func (s *StubChannel) SetFailRead(fail bool, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failRead = fail
	s.readErrorMsg = msg
}

// GetSentMessages retorna una copia de los mensajes enviados en el stub.
func (s *StubChannel) GetSentMessages() []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copied := make([]Message, len(s.sentMessages))
	copy(copied, s.sentMessages)
	return copied
}

// SeedThread carga una conversacion de prueba en el stub.
func (s *StubChannel) SeedThread(conv Conversation, messages []Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conversations = append(s.conversations, conv)
	s.threads[conv.ID] = Thread{
		Conversation: conv,
		Messages:     messages,
	}
}

func (s *StubChannel) Open(ctx context.Context, link Link) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	s.mu.RLock()
	paused := s.isPausedLocked()
	s.mu.RUnlock()
	if paused {
		return Session{}, ErrChannelPaused
	}
	if !link.Active {
		return Session{}, fmt.Errorf("channel %s: link %s is inactive", s.id, link.ID)
	}
	return Session{
		ID:        "sess-" + s.id + "-" + link.ID,
		ChannelID: s.id,
		CreatedAt: time.Now().UTC(),
	}, nil
}

func (s *StubChannel) Pending(ctx context.Context, session Session) ([]Conversation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.isPausedLocked() {
		return nil, ErrChannelPaused
	}
	out := make([]Conversation, len(s.conversations))
	copy(out, s.conversations)
	return out, nil
}

func (s *StubChannel) Read(ctx context.Context, session Session, conversation Conversation) (Thread, error) {
	if err := ctx.Err(); err != nil {
		return Thread{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.isPausedLocked() {
		return Thread{}, ErrChannelPaused
	}
	if s.failRead {
		msg := s.readErrorMsg
		if msg == "" {
			msg = "stub read failed"
		}
		return Thread{}, errors.New(msg)
	}
	th, ok := s.threads[conversation.ID]
	if !ok {
		return Thread{}, fmt.Errorf("conversation %s not found", conversation.ID)
	}
	return th, nil
}

func (s *StubChannel) Reply(ctx context.Context, session Session, conversation Conversation, message Message, mode Mode) (SendResult, error) {
	if err := ctx.Err(); err != nil {
		return SendResult{Status: SendFailed}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isPausedLocked() {
		return SendResult{Status: SendFailed}, ErrChannelPaused
	}

	if s.failSend {
		msg := s.sendErrorMsg
		if msg == "" {
			msg = "stub send failed"
		}
		return SendResult{
			Status:    SendFailed,
			MessageID: message.ID,
			Timestamp: time.Now().UTC(),
		}, errors.New(msg)
	}

	status := SendSimulated
	if mode == ModeDraft {
		status = SendDrafted
	} else if mode == ModeAutopilot {
		// En stub nunca sale a la red externa, pero registra verificacion simulada
		status = SendVerified
	}

	s.sentMessages = append(s.sentMessages, message)
	return SendResult{
		Status:    status,
		MessageID: "sim-msg-" + conversation.ID,
		Timestamp: time.Now().UTC(),
	}, nil
}
