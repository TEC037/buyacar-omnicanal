package channel

import (
	"fmt"
	"sync"
	"time"

	"buyacar.com.co/omnicanal/internal/escalate"
)

// ChannelHealthStatus representa el estado de salud y disponibilidad de un canal.
type ChannelHealthStatus string

const (
	// StatusHealthy indica que el canal opera con normalidad.
	StatusHealthy ChannelHealthStatus = "healthy"
	// StatusPaused indica que el canal fue pausado tras alcanzar el umbral de fallos consecutivos.
	StatusPaused ChannelHealthStatus = "paused"
)

const (
	// DefaultMaxConsecutiveFailures es el umbral estricto de fallos consecutivos (Regla 30-channels).
	DefaultMaxConsecutiveFailures = 3
	// EscalationReasonChannelAutoPaused es la razon de escalamiento ante auto-pausa.
	EscalationReasonChannelAutoPaused = "channel_auto_paused_consecutive_failures"
)

// VersionedSelector soporta un selector primario y selectores alternativos (fallbacks) en cascada.
// Permite failover inmediato ante cambios en el DOM o endpoints de canales omnicanal.
type VersionedSelector struct {
	ID          string   `json:"id"`
	Version     string   `json:"version"`
	Primary     string   `json:"primary"`
	Fallbacks   []string `json:"fallbacks"`
	ActiveIndex int      `json:"active_index"`
}

// NewVersionedSelector construye un selector versionado inicializado en el selector primario (ActiveIndex = -1).
func NewVersionedSelector(id, version, primary string, fallbacks ...string) VersionedSelector {
	return VersionedSelector{
		ID:          id,
		Version:     version,
		Primary:     primary,
		Fallbacks:   fallbacks,
		ActiveIndex: -1,
	}
}

// GetActiveSelector retorna la cadena del selector activo actual.
// Si ActiveIndex es negativo (-1), retorna Primary.
// Si ActiveIndex apunta a un fallback valido, retorna dicho fallback.
func (s *VersionedSelector) GetActiveSelector() string {
	if s.ActiveIndex >= 0 && s.ActiveIndex < len(s.Fallbacks) {
		return s.Fallbacks[s.ActiveIndex]
	}
	return s.Primary
}

// FailoverToNext avanza al siguiente selector de contingencia en la cadena de fallbacks.
// Retorna la nueva cadena del selector y true si hay un candidato disponible; si se agotaron, retorna ("", false).
func (s *VersionedSelector) FailoverToNext() (string, bool) {
	if s.ActiveIndex < 0 {
		if len(s.Fallbacks) > 0 {
			s.ActiveIndex = 0
			return s.Fallbacks[0], true
		}
		return "", false
	}
	next := s.ActiveIndex + 1
	if next < len(s.Fallbacks) {
		s.ActiveIndex = next
		return s.Fallbacks[next], true
	}
	s.ActiveIndex = len(s.Fallbacks) // marcamos agotado
	return "", false
}

// Reset restaura el selector al selector primario (ActiveIndex = -1).
func (s *VersionedSelector) Reset() {
	s.ActiveIndex = -1
}

// Resolve evalua los selectores contra un mapa de elementos rotos,
// retornando el selector funcional, su indice (-1 para Primary, >=0 para Fallback) y ok=true si se encontro uno disponible.
func (s *VersionedSelector) Resolve(brokenMap map[string]bool) (string, int, bool) {
	if brokenMap == nil || !brokenMap[s.Primary] {
		s.ActiveIndex = -1
		return s.Primary, -1, true
	}
	for i, fb := range s.Fallbacks {
		if !brokenMap[fb] {
			s.ActiveIndex = i
			return fb, i, true
		}
	}
	return "", -1, false
}

// ResolveFunc evalua los selectores usando un predicado de rotura arbitrario.
func (s *VersionedSelector) ResolveFunc(isBroken func(selector string) bool) (string, int, bool) {
	if isBroken == nil || !isBroken(s.Primary) {
		s.ActiveIndex = -1
		return s.Primary, -1, true
	}
	for i, fb := range s.Fallbacks {
		if !isBroken(fb) {
			s.ActiveIndex = i
			return fb, i, true
		}
	}
	return "", -1, false
}

// Clone produce una copia profunda e independiente del VersionedSelector.
func (s VersionedSelector) Clone() VersionedSelector {
	fb := make([]string, len(s.Fallbacks))
	copy(fb, s.Fallbacks)
	return VersionedSelector{
		ID:          s.ID,
		Version:     s.Version,
		Primary:     s.Primary,
		Fallbacks:   fb,
		ActiveIndex: s.ActiveIndex,
	}
}

// ResilienceTracker define el contrato de resiliencia, conteo de fallos consecutivos y auto-pausa de canal.
type ResilienceTracker interface {
	RecordSuccess(channelID string)
	RecordFailure(channelID string, reason string) (paused bool, consecutiveCount int)
	IsPaused(channelID string) bool
	SetPaused(channelID string, paused bool)
	GetConsecutiveFailures(channelID string) int
}

// MemoryResilienceTracker implementa ResilienceTracker de forma concurrente y segura con aislamiento por canal.
type MemoryResilienceTracker struct {
	mu                  sync.RWMutex
	consecutiveFailures map[string]int
	pausedState         map[string]bool
	lastError           map[string]string
	pausedAt            map[string]time.Time
	escQueue            *escalate.Queue
	maxConsecutive      int
}

// NewResilienceTracker crea una nueva instancia de MemoryResilienceTracker.
func NewResilienceTracker(escQueue *escalate.Queue) *MemoryResilienceTracker {
	return NewMemoryResilienceTracker(escQueue)
}

// NewMemoryResilienceTracker construye un tracker de resiliencia en memoria.
func NewMemoryResilienceTracker(escQueue *escalate.Queue) *MemoryResilienceTracker {
	return &MemoryResilienceTracker{
		consecutiveFailures: make(map[string]int),
		pausedState:         make(map[string]bool),
		lastError:           make(map[string]string),
		pausedAt:            make(map[string]time.Time),
		escQueue:            escQueue,
		maxConsecutive:      DefaultMaxConsecutiveFailures,
	}
}

// RecordSuccess restablece el contador de fallos consecutivos a 0 para el canal indicado.
// Mantiene estricto aislamiento entre canales.
func (rt *MemoryResilienceTracker) RecordSuccess(channelID string) {
	if channelID == "" {
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.consecutiveFailures[channelID] = 0
}

// RecordFailure incrementa el contador de fallos consecutivos para el canal.
// Estrictamente al alcanzar el umbral (3 fallos consecutivos), pausa el canal y encola una alerta
// de emergencia en escalate.Queue para operadores humanos.
// Suprime tormentas de alertas: fallos posteriores sobre un canal ya pausado no duplican alertas.
func (rt *MemoryResilienceTracker) RecordFailure(channelID string, reason string) (bool, int) {
	if channelID == "" {
		return false, 0
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	rt.consecutiveFailures[channelID]++
	count := rt.consecutiveFailures[channelID]
	rt.lastError[channelID] = reason
	justPaused := false

	if count >= rt.maxConsecutive {
		if !rt.pausedState[channelID] {
			rt.pausedState[channelID] = true
			rt.pausedAt[channelID] = time.Now().UTC()
			justPaused = true
			if rt.escQueue != nil {
				rt.escQueue.Enqueue(
					channelID,
					"channel-system",
					EscalationReasonChannelAutoPaused,
					fmt.Sprintf("Channel %s auto-paused after %d consecutive failures: %s", channelID, count, reason),
					time.Now().UTC(),
				)
			}
		}
	}

	return rt.pausedState[channelID] || justPaused, count
}

// IsPaused indica si el canal especificado esta actualmente pausado.
func (rt *MemoryResilienceTracker) IsPaused(channelID string) bool {
	if channelID == "" {
		return false
	}
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.pausedState[channelID]
}

// SetPaused permite a los operadores humanos pausar o reanudar manualmente un canal.
// Al reanudar (paused = false), restablece el contador de fallos consecutivos a 0.
func (rt *MemoryResilienceTracker) SetPaused(channelID string, paused bool) {
	if channelID == "" {
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.pausedState[channelID] = paused
	if !paused {
		rt.consecutiveFailures[channelID] = 0
		delete(rt.lastError, channelID)
		delete(rt.pausedAt, channelID)
	} else if rt.pausedAt[channelID].IsZero() {
		rt.pausedAt[channelID] = time.Now().UTC()
	}
}

// GetConsecutiveFailures consulta la cantidad actual de fallos consecutivos acumulados en un canal.
func (rt *MemoryResilienceTracker) GetConsecutiveFailures(channelID string) int {
	if channelID == "" {
		return 0
	}
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.consecutiveFailures[channelID]
}

// GetStatus retorna el estado de salud ("healthy" o "paused") del canal.
func (rt *MemoryResilienceTracker) GetStatus(channelID string) ChannelHealthStatus {
	if rt.IsPaused(channelID) {
		return StatusPaused
	}
	return StatusHealthy
}

// GetLastError consulta el ultimo mensaje de error registrado para un canal.
func (rt *MemoryResilienceTracker) GetLastError(channelID string) string {
	if channelID == "" {
		return ""
	}
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.lastError[channelID]
}

// GetPausedAt consulta la marca de tiempo UTC en que se activo la auto-pausa del canal.
func (rt *MemoryResilienceTracker) GetPausedAt(channelID string) time.Time {
	if channelID == "" {
		return time.Time{}
	}
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.pausedAt[channelID]
}

// Resume reanuda un canal pausado, reseteando sus contadores de fallos.
func (rt *MemoryResilienceTracker) Resume(channelID string) {
	rt.SetPaused(channelID, false)
}

// Reset elimina el estado de fallos de un canal especifico o de todos si channelID es vacio.
func (rt *MemoryResilienceTracker) Reset(channelID string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if channelID == "" {
		rt.consecutiveFailures = make(map[string]int)
		rt.pausedState = make(map[string]bool)
		rt.lastError = make(map[string]string)
		rt.pausedAt = make(map[string]time.Time)
	} else {
		delete(rt.consecutiveFailures, channelID)
		delete(rt.pausedState, channelID)
		delete(rt.lastError, channelID)
		delete(rt.pausedAt, channelID)
	}
}
