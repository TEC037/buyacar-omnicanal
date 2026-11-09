package crm

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"
)

// FunnelStage representa la fase exacta del prospecto en el embudo autónomo de ventas.
type FunnelStage string

const (
	StageCapture       FunnelStage = "captura_t0"        // T+0s: Ingesta del lead
	StageQualified     FunnelStage = "calificado"         // Intención clara, presupuesto y vehículo
	StageAppointment   FunnelStage = "cita_peritaje"      // Agendamiento de prueba de ruta en Tunja
	StageNegotiation   FunnelStage = "negociacion_humana" // Escalamiento comercial por retoma/regateo
	StageDiscarded     FunnelStage = "descartado"         // Sin coincidencia o inviable
)

// reColombianPhone detecta números celulares de 10 dígitos colombianos que comienzan con 3.
var reColombianPhone = regexp.MustCompile(`(?:(?:\+?57)?\s*)?(3[0-5]\d{8})`)

// ExtractColombianPhone extrae el primer número celular colombiano detectado en un texto.
func ExtractColombianPhone(text string) string {
	clean := strings.ReplaceAll(text, " ", "")
	clean = strings.ReplaceAll(clean, "-", "")
	clean = strings.ReplaceAll(clean, ".", "")
	clean = strings.ReplaceAll(clean, "(", "")
	clean = strings.ReplaceAll(clean, ")", "")
	match := reColombianPhone.FindStringSubmatch(clean)
	if len(match) >= 2 {
		return match[1]
	}
	return ""
}

// LeadProfile almacena el perfil dinámico de intención y memoria contextual del cliente.
type LeadProfile struct {
	CustomerID       string      `json:"customer_id"`
	CustomerAlias    string      `json:"customer_alias"`
	ChannelID        string      `json:"channel_id"`
	Phone            string      `json:"phone,omitempty"`
	WhatsAppHandoff  bool        `json:"whatsapp_handoff"`
	IntentScore      int         `json:"intent_score"` // 0 a 100
	Stage            FunnelStage `json:"stage"`
	TargetVehicle    string      `json:"target_vehicle,omitempty"`
	BudgetCOP        int64       `json:"budget_cop,omitempty"`
	PaymentMethod    string      `json:"payment_method,omitempty"` // contado, credito, permuta
	NeedsFinancing   bool        `json:"needs_financing"`
	HasTradeIn       bool        `json:"has_trade_in"`
	AppointmentTime  *time.Time  `json:"appointment_time,omitempty"`
	AppointmentSede  string      `json:"appointment_sede,omitempty"`
	AssignedAdvisor  string      `json:"assigned_advisor,omitempty"`
	RiskLevel        string      `json:"risk_level,omitempty"` // low, medium, high
	ConsentGiven     bool        `json:"consent_given"`
	OptOut           bool        `json:"opt_out"`
	PendingData      []string    `json:"pending_data,omitempty"`
	ConfirmedData    []string    `json:"confirmed_data,omitempty"`
	InteractionCount int         `json:"interaction_count"`
	LastInteraction  time.Time   `json:"last_interaction"`
	Notes            []string    `json:"notes,omitempty"`
}

// LeadScorer calcula el puntaje de intención de compra (0 a 100) en tiempo real.
func CalculateIntentScore(text, adTitle string, priorInteractions int) (int, FunnelStage) {
	norm := strings.ToLower(text)
	norm = strings.ReplaceAll(norm, "á", "a")
	norm = strings.ReplaceAll(norm, "é", "e")
	norm = strings.ReplaceAll(norm, "í", "i")
	norm = strings.ReplaceAll(norm, "ó", "o")
	norm = strings.ReplaceAll(norm, "ú", "u")
	score := 25 // Base por contactar

	// Si el usuario proporcionó su número telefónico de contacto
	detectedPhone := ExtractColombianPhone(text)
	if detectedPhone != "" {
		score += 45 // Señal máxima de conversión: entrega de datos de contacto
	}

	if adTitle != "" {
		score += 25 // Interés en anuncio específico de inventario
	}

	// Señales de alta intención comercial
	if strings.Contains(norm, "precio") || strings.Contains(norm, "cuanto vale") || strings.Contains(norm, "disponible") {
		score += 15
	}
	if strings.Contains(norm, "credito") || strings.Contains(norm, "financi") || strings.Contains(norm, "cuota") || strings.Contains(norm, "banco") {
		score += 20
	}
	if strings.Contains(norm, "verlo") || strings.Contains(norm, "cita") || strings.Contains(norm, "visita") || strings.Contains(norm, "prueba de ruta") || strings.Contains(norm, "ir a tunja") {
		score += 35
	}
	if strings.Contains(norm, "comprar") {
		score += 40
	}

	// Ponderación por continuidad
	if priorInteractions > 1 {
		score += 10
	}

	if score > 100 {
		score = 100
	}

	// Determinar etapa en el embudo
	stage := StageCapture
	if detectedPhone != "" {
		stage = StageAppointment // Con teléfono listo para agendamiento / peritaje por WhatsApp
	} else if strings.Contains(norm, "cuanto es lo menos") || strings.Contains(norm, "rebaja") || strings.Contains(norm, "permuta") {
		stage = StageNegotiation
	} else if score >= 80 || strings.Contains(norm, "cita") || strings.Contains(norm, "prueba de ruta") {
		stage = StageAppointment
	} else if score >= 50 {
		stage = StageQualified
	}

	return score, stage
}

// MemoryLeadStore implementa persistencia en memoria y thread-safe de perfiles de clientes.
type MemoryLeadStore struct {
	mu    sync.RWMutex
	leads map[string]*LeadProfile
}

func NewMemoryLeadStore() *MemoryLeadStore {
	return &MemoryLeadStore{
		leads: make(map[string]*LeadProfile),
	}
}

func (s *MemoryLeadStore) UpsertLead(ctx context.Context, lead LeadProfile) LeadProfile {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.leads[lead.CustomerID]
	if !ok {
		lead.InteractionCount = 1
		lead.LastInteraction = time.Now().UTC()
		s.leads[lead.CustomerID] = &lead
		return lead
	}

	existing.InteractionCount++
	existing.LastInteraction = time.Now().UTC()
	if lead.IntentScore > existing.IntentScore {
		existing.IntentScore = lead.IntentScore
	}
	if lead.TargetVehicle != "" {
		existing.TargetVehicle = lead.TargetVehicle
	}
	if lead.BudgetCOP > 0 {
		existing.BudgetCOP = lead.BudgetCOP
	}
	if lead.PaymentMethod != "" {
		existing.PaymentMethod = lead.PaymentMethod
	}
	if lead.Stage != "" {
		existing.Stage = lead.Stage
	}
	if lead.AppointmentTime != nil {
		existing.AppointmentTime = lead.AppointmentTime
		existing.AppointmentSede = lead.AppointmentSede
	}
	return *existing
}

func (s *MemoryLeadStore) GetLead(ctx context.Context, customerID string) (LeadProfile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	lead, ok := s.leads[customerID]
	if !ok {
		return LeadProfile{}, false
	}
	return *lead, true
}
