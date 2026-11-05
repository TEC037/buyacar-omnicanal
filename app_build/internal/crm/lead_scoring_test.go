package crm

import (
	"testing"
)

func TestLeadScorer_FunnelAndScoring(t *testing.T) {
	// 1. Mensaje inicial con anuncio en Marketplace
	score1, stage1 := CalculateIntentScore("¿Sigue disponible?", "Nissan Kicks 2020", 1)
	if score1 < 50 || stage1 != StageQualified {
		t.Fatalf("expected qualified stage with score >= 50, got score=%d, stage=%s", score1, stage1)
	}

	// 2. Mensaje de alta intención (agendar cita de prueba de ruta en Tunja)
	score2, stage2 := CalculateIntentScore("Quiero ir mañana a Tunja a verla y hacer prueba de ruta", "Nissan Kicks 2020", 2)
	if score2 < 80 || stage2 != StageAppointment {
		t.Fatalf("expected appointment stage with score >= 80, got score=%d, stage=%s", score2, stage2)
	}

	// 3. Intento de regateo (negociación comercial humana)
	_, stage3 := CalculateIntentScore("¿Cuánto es lo menos por la camioneta?", "Nissan Kicks 2020", 3)
	if stage3 != StageNegotiation {
		t.Fatalf("expected negotiation stage, got %s", stage3)
	}

	// 4. Captura de número de celular colombiano desde Marketplace
	phone := ExtractColombianPhone("Claro que sí, mi número es 310 987 6543 para que me escriban al WhatsApp")
	if phone != "3109876543" {
		t.Fatalf("expected phone 3109876543, got %s", phone)
	}

	score4, stage4 := CalculateIntentScore("Mi whatsapp es 3123456789, escríbanme allá por favor", "Toyota Fortuner 2024", 2)
	if score4 < 90 || stage4 != StageAppointment {
		t.Fatalf("expected high score >= 90 and StageAppointment when phone is provided, got score=%d, stage=%s", score4, stage4)
	}
}
