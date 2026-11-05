package core

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"buyacar.com.co/omnicanal/internal/ai"
	"buyacar.com.co/omnicanal/internal/channel"
	"buyacar.com.co/omnicanal/internal/crm"
	"buyacar.com.co/omnicanal/internal/escalate"
	"buyacar.com.co/omnicanal/internal/fast"
	"buyacar.com.co/omnicanal/internal/guard"
	"buyacar.com.co/omnicanal/internal/inventory"
	"buyacar.com.co/omnicanal/internal/obs"
	"buyacar.com.co/omnicanal/internal/rag"
	"buyacar.com.co/omnicanal/internal/skills"
	"buyacar.com.co/omnicanal/internal/store"
	"buyacar.com.co/omnicanal/internal/triage"
)

// PipelineOutcome resume la ejecucion completa de una conversacion a traves de las 7 etapas.
type PipelineOutcome struct {
	ConversationID   string
	Skipped          bool
	SkipReason       string
	UsedFastPath     bool
	UsedAI           bool
	Escalated        bool
	EscalationReason string
	SentReply        string
	SendStatus       channel.SendStatus
	InputTokens      int
	OutputTokens     int
	TokenStatus      string // "measured", "zero_cached", "not_measured"
	CheckpointsOK    int
	CRMTaskID        string
	Trace            obs.ConversationTrace
}

// Pipeline conecta todas las capas modulares respetando SIMULATION, idempotencia y reanudacion.
type Pipeline struct {
	Engine     *Engine
	InvCache   *inventory.Cache
	Fast       *fast.Engine
	Guard      *guard.Guard
	AIOrch     *ai.Orchestrator
	EscQueue   *escalate.Queue
	CRM        crm.Manager
	Resilience channel.ResilienceTracker
	TraceHook  func(trace obs.ConversationTrace)
	TraceDir   string
	RAGEngine  rag.Engine
	Strategy   string // "", "A1_fast_only", "A2_rag_synth", "A3_llm_only", "A4_adaptive"
}

// PipelineConfig agrupa dependencias para inicializar un Pipeline con motor RAG opcional.
type PipelineConfig struct {
	Engine     *Engine
	InvCache   *inventory.Cache
	Fast       *fast.Engine
	Guard      *guard.Guard
	AIOrch     *ai.Orchestrator
	EscQueue   *escalate.Queue
	CRM        crm.Manager
	Resilience channel.ResilienceTracker
	TraceHook  func(trace obs.ConversationTrace)
	TraceDir   string
	RAGEngine  rag.Engine
	Strategy   string
}

// NewPipeline inicializa una instancia de Pipeline a partir de la configuracion provista.
func NewPipeline(cfg PipelineConfig) *Pipeline {
	return &Pipeline{
		Engine:     cfg.Engine,
		InvCache:   cfg.InvCache,
		Fast:       cfg.Fast,
		Guard:      cfg.Guard,
		AIOrch:     cfg.AIOrch,
		EscQueue:   cfg.EscQueue,
		CRM:        cfg.CRM,
		Resilience: cfg.Resilience,
		TraceHook:  cfg.TraceHook,
		TraceDir:   cfg.TraceDir,
		RAGEngine:  cfg.RAGEngine,
		Strategy:   cfg.Strategy,
	}
}

// ExportTrace exporta la traza a un archivo JSON en targetDir (o en TraceDir / production_artifacts/traces).
func (p *Pipeline) ExportTrace(trace obs.ConversationTrace, targetDir ...string) (string, error) {
	dir := p.TraceDir
	if len(targetDir) > 0 && targetDir[0] != "" {
		dir = targetDir[0]
	}
	if dir == "" {
		dir = "production_artifacts/traces"
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		slog.Error("pipeline: failed to create trace directory", "dir", dir, "error", err)
		return "", err
	}
	return trace.SaveToDir(dir)
}

func (p *Pipeline) saveCheckpoint(convID string, state string) error {
	if p.Engine == nil || p.Engine.store == nil {
		return nil
	}
	if err := p.Engine.store.SaveCheckpointState(convID, state); err != nil {
		slog.Error("pipeline: failed to save checkpoint state",
			"conv_id", convID,
			"checkpoint", state,
			"error", err,
		)
		return err
	}
	return nil
}

func (p *Pipeline) handleTrace(trace *obs.ConversationTrace) {
	if trace == nil {
		return
	}
	if p.TraceHook != nil {
		p.TraceHook(*trace)
	}
	if p.TraceDir != "" {
		if _, err := p.ExportTrace(*trace, p.TraceDir); err != nil {
			slog.Error("pipeline: failed to export trace to TraceDir", "trace_dir", p.TraceDir, "error", err)
		}
	}
}

func (p *Pipeline) tracker(ch channel.Channel) channel.ResilienceTracker {
	if p.Resilience != nil {
		return p.Resilience
	}
	type trackerHolder interface {
		ResilienceTracker() channel.ResilienceTracker
	}
	if th, ok := ch.(trackerHolder); ok {
		return th.ResilienceTracker()
	}
	return nil
}

// RevalidatePreSend revalida frescura de inventario y disponibilidad del vehiculo antes del despacho.
func (p *Pipeline) RevalidatePreSend(ctx context.Context, conv channel.Conversation, vehicleID string, now time.Time) (bool, string) {
	if err := ctx.Err(); err != nil {
		return false, err.Error()
	}
	if p.InvCache == nil {
		return false, "inventory_unavailable"
	}
	return p.InvCache.CheckVehicleAvailability(vehicleID, now)
}

// ProcessConversation ejecuta los 12 pasos del flujo por conversacion con persistencia de estado.
func (p *Pipeline) ProcessConversation(ctx context.Context, ch channel.Channel, sess channel.Session, conv channel.Conversation, now time.Time) (PipelineOutcome, error) {
	outcome := PipelineOutcome{ConversationID: conv.ID}
	trace := obs.ConversationTrace{
		ConversationID: conv.ID,
		ChannelID:      ch.ID(),
		OperatingMode:  string(p.Engine.cfg.OperatingMode),
	}

	rt := p.tracker(ch)
	if rt != nil && rt.IsPaused(ch.ID()) {
		outcome.Skipped = true
		outcome.SkipReason = "channel_is_paused"
		outcome.Escalated = true
		outcome.EscalationReason = "channel_paused_skip"
		outcome.TokenStatus = "not_measured"
		trace.RecordCheckpoint(obs.CPLectura, false, 0, "channel_is_paused", time.Now())
		outcome.Trace = trace
		p.handleTrace(&trace)
		return outcome, nil
	}

	// CP1: Lectura
	t1 := time.Now()
	th, err := ch.Read(ctx, sess, conv)
	dur1 := time.Since(t1)
	if err != nil {
		trace.RecordCheckpoint(obs.CPLectura, false, dur1, err.Error(), time.Now())
		outcome.Trace = trace
		p.handleTrace(&trace)
		if rt != nil {
			paused, _ := rt.RecordFailure(ch.ID(), err.Error())
			if paused {
				outcome.Escalated = true
				outcome.EscalationReason = "channel_auto_paused_consecutive_failures"
			}
		}
		return outcome, err
	}

	// Paso 2: Evaluar reglas de omision (propio, hash duplicado, > 9 semanas, kill switch)
	hash, err := p.Engine.ShouldProcessThread(ctx, th, now)
	if err != nil {
		durSkip := time.Since(t1)
		trace.RecordCheckpoint(obs.CPLectura, false, durSkip, err.Error(), time.Now())
		outcome.Trace = trace
		p.handleTrace(&trace)
		outcome.Skipped = true
		outcome.SkipReason = err.Error()
		outcome.TokenStatus = "not_measured"
		return outcome, nil
	}
	_ = p.saveCheckpoint(conv.ID, "cp1_lectura:ok")
	trace.RecordCheckpoint(obs.CPLectura, true, dur1, "", time.Now())
	outcome.CheckpointsOK++

	lastMsg := th.Messages[len(th.Messages)-1]
	cleanedText := guard.CleanIncomingNoise(lastMsg.Text)

	var priorTexts []string
	if p.Strategy != "A1_fast_only" && len(th.Messages) > 1 {
		for _, m := range th.Messages[:len(th.Messages)-1] {
			priorTexts = append(priorTexts, m.Text)
		}
	}

	// CP2: Triage (con memoria de hilo para anuncios de Marketplace)
	t2 := time.Now()
	cl := triage.ClassifyWithContext(cleanedText, priorTexts)
	dur2 := time.Since(t2)
	_ = p.saveCheckpoint(conv.ID, "cp2_triage:ok")
	trace.RecordCheckpoint(obs.CPTriage, true, dur2, "", time.Now())
	outcome.CheckpointsOK++

	// CP3 & CP4: Inventario + Respuesta (Fast-Path primero para ahorrar tokens)
	var candidateReply string
	var fpFallbackReply string
	var relevantVehicles []inventory.Vehicle
	var aiRes ai.ExecutionResult

	if p.Engine.cfg.EnableFastPath {
		t3 := time.Now()
		fpRes := p.Fast.Evaluate(ctx, cl, p.InvCache, now)
		dur3 := time.Since(t3)
		_ = p.saveCheckpoint(conv.ID, "cp3_inventario:ok")
		trace.RecordCheckpoint(obs.CPInventario, true, dur3, "", time.Now())
		outcome.CheckpointsOK++

		if fpRes.Handled && p.Strategy == "A4_adaptive" && !fpRes.MustEscalate && isComparativeOrAdvisoryQuery(cl.NormalizedText) {
			fpFallbackReply = fpRes.Reply
			match := p.InvCache.Search(ctx, cl.ExtractedQuery, now)
			relevantVehicles = match.Vehicles
		} else if fpRes.Handled {
			outcome.UsedFastPath = true
			outcome.TokenStatus = "zero_cached"
			candidateReply = fpRes.Reply
			match := p.InvCache.Search(ctx, cl.ExtractedQuery, now)
			relevantVehicles = match.Vehicles

			// Enriquecimiento compuesto (Vehículo + Garantía/Crédito) cuando hay motor RAG activo
			if p.RAGEngine != nil && !fpRes.MustEscalate && p.Strategy != "A1_fast_only" && hasCompoundPolicyQuestion(cl.NormalizedText) {
				if extra := p.retrieveCompoundPolicyClause(ctx, cleanedText); extra != "" {
					combined := candidateReply + " " + extra
					if len([]rune(combined)) <= 495 {
						candidateReply = combined
					}
				}
			}

			_ = p.saveCheckpoint(conv.ID, "cp4_respuesta:ok")
			trace.RecordCheckpoint(obs.CPRespuesta, true, dur3, "", time.Now())
			outcome.CheckpointsOK++

			// Si es un vehiculo vendido, enviamos el aviso comercial y ademas escalamos (Leccion conocida #4)
			if fpRes.MustEscalate {
				p.EscQueue.Enqueue(ch.ID(), conv.ID, fpRes.EscalationReason, cleanedText, now)
				outcome.Escalated = true
				outcome.EscalationReason = fpRes.EscalationReason
			}
		} else if fpRes.MustEscalate {
			p.EscQueue.Enqueue(ch.ID(), conv.ID, fpRes.EscalationReason, cleanedText, now)
			outcome.Escalated = true
			outcome.EscalationReason = fpRes.EscalationReason
			outcome.TokenStatus = "not_measured"
			trace.RecordCheckpoint(obs.CPRespuesta, false, dur3, fpRes.EscalationReason, time.Now())
			outcome.Trace = trace
			p.handleTrace(&trace)
			return outcome, nil
		}
	}

	// Si Fast-Path no resolvio y no escalo, delegar a RAG-Synthesizer / IA (Gemini -> Ollama -> RAG-Synth -> Humano)
	if candidateReply == "" {
		t4 := time.Now()
		match := p.InvCache.Search(ctx, cl.ExtractedQuery, now)
		if match.Status == inventory.StatusExactMatch || match.Status == inventory.StatusMultipleMatches {
			relevantVehicles = match.Vehicles
		} else {
			relevantVehicles = inventory.DefaultFixtureVehicles()
		}

		var ragChunks []rag.Chunk
		var scoredRAG []rag.ScoredChunk
		if p.RAGEngine != nil && p.Strategy != "A1_fast_only" {
			q := rag.Query{
				Text:      cleanedText,
				TopK:      3,
				MaxTokens: 150,
			}
			scored, err := p.RAGEngine.Retrieve(ctx, q)
			if err == nil {
				scoredRAG = scored
				for _, sc := range scored {
					ragChunks = append(ragChunks, sc.Chunk)
				}
			} else {
				slog.Warn("pipeline: rag retrieval error, continuing without rag", "error", err)
			}
		}

		// Estrategia A2 (RAG-Direct Synthesizer puro sin llamar LLM)
		if p.Strategy == "A2_rag_synth" && len(scoredRAG) > 0 && scoredRAG[0].Score >= 0.65 {
			candidateReply = SynthesizeFromRAGChunk(scoredRAG[0].Chunk)
			dur4 := time.Since(t4)
			outcome.TokenStatus = "zero_cached"
			_ = p.saveCheckpoint(conv.ID, "cp4_respuesta:ok")
			trace.RecordCheckpoint(obs.CPRespuesta, true, dur4, "", time.Now())
			outcome.CheckpointsOK++
		} else if p.Strategy == "A1_fast_only" || p.AIOrch == nil {
			dur4 := time.Since(t4)
			p.EscQueue.Enqueue(ch.ID(), conv.ID, "fast_path_unhandled", cleanedText, now)
			outcome.Escalated = true
			outcome.EscalationReason = "fast_path_unhandled"
			outcome.TokenStatus = "not_measured"
			trace.RecordCheckpoint(obs.CPRespuesta, false, dur4, "fast_path_unhandled", time.Now())
			outcome.Trace = trace
			p.handleTrace(&trace)
			return outcome, nil
		} else {
			var compact skills.CompactContext
			if len(ragChunks) > 0 {
				compact = skills.BuildCompactContextWithRAG(cl.Intent, cleanedText, relevantVehicles, ragChunks, 150)
			} else {
				compact = skills.BuildCompactContext(cl.Intent, cleanedText, relevantVehicles)
			}

			var semanticHash string
			if p.AIOrch != nil {
				semanticHash = p.AIOrch.HashQuery(cleanedText)
			}
			aiRes = p.AIOrch.Resolve(ctx, semanticHash, compact)
			dur4 := time.Since(t4)
			outcome.UsedAI = aiRes.ProviderUsed == "gemini" || aiRes.ProviderUsed == "ollama" || aiRes.ProviderUsed == "groq"
			if aiRes.ProviderUsed == "fast_path" {
				outcome.UsedFastPath = true
			}
			outcome.InputTokens = aiRes.InputTokens
			outcome.OutputTokens = aiRes.OutputTokens
			if outcome.UsedAI {
				outcome.TokenStatus = "measured"
			} else if aiRes.ProviderUsed == "semantic_cache" || aiRes.ProviderUsed == "cache" || aiRes.ProviderUsed == "fast_path" {
				outcome.TokenStatus = "zero_cached"
			} else {
				outcome.TokenStatus = "not_measured"
			}

			// Fallback autónomo A4 (Cascada Adaptativa): si el proveedor LLM no está activo pero el RAG local tiene match fáctico >= 0.65, sintetiza determinísticamente
			if aiRes.Escalated && (aiRes.Reason == "both_ai_providers_failed" || aiRes.Reason == "ai_providers_failed_or_exhausted") && p.Strategy != "A3_llm_only" && len(scoredRAG) > 0 && scoredRAG[0].Score >= 0.65 {
				candidateReply = SynthesizeFromRAGChunk(scoredRAG[0].Chunk)
				outcome.TokenStatus = "zero_cached"
				_ = p.saveCheckpoint(conv.ID, "cp4_respuesta:ok")
				trace.RecordCheckpoint(obs.CPRespuesta, true, dur4, "", time.Now())
				outcome.CheckpointsOK++
			} else if aiRes.Escalated && fpFallbackReply != "" {
				candidateReply = fpFallbackReply
				outcome.UsedFastPath = true
				outcome.TokenStatus = "zero_cached"
				_ = p.saveCheckpoint(conv.ID, "cp4_respuesta:ok")
				trace.RecordCheckpoint(obs.CPRespuesta, true, dur4, "", time.Now())
				outcome.CheckpointsOK++
			} else if aiRes.Escalated {
				p.EscQueue.Enqueue(ch.ID(), conv.ID, aiRes.Reason, cleanedText, now)
				outcome.Escalated = true
				outcome.EscalationReason = aiRes.Reason
				trace.RecordCheckpoint(obs.CPRespuesta, false, dur4, aiRes.Reason, time.Now())
				outcome.Trace = trace
				p.handleTrace(&trace)
				return outcome, nil
			} else {
				candidateReply = aiRes.Output.ProposedReply
				_ = p.saveCheckpoint(conv.ID, "cp4_respuesta:ok")
				trace.RecordCheckpoint(obs.CPRespuesta, true, dur4, "", time.Now())
				outcome.CheckpointsOK++
			}
		}
	}

	// CP5: Guard (Validacion anti-alucinacion, URLs, PII, frases prohibidas y limites de canal)
	t5 := time.Now()
	var backedNumbers []string
	if p.RAGEngine != nil {
		backedNumbers = p.RAGEngine.GetBackedNumbers()
	}
	valRes := p.Guard.ValidateOutboxWithBackedNumbers(candidateReply, ch.ID(), relevantVehicles, backedNumbers)
	dur5 := time.Since(t5)
	if !valRes.Approved || valRes.MustEscalate {
		p.EscQueue.Enqueue(ch.ID(), conv.ID, valRes.RejectionReason, candidateReply, now)
		outcome.Escalated = true
		outcome.EscalationReason = valRes.RejectionReason
		trace.RecordCheckpoint(obs.CPGuard, false, dur5, valRes.RejectionReason, time.Now())
		outcome.Trace = trace
		p.handleTrace(&trace)
		return outcome, nil
	}
	_ = p.saveCheckpoint(conv.ID, "cp5_guard:ok")
	trace.RecordCheckpoint(obs.CPGuard, true, dur5, "", time.Now())
	outcome.CheckpointsOK++

	// Paso 11: Revalidacion Pre-Envio (frescura < 30 min y vehiculo available)
	var referencedVehicleID string
	if len(relevantVehicles) == 1 {
		referencedVehicleID = relevantVehicles[0].ID
	} else if len(relevantVehicles) > 1 {
		for _, v := range relevantVehicles {
			if strings.Contains(strings.ToLower(valRes.SanitizedReply), strings.ToLower(v.Model)) {
				referencedVehicleID = v.ID
				break
			}
		}
		if referencedVehicleID == "" {
			referencedVehicleID = relevantVehicles[0].ID
		}
	} else if cl.ExtractedQuery.Brand != "" || cl.ExtractedQuery.Model != "" {
		if p.InvCache != nil {
			match := p.InvCache.Search(ctx, cl.ExtractedQuery, now)
			if len(match.Vehicles) > 0 {
				referencedVehicleID = match.Vehicles[0].ID
			}
		}
	}

	if outcome.EscalationReason == "vehicle_sold_or_reserved" {
		if p.InvCache != nil && !p.InvCache.IsFresh(now) {
			p.EscQueue.Enqueue(ch.ID(), conv.ID, "inventory_stale", cleanedText, now)
			outcome.EscalationReason = "inventory_stale"
			outcome.Trace = trace
			p.handleTrace(&trace)
			return outcome, nil
		}
	} else if referencedVehicleID != "" {
		ok, reason := p.RevalidatePreSend(ctx, conv, referencedVehicleID, now)
		if !ok {
			if reason == "vehicle_sold_or_reserved" {
				notice := fast.DefaultBusinessTexts().SoldVehicleNotice
				valRes.SanitizedReply = notice
				p.EscQueue.Enqueue(ch.ID(), conv.ID, "vehicle_sold_or_reserved", cleanedText, now)
				outcome.Escalated = true
				outcome.EscalationReason = "vehicle_sold_or_reserved"
			} else {
				p.EscQueue.Enqueue(ch.ID(), conv.ID, reason, cleanedText, now)
				outcome.Escalated = true
				outcome.EscalationReason = reason
				outcome.Trace = trace
				p.handleTrace(&trace)
				return outcome, nil
			}
		}
	}

	// CP6: Envio en modo configurado y validacion de estado de entrega
	t6 := time.Now()
	activeMode := p.Engine.OperatingMode()
	sendRes, err := ch.Reply(ctx, sess, conv, channel.Message{
		ID:        "out-" + conv.ID,
		Sender:    "own",
		Text:      valRes.SanitizedReply,
		Timestamp: now,
	}, activeMode)
	dur6 := time.Since(t6)
	if err != nil {
		if rt != nil {
			paused, _ := rt.RecordFailure(ch.ID(), err.Error())
			if paused {
				outcome.EscalationReason = "channel_auto_paused_consecutive_failures"
			}
		}
		p.EscQueue.Enqueue(ch.ID(), conv.ID, "send_failed", err.Error(), now)
		outcome.Escalated = true
		if outcome.EscalationReason == "" {
			outcome.EscalationReason = "send_failed"
		}
		outcome.SendStatus = channel.SendFailed
		trace.RecordCheckpoint(obs.CPEnvio, false, dur6, err.Error(), time.Now())
		outcome.Trace = trace
		p.handleTrace(&trace)
		return outcome, err
	}
	if sendRes.Status == channel.SendFailed {
		if rt != nil {
			paused, _ := rt.RecordFailure(ch.ID(), "channel reported send_failed")
			if paused {
				outcome.EscalationReason = "channel_auto_paused_consecutive_failures"
			}
		}
		p.EscQueue.Enqueue(ch.ID(), conv.ID, "send_failed", "channel reported send_failed", now)
		outcome.Escalated = true
		if outcome.EscalationReason == "" {
			outcome.EscalationReason = "send_failed"
		}
		outcome.SendStatus = channel.SendFailed
		trace.RecordCheckpoint(obs.CPEnvio, false, dur6, "channel reported send_failed", time.Now())
		outcome.Trace = trace
		p.handleTrace(&trace)
		return outcome, fmt.Errorf("core: channel %s reported send_failed", ch.ID())
	}

	switch activeMode {
	case channel.ModeAutopilot:
		if sendRes.Status != channel.SendVerified {
			if rt != nil {
				rt.RecordFailure(ch.ID(), "send_unverified")
			}
			reason := fmt.Sprintf("autopilot requires sent_verified, got %s", sendRes.Status)
			p.EscQueue.Enqueue(ch.ID(), conv.ID, "send_unverified", reason, now)
			outcome.Escalated = true
			outcome.EscalationReason = "send_unverified"
			outcome.SendStatus = sendRes.Status
			trace.RecordCheckpoint(obs.CPEnvio, false, dur6, reason, time.Now())
			outcome.Trace = trace
			p.handleTrace(&trace)
			return outcome, fmt.Errorf("core: unverified send in autopilot mode: %s", sendRes.Status)
		}
	case channel.ModeDraft:
		if sendRes.Status != channel.SendDrafted {
			if rt != nil {
				rt.RecordFailure(ch.ID(), "draft_unrecorded")
			}
			reason := fmt.Sprintf("draft requires draft_recorded, got %s", sendRes.Status)
			p.EscQueue.Enqueue(ch.ID(), conv.ID, "draft_unrecorded", reason, now)
			outcome.Escalated = true
			outcome.EscalationReason = "draft_unrecorded"
			outcome.SendStatus = sendRes.Status
			trace.RecordCheckpoint(obs.CPEnvio, false, dur6, reason, time.Now())
			outcome.Trace = trace
			p.handleTrace(&trace)
			return outcome, fmt.Errorf("core: draft not recorded in draft mode: %s", sendRes.Status)
		}
	case channel.ModeSimulation:
		if sendRes.Status != channel.SendSimulated {
			if rt != nil {
				rt.RecordFailure(ch.ID(), "simulation_unrecorded")
			}
			reason := fmt.Sprintf("simulation requires simulated_no_op, got %s", sendRes.Status)
			p.EscQueue.Enqueue(ch.ID(), conv.ID, "simulation_unrecorded", reason, now)
			outcome.Escalated = true
			outcome.EscalationReason = "simulation_unrecorded"
			outcome.SendStatus = sendRes.Status
			trace.RecordCheckpoint(obs.CPEnvio, false, dur6, reason, time.Now())
			outcome.Trace = trace
			p.handleTrace(&trace)
			return outcome, fmt.Errorf("core: unexpected simulation status: %s", sendRes.Status)
		}
	}

	if rt != nil {
		rt.RecordSuccess(ch.ID())
	}

	_ = p.saveCheckpoint(conv.ID, "cp6_envio:ok")
	trace.RecordCheckpoint(obs.CPEnvio, true, dur6, "", time.Now())
	outcome.CheckpointsOK++

	// CP7: Persistir idempotencia y estado CRM para reanudar tras reinicio
	t7 := time.Now()
	var settledTaskID string
	if p.CRM != nil {
		settledTaskID = p.settleCRMTasks(ctx, ch, conv, outcome, valRes, relevantVehicles, aiRes, cl, referencedVehicleID, now)
	}

	currentETag := ""
	if p.InvCache != nil {
		currentETag = p.InvCache.GetETag()
	}

	if err := p.Engine.store.MarkProcessed(store.IdempotencyRecord{
		Hash:       hash,
		ChannelID:  ch.ID(),
		ConvID:     conv.ID,
		ReplyText:  valRes.SanitizedReply,
		CreatedAt:  now,
		SendStatus: string(sendRes.Status),
		ETag:       currentETag,
		CRMTaskID:  settledTaskID,
	}); err != nil {
		slog.Error("pipeline: failed to mark idempotency record in store", "conv_id", conv.ID, "error", err)
	}
	_ = p.saveCheckpoint(conv.ID, "cp7_crm:ok")
	dur7 := time.Since(t7)
	trace.RecordCheckpoint(obs.CPCRM, true, dur7, "", time.Now())
	outcome.CheckpointsOK++

	outcome.SentReply = valRes.SanitizedReply
	outcome.SendStatus = sendRes.Status
	outcome.CRMTaskID = settledTaskID
	outcome.Trace = trace
	p.handleTrace(&trace)
	return outcome, nil
}

func (p *Pipeline) settleCRMTasks(
	ctx context.Context,
	ch channel.Channel,
	conv channel.Conversation,
	outcome PipelineOutcome,
	valRes guard.ValidationResult,
	relevantVehicles []inventory.Vehicle,
	aiRes ai.ExecutionResult,
	cl triage.Classification,
	referencedVehicleID string,
	now time.Time,
) string {
	if p.CRM == nil {
		return ""
	}

	vehicleID := referencedVehicleID
	if vehicleID == "" && len(relevantVehicles) > 0 {
		vehicleID = relevantVehicles[0].ID
	}

	var primaryTaskID string

	if outcome.UsedAI && len(aiRes.Output.CRMUpdate.TasksToCreate) > 0 {
		for i, rawTask := range aiRes.Output.CRMUpdate.TasksToCreate {
			tType := crm.NormalizeTaskType(rawTask)
			taskID := fmt.Sprintf("task-%s-%d-%d", conv.ID, now.UnixNano(), i)
			if primaryTaskID == "" {
				primaryTaskID = taskID
			}
			t := crm.Task{
				ID:          taskID,
				ConvID:      conv.ID,
				ChannelID:   ch.ID(),
				Type:        tType,
				Status:      crm.TaskStatusPending,
				Description: fmt.Sprintf("AI proposed: %s (%s)", rawTask, aiRes.Output.ConversationSummary),
				VehicleID:   vehicleID,
				CreatedAt:   now,
			}
			if err := p.CRM.RecordTask(ctx, t); err != nil {
				slog.Error("pipeline: failed to record CRM task", "task_id", t.ID, "conv_id", conv.ID, "error", err)
			}
		}
	} else if outcome.UsedAI {
		taskID := fmt.Sprintf("task-%s-%d", conv.ID, now.UnixNano())
		primaryTaskID = taskID
		t := crm.Task{
			ID:          taskID,
			ConvID:      conv.ID,
			ChannelID:   ch.ID(),
			Type:        crm.TaskCommercialFollowup,
			Status:      crm.TaskStatusPending,
			Description: fmt.Sprintf("AI summary: %s", aiRes.Output.ConversationSummary),
			VehicleID:   vehicleID,
			CreatedAt:   now,
		}
		if err := p.CRM.RecordTask(ctx, t); err != nil {
			slog.Error("pipeline: failed to record CRM task", "task_id", t.ID, "conv_id", conv.ID, "error", err)
		}
	} else if outcome.UsedFastPath {
		tType, desc := crm.InferTaskFromTriage(cl.Intent, vehicleID != "")
		taskID := fmt.Sprintf("task-%s-%d", conv.ID, now.UnixNano())
		primaryTaskID = taskID
		status := crm.TaskStatusPending
		if outcome.Escalated {
			status = crm.TaskStatusEscalated
		}
		t := crm.Task{
			ID:          taskID,
			ConvID:      conv.ID,
			ChannelID:   ch.ID(),
			Type:        tType,
			Status:      status,
			Description: desc,
			VehicleID:   vehicleID,
			CreatedAt:   now,
		}
		if err := p.CRM.RecordTask(ctx, t); err != nil {
			slog.Error("pipeline: failed to record CRM task", "task_id", t.ID, "conv_id", conv.ID, "error", err)
		}
	} else {
		taskID := fmt.Sprintf("task-%s-%d", conv.ID, now.UnixNano())
		primaryTaskID = taskID
		t := crm.Task{
			ID:          taskID,
			ConvID:      conv.ID,
			ChannelID:   ch.ID(),
			Type:        crm.TaskGeneralInquiry,
			Status:      crm.TaskStatusPending,
			Description: "Atención comercial general BuyaCAR",
			VehicleID:   vehicleID,
			CreatedAt:   now,
		}
		if err := p.CRM.RecordTask(ctx, t); err != nil {
			slog.Error("pipeline: failed to record CRM task", "task_id", t.ID, "conv_id", conv.ID, "error", err)
		}
	}

	return primaryTaskID
}

// SynthesizeFromRAGChunk construye una respuesta determinista fáctica (<= 480 caracteres) a partir del fragmento RAG verificado.
func SynthesizeFromRAGChunk(chunk rag.Chunk) string {
	snippet := strings.TrimSpace(strings.ReplaceAll(chunk.Content, "\n", " "))
	if runes := []rune(snippet); len(runes) > 250 {
		snippet = string(runes[:250]) + "..."
	}
	return fmt.Sprintf("BuyaCAR Tunja (%s): %s WhatsApp 3151212223. Más info: https://buyacar.com.co", chunk.Title, snippet)
}

func hasCompoundPolicyQuestion(norm string) bool {
	for _, kw := range []string{"garantia", "requisitos", "credito", "cuota inicial", "bancos", "financiacion", "peritaje", "traspaso"} {
		if strings.Contains(norm, kw) {
			return true
		}
	}
	return false
}

func isComparativeOrAdvisoryQuery(norm string) bool {
	for _, kw := range []string{"recomienda", "recomiendas", "comparar", "diferencia entre", "cual es mejor", "entre la ", "entre el "} {
		if strings.Contains(norm, kw) {
			return true
		}
	}
	return false
}

func (p *Pipeline) retrieveCompoundPolicyClause(ctx context.Context, text string) string {
	if p.RAGEngine == nil {
		return ""
	}
	norm := strings.ToLower(text)
	cat := ""
	switch {
	case strings.Contains(norm, "garant"):
		cat = "warranty"
	case strings.Contains(norm, "credit") || strings.Contains(norm, "requisit") || strings.Contains(norm, "cuota") || strings.Contains(norm, "banco") || strings.Contains(norm, "financ"):
		cat = "credit"
	case strings.Contains(norm, "peritaje") || strings.Contains(norm, "traspaso"):
		cat = "faq"
	}
	scored, err := p.RAGEngine.Retrieve(ctx, rag.Query{Text: text, Category: cat, TopK: 1, MaxTokens: 120})
	if err != nil || len(scored) == 0 || scored[0].Score < 0.60 {
		return ""
	}
	switch cat {
	case "warranty":
		return "Garantía certificada: 12 meses o 20000 km en motor y caja."
	case "credit":
		return "Financiación desde 10% de cuota inicial con Bancolombia, Davivienda y Finandina."
	default:
		return "Incluye peritaje técnico certificado de 150 puntos."
	}
}
