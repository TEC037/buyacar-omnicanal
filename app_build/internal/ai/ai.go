package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"buyacar.com.co/omnicanal/internal/skills"
)

var (
	ErrQuotaExceeded   = errors.New("ai: provider quota exceeded")
	ErrProviderTimeout = errors.New("ai: provider timeout")
	ErrInvalidSchema   = errors.New("ai: response violated strict JSON schema")
	ErrCircuitOpen     = errors.New("ai: circuit breaker is open")
)

// TraceInfo cumple el campo trace del CONTRATO_IA_OPERATIVA.
type TraceInfo struct {
	TimestampUTC string `json:"timestamp_utc"`
	RequestID    string `json:"request_id"`
}

// CRMUpdate cumple el campo crm_update del CONTRATO_IA_OPERATIVA.
type CRMUpdate struct {
	TasksToCreate []string `json:"tasks_to_create"`
}

// Governance cumple el campo governance del CONTRATO_IA_OPERATIVA.
type Governance struct {
	Escalate bool    `json:"escalate"`
	Reason   *string `json:"reason"`
}

// StructuredOutput representa el esquema logico minimo obligatorio.
type StructuredOutput struct {
	ConfigID            string     `json:"config_id"`
	Trace               TraceInfo  `json:"trace"`
	ProposedReply       string     `json:"proposed_reply"`
	Intent              string     `json:"intent"`
	Confidence          float64    `json:"confidence"`
	ConversationSummary string     `json:"conversation_summary"`
	CRMUpdate           CRMUpdate  `json:"crm_update"`
	Governance          Governance `json:"governance"`
}

// ParseStrictJSON valida que el JSON cumpla exactamente el schema y rechaza claves desconocidas.
func ParseStrictJSON(raw []byte) (StructuredOutput, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	var out StructuredOutput
	if err := dec.Decode(&out); err != nil {
		return StructuredOutput{}, fmt.Errorf("%w: %v", ErrInvalidSchema, err)
	}
	if out.ConfigID == "" || out.Trace.RequestID == "" || out.Trace.TimestampUTC == "" || out.Intent == "" {
		return StructuredOutput{}, fmt.Errorf("%w: missing required fields", ErrInvalidSchema)
	}
	if _, err := time.Parse(time.RFC3339, out.Trace.TimestampUTC); err != nil {
		return StructuredOutput{}, fmt.Errorf("%w: invalid RFC3339 timestamp", ErrInvalidSchema)
	}
	if out.Confidence < 0.0 || out.Confidence > 1.0 {
		return StructuredOutput{}, fmt.Errorf("%w: confidence out of [0,1] range", ErrInvalidSchema)
	}
	if !out.Governance.Escalate && strings.TrimSpace(out.ProposedReply) == "" {
		return StructuredOutput{}, fmt.Errorf("%w: empty proposed_reply when escalate is false", ErrInvalidSchema)
	}
	return out, nil
}

// Provider define la interfaz para Gemini y Ollama.
type Provider interface {
	Name() string
	GenerateJSON(ctx context.Context, prompt skills.CompactContext) ([]byte, int, int, error)
}

// ExecutionResult resume la ejecucion de la cadena Gemini -> Ollama -> Humano.
type ExecutionResult struct {
	ProviderUsed string // "cache", "gemini", "ollama", "human_escalation"
	Output       StructuredOutput
	InputTokens  int
	OutputTokens int
	Escalated    bool
	Reason       string
}

// Orchestrator coordina Cache/SemanticCache -> Cascada (L1 Ollama -> L2 Groq/Gemini -> L3 Fast-Path) -> Humano.
type Orchestrator struct {
	mu              sync.Mutex
	primary         Provider
	fallback        Provider
	cache           *skills.ApprovedCache
	semanticCache   *SemanticCache
	cascade         *CascadeEngine
	dedup           *Deduplicator
	hasher          *QueryNormalizer
	primaryFailures int
	circuitMaxFails int
}

func NewOrchestrator(primary, fallback Provider, cache *skills.ApprovedCache) *Orchestrator {
	return &Orchestrator{
		primary:         primary,
		fallback:        fallback,
		cache:           cache,
		circuitMaxFails: 3,
		hasher:          NewQueryNormalizer(),
	}
}

// NewCascadeOrchestrator crea un orquestador configurado con motor de cascada y cache semantica.
func NewCascadeOrchestrator(cascade *CascadeEngine, cache *SemanticCache) *Orchestrator {
	return &Orchestrator{
		cascade:         cascade,
		semanticCache:   cache,
		circuitMaxFails: 3,
		hasher:          NewQueryNormalizer(),
	}
}

// SetSemanticCache configura la cache semantica de respuestas validadas (LRU/TTL).
func (o *Orchestrator) SetSemanticCache(sc *SemanticCache) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.semanticCache = sc
}

// SetCascade configura el motor de cascada jerarquica (L1 Ollama -> L2 Groq/Gemini -> L3 Fast-Path).
func (o *Orchestrator) SetCascade(c *CascadeEngine) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.cascade = c
}

// SetDeduplicator configura el agrupador Single-Flight de consultas concurrentes.
func (o *Orchestrator) SetDeduplicator(d *Deduplicator) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.dedup = d
}

// HashQuery implementa SemanticHasher computando el hash SHA-256 normalizado de la consulta.
func (o *Orchestrator) HashQuery(query string) string {
	if o == nil {
		return ComputeSemanticHash(query)
	}
	if o.hasher == nil {
		o.hasher = NewQueryNormalizer()
	}
	return o.hasher.HashQuery(query)
}

// ResolveWithCascade implementa CascadeOrchestrator resolviendo la consulta mediante la cascada de 3 niveles.
func (o *Orchestrator) ResolveWithCascade(ctx context.Context, query string, compact skills.CompactContext) (AIResponse, int, error) {
	if o.cascade != nil {
		return o.cascade.ResolveWithCascade(ctx, query, compact)
	}
	// Si no hay motor de cascada dedicado, resolver por via normal
	hash := o.HashQuery(query)
	res := o.Resolve(ctx, hash, compact)
	tier := 0
	switch res.ProviderUsed {
	case "ollama":
		tier = 1
	case "gemini", "groq":
		tier = 2
	case "fast_path":
		tier = 3
	}
	return res, tier, nil
}

// Resolve ejecuta la consulta aplicando caché, presupuesto de tokens (1500/300) y cadena de fallback.
func (o *Orchestrator) Resolve(ctx context.Context, cacheKey string, compact skills.CompactContext) ExecutionResult {
	// 1. Verificar cache semantica (LRU/TTL) si esta activa
	if o.semanticCache != nil && cacheKey != "" {
		if cachedOut, ok := o.semanticCache.Get(cacheKey); ok {
			return ExecutionResult{
				ProviderUsed: "semantic_cache",
				Output:       cachedOut,
				InputTokens:  0,
				OutputTokens: 0,
				Escalated:    cachedOut.Governance.Escalate,
			}
		}
	}

	// 2. Verificar cache de respuestas frecuentes aprobadas (ApprovedCache tradicional)
	if o.cache != nil && cacheKey != "" {
		if cachedReply, ok := o.cache.Get(cacheKey); ok {
			return ExecutionResult{
				ProviderUsed: "cache",
				Output: StructuredOutput{
					ConfigID:            "cfg-sim-v1",
					Trace:               TraceInfo{TimestampUTC: time.Now().UTC().Format(time.RFC3339), RequestID: "req-cache"},
					ProposedReply:       cachedReply,
					Intent:              string(compact.Intent),
					Confidence:          1.0,
					ConversationSummary: compact.Summary,
				},
			}
		}
	}

	// 3. Si hay motor de cascada configurado, delegar la ejecucion jerarquica
	if o.cascade != nil {
		res, _, _ := o.cascade.ResolveWithCascade(ctx, cacheKey, compact)
		return res
	}

	// 2. Verificar presupuesto de entrada (maximo 1.500 tokens)
	if compact.EstimatedTokens > skills.MaxInputTokensBudget {
		reason := "input_token_budget_exceeded"
		return humanFallback(reason, 0, 0)
	}

	totalIn, totalOut := 0, 0

	// 3. Intentar con proveedor primario (Gemini) si el circuit breaker esta cerrado
	if o.primary != nil && !o.isCircuitOpen() {
		out, inTok, outTok, err := o.tryProviderWithSingleJSONRetry(ctx, o.primary, compact)
		totalIn += inTok
		totalOut += outTok
		if err == nil {
			o.resetCircuit()
			if outTok > skills.MaxOutputTokensBudget {
				return humanFallback("output_token_budget_exceeded", totalIn, totalOut)
			}
			if out.Governance.Escalate {
				r := "model_requested_escalation"
				if out.Governance.Reason != nil {
					r = *out.Governance.Reason
				}
				return ExecutionResult{ProviderUsed: o.primary.Name(), Output: out, InputTokens: totalIn, OutputTokens: totalOut, Escalated: true, Reason: r}
			}
			return ExecutionResult{ProviderUsed: o.primary.Name(), Output: out, InputTokens: totalIn, OutputTokens: totalOut}
		}
		o.recordPrimaryFailure()
	}

	// 4. Fallback a Ollama (llama3.2:3b)
	if o.fallback != nil {
		out, inTok, outTok, err := o.tryProviderWithSingleJSONRetry(ctx, o.fallback, compact)
		totalIn += inTok
		totalOut += outTok
		if err == nil {
			if outTok > skills.MaxOutputTokensBudget {
				return humanFallback("output_token_budget_exceeded", totalIn, totalOut)
			}
			if out.Governance.Escalate {
				r := "model_requested_escalation"
				if out.Governance.Reason != nil {
					r = *out.Governance.Reason
				}
				return ExecutionResult{ProviderUsed: o.fallback.Name(), Output: out, InputTokens: totalIn, OutputTokens: totalOut, Escalated: true, Reason: r}
			}
			return ExecutionResult{ProviderUsed: o.fallback.Name(), Output: out, InputTokens: totalIn, OutputTokens: totalOut}
		}
	}

	// 5. Si ambos fallan -> escalar a humano
	return humanFallback("both_ai_providers_failed", totalIn, totalOut)
}

func (o *Orchestrator) tryProviderWithSingleJSONRetry(ctx context.Context, p Provider, compact skills.CompactContext) (StructuredOutput, int, int, error) {
	raw, in1, out1, err := p.GenerateJSON(ctx, compact)
	if err != nil {
		return StructuredOutput{}, in1, out1, err
	}
	parsed, parseErr := ParseStrictJSON(raw)
	if parseErr == nil {
		return parsed, in1, out1, nil
	}

	// Regla del CONTRATO_IA_OPERATIVA: Si el JSON es invalido, se permite un unico reintento; despues se escala.
	raw2, in2, out2, err2 := p.GenerateJSON(ctx, compact)
	if err2 != nil {
		return StructuredOutput{}, in1 + in2, out1 + out2, err2
	}
	parsed2, parseErr2 := ParseStrictJSON(raw2)
	if parseErr2 != nil {
		return StructuredOutput{}, in1 + in2, out1 + out2, parseErr2
	}
	return parsed2, in1 + in2, out1 + out2, nil
}

func (o *Orchestrator) isCircuitOpen() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.primaryFailures >= o.circuitMaxFails
}

func (o *Orchestrator) recordPrimaryFailure() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.primaryFailures++
}

func (o *Orchestrator) resetCircuit() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.primaryFailures = 0
}

func humanFallback(reason string, inTok, outTok int) ExecutionResult {
	return ExecutionResult{
		ProviderUsed: "human_escalation",
		InputTokens:  inTok,
		OutputTokens: outTok,
		Escalated:    true,
		Reason:       reason,
	}
}
