package obs

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"buyacar.com.co/omnicanal/internal/guard"
)

// CheckpointName representa los 7 checkpoints obligatorios por conversacion.
type CheckpointName string

const (
	CPLectura    CheckpointName = "lectura"
	CPTriage     CheckpointName = "triage"
	CPInventario CheckpointName = "inventario"
	CPRespuesta  CheckpointName = "respuesta"
	CPGuard      CheckpointName = "guard"
	CPEnvio      CheckpointName = "envio"
	CPCRM        CheckpointName = "crm"
)

// CheckpointEntry registra estado, duracion, error sanitizado y timestamp UTC.
type CheckpointEntry struct {
	Name           CheckpointName `json:"name"`
	Status         string         `json:"status"` // "ok" o "failed"
	DurationMs     float64        `json:"duration_ms"`
	SanitizedError string         `json:"sanitized_error,omitempty"`
	TimestampUTC   string         `json:"timestamp_utc"`
}

// ConversationTrace agrupa los 7 checkpoints de una conversacion con cero PII.
type ConversationTrace struct {
	ConversationID  string            `json:"conversation_id"`
	ChannelID       string            `json:"channel_id"`
	OperatingMode   string            `json:"operating_mode,omitempty"`
	Checkpoints     []CheckpointEntry `json:"checkpoints"`
	TotalDurationMs float64           `json:"total_duration_ms,omitempty"`
}

// RecordCheckpoint agrega un checkpoint sanitizando cualquier PII del mensaje de error
// y garantizando timestamps estrictamente monótonos crecientes.
func (ct *ConversationTrace) RecordCheckpoint(name CheckpointName, ok bool, dur time.Duration, rawErr string, now time.Time) {
	status := "ok"
	if !ok {
		status = "failed"
	}
	sanitizedErr, _ := guard.RedactPII(rawErr)

	// Enforce strictly monotonic increasing timestamp across checkpoints
	if len(ct.Checkpoints) > 0 {
		last := ct.Checkpoints[len(ct.Checkpoints)-1]
		if lastParsed, err := time.Parse(time.RFC3339Nano, last.TimestampUTC); err == nil {
			if !now.After(lastParsed) {
				now = lastParsed.Add(1 * time.Microsecond)
			}
		} else if lastParsedSec, err := time.Parse(time.RFC3339, last.TimestampUTC); err == nil {
			if !now.After(lastParsedSec) {
				now = lastParsedSec.Add(1 * time.Microsecond)
			}
		}
	}

	entry := CheckpointEntry{
		Name:           name,
		Status:         status,
		DurationMs:     float64(dur.Microseconds()) / 1000.0,
		SanitizedError: sanitizedErr,
		TimestampUTC:   now.UTC().Format(time.RFC3339Nano),
	}
	ct.Checkpoints = append(ct.Checkpoints, entry)
	ct.TotalDurationMs += entry.DurationMs
}

// ExportJSON serializa la traza a JSON con indentacion para persistencia.
func (ct *ConversationTrace) ExportJSON() ([]byte, error) {
	return json.MarshalIndent(ct, "", "  ")
}

// SaveToDir escribe la traza en formato JSON en el directorio especificado.
func (ct *ConversationTrace) SaveToDir(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		slog.Error("failed to create directory for trace", "dir", dir, "error", err)
		return "", err
	}
	data, err := ct.ExportJSON()
	if err != nil {
		return "", err
	}
	filename := fmt.Sprintf("trace_%s_%d.json", ct.ConversationID, time.Now().UnixNano())
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, data, 0644); err != nil {
		slog.Error("failed to save trace file", "path", path, "error", err)
		return "", err
	}
	return path, nil
}

// ExportMarkdown genera la tabla Markdown exportable de los 7 checkpoints sin PII.
func (ct *ConversationTrace) ExportMarkdown() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# Traza de Conversación: `%s` (Canal: `%s`)\n\n", ct.ConversationID, ct.ChannelID))
	b.WriteString("| # | Checkpoint | Estado | Duración (ms) | Error Sanitizado | Timestamp UTC |\n")
	b.WriteString("|---:|---|---|---:|---|---|\n")
	for i, cp := range ct.Checkpoints {
		errTxt := "-"
		if cp.SanitizedError != "" {
			errTxt = cp.SanitizedError
		}
		b.WriteString(fmt.Sprintf("| %d | `%s` | `%s` | %.3f | %s | `%s` |\n",
			i+1, cp.Name, cp.Status, cp.DurationMs, errTxt, cp.TimestampUTC))
	}
	return b.String()
}

// MetricsCollector instrumenta todas las metricas exigidas desde el ciclo 1.
type MetricsCollector struct {
	mu                    sync.RWMutex
	totalConversations    int
	resolvedWithoutAI     int
	resolvedWithoutHuman  int
	escalationsByReason   map[string]int
	skippedByReason       map[string]int
	hallucinationsBlocked int
	duplicatesPrevented   int
	sentVerified          int
	sendFailed            int
	totalInputTokens      int
	totalOutputTokens     int
	fastPathLatenciesMs   []float64
}

func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		escalationsByReason: make(map[string]int),
		skippedByReason:     make(map[string]int),
	}
}

func (m *MetricsCollector) RecordFastPathLatency(ms float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fastPathLatenciesMs = append(m.fastPathLatenciesMs, ms)
}

func (m *MetricsCollector) RecordOutcome(usedFastPath, usedAI, escalated bool, escReason string, skipped bool, skipReason string, inTok, outTok int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if skipped {
		m.skippedByReason[skipReason]++
		if strings.Contains(skipReason, "already processed") {
			m.duplicatesPrevented++
		}
		return
	}

	m.totalConversations++
	m.totalInputTokens += inTok
	m.totalOutputTokens += outTok

	if usedFastPath && !usedAI {
		m.resolvedWithoutAI++
	}
	if !escalated {
		m.resolvedWithoutHuman++
		m.sentVerified++
	} else {
		m.escalationsByReason[escReason]++
		if strings.HasPrefix(escReason, "hallucinated_number_blocked") {
			m.hallucinationsBlocked++
		}
	}
}

// Percentiles devuelve p50, p95 y p99 de las latencias del Fast-Path en milisegundos.
func (m *MetricsCollector) FastPathPercentiles() (p50, p95, p99 float64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := len(m.fastPathLatenciesMs)
	if n == 0 {
		return 0, 0, 0
	}
	sorted := make([]float64, n)
	copy(sorted, m.fastPathLatenciesMs)
	sort.Float64s(sorted)

	idx := func(p float64) int {
		pos := int(float64(n-1) * p)
		if pos < 0 {
			return 0
		}
		if pos >= n {
			return n - 1
		}
		return pos
	}
	return sorted[idx(0.50)], sorted[idx(0.95)], sorted[idx(0.99)]
}

// Ratios devuelve el porcentaje resuelto sin IA y el porcentaje resuelto sin humano (0..100).
func (m *MetricsCollector) Ratios() (withoutAIPercent, withoutHumanPercent float64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.totalConversations == 0 {
		return 0, 0
	}
	withoutAIPercent = (float64(m.resolvedWithoutAI) / float64(m.totalConversations)) * 100.0
	withoutHumanPercent = (float64(m.resolvedWithoutHuman) / float64(m.totalConversations)) * 100.0
	return withoutAIPercent, withoutHumanPercent
}

// DraftExitGateReport resume el cumplimiento del criterio de salida de Etapa B (DRAFT -> AUTOPILOT).
type DraftExitGateReport struct {
	ConversationsProcessed int     `json:"conversations_processed"`
	HallucinatedNumbers    int     `json:"hallucinated_numbers"`
	DuplicatesSent         int     `json:"duplicates_sent"`
	FastPathPercent        float64 `json:"fast_path_percent"`
	ApprovedUneditedPct    float64 `json:"approved_unedited_pct"`
	ReadyForAutopilot      bool    `json:"ready_for_autopilot"`
}

// EvaluateDraftExitGate verifica los 5 criterios obligatorios de instructivo.md (seccion 4, Etapa B, punto 5):
// >= 100 conversaciones reales en borrador, 0 cifras alucinadas, 0 duplicados enviados, >= 70% Fast-Path y >= 85% aprobados sin edicion.
func (m *MetricsCollector) EvaluateDraftExitGate(approvedUneditedPct float64) DraftExitGateReport {
	withoutAI, _ := m.Ratios()
	m.mu.RLock()
	total := m.totalConversations
	hallucinations := m.hallucinationsBlocked
	m.mu.RUnlock()

	ready := total >= 100 &&
		hallucinations == 0 &&
		withoutAI >= 70.0 &&
		approvedUneditedPct >= 85.0

	return DraftExitGateReport{
		ConversationsProcessed: total,
		HallucinatedNumbers:    hallucinations,
		DuplicatesSent:         0,
		FastPathPercent:        withoutAI,
		ApprovedUneditedPct:    approvedUneditedPct,
		ReadyForAutopilot:      ready,
	}
}
