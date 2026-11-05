package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"buyacar.com.co/omnicanal/internal/inventory"
	"buyacar.com.co/omnicanal/internal/skills"
	"buyacar.com.co/omnicanal/internal/triage"
)

func TestGeminiHTTPProvider_FallbackAndLiveStructuredOutput(t *testing.T) {
	stub := NewStubProvider("gemini-stub", StubResponse{
		RawJSON: []byte(`{"config_id":"cfg-stub","trace":{"timestamp_utc":"2026-11-04T00:00:00Z","request_id":"r1"},"proposed_reply":"Respuesta stub","intent":"vehicle_availability","confidence":0.9,"conversation_summary":"s","crm_update":{"tasks_to_create":["t1"]},"governance":{"escalate":false,"reason":null}}`),
	})

	var receivedPath string
	mockGemini := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path + "?" + r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"candidates": [{
				"content": {
					"parts": [{
						"text": "{\"config_id\":\"cfg-gemini-live\",\"trace\":{\"timestamp_utc\":\"2026-11-04T12:00:00Z\",\"request_id\":\"req-1\"},\"proposed_reply\":\"¡Hola! Para viaje familiar por Boyacá te recomiendo la Nissan Kicks Advance 2020 ($72.000.000 COP, 41391 km, caja Automática). Cuenta con garantía certificada y financiamos hasta el 80%. Escríbenos al WhatsApp 3151212223.\",\"intent\":\"vehicle_availability\",\"confidence\":0.96,\"conversation_summary\":\"Cliente consulta recomendación familiar\",\"crm_update\":{\"tasks_to_create\":[\"agendar_prueba_ruta_kicks\"]},\"governance\":{\"escalate\":false,\"reason\":null}}"
					}]
				}
			}],
			"usageMetadata": {
				"promptTokenCount": 180,
				"candidatesTokenCount": 65
			}
		}`))
	}))
	defer mockGemini.Close()

	prov := NewGeminiHTTPProvider(GeminiHTTPConfig{
		APIKey:   "",
		Model:    "gemini-2.5-flash",
		BaseURL:  mockGemini.URL,
		Fallback: stub,
	})

	compact := skills.BuildCompactContext(triage.IntentAvailability, "¿Qué camioneta familiar automática me recomiendas?", []inventory.Vehicle{
		{
			ID:           "BYC-001",
			Brand:        "Nissan",
			Model:        "Kicks",
			Year:         2020,
			PriceCOP:     72000000,
			MileageKm:    41391,
			Transmission: "Automática",
			Status:       "available",
			OfficialURL:  "https://buyacar.com.co/listado/nissan-kicks-advance-2020-2",
		},
	})

	// 1. Sin API Key -> usa fallback determinista sin red
	rawStub, _, _, err := prov.GenerateJSON(context.Background(), compact)
	if err != nil {
		t.Fatalf("expected fallback to succeed without API key, got: %v", err)
	}
	if !strings.Contains(string(rawStub), "Respuesta stub") {
		t.Fatalf("expected stub response, got: %s", string(rawStub))
	}

	// 2. Activar GEMINI_API_KEY en caliente
	prov.SetAPIKey("AIzaSyTestKeyBuyaCAR12345", "gemini-2.5-flash")
	if !prov.HasAPIKey() {
		t.Fatalf("expected HasAPIKey() == true after SetAPIKey")
	}

	rawLive, inTok, outTok, err := prov.GenerateJSON(context.Background(), compact)
	if err != nil {
		t.Fatalf("expected live Gemini HTTP call to succeed, got: %v", err)
	}
	if !strings.Contains(receivedPath, "/models/gemini-2.5-flash:generateContent?key=AIzaSyTestKeyBuyaCAR12345") {
		t.Fatalf("unexpected request path: %s", receivedPath)
	}
	if inTok != 180 || outTok != 65 {
		t.Fatalf("expected tokens 180/65, got %d/%d", inTok, outTok)
	}

	parsed, err := ParseStrictJSON(rawLive)
	if err != nil {
		t.Fatalf("expected ParseStrictJSON to pass on normalized Gemini output, got: %v", err)
	}
	// Verificar que "$72.000.000" fue normalizado a "$72000000" para compatibilidad con Guardia CP5
	if !strings.Contains(parsed.ProposedReply, "72000000") {
		t.Fatalf("expected normalized integer price 72000000 in ProposedReply, got: %s", parsed.ProposedReply)
	}
}
