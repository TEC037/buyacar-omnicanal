package channel

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

// RateConfig define los parametros de cuota y pausas humanas para la capa de canal.
type RateConfig struct {
	MaxMessagesPerHour int           // Limite maximo por hora (default: 120)
	MaxMessagesPerDay  int           // Limite maximo por dia (default: 250)
	ChatDelayBase      time.Duration // Pausa base entre conversaciones (default: 3200ms)
	ChannelDelayBase   time.Duration // Pausa base entre canales (default: 2500ms)
	JitterPercent      float64       // Porcentaje de jitter determinista (default: 0.30 => ±30%)
}

// DefaultRateConfig retorna la configuracion canonica segun instructivo.md y BUYACAR_FINAL_INTEGRATED.md.
func DefaultRateConfig() RateConfig {
	return RateConfig{
		MaxMessagesPerHour: 120,
		MaxMessagesPerDay:  250,
		ChatDelayBase:      3200 * time.Millisecond,
		ChannelDelayBase:   2500 * time.Millisecond,
		JitterPercent:      0.30,
	}
}

// RateController define el contrato de control de tasa, calculo de jitter y esperas cancelables.
type RateController interface {
	CanSend(channelID string, now time.Time) (bool, string)
	RecordSend(channelID string, now time.Time)
	CalculateChatDelay(seed int64) time.Duration
	CalculateChannelDelay(seed int64) time.Duration
	WaitWithContext(ctx context.Context, delay time.Duration) error
}

// MemoryRateController implementa RateController de manera segura en memoria con ventana deslizante.
type MemoryRateController struct {
	mu      sync.RWMutex
	cfg     RateConfig
	history map[string][]time.Time
}

// NewRateController instancia un nuevo RateController con la configuracion provista.
func NewRateController(cfg RateConfig) *MemoryRateController {
	return NewMemoryRateController(cfg)
}

// NewMemoryRateController construye un controlador en memoria aplicando valores por defecto si son invalidos.
func NewMemoryRateController(cfg RateConfig) *MemoryRateController {
	if cfg.MaxMessagesPerHour <= 0 {
		cfg.MaxMessagesPerHour = 120
	}
	if cfg.MaxMessagesPerDay <= 0 {
		cfg.MaxMessagesPerDay = 250
	}
	if cfg.ChatDelayBase <= 0 {
		cfg.ChatDelayBase = 3200 * time.Millisecond
	}
	if cfg.ChannelDelayBase <= 0 {
		cfg.ChannelDelayBase = 2500 * time.Millisecond
	}
	if cfg.JitterPercent <= 0 {
		cfg.JitterPercent = 0.30
	}
	return &MemoryRateController{
		cfg:     cfg,
		history: make(map[string][]time.Time),
	}
}

// CanSend evalua si el canal especificado tiene cuota disponible bajo las ventanas deslizantes de 1h y 24h.
// Poda marcas de tiempo obsoletas (> 24h) para mantener el uso de memoria acotado.
func (rc *MemoryRateController) CanSend(channelID string, now time.Time) (bool, string) {
	if channelID == "" {
		return false, "invalid_channel_id"
	}
	if now.IsZero() {
		now = time.Now()
	}

	rc.mu.Lock()
	defer rc.mu.Unlock()

	timestamps := rc.history[channelID]
	oneHourAgo := now.Add(-1 * time.Hour)
	oneDayAgo := now.Add(-24 * time.Hour)

	var validTimestamps []time.Time
	hourCount := 0
	dayCount := 0

	for _, ts := range timestamps {
		if ts.After(oneDayAgo) {
			validTimestamps = append(validTimestamps, ts)
			dayCount++
			if ts.After(oneHourAgo) {
				hourCount++
			}
		}
	}
	rc.history[channelID] = validTimestamps

	if hourCount >= rc.cfg.MaxMessagesPerHour {
		return false, "hourly_rate_limit_exceeded"
	}
	if dayCount >= rc.cfg.MaxMessagesPerDay {
		return false, "daily_rate_limit_exceeded"
	}
	return true, ""
}

// RecordSend asienta un envio exitoso en el historial del canal.
func (rc *MemoryRateController) RecordSend(channelID string, now time.Time) {
	if channelID == "" {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}

	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.history[channelID] = append(rc.history[channelID], now)
}

// CalculateChatDelay calcula la pausa entre conversaciones aplicando ±JitterPercent determinista.
// Para ChatDelayBase=3200ms y Jitter=30%, el rango es estrictamente [2240ms, 4160ms].
func (rc *MemoryRateController) CalculateChatDelay(seed int64) time.Duration {
	return rc.calculateDelayWithJitter(rc.cfg.ChatDelayBase, seed)
}

// CalculateChannelDelay calcula la pausa entre cambios de canal aplicando ±JitterPercent determinista.
// Para ChannelDelayBase=2500ms y Jitter=30%, el rango es estrictamente [1750ms, 3250ms].
func (rc *MemoryRateController) CalculateChannelDelay(seed int64) time.Duration {
	return rc.calculateDelayWithJitter(rc.cfg.ChannelDelayBase, seed)
}

// calculateDelayWithJitter mapea deterministicamente un seed a una fraccion en [-JitterPercent, +JitterPercent].
func (rc *MemoryRateController) calculateDelayWithJitter(base time.Duration, seed int64) time.Duration {
	return CalculateDeterministicJitter(base, rc.cfg.JitterPercent, seed)
}

// CalculateDeterministicJitter genera una duracion con jitter determinista usando SHA-256 sobre el seed.
// Garantiza 100% de reproducibilidad, libre de condiciones de carrera y sin depender de estado pseudoaleatorio global.
func CalculateDeterministicJitter(base time.Duration, jitterPercent float64, seed int64) time.Duration {
	h := sha256.Sum256([]byte(fmt.Sprintf("jitter-salt-%d", seed)))
	val := int(h[0])<<8 | int(h[1])                // 0 .. 65535
	normalized := (float64(val)/65535.0)*2.0 - 1.0 // -1.0 .. +1.0
	jitterFraction := normalized * jitterPercent
	delayNs := float64(base.Nanoseconds()) * (1.0 + jitterFraction)
	return time.Duration(delayNs)
}

// WaitWithContext espera de forma asincrona la duracion indicada, abortando de inmediato si ctx se cancela.
// Permite que el scheduler del daemon responda a apagados de emergencia (< 1s) o kill switches.
func (rc *MemoryRateController) WaitWithContext(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// GetCounts consulta los contadores actuales de mensajes en la ultima hora y en las ultimas 24 horas.
func (rc *MemoryRateController) GetCounts(channelID string, now time.Time) (hourly int, daily int) {
	if channelID == "" {
		return 0, 0
	}
	if now.IsZero() {
		now = time.Now()
	}

	rc.mu.RLock()
	defer rc.mu.RUnlock()

	timestamps := rc.history[channelID]
	oneHourAgo := now.Add(-1 * time.Hour)
	oneDayAgo := now.Add(-24 * time.Hour)

	for _, ts := range timestamps {
		if ts.After(oneDayAgo) {
			daily++
			if ts.After(oneHourAgo) {
				hourly++
			}
		}
	}
	return hourly, daily
}

// Reset elimina el historial registrado para un canal, o de todos si channelID es vacio.
func (rc *MemoryRateController) Reset(channelID string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if channelID == "" {
		rc.history = make(map[string][]time.Time)
	} else {
		delete(rc.history, channelID)
	}
}
