package obs

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// VerifyStrictlyMonotonicTraceTimestamps valida que todos los checkpoints de una traza
// tengan marcas de tiempo estrictamente monótonas crecientes y CERO marcas compartidas o duplicadas.
func VerifyStrictlyMonotonicTraceTimestamps(tr *ConversationTrace) error {
	if tr == nil || len(tr.Checkpoints) == 0 {
		return fmt.Errorf("trace has no checkpoints")
	}
	seen := make(map[string]bool)
	var prevTime time.Time

	for i, cp := range tr.Checkpoints {
		if cp.TimestampUTC == "" {
			return fmt.Errorf("checkpoint %d (%s) has empty timestamp", i, cp.Name)
		}
		if seen[cp.TimestampUTC] {
			return fmt.Errorf("shared timestamp detected at checkpoint %d (%s): %q", i, cp.Name, cp.TimestampUTC)
		}
		seen[cp.TimestampUTC] = true

		parsed, err := time.Parse(time.RFC3339Nano, cp.TimestampUTC)
		if err != nil {
			parsed, err = time.Parse(time.RFC3339, cp.TimestampUTC)
			if err != nil {
				return fmt.Errorf("checkpoint %d (%s) timestamp format invalid (%s): %w", i, cp.Name, cp.TimestampUTC, err)
			}
		}

		if i > 0 {
			if !parsed.After(prevTime) {
				return fmt.Errorf("checkpoint %d (%s at %s) is not strictly after previous checkpoint %d (%s at %s)",
					i, cp.Name, cp.TimestampUTC, i-1, tr.Checkpoints[i-1].Name, tr.Checkpoints[i-1].TimestampUTC)
			}
		}
		prevTime = parsed
	}
	return nil
}

func TestConversationTrace_ExportMarkdownWithoutPII(t *testing.T) {
	now := time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)
	tr := &ConversationTrace{
		ConversationID: "conv-700",
		ChannelID:      "whatsapp",
	}

	tr.RecordCheckpoint(CPLectura, true, 400*time.Microsecond, "", now)
	tr.RecordCheckpoint(CPTriage, true, 300*time.Microsecond, "", now.Add(1*time.Second))
	tr.RecordCheckpoint(CPInventario, true, 250*time.Microsecond, "", now.Add(2*time.Second))
	tr.RecordCheckpoint(CPRespuesta, true, 350*time.Microsecond, "", now.Add(3*time.Second))
	tr.RecordCheckpoint(CPGuard, false, 500*time.Microsecond, "Fallo con correo usuario@dominio.com y cedula 1098765432", now.Add(4*time.Second))
	tr.RecordCheckpoint(CPEnvio, false, 0, "skipped_by_guard", now.Add(5*time.Second))
	tr.RecordCheckpoint(CPCRM, true, 100*time.Microsecond, "", now.Add(6*time.Second))

	if err := VerifyStrictlyMonotonicTraceTimestamps(tr); err != nil {
		t.Fatalf("trace timestamps are not strictly monotonic increasing: %v", err)
	}

	md := tr.ExportMarkdown()
	if strings.Contains(md, "usuario@dominio.com") || strings.Contains(md, "1098765432") {
		t.Fatalf("PII leaked in exported Markdown trace:\n%s", md)
	}
	if !strings.Contains(md, "`lectura`") || !strings.Contains(md, "`crm`") {
		t.Fatalf("expected all 7 checkpoints in Markdown table:\n%s", md)
	}
}

func TestConversationTrace_StrictlyMonotonicTimestamps(t *testing.T) {
	now := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)

	t.Run("ValidStrictlyMonotonicTrace", func(t *testing.T) {
		tr := &ConversationTrace{ConversationID: "conv-mono-ok", ChannelID: "whatsapp"}
		checkpoints := []CheckpointName{
			CPLectura, CPTriage, CPInventario, CPRespuesta, CPGuard, CPEnvio, CPCRM,
		}
		for i, cpName := range checkpoints {
			tr.RecordCheckpoint(cpName, true, time.Duration(100+i*50)*time.Microsecond, "", now.Add(time.Duration(i+1)*time.Second))
		}
		if err := VerifyStrictlyMonotonicTraceTimestamps(tr); err != nil {
			t.Fatalf("expected valid strictly monotonic timestamps, got err: %v", err)
		}
		if len(tr.Checkpoints) != 7 {
			t.Fatalf("expected 7 checkpoints, got %d", len(tr.Checkpoints))
		}
	})

	t.Run("RejectsSharedTimestampViolation", func(t *testing.T) {
		// Validamos que VerifyStrictlyMonotonicTraceTimestamps detecte y rechace
		// cualquier traza maliciosa o corrompida que contenga timestamps compartidos.
		tr := &ConversationTrace{
			ConversationID: "conv-shared-ts",
			ChannelID:      "whatsapp",
			Checkpoints: []CheckpointEntry{
				{Name: CPLectura, TimestampUTC: now.UTC().Format(time.RFC3339Nano)},
				{Name: CPTriage, TimestampUTC: now.UTC().Format(time.RFC3339Nano)}, // Timestamp idéntico compartido
			},
		}
		err := VerifyStrictlyMonotonicTraceTimestamps(tr)
		if err == nil || !strings.Contains(err.Error(), "shared timestamp detected") {
			t.Fatalf("expected shared timestamp error, got %v", err)
		}
	})

	t.Run("RejectsRetrogradeTimestampViolation", func(t *testing.T) {
		// Validamos que VerifyStrictlyMonotonicTraceTimestamps detecte y rechace
		// regresiones temporales en los checkpoints.
		tr := &ConversationTrace{
			ConversationID: "conv-skew-ts",
			ChannelID:      "whatsapp",
			Checkpoints: []CheckpointEntry{
				{Name: CPLectura, TimestampUTC: now.Add(2 * time.Second).UTC().Format(time.RFC3339Nano)},
				{Name: CPTriage, TimestampUTC: now.Add(1 * time.Second).UTC().Format(time.RFC3339Nano)}, // Regresión
			},
		}
		err := VerifyStrictlyMonotonicTraceTimestamps(tr)
		if err == nil || !strings.Contains(err.Error(), "not strictly after") {
			t.Fatalf("expected retrograde timestamp error, got %v", err)
		}
	})
}

func TestTraceOutputs_StrictlyUTF8WithoutBOM(t *testing.T) {
	now := time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)
	tr := &ConversationTrace{
		ConversationID: "conv-utf8-check",
		ChannelID:      "whatsapp",
	}
	tr.RecordCheckpoint(CPLectura, true, 400*time.Microsecond, "", now)
	tr.RecordCheckpoint(CPTriage, true, 300*time.Microsecond, "", now.Add(time.Second))
	tr.RecordCheckpoint(CPGuard, false, 500*time.Microsecond, "Validación con acentos: éxito, dirección Tunja Boyacá", now.Add(2*time.Second))

	md := tr.ExportMarkdown()
	bytes := []byte(md)

	// Verificar ausencia de BOM UTF-8 (EF BB BF)
	if len(bytes) >= 3 && bytes[0] == 0xEF && bytes[1] == 0xBB && bytes[2] == 0xBF {
		t.Fatalf("BOM detected in exported markdown trace")
	}

	// Verificar validez UTF-8
	if !utf8.Valid(bytes) {
		t.Fatalf("exported markdown is not valid UTF-8")
	}

	// Verificar ausencia de mojibake
	for _, mojibake := range []string{"Ã¡", "Ã©", "Ã³", "Ã±", "\ufffd"} {
		if strings.Contains(md, mojibake) {
			t.Fatalf("mojibake pattern %q detected in exported trace", mojibake)
		}
	}
}

func TestCheckpointNomenclature_Canonical7Checkpoints(t *testing.T) {
	expectedCanonical := []struct {
		name     CheckpointName
		expected string
		code     string
	}{
		{CPLectura, "lectura", "cp1_lectura"},
		{CPTriage, "triage", "cp2_triage"},
		{CPInventario, "inventario", "cp3_inventario"},
		{CPRespuesta, "respuesta", "cp4_respuesta"},
		{CPGuard, "guard", "cp5_guard"},
		{CPEnvio, "envio", "cp6_envio"},
		{CPCRM, "crm", "cp7_crm"},
	}

	for _, tc := range expectedCanonical {
		if string(tc.name) != tc.expected {
			t.Fatalf("expected checkpoint constant %s to equal %s, got %s", tc.code, tc.expected, tc.name)
		}
	}

	legacyCycle0ToCanonical := map[string]string{
		"cp1_input_ingestion":   "cp1_lectura",
		"cp2_triage_decision":   "cp2_triage",
		"cp3_pii_sanitization":  "cp3_inventario",
		"cp4_fastpath_or_ai":    "cp4_respuesta",
		"cp5_output_guardrails": "cp5_guard",
		"cp6_persistence":       "cp6_envio",
		"cp7_token_audit":       "cp7_crm",
	}

	if len(legacyCycle0ToCanonical) != 7 {
		t.Fatalf("expected exactly 7 legacy to canonical mappings, got %d", len(legacyCycle0ToCanonical))
	}
}

func TestMetricsCollector_TargetsAndPercentiles(t *testing.T) {
	mc := NewMetricsCollector()
	for i := 0; i < 8; i++ {
		mc.RecordFastPathLatency(0.8)
		mc.RecordOutcome(true, false, false, "", false, "", 0, 0)
	}
	// 1 resuelta con IA
	mc.RecordFastPathLatency(1.2)
	mc.RecordOutcome(false, true, false, "", false, "", 320, 90)
	// 1 escalada por cifra alucinada bloqueada
	mc.RecordFastPathLatency(1.5)
	mc.RecordOutcome(false, true, true, "hallucinated_number_blocked:99999999", false, "", 310, 80)

	noAI, noHuman := mc.Ratios()
	if noAI < 70.0 {
		t.Fatalf("expected >= 70%% resolved without AI, got %.1f%%", noAI)
	}
	if noHuman < 85.0 {
		t.Fatalf("expected >= 85%% resolved without human, got %.1f%%", noHuman)
	}

	_, _, p99 := mc.FastPathPercentiles()
	if p99 >= 5.0 {
		t.Fatalf("expected Fast-Path p99 < 5 ms, got %.3f ms", p99)
	}
}

func TestEvaluateDraftExitGate_100ConversationsCriteria(t *testing.T) {
	mc := NewMetricsCollector()
	// Registrar 100 conversaciones reales en modo DRAFT: 78 por Fast-Path, 22 con IA, 0 alucinaciones
	for i := 0; i < 78; i++ {
		mc.RecordOutcome(true, false, false, "", false, "", 0, 0)
	}
	for i := 0; i < 22; i++ {
		mc.RecordOutcome(false, true, false, "", false, "", 250, 70)
	}

	// Con 90% de borradores aprobados sin edicion (>= 85%), la puerta hacia AUTOPILOT debe estar en true
	rep := mc.EvaluateDraftExitGate(90.0)
	if !rep.ReadyForAutopilot {
		t.Fatalf("expected ReadyForAutopilot=true with 100 conversations, 78%% fast-path, 90%% unedited, got %+v", rep)
	}

	// Si el porcentaje de aprobacion sin edicion cae a 80% (< 85%), la puerta debe bloquear el paso a AUTOPILOT
	repBlocked := mc.EvaluateDraftExitGate(80.0)
	if repBlocked.ReadyForAutopilot {
		t.Fatalf("expected ReadyForAutopilot=false when unedited approval < 85%%, got %+v", repBlocked)
	}
}

func TestKaizenCronAuditor_DetectsRegressionsAndValidatesHealthyCrons(t *testing.T) {
	now := time.Date(2026, 10, 28, 23, 35, 0, 0, time.UTC)
	auditor := NewKaizenCronAuditor(65, 3500)

	// 1. Registrar las iteraciones saludables de task-68 (Kaizen) y task-69 (Guardian)
	auditor.RecordRun(KaizenCronRunRecord{
		TaskID:         "task-68",
		CronExpression: "*/30 * * * *",
		Iteration:      1,
		TimestampUTC:   now.Add(-35 * time.Minute),
		TestsPassed:    true,
		GoldenCasesOK:  64,
		FastPathP99Ms:  0.0035,
		TokensConsumed: 420,
		Summary:        "Iteracion 1 Kaizen PDCA en GREEN",
	})
	auditor.RecordRun(KaizenCronRunRecord{
		TaskID:         "task-69",
		CronExpression: "0 * * * *",
		Iteration:      1,
		TimestampUTC:   now.Add(-35 * time.Minute),
		TestsPassed:    true,
		GoldenCasesOK:  64,
		FastPathP99Ms:  0.0035,
		TokensConsumed: 380,
		Summary:        "Iteracion 1 Guardian MVP en GREEN",
	})
	auditor.RecordRun(KaizenCronRunRecord{
		TaskID:         "task-68",
		CronExpression: "*/30 * * * *",
		Iteration:      2,
		TimestampUTC:   now.Add(-5 * time.Minute),
		TestsPassed:    true,
		GoldenCasesOK:  64,
		FastPathP99Ms:  0.0032,
		TokensConsumed: 290,
		Summary:        "Iteracion 2 Kaizen PDCA compacta en GREEN",
	})

	verdict := auditor.Audit(now)
	if verdict.Status != KaizenStatusImproved || !verdict.TokenBudgetOK || !verdict.LatencyGateOK || !verdict.DeadManSwitchOK || verdict.HealthyCronsCount != 2 {
		t.Fatalf("expected healthy improved verdict for 2 active crons, got %+v", verdict)
	}

	// 2. Ejecutar Autocalibracion y Automejora: debe estrechar el presupuesto de tokens de 3500 a 454 (125% de prom 363)
	// y el umbral de latencia de 5.0 ms a 0.02 ms (MinCalibratedLatencyTargetMs) basandose en las 3 corridas saludables.
	cal := auditor.AutoCalibrateAndSelfImprove()
	if cal.CalibrationEpoch != 1 || cal.CalibratedMaxTokensPerRun != 453 || cal.CalibratedLatencyTargetMs != MinCalibratedLatencyTargetMs {
		t.Fatalf("expected self-calibration to tighten thresholds to (453 tokens, %.2f ms), got %+v",
			MinCalibratedLatencyTargetMs, cal)
	}

	md := auditor.ExportMarkdown(now)
	if !strings.Contains(md, "`task-68`") || !strings.Contains(md, "`task-69`") || !strings.Contains(md, "Autocalibración (Época 1)") {
		t.Fatalf("expected Markdown report to include task-68, task-69 and self-calibration epoch:\n%s", md)
	}

	// 3. Simular un exceso respecto al nuevo umbral autocalibrado (ej. 900 tokens > 600 calibrados) -> debe marcar regressed
	auditor.RecordRun(KaizenCronRunRecord{
		TaskID:         "task-68",
		CronExpression: "*/30 * * * *",
		Iteration:      3,
		TimestampUTC:   now,
		TestsPassed:    true,
		GoldenCasesOK:  64,
		FastPathP99Ms:  0.12,
		TokensConsumed: 900,
		Summary:        "Regresion frente a umbral autocalibrado",
	})

	regVerdict := auditor.Audit(now)
	if regVerdict.Status != KaizenStatusRegressed || regVerdict.LatencyGateOK || regVerdict.TokenBudgetOK {
		t.Fatalf("expected regressed verdict on calibrated latency/token violation, got %+v", regVerdict)
	}
}

func TestStagnationAdvancer_DetectsIdleAndTriggersNextSafeIncrement(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 35, 0, 0, time.UTC)
	adv := NewStagnationAdvancer(2, nil)

	// 1. Primera corrida sin cambios -> aun bajo el umbral (1 < 2)
	v1 := adv.ObserveState("cycle-10", 1, false, now)
	if v1.StagnationDetected || v1.TriggeredIncrement != nil {
		t.Fatalf("expected no stagnation on first idle check, got %+v", v1)
	}

	// 2. Segunda corrida consecutiva sin cambios en cycle-10 -> detecta estancamiento y dispara inc-01
	v2 := adv.ObserveState("cycle-10", 1, false, now.Add(30*time.Minute))
	if !v2.StagnationDetected || v2.TriggeredIncrement == nil || v2.TriggeredIncrement.ID != "inc-01-draft-batch-100-simulator" {
		t.Fatalf("expected stagnation detected and inc-01 triggered, got %+v", v2)
	}

	md := adv.ExportMarkdown(v2)
	if !strings.Contains(md, "inc-01-draft-batch-100-simulator") || !strings.Contains(md, "completed") {
		t.Fatalf("expected markdown export to reflect completed increment:\n%s", md)
	}
}

func TestMasterCronSelfOptimizer_AppliesKaizenContinuousImprovement(t *testing.T) {
	now := time.Date(2026, 10, 29, 2, 40, 0, 0, time.UTC)
	opt := NewMasterCronSelfOptimizer()

	// 1. Corrida 1 estable: establece línea base (320 tokens, p99=0.0040 ms)
	rec1, applied1 := opt.EvaluateAndSelfImprove(0.0040, 320, true, now)
	if applied1 || rec1.Status != KaizenStatusUnchanged {
		t.Fatalf("expected baseline unchanged on run 1, got %+v", rec1)
	}

	// 2. Corrida 2 con métricas idénticas (sin cambio directamente medido)
	// REGLA ANTI-VIBECODING: Debe ser strictly unchanged (cero fórmula ficticia del 10%)
	rec2, applied2 := opt.EvaluateAndSelfImprove(0.0040, 320, true, now.Add(5*time.Minute))
	if applied2 || rec2.Status != KaizenStatusUnchanged {
		t.Fatalf("expected unchanged when metrics are identical (no fake 10%% reduction), got applied=%v rec=%+v", applied2, rec2)
	}

	// 3. Corrida 3 con optimización REAL y directamente medida: tokens reducidos de 320 a 270 (ahorro medido de 50 tokens)
	rec3, applied3 := opt.EvaluateAndSelfImprove(0.0033, 270, true, now.Add(10*time.Minute))
	if !applied3 || rec3.Status != KaizenStatusImproved || opt.CurrentBudgetCap() != 270 {
		t.Fatalf("expected directly measured improvement with budget 270, got applied=%v rec=%+v cap=%d",
			applied3, rec3, opt.CurrentBudgetCap())
	}
	if rec3.MetricBefore != 320 || rec3.MetricAfter != 270 {
		t.Fatalf("expected measured metrics before=320 after=270, got before=%.1f after=%.1f", rec3.MetricBefore, rec3.MetricAfter)
	}

	md := opt.ExportMarkdown()
	if !strings.Contains(md, "token_compression") || !strings.Contains(md, "270 tokens/corrida") {
		t.Fatalf("expected markdown to display self-optimization:\n%s", md)
	}
}

func TestConversationTrace_ExportJSONAndSave(t *testing.T) {
	tmpDir := t.TempDir()
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	tr := &ConversationTrace{
		ConversationID: "conv-trace-export",
		ChannelID:      "whatsapp",
		OperatingMode:  "SIMULATION",
	}
	tr.RecordCheckpoint(CPLectura, true, 200*time.Microsecond, "", now)
	tr.RecordCheckpoint(CPTriage, true, 300*time.Microsecond, "", now.Add(1*time.Second))

	jsonData, err := tr.ExportJSON()
	if err != nil {
		t.Fatalf("ExportJSON failed: %v", err)
	}
	if !strings.Contains(string(jsonData), "conv-trace-export") || !strings.Contains(string(jsonData), "SIMULATION") {
		t.Fatalf("JSON missing fields:\n%s", string(jsonData))
	}

	path, err := tr.SaveToDir(tmpDir)
	if err != nil {
		t.Fatalf("SaveToDir failed: %v", err)
	}
	if path == "" {
		t.Fatalf("expected non-empty path from SaveToDir")
	}
}
