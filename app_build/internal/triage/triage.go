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
	IntentLocation      Intent = "location"
	IntentHours         Intent = "hours"
	IntentContact       Intent = "contact"
	IntentGreeting      Intent = "greeting"
	IntentPrice         Intent = "price"
	IntentAvailability  Intent = "availability"
	IntentMileage       Intent = "mileage"
	IntentSpecs         Intent = "specs" // transmision, motor, traccion, color, carroceria
	IntentDocs          Intent = "docs"  // SOAT, tecnomecanica, placa
	IntentVisit         Intent = "visit_or_test_drive"
	IntentCreditGeneral Intent = "credit_general"
	IntentCategoryQuery Intent = "category_suv_or_family"

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
	"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
	"¿", " ", "?", " ", "¡", " ", "!", " ", ",", " ", ".", " ",
	"[", " ", "]", " ", "(", " ", ")", " ", ":", " ", ";", " ",
	"{", " ", "}", " ", "\"", " ", "'", " ", "*", " ", "/", " ",
	"\\", " ", "_", " ", "+", " ", "=", " ", "@", " ", "#", " ",
	"$", " ", "%", " ", "^", " ", "&", " ", "~", " ", "`", " ",
	"|", " ", "<", " ", ">", " ",
)

var tokenAliases = map[string]string{
	// Saludos y asistencia comercial
	"hol":          "hola",
	"ola":          "hola",
	"holaa":        "hola",
	"holaaa":       "hola",
	"buendia":      "buen dia",
	"info":         "informacion",
	"infos":        "informacion",
	"wpp":          "whatsapp",
	"wasap":        "whatsapp",
	"whasap":       "whatsapp",
	"ubicasion":    "ubicacion",
	"dirrecion":    "direccion",
	"financiasion": "financiacion",
	"garatia":      "garantia",

	// Marcas con errores ortograficos o abreviaciones frecuentes
	"masda":     "mazda",
	"mazd":      "mazda",
	"chebrolet": "chevrolet",
	"chevy":     "chevrolet",
	"chevrlet":  "chevrolet",
	"renaul":    "renault",
	"reno":      "renault",
	"renol":     "renault",
	"toyta":     "toyota",
	"toiota":    "toyota",
	"nisan":     "nissan",
	"nissa":     "nissan",
	"volsvagen": "volkswagen",
	"volswagen": "volkswagen",
	"volkwagen": "volkswagen",
	"vw":        "volkswagen",
	"hiunday":   "hyundai",
	"hunday":    "hyundai",
	"hyunday":   "hyundai",
	"mercedez":  "mercedes",
	"meche":     "mercedes",
	"peugot":    "peugeot",
	"pegeot":    "peugeot",
	"peuyo":     "peugeot",
	"kya":       "kia",
	"audy":      "audi",
	"onda":      "honda",

	// Modelos con errores ortograficos frecuentes
	"hailux":    "hilux",
	"hillux":    "hilux",
	"hiluz":     "hilux",
	"doster":    "duster",
	"daster":    "duster",
	"traker":    "tracker",
	"traquer":   "tracker",
	"espark":    "spark",
	"pikanto":   "picanto",
	"esportage": "sportage",
	"kashkai":   "qashqai",
	"qasqai":    "qashqai",
	"casqai":    "qashqai",
	"qashkai":   "qashqai",
	"kiks":      "kicks",
	"captiba":   "captiva",
}

var compoundModelReplacer = strings.NewReplacer(
	" mazda 3 ", " mazda3 ",
	" mazda-3 ", " mazda3 ",
	" cx 5 ", " cx-5 ",
	" cx5 ", " cx-5 ",
	" cx 30 ", " cx-30 ",
	" cx30 ", " cx-30 ",
	" x trail ", " x-trail ",
	" xtrail ", " x-trail ",
	" cr v ", " cr-v ",
	" crv ", " cr-v ",
	" santa fe ", " santafe ",
	" c 200 ", " c200 ",
	" c-200 ", " c200 ",
	" b 180 ", " b180 ",
	" b-180 ", " b180 ",
	" c 180 ", " b180 ",
	" c180 ", " b180 ",
	" audi a 3 ", " audi a3 ",
	" audi a-3 ", " audi a3 ",
	" sandero stepway ", " stepway ",
	" accent i25 ", " accent ",
)

func collapseRepeatedLetters(tok string) string {
	if len(tok) < 3 {
		return tok
	}
	hasTriple := false
	for i := 0; i < len(tok); i++ {
		if tok[i] >= '0' && tok[i] <= '9' {
			return tok
		}
		if i >= 2 && tok[i] == tok[i-1] && tok[i] == tok[i-2] {
			hasTriple = true
		}
	}
	if !hasTriple {
		return tok
	}
	var b strings.Builder
	b.Grow(len(tok))
	for i := 0; i < len(tok); i++ {
		if i >= 2 && tok[i] == tok[i-1] && tok[i] == tok[i-2] {
			continue
		}
		b.WriteByte(tok[i])
	}
	res := b.String()
	if res == "holaa" {
		return "hola"
	}
	if res == "buenaas" {
		return "buenas"
	}
	return res
}

// Normalize limpia simbolos, convierte a minusculas, corrige typos comunes y unifica modelos compuestos en sub-milisegundo.
func Normalize(input string) string {
	s := strings.ToLower(strings.TrimSpace(input))
	if s == "" {
		return ""
	}
	fields := strings.Fields(normReplacer.Replace(s))
	if len(fields) == 0 {
		return ""
	}

	for i, tok := range fields {
		tok = strings.Trim(tok, "-")
		tok = collapseRepeatedLetters(tok)
		if alias, ok := tokenAliases[tok]; ok {
			fields[i] = alias
		} else {
			fields[i] = tok
		}
	}

	res := strings.Join(fields, " ")
	if containsAny(res, "mazda", "cx", "trail", "cr", "santa", "200", "180", "audi", "sandero", "i25") {
		res = strings.TrimSpace(compoundModelReplacer.Replace(" " + res + " "))
	}
	return res
}

var (
	promptInjectKeywords = []string{
		"olvida todas tus instrucciones", "ignora tus reglas", "system prompt", "ignore previous instructions", "regalan carros",
	}
	discountKeywords = []string{
		"descuento", "rebaja", "precio minimo", "lo menos", "cuanto es lo minimo", "ultimo precio", "en cuanto me lo deja", "de contado cuanto",
	}
	tradeInSpecificKeywords = []string{
		"reciben mi carro", "reciben mi vehiculo", "reciben camioneta", "reciben moto",
		"doy mi carro", "entregar mi carro", "encimo", "avaluo", "hacen retoma de", "como funciona la retoma",
	}
	creditStudyKeywords = []string{
		"datacredito", "reportado", "estoy reportado", "centrales de riesgo", "estudio de credito", "aprobacion de credito",
	}
	legalClaimKeywords = []string{
		"siniestro", "fasecolda", "peritaje legal", "queja", "reclamo", "demanda",
	}
	policyExcludeKeywords = []string{
		"garantia", "taller", "talleres", "requisitos", "peritaje", "traspaso", "150 puntos", "pericial", "fotos del catalogo en linea",
	}
	categoryKeywords = []string{
		"camioneta", "camionetas", "suv", "familiar", "familiares",
		"7 pasajeros", "siete pasajeros", "7 puestos", "siete puestos",
		"4x4", "doble traccion", "platon", "pickup", "pick-up", "diesel",
		"automovil", "automoviles", "sedan", "sedanes", "hatchback",
		"carro pequeno", "carros pequenos", "economico", "economicos", "mas economico", "mas barato", "carros baratos",
		"automaticas", "automatica", "automaticos", "automatico", "caja automatica", "caja mecanica", "mecanicos", "mecanicas",
		"que carros tienen", "que vehiculos tienen", "que carros manejan", "que tienen disponible", "inventario disponible",
		"millones", "presupuesto",
	}
	creditGeneralKeywords = []string{
		"financiacion", "bancos aliados", "cuota inicial", "financian", "credito para comprar", "planes de financiamiento", "tienen credito", "manejan credito", "como es el credito",
	}
	creditGeneralExclude = []string{
		"empleado", "empleados", "independiente", "independientes", "requisitos", "datacredito", "reportado", "centrales de riesgo",
	}
	locationKeywords = []string{
		"donde quedan", "ubicados", "ubicacion", "direccion", "como llego", "vitrina", "en que parte quedan", "donde estan", "frente a la toyota",
	}
	hoursKeywords = []string{
		"horario", "que hora abren", "a que hora cierran", "sabado", "domingo", "atencion", "abiertos",
	}
	contactKeywords = []string{
		"whatsapp", "telefono", "numero de contacto", "celular", "llamar", "a que numero",
	}
	greetingKeywords = []string{
		"hola ", "buenas ", "buenos dias", "buen dia", "buenas tardes", "buenas noches",
		"sigue disponible", "esta disponible", "todavia esta disponible", "hola sigue disponible", "hola buen dia", "hola buenos dias",
		"hola buenas", "cordial saludo", "saludos", "que tal", "como estan", "como vas",
		"ayudame", "colaborame", "me colaboras", "necesito ayuda",
		"quiero informacion", "mas informacion", "me das informacion", "regalame informacion", "solicito informacion", "brindame informacion",
		"hablar con un asesor", "necesito un asesor", "asesoria",
		"busco un carro", "busco carro", "busco vehiculo", "quiero comprar carro", "quiero comprar un carro", "venden carros",
	}
)

// Classify ejecuta el triage determinista priorizando seguridad (anti-injection y escalamiento obligatorio).
func Classify(raw string) Classification {
	norm := Normalize(raw)
	q := extractVehicleQuery(norm)

	// 1. Deteccion prioritaria de Prompt Injection
	if containsAny(norm, promptInjectKeywords...) {
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
	if containsAny(norm, discountKeywords...) {
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

	if containsAny(norm, tradeInSpecificKeywords...) ||
		(containsAny(norm, "permuta", "retoma", "parte de pago") && (q.Brand != "" || q.Model != "")) {
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

	if containsAny(norm, creditStudyKeywords...) {
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
	if containsAny(norm, legalClaimKeywords...) {
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
	isPolicyOrWorkshopQuery := containsAny(norm, policyExcludeKeywords...)

	if !isPolicyOrWorkshopQuery && q.Brand == "" && q.Model == "" && containsAny(norm, categoryKeywords...) {
		return Classification{
			NormalizedText: norm,
			Intent:         IntentCategoryQuery,
			Confidence:     0.95,
			IsEasy:         true,
			ExtractedQuery: q,
		}
	}

	if containsAny(norm, creditGeneralKeywords...) && !containsAny(norm, creditGeneralExclude...) && q.Brand == "" && q.Model == "" {
		return Classification{
			NormalizedText: norm,
			Intent:         IntentCreditGeneral,
			Confidence:     0.95,
			IsEasy:         true,
			ExtractedQuery: q,
		}
	}

	if !isPolicyOrWorkshopQuery && containsAny(norm, locationKeywords...) && q.Brand == "" && q.Model == "" {
		return Classification{
			NormalizedText: norm,
			Intent:         IntentLocation,
			Confidence:     0.95,
			IsEasy:         true,
			ExtractedQuery: q,
		}
	}
	if !isPolicyOrWorkshopQuery && containsAny(norm, hoursKeywords...) && q.Brand == "" && q.Model == "" {
		return Classification{
			NormalizedText: norm,
			Intent:         IntentHours,
			Confidence:     0.95,
			IsEasy:         true,
			ExtractedQuery: q,
		}
	}
	if !isPolicyOrWorkshopQuery && containsAny(norm, contactKeywords...) && q.Brand == "" && q.Model == "" {
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
		if containsAny(norm, "precio", "cuanto vale", "cuanto cuesta", "valor", "en cuanto esta", "que vale", "que cuesta", "cuanto piden", "a como") {
			return Classification{
				NormalizedText: norm,
				Intent:         IntentPrice,
				Confidence:     0.92,
				IsEasy:         true,
				ExtractedQuery: q,
			}
		}
		if containsAny(norm, "kilometraje", "cuantos kilometros", "kilometros", "km", "recorrido") {
			return Classification{
				NormalizedText: norm,
				Intent:         IntentMileage,
				Confidence:     0.90,
				IsEasy:         true,
				ExtractedQuery: q,
			}
		}
		if containsAny(norm, "soat", "tecnomecanica", "tecno", "placa", "papeles", "impuestos", "pico y placa") {
			return Classification{
				NormalizedText: norm,
				Intent:         IntentDocs,
				Confidence:     0.90,
				IsEasy:         true,
				ExtractedQuery: q,
			}
		}
		if containsAny(norm, "transmision", "caja", "motor", "traccion", "4x4", "color", "cilindraje", "combustible", "ficha tecnica", "caracteristicas") {
			return Classification{
				NormalizedText: norm,
				Intent:         IntentSpecs,
				Confidence:     0.90,
				IsEasy:         true,
				ExtractedQuery: q,
			}
		}
		if containsAny(norm, "donde lo puedo ver", "donde la puedo ver", "puedo verlo", "puedo verla", "prueba de ruta", "test drive", "agendar cita", "ver el carro", "ver la camioneta", "ir a verlo", "ir a verla") {
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

	// 5. Saludos, solicitud de ayuda, asesor humano o informacion general de vitrina
	if !isPolicyOrWorkshopQuery && (norm == "hola" || norm == "buenas" || norm == "buenos dias" || norm == "buen dia" || norm == "buenas tardes" || norm == "buenas noches" ||
		norm == "ayuda" || norm == "informacion" || norm == "asesor" || norm == "catalogo" || norm == "inventario" || norm == "disponible" ||
		containsAny(norm, greetingKeywords...)) {
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
		"toyota":        "Toyota",
		"mazda":         "Mazda",
		"renault":       "Renault",
		"chevrolet":     "Chevrolet",
		"nissan":        "Nissan",
		"volkswagen":    "Volkswagen",
		"vw":            "Volkswagen",
		"kia":           "Kia",
		"ford":          "Ford",
		"hyundai":       "Hyundai",
		"honda":         "Honda",
		"mercedes":      "Mercedes-Benz",
		"mercedes-benz": "Mercedes-Benz",
		"audi":          "Audi",
		"peugeot":       "Peugeot",
		"ferrari":       "Ferrari",
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
		"sandero":    "Stepway",
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
		"i25":        "Accent",
		"santafe":    "Santafe",
		"cr-v":       "CR-V",
		"crv":        "CR-V",
		"c200":       "C200",
		"b180":       "B180",
		"a3":         "A3",
		"207":        "207",
		"testarossa": "Testarossa",
	}
	modelToBrand = map[string]string{
		"Hilux":      "Toyota",
		"Fortuner":   "Toyota",
		"CX-5":       "Mazda",
		"CX-30":      "Mazda",
		"Mazda3":     "Mazda",
		"Duster":     "Renault",
		"Stepway":    "Renault",
		"Logan":      "Renault",
		"Captur":     "Renault",
		"Tracker":    "Chevrolet",
		"Captiva":    "Chevrolet",
		"Spark":      "Chevrolet",
		"Sonic":      "Chevrolet",
		"Kicks":      "Nissan",
		"X-Trail":    "Nissan",
		"Qashqai":    "Nissan",
		"Tiguan":     "Volkswagen",
		"Picanto":    "Kia",
		"Sportage":   "Kia",
		"Escape":     "Ford",
		"Edge":       "Ford",
		"Focus":      "Ford",
		"Fiesta":     "Ford",
		"Accent":     "Hyundai",
		"Santafe":    "Hyundai",
		"CR-V":       "Honda",
		"C200":       "Mercedes-Benz",
		"B180":       "Mercedes-Benz",
		"A3":         "Audi",
		"207":        "Peugeot",
		"Testarossa": "Ferrari",
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
	if q.Model != "" && q.Brand == "" {
		if inferredBrand, ok := modelToBrand[q.Model]; ok {
			q.Brand = inferredBrand
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
