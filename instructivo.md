# INSTRUCTIVO INTEGRAL DE TRANSICIÓN, OPERACIÓN Y GOBERNANZA — BUYACAR OMNICANAL
**Archivo**: `instructivo.md`  
**Proyecto**: BuyaCAR Omnicanal (Tunja, Boyacá — Av. Oriental - Cra. 6 #52-23, frente a la Toyota)  
**Estado Actual del Producto**: MVP Completado al 100% en modo `SIMULATION` (Fases 0 a 8 en `GREEN`)  
**Fecha de Emisión**: 2026-10-26

---

## 1. PROPÓSITO DE ESTE INSTRUCTIVO
Este documento reúne en una sola fuente operativa **todo lo necesario para administrar el MVP actual y ejecutar la transición segura** desde el entorno de simulación (`SIMULATION`) hacia el modo borrador (`DRAFT`) y finalmente al modo autónomo (`AUTOPILOT`), incluyendo comandos de compilación, arquitectura implementada, reglas de negocio verificadas, protocolos de seguridad, economía de tokens y listas de chequeo (checklists).

---

## 2. DATOS OFICIALES E INMUTABLES DE NEGOCIO (AUTORIDAD DE VERDAD)
Ningún módulo, prompt o configuración puede alterar estos datos sin aprobación humana explícita:

- **Razón Comercial**: BuyaCAR — Vehículos usados certificados.
- **Dirección Física**: Av. Oriental - Cra. 6 #52-23, frente a la Toyota, Tunja, Boyacá.
- **Sitio Web Oficial (Allowlist HTTPS)**: `https://buyacar.com.co`
- **Endpoint Oficial de Inventario**: `GET https://buyacar.com.co/wp-json/vehica/v1/cars`
- **Líneas Oficiales Autorizadas**:
  - WhatsApp exclusivo: `3151212223`
  - Teléfonos oficiales: `3022866969` y `3160502717`
- **Horario de Atención**: Lunes a Viernes de 8:00 AM a 6:00 PM | Sábados de 8:00 AM a 2:00 PM.
- **Frases Comerciales Permitidas**:
  - *“confianza sobre ruedas”*
  - *“vehículos certificados”*
  - *“listo para prueba de ruta”*
  - *“manejamos todos los bancos”*
  - *“tu vehículo como parte de pago (permuta)”*
  - *“asesoría personalizada”*
- **Expresiones Estrictamente Prohibidas (Bloqueadas por Guard)**:
  - *“crédito aprobado sin requisitos”*
  - *“aprobación garantizada sin estudio”*
  - *“el más barato del mercado”*
  - *“compramos tu carro sin revisarlo”*

---

## 3. ARQUITECTURA IMPLEMENTADA EN `app_build/`
El código fuente en Go (`buyacar.com.co/omnicanal`, 100% librería estándar sin dependencias externas riesgosas) está organizado en los siguientes paquetes:

1. `cmd/buyacar/main.go`: Daemon principal y corredor reproducible end-to-end en `SIMULATION`.
2. `internal/channel`: Interfaz canónica `Channel` (`ID`, `Limits`, `Open`, `Pending`, `Read`, `Reply`) y adaptador `StubChannel`.
3. `internal/core`:
   - `core.go`: Configuración segura por defecto (`DefaultSafeConfig`), kill switch atómico (`TriggerKillSwitch` < 1s) y reglas de omisión (último mensaje propio, conversación > 9 semanas, o hash ya procesado).
   - `pipeline.go`: Orquestador de los 12 pasos por conversación y registro de los 7 checkpoints.
   - `golden64_test.go`: Suite ejecutable con los **64 casos dorados** divididos en 4 suites (Fast-Path, Inventario, Seguridad/Guards y Escalamiento).
4. `internal/store`: Persistencia thread-safe de idempotencia SHA-256 (`canal|conversacion|ultimo_mensaje`) y estado de checkpoints para reanudación tras reinicio.
5. `internal/links`: Repositorio editable sin recompilar con validación estricta `https://` y allowlist de dominios.
6. `internal/inventory`: Caché en memoria con ETag, refresco cada 15 min, caducidad (stale) a los 30 min y los 6 estados deterministas:
   - `exact_match`, `multiple_matches`, `no_match`, `inventory_unavailable`, `inventory_stale`, `vehicle_not_required`.
7. `internal/triage`: Normalización sub-milisegundo, clasificación fácil/difícil, cálculo de confianza y detección de las 10 causales de escalamiento obligatorio.
8. `internal/fast`: Motor Fast-Path determinista (0 tokens de IA) para confianza `>= 0.80`, con textos comerciales editables (`BusinessTexts`).
9. `internal/guard`:
   - Bloquea cualquier cifra de 5 o más dígitos que no exista en el vehículo relevante o en las 3 líneas oficiales.
   - Bloquea URLs fuera de `https://buyacar.com.co`.
   - Bloquea frases prohibidas.
   - Redacta PII (cédulas, NIT, tarjetas, correos) cumpliendo la Ley 1581 de 2012.
   - Aplica límites de caracteres por canal (`tiktok:400`, `instagram:450`, `facebook_marketplace:500`, `whatsapp:650`) sin truncar jamás precios ni teléfonos.
10. `internal/skills`: Compresión de contexto (`BuildCompactContext`: solo intención + 1 a 3 vehículos disponibles + resumen) y caché de respuestas aprobadas por humanos (`ApprovedCache`).
11. `internal/ai`: Cadena `Cache -> Gemini -> Ollama (llama3.2:3b) -> Humano`, validador `ParseStrictJSON` con `DisallowUnknownFields`, un único reintento ante JSON inválido, circuit breaker y límites de 1.500 tokens de entrada / 300 de salida.
12. `internal/escalate`: Cola humana de tickets con contexto sanitizado sin PII.
13. `internal/obs`: Exportador Markdown de los 7 checkpoints (`lectura`, `triage`, `inventario`, `respuesta`, `guard`, `envío`, `crm`) y colector de métricas p50/p95/p99.

---

## 4. PASO A PASO: QUÉ SIGUE DESPUÉS DE `SIMULATION`

La regla de oro del sistema es: **Simulación (`SIMULATION`) $\rightarrow$ Borrador (`DRAFT`) $\rightarrow$ Piloto Automático (`AUTOPILOT`)**. Nunca se salta una etapa.

### ETAPA A: Validación Nativa en el Host Local (Pre-requisito inmediato)
1. Instalar **Go 1.23+** desde `https://go.dev/dl/` en la máquina Windows y verificar que `go version` responda en PowerShell.
2. Ejecutar la suite completa de verificación en `app_build/`:
   ```powershell
   cd c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\app_build
   gofmt -s -w .
   go vet ./...
   go test -v ./...
   go test -race ./...
   go test -bench=BenchmarkFastPath -benchmem ./internal/fast
   go test -fuzz=FuzzTriageClassify -fuzztime=10s ./internal/fast
   go test -fuzz=FuzzGuardValidateOutbox -fuzztime=10s ./internal/guard
   go run ./cmd/buyacar
   ```
3. Confirmar que los 64 casos dorados (`TestGoldenSuite_64Cases`) pasen en verde, con `0` condiciones de carrera y latencia Fast-Path `p99 < 5 ms`.

---

### ETAPA B: Transición al Modo `DRAFT` (Borrador con Supervisión Humana)
En modo `DRAFT`, el daemon consume datos reales de lectura pero **jamás envía mensajes al cliente final**; todas las respuestas aprobadas por el Guard se guardan como borradores (`draft_recorded`) para revisión humana.

#### Pasos para habilitar `DRAFT`:
1. **Conectar lectura del inventario oficial (Solo GET)**:
   - Apuntar el cliente HTTP de `internal/inventory` a `GET https://buyacar.com.co/wp-json/vehica/v1/cars` con cabeceras `If-None-Match` (ETag) / `If-Modified-Since`, timeout de 30s (`upstreamTimeoutSeconds=30`) y refresco cada 15 minutos.
2. **Configurar proveedores de IA en entorno seguro**:
   - Definir la clave de Gemini en variable de entorno del sistema o Secret Manager (`GEMINI_API_KEY`), nunca en archivos del proyecto.
   - Levantar el servidor local de respaldo Ollama en `127.0.0.1:11434` con el modelo `llama3.2:3b` (`ollama pull llama3.2:3b`).
3. **Activar el Canal Piloto de Correo (Orden de implementación #6)**:
   - Antes de usar automatización de navegador, conectar únicamente el canal piloto de correo en modo lectura/borrador.
4. **Cambiar configuración a `DRAFT`**:
   - Actualizar `operatingMode: DRAFT` en la configuración activa.
5. **Criterio de salida de `DRAFT` (Puerta de Calidad hacia `AUTOPILOT`)**:
   - Mínimo 100 conversaciones reales procesadas en borrador con:
     - `0` cifras alucinadas.
     - `0` duplicados.
     - `>= 70%` resuelto por Fast-Path sin gastar tokens de IA.
     - `>= 85%` de borradores aprobados por asesores humanos sin requerir edición.

---

### ETAPA C: Transición al Modo `AUTOPILOT` (Operación Autónoma Controlada)
Solo se activa por canal individual cuando dicho canal ha superado la Etapa B en `DRAFT`.

#### Requisitos obligatorios para `AUTOPILOT`:
1. **Kill Switch de Emergencia a 1 clic / < 1 segundo**:
   - Verificar que `emergencyKillSwitch=true` o la señal de paro detenga cualquier acción en menos de 1 segundo.
2. **Control estricto de cuotas y pausas humanas**:
   - `maxMessagesPerHour=120` y `maxMessagesPerDay=250`.
   - `delayBetweenChatsMs=3200` (3,2 s) y `delayBetweenChannelsMs=2500` (2,5 s) con `jitterPercent=30`.
3. **Revalidación Pre-Envío (Paso 11 del flujo)**:
   - Justo antes de invocar `channel.Reply(..., ModeAutopilot)`, revalidar que el vehículo siga disponible en la caché fresca de inventario, verificar el estado `sent_verified` o `send_failed`, sellar el hash de idempotencia y registrar la tarea en el CRM.
4. **Canales de Navegador (`internal/browser`) solo como último recurso**:
   - Priorizar siempre APIs oficiales (WhatsApp Business Cloud API, Meta Graph API, Gmail API). Si se habilita un canal de navegador con Chrome/CDP, usar selectores versionados y detección de rotura de DOM que escale inmediatamente a humano ante cualquier cambio visual.

---

## 5. POLÍTICA DE ECONOMÍA DE TOKENS Y OBSERVABILIDAD CONTINUA
- **Prioridad innegociable**: `Seguridad y Exactitud > Cumplimiento > Economía de Tokens > Latencia > Comodidad`.
- **Presupuestos máximos**:
  - Por conversación que use IA: máximo **1.500 tokens de entrada** y **300 tokens de salida**.
  - Contexto enviado a IA: exclusivamente intención + resumen corto + de 1 a 3 vehículos prefiltrados.
- **Siete Checkpoints por Conversación**:
  1. `lectura`
  2. `triage`
  3. `inventario`
  4. `respuesta`
  5. `guard`
  6. `envio`
  7. `crm`
- **Auditoría Horaria Activa**:
  - El workspace cuenta con un cron horario que verifica que no existan inconsistencias entre [`docs/SPEC.md`](file:///c:/Users/Buyac/OneDrive/Documentos/I&D/buyacar-omnicanal-agent-workspace/docs/SPEC.md), [`docs/TERMOMETRO_GOOGLE.md`](file:///c:/Users/Buyac/OneDrive/Documentos/I&D/buyacar-omnicanal-agent-workspace/docs/TERMOMETRO_GOOGLE.md), [`docs/MVP_DOCUMENTATION.md`](file:///c:/Users/Buyac/OneDrive/Documentos/I&D/buyacar-omnicanal-agent-workspace/docs/MVP_DOCUMENTATION.md), [`app_build/go.mod`](file:///c:/Users/Buyac/OneDrive/Documentos/I&D/buyacar-omnicanal-agent-workspace/app_build/go.mod) y [`production_artifacts/STATUS.md`](file:///c:/Users/Buyac/OneDrive/Documentos/I&D/buyacar-omnicanal-agent-workspace/production_artifacts/STATUS.md), dejando su reporte en [`production_artifacts/evaluations/HOURLY_CONSISTENCY_CHECK.json`](file:///c:/Users/Buyac/OneDrive/Documentos/I&D/buyacar-omnicanal-agent-workspace/production_artifacts/evaluations/HOURLY_CONSISTENCY_CHECK.json).

---

## 6. MAPA DE REFERENCIA RÁPIDA DE ARCHIVOS DEL PROYECTO
- **Especificación Canónica**: `.agents/prompts/BUYACAR_FINAL_INTEGRATED.md`
- **Especificación Técnica**: `docs/SPEC.md`
- **Documentación del MVP**: `docs/MVP_DOCUMENTATION.md`
- **Termómetro de Afinidad Google**: `docs/TERMOMETRO_GOOGLE.md`
- **ADR de Arquitectura**: `docs/adr/0001-modular-go-simulation-token-economy.md`
- **Estado Vivo del Proyecto**: `production_artifacts/STATUS.md`
- **Runbook Cero Fricción**: `production_artifacts/ZERO_FRICTION_RUNBOOK.md`
- **Planificación Neutral Etapa 0**: `production_artifacts/ETAPA_0_PLANIFICACION_NEUTRAL.md`
- **Código Fuente Go y Tests**: `app_build/`
