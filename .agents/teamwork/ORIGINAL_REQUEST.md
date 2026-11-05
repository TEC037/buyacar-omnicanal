# Original User Request

## Initial Request — 2026-10-29T05:04:49Z

Implementar las capacidades evolutivas y de robustecimiento de producción para el control plane omnicanal de BuyaCAR en Go 1.23 estándar, integrando canales oficiales y webhooks, persistencia resiliente y conectores CRM/DMS, optimización cognitiva con inferencia gratuita y panel web local embebido con métricas, bajo modo SIMULATION y cumplimiento estricto de PII.

Working directory: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace
Integrity mode: development

## Contexto y Referencias
- Repositorio y código modular en `app_build/` (15 paquetes en Go 1.23 estándar, 100% tests en verde, 64 casos dorados, batería E2E multi-tier).
- Especificación y hoja de ruta: `TODO.md` (Punto 2: "Evoluciones Naturales"), `production_artifacts/STATUS.md`, `instructivo.md`.

## Requirements

### R1. Reglas Base, Gobernanza y Seguridad del Core
Mantener el core en Go 1.23 estándar sin dependencias pesadas, iniciando en modo `SIMULATION` con salida condicionada a `DRAFT`. Preservar en todo flujo la redacción de PII (Ley 1581 de 2012), el filtro determinista anti-alucinación (cifras de 5+ dígitos y allowlist estricta a `https://buyacar.com.co`), y los límites de tasa (120 msg/h, 250 msg/d con jitter SHA-256; 3 fallos consecutivos disparan auto-pausa y emisión de ticket a `escalate.Queue`).

### R2. Canales y Conectores de Comunicación (Squad 1)
Implementar el canal oficial de WhatsApp Business Cloud API (adaptador con validación de webhooks, soporte para plantillas HSM pre-aprobadas, media y gestión de opt-in/opt-out) con conmutación (fallback) resiliente a emulación Playwright y selectores versionados ante saturación de cuota o verificación pendiente. Implementar conector push para webhooks de MercadoLibre (ingesta de preguntas y mensajes post-venta con verificación criptográfica de firmas). Soportar integración omnicanal unificada para Facebook Messenger e Instagram Direct compartiendo triage y rate controller.

### R3. Persistencia, Sincronización DMS e Integración CRM (Squad 2)
Implementar persistencia ACID local en SQLite para `store.IdempotencyRecord` y tareas CRM, con conmutación automática ante indisponibilidad a `internal/crm` en memoria con persistencia en JSON local plano. Conectar sincronización periódica DMS (< 30 min) con validación por ETag, y webhook seguro para invalidación de inventario en tiempo real ante cambios de stock/precio. Soportar alertas de próximo ingreso (waitlist automática con sellado SHA-256) y exportación saliente de tareas y leads a CRM externo mediante webhook HTTP POST firmado con HMAC SHA-256 y reintentos exponenciales, con fallback a `escalate.Queue`.

### R4. Capa Cognitiva y Economía de Tokens con Fallbacks Gratuitos (Squad 3)
Implementar caché hash semántica para respuestas validadas y deduplicación de consultas idénticas para optimizar el consumo de tokens. Configurar la cascada de fallbacks de inferencia gratuita: Nivel 1 (gratis local con proveedor Ollama cuantizado en localhost, Llama 3 / Mistral), Nivel 2 (gratis cloud con Groq / Gemini Free API), y Nivel 3 (offline total con Fast-path determinista por reglas de inventario exact-match a ~2,59 µs/op sin inferencia).

### R5. Consola Web Embebida y Observabilidad (Squad 4)
Implementar panel web local de supervisión humana servido nativamente vía `net/http` con `embed.FS` en el binario Go, permitiendo a los asesores visualizar `escalate.Queue`, aprobar borradores DRAFT en 1 clic y desbloquear canales pausados. Proveer endpoint `/metrics` compatible con Prometheus y modo CLI estructurado JSON para inspección de métricas clave (latencias p95/p99, aciertos de caché, estados de circuit breaker, mensajes por ventana y fallos por canal).

## Acceptance Criteria

### Core y Gobernanza
- [ ] Todo el código compila nativamente en Go 1.23 estándar sin dependencias pesadas y arranca en modo `SIMULATION`.
- [ ] Las políticas de redacción PII (Ley 1581) y anti-alucinación están activas en todos los checkpoints y adaptadores.
- [ ] El controlador de tasa respeta las ventanas de 120 msg/h y 250 msg/d con jitter SHA-256 determinista y auto-pausa al tercer fallo consecutivo.

### Canales y Gateways
- [ ] El adaptador de WhatsApp Cloud API procesa webhooks verificados, gestiona plantillas HSM y conmuta ordenadamente a emulación Playwright ante saturación o bloqueo.
- [ ] El conector de MercadoLibre ingesta preguntas y mensajes post-venta validando firmas criptográficas de webhooks.
- [ ] Los adaptadores de Messenger e Instagram Direct operan reutilizando el controlador de tasa y el motor de triage omnicanal.

### Estado, DMS y CRM
- [ ] La persistencia local opera en SQLite con transacciones ACID y realiza fallback automático a memoria / JSON local plano y `escalate.Queue` ante fallos de disco o SQLite.
- [ ] El conector DMS sincroniza inventario con ventana menor a 30 minutos mediante ETag y el endpoint de invalidación refresca stock/precio en tiempo real.
- [ ] Las alertas de waitlist se generan con sellado SHA-256 cuando un vehículo reservado/no disponible pasa a disponible.
- [ ] La exportación a CRM externo emite HTTP POST firmado con HMAC SHA-256 y maneja reintentos con backoff exponencial.

### Cognitivo y Economía de Tokens
- [ ] La caché hash semántica deduplica consultas idénticas y reduce llamadas redundantes a modelos de lenguaje.
- [ ] La cascada de inferencia gratuita conmuta ordenadamente entre Nivel 1 (Ollama local), Nivel 2 (Groq / Gemini Free) y Nivel 3 (Fast-path determinista exact-match).
- [ ] El Fast-path exact-match resuelve consultas de inventario offline sin requerir inferencia en Nivel 3.

### Consola y Métricas
- [ ] El panel web local se sirve desde el binario mediante `embed.FS` en `net/http` nativo, visualiza `escalate.Queue` y permite la aprobación de borradores DRAFT en 1 clic.
- [ ] El endpoint `/metrics` expone métricas compatibles con Prometheus y se cuenta con exportación CLI estructurada en JSON.

### Verificación y Calidad
- [ ] La suite de pruebas en `app_build/` pasa al 100% con el detector de carreras activado (`go test -race ./...`).


## Follow-up — 2026-10-29T05:16:47Z

DIRECTIVA SUPREMA DE ARQUITECTURA Y BLINDAJE BUyACAR (7 MANDAMIENTOS ANTI-REGRESIÓN):

El usuario ha ordenado incorporar como directiva vinculante e inmutable los siguientes 7 Mandamientos Anti-Regresión para todo el trabajo en ejecución:

1. MANDAMIENTO 1: CERO DEPENDENCIAS DE DOM INESTABLE (API-FIRST O SELECTORES VERSIONADOS CON AUTO-KILL). Ingesta vía Webhook HTTP oficial con firma HMAC-SHA256 (X-Hub-Signature-256). Si hay contingencia gráfica, usar VersionedSelector con auto-pausa al 3er fallo consecutivo y ticket a escalate.Queue. Prohibido reintento infinito.
2. MANDAMIENTO 2: PIPELINE RÍGIDO DE 7 CHECKPOINTS (CP1–CP7) SIN ATAJOS:
   - CP1: Sanitización contra Indirect Prompt Injection.
   - CP2: Triage & Descarte inmediato de mensajes propios/sistema/spam (0 tokens/memoria).
   - CP3: Fast-Path / Inventario Atómico con sync.RWMutex (< 5 ms en exact_match; no disponible o ambigüedad escala a humano).
   - CP4: IA Estructurada & Contexto Estricto (máximo 3 vehículos, salida JSON tipado estricto).
   - CP5: Guardrails Deterministas Post-Generación: Filtro anti-alucinación numérica (cifras de 5+ dígitos cruzadas contra catálogo y teléfonos autorizados: 3151212223, 3022866969, 3160502717); allowlist estricta a https://buyacar.com.co; presupuesto de caracteres (WhatsApp: 650, FB: 500, IG: 450, TikTok: 400); redacción PII Ley 1581 obligatoria.
   - CP6: Despacho Protegido (revalidación pre-envío inventario TTL < 30m, rate controller 120/h y 250/d con jitter SHA-256, modo SIMULATION por defecto).
   - CP7: Sellado CRM e Idempotencia ACID (hash SHA-256 único, inferencia y registro de tarea comercial con reintentos offline).
3. MANDAMIENTO 3: AISLAMIENTO CONCURRENTE Y TOLERANCIA A REINICIOS (Goroutines con context.Context, timeouts < 30s upstream, estructuras thread-safe persistentes contra reinicios).
4. MANDAMIENTO 4: SEGURIDAD DEL ENTORNO LOCAL Y RESPETO DE FRONTERAS (Ollama exclusivo en 127.0.0.1, MAX_BODY_BYTES = 256 KB, timeout 30s, webhooks con validación Bearer/HMAC).
5. MANDAMIENTO 5: PROTOCOLO DE SALIDA Y REGISTRO FORENSE (Detención inmediata ante fallo o data race 'go test -race', suite dorada y tests unitarios sin red externa obligatorios para cada feature).

Por favor confirma recepción e intégralo inmediatamente a los requisitos de control y verificación de todos los squads.


## Follow-up — 2026-10-29T05:44:15Z

URGENTE - INSTRUCCIÓN DE REMEDIACIÓN DE TELEMETRÍA Y SANEAMIENTO DE GOBERNANZA (ANTI-VIBECODING):

El usuario ha ordenado erradicar inmediatamente la simulación cosmética ("teatro de observabilidad") y asegurar mediciones, trazas y gates reales.
Incorpora estas tareas obligatorias a los Squads:

1. SQUAD 1 (Telemetría e Integración del Pipeline en Go):
   - Conectar e importar formalmente `internal/obs` en `pipeline.go`.
   - Reemplazar los descartes ciegos `_ = p.Engine.store.SaveCheckpointState(...)` por captura y trazabilidad de errores reales.
   - Instrumentar la ejecución de los 7 checkpoints (`lectura`, `triage`, `inventario`, `respuesta`, `guard`, `envio`, `crm`) usando `time.Now()` real y duraciones efectivas con `time.Since(...)` para cada etapa.
   - Agregar método o hook en `Pipeline` para exportar trazas reales de ejecuciones efectivas hacia `production_artifacts/traces/`, eliminando archivos JSON/MD escritos a mano.

2. SQUAD 2 (Medición Real de Tokens, Caché y Anti-Automejora Ficticia):
   - Registrar mediciones reales de tokens o marcar explícitamente `not_measured` cuando no haya llamadas. Prohibido reportar 0 misses en cold start o tokens por conversación idénticos a los del prompt.
   - Instrumentar `internal/ai/semantic_cache.go` y `skills.ApprovedCache` para reflejar métricas reales de Hit/Miss.
   - Eliminar la fórmula matemática ficticia de reducción del 10% en `master_cron_self_optimizer.go`. Si una optimización no proviene de un cambio de código medible en p99 o tokens, el estado debe ser `unchanged`.

3. SQUAD 3 (Desacople de Tests Tautológicos y Encoding UTF-8):
   - Refactorizar `draft_batch_exerciser_test.go`: desacoplar la prueba unitaria del harness técnico de la compuerta humana real. Prohibido usar un bucle `for` que auto-apruebe el 100% de los borradores para declarar `ReadyForAutopilot=true`. La compuerta debe requerir entrada explícita o permanecer en `BLOCKED_PENDING_HUMAN_REVIEW`.
   - Actualizar `obs_test.go` para verificar progresión temporal real de los checkpoints (prohibido compartir el mismo timestamp idéntico en todos los pasos).
   - Corregir y forzar codificación UTF-8 sin BOM en todos los volcados de evaluación para erradicar caracteres corrompidos.

4. SQUAD 4 (Gobernanza, Saneamiento de STATUS y Limpieza de Crones):
   - Depurar y unificar el esquema de los 7 checkpoints: marcar obsoleto el esquema de Ciclo 0 (`cp1_input_ingestion`, etc.) y estandarizar la nomenclatura canónica (`cp1_lectura`, etc.).
   - Purgar la sobreingeniería burocrática de crones: cancelar o consolidar `task-158` ("Auditor de crones que auditan crones").
   - Reescribir `production_artifacts/STATUS.md`: retirar hipérboles como "VICTORY CONFIRMED emitido por auditoría independiente" y sustituirlo por el estado técnico real, honesto y reproducible basado en `go test -v -count=1 ./...`.

Criterios de aceptación: `go test -race -count=1 ./...` verde, trazas con timestamps monótonos crecientes y duraciones reales, cero auto-evaluaciones de 100/100 ficticias, y `operatingMode=SIMULATION` preservado.


## Follow-up — 2026-10-29T12:16:59Z

Reanudar ejecución: Se completó la remediación de las 3 observaciones de Reviewer 2 y Reviewer 1 (main.go con SIMULATION por defecto en core.DefaultSafeConfig(), eliminación del bucle de auto-aprobación en main.go, métricas iniciales sin semillas arbitrarias, y corrección de Port: 0 en server.go para asignación dinámica).
Toda la suite de pruebas corre al 100% uncached (go test -count=1 ./...) y el servidor embebido arranca y responde correctamente.
Por favor revalida la compuerta de verificación y procede con el cierre del milestone.


## Approved Task Prompt — 2026-10-29T12:41:16Z

Planificar, diseñar e implementar una arquitectura RAG (Retrieval-Augmented Generation) local en Go 1.23 estándar para producción en BuyaCAR Omnicanal, indexando catálogo vehicular y documentos normativos para enriquecer semánticamente la inferencia en CP4 bajo estricto control de tokens y mandamientos de seguridad.

Working directory: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace
Integrity mode: demo
The team must NOT: Use pre-built libraries or frameworks for core functionality

## Requirements

### R1. Motor RAG Local y Estrategia de Recuperación
Implementar el motor RAG local y gratuito en Go 1.23 estándar sin frameworks ni dependencias de terceros pesadas. Debe operar primariamente en memoria mediante indexación y recuperación léxica/vectorial pura en Go (BM25 / TF-IDF y similitud coseno), con conmutación y fallback transparente a embeddings locales mediante Ollama (`nomic-embed-text`) cuando el servicio esté disponible en localhost.

### R2. Indexación y Chunking de Documentos
Crear y estructurar fixtures locales en Markdown y JSON dentro del repositorio para fichas técnicas de vehículos, políticas de garantía, requisitos de crédito y preguntas frecuentes de BuyaCAR Boyacá/Tunja. Implementar chunking semántico acotado y determinista para segmentar dichos documentos.

### R3. Integración en CP4 (IA Estructurada) y Presupuesto de Tokens
Integrar la recuperación semántica en CP4 (IA Estructurada) inyectando los fragmentos más relevantes y re-rankeados dentro del contexto de inferencia, respetando estrictamente el presupuesto máximo de tokens asignado y manteniendo latencias óptimas.

### R4. Gobernanza, Seguridad y Mandamientos Anti-Regresión
Preservar el modo SIMULATION por defecto, la redacción obligatoria de PII bajo la Ley 1581 de 2012 en todos los fragmentos procesados y devueltos, el filtro determinista anti-alucinación (bloqueo de cifras de 5+ dígitos no sustentadas en inventario o documentos indexados y allowlist HTTPS estricta a buyacar.com.co), y asegurar cero dependencias no autorizadas.

## Acceptance Criteria

### Arquitectura RAG y Recuperación
- [ ] El motor RAG opera localmente en Go 1.23 estándar sin librerías externas ni frameworks pesados.
- [ ] La recuperación híbrida funciona con motor primario en memoria (BM25 / TF-IDF y similitud coseno pura en Go) y conmutación a Ollama (`nomic-embed-text`) si está disponible.
- [ ] El sistema indexa y recupera fragmentos pertinentes de fichas de vehículos, políticas de garantía, requisitos de crédito y FAQ de BuyaCAR Boyacá/Tunja.
- [ ] La inyección de contexto en CP4 respeta el presupuesto de tokens configurado sin desbordar el contexto de inferencia ni degradar la latencia.

### Gobernanza y Seguridad
- [ ] El sistema opera bajo modo `SIMULATION` por defecto.
- [ ] Se preserva la redacción de PII conforme a la Ley 1581 de 2012 en todos los fragmentos y respuestas.
- [ ] El filtro determinista anti-alucinación bloquea cifras de 5+ dígitos no respaldadas en los datos y restringe URLs a `https://buyacar.com.co`.

### Verificación y Calidad
- [ ] 100% de la suite de pruebas pasa en verde sin caché (`go test -count=1 ./...`).
- [ ] La suite completa pasa con el detector de carreras activado (`go test -race ./...`) sin data races.
- [ ] Se implementan casos dorados de prueba programática que validan la relevancia y precisión de recuperación para consultas representativas de usuarios.


## Follow-up — 2026-10-29T12:44:18Z

INSTRUCCIÓN CRÍTICA DE ECONOMÍA DE TOKENS:
El usuario ha expresado preocupación por el consumo de tokens debido a la repetición de texto y contexto extenso.
Aplica estrictamente la regla 50-token-economy.md:
1. No reenvíes ni repitas los requisitos completos, criterios de aceptación ni textos del prompt en tus actualizaciones o mensajes.
2. Limita tus reportes estrictamente a: Estado actual (1 línea), archivos modificados (diffs/rutas) y resultado de tests (1 línea).
3. Pasa directamente a la fase de implementación sin encuestas ni exploraciones redundantes.


## Follow-up — 2026-10-29T12:45:04Z

ORDEN ADICIONAL: Incorporar un subagente Kaizen de mejora continua autoevaluativo en la cuadrilla de Teamwork para el ciclo RAG.
Responsabilidades del rol Kaizen:
- Comparar cada incremento contra métricas objetivas (latencia p99 de recuperación en us, hit-rate de chunks relevantes, tokens inyectados vs límite).
- Detectar regresiones y evaluar la calidad sin autocalificaciones ficticias.
- Mantener reportes ultra-compactos (1 línea por métrica/evaluación).
