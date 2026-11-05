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
	SoldVehicleNotice string
}

// DefaultBusinessTexts retorna los textos verificados de BuyaCAR en Tunja con voz comercial colombiana.
func DefaultBusinessTexts() BusinessTexts {
	return BusinessTexts{
		AddressReply:      "Estamos ubicados en Tunja, Boyacá: Av. Oriental - Cra. 6 #52-23, frente a la Toyota. ¡Nuestros vehículos certificados están listos para prueba de ruta!",
		HoursReply:        "Nuestro horario de atención en Tunja es de Lunes a Viernes de 8:00 AM - 6:00 PM y Sábados de 8:00 AM - 2:00 PM. Te brindamos asesoría personalizada.",
		ContactReply:      "Nuestras líneas oficiales de BuyaCAR son: WhatsApp exclusivo 3151212223 y teléfonos 3022866969 / 3160502717. ¡Confianza sobre ruedas!",
		GreetingReply:     "¡Hola! Bienvenido a BuyaCAR en Tunja, confianza sobre ruedas. Contamos con vehículos certificados, manejamos todos los bancos y recibimos tu vehículo como parte de pago (permuta). ¿En qué te podemos asesorar hoy?",
		SoldVehicleNotice: "Te contamos que el vehículo consultado ya fue vendido/separado, pero un asesor te contactará para ofrecerte opciones similares de nuestro inventario certificado.",
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
		return fmt.Sprintf("El %s %s modelo %d certificado tiene un precio de $%d COP (%d km, caja %s). Manejamos todos los bancos, listos para prueba de ruta en Tunja (WhatsApp 3151212223). Más detalles en: %s",
			v.Brand, v.Model, v.Year, v.PriceCOP, v.MileageKm, v.Transmission, v.OfficialURL)
	case triage.IntentMileage:
		return fmt.Sprintf("El %s %s modelo %d cuenta con %d km verificados y precio de $%d COP. Agenda tu cita por WhatsApp 3151212223. Detalles oficiales: %s",
			v.Brand, v.Model, v.Year, v.MileageKm, v.PriceCOP, v.OfficialURL)
	case triage.IntentDocs:
		return fmt.Sprintf("El %s %s %d (placa terminada en %s) se entrega con SOAT y tecnomecánica al día. Precio: $%d COP. WhatsApp 3151212223. Ficha oficial: %s",
			v.Brand, v.Model, v.Year, v.PlateLastChar, v.PriceCOP, v.OfficialURL)
	case triage.IntentVisit:
		return fmt.Sprintf("Puedes ver y hacer prueba de ruta del %s %s %d ($%d COP, %d km) en nuestra vitrina de Tunja. Escríbenos al WhatsApp 3151212223. Ficha: %s",
			v.Brand, v.Model, v.Year, v.PriceCOP, v.MileageKm, v.OfficialURL)
	default:
		return fmt.Sprintf("Sí tenemos disponible el %s %s modelo %d ($%d COP, %d km, %s, motor %s, %s, color %s) en BuyaCAR Tunja (WhatsApp 3151212223). Enlace: %s",
			v.Brand, v.Model, v.Year, v.PriceCOP, v.MileageKm, v.Transmission, v.Engine, v.Traction, v.Color, v.OfficialURL)
	}
}
