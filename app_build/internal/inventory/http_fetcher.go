package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// OfficialInventoryEndpoint es el unico endpoint oficial autorizado de lectura de inventario.
	OfficialInventoryEndpoint = "https://buyacar.com.co/wp-json/vehica/v1/cars"
	// DefaultUpstreamTimeout define el timeout maximo de 30s exigido por instructivo.md.
	DefaultUpstreamTimeout = 30 * time.Second
	// MaxInventoryResponseBytes limita el payload de lectura para prevenir ataques de agotamiento de memoria.
	MaxInventoryResponseBytes = 5 * 1024 * 1024
)

var (
	ErrSchemeNotHTTPS       = errors.New("inventory: endpoint scheme must be https")
	ErrHostNotInAllowlist   = errors.New("inventory: endpoint host is not in allowlist")
	ErrUpstreamStatusFailed = errors.New("inventory: upstream returned unexpected status")
)

// HTTPFetcher realiza lecturas HTTP exclusivamente mediante GET con soporte ETag / If-Modified-Since
// y validacion estricta de allowlist HTTPS (anti-SSRF).
type HTTPFetcher struct {
	mu             sync.RWMutex
	endpoint       string
	allowedDomains map[string]bool
	client         *http.Client
	lastETag       string
	lastModified   string
}

// NewHTTPFetcher construye un cliente HTTP de solo lectura validando el esquema HTTPS y la allowlist.
func NewHTTPFetcher(endpoint string, allowedDomains []string, timeout time.Duration) (*HTTPFetcher, error) {
	if strings.TrimSpace(endpoint) == "" {
		endpoint = OfficialInventoryEndpoint
	}
	if timeout <= 0 {
		timeout = DefaultUpstreamTimeout
	}

	domains := make(map[string]bool, len(allowedDomains))
	for _, d := range allowedDomains {
		clean := strings.ToLower(strings.TrimSpace(d))
		if clean != "" {
			domains[clean] = true
		}
	}
	if len(domains) == 0 {
		domains["buyacar.com.co"] = true
	}

	if err := validateEndpointURL(endpoint, domains); err != nil {
		return nil, err
	}

	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("inventory: too many redirects")
			}
			return validateEndpointURL(req.URL.String(), domains)
		},
	}

	return &HTTPFetcher{
		endpoint:       endpoint,
		allowedDomains: domains,
		client:         client,
	}, nil
}

func validateEndpointURL(rawURL string, allowedDomains map[string]bool) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("inventory: invalid endpoint url: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return ErrSchemeNotHTTPS
	}
	host := strings.ToLower(u.Hostname())
	if !allowedDomains[host] {
		return fmt.Errorf("%w: %s", ErrHostNotInAllowlist, host)
	}
	return nil
}

// Sync ejecuta una solicitud GET condicional (If-None-Match / If-Modified-Since) y actualiza la Cache.
// Retorna (notModified=true, nil) cuando el servidor responde 304 Not Modified (ahorro de ancho de banda y CPU).
// Ante fallo de red o status no exitoso, marca la cache como no disponible para evitar adivinaciones.
func (f *HTTPFetcher) Sync(ctx context.Context, cache *Cache, now time.Time) (bool, error) {
	if cache == nil {
		return false, errors.New("inventory: nil cache")
	}
	if err := validateEndpointURL(f.endpoint, f.allowedDomains); err != nil {
		cache.SetUnavailable()
		return false, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.endpoint, nil)
	if err != nil {
		cache.SetUnavailable()
		return false, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "BuyaCAR-Omnicanal-ReadOnly/1.0")

	f.mu.RLock()
	etag := f.lastETag
	lastMod := f.lastModified
	f.mu.RUnlock()

	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastMod != "" {
		req.Header.Set("If-Modified-Since", lastMod)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		cache.SetUnavailable()
		return false, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotModified:
		// 304 Not Modified: renueva la marca temporal de frescura conservando el snapshot y ETag existentes.
		cache.TouchRefresh(now)
		return true, nil

	case http.StatusOK:
		rawBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, MaxInventoryResponseBytes))
		if readErr != nil {
			cache.SetUnavailable()
			return false, fmt.Errorf("inventory: failed reading payload: %w", readErr)
		}
		vehicles, err := decodeInventoryPayload(rawBytes)
		if err != nil {
			cache.SetUnavailable()
			return false, fmt.Errorf("inventory: invalid json payload: %w", err)
		}

		// Validar que toda URL oficial de vehiculo cumpla la allowlist HTTPS
		for i := range vehicles {
			if vehicles[i].OfficialURL != "" {
				if err := validateEndpointURL(vehicles[i].OfficialURL, f.allowedDomains); err != nil {
					cache.SetUnavailable()
					return false, fmt.Errorf("inventory: untrusted vehicle url %q: %w", vehicles[i].OfficialURL, err)
				}
			}
		}

		newETag := strings.TrimSpace(resp.Header.Get("ETag"))
		newLastMod := strings.TrimSpace(resp.Header.Get("Last-Modified"))

		f.mu.Lock()
		if newETag != "" {
			f.lastETag = newETag
		}
		if newLastMod != "" {
			f.lastModified = newLastMod
		}
		effectiveETag := f.lastETag
		f.mu.Unlock()

		cache.LoadSnapshot(vehicles, effectiveETag, now)
		return false, nil

	default:
		cache.SetUnavailable()
		return false, fmt.Errorf("%w: %d", ErrUpstreamStatusFailed, resp.StatusCode)
	}
}

// decodeInventoryPayload soporta tanto arreglos JSON planos ([]Vehicle) como el esquema real de WordPress Vehica ({"resultsCount":...,"results":[...]}) de buyacar.com.co.
func decodeInventoryPayload(raw []byte) ([]Vehicle, error) {
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		var vehicles []Vehicle
		if err := json.Unmarshal(raw, &vehicles); err != nil {
			return nil, err
		}
		return vehicles, nil
	}

	var env struct {
		Results []struct {
			ID         int    `json:"id"`
			Name       string `json:"name"`
			URL        string `json:"url"`
			Attributes []struct {
				ID           int             `json:"id"`
				Name         string          `json:"name"`
				Type         string          `json:"type"`
				Value        json.RawMessage `json:"value"`
				DisplayValue json.RawMessage `json:"displayValue"`
			} `json:"attributes"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	if len(env.Results) == 0 {
		return nil, errors.New("empty vehica results")
	}

	var out []Vehicle
	for _, r := range env.Results {
		v := Vehicle{
			ID:          fmt.Sprintf("vehica-%d", r.ID),
			Status:      "available",
			OfficialURL: strings.TrimRight(r.URL, "/"),
			SOATValid:   true,
			TecnoValid:  true,
		}
		var ccStr, fuelStr string
		for _, a := range r.Attributes {
			switch a.ID {
			case 6659: // Marca
				v.Brand = extractFirstTermName(a.Value)
			case 23940: // Línea - Referencia
				var line string
				_ = json.Unmarshal(a.Value, &line)
				v.Model = normalizeVehicaModel(line)
			case 14696: // Modelo (Año)
				var yr int
				if err := json.Unmarshal(a.Value, &yr); err == nil {
					v.Year = yr
				}
			case 6664: // Recorrido (Km)
				var km int
				if err := json.Unmarshal(a.Value, &km); err == nil {
					v.MileageKm = km
				}
			case 6662: // Tipo de caja
				v.Transmission = extractFirstTermName(a.Value)
			case 23817: // Tracción
				v.Traction = extractFirstTermName(a.Value)
			case 6666: // Color
				v.Color = extractFirstTermName(a.Value)
			case 6655: // Carrocería
				v.BodyType = extractFirstTermName(a.Value)
			case 23662: // Cilindraje
				_ = json.Unmarshal(a.DisplayValue, &ccStr)
			case 6663: // Combustible
				fuelStr = extractFirstTermName(a.Value)
			case 30606: // Placa
				var pl string
				_ = json.Unmarshal(a.Value, &pl)
				pl = strings.TrimLeft(strings.TrimSpace(pl), "-")
				v.PlateLastChar = pl
			case 24854: // Precio
				var priceMap map[string]int64
				if err := json.Unmarshal(a.Value, &priceMap); err == nil {
					for _, p := range priceMap {
						if p > 0 {
							v.PriceCOP = p
						}
					}
				}
			}
		}
		v.Engine = strings.TrimSpace(ccStr + " " + fuelStr)
		if v.Brand != "" && v.Model != "" && v.PriceCOP > 0 {
			out = append(out, v)
		}
	}
	return out, nil
}

func extractFirstTermName(raw json.RawMessage) string {
	var arr []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &arr); err == nil && len(arr) > 0 {
		return strings.TrimSpace(arr[0].Name)
	}
	var single struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &single); err == nil {
		return strings.TrimSpace(single.Name)
	}
	return ""
}

func normalizeVehicaModel(line string) string {
	upper := strings.ToUpper(strings.TrimSpace(line))
	switch {
	case strings.Contains(upper, "KICKS"):
		return "Kicks"
	case strings.Contains(upper, "X-TRAIL") || strings.Contains(upper, "XTRAIL"):
		return "X-Trail"
	case strings.Contains(upper, "QASHQAI"):
		return "Qashqai"
	case strings.Contains(upper, "TIGUAN"):
		return "Tiguan"
	case strings.Contains(upper, "SPORTAGE"):
		return "Sportage"
	case strings.Contains(upper, "PICANTO"):
		return "Picanto"
	case strings.Contains(upper, "HILUX"):
		return "Hilux"
	case strings.Contains(upper, "FORTUNER"):
		return "Fortuner"
	case strings.Contains(upper, "ESCAPE"):
		return "Escape"
	case strings.Contains(upper, "FOCUS"):
		return "Focus"
	case strings.Contains(upper, "EDGE"):
		return "Edge"
	case strings.Contains(upper, "FIESTA"):
		return "Fiesta"
	case strings.Contains(upper, "CX-5") || strings.Contains(upper, "CX5"):
		return "CX-5"
	case strings.Contains(upper, "CX-7") || strings.Contains(upper, "CX7"):
		return "CX-7"
	case strings.HasPrefix(upper, "3 ") || upper == "3":
		return "3"
	case strings.Contains(upper, "SPARK"):
		return "Spark"
	case strings.Contains(upper, "OPTRA"):
		return "Optra"
	case strings.Contains(upper, "CAPTIVA"):
		return "Captiva"
	case strings.Contains(upper, "TRACKER"):
		return "Tracker"
	case strings.Contains(upper, "SONIC"):
		return "Sonic"
	case strings.Contains(upper, "CAPTUR"):
		return "Captur"
	case strings.Contains(upper, "SANTAFE") || strings.Contains(upper, "SANTA FE"):
		return "Santafe"
	case strings.Contains(upper, "ACCENT"):
		return "Accent"
	case strings.Contains(upper, "CRV") || strings.Contains(upper, "CR-V"):
		return "CR-V"
	case strings.Contains(upper, "207"):
		return "207"
	case strings.Contains(upper, "B180") || strings.Contains(upper, "B 180"):
		return "B180"
	case strings.Contains(upper, "C 200") || strings.Contains(upper, "C200"):
		return "C200"
	case strings.Contains(upper, "A3"):
		return "A3"
	case strings.Contains(upper, "YORI"):
		return "Yori"
	case strings.Contains(upper, "FELICIA"):
		return "Felicia"
	default:
		parts := strings.Fields(line)
		if len(parts) > 0 {
			return parts[0]
		}
		return line
	}
}
