package inventory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrEmptyVehicleID  = errors.New("waitlist: vehicle ID cannot be empty")
	ErrEmptyCustomerID = errors.New("waitlist: customer ID cannot be empty")
)

// TaskRecorder desacopla la generacion de alertas comerciales para el CRM sin crear ciclos de importacion.
type TaskRecorder interface {
	RecordWaitlistAlert(ctx context.Context, customerID, channelID, vehicleID, description string, createdAt time.Time) error
}

// WaitlistEntry modela la reserva o intencion de compra de un cliente sobre un vehiculo no disponible.
type WaitlistEntry struct {
	VehicleID  string    `json:"vehicle_id"`
	CustomerID string    `json:"customer_id"`
	ChannelID  string    `json:"channel_id"`
	SealedHash string    `json:"sealed_hash"`
	CreatedAt  time.Time `json:"created_at"`
	Notified   bool      `json:"notified"`
	NotifiedAt time.Time `json:"notified_at,omitempty"`
}

// ComputeWaitlistSeal genera el sellado criptografico SHA-256 inmutable de la entrada en lista de espera.
func ComputeWaitlistSeal(vehicleID, customerID, channelID string, createdAt time.Time) string {
	h := sha256.New()
	cleanVID := strings.ToLower(strings.TrimSpace(vehicleID))
	cleanCID := strings.ToLower(strings.TrimSpace(customerID))
	cleanCh := strings.ToLower(strings.TrimSpace(channelID))
	timestamp := createdAt.UTC().Format(time.RFC3339Nano)
	h.Write([]byte(cleanVID + "|" + cleanCID + "|" + cleanCh + "|" + timestamp))
	return hex.EncodeToString(h.Sum(nil))
}

// WaitlistManager define el contrato para registro y generacion de alertas ante retorno de disponibilidad.
type WaitlistManager interface {
	Register(entry WaitlistEntry) error
	CheckAndNotify(vehicle Vehicle) ([]WaitlistEntry, error)
	GetEntries(vehicleID string) []WaitlistEntry
	PendingCount() int
}

// MemoryWaitlistManager implementa WaitlistManager de forma thread-safe en memoria.
type MemoryWaitlistManager struct {
	mu           sync.RWMutex
	taskRecorder TaskRecorder
	entries      map[string][]WaitlistEntry // indexado por vehicle_id en minusculas
}

// NewWaitlistManager construye una nueva instancia de MemoryWaitlistManager.
func NewWaitlistManager(recorder TaskRecorder) *MemoryWaitlistManager {
	return &MemoryWaitlistManager{
		taskRecorder: recorder,
		entries:      make(map[string][]WaitlistEntry),
	}
}

// Register almacena una reserva aplicando sellado SHA-256 si no fue provisto.
func (wm *MemoryWaitlistManager) Register(entry WaitlistEntry) error {
	vID := strings.ToLower(strings.TrimSpace(entry.VehicleID))
	cID := strings.TrimSpace(entry.CustomerID)
	if vID == "" {
		return ErrEmptyVehicleID
	}
	if cID == "" {
		return ErrEmptyCustomerID
	}

	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	if entry.ChannelID == "" {
		entry.ChannelID = "unknown"
	}
	if entry.SealedHash == "" {
		entry.SealedHash = ComputeWaitlistSeal(vID, cID, entry.ChannelID, entry.CreatedAt)
	}
	entry.VehicleID = vID
	entry.CustomerID = cID

	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.entries[vID] = append(wm.entries[vID], entry)
	return nil
}

// CheckAndNotify evalua si el vehiculo regreso al estado "available". De ser asi,
// notifica a los clientes en espera, asienta tareas prioritarias via TaskRecorder y retorna las entradas notificadas.
func (wm *MemoryWaitlistManager) CheckAndNotify(vehicle Vehicle) ([]WaitlistEntry, error) {
	if !strings.EqualFold(vehicle.Status, "available") {
		return nil, nil
	}

	vID := strings.ToLower(strings.TrimSpace(vehicle.ID))
	if vID == "" {
		return nil, ErrEmptyVehicleID
	}

	wm.mu.Lock()
	defer wm.mu.Unlock()

	items, exists := wm.entries[vID]
	if !exists || len(items) == 0 {
		return nil, nil
	}

	now := time.Now().UTC()
	var notified []WaitlistEntry

	for i := range items {
		if !items[i].Notified {
			items[i].Notified = true
			items[i].NotifiedAt = now

			// Registrar alerta prioritaria si hay grabador registrado
			if wm.taskRecorder != nil {
				desc := fmt.Sprintf(
					"PRIORITY WAITLIST ALERT: Vehículo %s %s (%d) vuelve a estar disponible para cliente %s en canal %s [Hash: %s]",
					vehicle.Brand, vehicle.Model, vehicle.Year, items[i].CustomerID, items[i].ChannelID, items[i].SealedHash,
				)
				_ = wm.taskRecorder.RecordWaitlistAlert(
					context.Background(),
					items[i].CustomerID,
					items[i].ChannelID,
					vehicle.ID,
					desc,
					now,
				)
			}

			notified = append(notified, items[i])
		}
	}

	wm.entries[vID] = items
	return notified, nil
}

// GetEntries retorna una copia de todas las reservas registradas para un vehiculo.
func (wm *MemoryWaitlistManager) GetEntries(vehicleID string) []WaitlistEntry {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	vID := strings.ToLower(strings.TrimSpace(vehicleID))
	items, exists := wm.entries[vID]
	if !exists {
		return []WaitlistEntry{}
	}

	out := make([]WaitlistEntry, len(items))
	copy(out, items)
	return out
}

// PendingCount cuenta la cantidad total de reservas activas pendientes de notificacion.
func (wm *MemoryWaitlistManager) PendingCount() int {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	count := 0
	for _, list := range wm.entries {
		for _, e := range list {
			if !e.Notified {
				count++
			}
		}
	}
	return count
}
