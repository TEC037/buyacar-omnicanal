package inventory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Status representa los 6 estados permitidos de inventario segun especificacion canonica.
type Status string

const (
	StatusExactMatch         Status = "exact_match"
	StatusMultipleMatches    Status = "multiple_matches"
	StatusNoMatch            Status = "no_match"
	StatusUnavailable        Status = "inventory_unavailable"
	StatusStale              Status = "inventory_stale"
	StatusVehicleNotRequired Status = "vehicle_not_required"
)

// Vehicle representa un vehiculo certificado del inventario oficial de BuyaCAR.
type Vehicle struct {
	ID            string
	Brand         string
	Model         string
	Year          int
	PriceCOP      int64
	MileageKm     int
	Transmission  string
	Engine        string
	Traction      string
	Color         string
	BodyType      string
	PlateLastChar string
	SOATValid     bool
	TecnoValid    bool
	Status        string // "available", "sold", "reserved"
	OfficialURL   string
}

// Query define los criterios de busqueda o prefiltrado de inventario.
type Query struct {
	Brand        string
	Model        string
	Year         int
	MaxBudgetCOP int64
	Transmission string
	Traction     string
}

// MatchResult encapsula el estado determinista y los vehiculos encontrados.
type MatchResult struct {
	Status   Status
	Vehicles []Vehicle
	ETag     string
	Updated  time.Time
}

// Cache gestiona el indice en memoria con control de frescura (15m refresh / 30m stale).
type Cache struct {
	mu             sync.RWMutex
	vehicles       []Vehicle
	byID           map[string]Vehicle
	etag           string
	lastUpdated    time.Time
	refreshWindow  time.Duration
	staleThreshold time.Duration
	available      bool
}

// NewCache inicializa la cache de inventario con ventanas de 15m y 30m por defecto.
func NewCache(refreshMinutes, staleMinutes int) *Cache {
	if refreshMinutes <= 0 {
		refreshMinutes = 15
	}
	if staleMinutes <= 0 {
		staleMinutes = 30
	}
	return &Cache{
		refreshWindow:  time.Duration(refreshMinutes) * time.Minute,
		staleThreshold: time.Duration(staleMinutes) * time.Minute,
	}
}

// LoadSnapshot actualiza la cache en memoria con ETag y timestamp.
func (c *Cache) LoadSnapshot(vehicles []Vehicle, etag string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	seen := make(map[string]int)
	var deduplicated []Vehicle
	for _, v := range vehicles {
		key := strings.ToLower(strings.TrimSpace(v.ID))
		if idx, exists := seen[key]; exists {
			if deduplicated[idx].Status == "available" && v.Status != "available" {
				deduplicated[idx] = v
			}
		} else {
			seen[key] = len(deduplicated)
			deduplicated = append(deduplicated, v)
		}
	}
	c.vehicles = deduplicated
	c.byID = make(map[string]Vehicle, len(deduplicated))
	for _, v := range deduplicated {
		c.byID[strings.ToLower(strings.TrimSpace(v.ID))] = v
	}
	c.etag = etag
	c.lastUpdated = now
	c.available = true
}

// CheckVehicleAvailability verifica disponibilidad, frescura y estado de un vehiculo en una sola operacion atomica bajo cerrojo.
func (c *Cache) CheckVehicleAvailability(id string, now time.Time) (bool, string) {
	if c == nil {
		return false, "inventory_unavailable"
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.available || c.lastUpdated.IsZero() {
		return false, "inventory_unavailable"
	}
	threshold := c.staleThreshold
	if threshold <= 0 {
		threshold = 30 * time.Minute
	}
	if now.Sub(c.lastUpdated) > threshold {
		return false, "inventory_stale"
	}
	if id == "" {
		return true, ""
	}
	trimmed := strings.ToLower(strings.TrimSpace(id))
	var v Vehicle
	var found bool
	if c.byID != nil {
		v, found = c.byID[trimmed]
	} else {
		for _, item := range c.vehicles {
			if strings.EqualFold(item.ID, trimmed) {
				v = item
				found = true
				break
			}
		}
	}
	if !found {
		return false, "vehicle_not_found"
	}
	if v.Status != "available" {
		return false, "vehicle_sold_or_reserved"
	}
	return true, ""
}

// GetVehicle recupera de forma thread-safe un vehiculo por su identificador unico (ID).
// Realiza busqueda insensible a mayusculas/minusculas y omite espacios en blanco.
// Retorna el vehiculo y true si existe; de lo contrario retorna Vehicle{} vacio y false.
func (c *Cache) GetVehicle(id string) (Vehicle, bool) {
	if c == nil {
		return Vehicle{}, false
	}
	trimmed := strings.ToLower(strings.TrimSpace(id))
	if trimmed == "" {
		return Vehicle{}, false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.byID != nil {
		v, ok := c.byID[trimmed]
		return v, ok
	}

	for _, v := range c.vehicles {
		if strings.EqualFold(v.ID, trimmed) {
			return v, true
		}
	}
	return Vehicle{}, false
}

// IsAvailable indica si la cache esta disponible y cargada de forma thread-safe.
func (c *Cache) IsAvailable() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.available && !c.lastUpdated.IsZero()
}

// IsFresh verifica de forma thread-safe si la cache de inventario esta disponible y si la
// ultima actualizacion no ha superado el umbral de caducidad (30 minutos por defecto).
// Retorna false si la cache no esta disponible, si nunca ha sido cargada, o si now - lastUpdated > staleThreshold.
func (c *Cache) IsFresh(now time.Time) bool {
	if c == nil {
		return false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.available || c.lastUpdated.IsZero() {
		return false
	}

	threshold := c.staleThreshold
	if threshold <= 0 {
		threshold = 30 * time.Minute
	}

	diff := now.Sub(c.lastUpdated)
	if diff < 0 {
		return true
	}

	return diff <= threshold
}

// ETag retorna el identificador de version ETag actual de la cache de forma thread-safe.
func (c *Cache) ETag() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.etag
}

// GetETag es un alias ergonomico de ETag para compatibilidad entre convenciones de acceso.
func (c *Cache) GetETag() string {
	return c.ETag()
}

// SetUnavailable marca la fuente como caida para forzar escalamiento seguro sin adivinar.
func (c *Cache) SetUnavailable() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.available = false
}

// TouchRefresh renueva el timestamp de actualizacion cuando el servidor responde 304 Not Modified.
func (c *Cache) TouchRefresh(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.vehicles) > 0 {
		c.lastUpdated = now
		c.available = true
	}
}

// NeedsRefresh indica si supero la ventana de refresco (15 min).
func (c *Cache) NeedsRefresh(now time.Time) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.available || c.lastUpdated.IsZero() {
		return true
	}
	return now.Sub(c.lastUpdated) >= c.refreshWindow
}

// recalculateETagLocked recalcula el ETag basado en el contenido y estado de los vehiculos en cache.
func (c *Cache) recalculateETagLocked() string {
	h := sha256.New()
	for _, v := range c.vehicles {
		fmt.Fprintf(h, "%s:%s:%d;", v.ID, v.Status, v.PriceCOP)
	}
	fmt.Fprintf(h, "%d", c.lastUpdated.UnixNano())
	return fmt.Sprintf("W/\"buyacar-etag-%s\"", hex.EncodeToString(h.Sum(nil)[:8]))
}

// UpdateVehicle actualiza o inserta un vehiculo en la cache, renueva la marca temporal y recalcula el ETag.
func (c *Cache) UpdateVehicle(v Vehicle, now time.Time) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	trimmed := strings.ToLower(strings.TrimSpace(v.ID))
	if c.byID == nil {
		c.byID = make(map[string]Vehicle)
	}
	c.byID[trimmed] = v

	found := false
	for i, item := range c.vehicles {
		if strings.EqualFold(item.ID, trimmed) {
			c.vehicles[i] = v
			found = true
			break
		}
	}
	if !found {
		c.vehicles = append(c.vehicles, v)
	}

	c.lastUpdated = now
	c.available = true
	c.etag = c.recalculateETagLocked()
	return c.etag
}

// UpdateVehicleStatus actualiza el estado y opcionalmente el precio de un vehiculo existente en tiempo real,
// recalculando el ETag para invalidacion inmediata en downstream.
func (c *Cache) UpdateVehicleStatus(id string, status string, newPriceCOP int64, now time.Time) (bool, Vehicle, string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	trimmed := strings.ToLower(strings.TrimSpace(id))
	v, ok := c.byID[trimmed]
	if !ok {
		return false, Vehicle{}, c.etag
	}

	if status != "" {
		v.Status = status
	}
	if newPriceCOP > 0 {
		v.PriceCOP = newPriceCOP
	}

	c.byID[trimmed] = v
	for i, item := range c.vehicles {
		if strings.EqualFold(item.ID, trimmed) {
			c.vehicles[i] = v
			break
		}
	}

	c.lastUpdated = now
	c.etag = c.recalculateETagLocked()
	return true, v, c.etag
}

// Search ejecuta la busqueda determinista sin adivinar jamas ante ambiguedad o caducidad.
func (c *Cache) Search(ctx context.Context, q Query, now time.Time) MatchResult {
	if err := ctx.Err(); err != nil {
		return MatchResult{Status: StatusUnavailable}
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.available || c.lastUpdated.IsZero() {
		return MatchResult{Status: StatusUnavailable}
	}

	if now.Sub(c.lastUpdated) > c.staleThreshold {
		return MatchResult{
			Status:  StatusStale,
			ETag:    c.etag,
			Updated: c.lastUpdated,
		}
	}

	// Si la consulta no solicita ningun filtro de vehiculo
	if q.Brand == "" && q.Model == "" && q.Year == 0 && q.MaxBudgetCOP == 0 && q.Transmission == "" && q.Traction == "" {
		return MatchResult{
			Status:  StatusVehicleNotRequired,
			ETag:    c.etag,
			Updated: c.lastUpdated,
		}
	}

	matches := make([]Vehicle, 0, 4)
	for _, v := range c.vehicles {
		if q.Brand != "" && !strings.EqualFold(v.Brand, q.Brand) {
			continue
		}
		if q.Model != "" && !strings.EqualFold(v.Model, q.Model) {
			continue
		}
		if q.Year != 0 && v.Year != q.Year {
			continue
		}
		if q.MaxBudgetCOP > 0 && v.PriceCOP > q.MaxBudgetCOP {
			continue
		}
		if q.Transmission != "" && !strings.EqualFold(v.Transmission, q.Transmission) {
			continue
		}
		if q.Traction != "" && !strings.EqualFold(v.Traction, q.Traction) {
			continue
		}
		matches = append(matches, v)
	}

	switch len(matches) {
	case 0:
		return MatchResult{
			Status:  StatusNoMatch,
			ETag:    c.etag,
			Updated: c.lastUpdated,
		}
	case 1:
		return MatchResult{
			Status:   StatusExactMatch,
			Vehicles: matches,
			ETag:     c.etag,
			Updated:  c.lastUpdated,
		}
	default:
		return MatchResult{
			Status:   StatusMultipleMatches,
			Vehicles: matches,
			ETag:     c.etag,
			Updated:  c.lastUpdated,
		}
	}
}
