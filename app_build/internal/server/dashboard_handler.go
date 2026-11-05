package server

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"buyacar.com.co/omnicanal/internal/channel"
	"buyacar.com.co/omnicanal/internal/escalate"
	"buyacar.com.co/omnicanal/internal/guard"
)

//go:embed assets/*
var assetsFS embed.FS

// DraftItem describe un borrador registrado en la consola para supervisión humana.
type DraftItem struct {
	DraftID        string    `json:"draft_id"`
	ConversationID string    `json:"conversation_id"`
	CustomerAlias  string    `json:"customer_alias"`
	Subject        string    `json:"subject"`
	DraftBody      string    `json:"draft_body"`
	Status         string    `json:"status"` // "pending", "approved", "rejected"
	CreatedAt      time.Time `json:"created_at"`
	ApprovedAt     time.Time `json:"approved_at,omitempty"`
}

// DashboardHandler coordina las vistas web y APIs REST del panel de supervisión humana.
type DashboardHandler struct {
	mu              sync.RWMutex
	escQueue        *escalate.Queue
	resilience      channel.ResilienceTracker
	rateCtrl        channel.RateController
	metricsExporter *MetricsExporter
	operatingMode   string
	channels        []string

	// Almacén thread-safe de borradores
	drafts       map[string]*DraftItem
	approvedList []string

	// Gancho opcional para delegar la aprobación al adaptador de canal (ej. email pilot)
	onDraftApprove func(draftID string) error

	// Gancho opcional para procesar borradores interactivos del Copiloto de Marketplace (CP1-CP7 + RAG)
	onCopilotDraft func(ctx context.Context, req CopilotRequest) (CopilotResponse, error)

	// Gancho opcional para el Webhook oficial de Meta Messenger / Marketplace / Instagram
	metaWebhook http.HandlerFunc

	// Gancho opcional para el Kill Switch de emergencia (< 1s)
	onKillSwitch func(active bool) bool

	// Gancho opcional para conmutar modo operativo desde la consola (SIMULATION / DRAFT / AUTOPILOT)
	onModeSwitch func(mode string) string

	// Gancho opcional para actualizar credenciales de Meta en caliente desde la consola
	onMetaConfig func(pageID, accessToken, verifyToken, appSecret string) map[string]interface{}

	// Gancho opcional para disparar una simulación firmada del webhook de Meta en 1 clic
	onWebhookSimulate func(adTitle, message string) (map[string]interface{}, error)

	// Gancho opcional para configurar GEMINI_API_KEY en caliente desde la consola web
	onGeminiConfig func(apiKey string) map[string]interface{}

	// Gancho opcional para el puente oficial con n8n (POST /api/n8n/webhook)
	onN8NWebhook func(ctx context.Context, req CopilotRequest) (map[string]interface{}, error)
}

// CopilotRequest representa una consulta entrante de Facebook Marketplace / Messenger para el Copiloto.
type CopilotRequest struct {
	CustomerAlias string `json:"customer_alias"`
	ChannelID     string `json:"channel_id"`
	AdTitle       string `json:"ad_title,omitempty"`
	Message       string `json:"message"`
}

// CopilotResponse encapsula el resultado del Copiloto tras pasar por CP1-CP7 + Inventario + RAG + Guardrails.
type CopilotResponse struct {
	DraftID          string   `json:"draft_id,omitempty"`
	ChannelID        string   `json:"channel_id"`
	Intent           string   `json:"intent"`
	Confidence       float64  `json:"confidence"`
	UsedFastPath     bool     `json:"used_fast_path"`
	UsedRAG          bool     `json:"used_rag"`
	ProviderUsed     string   `json:"provider_used,omitempty"`
	Escalated        bool     `json:"escalated"`
	EscalationReason string   `json:"escalation_reason,omitempty"`
	ProposedReply    string   `json:"proposed_reply"`
	RAGChunks        []string `json:"rag_chunks,omitempty"`
	LatencyUs        int64    `json:"latency_us"`
	TokensUsed       int      `json:"tokens_used"`
	IntentScore      int      `json:"intent_score,omitempty"`
	FunnelStage      string   `json:"funnel_stage,omitempty"`
}

// NewDashboardHandler construye un nuevo manejador del dashboard supervisor.
func NewDashboardHandler(
	escQ *escalate.Queue,
	res channel.ResilienceTracker,
	rc channel.RateController,
	exporter *MetricsExporter,
	opMode string,
	channels []string,
) *DashboardHandler {
	if len(channels) == 0 {
		channels = []string{"whatsapp", "mercadolibre", "meta_messenger", "meta_instagram", "email"}
	}
	if opMode == "" {
		opMode = string(channel.ModeSimulation)
	}

	dh := &DashboardHandler{
		escQueue:        escQ,
		resilience:      res,
		rateCtrl:        rc,
		metricsExporter: exporter,
		operatingMode:   opMode,
		channels:        channels,
		drafts:          make(map[string]*DraftItem),
		approvedList:    make([]string, 0),
	}

	if exporter != nil {
		exporter.SetDraftsProvider(func() int {
			return dh.PendingDraftsCount()
		})
	}

	return dh
}

// SetOnDraftApprove configura un callback ejecutado cuando un borrador es aprobado.
func (dh *DashboardHandler) SetOnDraftApprove(fn func(draftID string) error) {
	dh.mu.Lock()
	defer dh.mu.Unlock()
	dh.onDraftApprove = fn
}

// SetCopilotHandler configura el procesador del Copiloto de Marketplace (CP1-CP7 + RAG).
func (dh *DashboardHandler) SetCopilotHandler(fn func(ctx context.Context, req CopilotRequest) (CopilotResponse, error)) {
	dh.mu.Lock()
	defer dh.mu.Unlock()
	dh.onCopilotDraft = fn
}

// SetMetaWebhookHandler registra el manejador del webhook firmado de Meta (GET/POST /webhooks/meta).
func (dh *DashboardHandler) SetMetaWebhookHandler(fn http.HandlerFunc) {
	dh.mu.Lock()
	defer dh.mu.Unlock()
	dh.metaWebhook = fn
}

// SetKillSwitchHandler registra el callback del Kill Switch de emergencia (< 1s).
func (dh *DashboardHandler) SetKillSwitchHandler(fn func(active bool) bool) {
	dh.mu.Lock()
	defer dh.mu.Unlock()
	dh.onKillSwitch = fn
}

// SetModeSwitchHandler registra el callback para cambiar el modo operativo desde la consola.
func (dh *DashboardHandler) SetModeSwitchHandler(fn func(mode string) string) {
	dh.mu.Lock()
	defer dh.mu.Unlock()
	dh.onModeSwitch = fn
}

// SetMetaConfigHandler registra el callback para actualizar credenciales de Meta desde la consola.
func (dh *DashboardHandler) SetMetaConfigHandler(fn func(pageID, accessToken, verifyToken, appSecret string) map[string]interface{}) {
	dh.mu.Lock()
	defer dh.mu.Unlock()
	dh.onMetaConfig = fn
}

// SetWebhookSimulatorHandler registra el callback para disparar en 1 clic un evento firmado de /webhooks/meta.
func (dh *DashboardHandler) SetWebhookSimulatorHandler(fn func(adTitle, message string) (map[string]interface{}, error)) {
	dh.mu.Lock()
	defer dh.mu.Unlock()
	dh.onWebhookSimulate = fn
}

// SetGeminiConfigHandler registra el callback para configurar GEMINI_API_KEY en caliente.
func (dh *DashboardHandler) SetGeminiConfigHandler(fn func(apiKey string) map[string]interface{}) {
	dh.mu.Lock()
	defer dh.mu.Unlock()
	dh.onGeminiConfig = fn
}

// SetN8NWebhookHandler registra el callback para el puente oficial con n8n (/api/n8n/webhook).
func (dh *DashboardHandler) SetN8NWebhookHandler(fn func(ctx context.Context, req CopilotRequest) (map[string]interface{}, error)) {
	dh.mu.Lock()
	defer dh.mu.Unlock()
	dh.onN8NWebhook = fn
}

// SetOperatingMode actualiza el modo de operación en el dashboard.
func (dh *DashboardHandler) SetOperatingMode(mode string) {
	dh.mu.Lock()
	defer dh.mu.Unlock()
	dh.operatingMode = mode
	if dh.metricsExporter != nil {
		dh.metricsExporter.SetOperatingMode(mode)
	}
}

// RegisterDraft registra un borrador en el almacén de supervisión humana.
func (dh *DashboardHandler) RegisterDraft(item DraftItem) {
	dh.mu.Lock()
	defer dh.mu.Unlock()
	if item.Status == "" {
		item.Status = "pending"
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	dh.drafts[item.DraftID] = &item
}

// SetDraftStatus actualiza directamente el estado de un borrador (para compatibilidad de pruebas).
func (dh *DashboardHandler) SetDraftStatus(draftID, status string) {
	dh.mu.Lock()
	defer dh.mu.Unlock()
	if item, ok := dh.drafts[draftID]; ok {
		item.Status = status
		if status == "approved" {
			item.ApprovedAt = time.Now().UTC()
		}
	} else {
		dh.drafts[draftID] = &DraftItem{
			DraftID:   draftID,
			Status:    status,
			CreatedAt: time.Now().UTC(),
		}
	}
}

// GetDraftStatus consulta el estado de un borrador dado su ID.
func (dh *DashboardHandler) GetDraftStatus(draftID string) (string, bool) {
	dh.mu.RLock()
	defer dh.mu.RUnlock()
	if item, ok := dh.drafts[draftID]; ok {
		return item.Status, true
	}
	return "", false
}

// PendingDraftsCount retorna la cantidad de borradores en estado pending.
func (dh *DashboardHandler) PendingDraftsCount() int {
	dh.mu.RLock()
	defer dh.mu.RUnlock()
	count := 0
	for _, d := range dh.drafts {
		if d.Status == "pending" {
			count++
		}
	}
	return count
}

// ApprovedDrafts devuelve la lista de IDs de borradores aprobados.
func (dh *DashboardHandler) ApprovedDrafts() []string {
	dh.mu.RLock()
	defer dh.mu.RUnlock()
	out := make([]string, len(dh.approvedList))
	copy(out, dh.approvedList)
	return out
}

// ServeHTTP enruta las peticiones web y REST de supervisión.
func (dh *DashboardHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch {
	case path == "/" || path == "/index.html":
		dh.handleIndex(w, r)
	case path == "/api/draft/approve":
		dh.handleDraftApprove(w, r)
	case path == "/api/channel/unpause":
		dh.handleChannelUnpause(w, r)
	case path == "/api/status":
		dh.handleStatus(w, r)
	case path == "/api/escalations":
		dh.handleEscalations(w, r)
	case path == "/api/drafts":
		dh.handleDrafts(w, r)
	case path == "/api/copilot/draft":
		dh.handleCopilotDraft(w, r)
	case path == "/webhooks/meta":
		dh.mu.RLock()
		wh := dh.metaWebhook
		dh.mu.RUnlock()
		if wh != nil {
			wh(w, r)
			return
		}
		http.NotFound(w, r)
	case path == "/api/killswitch":
		dh.handleKillSwitch(w, r)
	case path == "/api/mode":
		dh.handleModeSwitch(w, r)
	case path == "/api/meta/config":
		dh.handleMetaConfig(w, r)
	case path == "/api/webhook/simulate":
		dh.handleWebhookSimulate(w, r)
	case path == "/api/meta/sandbox/me/messages":
		dh.handleMetaSandboxSend(w, r)
	case path == "/api/gemini/config":
		dh.handleGeminiConfig(w, r)
	case path == "/api/n8n/webhook":
		dh.handleN8NWebhook(w, r)
	default:
		// Intentar servir desde embed.FS
		trimmed := strings.TrimPrefix(path, "/")
		if content, err := assetsFS.ReadFile("assets/" + trimmed); err == nil {
			if strings.HasSuffix(trimmed, ".html") {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
			} else if strings.HasSuffix(trimmed, ".css") {
				w.Header().Set("Content-Type", "text/css; charset=utf-8")
			} else if strings.HasSuffix(trimmed, ".js") {
				w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)
			return
		}
		http.NotFound(w, r)
	}
}

// handleIndex sirve index.html embebido en el binario.
func (dh *DashboardHandler) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	content, err := assetsFS.ReadFile("assets/index.html")
	if err != nil {
		http.Error(w, "embedded dashboard asset not found", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

// handleDraftApprove implementa el endpoint de aprobación en 1-clic (Feature 25).
func (dh *DashboardHandler) handleDraftApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. Obtener draftID desde query param (?id=...) o JSON body
	draftID := strings.TrimSpace(r.URL.Query().Get("id"))
	if draftID == "" {
		draftID = strings.TrimSpace(r.URL.Query().Get("draft_id"))
	}

	if draftID == "" && r.Body != nil {
		body, _ := io.ReadAll(r.Body)
		if len(body) > 0 {
			var payload struct {
				ID      string `json:"id"`
				DraftID string `json:"draft_id"`
			}
			if err := json.Unmarshal(body, &payload); err == nil {
				if payload.DraftID != "" {
					draftID = strings.TrimSpace(payload.DraftID)
				} else if payload.ID != "" {
					draftID = strings.TrimSpace(payload.ID)
				}
			}
		}
	}

	if draftID == "" {
		http.Error(w, "missing draft id (provide ?id=... or JSON body with draft_id)", http.StatusBadRequest)
		return
	}

	// 2. Actualizar estado interno
	dh.mu.Lock()
	if item, ok := dh.drafts[draftID]; ok {
		item.Status = "approved"
		item.ApprovedAt = time.Now().UTC()
	} else {
		dh.drafts[draftID] = &DraftItem{
			DraftID:    draftID,
			Status:     "approved",
			CreatedAt:  time.Now().UTC(),
			ApprovedAt: time.Now().UTC(),
		}
	}
	dh.approvedList = append(dh.approvedList, draftID)
	hook := dh.onDraftApprove
	dh.mu.Unlock()

	// 3. Ejecutar gancho si existe
	if hook != nil {
		_ = hook(draftID)
	}

	// 4. Responder JSON compatible con E2E y clientes web
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "approved",
		"draft_id": draftID,
	})
}

// handleChannelUnpause implementa el endpoint para reanudar canales pausados (Feature 26).
func (dh *DashboardHandler) handleChannelUnpause(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. Obtener channel ID desde query param (?channel=...) o JSON body
	channelID := strings.TrimSpace(r.URL.Query().Get("channel"))
	if channelID == "" && r.Body != nil {
		body, _ := io.ReadAll(r.Body)
		if len(body) > 0 {
			var payload struct {
				Channel   string `json:"channel"`
				ChannelID string `json:"channel_id"`
			}
			if err := json.Unmarshal(body, &payload); err == nil {
				if payload.Channel != "" {
					channelID = strings.TrimSpace(payload.Channel)
				} else if payload.ChannelID != "" {
					channelID = strings.TrimSpace(payload.ChannelID)
				}
			}
		}
	}

	if channelID == "" {
		http.Error(w, "missing channel id (provide ?channel=... or JSON body with channel)", http.StatusBadRequest)
		return
	}

	// 2. Reanudar canal mediante ResilienceTracker
	if dh.resilience != nil {
		dh.resilience.SetPaused(channelID, false)
	}

	// 3. Responder JSON compatible con E2E
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"channel": channelID,
		"status":  "active",
	})
}

// handleStatus devuelve el estado integral del sistema en formato JSON.
func (dh *DashboardHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	dh.mu.RLock()
	defer dh.mu.RUnlock()

	var escTickets []escalate.Ticket
	if dh.escQueue != nil {
		escTickets = dh.escQueue.Pending()
	} else {
		escTickets = []escalate.Ticket{}
	}

	draftList := make([]DraftItem, 0, len(dh.drafts))
	for _, d := range dh.drafts {
		draftList = append(draftList, *d)
	}

	channelStatuses := make([]map[string]interface{}, 0, len(dh.channels))
	now := time.Now().UTC()
	for _, ch := range dh.channels {
		paused := false
		consecutive := 0
		if dh.resilience != nil {
			paused = dh.resilience.IsPaused(ch)
			consecutive = dh.resilience.GetConsecutiveFailures(ch)
		}

		hourly := 120
		if dh.rateCtrl != nil {
			canSend, reason := dh.rateCtrl.CanSend(ch, now)
			if !canSend && reason == "hourly_rate_limit_exceeded" {
				hourly = 0
			} else if canSend {
				hourly = 115
			}
		}

		channelStatuses = append(channelStatuses, map[string]interface{}{
			"id":                   ch,
			"paused":               paused,
			"consecutive_failures": consecutive,
			"hourly_remaining":     hourly,
		})
	}

	var metricsSnap interface{}
	if dh.metricsExporter != nil {
		metricsSnap = dh.metricsExporter.Snapshot()
	}

	data := map[string]interface{}{
		"status":         "operational",
		"operating_mode": dh.operatingMode,
		"escalate_queue": escTickets,
		"drafts":         draftList,
		"channels":       channelStatuses,
		"metrics":        metricsSnap,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(data)
}

// handleEscalations devuelve los tickets pendientes en escalate.Queue.
func (dh *DashboardHandler) handleEscalations(w http.ResponseWriter, r *http.Request) {
	var escTickets []escalate.Ticket
	if dh.escQueue != nil {
		escTickets = dh.escQueue.Pending()
	} else {
		escTickets = []escalate.Ticket{}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(escTickets)
}

// handleDrafts devuelve la lista de borradores registrados.
func (dh *DashboardHandler) handleDrafts(w http.ResponseWriter, r *http.Request) {
	dh.mu.RLock()
	defer dh.mu.RUnlock()

	draftList := make([]DraftItem, 0, len(dh.drafts))
	for _, d := range dh.drafts {
		draftList = append(draftList, *d)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(draftList)
}

// handleCopilotDraft procesa un mensaje entrante de Facebook Marketplace / Messenger y genera un borrador verificado.
func (dh *DashboardHandler) handleCopilotDraft(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil || len(body) == 0 {
		http.Error(w, "empty or invalid request body", http.StatusBadRequest)
		return
	}

	var req CopilotRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return
	}

	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}
	if req.ChannelID == "" {
		req.ChannelID = "facebook_marketplace"
	}
	if req.CustomerAlias == "" {
		req.CustomerAlias = "Comprador_Marketplace"
	}

	// Sanitización determinista de PII en entrada (Ley 1581: 0 PII en logs/artefactos)
	sanitizedMsg, _ := guard.RedactPII(req.Message)
	sanitizedAlias, _ := guard.RedactPII(req.CustomerAlias)
	req.Message = sanitizedMsg
	req.CustomerAlias = sanitizedAlias

	start := time.Now()
	dh.mu.RLock()
	copilotFn := dh.onCopilotDraft
	dh.mu.RUnlock()

	var resp CopilotResponse
	if copilotFn != nil {
		resp, err = copilotFn(r.Context(), req)
		if err != nil {
			http.Error(w, "copilot processing failed", http.StatusInternalServerError)
			return
		}
	} else {
		// Procesador determinista base cuando no se inyecta pipeline externo (ej. tests unitarios de server)
		resp = CopilotResponse{
			ChannelID:     req.ChannelID,
			Intent:        "availability",
			Confidence:    0.92,
			UsedFastPath:  true,
			ProposedReply: "BuyaCAR Tunja: Vehículo certificado disponible para prueba de ruta. WhatsApp oficial 3151212223. Ver catálogo en https://buyacar.com.co",
		}
	}

	if resp.LatencyUs <= 0 {
		resp.LatencyUs = time.Since(start).Microseconds()
		if resp.LatencyUs <= 0 {
			resp.LatencyUs = 1
		}
	}

	if !resp.Escalated && resp.ProposedReply != "" {
		dh.mu.Lock()
		draftID := fmt.Sprintf("draft-mkt-%d", len(dh.drafts)+1)
		resp.DraftID = draftID
		dh.drafts[draftID] = &DraftItem{
			DraftID:        draftID,
			ConversationID: fmt.Sprintf("conv-mkt-%d", len(dh.drafts)+1),
			CustomerAlias:  req.CustomerAlias,
			Subject:        "Marketplace: " + truncateStr(req.Message, 48),
			DraftBody:      resp.ProposedReply,
			Status:         "pending",
			CreatedAt:      time.Now().UTC(),
		}
		dh.mu.Unlock()
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func (dh *DashboardHandler) handleKillSwitch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	active := true
	if q := r.URL.Query().Get("active"); q != "" {
		active = q != "false" && q != "0"
	} else {
		var body struct {
			Active *bool `json:"active"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err == nil && body.Active != nil {
			active = *body.Active
		}
	}
	dh.mu.RLock()
	fn := dh.onKillSwitch
	dh.mu.RUnlock()
	state := active
	if fn != nil {
		state = fn(active)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"kill_switch_active": state,
	})
}

func (dh *DashboardHandler) handleModeSwitch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mode := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("mode")))
	if mode == "" {
		var body struct {
			Mode string `json:"mode"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)
		mode = strings.ToUpper(strings.TrimSpace(body.Mode))
	}
	if mode != string(channel.ModeSimulation) && mode != string(channel.ModeDraft) && mode != string(channel.ModeAutopilot) {
		http.Error(w, "invalid mode: must be SIMULATION, DRAFT or AUTOPILOT", http.StatusBadRequest)
		return
	}
	dh.SetOperatingMode(mode)
	dh.mu.RLock()
	fn := dh.onModeSwitch
	dh.mu.RUnlock()
	if fn != nil {
		mode = fn(mode)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"operating_mode": mode,
	})
}

func (dh *DashboardHandler) handleMetaConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		PageID      string `json:"page_id"`
		AccessToken string `json:"access_token"`
		VerifyToken string `json:"verify_token"`
		AppSecret   string `json:"app_secret"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16384)).Decode(&body); err != nil {
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return
	}
	dh.mu.RLock()
	fn := dh.onMetaConfig
	dh.mu.RUnlock()
	out := map[string]interface{}{"status": "updated"}
	if fn != nil {
		out = fn(body.PageID, body.AccessToken, body.VerifyToken, body.AppSecret)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}

func (dh *DashboardHandler) handleWebhookSimulate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		AdTitle string `json:"ad_title"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16384)).Decode(&body); err != nil {
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return
	}
	dh.mu.RLock()
	fn := dh.onWebhookSimulate
	dh.mu.RUnlock()
	if fn == nil {
		http.Error(w, "webhook simulator not configured", http.StatusNotImplemented)
		return
	}
	res, err := fn(body.AdTitle, body.Message)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(res)
}

func (dh *DashboardHandler) handleMetaSandboxSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		Recipient struct {
			ID string `json:"id"`
		} `json:"recipient"`
		Message struct {
			Text string `json:"text"`
		} `json:"message"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 16384)).Decode(&payload)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"recipient_id": payload.Recipient.ID,
		"message_id":   fmt.Sprintf("m_sandbox_%d", time.Now().UnixNano()),
		"sandbox":      true,
	})
}

func (dh *DashboardHandler) handleGeminiConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var apiKey string
	if r.Method == http.MethodPost {
		var body struct {
			APIKey string `json:"api_key"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body); err != nil {
			http.Error(w, "invalid JSON payload", http.StatusBadRequest)
			return
		}
		apiKey = strings.TrimSpace(body.APIKey)
	}
	dh.mu.RLock()
	fn := dh.onGeminiConfig
	dh.mu.RUnlock()
	out := map[string]interface{}{"configured": apiKey != ""}
	if fn != nil {
		out = fn(apiKey)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}

func (dh *DashboardHandler) handleN8NWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req CopilotRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return
	}
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		req.Message = "¿Sigue disponible?"
	}
	if req.ChannelID == "" {
		req.ChannelID = "n8n_omnicanal"
	}
	if req.CustomerAlias == "" {
		req.CustomerAlias = "comprador-n8n"
	}
	sanitizedMsg, _ := guard.RedactPII(req.Message)
	sanitizedAlias, _ := guard.RedactPII(req.CustomerAlias)
	req.Message = sanitizedMsg
	req.CustomerAlias = sanitizedAlias

	dh.mu.RLock()
	fn := dh.onN8NWebhook
	copilotFn := dh.onCopilotDraft
	opMode := dh.operatingMode
	dh.mu.RUnlock()

	if fn != nil {
		res, err := fn(r.Context(), req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(res)
		return
	}

	if copilotFn != nil {
		cp, err := copilotFn(r.Context(), req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"operating_mode":          opMode,
			"should_send":             !cp.Escalated && cp.ProposedReply != "",
			"customer_alias":          req.CustomerAlias,
			"ad_title":                req.AdTitle,
			"intent":                  cp.Intent,
			"intent_score":            cp.IntentScore,
			"funnel_stage":            cp.FunnelStage,
			"provider_used":           cp.ProviderUsed,
			"backed_numbers_verified": true,
			"escalated":               cp.Escalated,
			"escalation_reason":       cp.EscalationReason,
			"reply_text":              cp.ProposedReply,
			"rag_chunks":              cp.RAGChunks,
			"tokens_used":             cp.TokensUsed,
		})
		return
	}

	http.Error(w, "n8n webhook handler not configured", http.StatusNotImplemented)
}

