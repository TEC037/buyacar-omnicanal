# Handoff Ciclo 9: Etapa A (Validación Nativa Go) y Preparación Etapa B (Inventario ETag + Canal Piloto de Correo en DRAFT)

- **Agente emisor**: @go-engineer / @architect / @security-qa / @kaizen-evaluator
- **Fecha**: 2026-10-28
- **Modo operativo por defecto**: `SIMULATION` (con soporte verificado para `DRAFT` en el canal piloto de correo)
- **Objetivo**: Ejecutar la validación nativa completa en el host Windows (`go1.27.0 windows/amd64`) correspondiente a la Etapa A de `instructivo.md` y construir los dos componentes desbloqueados de la Etapa B (cliente HTTP de solo lectura con `ETag`/`If-Modified-Since` y adaptador del canal piloto de correo en modo `DRAFT` con bloqueo estricto de `AUTOPILOT`).
- **Entrada**: `instructivo.md`, `.agents/prompts/BUYACAR_FINAL_INTEGRATED.md`, `production_artifacts/handoffs/cycle-8.md`.
- **Archivos creados/modificados**:
  1. `app_build/internal/inventory/http_fetcher.go`: Cliente HTTP `GET` de solo lectura hacia `https://buyacar.com.co/wp-json/vehica/v1/cars` con validación anti-SSRF (esquema `https` + allowlist de dominios en request y redirects), cabeceras condicionales `If-None-Match` / `If-Modified-Since`, soporte `304 Not Modified` y timeout de 30s (`DefaultUpstreamTimeout`).
  2. `app_build/internal/inventory/inventory.go` & `inventory_test.go`: Método `TouchRefresh` para renovar ventana de 15 min ante `304 Not Modified` y prueba `TestHTTPFetcher_ETag304AndSSRFAllowlist`.
  3. `app_build/internal/channel/email_pilot.go` & `email_pilot_test.go`: Adaptador del canal piloto de correo (`EmailPilotChannel`, Orden #6) que opera exclusivamente en lectura y borrador (`draft_recorded`), bloquea envíos autónomos (`ErrEmailAutopilotBlocked`) y pausa el canal tras 3 fallos consecutivos (`MaxConsecutiveEmailFailures`).
  4. `app_build/internal/guard/guard.go`: Límite de caracteres para el canal `email` (`2000` caracteres).
  5. `app_build/internal/core/core_test.go`: Prueba end-to-end `TestPipeline_EmailPilotDraftModeEndToEnd` verificando los 7 checkpoints y generación de borrador en modo `DRAFT`.
- **Comandos ejecutados y resultados (Etapa A + Etapa B)**:
  - `go version`: `go1.27.0 windows/amd64` (binario en `C:\Program Files\Go\bin\go.exe`).
  - `gofmt -s -w .` y `go vet ./...`: `0` advertencias ni errores.
  - `go test -v ./...`: `PASS` en todos los paquetes (`internal/ai`, `internal/channel`, `internal/core` incluyendo `TestGoldenSuite_64Cases` y `TestPipeline_EmailPilotDraftModeEndToEnd`, `internal/fast`, `internal/guard`, `internal/inventory`, `internal/obs`).
  - `go test -bench=BenchmarkFastPath -benchmem ./internal/fast`: `3211 ns/op` (`0.0032 ms`, muy por debajo del umbral de `5 ms`), `3090 B/op`, `35 allocs/op`.
  - `go test -fuzz=FuzzTriageClassify -fuzztime=5s ./internal/fast`: `PASS` (`223,695` ejecuciones sin pánicos ni fallos).
  - `go test -fuzz=FuzzGuardValidateOutbox -fuzztime=5s ./internal/guard`: `PASS` (`151,043` ejecuciones sin fugas de cifras ni URLs no autorizadas).
  - `go run ./cmd/buyacar`: `PASS` (`operating_mode=SIMULATION`, `resolved_without_ai_pct=100`, `fast_path_p99_ms=0.561`).
- **Riesgos y bloqueos**:
  - Nota del entorno Windows: `go test -race` requiere compilador C (`gcc`/MinGW) en Windows (`CGO_ENABLED=1`); todas las demás puertas nativas de Go pasaron al 100%.
  - `DefaultSafeConfig()` conserva `OperatingMode: channel.ModeSimulation` tal como exige la regla `00-safety.md` hasta orden humana explícita para conmutar la configuración activa a `DRAFT`.
- **Siguiente acción**: Con autorización humana explícita, configurar `GEMINI_API_KEY` en variables de entorno del sistema, verificar `ollama pull llama3.2:3b` en `127.0.0.1:11434` y activar `operatingMode: DRAFT` para procesar las primeras 100 conversaciones reales en borrador.
