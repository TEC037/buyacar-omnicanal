# Guía de Operación y Arquitectura del MVP — BuyaCAR Omnicanal

- **Versión**: MVP 1.1 (Fases 0 a 8 Completadas + Etapa B `DRAFT` Activa)
- **Modo Operativo Activo**: `DRAFT` (Bajo orden humana explícita; `DefaultSafeConfig()` conserva `SIMULATION` para suites de regresión)
- **Negocio Oficial**: BuyaCAR — Vehículos usados certificados en Tunja, Boyacá (Av. Oriental - Cra. 6 #52-23, frente a la Toyota).
- **Líneas Oficiales**: WhatsApp exclusivo `3151212223` | Teléfonos `3022866969` y `3160502717`.

---

## 1. Mapa de Paquetes Implementados (`app_build/`)

| Paquete | Responsabilidad | Archivos Clave |
|---|---|---|
| `cmd/buyacar` | Punto de entrada del daemon en modo `DRAFT` y corredor reproducible en `SIMULATION`. | `main.go` |
| `internal/channel` | Interfaz canónica `Channel`, adaptador stub y Canal Piloto de Correo en modo `DRAFT` (con bloqueo de `AUTOPILOT`). | `channel.go`, `stub.go`, `email_pilot.go`, `email_pilot_test.go` |
| `internal/core` | Motor con kill switch atómico (< 1s), reglas de omisión, pipeline de 12 pasos y suite de 64 casos dorados. | `core.go`, `pipeline.go`, `core_test.go`, `pipeline_test.go`, `golden64_test.go` |
| `internal/store` | Almacenamiento de idempotencia SHA-256 (`canal\|conversacion\|mensaje`) y checkpoints de reanudación. | `store.go` |
| `internal/links` | Repositorio editable con allowlist HTTPS (`buyacar.com.co`) y filtro de enlaces activos. | `links.go` |
| `internal/inventory` | Caché ETag en memoria, cliente HTTP `GET` con `If-None-Match` (`304 Not Modified`), ventanas de 15m/30m y 6 estados. | `inventory.go`, `http_fetcher.go`, `stub.go`, `inventory_test.go` |
| `internal/triage` | Normalización sub-milisegundo, clasificación fácil/difícil y detección de las 10 causales de escalamiento. | `triage.go` |
| `internal/fast` | Respuestas deterministas con confianza >= 0.80, textos editables (`BusinessTexts`), benchmarks y fuzzing. | `fast.go`, `fast_test.go` |
| `internal/guard` | Bloqueo de cifras >= 5 dígitos no respaldadas, allowlist HTTPS, frases prohibidas, redacción de PII y recorte seguro por canal. | `guard.go`, `guard_test.go` |
| `internal/skills` | Compresión de contexto (intención + 1 a 3 vehículos + resumen) y caché de respuestas aprobadas por humanos. | `skills.go` |
| `internal/ai` | Cadena `Cache -> Gemini -> Ollama (llama3.2:3b) -> Humano`, prompt delimitado anti-injection, `DisallowUnknownFields` y 1 reintento. | `ai.go`, `providers.go`, `stub.go`, `ai_test.go` |
| `internal/escalate` | Cola humana con redacción automática de PII (Ley 1581 de 2012). | `escalate.go` |
| `internal/obs` | Trazas de 7 checkpoints exportables a Markdown sin PII, colector p50/p95/p99 y puerta de salida `EvaluateDraftExitGate` (100 conversaciones). | `obs.go`, `obs_test.go` |

---

## 2. Comandos de Validación Reproducible (Con Go 1.23+ en PATH)

```powershell
cd app_build
gofmt -s -w .
go vet ./...
go test -v ./...
go test -race ./...
go test -bench=BenchmarkFastPath -benchmem ./internal/fast
go test -fuzz=FuzzTriageClassify -fuzztime=5s ./internal/fast
go test -fuzz=FuzzGuardValidateOutbox -fuzztime=5s ./internal/guard
go run ./cmd/buyacar
```

---

## 3. Controles de Seguridad y Gobernanza Activos

1. **Kill Switch Global**: `Engine.TriggerKillSwitch()` detiene inmediatamente (< 1 ms) cualquier procesamiento nuevo mediante `atomic.Bool`.
2. **Cero Cifras Inventadas**: Toda cifra de 5 o más dígitos es contrastada contra precios/kilometrajes del inventario relevante o las 3 líneas oficiales.
3. **Protección de Datos Personales (Ley 1581 de 2012)**: `guard.RedactPII` redacta correos, cédulas, NIT y tarjetas antes de escribir trazas o tickets humanos.
4. **Economía de Tokens**: El Fast-Path y la caché aprobada resuelven las consultas frecuentes con **0 tokens** de IA; las consultas complejas respetan el tope de 1.500 tokens de entrada y 300 de salida.
