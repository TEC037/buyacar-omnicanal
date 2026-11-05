package crm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"buyacar.com.co/omnicanal/internal/triage"
)

var (
	ErrTaskNotFound = errors.New("crm: task not found")
	ErrEmptyConvID  = errors.New("crm: conversation ID cannot be empty")
	ErrEmptyTaskID  = errors.New("crm: task ID cannot be empty")
)

// TaskType clasifica comercialmente una tarea generada en la atencion omnicanal.
type TaskType string

const (
	TaskScheduleTestDrive  TaskType = "schedule_test_drive"
	TaskCommercialFollowup TaskType = "commercial_followup"
	TaskCreditApplication  TaskType = "credit_application"
	TaskGeneralInquiry     TaskType = "general_inquiry"
)

// TaskStatus representa el ciclo de vida de la tarea en el CRM.
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusEscalated TaskStatus = "escalated"
	TaskStatusCancelled TaskStatus = "cancelled"
)

// Task modela una tarea o gestion comercial asentada a partir de una conversacion.
type Task struct {
	ID          string     `json:"id"`
	ConvID      string     `json:"conv_id"`
	ChannelID   string     `json:"channel_id"`
	Type        TaskType   `json:"type"`
	Status      TaskStatus `json:"status"`
	Description string     `json:"description"`
	VehicleID   string     `json:"vehicle_id,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at,omitempty"`
}

// Manager define el contrato de gestion y consulta de tareas de CRM.
type Manager interface {
	RecordTask(ctx context.Context, task Task) error
	GetTasksByConv(convID string) []Task
	GetTask(ctx context.Context, id string) (Task, error)
	ListTasks(ctx context.Context) []Task
	UpdateTaskStatus(ctx context.Context, id string, status TaskStatus) error
}

// MemoryManager implementa Manager de forma thread-safe en memoria.
type MemoryManager struct {
	mu          sync.RWMutex
	tasksByID   map[string]Task
	tasksByConv map[string][]string
}

// NewMemoryManager crea una nueva instancia de MemoryManager.
func NewMemoryManager() *MemoryManager {
	return &MemoryManager{
		tasksByID:   make(map[string]Task),
		tasksByConv: make(map[string][]string),
	}
}

// RecordTask persiste una tarea asegurando unicidad, defaults y seguridad ante concurrencia.
func (m *MemoryManager) RecordTask(ctx context.Context, task Task) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(task.ConvID) == "" {
		return ErrEmptyConvID
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if strings.TrimSpace(task.ID) == "" {
		task.ID = fmt.Sprintf("task-%s-%d", task.ConvID, time.Now().UnixNano())
	}
	if task.Status == "" {
		task.Status = TaskStatusPending
	}
	if task.Type == "" {
		task.Type = TaskGeneralInquiry
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now().UTC()
	}

	_, exists := m.tasksByID[task.ID]
	m.tasksByID[task.ID] = task

	if !exists {
		m.tasksByConv[task.ConvID] = append(m.tasksByConv[task.ConvID], task.ID)
	}

	return nil
}

// RecordWaitlistAlert registra una tarea comercial originada por una alerta de lista de espera.
func (m *MemoryManager) RecordWaitlistAlert(ctx context.Context, customerID, channelID, vehicleID, description string, createdAt time.Time) error {
	return m.RecordTask(ctx, Task{
		ConvID:      customerID,
		ChannelID:   channelID,
		Type:        TaskCommercialFollowup,
		Status:      TaskStatusPending,
		Description: description,
		VehicleID:   vehicleID,
		CreatedAt:   createdAt,
	})
}

// GetTasksByConv retorna una copia de todas las tareas asociadas a una conversacion.
func (m *MemoryManager) GetTasksByConv(convID string) []Task {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids, ok := m.tasksByConv[convID]
	if !ok || len(ids) == 0 {
		return []Task{}
	}

	res := make([]Task, 0, len(ids))
	for _, id := range ids {
		if t, found := m.tasksByID[id]; found {
			res = append(res, t)
		}
	}
	return res
}

// GetTask busca una tarea por su ID unico.
func (m *MemoryManager) GetTask(ctx context.Context, id string) (Task, error) {
	if err := ctx.Err(); err != nil {
		return Task{}, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	t, ok := m.tasksByID[id]
	if !ok {
		return Task{}, ErrTaskNotFound
	}
	return t, nil
}

// ListTasks retorna una copia de todas las tareas registradas en el gestor.
func (m *MemoryManager) ListTasks(ctx context.Context) []Task {
	if err := ctx.Err(); err != nil {
		return nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	res := make([]Task, 0, len(m.tasksByID))
	for _, t := range m.tasksByID {
		res = append(res, t)
	}
	return res
}

// UpdateTaskStatus actualiza el estado de una tarea existente.
func (m *MemoryManager) UpdateTaskStatus(ctx context.Context, id string, status TaskStatus) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.tasksByID[id]
	if !ok {
		return ErrTaskNotFound
	}

	t.Status = status
	t.UpdatedAt = time.Now().UTC()
	m.tasksByID[id] = t
	return nil
}

// NormalizeTaskType mapea cadenas arbitrarias provenientes de LLMs o usuarios a tipos canonicos de CRM.
func NormalizeTaskType(raw string) TaskType {
	clean := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(clean, "prueba") || strings.Contains(clean, "test_drive") || strings.Contains(clean, "visita"):
		return TaskScheduleTestDrive
	case strings.Contains(clean, "credito") || strings.Contains(clean, "credit") || strings.Contains(clean, "financ"):
		return TaskCreditApplication
	case strings.Contains(clean, "seguimiento") || strings.Contains(clean, "followup") || strings.Contains(clean, "comercial") || strings.Contains(clean, "asesor"):
		return TaskCommercialFollowup
	default:
		return TaskGeneralInquiry
	}
}

// InferTaskFromTriage deduce deterministamente el tipo de tarea y descripcion a partir de la intencion de Fast-Path (0 tokens).
func InferTaskFromTriage(intent triage.Intent, hasVehicle bool) (TaskType, string) {
	switch intent {
	case triage.IntentVisit:
		return TaskScheduleTestDrive, "Cliente solicita agendar visita o prueba de ruta"
	case triage.IntentCreditStudy:
		return TaskCreditApplication, "Cliente solicita información de crédito o financiación"
	case triage.IntentPrice, triage.IntentMileage, triage.IntentSpecs, triage.IntentDocs:
		if hasVehicle {
			return TaskCommercialFollowup, "Seguimiento comercial sobre vehículo consultado"
		}
		return TaskGeneralInquiry, "Consulta de inventario general"
	case triage.IntentLocation, triage.IntentHours, triage.IntentContact, triage.IntentGreeting:
		return TaskGeneralInquiry, "Atención automática de información corporativa BuyaCAR"
	default:
		return TaskGeneralInquiry, "Atención general de canal"
	}
}
