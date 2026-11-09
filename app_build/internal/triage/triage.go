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

	// Nuevas intenciones del ciclo de vida automotriz integral (DAG)
	IntentAfterSales   Intent = "after_sales_service"    // Mantenimiento y posventa
	IntentParts        Intent = "parts_and_accessories"  // Repuestos y accesorios
	IntentPayments     Intent = "payments_and_reserves"  // Reservas, señas y cuentas oficiales
	IntentDocsLegal    Intent = "docs_and_transfers"     // Traspasos, impuestos y certificados
	IntentUniversal    Intent = "universal_fallback"     // Fallback guiado estructurado
)

// Classification contiene el resultado determinista del triage.
type Classification struct {
	NormalizedText   string
	Intent           Intent
	Intents          []Intent
	Confidence       float64
	IsEasy           bool
	MustEscalate     bool
	EscalationReason string
	ExtractedQuery   inventory.Query
	HasPIIOther      bool
	HasPIIOwn        bool
}

var normReplacer = strings.NewReplacer(
	"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
	"×", "x", "·", " ", "\ufffd", " ",
	"¿", " ", "?", " ", "¡", " ", "!", " ", ",", " ", ".", " ",
	"[", " ", "]", " ", "(", " ", ")", " ", ":", " ", ";", " ",
	"{", " ", "}", " ", "\"", " ", "'", " ", "*", " ", "/", " ",
	"\\", " ", "_", " ", "+", " ", "=", " ", "@", " ", "#", " ",
	"$", " ", "%", " ", "^", " ", "&", " ", "~", " ", "`", " ",
	"|", " ", "<", " ", ">", " ",
)

var tokenAliases = map[string]string{
	// Saludos, tildes truncadas y asistencia comercial
	"hol":          "hola",
	"ola":          "hola",
	"holaa":        "hola",
	"holaaa":       "hola",
	"buendia":      "buen dia",
	"da":           "dia",
	"das":          "dias",
	"info":         "informacion",
	"infos":        "informacion",
	"wpp":          "whatsapp",
	"wasap":        "whatsapp",
	"whasap":       "whatsapp",
	"ubicasion":    "ubicacion",
	"dirrecion":    "direccion",
	"direcion":     "direccion",
	"financiasion": "financiacion",
	"finansiacion": "financiacion",
	"garatia":      "garantia",
	"garantias":    "garantia",
	"presio":       "precio",
	"kilometrage":  "kilometraje",

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
	"susuki":    "suzuki",
	"zusuki":    "suzuki",
	"mitsubichi": "mitsubishi",
	"mitsubisi": "mitsubishi",

	// Modelos con errores ortograficos frecuentes
	"hailux":    "hilux",
	"hillux":    "hilux",
	"hiluz":     "hilux",
	"forchuner": "fortuner",
	"korolla":   "corolla",
	"corola":    "corolla",
	"koleoz":    "koleos",
	"coleos":    "koleos",
	"bitara":    "vitara",
	"suift":     "swift",
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
	"tuxon":     "tucson",
	"tukson":    "tucson",
}

var compoundModelReplacer = strings.NewReplacer(
	" mazda 3 ", " mazda3 ",
	" mazda-3 ", " mazda3 ",
	" mazda 2 ", " mazda2 ",
	" mazda-2 ", " mazda2 ",
	" mazda 6 ", " mazda6 ",
	" mazda-6 ", " mazda6 ",
	" bt 50 ", " bt-50 ",
	" bt50 ", " bt-50 ",
	" cx 5 ", " cx-5 ",
	" cx5 ", " cx-5 ",
	" cx 30 ", " cx-30 ",
	" cx30 ", " cx-30 ",
	" x trail ", " x-trail ",
	" xtrail ", " x-trail ",
	" cr v ", " cr-v ",
	" crv ", " cr-v ",
	" rav 4 ", " rav4 ",
	" rav-4 ", " rav4 ",
	" d max ", " d-max ",
	" dmax ", " d-max ",
	" t cross ", " t-cross ",
	" tcross ", " t-cross ",
	" grand vitara ", " vitara ",
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
	if containsAny(res, "mazda", "cx", "trail", "cr v", "crv", "santa", "200", "180", "audi", "sandero", "i25", "bt 50", "bt50", "d max", "dmax", "t cross", "tcross", "rav 4", "grand vitara") {
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
		"whatsapp", "telefono", "telefonico", "numero de contacto", "numero para llamarte",
		"regalas un numero", "me das un numero", "celular", "llamar", "llamarte", "a que numero",
	}
	visitGeneralKeywords = []string{
		"agendar cita", "agendar una cita", "apartar cita", "prueba de ruta", "test drive",
		"ir a verlos", "pasar a verlos", "visitar la vitrina", "puedo ir hoy", "puedo ir manana",
		"pasar hoy", "pasar manana", "ir a verlo", "ir a verla", "donde lo puedo ver",
		"donde la puedo ver", "puedo verlo", "puedo verla", "ver el carro", "ver la camioneta",
	}
	greetingKeywords = []string{
		"hola ", "buenas ", "buenos dias", "buen dia", "buenas tardes", "buenas noches",
		"sigue disponible", "esta disponible", "todavia esta disponible", "hola sigue disponible", "hola buen dia", "hola buenos dias",
		"hola buenas", "cordial saludo", "saludos", "que tal", "como estan", "como vas",
		"ayudame", "colaborame", "me colaboras", "necesito ayuda",
		"quiero informacion", "mas informacion", "me das informacion", "regalame informacion", "solicito informacion", "brindame informacion",
		"hablar con un asesor", "necesito un asesor", "asesoria",
		"busco un carro", "busco carro", "busco vehiculo", "quiero comprar carro", "quiero comprar un carro", "venden carros",
		"muchas gracias", "mil gracias", "ok gracias", "listo gracias", "quedo atento", "quedo atenta", "estare atento", "estare atenta", "muy amable", "muy amables",
	}
)

func isDigitsOnly(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isValidCustomerMobile10(d string) bool {
	if len(d) != 10 || !isDigitsOnly(d) {
		return false
	}
	if d[0] != '3' || d[1] < '0' || d[1] > '5' {
		return false
	}
	// Excluir lineas oficiales de BuyaCAR
	if d == "3151212223" || d == "3022866969" || d == "3160502717" {
		return false
	}
	return true
}

func hasCustomerMobile(norm string) bool {
	if containsAny(norm, "cedula", "cc ", "documento", "nit") {
		return false
	}
	if strings.Contains(norm, "pii num redacted") {
		return true
	}
	tokens := strings.Fields(norm)
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if len(tok) == 12 && strings.HasPrefix(tok, "57") && isValidCustomerMobile10(tok[2:]) {
			return true
		}
		if isValidCustomerMobile10(tok) {
			return true
		}
		if i+2 < len(tokens) && len(tok) == 3 && len(tokens[i+1]) == 3 && len(tokens[i+2]) == 4 {
			if isValidCustomerMobile10(tok + tokens[i+1] + tokens[i+2]) {
				return true
			}
		}
		if i+1 < len(tokens) && len(tok) == 3 && len(tokens[i+1]) == 7 {
			if isValidCustomerMobile10(tok + tokens[i+1]) {
				return true
			}
		}
	}
	return false
}

var courtesyAllowedTokens = map[string]bool{
	"gracias": true, "muchas": true, "mil": true, "ok": true, "okay": true,
	"listo": true, "vale": true, "perfecto": true, "entendido": true, "dale": true,
	"bueno": true, "super": true, "excelente": true, "genial": true, "bien": true,
	"quedo": true, "estare": true, "atento": true, "atenta": true, "muy": true,
	"amable": true, "amables": true, "feliz": true, "dia": true, "tarde": true,
	"noche": true, "bendiciones": true, "chao": true, "hasta": true, "luego": true,
}

var courtesyCoreTokens = map[string]bool{
	"gracias": true, "ok": true, "okay": true, "listo": true, "vale": true,
	"perfecto": true, "entendido": true, "dale": true, "excelente": true,
	"genial": true, "atento": true, "atenta": true, "amable": true, "amables": true,
	"bendiciones": true, "chao": true,
}

// IsCourtesyOnly indica si el mensaje normalizado es un agradecimiento o cierre conversacional breve.
func IsCourtesyOnly(norm string) bool {
	tokens := strings.Fields(norm)
	if len(tokens) == 0 || len(tokens) > 5 {
		return false
	}
	hasCore := false
	for _, t := range tokens {
		if !courtesyAllowedTokens[t] {
			return false
		}
		if courtesyCoreTokens[t] {
			hasCore = true
		}
	}
	return hasCore
}

// Classify ejecuta el triage determinista priorizando seguridad (anti-injection y escalamiento obligatorio).
func Classify(raw string) Classification {
	norm := Normalize(raw)
	q := extractVehicleQuery(norm)

	// 1. Deteccion prioritaria de Prompt Injection
	if containsAny(norm, promptInjectKeywords...) {
		return Classification{
			NormalizedText:   norm,
			Intent:           IntentPromptInject,
			Intents:          []Intent{IntentPromptInject},
			Confidence:       0.99,
			IsEasy:           false,
			MustEscalate:     true,
			EscalationReason: "prompt_injection_detected",
			ExtractedQuery:   q,
		}
	}

	// 1.1 Detección de solicitud de PII de terceros o requerimientos de autoridades
	if containsAny(norm, "cedula del dueno", "cedula del propietario", "quien era el dueno", "datos del dueno", "telefono del dueno", "nombre del dueno", "quien lo vendio", "fiscalia", "oficio urgente", "investigacion penal", "policia judicial", "juzgado", "requerimiento legal") {
		return Classification{
			NormalizedText:   norm,
			Intent:           IntentSensitivePII,
			Intents:          []Intent{IntentSensitivePII},
			Confidence:       0.99,
			IsEasy:           true,
			MustEscalate:     false,
			HasPIIOther:      true,
			ExtractedQuery:   q,
		}
	}

	// 1.2 Detección de Posventa, Mantenimiento y Repuestos
	if containsAny(norm, "posventa", "mantenimiento", "cambio de aceite", "taller para revision", "revision tecnica de mi carro", "servicio de taller") {
		return Classification{
			NormalizedText:   norm,
			Intent:           IntentAfterSales,
			Intents:          []Intent{IntentAfterSales},
			Confidence:       0.95,
			IsEasy:           true,
			ExtractedQuery:   q,
		}
	}

	if containsAny(norm, "repuesto", "repuestos", "autopartes", "accesorios", "venden repuestos", "conseguir repuestos", "pastillas de freno", "amortiguador", "amortiguadores", "bateria", "filtro", "llantas") {
		return Classification{
			NormalizedText:   norm,
			Intent:           IntentParts,
			Intents:          []Intent{IntentParts},
			Confidence:       0.95,
			IsEasy:           true,
			ExtractedQuery:   q,
		}
	}

	// 1.3 Detección de Pagos, Señas y Cuentas Bancarias
	if containsAny(norm, "donde consigno", "cuenta bancaria", "como pago", "separar el carro", "hacer la sena", "abono inicial", "a que cuenta consigno", "numero de cuenta", "daviplata", "nequi", "transferir el anticipo", "transferirles el anticipo", "giro ya mismo", "donde les giro", "consignar el anticipo") {
		return Classification{
			NormalizedText:   norm,
			Intent:           IntentPayments,
			Intents:          []Intent{IntentPayments},
			Confidence:       0.95,
			IsEasy:           true,
			ExtractedQuery:   q,
		}
	}

	// 2. Deteccion de causales de escalamiento obligatorio (paso 10 del flujo)
	if containsAny(norm, discountKeywords...) ||
		containsAny(norm, "millones en efectivo", "cerramos negocio", "te ofrezco", "le ofrezco", "te doy", "le doy") {
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
		containsAny(norm, "retoma", "retoman", "reciben usado", "parte de pago", "permuta", "permutas") {
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

	if !isPolicyOrWorkshopQuery && q.Brand == "" && q.Model == "" && (hasCustomerMobile(norm) || containsAny(norm, visitGeneralKeywords...)) {
		return Classification{
			NormalizedText: norm,
			Intent:         IntentVisit,
			Confidence:     0.92,
			IsEasy:         true,
			ExtractedQuery: q,
		}
	}

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
		if containsAny(norm, "precio", "cuanto vale", "cuanto cuesta", "valor", "en cuanto esta", "que vale", "que cuesta", "cuanto piden", "a como", "que precio") {
			return Classification{
				NormalizedText: norm,
				Intent:         IntentPrice,
				Confidence:     0.92,
				IsEasy:         true,
				ExtractedQuery: q,
			}
		}
		if containsAny(norm, "kilometraje", "cuantos kilometros", "kilometros", "km", "recorrido", "cuanto recorrido") {
			return Classification{
				NormalizedText: norm,
				Intent:         IntentMileage,
				Confidence:     0.90,
				IsEasy:         true,
				ExtractedQuery: q,
			}
		}
		if containsAny(norm, "soat", "tecnomecanica", "tecno", "placa", "papeles", "impuestos", "pico y placa", "matriculado", "matricula", "de donde es", "donde es la placa", "al dia") {
			return Classification{
				NormalizedText: norm,
				Intent:         IntentDocs,
				Confidence:     0.90,
				IsEasy:         true,
				ExtractedQuery: q,
			}
		}
		if hasCustomerMobile(norm) || containsAny(norm, visitGeneralKeywords...) {
			return Classification{
				NormalizedText: norm,
				Intent:         IntentVisit,
				Confidence:     0.90,
				IsEasy:         true,
				ExtractedQuery: q,
			}
		}
		if containsAny(norm, contactKeywords...) {
			return Classification{
				NormalizedText: norm,
				Intent:         IntentContact,
				Confidence:     0.92,
				IsEasy:         true,
				ExtractedQuery: q,
			}
		}
		if containsAny(norm, "transmision", "caja", "motor", "traccion", "4x4", "color", "cilindraje", "combustible", "ficha tecnica", "caracteristicas", "automatico", "automatica", "mecanico", "mecanica", "gasolina", "diesel", "version", "cojineria", "equipamiento") {
			return Classification{
				NormalizedText: norm,
				Intent:         IntentSpecs,
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

	// 5. Saludos, cortesias, solicitud de ayuda, asesor humano o informacion general de vitrina
	if !isPolicyOrWorkshopQuery && (norm == "hola" || norm == "buenas" || norm == "buenos dias" || norm == "buen dia" || norm == "buenas tardes" || norm == "buenas noches" ||
		norm == "ayuda" || norm == "informacion" || norm == "asesor" || norm == "catalogo" || norm == "inventario" || norm == "disponible" ||
		IsCourtesyOnly(norm) || containsAny(norm, greetingKeywords...)) {
		return Classification{
			NormalizedText: norm,
			Intent:         IntentGreeting,
			Confidence:     0.95,
			IsEasy:         true,
		}
	}

	// 6. Detección de mensajes ambiguos, ruido o sin coincidencia -> Fallback Guiado Universal
	if len(strings.TrimSpace(norm)) > 0 && q.Brand == "" && q.Model == "" && !containsAny(norm, policyExcludeKeywords...) {
		return Classification{
			NormalizedText: norm,
			Intent:         IntentUniversal,
			Intents:        []Intent{IntentUniversal},
			Confidence:     0.90,
			IsEasy:         true,
			ExtractedQuery: q,
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
	if cl.Intent == IntentLocation || cl.Intent == IntentHours || cl.Intent == IntentCreditGeneral {
		return cl
	}
	if cl.Intent == IntentGreeting {
		return cl
	}

	isPurePolicyFollowUp := containsAny(cl.NormalizedText, policyExcludeKeywords...) &&
		!containsAny(cl.NormalizedText, "sigue disponible", "esta disponible", "precio", "cuanto vale", "cuanto cuesta", "valor", "kilometraje", "kilometros")

	for i := len(priorTexts) - 1; i >= 0; i-- {
		pq := extractVehicleQuery(Normalize(priorTexts[i]))
		if pq.Brand != "" || pq.Model != "" {
			if isPurePolicyFollowUp {
				cl.ExtractedQuery = pq
				return cl
			}
			vehPrefix := strings.TrimSpace(pq.Brand + " " + pq.Model)
			if pq.Year > 0 {
				vehPrefix = vehPrefix + " " + strconv.Itoa(pq.Year)
			}
			combined := vehPrefix + " " + raw
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
		"suzuki":        "Suzuki",
		"mitsubishi":    "Mitsubishi",
		"bmw":           "BMW",
		"jeep":          "Jeep",
		"subaru":        "Subaru",
		"dodge":         "Dodge",
		"citroen":       "Citroen",
		"chery":         "Chery",
		"byd":           "BYD",
		"jac":           "JAC",
		"tesla":         "Tesla",
		"porsche":       "Porsche",
		"volvo":         "Volvo",
	}
	knownModels = map[string]string{
		"hilux":       "Hilux",
		"fortuner":    "Fortuner",
		"prado":       "Prado",
		"txl":         "Prado",
		"corolla":     "Corolla",
		"rav4":        "RAV4",
		"yaris":       "Yaris",
		"cx-5":        "CX-5",
		"cx5":         "CX-5",
		"cx-30":       "CX-30",
		"cx30":        "CX-30",
		"mazda3":      "Mazda3",
		"mazda2":      "Mazda2",
		"mazda6":      "Mazda6",
		"bt-50":       "BT-50",
		"bt50":        "BT-50",
		"duster":      "Duster",
		"stepway":     "Stepway",
		"sandero":     "Stepway",
		"logan":       "Logan",
		"captur":      "Captur",
		"koleos":      "Koleos",
		"kwid":        "Kwid",
		"oroch":       "Oroch",
		"twingo":      "Twingo",
		"clio":        "Clio",
		"tracker":     "Tracker",
		"captiva":     "Captiva",
		"spark":       "Spark",
		"sonic":       "Sonic",
		"onix":        "Onix",
		"sail":        "Sail",
		"aveo":        "Aveo",
		"cruze":       "Cruze",
		"equinox":     "Equinox",
		"trailblazer": "Trailblazer",
		"d-max":       "D-Max",
		"dmax":        "D-Max",
		"kicks":       "Kicks",
		"x-trail":     "X-Trail",
		"xtrail":      "X-Trail",
		"qashqai":     "Qashqai",
		"march":       "March",
		"versa":       "Versa",
		"sentra":      "Sentra",
		"frontier":    "Frontier",
		"tiguan":      "Tiguan",
		"gol":         "Gol",
		"voyage":      "Voyage",
		"jetta":       "Jetta",
		"virtus":      "Virtus",
		"t-cross":     "T-Cross",
		"tcross":      "T-Cross",
		"nivus":       "Nivus",
		"taos":        "Taos",
		"amarok":      "Amarok",
		"picanto":     "Picanto",
		"sportage":    "Sportage",
		"rio":         "Rio",
		"cerato":      "Cerato",
		"seltos":      "Seltos",
		"sorento":     "Sorento",
		"escape":      "Escape",
		"edge":        "Edge",
		"focus":       "Focus",
		"fiesta":      "Fiesta",
		"ranger":      "Ranger",
		"explorer":    "Explorer",
		"ecosport":    "EcoSport",
		"bronco":      "Bronco",
		"accent":      "Accent",
		"i25":         "Accent",
		"santafe":     "Santafe",
		"tucson":      "Tucson",
		"creta":       "Creta",
		"elantra":     "Elantra",
		"i10":         "i10",
		"cr-v":        "CR-V",
		"crv":         "CR-V",
		"civic":       "Civic",
		"hr-v":        "HR-V",
		"hrv":         "HR-V",
		"c200":        "C200",
		"b180":        "B180",
		"a3":          "A3",
		"207":         "207",
		"testarossa":  "Testarossa",
		"vitara":      "Vitara",
		"swift":       "Swift",
		"jimny":       "Jimny",
		"montero":     "Montero",
		"l200":        "L200",
		"outlander":   "Outlander",
		"asx":         "ASX",
		"forester":    "Forester",
		"xv":          "XV",
		"compass":     "Compass",
		"renegade":    "Renegade",
		"wrangler":    "Wrangler",
		"x1":          "X1",
		"x3":          "X3",
		"x5":          "X5",
		"journey":     "Journey",
		"durango":     "Durango",
	}
	modelToBrand = map[string]string{
		"Hilux":       "Toyota",
		"Fortuner":    "Toyota",
		"Prado":       "Toyota",
		"Corolla":     "Toyota",
		"RAV4":        "Toyota",
		"Yaris":       "Toyota",
		"CX-5":        "Mazda",
		"CX-30":       "Mazda",
		"Mazda3":      "Mazda",
		"Mazda2":      "Mazda",
		"Mazda6":      "Mazda",
		"BT-50":       "Mazda",
		"Duster":      "Renault",
		"Stepway":     "Renault",
		"Logan":       "Renault",
		"Captur":      "Renault",
		"Koleos":      "Renault",
		"Kwid":        "Renault",
		"Oroch":       "Renault",
		"Twingo":      "Renault",
		"Clio":        "Renault",
		"Tracker":     "Chevrolet",
		"Captiva":     "Chevrolet",
		"Spark":       "Chevrolet",
		"Sonic":       "Chevrolet",
		"Onix":        "Chevrolet",
		"Sail":        "Chevrolet",
		"Aveo":        "Chevrolet",
		"Cruze":       "Chevrolet",
		"Equinox":     "Chevrolet",
		"Trailblazer": "Chevrolet",
		"D-Max":       "Chevrolet",
		"Kicks":       "Nissan",
		"X-Trail":     "Nissan",
		"Qashqai":     "Nissan",
		"March":       "Nissan",
		"Versa":       "Nissan",
		"Sentra":      "Nissan",
		"Frontier":    "Nissan",
		"Tiguan":      "Volkswagen",
		"Gol":         "Volkswagen",
		"Voyage":      "Volkswagen",
		"Jetta":       "Volkswagen",
		"Virtus":      "Volkswagen",
		"T-Cross":     "Volkswagen",
		"Nivus":       "Volkswagen",
		"Taos":        "Volkswagen",
		"Amarok":      "Volkswagen",
		"Picanto":     "Kia",
		"Sportage":    "Kia",
		"Rio":         "Kia",
		"Cerato":      "Kia",
		"Seltos":      "Kia",
		"Sorento":     "Kia",
		"Escape":      "Ford",
		"Edge":        "Ford",
		"Focus":       "Ford",
		"Fiesta":      "Ford",
		"Ranger":      "Ford",
		"Explorer":    "Ford",
		"EcoSport":    "Ford",
		"Bronco":      "Ford",
		"Accent":      "Hyundai",
		"Santafe":     "Hyundai",
		"Tucson":      "Hyundai",
		"Creta":       "Hyundai",
		"Elantra":     "Hyundai",
		"i10":         "Hyundai",
		"CR-V":        "Honda",
		"Civic":       "Honda",
		"HR-V":        "Honda",
		"C200":        "Mercedes-Benz",
		"B180":        "Mercedes-Benz",
		"A3":          "Audi",
		"207":         "Peugeot",
		"Testarossa":  "Ferrari",
		"Vitara":      "Suzuki",
		"Swift":       "Suzuki",
		"Jimny":       "Suzuki",
		"Montero":     "Mitsubishi",
		"L200":        "Mitsubishi",
		"Outlander":   "Mitsubishi",
		"ASX":         "Mitsubishi",
		"Forester":    "Subaru",
		"XV":          "Subaru",
		"Compass":     "Jeep",
		"Renegade":    "Jeep",
		"Wrangler":    "Jeep",
		"X1":          "BMW",
		"X3":          "BMW",
		"X5":          "BMW",
		"Journey":     "Dodge",
		"Durango":     "Dodge",
	}
)

func extractVehicleQuery(norm string) inventory.Query {
	var q inventory.Query

	tokens := strings.Fields(norm)
	for i, tok := range tokens {
		if b, ok := knownBrands[tok]; ok {
			q.Brand = b
		}
		if m, ok := knownModels[tok]; ok {
			q.Model = m
		}
		if len(tok) == 4 {
			if yr, err := strconv.Atoi(tok); err == nil && yr >= 2000 && yr <= 2030 {
				if i >= 2 && tokens[i-1] == "en" && tokens[i-2] == "facebook" {
					continue
				}
				if q.Year == 0 {
					q.Year = yr
				}
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
