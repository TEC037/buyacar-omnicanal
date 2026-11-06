package triage

import (
	"strconv"
	"strings"

	"buyacar.com.co/omnicanal/internal/inventory"
)

// Intent representa la intencion detectada en el mensaje del cliente.
type Intent string

const (
	// Intenciones faciles (candidatas a Fast-Path si confianza >= 0.80)
	IntentLocation     Intent = "location"
	IntentHours        Intent = "hours"
	IntentContact      Intent = "contact"
	IntentGreeting     Intent = "greeting"
	IntentPrice        Intent = "price"
	IntentAvailability Intent = "availability"
	IntentMileage      Intent = "mileage"
	IntentSpecs        Intent = "specs" // transmision, motor, traccion, color, carroceria
	IntentDocs         Intent = "docs"  // SOAT, tecnomecanica, placa
	IntentVisit        Intent = "visit_or_test_drive"
	IntentCreditGeneral Intent = "credit_general"

	// Intenciones dificiles o de escalamiento obligatorio (paso 10 del flujo)
	IntentDiscount     Intent = "discount_or_min_price"
	IntentTradeIn      Intent = "trade_in_appraisal"
	IntentCreditStudy  Intent = "credit_or_risk_central"
	IntentLegalOrClaim Intent = "legal_fasecolda_or_claim"
	IntentPromptInject Intent = "prompt_injection"
	IntentSensitivePII Intent = "sensitive_pii"
	IntentComplexQuery Intent = "complex_query"
)

// Classification contiene el resultado determinista del triage.
type Classification struct {
	NormalizedText   string
	Intent           Intent
	Confidence       float64
	IsEasy           bool
	MustEscalate     bool
	EscalationReason string
	ExtractedQuery   inventory.Query
}

var normReplacer = strings.NewReplacer(
	"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u",
	"¿", "", "?", "", "¡", "", "!", "", ",", " ", ".", " ",
	"[", " ", "]", " ", "(", " ", ")", " ", ":", " ", ";", " ",
)

// Normalize limpia espacios, convierte a minusculas y elimina tildes basicas para matching sub-milisegundo.
func Normalize(input string) string {
	s := strings.ToLower(strings.TrimSpace(input))
	return strings.Join(strings.Fields(normReplacer.Replace(s)), " ")
}

// Classify ejecuta el triage determinista priorizando seguridad (anti-injection y escalamiento obligatorio).
func Classify(raw string) Classification {
	norm := Normalize(raw)
	q := extractVehicleQuery(norm)

	// 1. Deteccion prioritaria de Prompt Injection
	if containsAny(norm, "olvida todas tus instrucciones", "ignora tus reglas", "system prompt", "ignore previous instructions", "regalan carros") {
		return Classification{
			NormalizedText:   norm,
			Intent:           IntentPromptInject,
			Confidence:       0.99,
			IsEasy:           false,
			MustEscalate:     true,
			EscalationReason: "prompt_injection_detected",
			ExtractedQuery:   q,
		}
	}

	// 2. Deteccion de causales de escalamiento obligatorio (paso 10 del flujo)
	if containsAny(norm, "descuento", "rebaja", "precio minimo", "lo menos", "cuanto es lo minimo", "ultimo precio", "en cuanto me lo deja", "de contado cuanto") {
		return Classification{
			NormalizedText:   norm,
			Intent:           IntentDiscount,
			Confidence:       0.95,
			IsEasy:           false,
			MustEscalate:     true,
			EscalationReason: "discount_or_min_price_request",
			ExtractedQuery:   q,
		}
	}
	if containsAny(norm, "permuta", "retoma", "parte de pago", "reciben mi carro", "reciben camioneta", "reciben moto", "doy mi carro", "encimo", "avaluo") {
		return Classification{
			NormalizedText:   norm,
			Intent:           IntentTradeIn,
			Confidence:       0.95,
			IsEasy:           false,
			MustEscalate:     true,
			EscalationReason: "trade_in_appraisal_required",
			ExtractedQuery:   q,
		}
	}
	if containsAny(norm, "datacredito", "reportado", "estoy reportado", "centrales de riesgo", "estudio de credito", "aprobacion de credito") {
		return Classification{
			NormalizedText:   norm,
			Intent:           IntentCreditStudy,
			Confidence:       0.95,
			IsEasy:           false,
			MustEscalate:     true,
			EscalationReason: "credit_study_or_risk_central",
			ExtractedQuery:   q,
		}
	}
	if containsAny(norm, "siniestro", "fasecolda", "peritaje legal", "queja", "reclamo", "demanda") {
		return Classification{
			NormalizedText:   norm,
			Intent:           IntentLegalOrClaim,
			Confidence:       0.95,
			IsEasy:           false,
			MustEscalate:     true,
			EscalationReason: "legal_claim_or_fasecolda",
			ExtractedQuery:   q,
		}
	}

	// 3. Intenciones faciles sin requerir vehiculo especifico
	if containsAny(norm, "credito", "financiacion", "cuota inicial", "bancos", "financian") && !containsAny(norm, "datacredito", "reportado", "centrales de riesgo") && q.Brand == "" && q.Model == "" {
		return Classification{
			NormalizedText: norm,
			Intent:         IntentCreditGeneral,
			Confidence:     0.95,
			IsEasy:         true,
			ExtractedQuery: q,
		}
	}

	isPolicyOrWorkshopQuery := containsAny(norm, "garantia", "taller", "talleres", "requisitos", "peritaje", "traspaso")
	if !isPolicyOrWorkshopQuery && containsAny(norm, "donde quedan", "ubicados", "ubicacion", "direccion", "como llego", "vitrina") && q.Brand == "" && q.Model == "" {
		return Classification{
			NormalizedText: norm,
			Intent:         IntentLocation,
			Confidence:     0.95,
			IsEasy:         true,
			ExtractedQuery: q,
		}
	}
	if containsAny(norm, "horario", "que hora abren", "sabado", "atencion") && q.Brand == "" && q.Model == "" {
		return Classification{
			NormalizedText: norm,
			Intent:         IntentHours,
			Confidence:     0.95,
			IsEasy:         true,
			ExtractedQuery: q,
		}
	}
	if containsAny(norm, "whatsapp", "telefono", "numero de contacto", "llamar") && q.Brand == "" && q.Model == "" {
		return Classification{
			NormalizedText: norm,
			Intent:         IntentContact,
			Confidence:     0.95,
			IsEasy:         true,
			ExtractedQuery: q,
		}
	}

	// 4. Intenciones faciles sobre inventario
	if q.Brand != "" || q.Model != "" {
		if containsAny(norm, "precio", "cuanto vale", "cuanto cuesta", "valor") {
			return Classification{
				NormalizedText: norm,
				Intent:         IntentPrice,
				Confidence:     0.92,
				IsEasy:         true,
				ExtractedQuery: q,
			}
		}
		if containsAny(norm, "kilometraje", "cuantos kilometros", "km") {
			return Classification{
				NormalizedText: norm,
				Intent:         IntentMileage,
				Confidence:     0.90,
				IsEasy:         true,
				ExtractedQuery: q,
			}
		}
		if containsAny(norm, "soat", "tecnomecanica", "placa") {
			return Classification{
				NormalizedText: norm,
				Intent:         IntentDocs,
				Confidence:     0.90,
				IsEasy:         true,
				ExtractedQuery: q,
			}
		}
		if containsAny(norm, "transmision", "caja", "motor", "traccion", "4x4", "color") {
			return Classification{
				NormalizedText: norm,
				Intent:         IntentSpecs,
				Confidence:     0.90,
				IsEasy:         true,
				ExtractedQuery: q,
			}
		}
		if containsAny(norm, "donde lo puedo ver", "prueba de ruta", "agendar cita", "ver el carro", "ver la camioneta") {
			return Classification{
				NormalizedText: norm,
				Intent:         IntentVisit,
				Confidence:     0.90,
				IsEasy:         true,
				ExtractedQuery: q,
			}
		}
		// Por defecto si menciona un vehiculo: consulta de disponibilidad/ficha
		return Classification{
			NormalizedText: norm,
			Intent:         IntentAvailability,
			Confidence:     0.88,
			IsEasy:         true,
			ExtractedQuery: q,
		}
	}

	if norm == "hola" || norm == "buenos dias" || norm == "buenas tardes" || norm == "buenas noches" ||
		containsAny(norm, "sigue disponible", "esta disponible", "todavia esta disponible", "hola sigue disponible") {
		return Classification{
			NormalizedText: norm,
			Intent:         IntentGreeting,
			Confidence:     0.95,
			IsEasy:         true,
		}
	}

	// Consulta abierta o ambigua -> delegar a IA o escalar (confianza baja para Fast-Path)
	return Classification{
		NormalizedText: norm,
		Intent:         IntentComplexQuery,
		Confidence:     0.55,
		IsEasy:         false,
		ExtractedQuery: q,
	}
}

// ClassifyWithContext clasifica el mensaje actual heredando el vehiculo de mensajes previos del hilo
// (o del titulo del anuncio de Facebook Marketplace) cuando el mensaje actual no menciona marca/modelo.
func ClassifyWithContext(raw string, priorTexts []string) Classification {
	cl := Classify(raw)
	if cl.MustEscalate || cl.ExtractedQuery.Brand != "" || cl.ExtractedQuery.Model != "" || len(priorTexts) == 0 {
		return cl
	}
	if cl.Intent == IntentLocation || cl.Intent == IntentHours || cl.Intent == IntentContact {
		return cl
	}

	for i := len(priorTexts) - 1; i >= 0; i-- {
		pq := extractVehicleQuery(Normalize(priorTexts[i]))
		if pq.Brand != "" || pq.Model != "" {
			// Si el cliente pregunta "¿Sigue disponible?", "¿Qué precio tiene?", "¿Cuántos km tiene?", etc.
			// sobre un anuncio/hilo previo con vehículo identificado, re-evaluamos con el contexto del vehículo.
			combined := priorTexts[i] + " " + raw
			reCl := Classify(combined)
			if !reCl.MustEscalate {
				reCl.NormalizedText = cl.NormalizedText
				reCl.ExtractedQuery = pq
				return reCl
			}
			cl.ExtractedQuery = pq
			return cl
		}
	}
	return cl
}

var (
	knownBrands = map[string]string{
		"toyota":     "Toyota",
		"mazda":      "Mazda",
		"renault":    "Renault",
		"chevrolet":  "Chevrolet",
		"nissan":     "Nissan",
		"volkswagen": "Volkswagen",
		"vw":         "Volkswagen",
		"kia":        "Kia",
		"ford":       "Ford",
		"hyundai":    "Hyundai",
		"honda":      "Honda",
		"mercedes":   "Mercedes-Benz",
		"audi":       "Audi",
		"peugeot":    "Peugeot",
		"ferrari":    "Ferrari",
	}
	knownModels = map[string]string{
		"hilux":      "Hilux",
		"fortuner":   "Fortuner",
		"cx-5":       "CX-5",
		"cx5":        "CX-5",
		"cx-30":      "CX-30",
		"cx30":       "CX-30",
		"mazda3":     "Mazda3",
		"duster":     "Duster",
		"stepway":    "Stepway",
		"sandero":    "Sandero",
		"logan":      "Logan",
		"captur":     "Captur",
		"tracker":    "Tracker",
		"captiva":    "Captiva",
		"spark":      "Spark",
		"sonic":      "Sonic",
		"kicks":      "Kicks",
		"x-trail":    "X-Trail",
		"xtrail":     "X-Trail",
		"qashqai":    "Qashqai",
		"tiguan":     "Tiguan",
		"picanto":    "Picanto",
		"sportage":   "Sportage",
		"escape":     "Escape",
		"edge":       "Edge",
		"focus":      "Focus",
		"fiesta":     "Fiesta",
		"accent":     "Accent",
		"santafe":    "Santafe",
		"cr-v":       "CR-V",
		"crv":        "CR-V",
		"c200":       "C200",
		"b180":       "B180",
		"a3":         "A3",
		"207":        "207",
		"testarossa": "Testarossa",
	}
)

func extractVehicleQuery(norm string) inventory.Query {
	var q inventory.Query

	tokens := strings.Fields(norm)
	for _, tok := range tokens {
		if b, ok := knownBrands[tok]; ok {
			q.Brand = b
		}
		if m, ok := knownModels[tok]; ok {
			q.Model = m
		}
		if len(tok) == 4 {
			if yr, err := strconv.Atoi(tok); err == nil && yr >= 2000 && yr <= 2030 {
				q.Year = yr
			}
		}
	}
	return q
}

func containsAny(s string, patterns ...string) bool {
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}
