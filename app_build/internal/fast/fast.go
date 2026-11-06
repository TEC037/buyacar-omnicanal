package fast

import (
	"context"
	"fmt"
	"time"

	"buyacar.com.co/omnicanal/internal/inventory"
	"buyacar.com.co/omnicanal/internal/triage"
)

// BusinessTexts almacena los textos comerciales editables en configuracion (nunca hardcodeados en logica).
type BusinessTexts struct {
	AddressReply      string
	HoursReply        string
	ContactReply      string
	GreetingReply     string
	CreditReply       string
	SoldVehicleNotice string
}

// DefaultBusinessTexts retorna los textos verificados de BuyaCAR en Tunja con voz comercial colombiana.
func DefaultBusinessTexts() BusinessTexts {
	return BusinessTexts{
		AddressReply:      "¡Con mucho gusto! En BuyaCAR te esperamos en nuestra vitrina principal en Tunja, Boyacá: Av. Oriental - Cra. 6 #52-23, frente a la Toyota. ¡Ven y hacemos prueba de ruta de tu próximo auto!",
		HoursReply:        "¡Hola! Con todo gusto: en BuyaCAR Tunja atendemos de Lunes a Viernes de 8:00 AM a 6:00 PM (jornada continua) y los Sábados de 8:00 AM a 2:00 PM. Los Domingos descansamos. ¡Te esperamos!",
		ContactReply:      "¡Hola! Claro que sí, con mucho gusto: comunícate a nuestra línea oficial de WhatsApp exclusiva 3151212223 o a nuestras líneas fijas de vitrina 3022866969 y 3160502717. ¡BuyaCAR, confianza sobre ruedas!",
		GreetingReply:     "¡Hola! Qué gusto saludarte. Bienvenido a BuyaCAR en Tunja, confianza sobre ruedas. Contamos con vehículos certificados, manejamos todos los bancos y recibimos tu vehículo como parte de pago (permuta). ¿En qué te podemos asesorar hoy?",
		CreditReply:       "¡Claro que sí! En BuyaCAR te asesoramos con la financiación de tu vehículo a través de nuestros bancos aliados (Bancolombia, Davivienda, Sufi y Finandina). Hacemos la simulación indicativa de tu cuota inicial según tu perfil. ¿A qué número de WhatsApp te contactamos para orientarte con los requisitos?",
		SoldVehicleNotice: "¡Hola! Te contamos que el vehículo consultado ya fue vendido/separado, pero en BuyaCAR Tunja tenemos excelentes opciones similares en camionetas y autos certificados listos para entrega inmediata. ¿Te gustaría conocer las opciones disponibles?",
	}
}

// Result contiene la decision del motor Fast-Path.
type Result struct {
	Handled          bool
	Reply            string
	MustEscalate     bool
	EscalationReason string
	InventoryStatus  inventory.Status
}

// Engine ejecuta el Fast-Path determinista solo cuando confianza >= 0.80 y estado es exact_match o vehicle_not_required.
type Engine struct {
	texts               BusinessTexts
	confidenceThreshold float64
}

func NewEngine(texts BusinessTexts, threshold float64) *Engine {
	if threshold <= 0 {
		threshold = 0.80
	}
	return &Engine{
		texts:               texts,
		confidenceThreshold: threshold,
	}
}

// Evaluate procesa la clasificacion y consulta la cache de inventario sin usar IA (0 tokens).
func (e *Engine) Evaluate(ctx context.Context, cl triage.Classification, cache *inventory.Cache, now time.Time) Result {
	if cl.MustEscalate {
		return Result{
			Handled:          false,
			MustEscalate:     true,
			EscalationReason: cl.EscalationReason,
		}
	}

	if !cl.IsEasy || cl.Confidence < e.confidenceThreshold {
		return Result{
			Handled: false,
		}
	}

	// Intenciones que no requieren consultar un vehiculo especifico
	switch cl.Intent {
	case triage.IntentLocation:
		return Result{Handled: true, Reply: e.texts.AddressReply, InventoryStatus: inventory.StatusVehicleNotRequired}
	case triage.IntentHours:
		return Result{Handled: true, Reply: e.texts.HoursReply, InventoryStatus: inventory.StatusVehicleNotRequired}
	case triage.IntentContact:
		return Result{Handled: true, Reply: e.texts.ContactReply, InventoryStatus: inventory.StatusVehicleNotRequired}
	case triage.IntentGreeting:
		return Result{Handled: true, Reply: e.texts.GreetingReply, InventoryStatus: inventory.StatusVehicleNotRequired}
	case triage.IntentCreditGeneral:
		return Result{Handled: true, Reply: e.texts.CreditReply, InventoryStatus: inventory.StatusVehicleNotRequired}
	}

	// Consulta de inventario
	match := cache.Search(ctx, cl.ExtractedQuery, now)

	switch match.Status {
	case inventory.StatusExactMatch:
		v := match.Vehicles[0]
		// Leccion conocida #4: Vehiculos vendidos o no disponibles requieren respuesta comercial y escalamiento, nunca silencio.
		if v.Status != "available" {
			return Result{
				Handled:          true,
				Reply:            e.texts.SoldVehicleNotice,
				MustEscalate:     true,
				EscalationReason: "vehicle_sold_or_reserved",
				InventoryStatus:  inventory.StatusExactMatch,
			}
		}
		return Result{
			Handled:         true,
			Reply:           formatVehicleReply(cl.Intent, v),
			InventoryStatus: inventory.StatusExactMatch,
		}

	case inventory.StatusMultipleMatches:
		// Leccion conocida #3: Dos vehiculos del mismo modelo con distinto ano requieren pregunta o escalamiento.
		return Result{
			Handled:          false,
			MustEscalate:     true,
			EscalationReason: "multiple_matches_disambiguation_required",
			InventoryStatus:  inventory.StatusMultipleMatches,
		}

	case inventory.StatusNoMatch, inventory.StatusStale, inventory.StatusUnavailable:
		// Regla 6 y 10 del flujo: nunca adivinar; escalar cuando no hay match o inventario caducado/caido.
		return Result{
			Handled:          false,
			MustEscalate:     true,
			EscalationReason: string(match.Status),
			InventoryStatus:  match.Status,
		}

	default:
		return Result{
			Handled:         false,
			InventoryStatus: match.Status,
		}
	}
}

func formatVehicleReply(intent triage.Intent, v inventory.Vehicle) string {
	switch intent {
	case triage.IntentPrice:
		return fmt.Sprintf("¡Hola! Con mucho gusto: el %s %s modelo %d certificado cuenta con un excelente precio de $%d COP (%d km, transmisión %s). Recibimos tu vehículo en permuta y manejamos todos los bancos aliados. ¿A qué número de WhatsApp te compartimos más fotos y coordinamos tu prueba de ruta en Tunja? (Línea oficial: 3151212223). Más detalles: %s",
			v.Brand, v.Model, v.Year, v.PriceCOP, v.MileageKm, v.Transmission, v.OfficialURL)
	case triage.IntentMileage:
		return fmt.Sprintf("¡Hola! Con todo gusto: el %s %s modelo %d tiene solo %d km verificados y garantizados, con un precio de $%d COP. ¿Te gustaría agendar una prueba de ruta en nuestra vitrina de Tunja? Escríbenos o déjanos tu WhatsApp (Línea directa: 3151212223). Ficha: %s",
			v.Brand, v.Model, v.Year, v.MileageKm, v.PriceCOP, v.OfficialURL)
	case triage.IntentDocs:
		return fmt.Sprintf("¡Claro que sí! El %s %s modelo %d (placa terminada en %s) se entrega con peritaje certificado de 150 puntos, SOAT y revisión tecnomecánica al día. Precio: $%d COP. ¿A qué número de WhatsApp te enviamos el historial completo? (WhatsApp 3151212223). Ficha: %s",
			v.Brand, v.Model, v.Year, v.PlateLastChar, v.PriceCOP, v.OfficialURL)
	case triage.IntentVisit:
		return fmt.Sprintf("¡Te esperamos con los brazos abiertos! Ven a conocer y hacer prueba de ruta del %s %s modelo %d ($%d COP, %d km) en nuestra sede de Tunja frente a la Toyota. ¿A qué WhatsApp te agendamos tu cita con un asesor? (WhatsApp oficial 3151212223). Ficha: %s",
			v.Brand, v.Model, v.Year, v.PriceCOP, v.MileageKm, v.OfficialURL)
	default:
		return fmt.Sprintf("¡Hola! Sí señor(a), tenemos disponible el %s %s modelo %d ($%d COP, %d km, caja %s, motor %s, tracción %s, color %s) 100%% certificado en BuyaCAR Tunja. ¿A qué número de WhatsApp te compartimos más detalles y agendamos tu prueba de manejo? (WhatsApp oficial: 3151212223). Enlace: %s",
			v.Brand, v.Model, v.Year, v.PriceCOP, v.MileageKm, v.Transmission, v.Engine, v.Traction, v.Color, v.OfficialURL)
	}
}
