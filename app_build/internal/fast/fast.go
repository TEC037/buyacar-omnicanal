package fast

import (
	"context"
	"fmt"
	"strings"
	"time"

	"buyacar.com.co/omnicanal/internal/inventory"
	"buyacar.com.co/omnicanal/internal/triage"
)

// BusinessTexts contiene la informacion oficial verificada de BuyaCAR en Tunja.
type BusinessTexts struct {
	AddressReply       string
	HoursReply         string
	ContactReply       string
	GreetingReply      string
	SoldVehicleNotice  string
	CreditGeneralReply string
	CategorySUVReply   string
	UniversalFallback  string
	AfterSalesReply    string
	PartsReply         string
	PaymentsReply      string
	PIIOtherReply      string
	PromptInjectReply  string
}

func DefaultBusinessTexts() BusinessTexts {
	return BusinessTexts{
		AddressReply: "¡Hola! Te esperamos en BuyaCAR en Tunja:\n\n- Dirección: Av. Oriental - Cra. 6 #52-23 (frente a la Toyota)\n- Ciudad: Tunja, Boyacá\n\nTe invitamos a comunicarte por WhatsApp al 315 121 2223 para agendar tu visita.",
		HoursReply:   "¡Hola! Nuestro horario de atención en Tunja (Cra. 6 #52-23, frente a la Toyota) es:\n\n- Lunes a Viernes: 8:00 AM a 6:00 PM (jornada continua)\n- Sábados: 8:00 AM a 2:00 PM\n- Domingos descansamos\n\nTe invitamos a comunicarte por WhatsApp al 315 121 2223.",
		ContactReply: "¡Claro que sí! Te invitamos a comunicarte por WhatsApp al 315 121 2223. Nuestras líneas oficiales en Tunja son:\n\n- WhatsApp Comercial: 315 121 2223\n- Atención Vitrina: 3022866969 / 3160502717\n\n¿Sobre qué vehículo te gustaría recibir asesoría?",
		GreetingReply: "¡Hola! Qué gusto saludarte.\nBienvenido a BuyaCAR en Tunja · Vehículos certificados.\n\nTe invitamos a comunicarte por WhatsApp al 315 121 2223.\n\n¿Estás buscando algún vehículo en especial, o prefieres que te comparta opciones disponibles?",
		SoldVehicleNotice: "Te informamos que este vehículo ya fue vendido/separado en nuestra vitrina de Tunja.\n\nUn asesor humano te compartirá alternativas similares certificadas. Te invitamos a comunicarte por WhatsApp al 315 121 2223.",
		CreditGeneralReply: "En BuyaCAR en Tunja te ayudamos con financiación vehicular:\n\n- Convenios con bancos aliados (Bancolombia, Davivienda, Finandina)\n- Planes de cuota inicial desde el 10%-20% y simulación indicativa según perfil\n\nTe invitamos a comunicarte por WhatsApp al 315 121 2223.",
		CategorySUVReply: "¡Hola! En BuyaCAR en Tunja contamos con camionetas y SUVs certificadas:\n\n- Toyota Hilux 4x4 y Fortuner\n- Mazda CX-5 y Mazda CX-30\n- Chevrolet Tracker y Captiva\n- Renault Duster y Nissan Qashqai\n\nTe invitamos a comunicarte por WhatsApp al 315 121 2223 para enviarte fotos y precios.",
		UniversalFallback: "¡Hola! En BuyaCAR en Tunja estamos para servirte. Para orientarte mejor, ¿deseas información sobre:\n\n- Compra de vehículos certificados\n- Financiación y créditos\n- Retoma de tu usado\n- Garantías y talleres autorizados\n- Hablar con un asesor comercial?\n\nTe invitamos a comunicarte por WhatsApp al 315 121 2223.",
		AfterSalesReply: "¡Hola! Para servicios de posventa, mantenimiento y garantías en BuyaCAR:\n\n- Red de talleres autorizados en Tunja, Duitama y Sogamoso\n- Agendamiento técnico directo para clientes certificados\n\nTe invitamos a comunicarte por WhatsApp al 315 121 2223 para coordinar tu cita de taller.",
		PartsReply: "¡Hola! En BuyaCAR en Tunja somos un concesionario dedicado exclusivamente a la compra, venta, retoma y financiación de vehículos usados certificados. No comercializamos repuestos ni autopartes.\n\nSi deseas asesoría para comprar o vender un vehículo, con gusto te invitamos a comunicarte por WhatsApp al 315 121 2223.",
		PaymentsReply: "¡Hola! Por tu seguridad, en BuyaCAR SAS BIC únicamente recibimos pagos en nuestras cuentas bancarias empresariales oficiales o directamente en vitrina (Av. Oriental - Cra. 6 #52-23, Tunja).\n\nTe invitamos a comunicarte por WhatsApp al 315 121 2223 para validar cualquier consignación.",
		PIIOtherReply: "¡Hola! Por políticas de protección de datos personales (Ley 1581), en BuyaCAR no compartimos información privada de propietarios anteriores. Toda la tradición legal y peritaje de 150 puntos se verifica directamente en nuestra vitrina en Tunja.\n\nTe invitamos a comunicarte por WhatsApp al 315 121 2223.",
		PromptInjectReply: "¡Hola! En BuyaCAR en Tunja nos especializamos exclusivamente en asesoría y venta de vehículos certificados.\n\n¿Te interesa conocer nuestro catálogo de camionetas y automóviles disponibles, o coordinar una prueba de ruta? Te invitamos a comunicarte por WhatsApp al 315 121 2223.",
	}
}

// Result representa la salida determinista del motor Fast-Path (0 tokens de IA).
type Result struct {
	Handled          bool
	Reply            string
	MustEscalate     bool
	EscalationReason string
	InventoryStatus  inventory.Status
}

// Engine resuelve consultas frecuentes y de inventario exacto sin invocar modelos de IA.
type Engine struct {
	texts               BusinessTexts
	confidenceThreshold float64
}

func NewEngine(texts BusinessTexts, confidenceThreshold float64) *Engine {
	if confidenceThreshold <= 0 {
		confidenceThreshold = 0.80
	}
	return &Engine{
		texts:               texts,
		confidenceThreshold: confidenceThreshold,
	}
}

// Evaluate intenta resolver en sub-milisegundo con reglas deterministas.
func (e *Engine) Evaluate(ctx context.Context, cl triage.Classification, cache *inventory.Cache, now time.Time) Result {
	// 1. Si el triage ya marco escalamiento obligatorio o baja confianza
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

	// 2. Consultas corporativas sin requerir vehiculo
	switch cl.Intent {
	case triage.IntentLocation:
		return Result{
			Handled:         true,
			Reply:           e.texts.AddressReply,
			InventoryStatus: inventory.StatusVehicleNotRequired,
		}
	case triage.IntentHours:
		return Result{
			Handled:         true,
			Reply:           e.texts.HoursReply,
			InventoryStatus: inventory.StatusVehicleNotRequired,
		}
	case triage.IntentContact:
		if cl.ExtractedQuery.Brand == "" && cl.ExtractedQuery.Model == "" {
			return Result{
				Handled:         true,
				Reply:           e.texts.ContactReply,
				InventoryStatus: inventory.StatusVehicleNotRequired,
			}
		}
	case triage.IntentGreeting:
		if triage.IsCourtesyOnly(cl.NormalizedText) {
			return Result{
				Handled:         true,
				Reply:           "¡Con mucho gusto! En BuyaCAR en Tunja (Av. Oriental - Cra. 6 #52-23, frente a la Toyota) estamos para servirte.\n\nTe invitamos a comunicarte por WhatsApp al 315 121 2223. ¡Que tengas un excelente día!",
				InventoryStatus: inventory.StatusVehicleNotRequired,
			}
		}
		return Result{
			Handled:         true,
			Reply:           e.texts.GreetingReply,
			InventoryStatus: inventory.StatusVehicleNotRequired,
		}
	case triage.IntentVisit:
		if cl.ExtractedQuery.Brand == "" && cl.ExtractedQuery.Model == "" {
			return Result{
				Handled:         true,
				Reply:           "¡Perfecto! Hemos registrado tu solicitud. Te invitamos a comunicarte por WhatsApp al 315 121 2223 para coordinar tu cita de inmediato.\n\n- Vitrina: Av. Oriental - Cra. 6 #52-23 (frente a la Toyota), Tunja",
				InventoryStatus: inventory.StatusVehicleNotRequired,
			}
		}
	case triage.IntentCreditGeneral:
		return Result{
			Handled:         true,
			Reply:           e.texts.CreditGeneralReply,
			InventoryStatus: inventory.StatusVehicleNotRequired,
		}
	case triage.IntentCategoryQuery:
		return Result{
			Handled:         true,
			Reply:           e.formatCategoryReply(cl.NormalizedText),
			InventoryStatus: inventory.StatusVehicleNotRequired,
		}
	case triage.IntentAfterSales:
		return Result{
			Handled:         true,
			Reply:           e.texts.AfterSalesReply,
			InventoryStatus: inventory.StatusVehicleNotRequired,
		}
	case triage.IntentParts:
		return Result{
			Handled:         true,
			Reply:           e.texts.PartsReply,
			InventoryStatus: inventory.StatusVehicleNotRequired,
		}
	case triage.IntentPayments:
		return Result{
			Handled:         true,
			Reply:           e.texts.PaymentsReply,
			InventoryStatus: inventory.StatusVehicleNotRequired,
		}
	case triage.IntentSensitivePII:
		return Result{
			Handled:         true,
			Reply:           e.texts.PIIOtherReply,
			InventoryStatus: inventory.StatusVehicleNotRequired,
		}
	case triage.IntentUniversal:
		return Result{
			Handled:         true,
			Reply:           e.texts.UniversalFallback,
			InventoryStatus: inventory.StatusVehicleNotRequired,
		}
	}

	// 3. Consultas de inventario: validar estado de cache y coincidencias
	if cache == nil {
		return Result{
			Handled:          false,
			MustEscalate:     true,
			EscalationReason: "inventory_unavailable",
			InventoryStatus:  inventory.StatusUnavailable,
		}
	}

	match := cache.Search(ctx, cl.ExtractedQuery, now)
	switch match.Status {
	case inventory.StatusUnavailable, inventory.StatusStale:
		return Result{
			Handled:          false,
			MustEscalate:     true,
			EscalationReason: string(match.Status),
			InventoryStatus:  match.Status,
		}
	case inventory.StatusNoMatch:
		return Result{
			Handled:          false,
			MustEscalate:     true,
			EscalationReason: "vehicle_not_found_in_inventory",
			InventoryStatus:  inventory.StatusNoMatch,
		}
	case inventory.StatusMultipleMatches:
		var avail []inventory.Vehicle
		for _, v := range match.Vehicles {
			if v.Status == "available" {
				avail = append(avail, v)
			}
		}
		// Si solo 1 unidad del modelo sigue disponible (ej. Duster 2021 disponible vs 2020 vendida), responder por la unidad disponible
		if len(avail) == 1 {
			return Result{
				Handled:         true,
				Reply:           formatVehicleReply(cl.Intent, avail[0]),
				InventoryStatus: inventory.StatusExactMatch,
			}
		}
		// Si el usuario pregunta por una marca general sin especificar modelo (ej. "¿Qué Mazda tienen?"), listar las opciones disponibles de esa marca
		if cl.ExtractedQuery.Brand != "" && cl.ExtractedQuery.Model == "" && len(avail) > 1 {
			return Result{
				Handled:         true,
				Reply:           formatBrandSummaryReply(cl.ExtractedQuery.Brand, avail),
				InventoryStatus: inventory.StatusMultipleMatches,
			}
		}
		// Leccion conocida #3: jamas elegir al azar cuando hay multiples anos de un mismo modelo (ej. dos Mazda CX-5)
		return Result{
			Handled:          false,
			MustEscalate:     true,
			EscalationReason: "multiple_matches_disambiguation_required",
			InventoryStatus:  inventory.StatusMultipleMatches,
		}
	case inventory.StatusExactMatch:
		v := match.Vehicles[0]
		// Leccion conocida #4: si el vehiculo esta sold o reserved, informar y escalar a humano
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
	default:
		return Result{Handled: false}
	}
}

func (e *Engine) formatCategoryReply(norm string) string {
	switch {
	case strings.Contains(norm, "7 pasajeros") || strings.Contains(norm, "siete pasajeros") || strings.Contains(norm, "7 puestos") || strings.Contains(norm, "siete puestos"):
		return "¡Hola! En BuyaCAR en Tunja tenemos estas camionetas de 7 pasajeros certificadas:\n\n- Toyota Fortuner 2024 (Diésel, 7 puestos)\n- Nissan X-Trail 2019 (4x4, 7 puestos)\n\nTe invitamos a comunicarte por WhatsApp al 315 121 2223 para fotos y detalles."
	case strings.Contains(norm, "4x4") || strings.Contains(norm, "doble traccion") || strings.Contains(norm, "platon") || strings.Contains(norm, "pickup") || strings.Contains(norm, "pick-up") || strings.Contains(norm, "diesel"):
		return "¡Hola! En BuyaCAR en Tunja tenemos estas opciones 4x4 y Diésel certificadas:\n\n- Toyota Hilux 2022 (4x4 Turbo Diésel)\n- Nissan Qashqai 2026 (4x4 Turbo)\n- Nissan X-Trail 2019 (4x4)\n- Ford Escape 2017 (4x4 EcoBoost)\n\nTe invitamos a comunicarte por WhatsApp al 315 121 2223."
	case strings.Contains(norm, "automovil") || strings.Contains(norm, "sedan") || strings.Contains(norm, "hatchback") || strings.Contains(norm, "pequeno") || strings.Contains(norm, "economico") || strings.Contains(norm, "barato") || strings.Contains(norm, "millones") || strings.Contains(norm, "presupuesto"):
		return "¡Hola! En BuyaCAR en Tunja tenemos excelentes automóviles y compactos certificados:\n\n- Chevrolet Spark 2012 y Peugeot 207 2011\n- Chevrolet Sonic 2013 y Ford Fiesta 2011\n- Kia Picanto 2022 y Renault Logan 2022\n- Mazda3 2021 y Renault Stepway 2020\n\nTe invitamos a comunicarte por WhatsApp al 315 121 2223."
	case strings.Contains(norm, "mecanico") || strings.Contains(norm, "mecanica"):
		return "¡Hola! En BuyaCAR en Tunja contamos con estas unidades de caja mecánica certificadas:\n\n- Renault Duster 2021 y Nissan Kicks 2020\n- Renault Stepway 2020 y Renault Logan 2022\n- Kia Picanto 2022 y Hyundai Accent 2015\n\nTe invitamos a comunicarte por WhatsApp al 315 121 2223."
	default:
		return e.texts.CategorySUVReply
	}
}

func formatBrandSummaryReply(brand string, vehicles []inventory.Vehicle) string {
	var b strings.Builder
	fmt.Fprintf(&b, "¡Hola! En BuyaCAR en Tunja tenemos estas unidades %s certificadas disponibles:\n\n", brand)
	maxItems := 4
	if len(vehicles) < maxItems {
		maxItems = len(vehicles)
	}
	for i := 0; i < maxItems; i++ {
		v := vehicles[i]
		fmt.Fprintf(&b, "- %s %d: $%d COP (%s)\n", v.Model, v.Year, v.PriceCOP, v.Transmission)
	}
	b.WriteString("\nTe invitamos a comunicarte por WhatsApp al 315 121 2223.")
	return b.String()
}

func formatVehicleTitle(v inventory.Vehicle) string {
	switch {
	case strings.EqualFold(v.Model, "Mazda3"):
		return "Mazda 3"
	case strings.EqualFold(v.Model, "Mazda2"):
		return "Mazda 2"
	case strings.EqualFold(v.Model, "Mazda6"):
		return "Mazda 6"
	}
	if v.Brand != "" && strings.HasPrefix(strings.ToLower(v.Model), strings.ToLower(v.Brand)) {
		return v.Model
	}
	return strings.TrimSpace(v.Brand + " " + v.Model)
}

// BuildEscalationHandoffReply construye una respuesta comercial inmediata y coherente cuando una consulta
// requiere escalamiento humano o desambiguacion (ej. multiples anos de un modelo o referencia agotada),
// garantizando que el cliente nunca quede en silencio mientras el caso se escala al asesor.
func BuildEscalationHandoffReply(ctx context.Context, reason string, cl triage.Classification, cache *inventory.Cache, now time.Time) (string, []inventory.Vehicle) {
	switch reason {
	case "prompt_injection_detected":
		return "", nil

	case "fraud_or_odometer_tampering_request":
		return "¡Hola! En BuyaCAR en Tunja garantizamos total transparencia y legalidad: el kilometraje de todos nuestros vehículos es original, verificado y certificado mediante peritaje de 150 puntos. No realizamos ni aceptamos alteraciones de odómetros.\n\nTe invitamos a comunicarte por WhatsApp al 315 121 2223 para revisar el historial verificado del vehículo.", nil

	case "fraud_or_plate_cloning_request":
		return "¡Hola! En BuyaCAR en Tunja cumplimos estrictamente la normativa de tránsito y el Código Penal colombiano. Todos nuestros vehículos cuentan con identificación y placas originales verificadas ante el RUNT. No prestamos ni alteramos placas vehiculares.\n\nTe invitamos a comunicarte por WhatsApp al 315 121 2223.", nil

	case "compliance_aml_sarlaft_alert":
		return "¡Hola! En BuyaCAR SAS BIC aplicamos estricto cumplimiento del sistema SAGRILAFT y normatividad DIAN / UIAF: toda transacción vehicular exige origen legal de fondos, identificación del comprador y facturación electrónica oficial en vitrina.\n\nTe invitamos a comunicarte por WhatsApp al 315 121 2223.", nil

	case "bank_account_tampering_or_impersonation":
		return DefaultBusinessTexts().PaymentsReply, nil

	case "sensitive_pii_other_requested":
		return DefaultBusinessTexts().PIIOtherReply, nil

	case "universal_fallback_needed":
		return DefaultBusinessTexts().UniversalFallback, nil

	case "multiple_matches_disambiguation_required":
		if cache != nil {
			match := cache.Search(ctx, cl.ExtractedQuery, now)
			var avail []inventory.Vehicle
			for _, v := range match.Vehicles {
				if v.Status == "available" {
					avail = append(avail, v)
				}
			}
			if len(avail) >= 2 {
				var b strings.Builder
				title := formatVehicleTitle(avail[0])
				fmt.Fprintf(&b, "¡Hola! En BuyaCAR en Tunja tenemos %d unidades de %s certificadas disponibles:\n\n", len(avail), title)
				maxItems := 3
				if len(avail) < maxItems {
					maxItems = len(avail)
				}
				for i := 0; i < maxItems; i++ {
					v := avail[i]
					fmt.Fprintf(&b, "- Modelo %d: $%d COP (%d km, %s)\n", v.Year, v.PriceCOP, v.MileageKm, v.Transmission)
				}
				b.WriteString("\n¿Cuál te interesa? Te invitamos a comunicarte por WhatsApp al 315 121 2223.")
				return b.String(), avail[:maxItems]
			}
		}

	case "vehicle_not_found_in_inventory":
		reqTitle := formatVehicleTitle(inventory.Vehicle{
			Brand: cl.ExtractedQuery.Brand,
			Model: cl.ExtractedQuery.Model,
		})
		if cl.ExtractedQuery.Year > 0 {
			reqTitle = strings.TrimSpace(fmt.Sprintf("%s %d", reqTitle, cl.ExtractedQuery.Year))
		}
		if cl.ExtractedQuery.Brand != "" && cache != nil {
			brandMatch := cache.Search(ctx, inventory.Query{Brand: cl.ExtractedQuery.Brand}, now)
			var avail []inventory.Vehicle
			for _, v := range brandMatch.Vehicles {
				if v.Status == "available" {
					avail = append(avail, v)
				}
			}
			if len(avail) > 0 {
				var b strings.Builder
				fmt.Fprintf(&b, "¡Hola! Por el momento no tenemos %s en stock en Tunja, pero contamos con estas opciones %s certificadas:\n\n", reqTitle, cl.ExtractedQuery.Brand)
				maxItems := 3
				if len(avail) < maxItems {
					maxItems = len(avail)
				}
				for i := 0; i < maxItems; i++ {
					v := avail[i]
					fmt.Fprintf(&b, "- %s %d: $%d COP (%s)\n", formatVehicleTitle(v), v.Year, v.PriceCOP, v.Transmission)
				}
				b.WriteString("\nTe invitamos a comunicarte por WhatsApp al 315 121 2223.")
				return b.String(), avail[:maxItems]
			}
		}
		if reqTitle == "" {
			reqTitle = "esa referencia"
		}
		return fmt.Sprintf("¡Hola! Por el momento no tenemos %s disponible en nuestra vitrina de Tunja.\n\n- Te invitamos a comunicarte por WhatsApp al 315 121 2223 para compartirte opciones similares certificadas disponibles hoy.", reqTitle), nil

	case "trade_in_appraisal_required":
		return "¡Hola! Claro que sí, en BuyaCAR en Tunja recibimos tu vehículo usado en parte de pago (sujeto a peritaje técnico en nuestra vitrina: Av. Oriental - Cra. 6 #52-23, frente a la Toyota).\n\nUn asesor comercial ya recibió tu solicitud. Te invitamos a comunicarte por WhatsApp al 315 121 2223.", nil

	case "discount_or_min_price_request":
		return "¡Hola! Hemos asignado tu consulta a un asesor comercial de BuyaCAR en Tunja para revisar personalmente tu propuesta sobre el precio.\n\n- Vitrina: Av. Oriental - Cra. 6 #52-23 (frente a la Toyota)\n- WhatsApp directo: 315 121 2223\n\nTe invitamos a comunicarte por WhatsApp al 315 121 2223.", nil

	case "credit_study_or_risk_central":
		return "¡Hola! Hemos asignado tu consulta a nuestro especialista de financiación en BuyaCAR en Tunja para revisar tu caso con nuestros bancos aliados.\n\n- Vitrina: Av. Oriental - Cra. 6 #52-23 (frente a la Toyota)\n- WhatsApp directo: 315 121 2223\n\nTe invitamos a comunicarte por WhatsApp al 315 121 2223.", nil

	case "legal_claim_or_fasecolda":
		return "¡Hola! En BuyaCAR en Tunja atendemos cualquier reclamación con máxima seriedad y respaldo legal. Todos nuestros vehículos cuentan con peritaje de 150 puntos y tradición jurídica verificada.\n\nHemos asignado de inmediato tu caso a la gerencia y asesoría legal. Te invitamos a comunicarte por WhatsApp al 315 121 2223.", nil
	}

	return "¡Hola! Bienvenido a BuyaCAR en Tunja · Vehículos certificados.\n\nUn asesor comercial te atenderá en breve. Te invitamos a comunicarte por WhatsApp al 315 121 2223.", nil
}

func formatVehicleReply(intent triage.Intent, v inventory.Vehicle) string {
	title := formatVehicleTitle(v)
	switch intent {
	case triage.IntentPrice:
		return fmt.Sprintf("¡Hola! Con mucho gusto:\n\n- %s modelo %d\n- Precio: $%d COP\n- Kilometraje: %d km (%s)\n\nRecibimos tu usado en permuta y financiamos.\nTe invitamos a comunicarte por WhatsApp al 315 121 2223.",
			title, v.Year, v.PriceCOP, v.MileageKm, v.Transmission)
	case triage.IntentMileage:
		return fmt.Sprintf("¡Hola! Con todo gusto:\n\n- %s modelo %d\n- Kilometraje: %d km verificados\n- Precio: $%d COP\n\n¿Te gustaría agendar prueba de ruta en Tunja?\nTe invitamos a comunicarte por WhatsApp al 315 121 2223.",
			title, v.Year, v.MileageKm, v.PriceCOP)
	case triage.IntentDocs:
		return fmt.Sprintf("¡Claro que sí! Datos del vehículo:\n\n- %s modelo %d (placa terminada en %s)\n- SOAT y tecnomecánica al día\n- Precio: $%d COP\n\nTe invitamos a comunicarte por WhatsApp al 315 121 2223 para enviarte documentos.",
			title, v.Year, v.PlateLastChar, v.PriceCOP)
	case triage.IntentVisit:
		return fmt.Sprintf("¡Te esperamos con gusto en Tunja para prueba de ruta!\n\n- %s modelo %d ($%d COP, %d km)\n- Vitrina: Av. Oriental - Cra. 6 #52-23 (frente a Toyota)\n\nTe invitamos a comunicarte por WhatsApp al 315 121 2223 para agendar tu cita.",
			title, v.Year, v.PriceCOP, v.MileageKm)
	case triage.IntentContact:
		return fmt.Sprintf("¡Hola! Sí señor(a), unidad disponible en Tunja:\n\n- %s modelo %d: $%d COP (%d km, %s %s)\n- Vitrina: Av. Oriental - Cra. 6 #52-23 (frente a Toyota)\n\nTe invitamos a comunicarte por WhatsApp o llamarnos al 315 121 2223.",
			title, v.Year, v.PriceCOP, v.MileageKm, v.Transmission, v.Traction)
	default:
		return fmt.Sprintf("¡Hola! Sí señor(a), unidad disponible en Tunja:\n\n- %s modelo %d\n- Precio: $%d COP (%d km)\n- Transmisión: %s | Motor: %s | Tracción: %s | Color: %s\n\nTe invitamos a comunicarte por WhatsApp al 315 121 2223.",
			title, v.Year, v.PriceCOP, v.MileageKm, v.Transmission, v.Engine, v.Traction, v.Color)
	}
}
