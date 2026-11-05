package channel

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// WhatsAppCompositeChannelID es el identificador del adaptador compuesto de WhatsApp.
	WhatsAppCompositeChannelID = "whatsapp"
	// WhatsAppPlaywrightChannelID es el identificador del canal de contingencia browser Playwright.
	WhatsAppPlaywrightChannelID = "whatsapp_playwright"
	// DefaultQuotaCooldownDuration es el periodo de conmutacion automatica ante HTTP 429.
	DefaultQuotaCooldownDuration = 5 * time.Minute
)

var (
	// ErrBrowserSelectorsExhausted indica que todos los selectores versionados fallaron en el DOM.
	ErrBrowserSelectorsExhausted = errors.New("channel/playwright: all versioned DOM selectors exhausted, human operator intervention required")
	// ErrWABANotVerified indica que la cuenta oficial WABA aun no ha completado el proceso de verificacion de Meta.
	ErrWABANotVerified = errors.New("channel/whatsapp: WABA account verification pending, active traffic routed to Playwright fallback")
)

// PlaywrightBrowserDriver define la interfaz minima de emulacion de navegador Playwright / headless.
type PlaywrightBrowserDriver interface {
	SendChatMessage(ctx context.Context, recipient, text, searchSel, inputSel, sendSel string) error
}

// SimulatedPlaywrightDriver emula el comportamiento determinista del navegador para pruebas y ejecucion estandar.
type SimulatedPlaywrightDriver struct {
	mu           sync.Mutex
	BrokenMap    map[string]bool
	FailSend     bool
	RecordedMsgs []string
}

// NewSimulatedPlaywrightDriver inicializa el driver simulado.
func NewSimulatedPlaywrightDriver() *SimulatedPlaywrightDriver {
	return &SimulatedPlaywrightDriver{
		BrokenMap:    make(map[string]bool),
		RecordedMsgs: make([]string, 0),
	}
}

// SendChatMessage ejecuta el envio emulando la interaccion con el DOM de WhatsApp Web.
func (d *SimulatedPlaywrightDriver) SendChatMessage(ctx context.Context, recipient, text, searchSel, inputSel, sendSel string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.FailSend {
		return errors.New("driver: simulated browser network crash")
	}
	if d.BrokenMap[searchSel] {
		return fmt.Errorf("driver: search selector %s failed to locate element", searchSel)
	}
	if d.BrokenMap[inputSel] {
		return fmt.Errorf("driver: input compose selector %s failed to locate element", inputSel)
	}
	if d.BrokenMap[sendSel] {
		return fmt.Errorf("driver: send button selector %s failed to locate element", sendSel)
	}

	d.RecordedMsgs = append(d.RecordedMsgs, fmt.Sprintf("%s:%s", recipient, text))
	return nil
}

// WhatsAppPlaywrightChannel implementa el adaptador de contingencia por emulacion con selectores versionados.
type WhatsAppPlaywrightChannel struct {
	mu                  sync.RWMutex
	limits              Limits
	searchSelector      VersionedSelector
	inputSelector       VersionedSelector
	sendSelector        VersionedSelector
	driver              PlaywrightBrowserDriver
	tracker             ResilienceTracker
	rateCtrl            RateController
	conversations       []Conversation
	threads             map[string]Thread
	draftOutbox         []WhatsAppDraftEntry
	paused              bool
	consecutiveFailures int
}

// NewWhatsAppPlaywrightChannel construye el canal de contingencia con selectores versionados oficiales.
func NewWhatsAppPlaywrightChannel(driver PlaywrightBrowserDriver, rt ResilienceTracker, rc RateController) *WhatsAppPlaywrightChannel {
	if driver == nil {
		driver = NewSimulatedPlaywrightDriver()
	}
	if rt == nil {
		rt = NewResilienceTracker(nil)
	}
	if rc == nil {
		rc = NewRateController(DefaultRateConfig())
	}

	return &WhatsAppPlaywrightChannel{
		limits: Limits{
			MaxCharacterLength: WhatsAppMaxCharacters,
			MaxPerHour:         120,
			MaxPerDay:          250,
		},
		searchSelector: NewVersionedSelector(
			"wa-search",
			"2026.1",
			"div[contenteditable='true'][data-tab='3']",
			"[data-testid='chat-list-search']",
			"#side [role='textbox']",
			"div.selectable-text[data-tab='3']",
		),
		inputSelector: NewVersionedSelector(
			"wa-input",
			"2026.1",
			"footer div[contenteditable='true'][data-tab='10']",
			"div[data-testid='conversation-compose-box-input']",
			"footer [role='textbox']",
			"div.copyable-text.selectable-text[data-tab='10']",
		),
		sendSelector: NewVersionedSelector(
			"wa-send",
			"2026.1",
			"button[data-tab='11']",
			"span[data-testid='send']",
			"footer button span[data-icon='send']",
			"footer button[aria-label='Enviar']",
		),
		driver:        driver,
		tracker:       rt,
		rateCtrl:      rc,
		threads:       make(map[string]Thread),
		draftOutbox:   make([]WhatsAppDraftEntry, 0),
		conversations: make([]Conversation, 0),
	}
}

func (p *WhatsAppPlaywrightChannel) ID() string {
	return WhatsAppPlaywrightChannelID
}

func (p *WhatsAppPlaywrightChannel) Limits() Limits {
	return p.limits
}

// Selectors retorna copias de los tres selectores versionados.
func (p *WhatsAppPlaywrightChannel) Selectors() (search, input, send VersionedSelector) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.searchSelector.Clone(), p.inputSelector.Clone(), p.sendSelector.Clone()
}

// IsPaused verifica si el canal de contingencia esta pausado por seguridad tras 3 fallos consecutivos.
func (p *WhatsAppPlaywrightChannel) IsPaused() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.paused {
		return true
	}
	if p.tracker != nil && p.tracker.IsPaused(WhatsAppPlaywrightChannelID) {
		return true
	}
	return false
}

// SetPaused fija el estado de pausa local y notifica al ResilienceTracker.
func (p *WhatsAppPlaywrightChannel) SetPaused(paused bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.paused = paused
	if !paused {
		p.consecutiveFailures = 0
		p.searchSelector.Reset()
		p.inputSelector.Reset()
		p.sendSelector.Reset()
	}
	if p.tracker != nil {
		p.tracker.SetPaused(WhatsAppPlaywrightChannelID, paused)
	}
}

func (p *WhatsAppPlaywrightChannel) Open(ctx context.Context, link Link) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	if p.IsPaused() {
		return Session{}, ErrChannelPaused
	}
	return Session{
		ID:        "sess-pw-" + link.ID,
		ChannelID: WhatsAppPlaywrightChannelID,
		CreatedAt: time.Now().UTC(),
	}, nil
}

func (p *WhatsAppPlaywrightChannel) Pending(ctx context.Context, session Session) ([]Conversation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.IsPaused() {
		return nil, ErrChannelPaused
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Conversation, len(p.conversations))
	copy(out, p.conversations)
	return out, nil
}

func (p *WhatsAppPlaywrightChannel) Read(ctx context.Context, session Session, conversation Conversation) (Thread, error) {
	if err := ctx.Err(); err != nil {
		return Thread{}, err
	}
	if p.IsPaused() {
		return Thread{}, ErrChannelPaused
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	th, ok := p.threads[conversation.ID]
	if !ok {
		return Thread{}, fmt.Errorf("channel %s: conversation %s not found", WhatsAppPlaywrightChannelID, conversation.ID)
	}
	msgs := make([]Message, len(th.Messages))
	copy(msgs, th.Messages)
	return Thread{
		Conversation: th.Conversation,
		Messages:     msgs,
	}, nil
}

// Reply envia un mensaje emulando la interaccion con WhatsApp Web o registrando borrador segun el modo.
func (p *WhatsAppPlaywrightChannel) Reply(ctx context.Context, session Session, conversation Conversation, message Message, mode Mode) (SendResult, error) {
	if err := ctx.Err(); err != nil {
		return SendResult{Status: SendFailed}, err
	}
	if p.IsPaused() {
		return SendResult{Status: SendFailed}, ErrChannelPaused
	}

	if len(message.Text) > p.limits.MaxCharacterLength {
		return SendResult{Status: SendFailed}, ErrCharacterLimitExceeded
	}

	now := time.Now().UTC()
	if mode != ModeDraft && p.rateCtrl != nil {
		allowed, _ := p.rateCtrl.CanSend(WhatsAppPlaywrightChannelID, now)
		if !allowed {
			return SendResult{Status: SendFailed}, ErrRateLimitExceeded
		}
	}

	switch mode {
	case ModeSimulation:
		if p.rateCtrl != nil {
			p.rateCtrl.RecordSend(WhatsAppPlaywrightChannelID, now)
		}
		if p.tracker != nil {
			p.tracker.RecordSuccess(WhatsAppPlaywrightChannelID)
		}
		return SendResult{
			Status:    SendSimulated,
			MessageID: message.ID,
			Timestamp: now,
		}, nil

	case ModeDraft:
		p.mu.Lock()
		draft := WhatsAppDraftEntry{
			DraftID:        fmt.Sprintf("draft-pw-%s-%d", conversation.ID, len(p.draftOutbox)+1),
			ConversationID: conversation.ID,
			CustomerAlias:  conversation.CustomerAlias,
			Text:           message.Text,
			CreatedAt:      now,
		}
		p.draftOutbox = append(p.draftOutbox, draft)
		p.mu.Unlock()

		return SendResult{
			Status:    SendDrafted,
			MessageID: draft.DraftID,
			Timestamp: now,
		}, nil

	case ModeAutopilot:
		// Emulacion de despacho con navegacion y failover de selectores versionados
		err := p.dispatchWithVersionedSelectors(ctx, conversation.ID, message.Text)
		if err != nil {
			p.mu.Lock()
			p.consecutiveFailures++
			p.mu.Unlock()
			if p.tracker != nil {
				p.tracker.RecordFailure(WhatsAppPlaywrightChannelID, err.Error())
			}
			return SendResult{Status: SendFailed}, err
		}

		p.mu.Lock()
		p.consecutiveFailures = 0
		p.mu.Unlock()
		if p.rateCtrl != nil {
			p.rateCtrl.RecordSend(WhatsAppPlaywrightChannelID, now)
		}
		if p.tracker != nil {
			p.tracker.RecordSuccess(WhatsAppPlaywrightChannelID)
		}

		return SendResult{
			Status:    SendVerified,
			MessageID: fmt.Sprintf("pw-%d", now.UnixNano()),
			Timestamp: now,
		}, nil

	default:
		return SendResult{Status: SendFailed}, fmt.Errorf("channel %s: unknown mode %s", WhatsAppPlaywrightChannelID, mode)
	}
}

// dispatchWithVersionedSelectors intenta despachar rotando selectores alternativos si alguno falla.
func (p *WhatsAppPlaywrightChannel) dispatchWithVersionedSelectors(ctx context.Context, recipient, text string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	maxAttempts := len(p.searchSelector.Fallbacks) + len(p.inputSelector.Fallbacks) + len(p.sendSelector.Fallbacks) + 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		sSel := p.searchSelector.GetActiveSelector()
		iSel := p.inputSelector.GetActiveSelector()
		bSel := p.sendSelector.GetActiveSelector()

		err := p.driver.SendChatMessage(ctx, recipient, text, sSel, iSel, bSel)
		if err == nil {
			return nil
		}

		errStr := err.Error()
		if strings.Contains(errStr, "search selector") {
			if _, ok := p.searchSelector.FailoverToNext(); !ok {
				return ErrBrowserSelectorsExhausted
			}
			continue
		}
		if strings.Contains(errStr, "input compose selector") {
			if _, ok := p.inputSelector.FailoverToNext(); !ok {
				return ErrBrowserSelectorsExhausted
			}
			continue
		}
		if strings.Contains(errStr, "send button selector") {
			if _, ok := p.sendSelector.FailoverToNext(); !ok {
				return ErrBrowserSelectorsExhausted
			}
			continue
		}

		// Fallo de red o browser no atribuible a selectores: rotamos selectores o reportamos error
		return err
	}

	return ErrBrowserSelectorsExhausted
}

// IngestConversation permite registrar hilos en el canal Playwright.
func (p *WhatsAppPlaywrightChannel) IngestConversation(conv Conversation, messages []Message) {
	p.mu.Lock()
	defer p.mu.Unlock()
	conv.ChannelID = WhatsAppPlaywrightChannelID
	p.conversations = append(p.conversations, conv)
	copied := make([]Message, len(messages))
	copy(copied, messages)
	p.threads[conv.ID] = Thread{
		Conversation: conv,
		Messages:     copied,
	}
}

// WhatsAppCompositeChannel implementa la combinacion resiliente de Cloud API con Playwright Fallback.
type WhatsAppCompositeChannel struct {
	mu                 sync.RWMutex
	cloud              *WhatsAppCloudChannel
	playwright         *WhatsAppPlaywrightChannel
	wabaVerified       bool
	quotaCooldownUntil time.Time
	forceFallback      bool
	fallbackReason     string
	tracker            ResilienceTracker
	rateCtrl           RateController
}

// NewWhatsAppCompositeChannel construye el canal compuesto conmutando dinamicamente ante 429 o WABA pendiente.
func NewWhatsAppCompositeChannel(
	cloud *WhatsAppCloudChannel,
	playwright *WhatsAppPlaywrightChannel,
	wabaVerified bool,
	rt ResilienceTracker,
	rc RateController,
) *WhatsAppCompositeChannel {
	if rt == nil {
		rt = NewResilienceTracker(nil)
	}
	if rc == nil {
		rc = NewRateController(DefaultRateConfig())
	}
	return &WhatsAppCompositeChannel{
		cloud:        cloud,
		playwright:   playwright,
		wabaVerified: wabaVerified,
		tracker:      rt,
		rateCtrl:     rc,
	}
}

func (c *WhatsAppCompositeChannel) ID() string {
	return WhatsAppCompositeChannelID
}

func (c *WhatsAppCompositeChannel) Limits() Limits {
	return Limits{
		MaxCharacterLength: WhatsAppMaxCharacters,
		MaxPerHour:         120,
		MaxPerDay:          250,
	}
}

// CloudChannel expone el adaptador oficial primario.
func (c *WhatsAppCompositeChannel) CloudChannel() *WhatsAppCloudChannel {
	return c.cloud
}

// PlaywrightChannel expone el adaptador de emulacion de contingencia.
func (c *WhatsAppCompositeChannel) PlaywrightChannel() *WhatsAppPlaywrightChannel {
	return c.playwright
}

// SetWABAVerified actualiza el estado de verificacion de la cuenta WABA.
func (c *WhatsAppCompositeChannel) SetWABAVerified(verified bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wabaVerified = verified
}

// IsWABAVerified indica si WABA esta aprobada por Meta.
func (c *WhatsAppCompositeChannel) IsWABAVerified() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.wabaVerified
}

// TriggerQuotaCooldown activa la conmutacion forzosa hacia Playwright durante un periodo de enfriamiento.
func (c *WhatsAppCompositeChannel) TriggerQuotaCooldown(duration time.Duration) {
	if duration <= 0 {
		duration = DefaultQuotaCooldownDuration
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.quotaCooldownUntil = time.Now().UTC().Add(duration)
	c.fallbackReason = "http_429_quota_saturation"
}

// ForceFallback activa o desactiva la contingencia manualmente.
func (c *WhatsAppCompositeChannel) ForceFallback(force bool, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.forceFallback = force
	c.fallbackReason = reason
}

// IsFallbackActive determina si el trafico actual debe ser atendido por Playwright en lugar de Cloud API.
func (c *WhatsAppCompositeChannel) IsFallbackActive() (bool, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.forceFallback {
		return true, c.fallbackReason
	}
	if !c.wabaVerified {
		return true, "waba_verification_pending"
	}
	if time.Now().UTC().Before(c.quotaCooldownUntil) {
		return true, "http_429_quota_saturation"
	}
	if c.cloud != nil && c.cloud.IsPaused() {
		return true, "cloud_channel_paused"
	}
	return false, ""
}

func (c *WhatsAppCompositeChannel) Open(ctx context.Context, link Link) (Session, error) {
	useFallback, _ := c.IsFallbackActive()
	if useFallback && c.playwright != nil {
		return c.playwright.Open(ctx, link)
	}
	if c.cloud != nil {
		return c.cloud.Open(ctx, link)
	}
	return Session{}, ErrChannelPaused
}

func (c *WhatsAppCompositeChannel) Pending(ctx context.Context, session Session) ([]Conversation, error) {
	useFallback, _ := c.IsFallbackActive()
	if useFallback && c.playwright != nil {
		return c.playwright.Pending(ctx, session)
	}
	if c.cloud != nil {
		return c.cloud.Pending(ctx, session)
	}
	return nil, ErrChannelPaused
}

func (c *WhatsAppCompositeChannel) Read(ctx context.Context, session Session, conversation Conversation) (Thread, error) {
	useFallback, _ := c.IsFallbackActive()
	if useFallback && c.playwright != nil {
		return c.playwright.Read(ctx, session, conversation)
	}
	if c.cloud != nil {
		return c.cloud.Read(ctx, session, conversation)
	}
	return Thread{}, ErrChannelPaused
}

// Reply intenta el envio por Cloud API, ejecutando conmutacion automatica inmediata hacia Playwright ante 429.
func (c *WhatsAppCompositeChannel) Reply(ctx context.Context, session Session, conversation Conversation, message Message, mode Mode) (SendResult, error) {
	useFallback, reason := c.IsFallbackActive()
	if useFallback {
		if c.playwright == nil {
			return SendResult{Status: SendFailed}, fmt.Errorf("channel/whatsapp: fallback active (%s) but playwright adapter is nil", reason)
		}
		return c.playwright.Reply(ctx, session, conversation, message, mode)
	}

	// Intento primario a traves de Cloud API
	res, err := c.cloud.Reply(ctx, session, conversation, message, mode)
	if err == nil {
		return res, nil
	}

	// Conmutacion ante HTTP 429 o saturacion
	if errors.Is(err, ErrQuotaExceeded) {
		c.TriggerQuotaCooldown(DefaultQuotaCooldownDuration)
		if c.playwright != nil {
			return c.playwright.Reply(ctx, session, conversation, message, mode)
		}
	}

	return res, err
}

// HandleWebhook canaliza las notificaciones entrantes de Meta hacia el adaptador Cloud.
func (c *WhatsAppCompositeChannel) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if c.cloud != nil {
		c.cloud.HandleWebhook(w, r)
		return
	}
	http.Error(w, "cloud receiver unavailable", http.StatusServiceUnavailable)
}

// VerifySignature delega la validacion HMAC al adaptador Cloud.
func (c *WhatsAppCompositeChannel) VerifySignature(payload []byte, signature string) bool {
	if c.cloud != nil {
		return c.cloud.VerifySignature(payload, signature)
	}
	return false
}
