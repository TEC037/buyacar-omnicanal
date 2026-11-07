package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"buyacar.com.co/omnicanal/internal/ai"
	"buyacar.com.co/omnicanal/internal/channel"
	"buyacar.com.co/omnicanal/internal/core"
	"buyacar.com.co/omnicanal/internal/crm"
	"buyacar.com.co/omnicanal/internal/escalate"
	"buyacar.com.co/omnicanal/internal/fast"
	"buyacar.com.co/omnicanal/internal/guard"
	"buyacar.com.co/omnicanal/internal/inventory"
	"buyacar.com.co/omnicanal/internal/links"
	"buyacar.com.co/omnicanal/internal/obs"
	"buyacar.com.co/omnicanal/internal/rag"
	"buyacar.com.co/omnicanal/internal/server"
	"buyacar.com.co/omnicanal/internal/skills"
	"buyacar.com.co/omnicanal/internal/store"
	"buyacar.com.co/omnicanal/internal/triage"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	code := RunCLI(ctx, os.Args[1:], os.Stdout, os.Stderr)
	os.Exit(code)
}

// RunCLI ejecuta el comando BuyaCAR Omnicanal procesando flags CLI de forma determinista y comprobable.
func RunCLI(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("buyacar", flag.ContinueOnError)
	fs.SetOutput(stderr)

	serverFlag := fs.Bool("server", false, "Iniciar servidor web embebido y exportador Prometheus")
	portFlag := fs.Int("port", 8080, "Puerto TCP para el servidor web embebido")
	metricsFlag := fs.Bool("metrics", false, "Imprimir métricas estructuradas en JSON a stdout y salir")
	inspectFlag := fs.Bool("inspect", false, "Imprimir diagnóstico de salud en JSON a stdout y salir")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	logger := slog.New(slog.NewJSONHandler(stderr, nil))
	now := time.Now().UTC()

	// 1. Inicializar configuración operativa (SIMULATION por defecto; conmutable a DRAFT o AUTOPILOT bajo orden humana explícita)
	cfg := core.DefaultSafeConfig()
	switch strings.ToUpper(strings.TrimSpace(os.Getenv("BUYACAR_OPERATING_MODE"))) {
	case string(channel.ModeDraft):
		cfg.OperatingMode = channel.ModeDraft
	case string(channel.ModeAutopilot):
		// Permite activar AUTOPILOT si BUYACAR_OPERATING_MODE=AUTOPILOT (con o sin flag secundario)
		cfg.OperatingMode = channel.ModeAutopilot
	}

	st := store.NewMemoryStore()
	eng := core.NewEngine(cfg, st)

	// 2. Inicializar repositorio de enlaces con allowlist HTTPS estricta
	linkRepo := links.NewRepository([]string{"buyacar.com.co"})
	_ = linkRepo.Upsert(channel.Link{
		ID:        "link-email-pilot",
		URL:       "https://buyacar.com.co/pilot/email",
		ChannelID: channel.EmailPilotChannelID,
		Label:     "Canal Piloto de Correo BuyaCAR Tunja (Etapa B DRAFT)",
		Active:    true,
		Priority:  1,
	})
	_ = linkRepo.Upsert(channel.Link{
		ID:        "link-wa-sim",
		URL:       "https://buyacar.com.co/sim/whatsapp",
		ChannelID: "whatsapp",
		Label:     "WhatsApp Oficial BuyaCAR Tunja",
		Active:    true,
		Priority:  2,
	})
	_ = linkRepo.Upsert(channel.Link{
		ID:        "link-fb-marketplace",
		URL:       "https://buyacar.com.co/marketplace/facebook",
		ChannelID: "facebook_marketplace",
		Label:     "Facebook Marketplace / Messenger BuyaCAR Boyacá",
		Active:    true,
		Priority:  1,
	})

	// 3. Registrar canales omnicanal (incluyendo Facebook Marketplace autónomo)
	emailPilot := channel.NewEmailPilotChannel()
	_ = eng.RegisterChannel(emailPilot)

	waStub := channel.NewStubChannel("whatsapp", 650)
	_ = eng.RegisterChannel(waStub)

	fbMarketplace := channel.NewStubChannel("facebook_marketplace", 650)
	_ = eng.RegisterChannel(fbMarketplace)

	// Sembrar conversaciones iniciales para verificación
	emailConv := channel.Conversation{
		ID:            "draft-pilot-email-01",
		ChannelID:     channel.EmailPilotChannelID,
		CustomerAlias: "Cliente_Piloto_Tunja",
		UpdatedAt:     now,
	}
	emailPilot.IngestInboundEmail(emailConv, "Consulta Toyota Hilux 2022 y ubicación", []channel.Message{
		{ID: "em-01", Sender: "customer", Text: "¿Qué precio tiene la Toyota Hilux 2022?", Timestamp: now.Add(-45 * time.Second)},
	})

	demoConv := channel.Conversation{ID: "mvp-demo-01", ChannelID: "whatsapp", CustomerAlias: "Lead_Tunja", UpdatedAt: now}
	waStub.SeedThread(demoConv, []channel.Message{
		{ID: "msg-01", Sender: "customer", Text: "¿Dónde están ubicados y cuál es el horario?", Timestamp: now.Add(-30 * time.Second)},
	})

	fbConv := channel.Conversation{ID: "fb-mkt-lead-01", ChannelID: "facebook_marketplace", CustomerAlias: "Comprador_Marketplace_Boyaca", UpdatedAt: now}
	fbMarketplace.SeedThread(fbConv, []channel.Message{
		{ID: "fb-msg-01", Sender: "customer", Text: "Hola, vi en Marketplace la Toyota Hilux 2022, ¿qué precio tiene?", Timestamp: now.Add(-15 * time.Second)},
	})

	// 4. Inventario y proveedores de IA
	invCache := inventory.NewSeededStubCache(now)
	if invFile := strings.TrimSpace(os.Getenv("BUYACAR_INVENTORY_FILE")); invFile != "" {
		if customVehicles, loadErr := inventory.LoadVehiclesFromJSONFile(invFile); loadErr == nil && len(customVehicles) > 0 {
			invCache.LoadSnapshot(customVehicles, `W/"buyacar-file-etag-v1"`, now)
		}
	}
	fetcher, err := inventory.NewHTTPFetcher(inventory.OfficialInventoryEndpoint, []string{"buyacar.com.co"}, inventory.DefaultUpstreamTimeout)
	inventoryLiveSynced := false
	if err == nil && os.Getenv("BUYACAR_LIVE_INVENTORY_SYNC") == "1" {
		if _, syncErr := fetcher.Sync(ctx, invCache, now); syncErr == nil {
			inventoryLiveSynced = true
		} else {
			invCache = inventory.NewSeededStubCache(now)
		}
	}

	geminiProv, _ := ai.NewGeminiProviderFromEnv(ai.DefaultGeminiBaseURL, ai.DefaultAIProviderTimeout)
	ollamaProv, _ := ai.NewOllamaProvider(ai.DefaultOllamaEndpoint, ai.DefaultOllamaModel, ai.DefaultAIProviderTimeout)

	crmMgr := crm.NewMemoryManager()
	escQ := escalate.NewQueue()
	ragEng := rag.NewEngine()
	_, _ = rag.LoadEmbeddedFixtures(ctx, ragEng)

	strategy := "A2_rag_synth"
	if os.Getenv("GEMINI_API_KEY") != "" {
		strategy = "A4_adaptive"
	}

	pipe := &core.Pipeline{
		Engine:    eng,
		InvCache:  invCache,
		Fast:      fast.NewEngine(fast.DefaultBusinessTexts(), cfg.ConfidenceThreshold),
		Guard:     guard.NewGuard([]string{"buyacar.com.co"}),
		AIOrch:    ai.NewOrchestrator(geminiProv, ollamaProv, skills.NewApprovedCache()),
		EscQueue:  escQ,
		CRM:       crmMgr,
		RAGEngine: ragEng,
		TraceDir:  "../production_artifacts/traces",
		Strategy:  strategy,
	}

	resTracker := channel.NewMemoryResilienceTracker(escQ)
	rateCtrl := channel.NewMemoryRateController(channel.DefaultRateConfig())
	obsCollector := obs.NewMetricsCollector()

	// Adaptador event-driven de Meta / Facebook Marketplace (/webhooks/meta)
	metaPageID := os.Getenv("META_PAGE_ID")
	if metaPageID == "" {
		metaPageID = "buyacar-page-sim"
	}
	metaVerifyToken := os.Getenv("META_VERIFY_TOKEN")
	if metaVerifyToken == "" {
		metaVerifyToken = "buyacar-verify-sim"
	}
	metaAppSecret := os.Getenv("META_APP_SECRET")
	if metaAppSecret == "" {
		metaAppSecret = "buyacar-secret-sim"
	}
	metaBaseURL := strings.TrimSpace(os.Getenv("META_GRAPH_BASE_URL"))
	if metaBaseURL == "" && strings.TrimSpace(os.Getenv("META_ACCESS_TOKEN")) == "" {
		metaBaseURL = fmt.Sprintf("http://127.0.0.1:%d/api/meta/sandbox", *portFlag)
	}
	metaAdapter := channel.NewMetaOmnichannelAdapter(channel.MetaOmnichannelConfig{
		PageID:      metaPageID,
		InstagramID: "buyacar-ig-sim",
		AccessToken: os.Getenv("META_ACCESS_TOKEN"),
		VerifyToken: metaVerifyToken,
		AppSecret:   metaAppSecret,
		BaseURL:     metaBaseURL,
	}, resTracker, rateCtrl)
	messengerSub := metaAdapter.MessengerSubChannel()
	instagramSub := metaAdapter.InstagramSubChannel()
	_ = eng.RegisterChannel(messengerSub)
	_ = eng.RegisterChannel(instagramSub)

	activeChannels := []string{channel.EmailPilotChannelID, "whatsapp", "facebook_marketplace", channel.ChannelIDMessenger}
	metricsExporter := server.NewMetricsExporter(obsCollector, escQ, resTracker, rateCtrl, string(cfg.OperatingMode), activeChannels)
	dashboardHandler := server.NewDashboardHandler(escQ, resTracker, rateCtrl, metricsExporter, string(cfg.OperatingMode), activeChannels)

	leadStore := crm.NewMemoryLeadStore()
	adminSecret := os.Getenv("BUYACAR_ADMIN_PASSWORD")
	if adminSecret == "" {
		adminSecret = "buyacar2026"
	}
	dashboardHandler.SetCRMManagers(crmMgr, leadStore, adminSecret)

	var lastWebhookMu sync.Mutex
	var lastWebhookOutcome core.PipelineOutcome

	metaAdapter.SetOnInboundConversation(func(chID string, conv channel.Conversation) {
		var subCh channel.Channel = messengerSub
		if chID == channel.ChannelIDInstagram {
			subCh = instagramSub
		}
		wCtx, wCancel := context.WithTimeout(ctx, 10*time.Second)
		defer wCancel()
		sess, openErr := subCh.Open(wCtx, channel.Link{
			ID:        "link-meta-" + chID,
			URL:       "https://buyacar.com.co/marketplace/facebook",
			ChannelID: chID,
			Active:    true,
		})
		if openErr != nil {
			return
		}
		start := time.Now()
		out, _ := pipe.ProcessConversation(wCtx, subCh, sess, conv, start.UTC())
		lastWebhookMu.Lock()
		lastWebhookOutcome = out
		lastWebhookMu.Unlock()
		elapsedMs := float64(time.Since(start).Microseconds()) / 1000.0
		obsCollector.RecordFastPathLatency(elapsedMs)
		obsCollector.RecordOutcome(out.UsedFastPath, out.UsedAI, out.Escalated, out.EscalationReason, out.Skipped, out.SkipReason, out.InputTokens, out.OutputTokens)

		// Actualizar perfil de CRM / embudo de ventas para trazabilidad en vivo
		th, thErr := subCh.Read(wCtx, sess, conv)
		lastText := ""
		if thErr == nil && len(th.Messages) > 0 {
			lastText = th.Messages[len(th.Messages)-1].Text
		}
		intentScore, stage := crm.CalculateIntentScore(lastText, "", 1)
		detectedPhone := crm.ExtractColombianPhone(lastText)
		leadStore.UpsertLead(wCtx, crm.LeadProfile{
			CustomerID:      conv.ID,
			CustomerAlias:   conv.CustomerAlias,
			ChannelID:       chID,
			Phone:           detectedPhone,
			WhatsAppHandoff: detectedPhone != "",
			IntentScore:     intentScore,
			Stage:           stage,
			AppointmentSede: "Av. Oriental - Cra. 6 #52-23, frente a la Toyota, Tunja",
			LastInteraction: time.Now().UTC(),
		})

		if out.SentReply != "" && !out.Escalated {
			metricsExporter.IncrementChannelMessages(chID)
			status := "pending"
			subjPrefix := "Marketplace/Meta"
			if eng.OperatingMode() == channel.ModeAutopilot {
				status = "pending"
				subjPrefix = "AUTOPILOT ENVIADO (" + string(out.SendStatus) + ")"
			}
			dashboardHandler.RegisterDraft(server.DraftItem{
				DraftID:        fmt.Sprintf("draft-meta-%s-%d", conv.ID, start.UnixNano()),
				ConversationID: conv.ID,
				CustomerAlias:  conv.CustomerAlias,
				Subject:        fmt.Sprintf("%s [%s]", subjPrefix, chID),
				DraftBody:      out.SentReply,
				Status:         status,
				CreatedAt:      start.UTC(),
			})
		}
	})

	dashboardHandler.SetMetaWebhookHandler(metaAdapter.HandleWebhook)
	dashboardHandler.SetKillSwitchHandler(func(active bool) bool {
		eng.SetKillSwitch(active)
		return eng.IsKillSwitchActive()
	})
	dashboardHandler.SetModeSwitchHandler(func(mode string) string {
		eng.SetOperatingMode(channel.Mode(mode))
		dashboardHandler.SetOperatingMode(mode)
		metricsExporter.SetOperatingMode(mode)
		return string(eng.OperatingMode())
	})
	dashboardHandler.SetMetaConfigHandler(func(pageID, accessToken, verifyToken, appSecret string) map[string]interface{} {
		targetBase := ""
		if strings.TrimSpace(accessToken) != "" && strings.TrimSpace(os.Getenv("META_GRAPH_BASE_URL")) == "" {
			targetBase = channel.DefaultMetaGraphBaseURL
		}
		metaAdapter.UpdateConfig(pageID, accessToken, verifyToken, appSecret, targetBase)
		snap := metaAdapter.ConfigSnapshot()
		return map[string]interface{}{
			"status":         "updated",
			"page_id":        snap.PageID,
			"verify_token":   snap.VerifyToken,
			"token_present":  snap.AccessToken != "",
			"graph_base_url": snap.BaseURL,
		}
	})
	dashboardHandler.SetGeminiConfigHandler(func(apiKey string) map[string]interface{} {
		if strings.TrimSpace(apiKey) != "" && geminiProv != nil {
			geminiProv.SetAPIKey(apiKey)
			pipe.Strategy = "A4_adaptive"
		}
		if geminiProv != nil {
			snap := geminiProv.ConfigSnapshot()
			snap["strategy"] = pipe.Strategy
			return snap
		}
		return map[string]interface{}{"configured": false, "strategy": pipe.Strategy}
	})

	var simWebhookSeq int64
	dashboardHandler.SetWebhookSimulatorHandler(func(adTitle, message string) (map[string]interface{}, error) {
		simWebhookSeq++
		if strings.TrimSpace(message) == "" {
			message = "¿Sigue disponible?"
		}
		snap := metaAdapter.ConfigSnapshot()
		nowMs := time.Now().UnixMilli()
		senderID := fmt.Sprintf("comprador-mkt-%d", simWebhookSeq)
		mid := fmt.Sprintf("mid.sim.%d.%d", nowMs, simWebhookSeq)

		entryMap := map[string]interface{}{
			"object": "page",
			"entry": []map[string]interface{}{
				{
					"id":   snap.PageID,
					"time": nowMs,
					"messaging": []map[string]interface{}{
						{
							"sender":    map[string]string{"id": senderID},
							"recipient": map[string]string{"id": snap.PageID},
							"timestamp": nowMs,
							"referral":  map[string]string{"ad_title": strings.TrimSpace(adTitle)},
							"message": map[string]string{
								"mid":  mid,
								"text": strings.TrimSpace(message),
							},
						},
					},
				},
			},
		}
		payloadBytes, _ := json.Marshal(entryMap)
		mac := hmac.New(sha256.New, []byte(snap.AppSecret))
		_, _ = mac.Write(payloadBytes)
		sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/webhooks/meta", bytes.NewReader(payloadBytes))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hub-Signature-256", sig)

		rec := &memoryResponseRecorder{header: make(http.Header)}
		metaAdapter.HandleWebhook(rec, req)

		lastWebhookMu.Lock()
		out := lastWebhookOutcome
		lastWebhookMu.Unlock()

		detectedPhone := crm.ExtractColombianPhone(message)
		if detectedPhone != "" {
			leadStore.UpsertLead(ctx, crm.LeadProfile{
				CustomerID:      senderID,
				CustomerAlias:   "Comprador Marketplace Tunja",
				ChannelID:       "facebook_marketplace",
				Phone:           detectedPhone,
				WhatsAppHandoff: true,
				IntentScore:     95,
				Stage:           crm.StageAppointment,
				TargetVehicle:   adTitle,
				AppointmentSede: "Av. Oriental - Cra. 6 #52-23, frente a la Toyota, Tunja",
				LastInteraction: time.Now().UTC(),
				Notes:           []string{"Teléfono capturado en Marketplace. Enrutado a WhatsApp: " + detectedPhone},
			})
		}

		return map[string]interface{}{
			"webhook_http_status": rec.code,
			"operating_mode":      string(eng.OperatingMode()),
			"kill_switch_active":  eng.IsKillSwitchActive(),
			"sender_id":           senderID,
			"ad_title":            adTitle,
			"inbound_message":     message,
			"detected_phone":      detectedPhone,
			"whatsapp_handoff":    detectedPhone != "",
			"send_status":         string(out.SendStatus),
			"dispatched_reply":    out.SentReply,
			"escalated":           out.Escalated,
			"escalation_reason":   out.EscalationReason,
			"used_fast_path":      out.UsedFastPath,
			"crm_task_id":         out.CRMTaskID,
		}, nil
	})

	// Enlazar aprobación de borradores desde el dashboard hacia el canal piloto
	dashboardHandler.SetOnDraftApprove(func(draftID string) error {
		if strings.HasPrefix(draftID, "draft-meta-") {
			return nil
		}
		return emailPilot.ReviewDraft(draftID, "asesor-humano-dashboard", false)
	})

	// Enlazar Copiloto interactivo de Facebook Marketplace (CP1-CP7 + Inventario + RAG + Síntesis Fáctica)
	var copilotSeq int64
	dashboardHandler.SetCopilotHandler(func(cCtx context.Context, req server.CopilotRequest) (server.CopilotResponse, error) {
		start := time.Now()
		copilotSeq++
		convID := fmt.Sprintf("copilot-mkt-%d-%d", start.UnixNano(), copilotSeq)
		conv := channel.Conversation{
			ID:            convID,
			ChannelID:     "facebook_marketplace",
			CustomerAlias: req.CustomerAlias,
			UpdatedAt:     start.UTC(),
		}
		var msgs []channel.Message
		var priorTexts []string
		if adTitle := strings.TrimSpace(req.AdTitle); adTitle != "" {
			refText := fmt.Sprintf("[Anuncio Marketplace: %s]", adTitle)
			msgs = append(msgs, channel.Message{
				ID:        fmt.Sprintf("ref-%d", copilotSeq),
				Sender:    "customer",
				Text:      refText,
				Timestamp: start.UTC().Add(-1 * time.Second),
			})
			priorTexts = append(priorTexts, refText)
		}
		msgs = append(msgs, channel.Message{
			ID:        fmt.Sprintf("msg-%d", copilotSeq),
			Sender:    "customer",
			Text:      req.Message,
			Timestamp: start.UTC(),
		})
		fbMarketplace.SeedThread(conv, msgs)
		sess, _ := fbMarketplace.Open(cCtx, channel.Link{
			ID:        "link-fb-marketplace",
			URL:       "https://buyacar.com.co/marketplace/facebook",
			ChannelID: "facebook_marketplace",
			Active:    true,
		})

		cl := triage.ClassifyWithContext(req.Message, priorTexts)
		scoredRAG, _ := ragEng.Retrieve(cCtx, rag.Query{Text: req.Message, TopK: 2, MaxTokens: 250})
		var ragTitles []string
		for _, sc := range scoredRAG {
			ragTitles = append(ragTitles, fmt.Sprintf("%s (%.2f)", sc.Chunk.Title, sc.Score))
		}

		pipeTimeout := 400 * time.Millisecond
		if geminiProv != nil && geminiProv.HasAPIKey() {
			pipeTimeout = 8 * time.Second
		}
		pipeCtx, pipeCancel := context.WithTimeout(cCtx, pipeTimeout)
		out, _ := pipe.ProcessConversation(pipeCtx, fbMarketplace, sess, conv, start.UTC())
		pipeCancel()

		// Síntesis determinista respaldada por RAG cuando el proveedor LLM externo no está disponible o excede el SLA interactivo pero el RAG local tiene respuesta fáctica verificada
		if out.Escalated && (out.EscalationReason == "ai_providers_failed_or_exhausted" || strings.Contains(out.EscalationReason, "failed")) && len(scoredRAG) > 0 && scoredRAG[0].Score >= 0.65 {
			top := scoredRAG[0].Chunk
			snippet := strings.TrimSpace(strings.ReplaceAll(top.Content, "\n", " "))
			if runes := []rune(snippet); len(runes) > 260 {
				snippet = string(runes[:260]) + "..."
			}
			synth := fmt.Sprintf("BuyaCAR Tunja (%s): %s WhatsApp 3151212223. Más info: https://buyacar.com.co", top.Title, snippet)
			chk := pipe.Guard.ValidateOutboxWithBackedNumbers(synth, "facebook_marketplace", nil, ragEng.GetBackedNumbers())
			if chk.Approved {
				out.Escalated = false
				out.EscalationReason = ""
				out.SentReply = chk.SanitizedReply
				out.UsedAI = true
			}
		}

		elapsedUs := time.Since(start).Microseconds()
		obsCollector.RecordFastPathLatency(float64(elapsedUs) / 1000.0)
		obsCollector.RecordOutcome(out.UsedFastPath, out.UsedAI, out.Escalated, out.EscalationReason, out.Skipped, out.SkipReason, out.InputTokens, out.OutputTokens)

		provUsed := "rag_synth"
		if out.UsedFastPath {
			provUsed = "fast_path"
		} else if out.UsedAI && geminiProv != nil && geminiProv.HasAPIKey() && (out.InputTokens+out.OutputTokens) > 0 {
			provUsed = "gemini-2.5-flash"
		}

		intentScore, funnelStage := crm.CalculateIntentScore(req.Message, req.AdTitle, 1)

		detectedPhone := crm.ExtractColombianPhone(req.Message)
		if detectedPhone != "" {
			leadStore.UpsertLead(cCtx, crm.LeadProfile{
				CustomerID:      convID,
				CustomerAlias:   req.CustomerAlias,
				ChannelID:       req.ChannelID,
				Phone:           detectedPhone,
				WhatsAppHandoff: true,
				IntentScore:     intentScore,
				Stage:           funnelStage,
				TargetVehicle:   req.AdTitle,
				AppointmentSede: "Av. Oriental - Cra. 6 #52-23, frente a la Toyota, Tunja",
				LastInteraction: time.Now().UTC(),
				Notes:           []string{"Contacto WhatsApp recibido: " + detectedPhone},
			})
		}

		return server.CopilotResponse{
			ChannelID:        req.ChannelID,
			Intent:           string(cl.Intent),
			Confidence:       cl.Confidence,
			UsedFastPath:     out.UsedFastPath,
			UsedRAG:          len(ragTitles) > 0 && !out.UsedFastPath,
			ProviderUsed:     provUsed,
			Escalated:        out.Escalated,
			EscalationReason: out.EscalationReason,
			ProposedReply:    out.SentReply,
			RAGChunks:        ragTitles,
			LatencyUs:        elapsedUs,
			TokensUsed:       out.InputTokens + out.OutputTokens,
			IntentScore:      intentScore,
			FunnelStage:      string(funnelStage),
		}, nil
	})

	// Registrar borrador de demostración en el dashboard
	dashboardHandler.RegisterDraft(server.DraftItem{
		DraftID:        "draft-tunja-lead-01",
		ConversationID: emailConv.ID,
		CustomerAlias:  emailConv.CustomerAlias,
		Subject:        "Re: Consulta Toyota Hilux 2022",
		DraftBody:      "BuyaCAR Tunja: La Toyota Hilux 2022 está disponible. Para detalles visite https://buyacar.com.co.",
		Status:         "pending",
		CreatedAt:      now,
	})

	// 5. Modo CLI --metrics: volcados estructurados JSON a stdout
	if *metricsFlag {
		snap := metricsExporter.Snapshot()
		metricsOutput := map[string]interface{}{
			"fast_path_p99_us":              snap.FastPathP99Us,
			"fast_path_p95_us":              snap.FastPathP95Us,
			"cache_hits_total":              snap.CacheHitsTotal,
			"cache_misses_total":            snap.CacheMissesTotal,
			"operating_mode":                snap.OperatingMode,
			"pending_escalations":           snap.EscalateQueueSize,
			"drafts_pending":                dashboardHandler.PendingDraftsCount(),
			"channels_consecutive_failures": snap.ChannelsConsecutive,
			"channels_paused":               snap.ChannelsPaused,
		}
		b, _ := json.Marshal(metricsOutput)
		_, _ = fmt.Fprintln(stdout, string(b))
		return 0
	}

	// 6. Modo CLI --inspect: diagnóstico de salud en JSON a stdout
	if *inspectFlag {
		channelsHealth := make(map[string]string)
		for _, ch := range activeChannels {
			if resTracker.IsPaused(ch) {
				channelsHealth[ch] = "paused"
			} else {
				channelsHealth[ch] = "healthy"
			}
		}
		inspectOutput := map[string]interface{}{
			"status":                "operational",
			"active_mode":           string(cfg.OperatingMode),
			"operating_mode":        string(cfg.OperatingMode),
			"channels":              channelsHealth,
			"escalate_queue":        len(escQ.Pending()),
			"drafts_pending":        dashboardHandler.PendingDraftsCount(),
			"kill_switch_active":    eng.IsKillSwitchActive(),
			"inventory_live_synced": inventoryLiveSynced,
			"server_config": map[string]interface{}{
				"port":              *portFlag,
				"dashboard_enabled": true,
				"metrics_enabled":   true,
			},
		}
		b, _ := json.Marshal(inspectOutput)
		_, _ = fmt.Fprintln(stdout, string(b))
		return 0
	}

	// 7. Modo --server: arrancar servidor web embebido y bloquear hasta señal de apagado
	if *serverFlag {
		host := "127.0.0.1"
		if os.Getenv("RAILWAY_ENVIRONMENT") != "" || os.Getenv("HOST") != "" || os.Getenv("PORT") != "" {
			host = "0.0.0.0"
		}

		// En modo servidor permanente (Railway/Cloud), mantener fresca la cache de inventario periódicamente
		go func() {
			ticker := time.NewTicker(10 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case t := <-ticker.C:
					if fetcher != nil && os.Getenv("BUYACAR_LIVE_INVENTORY_SYNC") == "1" {
						_, _ = fetcher.Sync(ctx, invCache, t.UTC())
					} else if invCache != nil {
						invCache.TouchRefresh(t.UTC())
					}
				}
			}
		}()

		srvCfg := server.Config{
			Port:            *portFlag,
			Host:            host,
			OperatingMode:   string(cfg.OperatingMode),
			EnableDashboard: true,
			EnableMetrics:   true,
		}
		srv := server.NewServer(srvCfg, dashboardHandler, metricsExporter)
		srv.SetLogger(logger)

		logger.Info("starting buyacar omnicanal embedded supervisor server",
			"port", *portFlag,
			"mode", string(cfg.OperatingMode),
		)

		if err := srv.Start(ctx); err != nil && err != context.Canceled {
			logger.Error("server exited with error", "error", err)
			return 1
		}
		return 0
	}

	// 8. Modo batch por defecto: procesar conversaciones pendientes
	channelsByID := map[string]channel.Channel{
		channel.EmailPilotChannelID: emailPilot,
		"whatsapp":                  waStub,
		"facebook_marketplace":      fbMarketplace,
	}

	for _, lnk := range linkRepo.ActiveLinks() {
		ch, ok := channelsByID[lnk.ChannelID]
		if !ok {
			continue
		}
		sess, openErr := ch.Open(ctx, lnk)
		if openErr != nil {
			continue
		}
		pending, _ := ch.Pending(ctx, sess)
		for _, conv := range pending {
			start := time.Now()
			out, _ := pipe.ProcessConversation(ctx, ch, sess, conv, now)
			elapsedMs := float64(time.Since(start).Microseconds()) / 1000.0
			obsCollector.RecordFastPathLatency(elapsedMs)
			obsCollector.RecordOutcome(out.UsedFastPath, out.UsedAI, out.Escalated, out.EscalationReason, out.Skipped, out.SkipReason, out.InputTokens, out.OutputTokens)
		}
	}

	// En ejecución batch por defecto no se auto-aprueban borradores sintéticamente.
	// Las compuertas de salida a AUTOPILOT requieren revisión y aprobación humana real.
	noAI, noHuman := obsCollector.Ratios()
	_, _, p99 := obsCollector.FastPathPercentiles()
	totalDrafts, _, _, uneditedPct := emailPilot.DraftGateMetrics()
	exitGate := obsCollector.EvaluateDraftExitGate(uneditedPct)

	logger.Info("buyacar omnicanal execution cycle completed",
		"operating_mode", string(cfg.OperatingMode),
		"kill_switch_active", eng.IsKillSwitchActive(),
		"inventory_live_synced", inventoryLiveSynced,
		"gemini_api_key_present", os.Getenv("GEMINI_API_KEY") != "",
		"ollama_fallback_model", ai.DefaultOllamaModel,
		"email_pilot_drafts_recorded", totalDrafts,
		"email_pilot_unedited_approval_pct", uneditedPct,
		"resolved_without_ai_pct", noAI,
		"resolved_without_human_pct", noHuman,
		"fast_path_p99_ms", p99,
		"pending_human_tickets", len(pipe.EscQueue.Pending()),
		"ready_for_autopilot_gate", exitGate.ReadyForAutopilot,
	)

	return 0
}

type memoryResponseRecorder struct {
	header http.Header
	body   bytes.Buffer
	code   int
}

func (m *memoryResponseRecorder) Header() http.Header {
	return m.header
}

func (m *memoryResponseRecorder) Write(b []byte) (int, error) {
	if m.code == 0 {
		m.code = http.StatusOK
	}
	return m.body.Write(b)
}

func (m *memoryResponseRecorder) WriteHeader(statusCode int) {
	m.code = statusCode
}
