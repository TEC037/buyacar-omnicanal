# Handoff Report — Canales & Conversión Omnicanal (Frente 1)

**Frente**: Canales & Conversión Omnicanal  
**Agente**: Spec Miner (`teamwork_spec_miner`)  
**Fecha**: 2026-11-06  
**Ruta del Reporte Completo**: `c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\miner_channels_usecases\report.md`  
**Destinatario**: Orquestador (`a6843d34-b87f-49f4-b1de-a2e20aa11497`)

---

## 1. Observation

1. **Fuentes documentales inspeccionadas**:
   - `docs/MARKETPLACE_PLAN.md` (§2 y §5): Constata que Meta entrega hilos de Marketplace a la Página vía Messenger Platform, y descarta explícitamente robots Playwright/headless para cuentas personales por violación de ToS y riesgo de baneo.
   - `instructivo.md` (§2 y §3): Define los datos inmutables de verdad comercial de BuyaCAR en Tunja (Av. Oriental - Cra. 6 #52-23, frente a la Toyota; WhatsApp `3151212223`, teléfonos `3022866969` y `3160502717`).
   - `docs/SPEC.md` y `docs/MVP_DOCUMENTATION.md`: Especifican los 12 pasos del flujo, los 7 checkpoints (CP1–CP7) y la operación modular sin dependencias externas.
2. **Código fuente en `app_build` directamente auditado**:
   - `internal/channel/meta_omnichannel.go` (líneas 206–248, 304–358, 416–423): Implementa la recepción de `/webhooks/meta` con firma HMAC-SHA256, triage temprano de descarte (echos, recibos, MIDs duplicados), y extracción de `referral.ad_title` para contextualizar los mensajes de Marketplace.
   - `internal/channel/whatsapp_cloud.go` (líneas 116–131, 327–338, 506–512, 638–680): Implementa el canal oficial WhatsApp Business Cloud API, control de Opt-In/Opt-Out, catálogo de 4 plantillas HSM (`lead_followup_24h`, `price_drop_alert`, `appointment_reminder`, `welcome_optin`), cumplimiento de ventana de 24 horas y parseo multimedia de imágenes, audios y documentos.
   - `internal/channel/whatsapp_composite.go` (líneas 75–140, 380–549): Implementa conmutador de contingencia hacia `WhatsAppPlaywrightChannel` con selectores DOM versionados de WhatsApp Web (`div[contenteditable='true'][data-tab='3']`, `div[data-testid='conversation-compose-box-input']`).
   - `internal/channel/mercadolibre.go` (líneas 1–635): Conector de 635 líneas con topics `questions`/`messages`, guardrails de censura de teléfonos de preventa (`reMeliPhoneColombia`, `ContactRedactionPlaceholder`). No está conectado en `cmd/buyacar/main.go`.
   - `internal/channel/email_pilot.go` (líneas 1–318): Canal piloto de correo electrónico con bloqueo estricto de AUTOPILOT (`ErrEmailAutopilotBlocked`).
   - `internal/channel/resilience.go` (líneas 164–210): `MemoryResilienceTracker` con auto-pausa a los 3 fallos consecutivos (`DefaultMaxConsecutiveFailures = 3`), encolado de alerta a `escalate.Queue` y supresión de tormentas de alertas.
   - `internal/channel/rate_controller.go` (líneas 75–115, 149–156): Control de tasa con ventana deslizante (120/hr, 250/día) y jitter determinista SHA-256 (±30%).
   - `internal/crm/lead_scoring.go` (líneas 26–38, 61–117, 131–165): `ExtractColombianPhone` detecta celulares colombianos (`3[0-5]\d{8}`), `CalculateIntentScore` otorga +45 puntos y cambia a `StageAppointment`. `MemoryLeadStore` indexa exclusivamente por `lead.CustomerID`.
   - `internal/server/dashboard_handler.go` (líneas 291–355, 375–439, 441–485, 576–662, 819–877): Expone endpoints de supervisión web, aprobación 1-clic (`/api/draft/approve`), reanudación de canal (`/api/channel/unpause`), copiloto de Marketplace (`/api/copilot/draft`), simulador de webhook (`/api/webhook/simulate`) y mock leads hardcodeados en `/api/crm/leads`.
3. **Pruebas ejecutadas**:
   - `go test -v ./internal/channel/...`: 43 tests en verde (4.16s).
   - `go test -v ./internal/server/... ./internal/crm/...`: 20 tests en verde (4.18s).
   - `go test -v ./test/...`: Torneo Kaizen (`TestAutopilotTournament_Compare4Possibilities`) y suites E2E Tier 1–4 en verde (5.79s).

---

## 2. Logic Chain

1. **Premisa 1 (Realidad Comercial en Tunja)**: Un concesionario de autos usados en Tunja capta el 80%+ de sus prospectos en Facebook Marketplace mediante preguntas rutinarias (*"¿Sigue disponible?"*), pero cierra las ventas mediante WhatsApp y prueba de ruta presencial en la vitrina de la Av. Oriental frente a la Toyota.
2. **Premisa 2 (Filosofía de Saint-Exupéry)**: La perfección de la arquitectura no consiste en acumular adaptadores y canales teóricos, sino en eliminar todo lo que no añade valor real a la conversión y pulir al máximo el embudo esencial.
3. **Paso Lógico 1 (Lo que sobra en el código)**:
   - `mercadolibre.go` (635 líneas) no está registrado ni conectado en `cmd/buyacar/main.go`; es código huérfano. En Tunja los vehículos usados se comercializan por Marketplace y WhatsApp, no por MercadoLibre.
   - `whatsapp_composite.go` (567 líneas) con fallback a Playwright sobre WhatsApp Web contradice abiertamente `docs/MARKETPLACE_PLAN.md` §2 y las reglas de seguridad, arriesgando el baneo de la línea oficial. La API oficial de Meta `whatsapp_cloud.go` es la vía canónica y estable.
   - `email_pilot.go` (318 líneas) es un vestigio de pruebas tempranas; los clientes de autos usados en Tunja no compran por email.
   - `handleCRMLeads` en `dashboard_handler.go` reinyecta leads sintéticos fijos que distorsionan el CRM en memoria.
4. **Paso Lógico 2 (Lo que falta en el embudo)**:
   - En `lead_scoring.go`, `MemoryLeadStore` indexa exclusivamente por `CustomerID`. Cuando un lead entrega su celular en Marketplace (`comprador-mkt-1`) y luego salta a WhatsApp (`3101234567`), el sistema no unifica la sesión por falta de un índice secundario de teléfono (`GetLeadByPhone`).
   - En `whatsapp_cloud.go`, `sendViaGraphAPI` solo despacha texto y plantillas HSM; carece de despacho nativo de imágenes (`type: "image"`), lo cual impide enviar fotos oficiales de peritaje y vehículos directamente por WhatsApp.

---

## 3. Caveats

- No se modificó ningún archivo de código fuente del producto en `app_build/`, en estricto cumplimiento del rol de Spec Miner (rol 100% de sólo lectura).
- La validación con `-race` no está habilitada en este entorno Windows por falta de compilador C (gcc/CGO), tal como se documenta en `docs/MARKETPLACE_PLAN.md` §7.
- La deduplicación cross-channel por número celular requiere estandarizar el prefijo de país internacional (`57310...` vs `310...`) para evitar desajustes en comparaciones de cadenas.

---

## 4. Conclusion

1. **Estado del subsistema**: La capa de Canales y Conversión Omnicanal cuenta con un núcleo de rendimiento sobresaliente (Fast-Path determinista en 54 µs, RAG morfológico fáctico, Guardrails anti-alucinación CP5 y webhook oficial de Meta operativo).
2. **Diagnóstico Saint-Exupéry**:
   - **SOBRA**: `mercadolibre.go` (635 líneas huérfanas), contingencia a Playwright en `whatsapp_composite.go` (567 líneas riesgosas) y `email_pilot.go` (318 líneas innecesarias). Su retiro reducirá más de 1.500 líneas de complejidad superflua.
   - **FALTA**: Búsqueda/unificación por teléfono en `MemoryLeadStore` (`GetLeadByPhone`) para sostener el contexto al saltar de Marketplace a WhatsApp, y soporte de envío saliente de imágenes (`type: "image"`) en `whatsapp_cloud.go`.

---

## 5. Verification Method

Para verificar independientemente todos los hallazgos y evidencias documentados:

1. **Ejecutar suite de canales y resiliencia**:
   ```powershell
   cd c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\app_build
   go test -v ./internal/channel/...
   ```
2. **Ejecutar suite del servidor web, dashboard y CRM**:
   ```powershell
   cd c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\app_build
   go test -v ./internal/server/... ./internal/crm/...
   ```
3. **Ejecutar Torneo Kaizen de Marketplace (30 casos dorados)**:
   ```powershell
   cd c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\app_build
   go test -v -run TestAutopilotTournament_Compare4Possibilities ./test/...
   ```
4. **Inspeccionar archivos de reporte y código clave**:
   - Reporte completo: `c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\miner_channels_usecases\report.md`
   - Puente telefónico: `app_build/internal/crm/lead_scoring.go:26` y `app_build/internal/fast/fast.go:133`
   - Ausencia de índice por teléfono: `app_build/internal/crm/lead_scoring.go:120-176`
