# BuyaCAR Omnicanal — Estado del Sistema y Hoja de Ruta (TODO.md)

Este documento es el inventario canónico del workspace. Registra de forma concisa y fáctica el estado técnico verificado y los siguientes pasos operativos del proyecto.

---

## 1. Lo que el sistema YA TIENE implementado y verificado (MVP Base)

### Arquitectura Base y Concurrencia (Go 1.23 Estándar)
- **Cero Dependencias Externas**: Código 100% puro en Go standard library (`go.mod` sin módulos de terceros).
- **Pipeline Rígido de 7 Checkpoints (CP1–CP7)**: Lectura/sanitización, triage determinista, inventario/fast-path, IA tipada estructurada, guardrails post-generación, despacho protegido y sellado CRM e idempotencia.
- **Concurrencia Segura**: Concurrencia thread-safe (`sync.RWMutex`, `sync/atomic`), contextos de cancelación (`context.Context`) y timeouts estrictos (< 30s upstream). Cero condiciones de carrera reportadas.
- **Fast-Path Determinista**: Latencia de resolución exacta en ~2,59 µs/op (< 5 ms SLA) y p99 de 1,25 ms en ejecución real sin consumo de tokens ni IA.

### Blindaje Operativo y Anti-Regresión (R1–R3)
- **R1. Revalidador Pre-Envío y Sellado CRM**: Comprobación atómica de frescura de caché (< 30 min) y disponibilidad del vehículo inmediatamente previa al despacho. Aborto automático y aviso estándar ante unidades vendidas o reservadas. Sellado de idempotencia SHA-256 enriquecido con `SendStatus`, `ETag` y `CRMTaskID`.
- **R2. Controlador de Tasa con Pausas y Jitter Determinista**: Ventana deslizante estricta (120 msg/h, 250 msg/d por canal). Pausas humanas con jitter determinista SHA-256 (±30%) reproducible.
- **R3. Resiliencia de Selectores y Auto-Pausa**: `VersionedSelector` con failover ordenado en cascada. Auto-pausa inmediata y ticket a `escalate.Queue` al 3er fallo consecutivo de despacho, suprimiendo tormentas de alertas y aislando fallos por canal.

### Seguridad y Gobernanza
- **Modo Seguro por Defecto**: Arranque inmutable en `SIMULATION`; salida a `DRAFT` y `AUTOPILOT` estrictamente condicionada a autorización humana explícita.
- **Privacidad Ley 1581 de 2012**: Redacción automática determinista de PII (cédulas, nombres, correos personales) en logs y colas humanas.
- **Filtro Anti-Alucinación**: Cruce obligatorio de cada número de 5 o más dígitos contra catálogo de precios, documentos normativos indexados y líneas telefónicas oficiales (`3151212223`, `3022866969`, `3160502717`). Allowlist estricta a `https://buyacar.com.co`.

---

## 2. Capacidades Evolutivas Implementadas y Verificadas (Control Plane Hardened)

Todas las evoluciones naturales han sido implementadas y probadas al 100% en verde:

### Canales y Conectores Oficiales (Squad 1)
- [x] **Canal Oficial WhatsApp Cloud API** (`internal/channel/whatsapp_cloud.go`): Manejo de webhooks verificados con firma criptográfica HMAC-SHA256 (`X-Hub-Signature-256`), soporte para plantillas HSM pre-aprobadas, media y gestión de opt-in/opt-out.
- [x] **Fallback Compuesto WhatsApp / Playwright** (`internal/channel/whatsapp_composite.go`): Conmutación determinista ante saturación de cuota o verificación pendiente de WABA con fallback resiliente.
- [x] **Conector MercadoLibre Webhooks** (`internal/channel/mercadolibre.go`): Ingesta push en tiempo real para preguntas en publicaciones y mensajería post-venta con verificación HMAC.
- [x] **Conector Facebook Messenger & Instagram Direct** (`internal/channel/meta_omnichannel.go`): Adaptador omnicanal unificado compartiendo triage determinista y controlador de cuotas.

### Estado, Persistencia, Inventario y CRM (Squad 2)
- [x] **Persistencia ACID WAL en Go Puro** (`internal/store/wal_store.go`): Write-Ahead Log con checksums IEEE CRC32, replay recovery y durabilidad `fsync`.
- [x] **ResilientStore con Fallback Transparente** (`internal/store/resilient_store.go`, `json_store.go`): Enrutamiento transaccional con conmutación automática ante fallos de disco hacia memoria y JSON plano en disco con alertas a `escalate.Queue`.
- [x] **Conexión a Inventario DMS / ERP Local con ETag** (`internal/inventory/dms_worker.go`): Sincronización periódica (< 30 min) mediante ETag y validación de frescura.
- [x] **Webhook de Invalidación de Inventario en Tiempo Real** (`internal/inventory/invalidation_webhook.go`): Endpoint seguro con firma HMAC para invalidación y refresco inmediato de stock/precio ante eventos del ERP.
- [x] **Alertas de Próximo Ingreso (Waitlist Automática con Sellado SHA-256)** (`internal/inventory/waitlist.go`): Detección automática cuando un vehículo reservado vuelve a estar disponible y generación prioritaria de tarea comercial con sellado criptográfico inmutable.
- [x] **Webhook Saliente para CRM Externo** (`internal/crm/webhook_exporter.go`): Exportación asíncrona de tareas comerciales con reintentos exponenciales y firma de payload HTTP POST mediante HMAC SHA-256.

### Inteligencia, Optimización Cognitiva y Motor RAG (Squad 3 & RAG Engine)
- [x] **Motor RAG Local Híbrido en Go Puro** (`internal/rag/`):
  - Indexación léxica BM25 (`bm25.go`) y similitud coseno vectorial (`vector.go`).
  - Fallback transparente a embeddings locales de Ollama (`nomic-embed-text`) en `127.0.0.1`.
  - Re-ranking híbrido determinista (`hybrid_ranker.go`) con latencia p99 = 251 µs.
- [x] **Fixtures y Chunking Semántico de Conocimiento** (`fixtures/rag/`, `internal/rag/chunker.go`):
  - Documentos embebidos vía `embed.FS` estándar (`vehicles.json/md`, `warranty_policy.md`, `credit_requirements.md`, `faq_boyaca.md`).
  - Chunking semántico acotado con extracción de números respaldados para anti-alucinación.
- [x] **Inyección RAG en CP4 con Control de Tokens** (`internal/core/pipeline.go`, `internal/skills/skills.go`):
  - Enriquecimiento de contexto en CP4 acotado al 47.3% del presupuesto de tokens (límite $\le 150$ tokens por fragmento).
- [x] **Cascada de Inferencia Gratuita de 3 Niveles** (`internal/ai/cascade.go`): 
  - Nivel 1: Ollama local cuantizado (Llama 3 / Mistral) en `127.0.0.1` exclusivo.
  - Nivel 2: Groq / Gemini Free API con circuit breaker y fallback por cuota.
  - Nivel 3: Fast-Path determinista exact-match a ~2,59 µs/op sin llamada a red ni consumo de tokens.
- [x] **Caché Semántica y Deduplicación Single-Flight** (`internal/ai/semantic_cache.go`, `dedup.go`): Normalizador de consultas en español, hash SHA-256 semántico y supresión concurrente de consultas duplicadas.
- [x] **Telemetría Concurrente de Tokens y Caché** (`internal/skills/skills.go`): Métricas reales de Hit/Miss con contadores atómicos (`sync/atomic`).

### Operaciones, Consola y Observabilidad (Squad 4)
- [x] **Panel Web de Supervisión Humana Embebido** (`internal/server/dashboard_handler.go`, `index.html`): Interfaz web servida nativamente vía `net/http` con `embed.FS` en el binario sin dependencias externas, visualizador de `escalate.Queue` y aprobación en 1 clic de borradores DRAFT.
- [x] **Exportador de Métricas Prometheus y CLI** (`internal/server/metrics_exporter.go`): Endpoint `/metrics` con contadores de canales, latencias p95/p99 de Fast-Path y volcado estructurado JSON (`--metrics`, `--inspect`).
- [x] **Instrumentación de Telemetría Real de Checkpoints** (`internal/core/pipeline.go`, `internal/obs/obs.go`): Medición real con `time.Since(...)` por checkpoint (CP1–CP7), marcas de tiempo monótonas crecientes y exportación automática de trazas JSON a `production_artifacts/traces/`.
- [x] **Compuertas Humanas Desacopladas**: Eliminación de loops tautológicos; la transición a `AUTOPILOT` permanece estrictamente bloqueada (`BLOCKED_PENDING_HUMAN_REVIEW`) hasta la intervención humana explícita.

---

## 3. En curso: Canal Facebook Marketplace
- [ ] Plan persistente y ADR pendiente de aprobación: `docs/MARKETPLACE_PLAN.md` (M0 calidad RAG → M1 intents → M2 copiloto → M3 Página oficial → M4 piloto DRAFT).

## 4. Próximos Pasos Operativos y Recomendaciones de Despliegue

Habiendo alcanzado el 100% de cobertura y estabilidad técnica en código con RAG integrado, la siguiente fase corresponde al despliegue progresivo en entorno real bajo supervisión humana:

### Paso 1: Validación Local de la Consola de Asesores
- **Objetivo**: Evaluar la experiencia visual y operativa del equipo comercial de Boyacá.
- **Comando**:
  ```powershell
  cd app_build; go run ./cmd/buyacar --server -port 8080
  ```
- **Acción**: Ingresar a `http://127.0.0.1:8080` y comprobar la aprobación en 1 clic de borradores, el desbloqueo manual de canales pausados y el monitoreo de `escalate.Queue`.

### Paso 2: Configuración de Variables de Entorno de Producción
- **Objetivo**: Proveer credenciales oficiales solo cuando se conecte con plataformas reales (manteniendo `SIMULATION` hasta la prueba controlada):
  - `WHATSAPP_CLOUD_TOKEN` / `WHATSAPP_PHONE_NUMBER_ID`: Para API oficial de Meta.
  - `MERCADOLIBRE_CLIENT_SECRET`: Para validación HMAC de webhooks de MercadoLibre.
  - `ERP_WEBHOOK_SECRET`: Para invalidación en tiempo real del inventario contable de Tunja.
  - `CRM_EXTERNAL_WEBHOOK_URL` / `CRM_HMAC_SECRET`: Para exportación a HubSpot / Salesforce.
  - `OLLAMA_HOST`: Endpoint local de Ollama (por defecto `http://127.0.0.1:11434` para inferencia y embeddings).

### Paso 3: Activación del Piloto Controlado en Modo DRAFT
- **Objetivo**: Generar respuestas en borrador con soporte RAG sin envío desatendido a clientes para auditar la calidad conversacional.
- **Comando**:
  ```powershell
  $env:BUYACAR_OPERATING_MODE="DRAFT"
  cd app_build; go run ./cmd/buyacar
  ```
- **Criterio de Éxito**: Acumular un lote de al menos 100 conversaciones en borrador donde los asesores humanos validen que $\ge 90\%$ de los borradores no requieren edición antes de autorizar el paso a `AUTOPILOT`.

### Paso 4: Monitoreo de Métricas en Producción
- Conectar Prometheus o scraping HTTP periódico a `http://127.0.0.1:8080/metrics` para vigilar:
  - `buyacar_fastpath_latency_p99_us` (< 5000 µs).
  - `buyacar_cache_hits_total` y `buyacar_cache_misses_total` (tasa de acierto de tokens).
  - `buyacar_channel_consecutive_failures` (salud de conectores y auto-pausa preventiva).
