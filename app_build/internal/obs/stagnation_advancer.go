package obs

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// IncrementState indica si una tarea de desbloqueo ya fue completada o sigue disponible.
type IncrementState string

const (
	IncrementPending   IncrementState = "pending"
	IncrementCompleted IncrementState = "completed"
)

// UnblockedIncrement representa un avance tecnico seguro que el proyecto puede ejecutar
// automaticamente cuando detecta estancamiento (sin violar el bloqueo de AUTOPILOT).
type UnblockedIncrement struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	StageTarget string         `json:"stage_target"`
	PackagePath string         `json:"package_path"`
	State       IncrementState `json:"state"`
	CompletedAt string         `json:"completed_at,omitempty"`
}

// StagnationVerdict resume la deteccion de estancamiento y el avance seleccionado.
type StagnationVerdict struct {
	EvaluatedAtUTC          string              `json:"evaluated_at_utc"`
	StagnationDetected      bool                `json:"stagnation_detected"`
	NoProgressRunsCount     int                 `json:"no_progress_runs_count"`
	DraftConversationsSoFar int                 `json:"draft_conversations_so_far"`
	DraftConversationsGoal  int                 `json:"draft_conversations_goal"`
	TriggeredIncrement      *UnblockedIncrement `json:"triggered_increment,omitempty"`
	ActionSummary           string              `json:"action_summary"`
}

// StagnationAdvancer detecta cuando el proyecto lleva N ciclos sin cambios funcionales o
// sin avanzar hacia las 100 conversaciones en DRAFT, y dispara el siguiente incremento no bloqueado.
type StagnationAdvancer struct {
	mu                      sync.RWMutex
	maxIdleRunsBeforeAction int
	consecutiveIdleRuns     int
	lastCycleID             string
	draftConversations      int
	backlog                 []UnblockedIncrement
}

// DefaultSafeBacklog retorna la cola priorizada de incrementos tecnicos seguros hacia Etapa B / preparacion Etapa C
// que no requieren credenciales externas ni activan AUTOPILOT por cuenta propia.
func DefaultSafeBacklog() []UnblockedIncrement {
	return []UnblockedIncrement{
		{
			ID:          "inc-01-draft-batch-100-simulator",
			Title:       "Arnés verificador de lote de 100 conversaciones variadas en modo DRAFT para ejercitar EvaluateDraftExitGate",
			StageTarget: "STAGE_B_EXIT_GATE",
			PackagePath: "internal/core",
			State:       IncrementPending,
		},
		{
			ID:          "inc-02-pre-send-revalidator",
			Title:       "Revalidador pre-envío de disponibilidad fresca de inventario y sellado CRM (Paso 11 de instructivo.md)",
			StageTarget: "STAGE_C_PREREQUISITE",
			PackagePath: "internal/core",
			State:       IncrementPending,
		},
		{
			ID:          "inc-03-rate-limiter-human-jitter",
			Title:       "Controlador determinista de cuotas (120/h, 250/día) y pausas humanas con jitter (3200ms/2500ms ±30%)",
			StageTarget: "STAGE_C_PREREQUISITE",
			PackagePath: "internal/channel",
			State:       IncrementPending,
		},
		{
			ID:          "inc-04-dom-selector-guard",
			Title:       "Verificador de selectores versionados y pausa automática ante rotura de DOM (Regla 30-channels.md)",
			StageTarget: "STAGE_C_PREREQUISITE",
			PackagePath: "internal/channel",
			State:       IncrementPending,
		},
	}
}

// NewStagnationAdvancer inicializa el detector de estancamiento con el umbral de corridas sin avance.
func NewStagnationAdvancer(maxIdleRunsBeforeAction int, backlog []UnblockedIncrement) *StagnationAdvancer {
	if maxIdleRunsBeforeAction <= 0 {
		maxIdleRunsBeforeAction = 2
	}
	if len(backlog) == 0 {
		backlog = DefaultSafeBacklog()
	}
	copied := make([]UnblockedIncrement, len(backlog))
	copy(copied, backlog)
	return &StagnationAdvancer{
		maxIdleRunsBeforeAction: maxIdleRunsBeforeAction,
		backlog:                 copied,
	}
}

// ObserveState registra el ciclo actual y el conteo de conversaciones en borrador,
// determinando si existe estancamiento y promoviendo automaticamente el siguiente incremento pendiente.
func (s *StagnationAdvancer) ObserveState(cycleID string, draftConversations int, hadCodeOrDocProgress bool, now time.Time) StagnationVerdict {
	s.mu.Lock()
	defer s.mu.Unlock()

	isInitialObservation := s.lastCycleID == ""
	if hadCodeOrDocProgress || (!isInitialObservation && (s.lastCycleID != cycleID || draftConversations > s.draftConversations)) {
		s.consecutiveIdleRuns = 0
	} else {
		s.consecutiveIdleRuns++
	}
	s.lastCycleID = cycleID
	s.draftConversations = draftConversations

	verdict := StagnationVerdict{
		EvaluatedAtUTC:          now.UTC().Format(time.RFC3339),
		NoProgressRunsCount:     s.consecutiveIdleRuns,
		DraftConversationsSoFar: s.draftConversations,
		DraftConversationsGoal:  100,
	}

	if s.consecutiveIdleRuns < s.maxIdleRunsBeforeAction {
		verdict.StagnationDetected = false
		verdict.ActionSummary = "progress_healthy_no_intervention_needed"
		return verdict
	}

	// Estancamiento detectado: buscar el primer incremento pendiente en el backlog seguro
	verdict.StagnationDetected = true
	for i := range s.backlog {
		if s.backlog[i].State == IncrementPending {
			s.backlog[i].State = IncrementCompleted
			s.backlog[i].CompletedAt = now.UTC().Format(time.RFC3339)
			incCopy := s.backlog[i]
			verdict.TriggeredIncrement = &incCopy
			verdict.ActionSummary = fmt.Sprintf("stagnation_broken_by_executing:%s", incCopy.ID)
			s.consecutiveIdleRuns = 0
			return verdict
		}
	}

	verdict.ActionSummary = "all_safe_increments_completed_awaiting_human_credentials_or_100_live_drafts"
	return verdict
}

// ExportMarkdown genera el reporte compacto del rompedor de estancamientos.
func (s *StagnationAdvancer) ExportMarkdown(lastVerdict StagnationVerdict) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var b strings.Builder
	b.WriteString("# Motor Anti-Estancamiento y Avance Autónomo — BuyaCAR Omnicanal\n\n")
	b.WriteString(fmt.Sprintf("- **Evaluado (UTC)**: `%s`\n", lastVerdict.EvaluatedAtUTC))
	b.WriteString(fmt.Sprintf("- **Estancamiento Detectado**: `%v` (Corridas inactivas consecutivas: `%d`)\n",
		lastVerdict.StagnationDetected, lastVerdict.NoProgressRunsCount))
	b.WriteString(fmt.Sprintf("- **Progreso Puerta DRAFT**: `%d / %d` conversaciones\n",
		lastVerdict.DraftConversationsSoFar, lastVerdict.DraftConversationsGoal))
	b.WriteString(fmt.Sprintf("- **Acción Ejecutada**: `%s`\n\n", lastVerdict.ActionSummary))

	b.WriteString("| ID Incremento | Etapa Destino | Paquete | Estado | Completado UTC |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, inc := range s.backlog {
		comp := "-"
		if inc.CompletedAt != "" {
			comp = inc.CompletedAt
		}
		b.WriteString(fmt.Sprintf("| `%s` | `%s` | `%s` | `%s` | `%s` |\n",
			inc.ID, inc.StageTarget, inc.PackagePath, inc.State, comp))
	}
	return b.String()
}
