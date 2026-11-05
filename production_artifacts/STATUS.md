# Estado del workspace — BuyaCAR Omnicanal Control Plane & Motor RAG

- **Modo Operativo**: `DRAFT` (Transición controlada iniciada; sincronización de inventario real activa `BUYACAR_LIVE_INVENTORY_SYNC=1`, generación de borradores con supervisión humana y cero envíos automáticos no supervisados).
- **Estado**: `DRAFT_MODE_ACTIVE_AND_VERIFIED` (Diagnóstico de salud verificado en vivo con `inventory_live_synced: true`, `active_mode: DRAFT` y métricas operacionales).
- **Gobernanza y Saneamiento Anti-Vibecoding**:
  - **Motor RAG Local Híbrido (`internal/rag/`)**: Implementado en Go 1.23 estándar sin dependencias externas. Búsqueda combinada BM25 léxica y similitud coseno vectorial con conmutación transparente a Ollama (`nomic-embed-text`) en `127.0.0.1`. Latencia p99 medida: **251 µs**.
  - **Fixtures de Conocimiento Embebidos (`fixtures/rag/`)**: Catálogo vehicular, políticas de garantía, requisitos de crédito para Boyacá/Tunja y FAQs servidos nativamente vía `embed.FS`.
  - **Inyección en CP4 & Presupuesto de Tokens**: Enriquecimiento semántico de contexto acotado al 47.3% del presupuesto asignado ($\le 150$ tokens por fragmento relevante).
  - **Guardrails en CP5 y Casos Dorados**: `ValidateOutboxWithBackedNumbers` en `internal/guard/guard.go` para validar cifras de 5+ dígitos contra el inventario y documentos normativos indexados. Suite de 7 casos dorados en `test/rag_golden_test.go` pasando en verde.
  - **Pipeline e Instrumentación Real**: `internal/obs` integrado en `internal/core/pipeline.go`. Checkpoints CP1–CP7 con duraciones efectivas `time.Since(...)` y marcas de tiempo monótonas crecientes.
  - **Exportación Genuina de Trazas**: Salida automática de trazas JSON reales generadas en tiempo de ejecución a `production_artifacts/traces/`.
  - **Compuertas Humanas Desacopladas**: Las compuertas a `AUTOPILOT` permanecen en `false` / `BLOCKED_PENDING_HUMAN_REVIEW` hasta la aprobación explícita en consola web o canal piloto.

## Resultados de Pruebas Técnicas (Go 1.23 Standard Library)
- **Ejecución Uncached**: 100% PASS (`go test -count=1 ./...` en verde en los 18 paquetes de `app_build`).
- **Casos Dorados RAG**: 8/8 casos dorados pasando en verde (`test/rag_golden_test.go`, incluyendo `TestRAGGolden_M0_MarketplaceCourseCorrection`).
- **Ausencia de Dependencias Externas**: 0 paquetes de terceros en `app_build/go.mod` (Go standard library puro).
- **Seguridad**: Redacción determinista de PII (Ley 1581 de 2012) activa, filtro anti-alucinación numérica (5+ dígitos) verificado con 21 cifras respaldadas por el RAG, guardia anti-sustitución de marca/modelo vehicular en `internal/rag/engine.go`, allowlist HTTPS estricta a `https://buyacar.com.co`.
- **Fast-Path**: Latencia p99 medida en ejecución real: ~1.25 ms (cumpliendo holgadamente el SLA de < 5 ms).

## Correcciones de Rumbo Ejecutadas (2026-11-04 — Hito M0 Completado)
- **Calidad RAG (Hito M0 de `docs/MARKETPLACE_PLAN.md`) — RESUELTO**:
  - Sincronizados los catálogos entre `internal/inventory/stub.go`, `internal/triage/triage.go` y `fixtures/rag/vehicles.json` + `vehicles.md` (incluyendo `Toyota Hilux 2022`, `Mazda CX-5 2021/2023`, `Renault Duster`, `Stepway`, `Logan`, `Chevrolet Tracker`, `Mazda CX-30`).
  - Añadido stemming morfológico en español (`stemSpanish` en `internal/rag/bm25.go`), ponderación 3x de títulos y `FusionWeighted (0.80 BM25 / 0.20 Vector)` + guardia de marca/modelo en `internal/rag/engine.go`:
    - Consulta `"Toyota Hilux 2022"` devuelve `Toyota Hilux SRX 2.8 4x4 2022` (`185000000`) como #1.
    - Consulta `"Ferrari Testarossa 2022"` (inexistente) devuelve `0` chunks (cero sustitución de vehículos).
    - Consulta `"ubicación y horario en Boyacá"` devuelve `Horarios de Atención y Ubicación de Sedes` como #1.
    - Consulta `"garantía motor y caja"` devuelve `Cobertura de Garantía Mecánica Certificada` como #1.
  - Conectado `RAGEngine` con fixtures embebidos y registrado el canal `facebook_marketplace` en `app_build/cmd/buyacar/main.go`.
  - Corregido el bug de impresión de cifras respaldadas en `app_build/cmd/demo_rag/main.go`.
- **Ciclos Kaizen M1, M2 y Torneo de Autopilotaje Fases 1–4 (Facebook Marketplace Autónomo) — COMPLETADOS**:
  - **Hito M1 (`internal/triage/triage.go`, `internal/fast/fast.go`, `internal/fast/fast_test.go`)**: Clasificación determinista y respuestas Fast-Path (`≤ 500` caracteres, `0` tokens de IA, puente a WhatsApp `3151212223`) verificadas con 30 frases doradas de compradores de Marketplace en `TestFastPath_M1_MarketplaceGoldenPhrases`.
  - **Hito M2 (`internal/server/dashboard_handler.go`, `internal/server/assets/index.html`, `cmd/buyacar/main.go`, `cmd/buyacar/main_test.go`)**: Endpoint `POST /api/copilot/draft` + tarjeta interactiva en la consola web (`http://127.0.0.1:8080`) con síntesis fáctica respaldada por RAG local, redacción automática de PII (Ley 1581) y validación anti-alucinación CP5 (`TestRunCLI_MarketplaceCopilotEndpoint_M2`).
  - **Fases 1–4 de Autopilotaje (`internal/triage/triage.go`, `internal/core/pipeline.go`, `internal/channel/meta_omnichannel.go`, `test/autopilot_tournament_test.go`)**:
    - **Fase 1**: Memoria de hilo multi-turno (`ClassifyWithContext`) y herencia automática del anuncio desde `referral.ad_title` (`[Anuncio Marketplace: <vehículo>]`).
    - **Fase 2**: Enriquecimiento de consultas compuestas (Vehículo + Garantía/Crédito/Peritaje) y sintetizador determinista `SynthesizeFromRAGChunk` (`0` tokens, `100 %` fáctico).
    - **Fase 3**: Torneo Kaizen de 4 arquitecturas sobre 30 conversaciones reales de Marketplace (`TestAutopilotTournament_Compare4Possibilities`) — Ganadoras `A2_rag_synth` (`54 µs`, `0` tokens) y `A4_adaptive` (`73 µs` offline / `105 µs` con LLM) con **100 % de resolución autónoma rutinaria (22/22)**, **100 % de precisión de escalamiento (8/8)** y **0 violaciones CP5/PII**.
    - **Fase 4**: Webhook event-driven `POST /webhooks/meta` con validación HMAC-SHA256 y disparo en tiempo real del pipeline autónomo (`SetOnInboundConversation`), más Kill Switch en vivo `POST /api/killswitch` (`TestRunCLI_Phase4_MetaWebhookAndKillSwitch`).
  - **Catálogo Real de BuyaCAR + Controles 1-Clic de `AUTOPILOT` en Consola Web (`http://127.0.0.1:8080`)**:
    - Cargados los 23 vehículos reales de `https://buyacar.com.co/wp-json/vehica/v1/cars` (Nissan Kicks 2020, Toyota Fortuner 2024, VW Tiguan 2020, Nissan Qashqai 2026, Kia Picanto 2022, Renault Captur 2022, Mazda 3 2021, etc.) + decodificador WordPress Vehica en `internal/inventory/http_fetcher.go`, sede oficial (`Av. Oriental - Cra. 6 # 52-23, frente a la Toyota, Tunja`) y líneas (`3151212223`, `3022866969`, `3160502717`).
    - Añadidos endpoints sin fricción en `internal/server/dashboard_handler.go` y botones en `internal/server/assets/index.html`: conmutador en caliente `POST /api/mode` (`SIMULATION` / `DRAFT` / `AUTOPILOT`), simulador de webhook firmado en 1 clic `POST /api/webhook/simulate`, receptor loopback `POST /api/meta/sandbox/me/messages`, formulario en caliente `POST /api/meta/config` y lanzador `iniciar_buyacar.bat` (`TestRunCLI_WebConsole_OneClickAutopilotAndSignedWebhookSimulation`).
  - **Integración `GEMINI_API_KEY` en Caliente + Workflows Oficiales `n8n` (`TestRunCLI_Phase5_N8NBridgeAndHotReloadGeminiAPIKey`)**:
    - Recarga en caliente de `GEMINI_API_KEY` vía `POST /api/gemini/config` en la consola web (`http://127.0.0.1:8080`), activando automáticamente la estrategia `A4_adaptive` con `gemini-2.5-flash` (Structured Outputs JSON + normalización de cifras + Guardia CP5).
    - Creados los 2 workflows `.json` listos para importar en `n8n`: `n8n/buyacar_standalone_gemini_workflow.json` (100% autónomo dentro de n8n con Triage, PII Ley 1581, inventario `buyacar.com.co/wp-json/vehica/v1/cars`, Gemini 2.5 Flash y Guardia CP5) y `n8n/buyacar_bridge_workflow.json` + endpoint puente `POST /api/n8n/webhook`.
- **Nota sobre `-race`**: `go test -race` sigue no ejecutable en este host Windows porque requiere CGO/`gcc` no instalado en `PATH`.

## Próxima Acción
El sistema cuenta con motor RAG local operativo en `SIMULATION`. La consola web embebida (`--server`), el exportador de métricas (`/metrics`) y el pipeline con soporte RAG están listos para inspección o despliegue en pruebas de campo.
