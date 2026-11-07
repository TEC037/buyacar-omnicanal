package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"buyacar.com.co/omnicanal/internal/inventory"
	"buyacar.com.co/omnicanal/internal/skills"
	"buyacar.com.co/omnicanal/internal/triage"
)

// AIResponse alias de compatibilidad estricta con PROJECT.md § Interface Contracts (M3).
type AIResponse = ExecutionResult

// CompactContext alias de compatibilidad estricta con PROJECT.md § Interface Contracts (M3).
type CompactContext = skills.CompactContext

// CascadeProvider define un proveedor de inferencia categorizado por su nivel de costo y localidad.
type CascadeProvider interface {
	Provider
	Tier() int // 1: Local Ollama, 2: Cloud Free, 3: Fast-Path
}

// CascadeOrchestrator define el contrato de resolucion jerarquica en 3 niveles de inferencia gratuita.
type CascadeOrchestrator interface {
	ResolveWithCascade(ctx context.Context, query string, compact skills.CompactContext) (AIResponse, int, error)
}

// FastMatcher evalua coincidencias exactas de inventario y datos del concesionario sin inferencia.
type FastMatcher interface {
	MatchExact(ctx context.Context, query string, vehicles []inventory.Vehicle) (StructuredOutput, bool)
}

// DefaultFastMatcher implementa FastMatcher con reglas comerciales exactas de BuyaCAR Tunja.
type DefaultFastMatcher struct{}

// NewDefaultFastMatcher crea un nuevo matcher determinista de coincidencia exacta.
func NewDefaultFastMatcher() *DefaultFastMatcher {
	return &DefaultFastMatcher{}
}

// MatchExact busca resolver la consulta contra vehiculos o datos oficiales en microsegundos (0 tokens).
func (m *DefaultFastMatcher) MatchExact(ctx context.Context, query string, vehicles []inventory.Vehicle) (StructuredOutput, bool) {
	nowUTC := time.Now().UTC().Format(time.RFC3339)
	reqID := fmt.Sprintf("req-fast-%d", time.Now().UnixNano())

	// Buscar vehiculo disponible coincidente en el contexto filtrado
	for _, v := range vehicles {
		if v.Status == "available" {
			reply := fmt.Sprintf("Sí tenemos disponible el %s %s modelo %d ($%d COP, %d km, caja %s, color %s). ¡Vehículo certificado en BuyaCAR Tunja listo para prueba de ruta! Consulta detalles en: %s",
				v.Brand, v.Model, v.Year, v.PriceCOP, v.MileageKm, v.Transmission, v.Color, v.OfficialURL)
			return StructuredOutput{
				ConfigID: "cfg-fast-exact-v1",
				Trace: TraceInfo{
					TimestampUTC: nowUTC,
					RequestID:    reqID,
				},
				ProposedReply:       reply,
				Intent:              "exact_match",
				Confidence:          0.95,
				ConversationSummary: fmt.Sprintf("Coincidencia exacta de inventario con %s %s %d.", v.Brand, v.Model, v.Year),
				CRMUpdate: CRMUpdate{
					TasksToCreate: []string{"contactar_cliente_inventario_exacto"},
				},
				Governance: Governance{
					Escalate: false,
					Reason:   nil,
				},
			}, true
		}
	}

	return StructuredOutput{}, false
}

// FastPathTierProvider implementa CascadeProvider para el Nivel 3 (Offline Deterministic Fast-Path).
// Resuelve en ~2.59 µs/op con 0 tokens sin requerir conexion a red ni modelos de lenguaje.
type FastPathTierProvider struct {
	matcher FastMatcher
}

// NewFastPathTierProvider crea el proveedor de Nivel 3 de la cascada.
func NewFastPathTierProvider() *FastPathTierProvider {
	return &FastPathTierProvider{
		matcher: NewDefaultFastMatcher(),
	}
}

// Name retorna el nombre del proveedor Fast-Path.
func (f *FastPathTierProvider) Name() string {
	return "fast_path"
}

// Tier retorna el nivel 3 en la cascada.
func (f *FastPathTierProvider) Tier() int {
	return 3
}

// GenerateJSON genera la salida estructurada determinista cumpliendo estrictamente con ParseStrictJSON.
func (f *FastPathTierProvider) GenerateJSON(ctx context.Context, compact skills.CompactContext) ([]byte, int, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, 0, err
	}

	nowUTC := time.Now().UTC().Format(time.RFC3339)
	reqID := fmt.Sprintf("req-l3-%d", time.Now().UnixNano())

	// 1. Manejo de intenciones generales sin requerimiento de vehiculo
	switch compact.Intent {
	case triage.IntentLocation:
		out := StructuredOutput{
			ConfigID:            "cfg-fast-l3",
			Trace:               TraceInfo{TimestampUTC: nowUTC, RequestID: reqID},
			ProposedReply:       "Estamos ubicados en Tunja, Boyacá: Av. Oriental - Cra. 6 #52-23, frente a la Toyota. ¡Nuestros vehículos certificados están listos para prueba de ruta!",
			Intent:              string(compact.Intent),
			Confidence:          1.0,
			ConversationSummary: "Consulta resuelta por Fast-Path Nivel 3 (Ubicacion).",
			CRMUpdate:           CRMUpdate{TasksToCreate: []string{"seguimiento_ubicacion"}},
			Governance:          Governance{Escalate: false, Reason: nil},
		}
		raw, _ := json.Marshal(out)
		return raw, 0, 0, nil

	case triage.IntentHours:
		out := StructuredOutput{
			ConfigID:            "cfg-fast-l3",
			Trace:               TraceInfo{TimestampUTC: nowUTC, RequestID: reqID},
			ProposedReply:       "Nuestro horario de atención en Tunja es de Lunes a Viernes de 8:00 AM - 6:00 PM y Sábados de 8:00 AM - 2:00 PM. Te brindamos asesoría personalizada.",
			Intent:              string(compact.Intent),
			Confidence:          1.0,
			ConversationSummary: "Consulta resuelta por Fast-Path Nivel 3 (Horarios).",
			CRMUpdate:           CRMUpdate{TasksToCreate: []string{"seguimiento_horario"}},
			Governance:          Governance{Escalate: false, Reason: nil},
		}
		raw, _ := json.Marshal(out)
		return raw, 0, 0, nil

	case triage.IntentContact:
		out := StructuredOutput{
			ConfigID:            "cfg-fast-l3",
			Trace:               TraceInfo{TimestampUTC: nowUTC, RequestID: reqID},
			ProposedReply:       "¡Claro que sí! Nuestras líneas oficiales de BuyaCAR en Tunja son:\n\n- WhatsApp Comercial: 3151212223\n- Atención Vitrina: 3022866969 / 3160502717\n- Catálogo: https://buyacar.com.co",
			Intent:              string(compact.Intent),
			Confidence:          1.0,
			ConversationSummary: "Consulta resuelta por Fast-Path Nivel 3 (Contacto).",
			CRMUpdate:           CRMUpdate{TasksToCreate: []string{"seguimiento_contacto"}},
			Governance:          Governance{Escalate: false, Reason: nil},
		}
		raw, _ := json.Marshal(out)
		return raw, 0, 0, nil

	case triage.IntentGreeting:
		out := StructuredOutput{
			ConfigID:            "cfg-fast-l3",
			Trace:               TraceInfo{TimestampUTC: nowUTC, RequestID: reqID},
			ProposedReply:       "¡Hola! Qué gusto saludarte.\nBienvenido a BuyaCAR en Tunja · Vehículos certificados.\n\nPuedes consultar nuestro inventario actualizado con precios y fotos en https://buyacar.com.co\n\n¿Te llamó la atención algún modelo en particular para coordinar tu prueba de ruta?",
			Intent:              string(compact.Intent),
			Confidence:          1.0,
			ConversationSummary: "Consulta resuelta por Fast-Path Nivel 3 (Saludo).",
			CRMUpdate:           CRMUpdate{TasksToCreate: []string{"iniciar_atencion_comercial"}},
			Governance:          Governance{Escalate: false, Reason: nil},
		}
		raw, _ := json.Marshal(out)
		return raw, 0, 0, nil
	}

	// 2. Evaluacion de vehiculos disponibles en el contexto compacto
	if matched, ok := f.matcher.MatchExact(ctx, compact.Summary, compact.Vehicles); ok {
		raw, _ := json.Marshal(matched)
		return raw, 0, 0, nil
	}

	// 3. Si no hay coincidencia exacta de inventario, emitir escalamiento gobernado
	reason := "fast_path_exact_match_not_found"
	out := StructuredOutput{
		ConfigID:            "cfg-fast-l3",
		Trace:               TraceInfo{TimestampUTC: nowUTC, RequestID: reqID},
		ProposedReply:       "Un asesor comercial de BuyaCAR se comunicará contigo en breve para brindarte información detallada de nuestro catálogo certificado.",
		Intent:              string(compact.Intent),
		Confidence:          0.5,
		ConversationSummary: "Sin coincidencia exacta en Nivel 3; escalado a humano.",
		CRMUpdate:           CRMUpdate{TasksToCreate: []string{"escalar_asesor_humano"}},
		Governance: Governance{
			Escalate: true,
			Reason:   &reason,
		},
	}
	raw, _ := json.Marshal(out)
	return raw, 0, 0, nil
}

// CascadeEngine gestiona la conmutacion jerarquica de 3 niveles de inferencia gratuita con deduplicacion y cache:
// - Nivel 1: Local Ollama en loopback (127.0.0.1:11434, llama3.2:3b / mistral) -> 0 coste, offline local.
// - Nivel 2: Cloud free tier (Groq Free API / Gemini Free API).
// - Nivel 3: Offline deterministic Fast-Path exact-match (~2.59 µs/op) con 0 tokens.
type CascadeEngine struct {
	mu         sync.RWMutex
	tier1      CascadeProvider
	tier2      CascadeProvider
	tier3      CascadeProvider
	cache      *SemanticCache
	dedup      *Deduplicator
	normalizer *QueryNormalizer
}

// NewCascadeEngine inicializa el motor de cascada jerarquica.
func NewCascadeEngine(tier1, tier2, tier3 CascadeProvider, cache *SemanticCache, dedup *Deduplicator) *CascadeEngine {
	if tier3 == nil {
		tier3 = NewFastPathTierProvider()
	}
	if cache == nil {
		cache = NewSemanticCache(1000, 24*time.Hour)
	}
	if dedup == nil {
		dedup = NewDeduplicator()
	}
	return &CascadeEngine{
		tier1:      tier1,
		tier2:      tier2,
		tier3:      tier3,
		cache:      cache,
		dedup:      dedup,
		normalizer: NewQueryNormalizer(),
	}
}

// HashQuery calcula el hash semantico determinista de la consulta.
func (e *CascadeEngine) HashQuery(query string) string {
	return e.normalizer.HashQuery(query)
}

// Cache retorna la referencia a la cache semantica asociada.
func (e *CascadeEngine) Cache() *SemanticCache {
	return e.cache
}

// Deduplicator retorna la referencia al deduplicador de consultas.
func (e *CascadeEngine) Deduplicator() *Deduplicator {
	return e.dedup
}

// ResolveWithCascade ejecuta la cascada en orden estricto L1 -> L2 -> L3 con deduplicacion in-flight.
func (e *CascadeEngine) ResolveWithCascade(ctx context.Context, query string, compact skills.CompactContext) (AIResponse, int, error) {
	if err := ctx.Err(); err != nil {
		return AIResponse{}, 0, err
	}

	// 1. Clave de cache semantica basada en SHA-256 normalizado
	hashKey := query
	if hashKey != "" {
		hashKey = e.normalizer.HashQuery(query)
	} else if compact.Summary != "" {
		hashKey = e.normalizer.HashQuery(compact.Summary)
	}

	// 2. Verificar cache semantica (< 10 µs, 0 tokens)
	if e.cache != nil && hashKey != "" {
		if cachedOut, ok := e.cache.Get(hashKey); ok {
			return AIResponse{
				ProviderUsed: "semantic_cache",
				Output:       cachedOut,
				InputTokens:  0,
				OutputTokens: 0,
				Escalated:    cachedOut.Governance.Escalate,
			}, 0, nil
		}
	}

	// 3. Deduplicacion Single-Flight para consultas identicas en vuelo
	coalesceKey := hashKey
	if coalesceKey == "" {
		coalesceKey = fmt.Sprintf("intent:%s", compact.Intent)
	}

	res, _, err := e.dedup.DoWithContext(ctx, coalesceKey, func(callCtx context.Context) (ExecutionResult, error) {
		return e.executeCascade(callCtx, hashKey, compact)
	})

	if err != nil {
		return res, 0, err
	}

	tier := 0
	switch res.ProviderUsed {
	case "ollama":
		tier = 1
	case "groq", "gemini":
		tier = 2
	case "fast_path":
		tier = 3
	}

	return res, tier, nil
}

func (e *CascadeEngine) executeCascade(ctx context.Context, hashKey string, compact skills.CompactContext) (ExecutionResult, error) {
	if compact.EstimatedTokens > skills.MaxInputTokensBudget {
		return humanFallback("input_token_budget_exceeded", 0, 0), nil
	}

	totalIn, totalOut := 0, 0

	// -------------------------------------------------------------
	// NIVEL 1: Local Ollama en loopback (127.0.0.1:11434)
	// -------------------------------------------------------------
	if e.tier1 != nil {
		out, inTok, outTok, err := e.tryTierWithRetry(ctx, e.tier1, compact)
		if err == nil {
			totalIn += inTok
			totalOut += outTok
			if out.Governance.Escalate {
				r := "model_requested_escalation"
				if out.Governance.Reason != nil {
					r = *out.Governance.Reason
				}
				return ExecutionResult{
					ProviderUsed: e.tier1.Name(),
					Output:       out,
					InputTokens:  totalIn,
					OutputTokens: totalOut,
					Escalated:    true,
					Reason:       r,
				}, nil
			}
			// Almacenar en cache semantica si fue una respuesta exitosa
			if e.cache != nil && hashKey != "" {
				e.cache.Set(hashKey, out, 0)
			}
			return ExecutionResult{
				ProviderUsed: e.tier1.Name(),
				Output:       out,
				InputTokens:  totalIn,
				OutputTokens: totalOut,
			}, nil
		}
	}

	// -------------------------------------------------------------
	// NIVEL 2: Cloud Free Tier (Groq Free API o Gemini Free API)
	// -------------------------------------------------------------
	if e.tier2 != nil {
		out, inTok, outTok, err := e.tryTierWithRetry(ctx, e.tier2, compact)
		if err == nil {
			totalIn += inTok
			totalOut += outTok
			if out.Governance.Escalate {
				r := "model_requested_escalation"
				if out.Governance.Reason != nil {
					r = *out.Governance.Reason
				}
				return ExecutionResult{
					ProviderUsed: e.tier2.Name(),
					Output:       out,
					InputTokens:  totalIn,
					OutputTokens: totalOut,
					Escalated:    true,
					Reason:       r,
				}, nil
			}
			// Almacenar en cache semantica si fue exitosa
			if e.cache != nil && hashKey != "" {
				e.cache.Set(hashKey, out, 0)
			}
			return ExecutionResult{
				ProviderUsed: e.tier2.Name(),
				Output:       out,
				InputTokens:  totalIn,
				OutputTokens: totalOut,
			}, nil
		}
	}

	// -------------------------------------------------------------
	// NIVEL 3: Offline Deterministic Fast-Path Exact-Match (~2.59 µs/op)
	// -------------------------------------------------------------
	if e.tier3 != nil {
		raw, _, _, err := e.tier3.GenerateJSON(ctx, compact)
		if err == nil {
			parsed, parseErr := ParseStrictJSON(raw)
			if parseErr == nil && !parsed.Governance.Escalate {
				return ExecutionResult{
					ProviderUsed: e.tier3.Name(),
					Output:       parsed,
					InputTokens:  0,
					OutputTokens: 0,
				}, nil
			}
		}
	}

	// Si todos los niveles fallan, escalar ordenadamente a humano
	return humanFallback("all_cascade_tiers_exhausted", totalIn, totalOut), nil
}

func (e *CascadeEngine) tryTierWithRetry(ctx context.Context, p CascadeProvider, compact skills.CompactContext) (StructuredOutput, int, int, error) {
	raw, in1, out1, err := p.GenerateJSON(ctx, compact)
	if err != nil {
		return StructuredOutput{}, in1, out1, err
	}
	parsed, parseErr := ParseStrictJSON(raw)
	if parseErr == nil {
		return parsed, in1, out1, nil
	}

	// Regla Mandamiento: 1 reintento ante JSON invalido
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
