package guard

import (
	"strings"
	"testing"

	"buyacar.com.co/omnicanal/internal/inventory"
)

func TestGuard_ZeroHallucinatedNumbersAndURLAllowlist(t *testing.T) {
	g := NewGuard([]string{"buyacar.com.co"})
	vehicles := inventory.DefaultFixtureVehicles()[:1] // Toyota Hilux 2022 ($185000000 COP, 42000 km)

	// 1. Respuesta valida con cifras respaldadas y URL oficial
	validReply := "La Toyota Hilux 2022 tiene un precio de $185000000 COP y 42000 km. WhatsApp 3151212223. Ver en https://buyacar.com.co/vehiculos/toyota-hilux-2022"
	res := g.ValidateOutbox(validReply, "whatsapp", vehicles)
	if !res.Approved || res.MustEscalate {
		t.Fatalf("expected valid reply to be approved, got %+v", res)
	}

	// 2. Bloquear cifra inventada (>= 5 digitos no presente en inventario ni lineas oficiales)
	hallucinatedPrice := "La Toyota Hilux 2022 te queda en $150000000 COP."
	res = g.ValidateOutbox(hallucinatedPrice, "whatsapp", vehicles)
	if res.Approved || !res.MustEscalate || !strings.HasPrefix(res.RejectionReason, "hallucinated_number_blocked") {
		t.Fatalf("expected hallucinated price to be blocked, got %+v", res)
	}

	// 3. Bloquear URL fuera de allowlist
	badURL := "Mira la ficha en https://phishing-cars.example.com/hilux"
	res = g.ValidateOutbox(badURL, "whatsapp", vehicles)
	if res.Approved || !res.MustEscalate || !strings.HasPrefix(res.RejectionReason, "unauthorized_url_domain") {
		t.Fatalf("expected unauthorized URL to be blocked, got %+v", res)
	}

	// 4. Bloquear expresion prohibida
	forbidden := "Ven hoy, tenemos crédito aprobado sin requisitos en Tunja."
	res = g.ValidateOutbox(forbidden, "whatsapp", vehicles)
	if res.Approved || !res.MustEscalate || !strings.HasPrefix(res.RejectionReason, "forbidden_phrase_detected") {
		t.Fatalf("expected forbidden phrase to be blocked, got %+v", res)
	}

	// 5. No truncar precio al final si excede el limite del canal (ej. tiktok = 400 chars)
	longPrefix := strings.Repeat("Vehículo certificado en Tunja listo para prueba de ruta. ", 7) // ~399 chars
	unsafeToTruncate := longPrefix + "185000000"
	res = g.ValidateOutbox(unsafeToTruncate, "tiktok", vehicles)
	if res.Approved || !res.MustEscalate {
		t.Fatalf("expected unsafe truncation of price to escalate, got %+v", res)
	}
}

func TestRedactPII_PreservesOfficialPhones(t *testing.T) {
	raw := "Mi cédula es 1098765432, mi correo es cliente@gmail.com y escribo al WhatsApp 3151212223."
	sanitized, count := RedactPII(raw)
	if count != 2 {
		t.Fatalf("expected 2 PII redactions (ID and email), got %d: %s", count, sanitized)
	}
	if !strings.Contains(sanitized, "3151212223") {
		t.Fatalf("expected official WhatsApp number to be preserved, got %s", sanitized)
	}
	if strings.Contains(sanitized, "1098765432") || strings.Contains(sanitized, "cliente@gmail.com") {
		t.Fatalf("PII leaked in sanitized string: %s", sanitized)
	}
}

func TestGuard_ValidateOutboxWithBackedNumbers(t *testing.T) {
	g := NewGuard([]string{"buyacar.com.co"})
	vehicles := inventory.DefaultFixtureVehicles()[:1] // Toyota Hilux 2022 ($185000000 COP, 42000 km)
	ragBackedNumbers := []string{"20000", "2000000"}

	// 1. Cifra respaldada por RAG (20000 km de garantía) aprobada al pasar backed numbers
	ragReply := "La garantía certificada es de 12 meses o 20000 km en talleres Tunja. Info https://buyacar.com.co/garantia"
	res := g.ValidateOutboxWithBackedNumbers(ragReply, "whatsapp", vehicles, ragBackedNumbers)
	if !res.Approved || res.MustEscalate {
		t.Fatalf("expected backed number 20000 to be approved, got %+v", res)
	}

	// 2. Misma cifra sin backed numbers (ValidateOutbox retrocompatible) debe ser bloqueada
	res = g.ValidateOutbox(ragReply, "whatsapp", vehicles)
	if res.Approved || !res.MustEscalate || !strings.HasPrefix(res.RejectionReason, "hallucinated_number_blocked:20000") {
		t.Fatalf("expected unbacked number 20000 to be blocked under ValidateOutbox, got %+v", res)
	}

	// 3. Cifra de 5+ digitos no respaldada ni en inventario ni en RAG debe ser bloqueada
	unbackedReply := "Descuento especial por liquidación en $99999999 COP."
	res = g.ValidateOutboxWithBackedNumbers(unbackedReply, "whatsapp", vehicles, ragBackedNumbers)
	if res.Approved || !res.MustEscalate || !strings.HasPrefix(res.RejectionReason, "hallucinated_number_blocked:99999999") {
		t.Fatalf("expected hallucinated number 99999999 to be blocked, got %+v", res)
	}
}

func FuzzGuardValidateOutbox(f *testing.F) {
	f.Add("Precio $185000000 COP en https://buyacar.com.co")
	f.Add("Te damos credito aprobado sin requisitos por 99999999")
	f.Add("http://insecure.buyacar.com.co/test")
	g := NewGuard([]string{"buyacar.com.co"})
	vehicles := inventory.DefaultFixtureVehicles()[:1]

	f.Fuzz(func(t *testing.T, reply string) {
		res := g.ValidateOutbox(reply, "tiktok", vehicles)
		if res.Approved && res.MustEscalate {
			t.Fatalf("contradictory state: both Approved and MustEscalate are true")
		}
	})
}
