package inventory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newInventoryTLSServer(t *testing.T, handler func(ifNoneMatch string) (int, string, []Vehicle)) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		status, etag, vehicles := handler(r.Header.Get("If-None-Match"))
		if etag != "" {
			w.Header().Set("ETag", etag)
		}
		if status == http.StatusNotModified {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(vehicles)
	}))
}

func TestCache_AllSixStatuses(t *testing.T) {
	now := time.Date(2026, 10, 26, 13, 35, 0, 0, time.UTC)
	cache := NewSeededStubCache(now)
	ctx := context.Background()

	// 1. exact_match (Toyota Hilux 2022)
	res := cache.Search(ctx, Query{Brand: "Toyota", Model: "Hilux"}, now)
	if res.Status != StatusExactMatch || len(res.Vehicles) != 1 {
		t.Fatalf("expected exact_match with 1 vehicle, got %s (%d)", res.Status, len(res.Vehicles))
	}

	// 2. multiple_matches (Mazda CX-5 tiene 2021 y 2023 -> Leccion conocida #3: no adivinar)
	res = cache.Search(ctx, Query{Brand: "Mazda", Model: "CX-5"}, now)
	if res.Status != StatusMultipleMatches || len(res.Vehicles) != 2 {
		t.Fatalf("expected multiple_matches with 2 vehicles, got %s (%d)", res.Status, len(res.Vehicles))
	}

	// Desambiguacion por ano -> exact_match
	res = cache.Search(ctx, Query{Brand: "Mazda", Model: "CX-5", Year: 2023}, now)
	if res.Status != StatusExactMatch || res.Vehicles[0].PriceCOP != 142000000 {
		t.Fatalf("expected exact_match for Mazda CX-5 2023, got %+v", res)
	}

	// 3. no_match (Ferrari Testarossa)
	res = cache.Search(ctx, Query{Brand: "Ferrari", Model: "Testarossa"}, now)
	if res.Status != StatusNoMatch || len(res.Vehicles) != 0 {
		t.Fatalf("expected no_match, got %s", res.Status)
	}

	// 4. vehicle_not_required (consulta vacia de horario/ubicacion)
	res = cache.Search(ctx, Query{}, now)
	if res.Status != StatusVehicleNotRequired {
		t.Fatalf("expected vehicle_not_required, got %s", res.Status)
	}

	// 5. inventory_stale (> 30 minutos sin refrescar)
	staleTime := now.Add(31 * time.Minute)
	res = cache.Search(ctx, Query{Brand: "Toyota"}, staleTime)
	if res.Status != StatusStale {
		t.Fatalf("expected inventory_stale after 31 minutes, got %s", res.Status)
	}

	// 6. inventory_unavailable (fuente caida)
	cache.SetUnavailable()
	res = cache.Search(ctx, Query{Brand: "Toyota"}, now)
	if res.Status != StatusUnavailable {
		t.Fatalf("expected inventory_unavailable, got %s", res.Status)
	}
}

func TestHTTPFetcher_ETag304AndSSRFAllowlist(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 28, 17, 25, 0, 0, time.UTC)

	// 1. Bloqueo de esquema no-HTTPS y dominios fuera de allowlist (Anti-SSRF)
	if _, err := NewHTTPFetcher("http://buyacar.com.co/wp-json/vehica/v1/cars", []string{"buyacar.com.co"}, 5*time.Second); err == nil {
		t.Fatalf("expected HTTP scheme to be rejected")
	}
	if _, err := NewHTTPFetcher("https://evil-attacker.com/wp-json/vehica/v1/cars", []string{"buyacar.com.co"}, 5*time.Second); err == nil {
		t.Fatalf("expected untrusted domain to be rejected")
	}

	// 2. Servidor TLS local simulando el endpoint oficial con soporte ETag / 304 Not Modified
	requestsCount := 0
	var lastSeenIfNoneMatch string
	srv := newInventoryTLSServer(t, func(inm string) (int, string, []Vehicle) {
		requestsCount++
		lastSeenIfNoneMatch = inm
		if inm == `W/"etag-buyacar-v2"` {
			return 304, `W/"etag-buyacar-v2"`, nil
		}
		return 200, `W/"etag-buyacar-v2"`, DefaultFixtureVehicles()
	})
	defer srv.Close()

	fetcher, err := NewHTTPFetcher(srv.URL, []string{"127.0.0.1", "localhost", "buyacar.com.co"}, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error creating fetcher: %v", err)
	}
	fetcher.client = srv.Client()

	cache := NewCache(15, 30)

	// Primera sincronizacion: 200 OK y carga de ETag
	notMod, err := fetcher.Sync(ctx, cache, now)
	if err != nil || notMod {
		t.Fatalf("expected initial sync 200 OK (notMod=false), got notMod=%v err=%v", notMod, err)
	}
	res := cache.Search(ctx, Query{Brand: "Toyota", Model: "Hilux"}, now)
	if res.Status != StatusExactMatch || res.ETag != `W/"etag-buyacar-v2"` {
		t.Fatalf("expected exact_match with ETag W/\"etag-buyacar-v2\", got %+v", res)
	}

	// Segunda sincronizacion a los 20 minutos: envia If-None-Match y recibe 304 Not Modified
	later := now.Add(20 * time.Minute)
	notMod, err = fetcher.Sync(ctx, cache, later)
	if err != nil || !notMod {
		t.Fatalf("expected second sync 304 Not Modified (notMod=true), got notMod=%v err=%v", notMod, err)
	}
	if lastSeenIfNoneMatch != `W/"etag-buyacar-v2"` {
		t.Fatalf("expected If-None-Match header W/\"etag-buyacar-v2\", got %q", lastSeenIfNoneMatch)
	}
	if cache.NeedsRefresh(later.Add(5 * time.Minute)) {
		t.Fatalf("expected cache refresh window to be renewed after 304 Not Modified")
	}
}

func TestCache_GetVehicle_ExistingAndNonExisting(t *testing.T) {
	now := time.Date(2026, 10, 26, 13, 35, 0, 0, time.UTC)
	cache := NewSeededStubCache(now)

	// 1. Vehiculo existente (Toyota Hilux 2022)
	veh, ok := cache.GetVehicle("veh-001")
	if !ok || veh.Brand != "Toyota" || veh.Model != "Hilux" {
		t.Fatalf("expected to find veh-001 Toyota Hilux, got ok=%v veh=%+v", ok, veh)
	}

	// 2. Insensibilidad a mayusculas y espacios
	vehUpper, okUpper := cache.GetVehicle("  VEH-001  ")
	if !okUpper || vehUpper.ID != "veh-001" {
		t.Fatalf("expected case-insensitive match for VEH-001, got ok=%v veh=%+v", okUpper, vehUpper)
	}

	// 3. Vehiculo vendido en fixture (veh-004 Renault Duster)
	vehSold, okSold := cache.GetVehicle("veh-004")
	if !okSold || vehSold.Status != "sold" {
		t.Fatalf("expected to find veh-004 with status sold, got ok=%v status=%s", okSold, vehSold.Status)
	}

	// 4. Vehiculo inexistente
	_, okMissing := cache.GetVehicle("veh-nonexistent")
	if okMissing {
		t.Fatalf("expected false for nonexistent vehicle")
	}

	// 5. ID vacio
	_, okEmpty := cache.GetVehicle("")
	if okEmpty {
		t.Fatalf("expected false for empty vehicle ID")
	}
}

func TestCache_IsFresh_WindowAndUnavailable(t *testing.T) {
	now := time.Date(2026, 10, 26, 13, 35, 0, 0, time.UTC)
	cache := NewSeededStubCache(now)

	// Fresco al instante t0
	if !cache.IsFresh(now) {
		t.Fatalf("expected cache to be fresh at t0")
	}

	// Fresco a los 15 minutos (ventana de refresco)
	if !cache.IsFresh(now.Add(15 * time.Minute)) {
		t.Fatalf("expected cache to be fresh at 15m")
	}

	// Fresco a los 29 minutos
	if !cache.IsFresh(now.Add(29 * time.Minute)) {
		t.Fatalf("expected cache to be fresh at 29m")
	}

	// Caducado (> 30 minutos)
	if cache.IsFresh(now.Add(31 * time.Minute)) {
		t.Fatalf("expected cache to NOT be fresh at 31m")
	}

	// Si se marca unavailable, no es fresco aunque el tiempo sea reciente
	cache.SetUnavailable()
	if cache.IsFresh(now) {
		t.Fatalf("expected unavailable cache to return IsFresh = false")
	}
}

func TestCache_ETag(t *testing.T) {
	now := time.Date(2026, 10, 26, 13, 35, 0, 0, time.UTC)
	cache := NewSeededStubCache(now)

	etag := cache.ETag()
	if etag != `W/"buyacar-sim-etag-v1"` {
		t.Fatalf("expected ETag W/\"buyacar-sim-etag-v1\", got %q", etag)
	}
	if cache.GetETag() != etag {
		t.Fatalf("expected GetETag() to match ETag(), got %q", cache.GetETag())
	}
}
