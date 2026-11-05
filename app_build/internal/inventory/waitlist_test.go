package inventory

import (
	"context"
	"sync"
	"testing"
	"time"
)

type mockRecordedAlert struct {
	CustomerID  string
	ChannelID   string
	VehicleID   string
	Description string
	CreatedAt   time.Time
}

type mockTaskRecorder struct {
	mu     sync.Mutex
	alerts []mockRecordedAlert
}

func (m *mockTaskRecorder) RecordWaitlistAlert(ctx context.Context, customerID, channelID, vehicleID, description string, createdAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alerts = append(m.alerts, mockRecordedAlert{
		CustomerID:  customerID,
		ChannelID:   channelID,
		VehicleID:   vehicleID,
		Description: description,
		CreatedAt:   createdAt,
	})
	return nil
}

func (m *mockTaskRecorder) GetAlerts() []mockRecordedAlert {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]mockRecordedAlert, len(m.alerts))
	copy(out, m.alerts)
	return out
}

func TestWaitlistManager_RegisterAndSeal(t *testing.T) {
	recorder := &mockTaskRecorder{}
	wm := NewWaitlistManager(recorder)

	entry := WaitlistEntry{
		VehicleID:  "toyota-hilux-2022",
		CustomerID: "cust-101",
		ChannelID:  "whatsapp",
	}

	if err := wm.Register(entry); err != nil {
		t.Fatalf("unexpected error registering entry: %v", err)
	}

	entries := wm.GetEntries("toyota-hilux-2022")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	registered := entries[0]
	if registered.SealedHash == "" || len(registered.SealedHash) != 64 {
		t.Fatalf("expected 64-char SHA256 sealed hash, got %q", registered.SealedHash)
	}
	if registered.Notified {
		t.Fatalf("expected unnotified entry upon register")
	}
	if wm.PendingCount() != 1 {
		t.Fatalf("expected pending count 1, got %d", wm.PendingCount())
	}
}

func TestWaitlistManager_CheckAndNotify_AvailableReturn(t *testing.T) {
	recorder := &mockTaskRecorder{}
	wm := NewWaitlistManager(recorder)

	now := time.Now().UTC()
	_ = wm.Register(WaitlistEntry{
		VehicleID:  "mazda-cx5-2023",
		CustomerID: "cust-client-alpha",
		ChannelID:  "instagram",
		CreatedAt:  now,
	})

	// 1. Vehiculo pasa a "reserved" -> NO debe notificar
	reservedVeh := Vehicle{
		ID:     "mazda-cx5-2023",
		Brand:  "Mazda",
		Model:  "CX-5",
		Year:   2023,
		Status: "reserved",
	}
	notified, err := wm.CheckAndNotify(reservedVeh)
	if err != nil {
		t.Fatalf("unexpected error on reserved check: %v", err)
	}
	if len(notified) != 0 {
		t.Fatalf("expected 0 notified when vehicle is reserved, got %d", len(notified))
	}

	// 2. Vehiculo regresa a "available" -> DEBE notificar y crear alerta via TaskRecorder
	availableVeh := Vehicle{
		ID:     "mazda-cx5-2023",
		Brand:  "Mazda",
		Model:  "CX-5",
		Year:   2023,
		Status: "available",
	}
	notified, err = wm.CheckAndNotify(availableVeh)
	if err != nil {
		t.Fatalf("unexpected error on available check: %v", err)
	}
	if len(notified) != 1 {
		t.Fatalf("expected 1 notified entry, got %d", len(notified))
	}
	if !notified[0].Notified {
		t.Fatalf("expected notified flag = true")
	}

	// Verificar alerta registrada en TaskRecorder
	alerts := recorder.GetAlerts()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert recorded for client alpha, got %d", len(alerts))
	}
	if alerts[0].VehicleID != "mazda-cx5-2023" {
		t.Fatalf("expected VehicleID mazda-cx5-2023 in recorded alert, got %s", alerts[0].VehicleID)
	}

	// 3. Segunda llamada no debe volver a notificar (idempotente)
	notifiedSecond, err := wm.CheckAndNotify(availableVeh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(notifiedSecond) != 0 {
		t.Fatalf("expected 0 notifications on second check, got %d", len(notifiedSecond))
	}
	if wm.PendingCount() != 0 {
		t.Fatalf("expected 0 pending, got %d", wm.PendingCount())
	}
}
