package guard

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"buyacar.com.co/omnicanal/internal/inventory"
)

var (
	reFiveOrMoreDigits = regexp.MustCompile(`\b\d{5,}\b`)
	reURL              = regexp.MustCompile(`https?://[^\s)\]"']+`)
	reEmail            = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	reCardOrID         = regexp.MustCompile(`\b\d{7,16}\b`)
)

// ForbiddenPhrases contiene las expresiones prohibidas por la especificacion canonica.
var ForbiddenPhrases = []string{
	"credito aprobado sin requisitos",
	"aprobacion garantizada sin estudio",
	"el mas barato del mercado",
	"compramos tu carro sin revisarlo",
	"descuento garantizado",
	"aprobacion inmediata sin requisitos",
}

// MarketplaceNoise contiene sugerencias rapidas y texto contaminante de Meta/Marketplace.
var MarketplaceNoise = []string{
	"¿sigue disponible?",
	"envía un mensaje a",
	"respuesta rápida de marketplace",
}

// ChannelCharLimits define los limites de caracteres oficiales por canal.
var ChannelCharLimits = map[string]int{
	"tiktok":               400,
	"instagram":            450,
	"messenger":            500,
	"facebook_marketplace": 500,
	"whatsapp":             650,
	"email":                2000,
}

// OfficialPhoneNumbers contiene las lineas oficiales verificadas de BuyaCAR.
var OfficialPhoneNumbers = map[string]bool{
	"3151212223": true,
	"3022866969": true,
	"3160502717": true,
}

// ValidationResult encapsula el veredicto del guard pre-envio / post-IA.
type ValidationResult struct {
	Approved        bool
	MustEscalate    bool
	RejectionReason string
	SanitizedReply  string
}

// Guard aplica todas las defensas deterministas antes de cualquier envio.
type Guard struct {
	allowedDomains map[string]bool
}

func NewGuard(allowedDomains []string) *Guard {
	domains := make(map[string]bool, len(allowedDomains))
	for _, d := range allowedDomains {
		domains[strings.ToLower(strings.TrimSpace(d))] = true
	}
	if len(domains) == 0 {
		domains["buyacar.com.co"] = true
	}
	return &Guard{allowedDomains: domains}
}

// RedactPII oculta correos, cedulas, NIT y tarjetas en trazas y notificaciones (Ley 1581 de 2012).
func RedactPII(text string) (string, int) {
	redactions := 0
	out := reEmail.ReplaceAllStringFunc(text, func(_ string) string {
		redactions++
		return "[PII_EMAIL_REDACTED]"
	})
	out = reCardOrID.ReplaceAllStringFunc(out, func(num string) string {
		if OfficialPhoneNumbers[num] {
			return num
		}
		redactions++
		return "[PII_NUM_REDACTED]"
	})
	return out, redactions
}

// CleanIncomingNoise filtra sugerencias automaticas contaminantes de Marketplace/Meta.
func CleanIncomingNoise(text string) string {
	out := text
	for _, noise := range MarketplaceNoise {
		if strings.EqualFold(strings.TrimSpace(out), noise) {
			return "¿Está disponible?"
		}
	}
	return strings.TrimSpace(out)
}

// ValidateOutbox verifica una respuesta candidata contra todas las reglas del GUARD.
// Ante cualquier fallo no envia: devuelve Approved=false y MustEscalate=true.
// Mantiene 100% de compatibilidad hacia atrás llamando a ValidateOutboxWithBackedNumbers con extraAllowedNumbers=nil.
func (g *Guard) ValidateOutbox(reply string, channelID string, relevantVehicles []inventory.Vehicle) ValidationResult {
	return g.ValidateOutboxWithBackedNumbers(reply, channelID, relevantVehicles, nil)
}

// ValidateOutboxWithBackedNumbers verifica una respuesta candidata contra todas las reglas del GUARD,
// permitiendo cifras respaldadas por inventario, líneas oficiales y fragmentos normativos RAG (extraAllowedNumbers).
func (g *Guard) ValidateOutboxWithBackedNumbers(reply string, channelID string, relevantVehicles []inventory.Vehicle, extraAllowedNumbers []string) ValidationResult {
	trimmed := strings.TrimSpace(reply)
	if trimmed == "" {
		return ValidationResult{
			Approved:        false,
			MustEscalate:    true,
			RejectionReason: "empty_reply",
		}
	}

	// 1. Verificar expresiones prohibidas
	norm := normalizeForCheck(trimmed)
	for _, phrase := range ForbiddenPhrases {
		if strings.Contains(norm, phrase) {
			return ValidationResult{
				Approved:        false,
				MustEscalate:    true,
				RejectionReason: fmt.Sprintf("forbidden_phrase_detected:%s", phrase),
			}
		}
	}

	// 2. Verificar URLs: solo HTTPS y en allowlist
	urls := reURL.FindAllString(trimmed, -1)
	for _, rawURL := range urls {
		cleanURL := strings.TrimRight(rawURL, ".,;:!?")
		parsed, err := url.Parse(cleanURL)
		if err != nil || parsed.Scheme != "https" {
			return ValidationResult{
				Approved:        false,
				MustEscalate:    true,
				RejectionReason: fmt.Sprintf("non_https_or_invalid_url:%s", rawURL),
			}
		}
		host := strings.ToLower(parsed.Hostname())
		if !g.isAllowedDomain(host) {
			return ValidationResult{
				Approved:        false,
				MustEscalate:    true,
				RejectionReason: fmt.Sprintf("unauthorized_url_domain:%s", host),
			}
		}
	}

	// 3. Verificar que toda cifra de 5 o mas digitos exista en inventario relevante,
	// sea linea oficial o este respaldada por fixtures RAG (extraAllowedNumbers).
	allowedNumbers := buildAllowedNumbersWithExtras(relevantVehicles, extraAllowedNumbers)
	digitsFound := reFiveOrMoreDigits.FindAllString(trimmed, -1)
	for _, numStr := range digitsFound {
		if !allowedNumbers[numStr] {
			return ValidationResult{
				Approved:        false,
				MustEscalate:    true,
				RejectionReason: fmt.Sprintf("hallucinated_number_blocked:%s", numStr),
			}
		}
	}

	// 4. Verificar limite de caracteres del canal sin truncar precios ni telefonos
	limit := ChannelCharLimits[strings.ToLower(channelID)]
	if limit > 0 && len([]rune(trimmed)) > limit {
		safeShort, ok := safeTrimWithoutCuttingNumbers(trimmed, limit, allowedNumbers)
		if !ok {
			return ValidationResult{
				Approved:        false,
				MustEscalate:    true,
				RejectionReason: "channel_limit_exceeded_unsafe_to_truncate_price_or_phone",
			}
		}
		trimmed = safeShort
	}

	return ValidationResult{
		Approved:       true,
		MustEscalate:   false,
		SanitizedReply: trimmed,
	}
}

func (g *Guard) isAllowedDomain(host string) bool {
	if g.allowedDomains[host] {
		return true
	}
	for d := range g.allowedDomains {
		if strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

func buildAllowedNumbers(vehicles []inventory.Vehicle) map[string]bool {
	return buildAllowedNumbersWithExtras(vehicles, nil)
}

func buildAllowedNumbersWithExtras(vehicles []inventory.Vehicle, extraAllowedNumbers []string) map[string]bool {
	allowed := make(map[string]bool)
	for phone := range OfficialPhoneNumbers {
		allowed[phone] = true
	}
	for _, v := range vehicles {
		if v.PriceCOP > 0 {
			allowed[strconv.FormatInt(v.PriceCOP, 10)] = true
		}
		if v.MileageKm > 0 {
			allowed[strconv.Itoa(v.MileageKm)] = true
		}
	}
	for _, num := range extraAllowedNumbers {
		s := strings.TrimSpace(num)
		if s != "" {
			allowed[s] = true
			for _, m := range reFiveOrMoreDigits.FindAllString(s, -1) {
				allowed[m] = true
			}
		}
	}
	return allowed
}

func safeTrimWithoutCuttingNumbers(text string, limit int, allowedNumbers map[string]bool) (string, bool) {
	runes := []rune(text)
	if len(runes) <= limit {
		return text, true
	}
	// Cortar en el ultimo espacio antes del limite
	cutIdx := limit
	for cutIdx > 0 && runes[cutIdx-1] != ' ' {
		cutIdx--
	}
	if cutIdx == 0 {
		return "", false
	}
	candidate := strings.TrimSpace(string(runes[:cutIdx]))

	// Verificar que todas las cifras >= 5 digitos del original sigan intactas en el candidato
	origNums := reFiveOrMoreDigits.FindAllString(text, -1)
	candNums := reFiveOrMoreDigits.FindAllString(candidate, -1)
	if len(origNums) != len(candNums) {
		return "", false
	}
	for i := range origNums {
		if origNums[i] != candNums[i] || !allowedNumbers[candNums[i]] {
			return "", false
		}
	}
	return candidate, true
}

func normalizeForCheck(s string) string {
	s = strings.ToLower(s)
	r := strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u")
	return r.Replace(s)
}
