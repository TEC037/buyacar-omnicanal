package obs

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// KaizenImprovementDomain clasifica las areas donde el Cron Maestro puede auto-optimizarse.
type KaizenImprovementDomain string

const (
	DomainTokenCompression    KaizenImprovementDomain = "token_compression"
	DomainLatencyOptimization KaizenImprovementDomain = "latency_optimization"
	DomainDeadlockPrevention  KaizenImprovementDomain = "deadlock_prevention"
	DomainVerificationSpeed   KaizenImprovementDomain = "verification_speed"
)

// MasterCronImprovementRecord registra una mutacion o mejora aplicada autonomamente por el Cron Maestro sobre si mismo.
type MasterCronImprovementRecord struct {
	Epoch           int                     `json:"epoch"`
	TimestampUTC    time.Time               `json:"timestamp_utc"`
	Domain          KaizenImprovementDomain `json:"domain"`
	Hypothesis      string                  `json:"hypothesis"`
	MetricBefore    float64                 `json:"metric_before"`
	MetricAfter     float64                 `json:"metric_after"`
	PercentImproved float64                 `json:"percent_improved"`
	Status          KaizenCronStatus        `json:"status"`
	SafetyVerified  bool                    `json:"safety_verified"`
	Summary         string                  `json:"summary"`
}

// MasterCronSelfOptimizer encapsula el bucle OODA (Observe-Orient-Decide-Act) y Kaizen PDCA
// que permite al Cron Maestro automejorar sus propias rutinas, compresión y caché de verificación.
type MasterCronSelfOptimizer struct {
	mu                    sync.RWMutex
	currentEpoch          int
	consecutiveStableRuns int
	totalImprovements     int
	improvements          []MasterCronImprovementRecord
	cachedTestHashes      map[string]string
	maxBudgetCap          int
	baselineTokens        int
	baselineP99Ms         float64
	lastTokensUsed        int
	lastP99Ms             float64
}

// NewMasterCronSelfOptimizer construye el modulo Kaizen de automejora del Cron Maestro.
func NewMasterCronSelfOptimizer() *MasterCronSelfOptimizer {
	return &MasterCronSelfOptimizer{
		currentEpoch:     1,
		maxBudgetCap:     400,
		cachedTestHashes: make(map[string]string),
	}
}

// EvaluateAndSelfImprove ejecuta el ciclo Plan-Do-Check-Act del propio cron sobre si mismo:
// 1. Observa latencias, consumo de tokens y tasa de estabilidad.
// 2. Si y solo si existe una reduccion directamente medida en p99 o tokens, formula la hipotesis de mejora.
// 3. Prohibe formulas ficticias como reducciones artificiales del 10%; marca unchanged salvo medicion efectiva.
func (m *MasterCronSelfOptimizer) EvaluateAndSelfImprove(currentP99Ms float64, tokensUsed int, allTestsGreen bool, now time.Time) (MasterCronImprovementRecord, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !allTestsGreen {
		m.consecutiveStableRuns = 0
		return MasterCronImprovementRecord{
			Epoch:          m.currentEpoch,
			TimestampUTC:   now,
			Status:         KaizenStatusRegressed,
			SafetyVerified: false,
			Summary:        "self_improvement_blocked_tests_failing",
		}, false
	}

	// Primera corrida establece la línea base de métricas observadas
	if m.lastTokensUsed == 0 && m.lastP99Ms == 0 {
		m.lastTokensUsed = tokensUsed
		m.lastP99Ms = currentP99Ms
		m.baselineTokens = tokensUsed
		m.baselineP99Ms = currentP99Ms
		return MasterCronImprovementRecord{
			Epoch:          m.currentEpoch,
			TimestampUTC:   now,
			Status:         KaizenStatusUnchanged,
			SafetyVerified: true,
			Summary:        "baseline_established_monitoring_for_measured_improvements",
		}, false
	}

	m.consecutiveStableRuns++

	// Regla Anti-Vibecoding (40-kaizen.md y Directiva Suprema):
	// Prohibido aplicar fórmulas matemáticas ficticias de reducción del 10%.
	// Una mejora sólo se declara si proviene de una reducción real y directamente medida
	// en tokens consumidos o latencia p99 frente a la corrida anterior.
	tokensImproved := tokensUsed < m.lastTokensUsed && m.lastTokensUsed > 0
	latencyImproved := currentP99Ms < m.lastP99Ms && m.lastP99Ms > 0

	if (tokensImproved || latencyImproved) && m.consecutiveStableRuns >= 1 {
		m.currentEpoch++
		m.totalImprovements++
		m.consecutiveStableRuns = 0

		beforeVal := float64(m.lastTokensUsed)
		afterVal := float64(tokensUsed)
		domain := DomainTokenCompression
		summary := fmt.Sprintf("tokens_measured_reduced_from_%d_to_%d", m.lastTokensUsed, tokensUsed)

		if !tokensImproved && latencyImproved {
			beforeVal = m.lastP99Ms
			afterVal = currentP99Ms
			domain = DomainLatencyOptimization
			summary = fmt.Sprintf("fast_path_p99_measured_reduced_from_%.4fms_to_%.4fms", m.lastP99Ms, currentP99Ms)
		}

		pct := 0.0
		if beforeVal > 0 {
			pct = ((beforeVal - afterVal) / beforeVal) * 100.0
		}

		// Calibrar el techo de tokens basándose en el consumo real medido (piso seguro en 250 tokens)
		if tokensImproved {
			newCap := tokensUsed
			if newCap < 250 {
				newCap = 250
			}
			m.maxBudgetCap = newCap
		}

		rec := MasterCronImprovementRecord{
			Epoch:           m.currentEpoch,
			TimestampUTC:    now,
			Domain:          domain,
			Hypothesis:      fmt.Sprintf("mejora real y directamente medida en %s: antes=%.4f, despues=%.4f (ahorro %.1f%%)", domain, beforeVal, afterVal, pct),
			MetricBefore:    beforeVal,
			MetricAfter:     afterVal,
			PercentImproved: pct,
			Status:          KaizenStatusImproved,
			SafetyVerified:  true,
			Summary:         summary,
		}
		m.lastTokensUsed = tokensUsed
		m.lastP99Ms = currentP99Ms
		m.improvements = append(m.improvements, rec)
		return rec, true
	}

	// Sin cambio medible o sin reduccion real -> el estado DEBE ser strictly unchanged
	m.lastTokensUsed = tokensUsed
	m.lastP99Ms = currentP99Ms

	return MasterCronImprovementRecord{
		Epoch:          m.currentEpoch,
		TimestampUTC:   now,
		Status:         KaizenStatusUnchanged,
		SafetyVerified: true,
		Summary:        "metrics_stable_or_unchanged_no_fictitious_reduction",
	}, false
}

// CurrentBudgetCap devuelve el presupuesto maximo autocalibrado y automejorado vigente.
func (m *MasterCronSelfOptimizer) CurrentBudgetCap() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.maxBudgetCap
}

// ExportMarkdown genera el reporte visual de automejora continua del Cron Maestro.
func (m *MasterCronSelfOptimizer) ExportMarkdown() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var b strings.Builder
	b.WriteString("# Módulo Kaizen de Automejora del Cron Maestro — BuyaCAR Omnicanal\n\n")
	b.WriteString(fmt.Sprintf("- **Época de Automejora**: `%d`\n", m.currentEpoch))
	b.WriteString(fmt.Sprintf("- **Techo de Tokens Optimizado**: `%d tokens/corrida`\n", m.maxBudgetCap))
	b.WriteString(fmt.Sprintf("- **Mejoras Aplicadas con Éxito**: `%d`\n\n", m.totalImprovements))

	b.WriteString("| Época | Dominio | Métrica Antes | Métrica Después | % Mejora | Estado | Resumen |\n")
	b.WriteString("|---|---|---:|---:|---:|---|---|\n")
	for _, imp := range m.improvements {
		b.WriteString(fmt.Sprintf("| %d | `%s` | %.1f | %.1f | %.1f%% | `%s` | %s |\n",
			imp.Epoch, imp.Domain, imp.MetricBefore, imp.MetricAfter, imp.PercentImproved, imp.Status, imp.Summary))
	}
	return b.String()
}
