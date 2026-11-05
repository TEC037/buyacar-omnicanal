package inventory

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func computeTestHMAC(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestInvalidationWebhook_Success_And_WaitlistTrigger(t *testing.T) {
	secret := "secret-erp-webhook-key-2026"
	recorder := &mockTaskRecorder{}
	waitlist := NewWaitlistManager(recorder)
	cache := NewCache(15, 30)

	// Inicializar cache con un vehiculo reservado
	now := time.Now().UTC()
	cache.LoadSnapshot([]Vehicle{
		{
			ID:          "renault-duster-2021",
			Brand:       "Renault",
			Model:       "Duster",
			Year:        2021,
			PriceCOP:    68000000,
			Status:      "reserved",
			OfficialURL: "https://buyacar.com.co/vehiculos/duster-2021",
		},
	}, "etag-v1", now)

	// Registrar reserva en waitlist para ese vehiculo
	_ = waitlist.Register(WaitlistEntry{
		VehicleID:  "renault-duster-2021",
		CustomerID: "cust-waitlist-99",
		ChannelID:  "whatsapp",
		CreatedAt:  now,
	})

	handler := NewInvalidationWebhookHandler(cache, waitlist, secret)

	event := InvalidationEvent{
		EventID:   "evt-101",
		EventType: "status_change",
		VehicleID: "renault-duster-2021",
		Status:    "available",
		PriceCOP:  67000000,
		Timestamp: now,
	}
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed marshalling event: %v", err)
	}

	sig := computeTestHMAC(secret, body)

	req := httptest.NewRequest(http.MethodPost, "/webhook/inventory/invalidation", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-BuyaCAR-Signature-256", sig)

	rec := httptest.NewRecorder()
	handler.HandleInvalidation(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp InvalidationResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed decoding response: %v", err)
	}

	if resp.Status != "invalidated" || resp.VehicleID != "renault-duster-2021" {
		t.Fatalf("expected status invalidated, got %+v", resp)
	}
	if resp.NotifiedWaitlist != 1 {
		t.Fatalf("expected 1 waitlist notification, got %d", resp.NotifiedWaitlist)
	}
	if resp.ETag == "" || resp.ETag == "etag-v1" {
		t.Fatalf("expected newly recalculated ETag, got %q", resp.ETag)
	}

	// Verificar vehiculo actualizado en cache
	v, ok := cache.GetVehicle("renault-duster-2021")
	if !ok || v.Status != "available" || v.PriceCOP != 67000000 {
		t.Fatalf("expected status available and price 67M in cache, got %+v", v)
	}

	// Verificar alerta registrada en TaskRecorder
	alerts := recorder.GetAlerts()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert recorded, got %d", len(alerts))
	}
}

func TestInvalidationWebhook_InvalidSignature(t *testing.T) {
	secret := "correct-secret"
	handler := NewInvalidationWebhookHandler(NewCache(15, 30), nil, secret)

	body := []byte(`{"event_id":"1","vehicle_id":"veh-1","status":"available"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/inventory/invalidation", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-BuyaCAR-Signature-256", "bad-signature")

	rec := httptest.NewRecorder()
	handler.HandleInvalidation(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for bad HMAC, got %d", rec.Code)
	}
}

func TestInvalidationWebhook_MethodNotAllowed(t *testing.T) {
	handler := NewInvalidationWebhookHandler(NewCache(15, 30), nil, "")

	req := httptest.NewRequest(http.MethodGet, "/webhook/inventory/invalidation", nil)
	rec := httptest.NewRecorder()
	handler.HandleInvalidation(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed, got %d", rec.Code)
	}
}

func TestInvalidationWebhook_MissingVehicleID(t *testing.T) {
	handler := NewInvalidationWebhookHandler(NewCache(15, 30), nil, "")

	body := []byte(`{"event_id":"1","status":"available"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/inventory/invalidation", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.HandleInvalidation(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
	}
}
