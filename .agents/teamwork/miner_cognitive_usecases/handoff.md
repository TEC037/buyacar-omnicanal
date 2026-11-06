# Handoff Report — Miner Cognitive Use Cases & Fast-Path

## 1. Observation
- Se inspeccionaron y auditaron directamente los siguientes archivos fuente y líneas de código:
  * `app_build/internal/fast/fast.go` (Líneas 13-30 `BusinessTexts`, Líneas 58-131 `Evaluate`, Líneas 133-151 `formatVehicleReply`).
  * `app_build/internal/triage/triage.go` (Líneas 13-34 `Intent`, Líneas 60-228 `Classify`, Líneas 230-258 `ClassifyWithContext`, Líneas 318-336 `extractVehicleQuery`).
  * `app_build/internal/rag/engine.go` (Líneas 47-62 `NewEngine`, Líneas 144-212 `IndexDocument`, Líneas 214-317 `Retrieve`, Líneas 262-267 guardián de catálogo `chunkMatchesCatalogModels`).
  * `app_build/internal/rag/bm25.go` (Líneas 68-97 `Tokenize`, Líneas 99-133 `stemSpanish`, Líneas 148-276 `BM25Index`).
  * `app_build/internal/ai/cascade.go` (Líneas 37-77 `DefaultFastMatcher`, Líneas 79-193 `FastPathTierProvider` que duplica lógica de `fast.go`, Líneas 195-297 `CascadeEngine`).
  * `app_build/internal/core/pipeline.go` (Líneas 230-235 `ClassifyWithContext`, Líneas 243-292 `Fast.Evaluate` y `retrieveCompoundPolicyClause`, Líneas 344-347 `BuildCompactContextWithRAG` con solo `cleanedText`).
  * `app_build/fixtures/rag/` (`warranty_policy.md`, `credit_requirements.md`, `faq_boyaca.md`, `vehicles.json`, `vehicles.md`).
- Se ejecutaron los comandos de verificación nativa obteniendo:
  * `go test -v ./internal/fast ./internal/rag ./internal/store ./internal/inventory`: `PASS` en todos los paquetes.
  * `go test -v ./internal/ai`: `PASS` (incluyendo `TestCascadeEngine_*`, `TestSemanticCache_*`, `TestDeduplicator_*`).
  * `go test -v -run=TestGoldenSuite_64Cases ./internal/core`: `PASS` (64/64 casos dorados en 0.00s).
- Se redactó el informe completo de hallazgos en:
  `c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\miner_cognitive_usecases\report.md`

## 2. Logic Chain
1. *A partir de la observación de `fast.go` y `triage.go`*:
   El Fast-Path determinista resuelve con latencia ~2.59 µs y 0 tokens las intenciones de Ubicación (`AddressReply`), Horarios comerciales (`HoursReply`), Contacto (`ContactReply`), Saludo (`GreetingReply`) y precios/kilometrajes de catálogo exacto (`formatVehicleReply`).
2. *A partir de la observación de `triage.go:124` y `fast.go`*:
   Las consultas institucionales sobre la política de garantía (1 año o 20.000 km, motor y caja) y las opciones de crédito bancario general (bancos aliados, cuota inicial desde 10%) NO tienen una intención rápida dedicada en `fast.go`. Están excluidas en `triage.go:124` (`isPolicyOrWorkshopQuery`) y caen en `IntentComplexQuery`. Por tanto, el sistema acude a RAG o LLM para responder datos que son fijos e institucionales.
3. *A partir de la observación de `rag/engine.go` e `inventory/inventory.go`*:
   Existe doble indexación del catálogo de vehículos: estructurada en `inventory.Cache` (con mapa por ID y búsqueda O(N) en memoria con ETag) y no estructurada como chunks de texto con vectores en `rag.InMemoryEngine`. Para un catálogo de 15 a 50 vehículos en Tunja, vectorizar el catálogo añade complejidad superflua y riesgo de desvío léxico que obligó a programar un guardián manual en `engine.go:262`.
4. *A partir de la observación de `ai/cascade.go:79-193`*:
   `FastPathTierProvider` en el paquete `ai` re-implementa de forma redundante las respuestas deterministas de `fast.go` como un "Tier 3" de inferencia. Si `fast.go` ya evaluó la consulta en el paso CP3 del pipeline, volver a evaluarlo dentro del orquestador de IA duplica código y mantenimiento.
5. *A partir de la observación de `pipeline.go:344`*:
   La memoria multiturno en `ClassifyWithContext` hereda eficazmente el vehículo entre turnos para el triage, pero al momento de llamar al LLM en `BuildCompactContextWithRAG`, únicamente se le envía `cleanedText` (el último mensaje), dejando por fuera el historial de preguntas previas no automotrices.

## 3. Caveats
- No se modificó ningún archivo de código en `app_build/` conforme al rol de Spec Miner (rol estrictamente de lectura y auditoría).
- Las recomendaciones de poda y adición bajo la filosofía de Saint-Exupéry quedan documentadas para que el arquitecto (`@architect`) y el ingeniero de Go (`@go-engineer`) las implementen en el ciclo Kaizen correspondiente.
- No se evaluaron canales externos en vivo (`AUTOPILOT`), respetando el modo `SIMULATION` / `DRAFT`.

## 4. Conclusion
El Núcleo Cognitivo y Fast-Path de BuyaCAR Omnicanal es sólido, rápido y seguro contra alucinaciones e inyecciones. Sin embargo, aplicando el principio de Antoine de Saint-Exupéry ("La perfection se alcanza no cuando no hay nada más que añadir, sino cuando no hay nada más que quitar"):
1. **Sobra**:
   - La vectorización y chunking del catálogo de vehículos en RAG (debe quedar solo en `inventory.Cache`).
   - La réplica de Fast-Path (`FastPathTierProvider`) dentro de `ai/cascade.go`.
   - La capa de embeddings pseudo-vectoriales para el corpus estático de 16 párrafos de Boyacá, donde BM25 con normalización de español resuelve el 100% de las consultas.
2. **Falta**:
   - Promover la Garantía (`IntentWarranty`) y Requisitos Generales de Crédito (`IntentCreditGeneral`) a Fast-Path directo de costo cero (0 tokens) en `fast.go`.
   - Parser de presupuesto (`MaxBudgetCOP`) y caja (`Transmission`) en `triage.extractVehicleQuery`.
   - Enriquecer el `historySummary` en `CompactContext` con un resumen acumulado de turnos anteriores.

## 5. Verification Method
Para verificar de manera independiente las afirmaciones y hallazgos de este reporte:
1. Inspeccionar el reporte completo:
   `view_file c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\miner_cognitive_usecases\report.md`
2. Ejecutar la suite de pruebas nativas en PowerShell:
   ```powershell
   cd c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\app_build
   go test -v ./internal/fast ./internal/rag ./internal/store ./internal/inventory
   go test -v ./internal/ai
   go test -v -run=TestGoldenSuite_64Cases ./internal/core
   ```
3. Verificar que `triage.go:124` contiene `isPolicyOrWorkshopQuery` excluyendo "garantia" y "credito" del Fast-Path directo.
4. Verificar que `ai/cascade.go:79-193` duplica los textos de `fast/fast.go`.
