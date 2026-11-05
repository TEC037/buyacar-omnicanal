package inventory

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// MaxInvalidationBodyBytes limita el tamano maximo del payload a 256 KB segun Mandamiento 4.
	MaxInvalidationBodyBytes = 256 * 1024
)

// InvalidationEvent representa la carga util recibida desde el ERP/DMS para invalidacion en tiempo real.
type InvalidationEvent struct {
	EventID   string    `json:"event_id"`
	EventType string    `json:"event_type"` // e.g. "status_change", "price_update", "vehicle_returned"
	VehicleID string    `json:"vehicle_id"`
	Status    string    `json:"status,omitempty"` // "available", "sold", "reserved"
	PriceCOP  int64     `json:"price_cop,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// InvalidationResponse resume el resultado de la invalidacion en cache y alertas asociadas.
type InvalidationResponse struct {
	Status           string `json:"status"`
	VehicleID        string `json:"vehicle_id"`
	ETag             string `json:"etag"`
	NotifiedWaitlist int    `json:"notified_waitlist"`
	Error            string `json:"error,omitempty"`
}

// InvalidationWebhookHandler define el contrato oficial para la recepcion de webhooks de invalidacion.
type InvalidationWebhookHandler interface {
	HandleInvalidation(w http.ResponseWriter, r *http.Request)
}

// ERPInvalidationHandler implementa InvalidationWebhookHandler para procesar eventos del ERP/DMS.
type ERPInvalidationHandler struct {
	cache    *Cache
	waitlist WaitlistManager
	secret   string
}

// NewInvalidationWebhookHandler construye un manejador de webhooks con cache, waitlist y secreto HMAC.
func NewInvalidationWebhookHandler(cache *Cache, waitlist WaitlistManager, secret string) *ERPInvalidationHandler {
	return &ERPInvalidationHandler{
		cache:    cache,
		waitlist: waitlist,
		secret:   secret,
	}
}

// VerifySignature valida la firma HMAC-SHA256 del payload recibido en cabeceras oficiales.
func (h *ERPInvalidationHandler) VerifySignature(payload []byte, signature string) bool {
	if h.secret == "" {
		return true // autenticacion por secreto deshabilitada en entorno simulado
	}
	mac := hmac.New(sha256.New, []byte(h.secret))
	mac.Write(payload)
	expectedMAC := hex.EncodeToString(mac.Sum(nil))

	cleanSig := strings.TrimSpace(signature)
	cleanSig = strings.TrimPrefix(cleanSig, "sha256=")
	return hmac.Equal([]byte(cleanSig), []byte(expectedMAC))
}

// HandleInvalidation procesa la peticion HTTP POST, actualiza la cache, recalcula el ETag y dispara waitlist.
func (h *ERPInvalidationHandler) HandleInvalidation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(InvalidationResponse{
			Status: "error",
			Error:  "method not allowed: must be POST",
		})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, MaxInvalidationBodyBytes))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(InvalidationResponse{
			Status: "error",
			Error:  "failed to read request body: " + err.Error(),
		})
		return
	}
	defer r.Body.Close()

	// Validar firma HMAC si hay secreto configurado
	sig := r.Header.Get("X-BuyaCAR-Signature-256")
	if sig == "" {
		sig = r.Header.Get("X-Signature-256")
	}
	if h.secret != "" && !h.VerifySignature(body, sig) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(InvalidationResponse{
			Status: "error",
			Error:  "unauthorized: invalid or missing HMAC signature",
		})
		return
	}

	var event InvalidationEvent
	if err := json.Unmarshal(body, &event); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(InvalidationResponse{
			Status: "error",
			Error:  "invalid JSON payload: " + err.Error(),
		})
		return
	}

	cleanVID := strings.TrimSpace(event.VehicleID)
	if cleanVID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(InvalidationResponse{
			Status: "error",
			Error:  "vehicle_id is required",
		})
		return
	}

	now := time.Now().UTC()
	if !event.Timestamp.IsZero() {
		now = event.Timestamp.UTC()
	}

	var newETag string
	var updatedVehicle Vehicle
	var found bool

	if h.cache != nil {
		found, updatedVehicle, newETag = h.cache.UpdateVehicleStatus(cleanVID, event.Status, event.PriceCOP, now)
		if !found {
			// Si no existia en cache, crear vehiculo minimo y refrescar
			updatedVehicle = Vehicle{
				ID:       cleanVID,
				Status:   event.Status,
				PriceCOP: event.PriceCOP,
			}
			newETag = h.cache.UpdateVehicle(updatedVehicle, now)
		}
	}

	notifiedCount := 0
	if h.waitlist != nil && strings.EqualFold(updatedVehicle.Status, "available") {
		notified, err := h.waitlist.CheckAndNotify(updatedVehicle)
		if err == nil {
			notifiedCount = len(notified)
		}
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(InvalidationResponse{
		Status:           "invalidated",
		VehicleID:        cleanVID,
		ETag:             newETag,
		NotifiedWaitlist: notifiedCount,
	})
}
