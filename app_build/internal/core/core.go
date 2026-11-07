package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"buyacar.com.co/omnicanal/internal/channel"
	"buyacar.com.co/omnicanal/internal/store"
)

var (
	ErrKillSwitchActive = errors.New("core: emergency kill switch is active")
	ErrOwnLastMessage   = errors.New("core: skipped because last message is own")
	ErrAlreadyProcessed = errors.New("core: skipped because message hash was already processed")
	ErrOlderThan9Weeks  = errors.New("core: skipped because conversation is older than 9 weeks")
)

// Config encapsula los parametros iniciales seguros del sistema.
type Config struct {
	OperatingMode       channel.Mode
	EmergencyKillSwitch bool
	EnableFastPath      bool
	ConfidenceThreshold float64
	SkipOlderThanWeeks  int
	LocalBindAddress    string
	MaxMessagesPerHour  int
	MaxMessagesPerDay   int
}

// DefaultSafeConfig retorna la configuracion por defecto arrancando en SIMULATION.
func DefaultSafeConfig() Config {
	return Config{
		OperatingMode:       channel.ModeSimulation,
		EmergencyKillSwitch: false,
		EnableFastPath:      true,
		ConfidenceThreshold: 0.80,
		SkipOlderThanWeeks:  9,
		LocalBindAddress:    "127.0.0.1",
		MaxMessagesPerHour:  120,
		MaxMessagesPerDay:   250,
	}
}

// DefaultDraftConfig retorna la configuracion activa autorizada por orden humana para Etapa B (DRAFT).
// En modo DRAFT se consumen datos reales de lectura y todas las respuestas aprobadas se registran como borradores (draft_recorded).
func DefaultDraftConfig() Config {
	cfg := DefaultSafeConfig()
	cfg.OperatingMode = channel.ModeDraft
	return cfg
}

// Engine gestiona el registro de canales, kill switch global y estado de ejecucion.
type Engine struct {
	mu         sync.RWMutex
	cfg        Config
	killSwitch atomic.Bool
	channels   map[string]channel.Channel
	store      store.Store
}

func NewEngine(cfg Config, st store.Store) *Engine {
	if cfg.OperatingMode == "" {
		cfg.OperatingMode = channel.ModeSimulation
	}
	e := &Engine{
		cfg:      cfg,
		channels: make(map[string]channel.Channel),
		store:    st,
	}
	e.killSwitch.Store(cfg.EmergencyKillSwitch)
	return e
}

// TriggerKillSwitch activa el paro de emergencia inmediatamente (< 1s).
func (e *Engine) TriggerKillSwitch() {
	e.killSwitch.Store(true)
}

// SetKillSwitch activa o desactiva el paro de emergencia de forma atómica.
func (e *Engine) SetKillSwitch(active bool) {
	e.killSwitch.Store(active)
}

// IsKillSwitchActive verifica si el kill switch esta activo.
func (e *Engine) IsKillSwitchActive() bool {
	return e.killSwitch.Load()
}

// SetOperatingMode actualiza el modo operativo (SIMULATION, DRAFT, AUTOPILOT) de forma thread-safe bajo autorización humana.
func (e *Engine) SetOperatingMode(mode channel.Mode) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cfg.OperatingMode = mode
}

// OperatingMode retorna el modo operativo vigente.
func (e *Engine) OperatingMode() channel.Mode {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cfg.OperatingMode
}

// RegisterChannel anade un adaptador sin modificar la logica interna del core.
func (e *Engine) RegisterChannel(ch channel.Channel) error {
	if ch == nil || ch.ID() == "" {
		return fmt.Errorf("core: invalid channel")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.channels[ch.ID()] = ch
	return nil
}

// ShouldProcessThread evalua las reglas de omision del flujo por conversacion (paso 2).
func (e *Engine) ShouldProcessThread(ctx context.Context, th channel.Thread, now time.Time) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if e.IsKillSwitchActive() {
		return "", ErrKillSwitchActive
	}
	if len(th.Messages) == 0 {
		return "", fmt.Errorf("core: empty thread")
	}

	lastMsg := th.Messages[len(th.Messages)-1]
	if lastMsg.Sender == "own" {
		return "", ErrOwnLastMessage
	}

	maxAge := time.Duration(e.cfg.SkipOlderThanWeeks) * 7 * 24 * time.Hour
	if maxAge > 0 && now.Sub(lastMsg.Timestamp) > maxAge {
		return "", ErrOlderThan9Weeks
	}

	hashInput := lastMsg.Text
	if len(th.Messages) > 1 && lastMsg.ID != "" {
		hashInput = lastMsg.ID + ":" + lastMsg.Text
	}
	hash := e.store.ComputeHash(th.Conversation.ChannelID, th.Conversation.ID, hashInput)
	if processed, _ := e.store.HasProcessed(hash); processed {
		return hash, ErrAlreadyProcessed
	}

	return hash, nil
}
