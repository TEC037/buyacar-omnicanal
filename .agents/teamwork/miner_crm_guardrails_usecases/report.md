# Auditoría e Inventario Exhaustivo de Casos de Uso: Seguridad Comercial, Guardrails CP5 & CRM Minimalista
**Proyecto**: BuyaCAR Omnicanal — Concesionario de Usados Certificados en Tunja, Boyacá  
**Ubicación Oficial**: Av. Oriental - Cra. 6 #52-23 (frente a la Toyota, Tunja, Boyacá)  
**Frente Auditado**: Frente 3 — Seguridad Comercial, Guardrails Anti-Alucinación (CP5), Protección de PII (Ley 1581 de 2012) y CRM Comercial Minimalista  
**Filosofía Guía**: Antoine de Saint-Exupéry (*"La perfection est atteinte non quand il n'y a plus rien à ajouter, mais quand il n'y a plus rien à retrancher"*)  
**Fecha de Emisión**: 2026-11-06  
**Auditor**: Spec Miner (`teamwork_spec_miner`)

---

## 1. Resumen Ejecutivo y Alcance de la Auditoría

El presente informe constituye la auditoría exhaustiva e inventario técnico-comercial de la capa de **Seguridad Comercial**, **Guardrails Deterministas (CP5)**, **Protección de Datos Personales (Ley Estatutaria 1581 de 2012)** y el **Centro de Decisiones CRM Minimalista** de BuyaCAR Omnicanal.

A partir del mandato explícito del usuario y la inspección minuciosa del código fuente en Go (`app_build/internal/guard/`, `app_build/internal/crm/`, `app_build/internal/escalate/`, `app_build/internal/triage/`, `app_build/internal/core/`, `app_build/internal/server/`) y la documentación canónica (`docs/SPEC.md`, `docs/MVP_DOCUMENTATION.md`, `docs/OPERATING_GUIDE.md`, `instructivo.md`, `.agents/prompts/BUYACAR_FINAL_INTEGRATED.md`), se ha evaluado la concordancia entre:
1. **Lo que el concesionario de Tunja DEBE TENER**: Un sistema comercial implacable, ágil y jurídicamente blindado, adaptado a la idiosincrasia de Boyacá (peritaje físico de 150 puntos, retoma en 45 min, separación con $2.000.000 COP, atención por WhatsApp en vitrina y estricto Habeas Data).
2. **Lo que el sistema TIENE actualmente**: La implementación en Go 1.23 estándar, sus algoritmos deterministas, sus estructuras de datos, sus canales, sus fortalezas y sus puntos ciegos.
3. **El Criterio de Saint-Exupéry**: Identificación quirúrgica de la complejidad superflua y la burocracia de software que debe ser extirpada, junto con los vacíos esenciales que deben ser subsanados para lograr un CRM espartano, eficaz y comercialmente letal.

---

## 2. Inventario Exhaustivo de Casos de Uso por Dominio

### Dominio A: Guardrails Numéricos Anti-Alucinación CP5

El Checkpoint 5 (**CP5**) es la barrera determinista post-generación y pre-envío que inspecciona todo borrador o mensaje generado por el Fast-Path o los modelos de IA (Gemini / Ollama) antes de su entrega al canal.

#### CU-CP5-01: Detección y Bloqueo de Precios Inventados
- **Propósito**: Impedir que un modelo de IA o un fallo de contexto ofrezca un vehículo por un precio inferior o superior al oficial registrado en el catálogo de usados certificados.
- **Implementación Actual (`guard.go:157-170`)**: Extrae todas las secuencias de 5 o más dígitos (`reFiveOrMoreDigits = \b\d{5,}\b`). Construye el mapa `allowedNumbers` a partir de `PriceCOP` de los vehículos relevantes. Si una cifra $\ge 5$ dígitos no coincide exactamente con el precio del catálogo, la respuesta es bloqueada inmediatamente (`Approved=false`, `MustEscalate=true`, `RejectionReason="hallucinated_number_blocked:<cifra>"`).
- **Enrutamiento**: Se cancela el envío saliente y se encola un ticket en `escalate.Queue` para intervención del asesor humano.
- **Hallazgo Crítico**: El regex `\b\d{5,}\b` evalúa secuencias contiguas de dígitos sin formato. Si una respuesta contiene un precio formateado con puntos o comas (ej. `$150.000.000 COP` o `$150,000,000`), el regex se fragmenta en bloques de 3 dígitos (`150`, `000`, `000`) y **no detecta la cifra**, permitiendo que un precio inventado con separadores de miles burle el guardrail.

#### CU-CP5-02: Validación de Kilometrajes Reales del Inventario
- **Propósito**: Garantizar que el kilometraje prometido al cliente coincida fielmente con el odómetro certificado en el peritaje de 150 puntos.
- **Implementación Actual (`guard.go:217-220`)**: Si el kilometraje del vehículo es mayor a cero, se inyecta en `allowedNumbers` como cadena (`strconv.Itoa(v.MileageKm)`). Si el kilometraje tiene 5 dígitos (ej. `42000` km, `38500` km), cualquier intento de la IA de inventar un kilometraje de 5 dígitos (ej. `25000` km) es bloqueado por CP5.
- **Hallazgo Crítico**: Si un vehículo seminuevo tiene menos de 10.000 km (ej. `8500` km, 4 dígitos), `reFiveOrMoreDigits` no lo captura. La IA podría inventar un kilometraje de 4 dígitos (ej. `3000` km) sin ser bloqueada por CP5.

#### CU-CP5-03: Validación de Años de Modelo
- **Propósito**: Evitar que la IA "rejuvenezca" o confunda el año del vehículo (ej. ofrecer una Hilux 2018 diciendo que es 2022 o 2024).
- **Implementación Actual**: En `triage.go:329-333`, la función `extractVehicleQuery` reconoce años entre 2000 y 2030 para filtrar el catálogo. En `rag/engine.go` existe una guarda de marca/modelo.
- **Hallazgo Crítico**: En CP5 (`guard.go`), **no existe validación determinista de números de 4 dígitos**. Los años de modelo (`2020`, `2022`, `2024`) tienen 4 dígitos y están completamente desprotegidos frente a alucinaciones de la IA post-generación.

#### CU-CP5-04: Cifras Respaldadas por RAG y Fixtures Normativos
- **Propósito**: Permitir que el sistema mencione cifras comerciales y normativas válidas que no son precios ni kilometrajes vehiculares (ej. los `20000` km de garantía mecánica o los `$2000000` COP de abono mínimo de separación en Tunja).
- **Implementación Actual (`guard.go:114-121`, `158-169`)**: `ValidateOutboxWithBackedNumbers` recibe `extraAllowedNumbers` provenientes del motor RAG (`RAGEngine.GetBackedNumbers()`). Registra `20000` y `2000000` en la lista blanca dinámica. Si la cifra está respaldada en los fixtures de conocimiento (`warranty_policy.md`, `faq_boyaca.md`), se aprueba; si no, se bloquea.

#### CU-CP5-05: Prevención de Promesas Comerciales Prohibidas
- **Propósito**: Impedir legal y comercialmente compromisos fraudulentos, engañosos o lesivos para BuyaCAR.
- **Implementación Actual (`guard.go:20-28`, `124-134`)**: Lista determinista `ForbiddenPhrases` que busca coincidencias normalizadas (sin tildes, minúsculas):
  * `"credito aprobado sin requisitos"`
  * `"aprobacion garantizada sin estudio"`
  * `"el mas barato del mercado"`
  * `"compramos tu carro sin revisarlo"`
  * `"descuento garantizado"`
  * `"aprobacion inmediata sin requisitos"`
- **Hallazgo Crítico**: La lista actual no incluye promesas no autorizadas habituales en la negociación informal de usados en Colombia, tales como: `"papeles gratis"`, `"traspaso gratis"`, `"soat de regalo"`, `"obsequio de papeles"` o `"precio negociable"`.

#### CU-CP5-06: Control Estricto de URLs y Allowlist HTTPS
- **Propósito**: Prevenir ataques de Server-Side Request Forgery (SSRF), phishing o derivación de prospectos a sitios externos.
- **Implementación Actual (`guard.go:15-18`, `136-155`)**: Extrae URLs mediante `https?://[^\s)\]"']+`. Exige obligatoriamente esquema `https://` y valida el dominio contra `allowedDomains` (`buyacar.com.co` y sus subdominios autorizados). Cualquier enlace HTTP inseguro o a dominios ajenos es rechazado con escalamiento inmediato (`unauthorized_url_domain`).

#### CU-CP5-07: Recorte Seguro de Longitud por Canal sin Mutilación Numérica
- **Propósito**: Respetar los límites máximos de caracteres por plataforma (`tiktok:400`, `instagram:450`, `facebook_marketplace:500`, `whatsapp:650`) garantizando que jamás se corte un precio o un número telefónico por la mitad.
- **Implementación Actual (`guard.go:171-184`, `233-260`)**: `safeTrimWithoutCuttingNumbers` recorta en el último espacio antes del límite y revalida que todas las cifras de 5+ dígitos del texto original sigan completas e idénticas en el texto recortado. Si el corte mutila un número, no envía la respuesta y escala a humano (`channel_limit_exceeded_unsafe_to_truncate_price_or_phone`).

---

### Dominio B: Redacción de PII y Protección de Datos (Ley Estatutaria 1581 de 2012)

#### CU-PII-01: Ofuscación Determinista de Cédulas, NITs y Tarjetas
- **Propósito**: Cumplir con los principios de seguridad, confidencialidad y circulación restringida de la Ley 1581 de 2012 de Colombia, evitando la exposición de identificaciones personales en trazas, archivos y commits.
- **Implementación Actual (`guard.go:77-92`)**: `RedactPII(text string) (string, int)` ejecuta `reCardOrID.ReplaceAllStringFunc(out, ...)` con el patrón `\b\d{7,16}\b`.
  * Números entre 7 y 10 dígitos (cédulas de ciudadanía colombiana, extranjería, NITs sin dígito de verificación) y entre 13 y 16 dígitos (tarjetas de crédito/débito) se reemplazan por `[PII_NUM_REDACTED]`.
  * Se verifica contra `OfficialPhoneNumbers`: las líneas oficiales de la empresa (`3151212223`, `3022866969`, `3160502717`) se conservan intactas.
- **Hallazgo Crítico**: Si el cliente escribe su cédula o teléfono con formato legible (ej. `1.049.521.890` o `310-456-7890`), `reCardOrID` no la detecta debido a los puntos y guiones, dejando expuesta la PII en trazas y resúmenes de IA.

#### CU-PII-02: Ofuscación de Correos Electrónicos
- **Propósito**: Proteger los datos de contacto del cliente en trazas públicas y exportaciones.
- **Implementación Actual (`guard.go:16`, `78-83`)**: `reEmail` detecta cualquier correo electrónico estándar y lo sustituye por `[PII_EMAIL_REDACTED]`.

#### CU-PII-03: Sanitización de Contexto en Cola de Escalamiento
- **Propósito**: Garantizar que los tickets derivados a revisión humana no contengan datos no autorizados ni expongan PII en sistemas de monitoreo.
- **Implementación Actual (`escalate/escalate.go:30-45`)**: En `Queue.Enqueue()`, el texto entrante se pasa obligatoriamente por `guard.RedactPII` antes de instanciar el struct `Ticket`.

#### CU-PII-04: Sanitización de Trazas de Checkpoints y Métricas
- **Propósito**: Evitar que errores del sistema o mensajes de excepción filtren información sensible del cliente en `production_artifacts/traces/`.
- **Implementación Actual (`obs/obs.go:55`)**: En `ConversationTrace.RecordCheckpoint()`, el mensaje de error `rawErr` se sanitiza automáticamente con `guard.RedactPII` antes de guardarse en JSON.

#### CU-PII-05: Captura Determinista de Celular Colombiano para WhatsApp
- **Propósito**: Rescatar el número de contacto de un comprador en Facebook Marketplace o Instagram para continuar la atención comercial en WhatsApp.
- **Implementación Actual (`crm/lead_scoring.go:23-38`)**: `ExtractColombianPhone(text)` limpia espacios, guiones, puntos y paréntesis, y busca mediante regex `(?:(?:\+?57)?\s*)?(3[0-5]\d{8})`. Si halla un celular válido de 10 dígitos que inicia por 3, lo extrae limpiamente y eleva el `IntentScore` del prospecto en +45 puntos (máxima señal de conversión).

#### CU-PII-06: Gestión del Consentimiento de Habeas Data
- **Propósito**: Obtener autorización previa, expresa e informada del titular antes de almacenar y tratar sus datos comerciales en el CRM, conforme a los artículos 9 y 12 de la Ley 1581 y el Decreto 1377 de 2013.
- **Implementación Actual**: **INEXISTENTE en el código**. El sistema captura el teléfono y lo almacena inmediatamente en `MemoryLeadStore` sin registrar si el cliente autorizó el tratamiento ni presentarle el aviso de privacidad de BuyaCAR (`https://buyacar.com.co/politica-privacidad`).

#### CU-PII-07: Retención y Depuración de Datos Personales
- **Propósito**: Evitar el almacenamiento indefinido de datos personales de prospectos inactivos o descartados, aplicando el principio de necesidad y caducidad.
- **Implementación Actual**: **PARCIAL / LIMITADA**. En `core.go` existe `skipOlderThanWeeks=9` para no responder a hilos antiguos de más de 9 semanas. Sin embargo, en `MemoryLeadStore` y `MemoryManager`, los datos quedan en memoria indefinidamente (`map[string]*LeadProfile`), sin función de expiración automática (TTL), borrado por solicitud de revocatoria (`DeleteLead`) ni purga programada.

---

### Dominio C: CRM Comercial Minimalista para el Concesionario en Tunja

El CRM de BuyaCAR no es un software corporativo masivo, sino un **Centro de Decisiones Tácticas en 1-Clic** diseñado para la operación real del concesionario en Tunja.

#### CU-CRM-01: Agendamiento de Cita para Peritaje Técnico y Prueba de Ruta
- **Propósito**: Consolidar la visita física del comprador a la vitrina de Tunja para evaluar el vehículo en rampa, revisar los 150 puntos y realizar prueba de manejo.
- **Implementación Actual (`crm/lead_scoring.go:17`, `server/dashboard_handler.go:903-907`, `assets/index.html:765-767`)**:
  * Cuando el cliente solicita ver el carro o deja su teléfono, pasa a la etapa `StageAppointment` (`"cita_peritaje"`).
  * Desde la consola web (`http://127.0.0.1:8080`), el botón **"Agendar Cita"** dispara `POST /api/crm/lead/decision` con `decision: "agendar_cita"`.
  * El backend asigna la sede oficial: `Av. Oriental - Cra. 6 #52-23, frente a la Toyota, Tunja` y calcula la fecha sumando 24 horas (`time.Now().Add(24 * time.Hour)`).
- **Hallazgo Crítico**: No permite al asesor seleccionar la fecha/hora exacta acordada con el cliente ni elegir la sede alterna autorizada en Tunja (`Avenida Universitaria #45-12, frente a Unicentro`).

#### CU-CRM-02: Agendamiento de Peritaje para Retoma de Usados (Permuta)
- **Propósito**: Coordinar la recepción del vehículo usado que el cliente desea entregar como parte de pago, para efectuar el avalúo comercial físico de 45 minutos.
- **Implementación Actual (`triage.go:89-98`, `crm/lead_scoring.go:53`, `pipeline.go:752-765`)**:
  * Triage detecta la intención `IntentTradeIn` (`"permuta"`, `"retoma"`, `"reciben mi carro"`, `"avaluo"`).
  * Activa `MustEscalate: true` con motivo `"trade_in_appraisal_required"`.
  * En `LeadProfile`, marca el flag `HasTradeIn = true` y sitúa al prospecto en `StageNegotiation`.
- **Hallazgo Crítico**: El CRM no tiene campos para capturar los datos básicos del carro que el cliente quiere entregar en retoma (marca, modelo, año, kilometraje estimado y placa), obligando al asesor a pedir todo desde cero.

#### CU-CRM-03: Asignación de Asesor Comercial Humano y Notificación
- **Propósito**: Transferir la atención de prospectos calificados o con solicitudes complejas a un asesor comercial específico de la sala de ventas en Tunja.
- **Implementación Actual (`server/dashboard_handler.go:908-909`, `assets/index.html:768-770`)**:
  * En la interfaz web existe el botón **"Asignar Asesor"**.
  * Al presionarse, envía `decision: "aprobar_descuento"`, y en backend actualiza `lead.Stage = crm.StageNegotiation`.
- **Hallazgo Crítico**: El struct `LeadProfile` **no tiene campo de Asesor Asignado** (`AssignedAdvisorName` o `AssignedAdvisorPhone`). No existe selección de asesor ni notificación instantánea (alerta sonora, mensaje interno o WhatsApp al vendedor). El prospecto simplemente cambia de etiqueta en la tabla web.

#### CU-CRM-04: Solicitud de Reserva y Apartado de Vehículo con Abono ($2M COP)
- **Propósito**: Bloquear la venta de un vehículo seleccionado durante 5 días hábiles mientras se legaliza el crédito o el desembolso, mediante un anticipo mínimo de $2.000.000 COP (conforme a `fixtures/rag/faq_boyaca.md:35`).
- **Implementación Actual (`crm/lead_scoring.go:19`, `server/dashboard_handler.go:910-911`, `assets/index.html:771-773`)**:
  * El botón **"Reservar ($2M)"** en la consola dispara `decision: "cerrar_reserva"`.
  * El prospecto pasa al estado `StageClosedWon` (`"cierre_reserva"`).
- **Hallazgo Crítico**: Esta acción **no se comunica con el inventario**. El vehículo en `inventory.Cache` permanece en estado `"available"`. El sistema no marca la unidad como `"reserved"`, lo que permite que otros clientes sigan preguntando por ella y el bot les diga que está disponible.

#### CU-CRM-05: Embudo Comercial Dinámico (Lead Scoring & Pipeline)
- **Propósito**: Clasificar y priorizar a los compradores según su temperatura comercial y etapa en el ciclo de ventas.
- **Implementación Actual (`crm/lead_scoring.go:12-21`, `61-117`)**:
  * Puntuación matemática en tiempo real de 0 a 100 (`CalculateIntentScore`):
    - Base por contacto: 25 pts.
    - Teléfono celular entregado: +45 pts.
    - Interés en vehículo concreto del catálogo: +25 pts.
    - Pregunta por precio/disponibilidad: +15 pts.
    - Interés en crédito/bancos: +20 pts.
    - Solicitud de cita / prueba de ruta: +35 pts.
    - Solicitud de reserva / compra: +40 pts.
    - Interacciones recurrentes: +10 pts.
  * Etapas canónicas del embudo:
    1. `StageCapture` (`"captura_t0"`): Mensaje inicial sin teléfono.
    2. `StageQualified` (`"calificado"`): Interés definido, presupuesto o vehículo (score $\ge 50$).
    3. `StageAppointment` (`"cita_peritaje"`): Teléfono entregado o cita solicitada (score $\ge 80$).
    4. `StageNegotiation` (`"negociacion_humana"`): Solicitud de descuento, permuta o retoma.
    5. `StageClosedWon` (`"cierre_reserva"`): Reserva de $2M efectuada o compra cerrada.
    6. `StageDiscarded` (`"descartado"`): Sin presupuesto o inviable.

#### CU-CRM-06: Escalamiento por Frustración o Complejidad Comercial
- **Propósito**: Resguardar la relación con el cliente entregando la conversación a un humano ante riesgo de pérdida comercial, enojo o trámites bancarios complejos.
- **Implementación Actual (`triage.go:77-121`, `pipeline.go:408-416`, `escalate/escalate.go:31-45`)**:
  * Escalamiento determinista en Triage (0 tokens de IA):
    - Solicitud de descuento o rebaja (`IntentDiscount`).
    - Avalúo de retoma / permuta (`IntentTradeIn`).
    - Reportes en Datacrédito / centrales de riesgo (`IntentCreditStudy`).
    - Reclamos, demandas, siniestros y Fasecolda (`IntentLegalOrClaim`).
    - Intentos de prompt injection (`IntentPromptInject`).
  * Al escalar, se genera un ticket en `escalate.Queue` con motivo y contexto redactado de PII.
- **Hallazgo Crítico**: Si el cliente expresa frustración directa con el asistente (ej. *"este bot no entiende"*, *"quiero hablar con una persona de carne y hueso"*, *"pésimo servicio"*), Triage no tiene esas frases en su lista y lo clasifica como `IntentComplexQuery`, enviándolo al LLM en vez de escalarlo a un humano.

#### CU-CRM-07: Doble Sistema de Tareas y Exportación Webhook
- **Propósito**: Registrar eventos transaccionales en un ledger interno (`crm.Task`) y exportarlos mediante webhook firmado a CRMs externos (HubSpot / Salesforce).
- **Implementación Actual (`crm/crm.go`, `crm/webhook_exporter.go`)**:
  * Maneja `TaskScheduleTestDrive`, `TaskCommercialFollowup`, `TaskCreditApplication`, `TaskGeneralInquiry`.
  * Posee un cliente HTTP con firma criptográfica HMAC-SHA256 (`X-BuyaCAR-Signature-256`), reintentos con retroceso exponencial (backoff) y re-enrutamiento a `escalate.Queue` si el webhook falla tras 3 intentos.
- **Hallazgo Crítico (Saint-Exupéry)**: Esta arquitectura es una duplicación redundante frente a `LeadProfile`. Genera tareas para simples saludos (`TaskGeneralInquiry`) y asume la existencia de un CRM corporativo externo con webhook HMAC que la vitrina de Tunja no utiliza.

---

## 3. Matriz Canónica: Casos de Uso DEBE TENER vs TIENE

| # | Caso de Uso | ¿DEBE TENER el Concesionario en Tunja? | ¿TIENE en `app_build`? | Estado de Cobertura | Brecha / Hallazgo Técnico |
|---|---|:---:|:---:|:---:|---|
| **1** | Bloqueo de precios inventados por la IA | **SÍ** (Innegociable) | **SÍ** | Parcial (Punto ciego) | `guard.go` bloquea secuencias `\b\d{5,}\b`. Si el precio tiene puntos (`$150.000.000`), el regex no lo detecta. |
| **2** | Validación de kilometrajes certificados | **SÍ** | **SÍ** | Parcial (Punto ciego) | Funciona bien para $\ge 10.000$ km (5 dígitos). Para $< 10.000$ km (4 dígitos: ej. 8.500 km) no se valida en CP5. |
| **3** | Validación de años de modelo en catálogo | **SÍ** | **NO** | No Implementado en CP5 | Los años (4 dígitos: 2020, 2022) no son revisados por el Guard post-generación. |
| **4** | Cifras respaldadas por fixtures RAG (garantía/abono) | **SÍ** | **SÍ** | Total (Verificado) | `ValidateOutboxWithBackedNumbers` aprueba 20000 km y $2.000.000 de abono de RAG. |
| **5** | Bloqueo de promesas de descuento no autorizadas | **SÍ** | **SÍ** | Total (Verificado) | Triage escala solicitudes de descuento y Guard bloquea `"descuento garantizado"`. |
| **6** | Bloqueo de promesas de trámites/papeles gratis | **SÍ** | **NO** | Brecha en Guard | `ForbiddenPhrases` no incluye `"papeles gratis"`, `"traspaso gratis"` ni `"soat de regalo"`. |
| **7** | Allowlist estricta de dominios y URLs HTTPS | **SÍ** | **SÍ** | Total (Verificado) | Solo permite `https://` y dominios autorizados de `buyacar.com.co`. |
| **8** | No truncar precios ni teléfonos por límite de canal | **SÍ** | **SÍ** | Total (Verificado) | `safeTrimWithoutCuttingNumbers` verifica la integridad numérica antes de enviar. |
| **9** | Ofuscación de cédulas, NIT y tarjetas en logs | **SÍ** (Ley 1581) | **SÍ** | Parcial (Punto ciego) | Funciona para números continuos; omite números con puntos o guiones (`1.049.521.890`). |
| **10** | Ofuscación de correos electrónicos en trazas | **SÍ** (Ley 1581) | **SÍ** | Total (Verificado) | Regex `reEmail` reemplaza por `[PII_EMAIL_REDACTED]`. |
| **11** | Preservación de líneas oficiales de BuyaCAR | **SÍ** | **SÍ** | Total (Verificado) | Las 3 líneas oficiales (`3151212223`, etc.) no son ofuscadas por error. |
| **12** | Captura de celular colombiano para WhatsApp | **SÍ** | **SÍ** | Total (Verificado) | `ExtractColombianPhone` detecta celulares de 10 dígitos iniciando por 3 y eleva score en +45 pts. |
| **13** | Consentimiento explícito de Habeas Data | **SÍ** (Obligatorio Ley 1581) | **NO** | No Implementado | No existe registro de autorización de datos ni micro-texto de aceptación de política. |
| **14** | Política de retención y depuración de prospectos | **SÍ** (Ley 1581) | **NO** | No Implementado | Almacén en memoria no tiene TTL, expiración de leads ni función de borrado de datos. |
| **15** | Agendamiento de peritaje / prueba de ruta en Tunja | **SÍ** | **SÍ** | Parcial (Rígido) | Botón en 1-clic agenda cita a +24 horas fija, sin selección de hora ni sede alternativa. |
| **16** | Agendamiento de peritaje para retoma de usados | **SÍ** | **PARCIAL** | Parcial | Detecta intención de retoma en Triage, pero el CRM no captura datos del vehículo del cliente. |
| **17** | Asignación de asesor comercial nominal | **SÍ** | **NO** | No Implementado | No hay nombre ni teléfono de asesor en el perfil del lead; no hay notificación en tiempo real. |
| **18** | Reserva de vehículo con abono de $2.000.000 COP | **SÍ** | **PARCIAL** | Desconectado de Inventario | Cambia el estado del lead a `cierre_reserva`, pero NO congela el vehículo en el catálogo. |
| **19** | Gestión visual de embudo (Pipeline 6 estados) | **SÍ** | **SÍ** | Total (Verificado) | 6 estados coherentes (`captura_t0` a `cierre_reserva`) con Lead Scoring dinámico 0-100. |
| **20** | Escalamiento ante frustración o pedir humano | **SÍ** | **NO** | Brecha en Triage | Frases como *"quiero un humano"* o *"este robot no sirve"* se envían al LLM en vez de escalar. |
| **21** | Escalamiento ante regateo y reclamos jurídicos | **SÍ** | **SÍ** | Total (Verificado) | Escala en 0 tokens ante `"descuento"`, `"Fasecolda"`, `"siniestro"` o `"demanda"`. |
| **22** | Exportador Webhook HMAC a CRM externo | **NO** (Innecesario en Tunja) | **SÍ** | Superfluo (Sobra) | Despachador de webhooks corporativos innecesario para la operación de la vitrina local. |

---

## 4. Evaluación Filosófica de Antoine de Saint-Exupéry

> *"Il semble que la perfection soit atteinte non quand il n'y a plus rien à ajouter, mais quand il n'y a plus rien à retrancher."*  
> — Antoine de Saint-Exupéry, *Terre des hommes* (1939)

La aplicación de este principio al software de BuyaCAR exige eliminar toda la burocracia técnica que añade fricción, tokens y mantenimiento sin aportar una sola venta en Tunja, puliendo al mismo tiempo lo esencial para que sea espartano, eficaz y comercialmente letal.

### 4.1 ¿Qué SOBRA? (Complejidad superflua y burocracia que debe suprimirse)

1. **La duplicidad paralela entre `crm.Task` y `crm.LeadProfile`**:
   - Actualmente coexisten dos mundos en `internal/crm/`: uno modela *Tareas* (`Task`, `TaskType`, `TaskStatus`, `MemoryManager`) y el otro modela *Prospectos* (`LeadProfile`, `FunnelStage`, `IntentScore`, `MemoryLeadStore`).
   - En una vitrina con 3 asesores en Tunja, esta doble contabilidad confunde al operador: el pipeline crea una tarea de "seguimiento comercial" mientras el embudo registra un prospecto en "calificado".
   - **Veredicto Saint-Exupéry**: Debe suprimirse la abstracción pesada de `Task` y consolidar todo en el **Perfil de Prospecto Comercial (`LeadProfile`)**. Menos código, cero duplicidad.

2. **El Despachador Webhook HMAC-SHA256 (`webhook_exporter.go`)**:
   - 203 líneas de código con firmas HMAC, reintentos exponenciales y canal HTTP saliente hacia un hipotético CRM empresarial (HubSpot o Salesforce).
   - BuyaCAR opera con el WhatsApp del asesor de turno y el panel web embebido. Ningún concesionario en Boyacá mantiene un microservicio de ingestión de webhooks con verificación criptográfica.
   - **Veredicto Saint-Exupéry**: Suprimir este despachador o dejarlo como stub dormido. La notificación más rápida y letal es enviar un mensaje al WhatsApp del asesor o activar una alerta sonora/visual en la vitrina.

3. **Creación de Tareas CRM para Consultas Generales de Fast-Path**:
   - En `pipeline.go:696-709`, cuando un cliente pregunta la dirección o el horario y el Fast-Path le responde en 1 ms, el sistema crea una tarea `TaskGeneralInquiry` en el CRM.
   - Esto inunda la base de datos de basura administrativa (ruido blanco). Un cliente que solo preguntó si abrían el sábado no es una tarea para un vendedor.
   - **Veredicto Saint-Exupéry**: Eliminar la creación de tareas por consultas informativas rutinarias resueltas por Fast-Path. El CRM solo debe registrar prospectos con intención de compra real o teléfono capturado.

4. **Inyección forzada de leads ficticios en el Handler de Producción**:
   - En `dashboard_handler.go:827-862`, cada vez que se invoca `GET /api/crm/leads`, el código inyecta 3 prospectos mock ("Carlos Mendoza", "Mariana Silva", "Juan Carlos Pérez").
   - Esto ensucia la pantalla del asesor real con datos falsos de prueba.
   - **Veredicto Saint-Exupéry**: Extirpar los mocks del handler HTTP y dejarlos exclusivamente en los archivos de test (`dashboard_handler_test.go`).

5. **El botón "Asignar Asesor" disparando `"aprobar_descuento"`**:
   - En `assets/index.html:768`, la acción del botón está enmascarada con una decisión ajena.
   - **Veredicto Saint-Exupéry**: Alinear el verbo a la acción real (`asignar_asesor`) y vincularlo al nombre del asesor de turno.

---

### 4.2 ¿Qué FALTA o DEBE PULIRSE? (Para ser espartano y comercialmente letal)

1. **Normalización previa a los Guardrails numéricos (Anti-Bypass de Puntos y Comas)**:
   - Debe limpiarse la puntuación (`.` y `,`) de los números en el texto antes de invocar `reFiveOrMoreDigits`. De lo contrario, un precio como `$150.000.000` evade el filtro anti-alucinación de CP5.
2. **Validación de Años de 4 Dígitos y Kilometrajes Bajos**:
   - Agregar en CP5 la contrastación de números de 4 dígitos cuando corresponden al año del modelo (ej. verificar que `2024` pertenezca al vehículo del catálogo).
3. **Ampliación de Frases Prohibidas en Negociación Colombiana**:
   - Incorporar en `ForbiddenPhrases`: `"papeles gratis"`, `"traspaso gratis"`, `"soat de regalo"`, `"obsequio de papeles"`, `"descuento garantizado"`.
4. **Detección Inmediata de Frustración y Petición de Humano en Triage**:
   - Agregar a los disparadores de escalamiento en `triage.go`: `"humano"`, `"persona"`, `"asesor humano"`, `"no me entiende"`, `"robot"`, `"hablar con alguien"`, escalando en 0 tokens sin pasar por el LLM.
5. **Congelación de Inventario al Reservar ($2M COP)**:
   - Al pulsar "Reservar ($2M)", el vehículo en `inventory.Cache` debe pasar de `Status: "available"` a `Status: "reserved"`, garantizando que ningún otro canal lo ofrezca durante los 5 días de gracia.
6. **Captura de Consentimiento Habeas Data (Ley 1581)**:
   - Incorporar en la respuesta cuando se pide el WhatsApp una línea compacta: *"Al dejarnos tu WhatsApp autorizas el tratamiento de datos según nuestra política en buyacar.com.co/politica"*, y almacenar `HabeasDataConsent: true` en el prospecto.
7. **Campos Específicos para Retoma de Usados**:
   - Cuando `HasTradeIn = true`, el perfil debe guardar la marca, modelo y año del carro que el cliente entrega, para que el evaluador técnico de Tunja tenga la ficha lista antes de la cita de 45 minutos.

---

## 5. Tabla Canónica: Features Discovered

```
## Features Discovered
| # | Category | Feature | Description | Inputs | Outputs | Error Behavior | Discovered Via |
|---|----------|---------|-------------|--------|---------|----------------|----------------|
| 1 | CP5 Guard | Validador Anti-Alucinación Numérica | Bloquea cifras >= 5 dígitos que no figuren en catálogo o líneas oficiales | Texto saliente, catálogo de vehículos | `ValidationResult{Approved: bool, MustEscalate: bool}` | Escala con `hallucinated_number_blocked:<cifra>` | `internal/guard/guard.go:157` |
| 2 | CP5 Guard | Cifras Respaldadas por RAG | Permite cifras normativas (20.000 km garantía, 2.000.000 abono) indexadas | Texto saliente, `extraAllowedNumbers` | `Approved: true` si la cifra está en fixtures RAG | Bloquea si no está en inventario ni RAG | `internal/guard/guard.go:114` |
| 3 | CP5 Guard | Filtro de Frases Prohibidas | Impide promesas de crédito aprobado sin requisitos o precios mínimos | Texto normalizado (minúsculas, sin tildes) | Rechazo pre-envío | Escala con `forbidden_phrase_detected:<frase>` | `internal/guard/guard.go:124` |
| 4 | CP5 Guard | Allowlist de Enlaces HTTPS | Solo permite enlaces seguros a dominios autorizados (`buyacar.com.co`) | URLs detectadas en la respuesta | Rechazo si es HTTP o dominio ajeno | Escala con `unauthorized_url_domain:<host>` | `internal/guard/guard.go:136` |
| 5 | CP5 Guard | Recorte Seguro de Caracteres | Ajusta longitud al límite del canal sin cortar precios ni teléfonos | Texto, límite de canal (`ChannelCharLimits`) | Texto recortado en espacio seguro | Escala si recortar mutila una cifra | `internal/guard/guard.go:233` |
| 6 | CP5 Guard | Filtro de Ruido de Marketplace | Limpia saludos basura de Meta ("¿sigue disponible?", "respuesta rápida") | Texto entrante de Marketplace | "¿Está disponible?" limpio | Retorna texto recortado si no coincide | `internal/guard/guard.go:94` |
| 7 | Ley 1581 | Redacción de Correos Electrónicos | Oculta correos en trazas, logs y contexto de IA para proteger privacidad | Cadena de texto | `[PII_EMAIL_REDACTED]` | Conserva el resto del texto intacto | `internal/guard/guard.go:80` |
| 8 | Ley 1581 | Redacción de Cédulas y Tarjetas | Oculta números de 7 a 16 dígitos preservando líneas oficiales de BuyaCAR | Cadena de texto | `[PII_NUM_REDACTED]` | Mantiene números de `OfficialPhoneNumbers` | `internal/guard/guard.go:84` |
| 9 | Ley 1581 | Sanitización en Cola de Escalamiento | Asegura que los tickets para asesores humanos no almacenen PII residual | Mensaje original y contexto | Ticket con `RedactedContext` sanitizado | Encola con éxito en estructura thread-safe | `internal/escalate/escalate.go:30` |
| 10 | Ley 1581 | Sanitización en Trazas de Checkpoints | Redacta datos sensibles en los mensajes de error de los 7 checkpoints | `rawErr` string | `SanitizedError` en traza JSON | Exporta sin PII a `production_artifacts/traces/` | `internal/obs/obs.go:50` |
| 11 | Ley 1581 | Sanitización en Prompts de IA | Limpia PII del resumen del historial antes de enviarlo a Gemini/Ollama | `CompactContext.Summary` | Prompt delimitado sin PII del cliente | Previene fuga de datos hacia proveedores de IA | `internal/ai/providers.go:40` |
| 12 | CRM Tunja | Extracción de Teléfono Colombiano | Extrae celulares colombianos (10 dígitos, inicia en 3) para enrutar a WhatsApp | Texto del mensaje | Número celular limpio (ej. `3109876543`) | Retorna cadena vacía si no detecta celular | `internal/crm/lead_scoring.go:27` |
| 13 | CRM Tunja | Lead Scoring Predictivo en Vivo | Calcula puntaje de intención de 0 a 100 y ubica al lead en el embudo | Texto, título de anuncio, interacciones | `score int`, `stage FunnelStage` | Tope máximo 100 puntos | `internal/crm/lead_scoring.go:62` |
| 14 | CRM Tunja | Embudo Comercial de 6 Fases | Modela el ciclo comercial: Captura, Calificado, Cita, Negociación, Reserva, Descartado | Eventos de conversación y decisiones | Estado `FunnelStage` actualizado | Permite transiciones bidireccionales | `internal/crm/lead_scoring.go:12` |
| 15 | CRM Tunja | Almacén de Prospectos Concurrente | Almacena y actualiza perfiles de prospectos en memoria con `sync.RWMutex` | `LeadProfile` struct | `LeadProfile` persistido/actualizado | Thread-safe ante lecturas y escrituras | `internal/crm/lead_scoring.go:120` |
| 16 | CRM Tunja | Decisión en 1-Clic: Agendar Cita | Asigna sede en Tunja (`Av. Oriental`) y cita de prueba de ruta a 24 horas | `POST /api/crm/lead/decision` (`agendar_cita`) | Lead en `cita_peritaje` con fecha y sede | Retorna 400 ante payload inválido | `internal/server/dashboard_handler.go:903` |
| 17 | CRM Tunja | Decisión en 1-Clic: Asignar Asesor | Transfiere prospecto calificado a negociación humana personalizada | `POST /api/crm/lead/decision` (`asignar_asesor`) | Lead en `negociacion_humana` | No cambia datos si el prospecto no existe | `internal/server/dashboard_handler.go:908` |
| 18 | CRM Tunja | Decisión en 1-Clic: Reservar ($2M) | Marca el prospecto como cerrado/ganado con apartado formal | `POST /api/crm/lead/decision` (`cerrar_reserva`) | Lead en `cierre_reserva` | No actualiza el inventario vehicular | `internal/server/dashboard_handler.go:910` |
| 19 | CRM Tunja | Inferencia de Tareas desde Triage | Deduce automáticamente la tarea comercial a partir del Fast-Path (0 tokens) | `triage.Intent`, `hasVehicle bool` | `TaskType`, `description string` | Asigna `general_inquiry` por defecto | `internal/crm/crm.go:212` |
| 20 | CRM Tunja | Normalización de Tipos de Tareas | Homologa cadenas arbitrarias de LLMs a tipos canónicos del CRM | Cadena de texto arbitraria | `TaskScheduleTestDrive`, `TaskCreditApplication`, etc. | `TaskGeneralInquiry` si no hay coincidencia | `internal/crm/crm.go:197` |
| 21 | CRM Tunja | Alertas de Lista de Espera | Registra tarea comercial cuando ingresa un vehículo solicitado previamente | Datos de cliente y vehículo | Tarea persistida en `MemoryManager` | Error si `ConvID` está vacío | `internal/crm/crm.go:113` |
| 22 | CRM Tunja | Exportador Webhook HMAC-SHA256 | Despacha tareas al CRM externo con firma criptográfica y backoff | Tarea comercial, endpoint y secreto HMAC | Cabeceras `X-BuyaCAR-Signature-256`, HTTP 200 | Reintenta 3 veces; deriva a `escalate.Queue` | `internal/crm/webhook_exporter.go:94` |
| 23 | CRM Tunja | Autenticación Segura de Consola CRM | Protege la consola de asesores exigiendo clave administrativa | `POST /api/crm/login` (`password`) | Token de sesión `byc_sess_<ts>` | Retorna 401 si la credencial es errónea | `internal/server/dashboard_handler.go:785` |
| 24 | CRM Tunja | Escalamiento por Descuento / Regateo | Triage determinista deriva solicitudes de rebaja a asesores humanos | Frases: "lo menos", "precio mínimo", "rebaja" | `IntentDiscount`, `MustEscalate: true` | Pasa a cola humana sin consultar LLM | `internal/triage/triage.go:78` |
| 25 | CRM Tunja | Escalamiento por Permuta / Retoma | Triage deriva solicitudes de retoma para coordinar peritaje de 45 min | Frases: "permuta", "retoma", "reciben mi carro" | `IntentTradeIn`, `MustEscalate: true` | Pasa a cola humana sin consultar LLM | `internal/triage/triage.go:89` |
| 26 | CRM Tunja | Escalamiento por Centrales de Riesgo | Triage deriva consultas de Datacrédito y reportes a estudio manual | Frases: "datacredito", "reportado", "centrales" | `IntentCreditStudy`, `MustEscalate: true` | Pasa a cola humana sin consultar LLM | `internal/triage/triage.go:100` |
| 27 | CRM Tunja | Escalamiento por Reclamos y Siniestros | Triage deriva quejas, demandas y siniestros Fasecolda a gerencia | Frases: "siniestro", "fasecolda", "reclamo", "demanda" | `IntentLegalOrClaim`, `MustEscalate: true` | Pasa a cola humana sin consultar LLM | `internal/triage/triage.go:111` |
| 28 | CRM Tunja | Escalamiento por Prompt Injection | Neutraliza ataques de inyección de instrucciones derivando a seguridad | Frases: "olvida tus instrucciones", "system prompt" | `IntentPromptInject`, `MustEscalate: true` | Bloqueo con confianza 0.99 | `internal/triage/triage.go:65` |
| 29 | CRM Tunja | Escalamiento por Vehículo Vendido | Responde comercialmente y escala si el carro consultado ya no está libre | Vehículo con `Status != "available"` | `SoldVehicleNotice`, `MustEscalate: true` | Escala con `vehicle_sold_or_reserved` | `internal/fast/fast.go:92` |
| 30 | CRM Tunja | Escalamiento por Fallo de Envíos | Detecta caídas de canal o falta de verificación y alerta a asesores | Estado de envío en canal (`SendFailed`) | Error registrado, ticket en `escalate.Queue` | Activa auto-pausa de canal tras fallos | `internal/core/pipeline.go:482` |
```

---

## 6. Tabla Canónica: Edge Cases

```
## Edge Cases
| # | Feature | Input | Observed Behavior |
|---|---------|-------|-------------------|
| 1 | Anti-Alucinación Numérica CP5 | Precio formateado con puntos: `"Te queda en $150.000.000 COP"` | `\b\d{5,}\b` no hace match porque los puntos fragmentan la cadena en tokens de 3 dígitos ("150", "000", "000"). El guardrail CP5 no bloquea la cifra y el precio inventado se envía al cliente. |
| 2 | Anti-Alucinación Numérica CP5 | Precio formateado con comas: `"Precio final: $140,000,000"` | Mismo comportamiento: las comas actúan como separadores de palabras; ningún bloque alcanza 5 dígitos. Evade el guardrail. |
| 3 | Validación de Años de Modelo | IA alucina año inexistente: `"Toyota Hilux modelo 2026 en vitrina"` | El año `2026` tiene 4 dígitos. CP5 solo monitorea números $\ge 5$ dígitos, permitiendo que la IA atribuya un año inventado al vehículo. |
| 4 | Validación de Kilometraje | IA alucina kilometraje bajo: `"Camioneta con solo 4500 km certificados"` | `4500` tiene 4 dígitos. El validador numérico de CP5 lo ignora por ser $< 5$ dígitos; no valida si el carro realmente tiene 42.000 km. |
| 5 | Promesas Prohibidas | IA promete trámites gratis: `"Te regalamos el soat y el traspaso es gratis"` | No coincide con ninguna de las 6 frases de `ForbiddenPhrases`. La respuesta es aprobada por CP5 y despachada al cliente. |
| 6 | Redacción de PII (Ley 1581) | Cédula colombiana formateada: `"Mi cédula es 1.049.521.890 de Tunja"` | Regex `\b\d{7,16}\b` busca dígitos continuos. Los puntos rompen la expresión; la cédula se filtra sin ofuscar a logs y trazas. |
| 7 | Redacción de PII (Ley 1581) | Teléfono con guiones: `"Mi número es 310-456-7890"` | `\b\d{7,16}\b` se divide en bloques "310", "456", "7890" (ninguno $\ge 7$). El teléfono personal no es ofuscado por `RedactPII`. |
| 8 | Triage de Frustración | Cliente enojado: `"Este bot no entiende nada, quiero hablar con una persona"` | No coincide con disparadores de escalamiento; se clasifica como `IntentComplexQuery` (confianza 0.55). Se envía al LLM en vez de pasar a un asesor humano. |
| 9 | Reserva Comercial ($2M) | Vendedor presiona "Reservar ($2M)" en consola web para Toyota Fortuner | Se actualiza el lead a `cierre_reserva`, pero el vehículo en `inventory.Cache` sigue en `"available"`. Otros clientes que pregunten recibirán confirmación de disponibilidad. |
| 10 | Agendamiento de Citas | Asesor pulsa "Agendar Cita" el viernes a las 5:00 PM | Se agenda automáticamente para el sábado a las 5:00 PM (`now + 24h`). El concesionario cierra los sábados a las 2:00 PM; la cita queda fijada fuera de horario de atención. |
| 11 | Recorte Seguro de Longitud | Respuesta en TikTok de 405 caracteres donde el char 400 corta a la mitad `$185000000` | `safeTrimWithoutCuttingNumbers` detecta que el número resultante no está en `allowedNumbers` ni intacto; aborta el envío y fuerza `MustEscalate: true`. |
| 12 | Persistencia de Mocks en CRM | Operador consulta `GET /api/crm/leads` en el panel de control | El backend inserta forzosamente los 3 leads ficticios de prueba en `MemoryLeadStore`, mezclando prospectos simulados con clientes reales de Tunja. |
| 13 | Fallo de Webhook CRM Externo | Servidor CRM externo no disponible tras 3 reintentos con backoff | El despachador agota reintentos (`ErrExportExhausted`) y deriva automáticamente una alerta a `escalate.Queue` para que el ticket sea atendido localmente. |
| 14 | Consulta Informativa Fast-Path | Cliente saluda: `"Hola, buenos días"` | Fast-Path responde en 1 ms con 0 tokens. El pipeline ejecuta `settleCRMTasks` y crea una tarea `TaskGeneralInquiry` en el CRM, saturando la base de datos de ruido administrativo. |
```

---

## 7. Plan de Acción y Recomendaciones para BuyaCAR Tunja

Para transformar este diagnóstico en un incremento de valor concreto sin incurrir en deuda técnica ni scope creep, se recomienda ejecutar las siguientes acciones:

1. **Parche Inmediato de Robustecimiento en CP5 (`guard.go`)**:
   - Pre-procesar el texto antes de extraer cifras de 5+ dígitos: normalizar separadores de miles (`150.000.000` $\rightarrow$ `150000000`) para que ningún precio inventado burle el guardrail.
   - Normalizar de igual forma antes de `RedactPII` para que cédulas y teléfonos formateados (`1.049.521.890`, `310-456-7890`) sean ofuscados al 100%.
2. **Ampliación de `ForbiddenPhrases`**:
   - Añadir: `"traspaso gratis"`, `"papeles gratis"`, `"soat de regalo"`, `"obsequio de papeles"`, `"descuento garantizado"`.
3. **Escalamiento Inmediato por Frustración en Triage (`triage.go`)**:
   - Incluir las palabras: `"humano"`, `"persona"`, `"asesor humano"`, `"no me entiende"`, `"robot"`, `"hablar con alguien"` como causal directa de `MustEscalate: true` (0 tokens).
4. **Saneamiento Minimalista del CRM (Saint-Exupéry)**:
   - Eliminar los mocks automáticos en `dashboard_handler.go:827-862`.
   - Conectar la decisión "Reservar ($2M)" con el catálogo vehicular: cuando un lead se marca como separado, invocar `cache.MarkVehicleReserved(vehicleID)`.
   - Suspender la creación de tareas en el CRM para saludos o consultas rutinarias resueltas por Fast-Path.
5. **Cumplimiento Legal Ley 1581**:
   - Incorporar en el struct `LeadProfile` el campo `HabeasDataConsent: bool` y registrar la marca de tiempo de autorización cuando el cliente entrega su WhatsApp tras el aviso legal.

---
*Reporte finalizado y verificado contra el código fuente de BuyaCAR Omnicanal.*
