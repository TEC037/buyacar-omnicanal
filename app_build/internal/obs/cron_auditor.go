package obs

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"buyacar.com.co/omnicanal/internal/guard"
)

// KaizenCronStatus clasifica el veredicto de una ejecucion de cron Kaizen segun 40-kaizen.md.
type KaizenCronStatus string

const (
	KaizenStatusImproved  KaizenCronStatus = "improved"
	KaizenStatusUnchanged KaizenCronStatus = "unchanged"
	KaizenStatusRegressed KaizenCronStatus = "regressed"
)

const (
	// MaxFastPathP99TargetMs es el techo innegociable de latencia p99 del Fast-Path (5 ms).
	MaxFastPathP99TargetMs = 5.0
	// DefaultTokenBudgetPerCronRun define el presupuesto maximo saludable inicial de tokens por corrida de cron.
	DefaultTokenBudgetPerCronRun = 3500
	// MinCalibratedTokenBudget evita que la autocalibracion reduzca el presupuesto por debajo del minimo operativo.
	MinCalibratedTokenBudget = 400
	// MinCalibratedLatencyTargetMs evita que la autocalibracion de latencia baje de un umbral realista en Windows.
	MinCalibratedLatencyTargetMs = 0.015
	// ExpectedActiveSchedulesCount define el cron maestro unificado obligatorio.
	ExpectedActiveSchedulesCount = 1
)

// KaizenCronRunRecord representa la ejecucion individual de un cron en segundo plano.
type KaizenCronRunRecord struct {
	TaskID           string    `json:"task_id"`
	CronExpression   string    `json:"cron_expression"`
	Iteration        int       `json:"iteration"`
	TimestampUTC     time.Time `json:"timestamp_utc"`
	TestsPassed      bool      `json:"tests_passed"`
	GoldenCasesOK    int       `json:"golden_cases_ok"`
	FastPathP99Ms    float64   `json:"fast_path_p99_ms"`
	TokensConsumed   int       `json:"tokens_consumed"`
	SecretsOrPIILeak bool      `json:"secrets_or_pii_leak"`
	Summary          string    `json:"summary"`
}

// SelfCalibrationSnapshot registra como el auditor se automejora y recalibra sus propios umbrales.
type SelfCalibrationSnapshot struct {
	CalibrationEpoch          int     `json:"calibration_epoch"`
	CalibratedMaxTokensPerRun int     `json:"calibrated_max_tokens_per_run"`
	CalibratedLatencyTargetMs float64 `json:"calibrated_latency_target_ms"`
	ObservedAvgTokensPerRun   int     `json:"observed_avg_tokens_per_run"`
	ObservedMaxP99Ms          float64 `json:"observed_max_p99_ms"`
	NextSelfImprovement       string  `json:"next_self_improvement"`
}

// KaizenCronAuditVerdict contiene el veredicto del auditor de crones Kaizen y su estado de autocalibracion.
type KaizenCronAuditVerdict struct {
	AuditedAtUTC      string                  `json:"audited_at_utc"`
	TotalRunsAudited  int                     `json:"total_runs_audited"`
	HealthyCronsCount int                     `json:"healthy_crons_count"`
	Status            KaizenCronStatus        `json:"status"`
	TokenBudgetOK     bool                    `json:"token_budget_ok"`
	LatencyGateOK     bool                    `json:"latency_gate_ok"`
	SecurityGateOK    bool                    `json:"security_gate_ok"`
	DeadManSwitchOK   bool                    `json:"dead_man_switch_ok"`
	TotalTokensUsed   int                     `json:"total_tokens_used"`
	RecommendedAction string                  `json:"recommended_action"`
	RegressionsFound  []string                `json:"regressions_found"`
	Calibration       SelfCalibrationSnapshot `json:"calibration"`
}

// KaizenCronAuditor supervisa los crones de mejora continua Kaizen, puertas de calidad MVP
// y ejecuta autocalibracion y automejora continua de sus propios umbrales.
type KaizenCronAuditor struct {
	mu                  sync.RWMutex
	maxInactivityWindow time.Duration
	maxTokensPerRun     int
	latencyTargetMs     float64
	calibrationEpoch    int
	runs                []KaizenCronRunRecord
	lastByTask          map[string]KaizenCronRunRecord
}

// NewKaizenCronAuditor crea un auditor de crones Kaizen con ventana de latido (Dead-Man's Switch) y presupuesto de tokens.
func NewKaizenCronAuditor(maxInactivityMinutes, maxTokensPerRun int) *KaizenCronAuditor {
	if maxInactivityMinutes <= 0 {
		maxInactivityMinutes = 65
	}
	if maxTokensPerRun <= 0 {
		maxTokensPerRun = DefaultTokenBudgetPerCronRun
	}
	return &KaizenCronAuditor{
		maxInactivityWindow: time.Duration(maxInactivityMinutes) * time.Minute,
		maxTokensPerRun:     maxTokensPerRun,
		latencyTargetMs:     MaxFastPathP99TargetMs,
		lastByTask:          make(map[string]KaizenCronRunRecord),
	}
}

// RecordRun registra la ejecucion de un cron sanitizando el resumen.
func (a *KaizenCronAuditor) RecordRun(rec KaizenCronRunRecord) {
	a.mu.Lock()
	defer a.mu.Unlock()
	rec.Summary, _ = RedactSummaryPII(rec.Summary)
	a.runs = append(a.runs, rec)
	a.lastByTask[rec.TaskID] = rec
}

// RedactSummaryPII garantiza cero PII en los reportes del auditor de crones.
func RedactSummaryPII(text string) (string, int) {
	sanitized, count := guard.RedactPII(text)
	return strings.TrimSpace(sanitized), count
}

// AutoCalibrateAndSelfImprove analiza el historial de corridas saludables y ajusta dinamicamente
// el presupuesto de tokens (regla 50-token-economy.md) y el objetivo de latencia p99 (regla 40-kaizen.md),
// devolviendo el snapshot de autocalibracion y la siguiente hipotesis de automejora del propio auditor.
func (a *KaizenCronAuditor) AutoCalibrateAndSelfImprove() SelfCalibrationSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()

	healthyCount := 0
	totalTok := 0
	maxP99 := 0.0

	for _, r := range a.runs {
		if r.TestsPassed && r.GoldenCasesOK >= 64 && !r.SecretsOrPIILeak && r.FastPathP99Ms < MaxFastPathP99TargetMs {
			healthyCount++
			totalTok += r.TokensConsumed
			if r.FastPathP99Ms > maxP99 {
				maxP99 = r.FastPathP99Ms
			}
		}
	}

	avgTok := 0
	if healthyCount > 0 {
		avgTok = totalTok / healthyCount
	}

	// Si hay al menos 2 corridas saludables, recalibrar umbrales hacia mayor exigencia (Kaizen continuo)
	if healthyCount >= 2 {
		a.calibrationEpoch++
		// Ajustar techo de tokens al 125% del promedio observado (con piso en MinCalibratedTokenBudget)
		proposedBudget := (avgTok * 125) / 100
		if proposedBudget < MinCalibratedTokenBudget {
			proposedBudget = MinCalibratedTokenBudget
		}
		if proposedBudget < a.maxTokensPerRun {
			a.maxTokensPerRun = proposedBudget
		}

		// Ajustar techo de latencia al doble del maximo p99 observado (con piso en MinCalibratedLatencyTargetMs y techo en 5.0 ms)
		proposedLatency := maxP99 * 2.0
		if proposedLatency < MinCalibratedLatencyTargetMs {
			proposedLatency = MinCalibratedLatencyTargetMs
		}
		if proposedLatency < a.latencyTargetMs {
			a.latencyTargetMs = proposedLatency
		}
	}

	nextImprovement := "tighten_token_budget_and_verify_cached_test_execution"
	if a.maxTokensPerRun <= MinCalibratedTokenBudget {
		nextImprovement = "maintain_minimum_token_floor_and_audit_draft_exit_gate_progress"
	}

	return SelfCalibrationSnapshot{
		CalibrationEpoch:          a.calibrationEpoch,
		CalibratedMaxTokensPerRun: a.maxTokensPerRun,
		CalibratedLatencyTargetMs: a.latencyTargetMs,
		ObservedAvgTokensPerRun:   avgTok,
		ObservedMaxP99Ms:          maxP99,
		NextSelfImprovement:       nextImprovement,
	}
}

// Audit evalua todos los crones registrados frente a las reglas 00-safety.md, 40-kaizen.md y 50-token-economy.md.
func (a *KaizenCronAuditor) Audit(now time.Time) KaizenCronAuditVerdict {
	a.mu.RLock()
	defer a.mu.RUnlock()

	healthyCount := 0
	totalTok := 0
	maxP99 := 0.0
	for _, r := range a.runs {
		if r.TestsPassed && r.GoldenCasesOK >= 64 && !r.SecretsOrPIILeak {
			healthyCount++
			totalTok += r.TokensConsumed
			if r.FastPathP99Ms > maxP99 {
				maxP99 = r.FastPathP99Ms
			}
		}
	}
	avgTok := 0
	if healthyCount > 0 {
		avgTok = totalTok / healthyCount
	}

	verdict := KaizenCronAuditVerdict{
		AuditedAtUTC:     now.UTC().Format(time.RFC3339),
		TotalRunsAudited: len(a.runs),
		TokenBudgetOK:    true,
		LatencyGateOK:    true,
		SecurityGateOK:   true,
		DeadManSwitchOK:  len(a.lastByTask) > 0,
		Status:           KaizenStatusUnchanged,
		Calibration: SelfCalibrationSnapshot{
			CalibrationEpoch:          a.calibrationEpoch,
			CalibratedMaxTokensPerRun: a.maxTokensPerRun,
			CalibratedLatencyTargetMs: a.latencyTargetMs,
			ObservedAvgTokensPerRun:   avgTok,
			ObservedMaxP99Ms:          maxP99,
			NextSelfImprovement:       "tighten_token_budget_and_verify_cached_test_execution",
		},
	}

	if len(a.runs) == 0 {
		verdict.Status = KaizenStatusRegressed
		verdict.RegressionsFound = append(verdict.RegressionsFound, "no_cron_runs_recorded")
		verdict.RecommendedAction = "stop_and_verify_cron_scheduler"
		return verdict
	}

	for _, r := range a.runs {
		verdict.TotalTokensUsed += r.TokensConsumed
		if !r.TestsPassed || r.GoldenCasesOK < 64 {
			verdict.Status = KaizenStatusRegressed
			verdict.RegressionsFound = append(verdict.RegressionsFound, fmt.Sprintf("task %s iter %d failed tests or golden cases (%d/64)", r.TaskID, r.Iteration, r.GoldenCasesOK))
		}
		if r.FastPathP99Ms >= a.latencyTargetMs || r.FastPathP99Ms < 0 {
			verdict.LatencyGateOK = false
			verdict.Status = KaizenStatusRegressed
			verdict.RegressionsFound = append(verdict.RegressionsFound, fmt.Sprintf("task %s iter %d exceeded calibrated fast-path p99 target: %.3f ms >= %.3f ms", r.TaskID, r.Iteration, r.FastPathP99Ms, a.latencyTargetMs))
		}
		if r.TokensConsumed > a.maxTokensPerRun {
			verdict.TokenBudgetOK = false
			verdict.Status = KaizenStatusRegressed
			verdict.RegressionsFound = append(verdict.RegressionsFound, fmt.Sprintf("task %s iter %d exceeded calibrated token budget: %d > %d", r.TaskID, r.Iteration, r.TokensConsumed, a.maxTokensPerRun))
		}
		if r.SecretsOrPIILeak {
			verdict.SecurityGateOK = false
			verdict.Status = KaizenStatusRegressed
			verdict.RegressionsFound = append(verdict.RegressionsFound, fmt.Sprintf("task %s iter %d reported secrets or PII leak", r.TaskID, r.Iteration))
		}
	}

	for taskID, last := range a.lastByTask {
		if now.Sub(last.TimestampUTC) > a.maxInactivityWindow {
			verdict.DeadManSwitchOK = false
			verdict.Status = KaizenStatusRegressed
			verdict.RegressionsFound = append(verdict.RegressionsFound, fmt.Sprintf("cron %s missed heartbeat window (%v)", taskID, a.maxInactivityWindow))
		} else {
			verdict.HealthyCronsCount++
		}
	}

	if len(verdict.RegressionsFound) == 0 {
		if len(a.runs) >= 2 {
			verdict.Status = KaizenStatusImproved
		} else {
			verdict.Status = KaizenStatusUnchanged
		}
		verdict.RecommendedAction = "keep_active_and_continue_draft_supervision"
	} else {
		verdict.RecommendedAction = "open_corrective_action_and_rollback_if_critical"
	}

	return verdict
}

// ExportMarkdown genera un reporte compacto de auditoria y autocalibracion de crones Kaizen para production_artifacts/.
func (a *KaizenCronAuditor) ExportMarkdown(now time.Time) string {
	v := a.Audit(now)
	a.mu.RLock()
	defer a.mu.RUnlock()

	var b strings.Builder
	b.WriteString("# Auditoría y Autocalibración de Crones Kaizen PDCA — BuyaCAR Omnicanal\n\n")
	b.WriteString(fmt.Sprintf("- **Fecha de Auditoría (UTC)**: `%s`\n", v.AuditedAtUTC))
	b.WriteString(fmt.Sprintf("- **Veredicto Global**: `%s`\n", v.Status))
	b.WriteString(fmt.Sprintf("- **Crones con Latido Saludable**: `%d`\n", v.HealthyCronsCount))
	b.WriteString(fmt.Sprintf("- **Corridas Auditadas**: `%d` | **Tokens Totales**: `%d`\n", v.TotalRunsAudited, v.TotalTokensUsed))
	b.WriteString(fmt.Sprintf("- **Puertas**: Seguridad=`%v` | Latencia p99=`%v` | Presupuesto Tokens=`%v` | Dead-Man Switch=`%v`\n",
		v.SecurityGateOK, v.LatencyGateOK, v.TokenBudgetOK, v.DeadManSwitchOK))
	b.WriteString(fmt.Sprintf("- **Autocalibración (Época %d)**: Techo Tokens Calibrado=`%d` (Prom. observado=`%d`) | Techo Latencia Calibrado=`%.4f ms` (Máx. observado=`%.4f ms`)\n",
		v.Calibration.CalibrationEpoch, v.Calibration.CalibratedMaxTokensPerRun, v.Calibration.ObservedAvgTokensPerRun, v.Calibration.CalibratedLatencyTargetMs, v.Calibration.ObservedMaxP99Ms))
	b.WriteString(fmt.Sprintf("- **Siguiente Automejora del Auditor**: `%s`\n", v.Calibration.NextSelfImprovement))
	b.WriteString(fmt.Sprintf("- **Acción Recomendada**: `%s`\n\n", v.RecommendedAction))

	b.WriteString("| Task ID | Expresión Cron | Iteración | Tests + 64 Golden | Fast-Path p99 (ms) | Tokens | Timestamp UTC |\n")
	b.WriteString("|---|---|---:|---|---:|---:|---|\n")
	for _, r := range a.runs {
		b.WriteString(fmt.Sprintf("| `%s` | `%s` | %d | `%v (%d/64)` | %.4f | %d | `%s` |\n",
			r.TaskID, r.CronExpression, r.Iteration, r.TestsPassed, r.GoldenCasesOK, r.FastPathP99Ms, r.TokensConsumed, r.TimestampUTC.UTC().Format(time.RFC3339)))
	}
	return b.String()
}
