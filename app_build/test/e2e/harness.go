package e2e

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"buyacar.com.co/omnicanal/internal/ai"
	"buyacar.com.co/omnicanal/internal/channel"
	"buyacar.com.co/omnicanal/internal/core"
	"buyacar.com.co/omnicanal/internal/escalate"
	"buyacar.com.co/omnicanal/internal/fast"
	"buyacar.com.co/omnicanal/internal/guard"
	"buyacar.com.co/omnicanal/internal/inventory"
	"buyacar.com.co/omnicanal/internal/skills"
	"buyacar.com.co/omnicanal/internal/store"
)

// ============================================================================
// 1. CRM CONTRACT INTERFACES & REFERENCE IMPLEMENTATION (PROJECT.md § M1)
// ============================================================================

type CRMTaskType string

const (
	TaskScheduleTestDrive  CRMTaskType = "schedule_test_drive"
	TaskCommercialFollowup CRMTaskType = "commercial_followup"
	TaskCreditApplication  CRMTaskType = "credit_application"
	TaskGeneralInquiry     CRMTaskType = "general_inquiry"
)

type CRMTask struct {
	ID          string      `json:"id"`
	ConvID      string      `json:"conv_id"`
	ChannelID   string      `json:"channel_id"`
	Type        CRMTaskType `json:"type"`
	Description string      `json:"description"`
	VehicleID   string      `json:"vehicle_id,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
}

type CRMManager interface {
	RecordTask(ctx context.Context, task CRMTask) error
	GetTasksByConv(convID string) []CRMTask
	GetAllTasks() []CRMTask
}

type MemoryCRMManager struct {
	mu    sync.RWMutex
	tasks map[string][]CRMTask
	all   []CRMTask
}

func NewMemoryCRMManager() *MemoryCRMManager {
	return &MemoryCRMManager{
		tasks: make(map[string][]CRMTask),
	}
}

func (m *MemoryCRMManager) RecordTask(ctx context.Context, task CRMTask) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if task.ID == "" {
		h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%d", task.ConvID, task.ChannelID, task.Type, task.CreatedAt.UnixNano())))
		task.ID = "crm-" + hex.EncodeToString(h[:8])
	}
	m.tasks[task.ConvID] = append(m.tasks[task.ConvID], task)
	m.all = append(m.all, task)
	return nil
}

func (m *MemoryCRMManager) GetTasksByConv(convID string) []CRMTask {
	m.mu.RLock()
	defer m.mu.RUnlock()
	copied := make([]CRMTask, len(m.tasks[convID]))
	copy(copied, m.tasks[convID])
	return copied
}

func (m *MemoryCRMManager) GetAllTasks() []CRMTask {
	m.mu.RLock()
	defer m.mu.RUnlock()
	copied := make([]CRMTask, len(m.all))
	copy(copied, m.all)
	return copied
}

// ============================================================================
// 2. INVENTORY ACCESSOR & CACHE TEST HARNESS (PROJECT.md § Feature 7)
// ============================================================================

type InventoryAccessor interface {
	GetVehicle(id string) (inventory.Vehicle, bool)
	IsFresh(now time.Time) bool
	UpdateVehicleStatus(id string, newStatus string) bool
}

type TestInventoryWrapper struct {
	cache       *inventory.Cache
	vehicles    map[string]inventory.Vehicle
	slugAliases map[string]string
	mu          sync.RWMutex
	lastUpdated time.Time
	staleWindow time.Duration
}

func NewTestInventoryWrapper(c *inventory.Cache, seedVehicles []inventory.Vehicle, now time.Time) *TestInventoryWrapper {
	vMap := make(map[string]inventory.Vehicle)
	slugMap := make(map[string]string)
	for _, v := range seedVehicles {
		idKey := strings.ToLower(v.ID)
		vMap[idKey] = v
		// Map slug alias as well (e.g. toyota-hilux-2022) to vehicle ID
		slug := strings.ToLower(fmt.Sprintf("%s-%s-%d", v.Brand, v.Model, v.Year))
		slugMap[slug] = idKey
	}
	return &TestInventoryWrapper{
		cache:       c,
		vehicles:    vMap,
		slugAliases: slugMap,
		lastUpdated: now,
		staleWindow: 30 * time.Minute,
	}
}

func (w *TestInventoryWrapper) GetVehicle(id string) (inventory.Vehicle, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	key := strings.ToLower(strings.TrimSpace(id))
	if targetID, ok := w.slugAliases[key]; ok {
		key = targetID
	}
	v, ok := w.vehicles[key]
	return v, ok
}

func (w *TestInventoryWrapper) IsFresh(now time.Time) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.lastUpdated.IsZero() {
		return false
	}
	return now.Sub(w.lastUpdated) <= w.staleWindow
}

func (w *TestInventoryWrapper) SetUpdated(now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastUpdated = now
}

func (w *TestInventoryWrapper) UpdateVehicleStatus(id string, newStatus string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	key := strings.ToLower(strings.TrimSpace(id))
	if targetID, ok := w.slugAliases[key]; ok {
		key = targetID
	}
	v, ok := w.vehicles[key]
	if !ok {
		return false
	}
	v.Status = newStatus
	w.vehicles[key] = v

	// Also reload into underlying cache
	var updatedList []inventory.Vehicle
	for _, item := range w.vehicles {
		updatedList = append(updatedList, item)
	}
	w.cache.LoadSnapshot(updatedList, fmt.Sprintf("etag-%d", time.Now().UnixNano()), w.lastUpdated)
	return true
}

func (w *TestInventoryWrapper) Cache() *inventory.Cache {
	return w.cache
}

// ============================================================================
// 3. RATE CONTROLLER & JITTER CONTRACT (PROJECT.md § M2)
// ============================================================================

type RateConfig = channel.RateConfig
type RateController = channel.RateController
type MemoryRateController = channel.MemoryRateController

var (
	DefaultRateConfig       = channel.DefaultRateConfig
	NewMemoryRateController = channel.NewMemoryRateController
)

// ============================================================================
// 4. SELECTOR RESILIENCE & AUTO-PAUSE CONTRACT (PROJECT.md § M3)
// ============================================================================

type VersionedSelector = channel.VersionedSelector
type ResilienceTracker = channel.ResilienceTracker
type MemoryResilienceTracker = channel.MemoryResilienceTracker

var (
	NewMemoryResilienceTracker = channel.NewMemoryResilienceTracker
	NewVersionedSelector       = channel.NewVersionedSelector
)

// ============================================================================
// 5. MOCK CHANNEL ADAPTER & MULTI-CHANNEL HOST
// ============================================================================

type MockChannel struct {
	mu           sync.RWMutex
	id           string
	limits       channel.Limits
	repliesSent  []channel.Message
	failSend     bool
	failRead     bool
	sendErrorMsg string
	sendResult   channel.SendStatus
	threads      map[string]channel.Thread
}

func NewMockChannel(id string, charLimit int) *MockChannel {
	return &MockChannel{
		id: id,
		limits: channel.Limits{
			MaxCharacterLength: charLimit,
			MaxPerHour:         120,
			MaxPerDay:          250,
		},
		threads: make(map[string]channel.Thread),
	}
}

func (m *MockChannel) ID() string {
	return m.id
}

func (m *MockChannel) Limits() channel.Limits {
	return m.limits
}

func (m *MockChannel) Open(ctx context.Context, link channel.Link) (channel.Session, error) {
	return channel.Session{ID: "sess-" + m.id, ChannelID: m.id, CreatedAt: time.Now()}, nil
}

func (m *MockChannel) Pending(ctx context.Context, session channel.Session) ([]channel.Conversation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []channel.Conversation
	for _, th := range m.threads {
		list = append(list, th.Conversation)
	}
	return list, nil
}

func (m *MockChannel) Read(ctx context.Context, session channel.Session, conversation channel.Conversation) (channel.Thread, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.failRead {
		return channel.Thread{}, errors.New("mock read error: selector or connection failed")
	}
	th, ok := m.threads[conversation.ID]
	if !ok {
		return channel.Thread{Conversation: conversation}, nil
	}
	return th, nil
}

func (m *MockChannel) Reply(ctx context.Context, session channel.Session, conversation channel.Conversation, message channel.Message, mode channel.Mode) (channel.SendResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.failSend {
		return channel.SendResult{
			Status:    channel.SendFailed,
			MessageID: message.ID,
			Timestamp: time.Now(),
		}, errors.New(m.sendErrorMsg)
	}

	m.repliesSent = append(m.repliesSent, message)

	var st channel.SendStatus
	switch mode {
	case channel.ModeSimulation:
		st = channel.SendSimulated
	case channel.ModeDraft:
		st = channel.SendDrafted
	case channel.ModeAutopilot:
		st = channel.SendVerified
	default:
		st = channel.SendSimulated
	}

	if m.sendResult != "" {
		st = m.sendResult
	}

	return channel.SendResult{
		Status:    st,
		MessageID: message.ID,
		Timestamp: time.Now(),
	}, nil
}

func (m *MockChannel) SeedThread(conv channel.Conversation, msgs []channel.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.threads[conv.ID] = channel.Thread{
		Conversation: conv,
		Messages:     msgs,
	}
}

func (m *MockChannel) SetFailSend(fail bool, msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failSend = fail
	m.sendErrorMsg = msg
}

func (m *MockChannel) SetFailRead(fail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failRead = fail
}

func (m *MockChannel) SetSendResultStatus(status channel.SendStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendResult = status
}

func (m *MockChannel) GetRepliesSent() []channel.Message {
	m.mu.RLock()
	defer m.mu.RUnlock()
	copied := make([]channel.Message, len(m.repliesSent))
	copy(copied, m.repliesSent)
	return copied
}

// ============================================================================
// 6. EVOLUTION ADAPTERS & MOCKS (FEATURES 1–28)
// ============================================================================

// OptInStatus represents consent status (Feature 5)
type OptInStatus string

const (
	OptInGranted OptInStatus = "granted"
	OptInRevoked OptInStatus = "revoked"
	OptInUnknown OptInStatus = "unknown"
)

type OptInManager interface {
	GetStatus(customerID string) OptInStatus
	SetStatus(customerID string, status OptInStatus)
}

type MemoryOptInManager struct {
	mu     sync.RWMutex
	status map[string]OptInStatus
}

func NewMemoryOptInManager() *MemoryOptInManager {
	return &MemoryOptInManager{status: make(map[string]OptInStatus)}
}

func (m *MemoryOptInManager) GetStatus(customerID string) OptInStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if st, ok := m.status[customerID]; ok {
		return st
	}
	return OptInUnknown
}

func (m *MemoryOptInManager) SetStatus(customerID string, status OptInStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status[customerID] = status
}

// HSMTemplate represents a pre-approved template (Feature 3)
type TemplateParam struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type HSMTemplate struct {
	Name       string          `json:"name"`
	Language   string          `json:"language"`
	Components []TemplateParam `json:"components"`
}

// WhatsAppCloudMockAdapter provides simulated WhatsApp Cloud API behavior (Features 1–6)
type WhatsAppCloudMockAdapter struct {
	VerifyToken         string
	AppSecret           string
	OptIn               *MemoryOptInManager
	Templates           map[string]HSMTemplate
	SimulateQuota429    bool
	SimulatePendingWABA bool
	FallbackActive      bool
	FallbackAdapter     *MockChannel
	mu                  sync.RWMutex
	DeliveredMsgs       []channel.Message
}

func NewWhatsAppCloudMockAdapter(verifyToken, appSecret string) *WhatsAppCloudMockAdapter {
	adapter := &WhatsAppCloudMockAdapter{
		VerifyToken:     verifyToken,
		AppSecret:       appSecret,
		OptIn:           NewMemoryOptInManager(),
		Templates:       make(map[string]HSMTemplate),
		FallbackAdapter: NewMockChannel("whatsapp_playwright_fallback", 650),
	}
	// Seed standard HSM templates
	adapter.Templates["lead_reengagement_24h"] = HSMTemplate{
		Name:     "lead_reengagement_24h",
		Language: "es_CO",
		Components: []TemplateParam{
			{Type: "body", Text: "Hola {{1}}, seguimos a tu disposición en BuyaCAR Tunja para asesorarte sobre {{2}}."},
		},
	}
	return adapter
}

func (wa *WhatsAppCloudMockAdapter) VerifyWebhookGET(mode, token, challenge string) (string, int) {
	if mode == "subscribe" && token == wa.VerifyToken {
		return challenge, http.StatusOK
	}
	return "forbidden", http.StatusForbidden
}

func (wa *WhatsAppCloudMockAdapter) ComputeHMACSHA256(payload []byte) string {
	mac := hmac.New(sha256.New, []byte(wa.AppSecret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func (wa *WhatsAppCloudMockAdapter) VerifySignature(payload []byte, signature string) bool {
	expected := wa.ComputeHMACSHA256(payload)
	return hmac.Equal([]byte(expected), []byte(signature))
}

func (wa *WhatsAppCloudMockAdapter) HandleWebhookPOST(signature string, body []byte) (int, error) {
	// Enforce MAX_BODY_BYTES = 256KB (Mandament 4)
	if len(body) > 256*1024 {
		return http.StatusRequestEntityTooLarge, errors.New("payload exceeds MAX_BODY_BYTES (256KB)")
	}
	if !wa.VerifySignature(body, signature) {
		return http.StatusUnauthorized, errors.New("invalid webhook signature")
	}
	return http.StatusOK, nil
}

func (wa *WhatsAppCloudMockAdapter) SendMessage(ctx context.Context, customerID, text string, isMarketing bool) (channel.SendResult, error) {
	wa.mu.Lock()
	defer wa.mu.Unlock()

	// Check Opt-In for marketing/re-engagement (Feature 5)
	if isMarketing {
		status := wa.OptIn.GetStatus(customerID)
		if status == OptInRevoked {
			return channel.SendResult{Status: channel.SendFailed}, errors.New("opt_in_revoked: customer opted out")
		}
	}

	// Failover check (Feature 6): quota 429 or pending WABA
	if wa.SimulateQuota429 || wa.SimulatePendingWABA {
		wa.FallbackActive = true
		msg := channel.Message{ID: "msg-wa-fb-" + strconv.FormatInt(time.Now().UnixNano(), 10), Sender: "bot", Text: text, Timestamp: time.Now()}
		conv := channel.Conversation{ID: "conv-fb-" + customerID, ChannelID: "whatsapp_playwright_fallback"}
		sess := channel.Session{ID: "sess-wa-fb", ChannelID: "whatsapp_playwright_fallback"}
		return wa.FallbackAdapter.Reply(ctx, sess, conv, msg, channel.ModeSimulation)
	}

	wa.DeliveredMsgs = append(wa.DeliveredMsgs, channel.Message{
		ID:        "msg-cloud-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		Sender:    "bot",
		Text:      text,
		Timestamp: time.Now(),
	})

	return channel.SendResult{
		Status:    channel.SendSimulated,
		MessageID: "msg-cloud-ok",
		Timestamp: time.Now(),
	}, nil
}

// MercadoLibreMockAdapter provides simulated MercadoLibre webhook & pre-sale guard (Features 7 & 8)
type MercadoLibreMockAdapter struct {
	AppSecret  string
	PhoneRegex *regexp.Regexp
	URLRegex   *regexp.Regexp
	mu         sync.RWMutex
	ReceivedQs []string
}

func NewMercadoLibreMockAdapter(appSecret string) *MercadoLibreMockAdapter {
	return &MercadoLibreMockAdapter{
		AppSecret:  appSecret,
		PhoneRegex: regexp.MustCompile(`(\+?57\s?)?3\d{2}[-\s]?\d{3}[-\s]?\d{4}`),
		URLRegex:   regexp.MustCompile(`(https?://[^\s]+|www\.[^\s]+|[a-zA-Z0-9-]+\.(com|co|net|org))`),
	}
}

func (ml *MercadoLibreMockAdapter) ComputeSignature(payload []byte) string {
	mac := hmac.New(sha256.New, []byte(ml.AppSecret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func (ml *MercadoLibreMockAdapter) HandleWebhook(signature string, body []byte) (int, error) {
	if len(body) > 256*1024 {
		return http.StatusRequestEntityTooLarge, errors.New("body exceeds 256KB")
	}
	expected := ml.ComputeSignature(body)
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return http.StatusUnauthorized, errors.New("signature mismatch")
	}
	return http.StatusOK, nil
}

// PreSaleGuardrail applies strict policy: strips phone numbers and external links in public pre-sale questions (Feature 8)
func (ml *MercadoLibreMockAdapter) PreSaleGuardrail(text string) (string, bool) {
	sanitized := ml.PhoneRegex.ReplaceAllString(text, "[TELEFONO_REDACTADO]")
	sanitized = ml.URLRegex.ReplaceAllString(sanitized, "[ENLACE_EXTERNO_REDACTADO]")
	modified := sanitized != text
	return sanitized, modified
}

// MetaOmnichannelMockAdapter handles multiplexed FB Messenger (500 chars) & Instagram Direct (450 chars) (Feature 9)
type MetaOmnichannelMockAdapter struct {
	MessengerChannel *MockChannel
	InstagramChannel *MockChannel
}

func NewMetaOmnichannelMockAdapter() *MetaOmnichannelMockAdapter {
	return &MetaOmnichannelMockAdapter{
		MessengerChannel: NewMockChannel("facebook_messenger", 500),
		InstagramChannel: NewMockChannel("instagram_direct", 450),
	}
}

// ResilientStoreMock provides simulated SQLite ACID storage with fallback to local JSON flat log (Features 11 & 12)
type MockResilientStore struct {
	mu           sync.RWMutex
	primaryOK    bool
	fallbackUsed bool
	acidRecords  map[string]store.IdempotencyRecord
	checkpoints  map[string]string
	flatJSONLog  []store.IdempotencyRecord
}

func NewMockResilientStore() *MockResilientStore {
	return &MockResilientStore{
		primaryOK:   true,
		acidRecords: make(map[string]store.IdempotencyRecord),
		checkpoints: make(map[string]string),
	}
}

func (s *MockResilientStore) ForceFallback() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.primaryOK = false
	s.fallbackUsed = true
}

func (s *MockResilientStore) RecoverPrimary(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.primaryOK = true
	// Replay flat JSON log into primary
	for _, rec := range s.flatJSONLog {
		s.acidRecords[rec.Hash] = rec
	}
	s.fallbackUsed = false
	return nil
}

func (s *MockResilientStore) IsUsingFallback() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.fallbackUsed
}

func (s *MockResilientStore) SaveIdempotency(rec store.IdempotencyRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.primaryOK {
		s.fallbackUsed = true
		s.flatJSONLog = append(s.flatJSONLog, rec)
		return nil
	}
	s.acidRecords[rec.Hash] = rec
	return nil
}

func (s *MockResilientStore) GetIdempotency(hash string) (store.IdempotencyRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.primaryOK || s.fallbackUsed {
		for _, r := range s.flatJSONLog {
			if r.Hash == hash {
				return r, true
			}
		}
	}
	rec, ok := s.acidRecords[hash]
	return rec, ok
}

// DMS Sync Worker (Feature 13)
type MockDMSWorker struct {
	mu          sync.RWMutex
	lastETag    string
	syncCount   int
	failNetwork bool
}

func NewMockDMSWorker() *MockDMSWorker {
	return &MockDMSWorker{lastETag: "etag-v1"}
}

func (dms *MockDMSWorker) Sync(ctx context.Context, incomingETag string, wrapper *TestInventoryWrapper, now time.Time) (bool, error) {
	dms.mu.Lock()
	defer dms.mu.Unlock()
	if dms.failNetwork {
		return false, errors.New("dms upstream timeout (504)")
	}
	dms.syncCount++
	if incomingETag == dms.lastETag {
		// 304 Not Modified: cache remains valid
		return false, nil
	}
	dms.lastETag = incomingETag
	wrapper.SetUpdated(now)
	return true, nil
}

// Invalidation Webhook Handler (Feature 14)
type MockInvalidationWebhookHandler struct {
	BearerToken string
	Wrapper     *TestInventoryWrapper
}

func (inv *MockInvalidationWebhookHandler) HandleInvalidation(r *http.Request) (int, error) {
	auth := r.Header.Get("Authorization")
	if auth != "Bearer "+inv.BearerToken {
		return http.StatusUnauthorized, errors.New("invalid bearer token")
	}
	var payload struct {
		VehicleID string `json:"vehicle_id"`
		Status    string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return http.StatusBadRequest, err
	}
	ok := inv.Wrapper.UpdateVehicleStatus(payload.VehicleID, payload.Status)
	if !ok {
		return http.StatusNotFound, errors.New("vehicle not found")
	}
	return http.StatusOK, nil
}

// Waitlist Manager with SHA-256 sealing (Feature 15)
type WaitlistEntry struct {
	VehicleID  string    `json:"vehicle_id"`
	CustomerID string    `json:"customer_id"`
	ChannelID  string    `json:"channel_id"`
	SealedHash string    `json:"sealed_hash"`
	CreatedAt  time.Time `json:"created_at"`
}

type MockWaitlistManager struct {
	mu      sync.RWMutex
	entries map[string][]WaitlistEntry // keyed by vehicle ID
}

func NewMockWaitlistManager() *MockWaitlistManager {
	return &MockWaitlistManager{entries: make(map[string][]WaitlistEntry)}
}

func (w *MockWaitlistManager) Register(vID, cID, chID string, t time.Time) WaitlistEntry {
	w.mu.Lock()
	defer w.mu.Unlock()
	raw := fmt.Sprintf("%s|%s|%s|%d", vID, cID, chID, t.Unix())
	h := sha256.Sum256([]byte(raw))
	entry := WaitlistEntry{
		VehicleID:  vID,
		CustomerID: cID,
		ChannelID:  chID,
		SealedHash: hex.EncodeToString(h[:]),
		CreatedAt:  t,
	}
	w.entries[vID] = append(w.entries[vID], entry)
	return entry
}

func (w *MockWaitlistManager) CheckAndNotify(vID, newStatus string) []WaitlistEntry {
	w.mu.Lock()
	defer w.mu.Unlock()
	if newStatus != "available" {
		return nil
	}
	list := w.entries[vID]
	delete(w.entries, vID)
	return list
}

// Outbound CRM Webhook Dispatcher with HMAC-SHA256 & Exponential Backoff (Features 16 & 17)
type MockOutboundCRMDispatcher struct {
	mu           sync.RWMutex
	Secret       string
	FailUpstream bool
	MaxAttempts  int
	AttemptsMade int
	Dispatched   []CRMTask
	EscQueue     *escalate.Queue
}

func NewMockOutboundCRMDispatcher(secret string, escQ *escalate.Queue) *MockOutboundCRMDispatcher {
	return &MockOutboundCRMDispatcher{
		Secret:      secret,
		MaxAttempts: 3,
		EscQueue:    escQ,
	}
}

func (d *MockOutboundCRMDispatcher) Dispatch(ctx context.Context, task CRMTask) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	body, _ := json.Marshal(task)
	mac := hmac.New(sha256.New, []byte(d.Secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	for attempt := 1; attempt <= d.MaxAttempts; attempt++ {
		d.AttemptsMade++
		if !d.FailUpstream {
			_ = sig
			d.Dispatched = append(d.Dispatched, task)
			return nil
		}
	}

	// Backoff exhausted: Enqueue to escalate.Queue (Feature 17)
	d.EscQueue.Enqueue(task.ChannelID, task.ConvID, "crm_webhook_exhausted", fmt.Sprintf("Failed to export task %s after %d attempts", task.ID, d.MaxAttempts), time.Now())
	return errors.New("crm webhook exhausted max retries")
}

// Semantic Query Cache & Single Flight (Features 18 & 19)
type MockSemanticCache struct {
	mu    sync.RWMutex
	cache map[string]string
}

func NewMockSemanticCache() *MockSemanticCache {
	return &MockSemanticCache{cache: make(map[string]string)}
}

func (sc *MockSemanticCache) NormalizeQuery(q string) string {
	clean := strings.ToLower(strings.TrimSpace(q))
	// Strip accents and punctuation
	clean = strings.ReplaceAll(clean, "¿", "")
	clean = strings.ReplaceAll(clean, "?", "")
	clean = strings.ReplaceAll(clean, "!", "")
	clean = strings.ReplaceAll(clean, "¡", "")
	clean = strings.ReplaceAll(clean, "á", "a")
	clean = strings.ReplaceAll(clean, "é", "e")
	clean = strings.ReplaceAll(clean, "í", "i")
	clean = strings.ReplaceAll(clean, "ó", "o")
	clean = strings.ReplaceAll(clean, "ú", "u")
	clean = strings.ReplaceAll(clean, "ü", "u")
	words := strings.Fields(clean)
	sort.Strings(words)
	return strings.Join(words, " ")
}

func (sc *MockSemanticCache) HashQuery(q string) string {
	norm := sc.NormalizeQuery(q)
	h := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(h[:])
}

func (sc *MockSemanticCache) Get(q string) (string, bool) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	h := sc.HashQuery(q)
	ans, ok := sc.cache[h]
	return ans, ok
}

func (sc *MockSemanticCache) Put(q, ans string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	h := sc.HashQuery(q)
	sc.cache[h] = ans
}

type MockSingleFlight struct {
	mu    sync.Mutex
	calls map[string]int
}

func NewMockSingleFlight() *MockSingleFlight {
	return &MockSingleFlight{calls: make(map[string]int)}
}

func (sf *MockSingleFlight) Execute(key string, fn func() (string, error)) (string, error, bool) {
	sf.mu.Lock()
	sf.calls[key]++
	callNum := sf.calls[key]
	sf.mu.Unlock()

	res, err := fn()
	wasShared := callNum > 1
	return res, err, wasShared
}

// Free Inference Cascade Engine (Features 20, 21, 22)
type MockFreeInferenceCascade struct {
	OllamaAvailable bool
	GroqAvailable   bool
	FastPathOnly    bool
	OllamaCalls     int32
	GroqCalls       int32
	FastPathCalls   int32
}

func NewMockFreeInferenceCascade() *MockFreeInferenceCascade {
	return &MockFreeInferenceCascade{
		OllamaAvailable: true,
		GroqAvailable:   true,
	}
}

func (c *MockFreeInferenceCascade) Resolve(ctx context.Context, query string) (string, int, error) {
	// Level 1: Ollama Local (127.0.0.1)
	if c.OllamaAvailable && !c.FastPathOnly {
		atomic.AddInt32(&c.OllamaCalls, 1)
		return "Respuesta generada por L1 Ollama Local cuantizado", 1, nil
	}

	// Level 2: Cloud Free (Groq / Gemini)
	if c.GroqAvailable && !c.FastPathOnly {
		atomic.AddInt32(&c.GroqCalls, 1)
		return "Respuesta generada por L2 Groq Free Cloud API", 2, nil
	}

	// Level 3: Fast-Path Determinista (Offline exact-match ~2.59 µs/op)
	atomic.AddInt32(&c.FastPathCalls, 1)
	return "BuyaCAR Tunja: Cra. 6 #52-23 frente a la Toyota. Atención lun-vie 8am-6pm.", 3, nil
}

// Embedded Web Console & Prometheus Exporter (Features 23–27)
type MockEmbeddedWebServer struct {
	Server       *httptest.Server
	EscQueue     *escalate.Queue
	Resilience   *MemoryResilienceTracker
	RateCtrl     *MemoryRateController
	Drafts       map[string]string // draftID -> status
	mu           sync.RWMutex
	ApprovedList []string
}

func NewMockEmbeddedWebServer(escQ *escalate.Queue, res *MemoryResilienceTracker, rc *MemoryRateController) *MockEmbeddedWebServer {
	ws := &MockEmbeddedWebServer{
		EscQueue:   escQ,
		Resilience: res,
		RateCtrl:   rc,
		Drafts:     make(map[string]string),
	}

	mux := http.NewServeMux()
	// Feature 23 & 24: Embedded UI
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!DOCTYPE html><html><body><h1>BuyaCAR Supervisor Console</h1></body></html>"))
	})

	// Feature 25: 1-Click DRAFT Approval Endpoint
	mux.HandleFunc("/api/draft/approve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		draftID := r.URL.Query().Get("id")
		ws.mu.Lock()
		ws.Drafts[draftID] = "approved"
		ws.ApprovedList = append(ws.ApprovedList, draftID)
		ws.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"approved","draft_id":"` + draftID + `"}`))
	})

	// Feature 26: Channel Pause / Resume API
	mux.HandleFunc("/api/channel/unpause", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		chID := r.URL.Query().Get("channel")
		ws.Resilience.SetPaused(chID, false) // resets consecutive failures and clears pause
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"channel":"` + chID + `","status":"active"}`))
	})

	// Feature 27: Prometheus /metrics Endpoint
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		metrics := `# HELP buyacar_fastpath_latency_p99 FastPath latency p99 in microseconds
# TYPE buyacar_fastpath_latency_p99 gauge
buyacar_fastpath_latency_p99 2.59
# HELP buyacar_cache_hits_total Total semantic cache hits
# TYPE buyacar_cache_hits_total counter
buyacar_cache_hits_total 42
# HELP buyacar_channel_consecutive_failures Consecutive failures by channel
# TYPE buyacar_channel_consecutive_failures gauge
buyacar_channel_consecutive_failures{channel="whatsapp"} 0
# HELP buyacar_hourly_quota_remaining Messages remaining in hourly quota
# TYPE buyacar_hourly_quota_remaining gauge
buyacar_hourly_quota_remaining{channel="whatsapp"} 115
`
		_, _ = w.Write([]byte(metrics))
	})

	ws.Server = httptest.NewServer(mux)
	return ws
}

func (ws *MockEmbeddedWebServer) Close() {
	if ws.Server != nil {
		ws.Server.Close()
	}
}

// JSON CLI Inspection Mode (Feature 28)
type CLIInspector struct {
	Harness *E2EHarness
}

func (cli *CLIInspector) ExecuteCLI(flag string) (string, error) {
	switch flag {
	case "--metrics":
		data := map[string]interface{}{
			"fast_path_p99_us":    2.59,
			"cache_hits_total":    42,
			"operating_mode":      string(cli.Harness.OperatingMode),
			"pending_escalations": len(cli.Harness.EscQueue.Pending()),
		}
		b, _ := json.Marshal(data)
		return string(b), nil
	case "--inspect":
		channelsHealth := make(map[string]string)
		for chID := range cli.Harness.Channels {
			if cli.Harness.Resilience.IsPaused(chID) {
				channelsHealth[chID] = "paused"
			} else {
				channelsHealth[chID] = "healthy"
			}
		}
		data := map[string]interface{}{
			"status":         "operational",
			"channels":       channelsHealth,
			"escalate_queue": len(cli.Harness.EscQueue.Pending()),
			"active_mode":    string(cli.Harness.OperatingMode),
		}
		b, _ := json.Marshal(data)
		return string(b), nil
	default:
		return "", errors.New("unknown cli flag: " + flag)
	}
}

// ============================================================================
// 7. E2E INTEGRATED TEST HARNESS
// ============================================================================

type E2EHarness struct {
	Store         *store.MemoryStore
	Engine        *core.Engine
	Pipeline      *core.Pipeline
	InvWrapper    *TestInventoryWrapper
	EscQueue      *escalate.Queue
	CRM           *MemoryCRMManager
	RateCtrl      *MemoryRateController
	Resilience    *MemoryResilienceTracker
	Channels      map[string]*MockChannel
	OperatingMode channel.Mode
	BaseTime      time.Time

	// Evolutionary Mock Adapters (Features 1–28)
	WhatsAppCloud  *WhatsAppCloudMockAdapter
	MercadoLibre   *MercadoLibreMockAdapter
	MetaOmni       *MetaOmnichannelMockAdapter
	ResilientStore *MockResilientStore
	DMSWorker      *MockDMSWorker
	Invalidation   *MockInvalidationWebhookHandler
	Waitlist       *MockWaitlistManager
	CRMDispatcher  *MockOutboundCRMDispatcher
	SemanticCache  *MockSemanticCache
	SingleFlight   *MockSingleFlight
	Cascade        *MockFreeInferenceCascade
	WebServer      *MockEmbeddedWebServer
	CLI            *CLIInspector
}

func NewE2EHarness(mode channel.Mode, now time.Time) *E2EHarness {
	st := store.NewMemoryStore()
	cfg := core.DefaultSafeConfig()
	cfg.OperatingMode = mode
	if mode == channel.ModeDraft {
		cfg = core.DefaultDraftConfig()
	}
	eng := core.NewEngine(cfg, st)

	invCache := inventory.NewSeededStubCache(now)
	seedVehicles := inventory.DefaultFixtureVehicles()
	invWrapper := NewTestInventoryWrapper(invCache, seedVehicles, now)

	escQ := escalate.NewQueue()
	crmMgr := NewMemoryCRMManager()
	rateCtrl := NewMemoryRateController(DefaultRateConfig())
	resilience := NewMemoryResilienceTracker(escQ)

	guardInst := guard.NewGuard([]string{"buyacar.com.co"})
	aiJSON := []byte(`{
	  "config_id": "cfg-sim-v1",
	  "trace": {
	    "timestamp_utc": "2026-10-26T14:00:00Z",
	    "request_id": "req-sim-01"
	  },
	  "proposed_reply": "Con gusto le brindamos asesoría personalizada en BuyaCAR Tunja frente a la Toyota.",
	  "intent": "complex_query",
	  "confidence": 0.90,
	  "conversation_summary": "Cliente solicita asesoria comercial.",
	  "crm_update": {
	    "tasks_to_create": ["seguimiento_comercial"]
	  },
	  "governance": {
	    "escalate": false,
	    "reason": null
	  }
	}`)
	aiStub := ai.NewStubProvider("gemini", ai.StubResponse{RawJSON: aiJSON, OutputTokens: 50})
	pipe := &core.Pipeline{
		Engine:   eng,
		InvCache: invCache,
		Fast:     fast.NewEngine(fast.DefaultBusinessTexts(), cfg.ConfidenceThreshold),
		Guard:    guardInst,
		AIOrch:   ai.NewOrchestrator(aiStub, nil, skills.NewApprovedCache()),
		EscQueue: escQ,
	}

	h := &E2EHarness{
		Store:         st,
		Engine:        eng,
		Pipeline:      pipe,
		InvWrapper:    invWrapper,
		EscQueue:      escQ,
		CRM:           crmMgr,
		RateCtrl:      rateCtrl,
		Resilience:    resilience,
		Channels:      make(map[string]*MockChannel),
		OperatingMode: mode,
		BaseTime:      now,

		// Instantiate evolutionary test doubles
		WhatsAppCloud:  NewWhatsAppCloudMockAdapter("test_verify_token_123", "meta_app_secret_abc"),
		MercadoLibre:   NewMercadoLibreMockAdapter("meli_client_secret_xyz"),
		MetaOmni:       NewMetaOmnichannelMockAdapter(),
		ResilientStore: NewMockResilientStore(),
		DMSWorker:      NewMockDMSWorker(),
		Invalidation:   &MockInvalidationWebhookHandler{BearerToken: "dms_secure_token_999", Wrapper: invWrapper},
		Waitlist:       NewMockWaitlistManager(),
		CRMDispatcher:  NewMockOutboundCRMDispatcher("crm_secret_hmac_456", escQ),
		SemanticCache:  NewMockSemanticCache(),
		SingleFlight:   NewMockSingleFlight(),
		Cascade:        NewMockFreeInferenceCascade(),
	}

	// WebServer & CLI
	h.WebServer = NewMockEmbeddedWebServer(escQ, resilience, rateCtrl)
	h.CLI = &CLIInspector{Harness: h}

	// Register standard channels
	h.RegisterMockChannel("whatsapp", 650)
	h.RegisterMockChannel("email_pilot", 1000)
	h.RegisterMockChannel("mercadolibre", 500)
	h.RegisterMockChannel("web", 800)
	h.RegisterMockChannel("facebook_messenger", 500)
	h.RegisterMockChannel("instagram_direct", 450)

	return h
}

func (h *E2EHarness) Close() {
	if h.WebServer != nil {
		h.WebServer.Close()
	}
}

func (h *E2EHarness) RegisterMockChannel(id string, charLimit int) *MockChannel {
	ch := NewMockChannel(id, charLimit)
	h.Channels[id] = ch
	_ = h.Engine.RegisterChannel(ch)
	return ch
}

// ExecuteFlow executes a full conversation thread end-to-end through the harness,
// simulating the complete pipeline with pre-send checks, rate limits, CRM settling, and resilience.
func (h *E2EHarness) ExecuteFlow(ctx context.Context, channelID, convID, userMessage string, now time.Time) (core.PipelineOutcome, error) {
	ch, ok := h.Channels[channelID]
	if !ok {
		return core.PipelineOutcome{}, fmt.Errorf("channel %s not registered", channelID)
	}

	// Check channel pause state (Feature 16)
	if h.Resilience.IsPaused(channelID) {
		return core.PipelineOutcome{
			ConversationID:   convID,
			Skipped:          true,
			SkipReason:       "channel_is_paused",
			Escalated:        true,
			EscalationReason: "channel_paused_skip",
		}, nil
	}

	// Check Rate Limiter (Feature 8 & 9)
	canSend, rateReason := h.RateCtrl.CanSend(channelID, now)
	if !canSend {
		return core.PipelineOutcome{
			ConversationID:   convID,
			Skipped:          true,
			SkipReason:       rateReason,
			Escalated:        true,
			EscalationReason: rateReason,
		}, nil
	}

	conv := channel.Conversation{
		ID:            convID,
		ChannelID:     channelID,
		CustomerAlias: "Customer-" + convID,
		UpdatedAt:     now,
	}

	ch.SeedThread(conv, []channel.Message{
		{ID: "msg-in-" + convID, Sender: "customer", Text: userMessage, Timestamp: now},
	})

	sess, _ := ch.Open(ctx, channel.Link{ID: "link-" + channelID, URL: "https://buyacar.com.co/" + channelID, Active: true})

	// Execute core pipeline
	outcome, err := h.Pipeline.ProcessConversation(ctx, ch, sess, conv, now)
	if err != nil {
		// Track consecutive failure (Feature 15 & 16)
		paused, _ := h.Resilience.RecordFailure(channelID, err.Error())
		if paused {
			outcome.Escalated = true
			outcome.EscalationReason = "channel_auto_paused_consecutive_failures"
		}
		return outcome, err
	}

	// Record success in resilience tracker (Feature 15)
	h.Resilience.RecordSuccess(channelID)

	// Record rate limit consumption (Feature 8 & 9)
	h.RateCtrl.RecordSend(channelID, now)

	// Settle CRM task if high intent detected (Feature 5)
	var taskType CRMTaskType
	desc := ""
	cleanMsg := strings.ToLower(userMessage)
	if strings.Contains(cleanMsg, "prueba de ruta") || strings.Contains(cleanMsg, "test drive") || strings.Contains(cleanMsg, "conocerla") {
		taskType = TaskScheduleTestDrive
		desc = "Agendar prueba de ruta para cliente interesado"
	} else if strings.Contains(cleanMsg, "credito") || strings.Contains(cleanMsg, "financiamiento") || strings.Contains(cleanMsg, "bancos") {
		taskType = TaskCreditApplication
		desc = "Solicitud de estudio de crédito y tasas de interés"
	} else if strings.Contains(cleanMsg, "precio") || strings.Contains(cleanMsg, "cuanto vale") || strings.Contains(cleanMsg, "kilometros") {
		taskType = TaskCommercialFollowup
		desc = "Seguimiento comercial sobre características del vehículo"
	}

	if taskType != "" {
		_ = h.CRM.RecordTask(ctx, CRMTask{
			ConvID:      convID,
			ChannelID:   channelID,
			Type:        taskType,
			Description: desc,
			CreatedAt:   now,
		})
	}

	return outcome, nil
}

// SanitizePromptInjection checks for indirect prompt injection attempts at CP1 (Mandament 2 CP1)
func SanitizePromptInjection(input string) (string, bool) {
	lower := strings.ToLower(input)
	suspicious := []string{
		"ignore previous instructions",
		"ignora las instrucciones",
		"ignora todas las instrucciones",
		"system override",
		"nuevo rol:",
		"you are now",
		"regálame el carro",
		"regalame el carro",
		"precio: $0",
	}
	for _, term := range suspicious {
		if strings.Contains(lower, term) {
			return "[CONTENIDO_MALICIOSO_BLOQUEADO]", true
		}
	}
	return input, false
}

// VerifyOfficialPhones checks CP5 phone anti-hallucination whitelist (Mandament 2 CP5)
var officialPhones = map[string]bool{
	"3151212223": true,
	"3022866969": true,
	"3160502717": true,
}

func ValidateResponsePhones(reply string, allowedCatalogPhones []string) bool {
	phoneRe := regexp.MustCompile(`\b3\d{9}\b`)
	matches := phoneRe.FindAllString(reply, -1)
	for _, m := range matches {
		if officialPhones[m] {
			continue
		}
		foundInCatalog := false
		for _, cat := range allowedCatalogPhones {
			if m == cat {
				foundInCatalog = true
				break
			}
		}
		if !foundInCatalog {
			return false // Hallucinated phone number detected!
		}
	}
	return true
}

// FormatBuffer is a helper for test assertions
func NewTestBuffer(s string) *bytes.Buffer {
	return bytes.NewBufferString(s)
}
