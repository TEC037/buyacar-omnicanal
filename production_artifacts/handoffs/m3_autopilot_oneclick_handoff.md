# Handoff — Catálogo Real BuyaCAR & Consola Web 1-Clic para AUTOPILOT

- **Objetivo**: Cargar el inventario real de BuyaCAR (`buyacar.com.co/wp-json/vehica/v1/cars`), dirección y horarios oficiales de Tunja, y automatizar en la consola web (`http://127.0.0.1:8080`) el encendido y prueba de `AUTOPILOT` sin requerir variables de entorno ni comandos complejos por parte del usuario.
- **Entrada**: `autopilot_marketplace_plan.md`, API pública WordPress Vehica (`https://buyacar.com.co/wp-json/vehica/v1/cars`), `app_build/cmd/buyacar/main.go`, `app_build/internal/server/dashboard_handler.go`.
- **Cambios**:
  1. Inventario real (23 vehículos de BuyaCAR Tunja + 7 casos dorados), decodificador JSON WordPress Vehica en `app_build/internal/inventory/http_fetcher.go`, dirección (`Av. Oriental - Cra. 6 # 52-23, frente a la Toyota, Tunja`) y teléfonos (`3151212223`, `3022866969`, `3160502717`) en `fixtures/rag/`.
  2. Endpoints en `app_build/internal/server/dashboard_handler.go`: `POST /api/mode`, `POST /api/webhook/simulate`, `POST /api/meta/config`, `POST /api/meta/sandbox/me/messages`.
  3. Controles visuales de 1 clic en `app_build/internal/server/assets/index.html` y lanzador Windows `iniciar_buyacar.bat`.
  4. Proveedor HTTP nativo `GeminiHTTPProvider` (`app_build/internal/ai/gemini_http.go`), recarga en caliente de `GEMINI_API_KEY` (`POST /api/gemini/config`), puente `POST /api/n8n/webhook` y workflows importables `n8n/buyacar_standalone_gemini_workflow.json` y `n8n/buyacar_bridge_workflow.json`.
- **Pruebas**: `go test -count=1 ./...` — 100% PASS (`TestRunCLI_Phase5_N8NBridgeAndHotReloadGeminiAPIKey`, `TestRunCLI_WebConsole_OneClickAutopilotAndSignedWebhookSimulation`, `TestGeminiHTTPProvider_FallbackAndLiveStructuredOutput`).
- **Riesgos**: El arranque por defecto se mantiene en `SIMULATION` cumpliendo `00-safety.md`; el usuario activa `AUTOPILOT` explícitamente con el botón de la consola.
- **Decisiones**: Si `META_ACCESS_TOKEN` está vacío, `metaAdapter` apunta automáticamente al receptor loopback local (`/api/meta/sandbox/me/messages`) para probar el ciclo completo `SendVerified` sin credenciales externas, y conmuta a `https://graph.facebook.com/v21.0` apenas el usuario guarda su token en el formulario web.
- **Siguiente acción**: Operación guiada por el usuario desde `http://127.0.0.1:8080`.
