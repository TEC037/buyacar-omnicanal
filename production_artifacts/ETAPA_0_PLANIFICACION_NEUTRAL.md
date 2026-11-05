# ETAPA 0: DESCUBRIMIENTO Y PLANIFICACIÓN NEUTRAL
## Base de Conocimiento para Automatización de Comunicaciones Empresariales

- **Fecha de elaboración / verificación**: 2026-10-24
- **Modo operativo**: `SIMULATION` / Planificación Neutral (Sin ejecución técnica, sin selección de stack, sin credenciales)
- **Rol del agente**: Agente de Planificación (Etapa 0)
- **Estado de decisión tecnológica**: Abierta / Neutral (No se impone ni recomienda proveedor, infraestructura o canal)

---

## 1. Objetivo y Límites de la Etapa 0

### Objetivo
Construir una base de conocimiento neutral, exhaustiva y estructurada que permita a un futuro agente de decisión o arquitectura recibir los requerimientos específicos del producto, evaluar opciones tecnológicas e implementar la solución óptima sin sesgos previos.

### Límites Estrictos
1. **Cero ejecución técnica**: No se configuran servicios, no se conectan APIs, no se crean cuentas, no se escriben automatizaciones funcionales.
2. **Cero imposición de stack**: No se asume ni se recomienda ningún proveedor específico (ej. Google Apps Script, Make, Zapier, n8n, WhatsApp Cloud API, Twilio, Resend, Sendgrid, AWS, GCP, Azure, etc.).
3. **Cero decisiones irreversibles**: Todas las arquitecturas, canales e infraestructuras se presentan con ventajas, restricciones, riesgos y costos comparativos.
4. **Cero PII o credenciales**: No se solicitan ni almacenan datos personales reales ni llaves criptográficas.

---

## 2. Glosario de Conceptos Relevantes

- **Event-Driven Architecture (EDA)**: Paradigma donde el flujo del sistema está determinado por la emisión, captura y procesamiento de eventos discretos en tiempo real.
- **Webhook**: Mecanismo HTTP mediante el cual una aplicación envía una carga de datos a otra URL receptora de forma asíncrona ante la ocurrencia de un evento.
- **Fast-Path Determinista**: Ruta de procesamiento basada en reglas fijas, expresiones regulares o coincidencia exacta que responde sin invocar modelos probabilísticos (IA/LLM).
- **Idempotencia**: Propiedad de una operación que garantiza que múltiples ejecuciones con el mismo identificador o carga útil producen exactamente el mismo resultado y estado que una sola ejecución, evitando duplicados.
- **Ventana de Atención (Messaging Window)**: Restricción temporal impuesta por proveedores de mensajería (frecuentemente 24 horas tras el último mensaje entrante del usuario) fuera de la cual no se permite el envío de mensajes libres sin plantillas aprobadas.
- **Opt-in / Consentimiento Expreso**: Autorización verificable, previa e informada otorgada por el titular para recibir comunicaciones por un canal específico.
- **Escalamiento Humano (Human-in-the-loop / Handoff)**: Mecanismo de transferencia de la conversación o tarea desde un flujo automatizado a un operador humano ante ambigüedad, solicitud explícita, riesgo o fallo técnico.
- **Prompt Injection (Directo e Indirecto)**: Vulnerabilidad de seguridad donde datos de entrada no confiables alteran las instrucciones operativas del modelo de lenguaje.
- **Allowlist / Lista de Permisiones**: Control de seguridad que restringe destinos de red (URLs), orígenes de datos o formatos de entrada exclusivamente a aquellos previamente autorizados y verificados.
- **Circuit Breaker**: Patrón de resiliencia que interrumpe temporalmente las llamadas a un servicio externo que presenta fallos reiterados para evitar saturación de recursos y degradación en cascada.

---

## 3. Mapa de Tendencias

### T-01: Automatización Basada en Eventos y Webhooks
- **Hecho o tendencia**: Tendencia consolidada.
- **Aplica cuando**: Se requiere procesamiento en tiempo casi real con baja latencia y eliminación de sondeos periódicos (polling).
- **Capacidades**: Reacción inmediata a cambios de estado, desacoplamiento entre emisor y receptor.
- **Limitaciones**: Requiere endpoints públicos de alta disponibilidad, manejo de reintentos agresivos del emisor y deduplicación.
- **Riesgos**: Avalanchas de peticiones (thundering herd), saturación de colas y pérdida de eventos ante caídas sin almacenamiento persistente.
- **Costos/Cuotas**: Generalmente ligados a peticiones HTTP y ancho de banda de la infraestructura receptora.

### T-02: Comunicación Omnicanal con Estado Unificado
- **Hecho o tendencia**: Tendencia en evolución.
- **Aplica cuando**: El usuario interactúa indistintamente a través de múltiples puntos de contacto (chat, correo, mensajería instantánea).
- **Capacidades**: Persistencia del hilo conversacional y del contexto del cliente independientemente del canal utilizado.
- **Limitaciones**: Identificación unívoca del usuario a través de diferentes identificadores (número telefónico, email, ID de red social).
- **Riesgos**: Confusión de contexto si un identificador es compartido por varios usuarios físicos.

### T-03: Agentes Híbridos (Fast-Path Determinista + LLM + Humano)
- **Hecho o tendencia**: Tendencia dominante en arquitecturas modernas.
- **Aplica cuando**: Existe un alto volumen de consultas frecuentes y repetitivas junto a consultas complejas, abiertas o transaccionales.
- **Capacidades**: Reducción drástica del consumo de tokens y costo mediante respuestas instantáneas en consultas predecibles, reservando la IA para comprensión semántica.
- **Limitaciones**: Requiere categorización y calibración estricta de umbrales de confianza para evitar fallos de enrutamiento.
- **Riesgos**: Alucinaciones del modelo en consultas no cubiertas por Fast-Path o escalamiento tardío a humanos.

### T-04: Trazabilidad, Auditoría y Observabilidad Granular
- **Hecho o tendencia**: Hecho técnico y exigencia regulatoria.
- **Aplica cuando**: Se procesan comunicaciones comerciales o de soporte que implican compromisos de servicio, ventas o datos sensibles.
- **Capacidades**: Registro cronológico de checkpoints (ingreso, clasificación, sanitización, llamada a IA, respuesta, entrega).
- **Limitaciones**: Incremento en almacenamiento y procesamiento de telemetría.
- **Riesgos**: Filtración involuntaria de PII en los logs de auditoría si no se aplica sanitización previa al almacenamiento.

### T-05: Reducción de Infraestructura mediante Modelos Serverless y Gestionados
- **Hecho o tendencia**: Tendencia establecida.
- **Aplica cuando**: Se busca minimizar la carga operativa de mantenimiento de servidores y escalar a cero en periodos de inactividad.
- **Capacidades**: Autoescalado horizontal instantáneo, facturación por uso efectivo.
- **Limitaciones**: Tiempos de arranque en frío (cold starts), límites de tiempo de ejecución (máx. 5-15 min según plataforma).
- **Riesgos**: Bloqueo con el proveedor de nube (vendor lock-in) y facturación impredecible ante picos masivos de tráfico.

---

## 4. Mapa de Patrones Funcionales

| Patrón | Propósito | Entradas | Salidas | Controles Obligatorios |
|---|---|---|---|---|
| **P-01: Captura y Validación** | Recepción de la carga útil del canal y verificación de integridad. | Payload HTTP / Evento | Payload tipado y sanitizado | Validación de esquema JSON/XML, verificación de firmas criptográficas (HMAC), control de tamaño de carga. |
| **P-02: Idempotencia y Deduplicación** | Prevenir procesamiento múltiple de un mismo mensaje. | ID de mensaje / Hash | Estado de procesamiento | Registro atómico de `message_id` con TTL en almacenamiento clave-valor. |
| **P-03: Sanitización de PII** | Ofuscar o eliminar datos personales antes de registrar o enviar a IA. | Texto bruto | Texto sanitizado | Reglas de detección de patrones (cédulas, tarjetas, teléfonos, correos) y tokenización si aplica. |
| **P-04: Triage y Clasificación** | Determinar intención, urgencia y ruta de atención. | Texto sanitizado | Intención + Nivel de Confianza | Umbral mínimo de confianza; si no supera el corte, derivar a ruta segura o humana. |
| **P-05: Fast-Path Determinista** | Responder consultas estándar sin invocar modelos generativos. | Intención validada | Plantilla de respuesta estructurada | Verificación de datos de origen (horarios, ubicación, inventario verificado en caché). |
| **P-06: Consulta a IA con Fallback** | Generar respuesta asistida para consultas complejas. | Prompt estructurado + Contexto | Respuesta validada sintácticamente | JSON Schema forzado en salida (Structured Output), timeouts estrictos, proveedor secundario de respaldo. |
| **P-07: Guard Anti-Alucinación** | Validar que la respuesta no invente datos ni URLs prohibidas. | Respuesta propuesta | Respuesta aprobada o rechazada | Coincidencia estricta contra base de datos oficial; allowlist estricta de dominios HTTPS. |
| **P-08: Escalamiento Humano** | Transferir la conversación a un operador de soporte. | Contexto conversacional + Razón | Ticket / Conversación asignada | Notificación al operador, cambio de estado de la conversación y bloqueo de respuestas automáticas competidoras. |
| **P-09: Despacho y Reintentos con Backoff** | Entrega del mensaje saliente al canal correspondiente. | Mensaje saliente + Destino | Confirmación de envío (`message_sid`) | Respeto a ventanas de atención (24h), backoff exponencial con jitter, límite máximo de reintentos (ej. 3). |
| **P-10: Cierre y Observabilidad** | Finalizar ciclo y emitir métricas. | Trazas de checkpoints | Métrica registrada en dashboard | Cero PII en logs, cálculo de latencia de ciclo y cómputo de tokens consumidos. |

---

## 5. Mapa Comparativo Neutral de Canales de Comunicación

| Canal | Capacidades | Limitaciones | Restricciones de Plantilla y Políticas | Ventanas de Atención | Latencia Típica | Dependencia y Portabilidad |
|---|---|---|---|---|---|---|
| **Correo Electrónico (SMTP/API)** | Mensajes largos, formato HTML enriquecido, adjuntos estructurados. | Entregabilidad sujeta a reputación de dominio (SPF, DKIM, DMARC), filtros de spam. | Normativas anti-spam (CAN-SPAM, GDPR), enlace de desuscripción obligatorio. | Sin ventana estricta; asíncrono por naturaleza. | 1 a 60 segundos (depende de servidores receptores). | Alta portabilidad (fácil cambio de proveedor como Sendgrid, Mailgun, Amazon SES). |
| **Mensajería Instantánea (WhatsApp, Telegram, etc.)** | Alta tasa de apertura, soporte multimedia interactivo (botones, listas). | Políticas de uso muy estrictas contra spam y ventas directas no autorizadas. | Fuera de ventana se exige plantilla previamente aprobada por el proveedor (WhatsApp). | 24 horas desde el último mensaje entrante del usuario (estándar Meta). | < 2 segundos (tiempo casi real). | Baja portabilidad; dependencia crítica de APIs propietarias y procesos de verificación empresarial. |
| **SMS** | Alcance universal sin requerir conexión a internet de datos por parte del receptor. | Máximo 160 caracteres por segmento GSM-7 (o 70 en Unicode), sin interactividad visual directa. | Requisitos regulatorios estrictos por país (10DLC en EE.UU., registro de remitente alfanumérico en LATAM). | Sin ventana formal, pero limitado por leyes de contacto comercial (horarios hábiles). | 2 a 15 segundos. | Media a alta; fácil cambio entre agregadores (Twilio, Infobip, Sinch). Costo unitario significativamente superior. |
| **Chat Web / In-App** | Control 100% sobre interfaz, datos de contexto de sesión del navegador, sin costo por mensaje de red de telecomunicaciones. | Requiere que el usuario permanezca en la página web o mantenga la pestaña activa. | Políticas de cookies y aviso de privacidad de navegación. | Sesión activa del usuario o persistencia local en almacenamiento web. | < 500 ms (WebSockets / Server-Sent Events). | Total independencia si es desarrollo propio; media si usa software de terceros. |
| **Formularios Web** | Estructuración rígida de datos de entrada desde el origen, validación en cliente y servidor. | Comunicación unidireccional entrante; no permite conversación fluida en tiempo real. | Verificación de consentimientos y captchas contra ataques automatizados. | Asíncrono; la respuesta debe enviarse por otro canal (correo o mensajería). | Depende del canal de respuesta. | Alta independencia y portabilidad. |
| **Voz (Telefonía / VoIP)** | Máxima cercanía y resolución rápida de problemas complejos. | Alto costo de infraestructura y procesamiento (STT/TTS o agentes humanos), propensa a ruido ambiental. | Grabación sujeta a consentimiento expreso en la llamada. | Conexión síncrona en tiempo real. | Tiempo real síncrono. | Media a baja dependencia según carrier SIP o proveedor de telefonía en la nube. |

---

## 6. Mapa Comparativo Neutral de Opciones de Infraestructura

| Categoría | Problemas que Resuelve | Infraestructura Requerida | Conocimiento Exigido | Modelo de Costos | Límites y Restricciones | Riesgos Técnicos |
|---|---|---|---|---|---|---|
| **Herramientas No-Code / Low-Code** | Prototipado ultrarrápido, flujos visuales sin programación profunda. | Ninguna (SaaS gestionado). | Configuración lógica y mapeo de datos. | Suscripción mensual + cobro por operación/paso ejecutado. | Límites estrictos de operaciones por minuto, difícil testing automatizado y CI/CD. | Vendor lock-in absoluto, costos exorbitantes a gran escala, opacidad en logs. |
| **Scripts en Suites de Productividad (ej. Apps Script)** | Automatización ad-hoc vinculada a hojas de cálculo y documentos de oficina. | Entorno de ejecución serverless del proveedor. | JavaScript básico. | Gratuito dentro de la suite, o incluido en la licencia ofimática. | Tiempo máximo de ejecución (ej. 6 minutos por ejecución), cuotas diarias de envíos de correo estrictas. | No apto para cargas de producción críticas ni alta concurrencia; falta de control de versiones robusto. |
| **Servicios Serverless (FaaS / Cloud Functions)** | Ejecución elástica bajo demanda, cero gestión de servidores, autoescalado. | Nube pública (AWS, GCP, Azure, Cloudflare, etc.). | Desarrollo de software, gestión de la nube e IaC. | Pago por milisegundo de ejecución y volumen de memoria/CPU. | Tiempos de ejecución limitados, arranque en frío (cold start), gestión compleja de conexiones a BD. | Complejidad en pruebas locales integradas y costos imprevistos ante bucles de eventos. |
| **Orquestadores y Automatización Autoalojada** | Control total sobre datos y flujos, reducción de costos por ejecución en alto volumen. | Servidor virtual (VPS) o contenedores (Docker, Kubernetes). | Sysadmin, Docker, redes, seguridad de servidores y backup. | Costo fijo de servidor/hosting + mantenimiento. | Limitado por el hardware del servidor provisto; escalado no automático si no se configura. | Responsabilidad total sobre parches de seguridad, disponibilidad y copias de respaldo. |
| **Aplicaciones Modulares / Monolitos Modernos (Go, Node, Python, Java)** | Máximo rendimiento, baja latencia, pruebas unitarias exhaustivas y tipado estricto. | Servidor o contenedor estándar. | Ingeniería de software avanzada, arquitectura de sistemas. | Costo de hosting/cómputo y horas de desarrollo. | Requiere despliegue e infraestructura de monitoreo propia. | Mayor tiempo inicial de desarrollo frente a plataformas no-code. |
| **Arquitecturas Orientadas a Eventos y Colas (Kafka, RabbitMQ, SQS)** | Desacoplamiento total, absorción de picos de carga (backpressure) y resiliencia. | Clúster de mensajería gestionado o autoalojado. | Diseño de sistemas distribuidos y consistencia eventual. | Costo de instancias o precio por mensaje/operación de cola. | Complejidad operativa añadida; procesamiento asíncrono no siempre aplicable a chat síncrono. | Dificultad para rastrear flujos distribuidos sin tracing distribuido (OpenTelemetry). |

---

## 7. Costos y Límites que Deben Investigarse antes de Decidir

1. **Mensajería Instantánea Empresarial**:
   - Tarifas por conversación iniciada por el negocio vs. iniciada por el usuario (según país de destino).
   - Costo de verificación de cuenta oficial (Green Badge) o cargos de activación de agregadores (ISVs / BSPs).
2. **Servicios de Correo Transaccional**:
   - Costo por millar de correos enviados.
   - Costo de asignación de IP dedicada (necesaria para grandes volúmenes para no compartir reputación).
3. **Plataformas de Modelos de Lenguaje (IA)**:
   - Costo por 1.000 tokens de entrada (Prompt) y salida (Completion), discriminando por modelo (pequeño/rápido vs. grande/avanzado).
   - Costo de infraestructura para modelos autoalojados (Ollama/vLLM) en servidores con GPU vs. APIs gestionadas.
   - Cuotas de peticiones por minuto (RPM) y tokens por minuto (TPM).
4. **Infraestructura de Cómputo y Almacenamiento**:
   - Costo de transferencia de datos de salida (Data Egress) entre nube y servicios externos.
   - Costo de almacenamiento transaccional y retención de registros de auditoría (logs).

---

## 8. Requisitos de Privacidad, Consentimiento y Gobernanza

1. **Marco Legal y Cumplimiento**:
   - **Colombia**: Ley Estatutaria 1581 de 2012 y Decreto 1377 de 2013 (Habeas Data). Obligatoriedad de autorización previa, expresa e informada del titular, y Registro Nacional de Bases de Datos (RNBD) si aplica.
   - **Internacional**: GDPR (Europa) si se atienden ciudadanos europeos; LGPD (Brasil); regulaciones CAN-SPAM (EE.UU.).
2. **Políticas de Retención y Supresión**:
   - Definir plazos máximos para conservar transcripciones completas de conversaciones.
   - Procedimiento automatizado o documentado para procesar solicitudes de supresión de datos (derecho al olvido).
3. **Sanitización de Datos en Tránsito y Reposo**:
   - Prohibición estricta de enviar datos personales a proveedores de IA de terceros sin acuerdos de procesamiento de datos (DPA) que aseguren que los datos no se usarán para entrenar modelos públicos.
   - Cifrado en tránsito (TLS 1.3) y en reposo (AES-256).

---

## 9. Riesgos Operativos y Controles Posibles

| Riesgo Operativo | Causa Raíz Potencial | Control Arquitectónico Posible |
|---|---|---|
| **Duplicación de Mensajes** | Reintentos automáticos de webhooks ante respuestas lentas del servidor. | Tabla de idempotencia con clave única (`message_id` + hash) y bloqueo distribuido. |
| **Alucinación o Información Errónea** | Respuestas no aterrizadas del modelo generativo sobre precios o disponibilidad. | Guardrails deterministas post-IA; consultas exclusivas a bases de datos con stock verificado; structured output forzado. |
| **Prompt Injection** | Contenido del usuario que incluye comandos para alterar el comportamiento del sistema. | Separación estricta de datos del usuario dentro de delimitadores XML/Markdown; validación con modelo clasificador previo. |
| **Bloqueo de Número / Cuenta por Spam** | Envío de mensajes no solicitados o reporte masivo de usuarios en canales cerrados. | Verificación estricta de Opt-in; respeto estricto de ventanas de 24h; inclusión de opción explícita de "Detener / Dar de baja". |
| **Costos Inesperados por Bucles** | Bucle infinito de respuestas entre dos bots automatizados. | Detección de bucles (límite de mensajes por conversación en periodo corto); detector de autoría bot. |
| **Agotamiento de Cuotas de API** | Tráfico inesperado o ataques de denegación de servicio (DoS). | Limitadores de tasa locales (Rate Limiting por IP y por número de teléfono); colas con estrangulamiento (throttling). |

---

## 10. Variables que el Producto Deberá Proporcionar al Futuro Agente Decisor

El agente que diseñe la arquitectura requerirá obligatoriamente los siguientes parámetros del negocio:

1. **Volumen Proyectado**: Mensajes entrantes por día, picos estimados por hora (QPS requerido).
2. **Distribución de Canales**: Porcentajes de interacción previstos por WhatsApp, Web Chat, Correo o Redes Sociales.
3. **Presupuesto Financiero**: Presupuesto de inversión inicial (CAPEX) vs. presupuesto operativo mensual máximo (OPEX).
4. **Capacidades del Equipo de Mantenimiento**: Nivel de experiencia técnica disponible para soporte del sistema (desarrolladores de software, administradores de sistemas, o personal sin conocimientos de programación).
5. **Nivel de Autonomía Requerido**: Porcentaje de conversaciones que deben resolverse automáticamente sin intervención humana frente a las que requieren supervisión.
6. **Requisitos de Latencia**: Tiempo máximo tolerable para entregar una respuesta (ej. < 3 segundos para chat; < 5 minutos para correo).
7. **Sensibilidad de los Datos**: Naturaleza de los datos manipulados (datos públicos de inventario vs. datos financieros o documentos de identidad).

---

## 11. Preguntas que Deben Responderse antes de Seleccionar la Arquitectura

1. ¿El negocio cuenta actualmente con cuentas oficiales y verificadas en los canales clave o debe iniciar el proceso de verificación corporativa?
2. ¿Existe un sistema de inventario o CRM preexistente con API documentada, o los datos se gestionarán de forma centralizada en esta misma solución?
3. ¿Cuál es el procedimiento y canal definido para el equipo de operadores humanos cuando se activa un escalamiento?
4. ¿Se exige soberanía de datos estricta (datos alojados dentro del país o en infraestructura propia) o se permite el uso de nubes públicas globales?
5. ¿Qué nivel de disponibilidad (SLA) se exige al sistema (99.0%, 99.9%, o mejor esfuerzo)?

---

## 12. Estructura Propuesta de la Base de Conocimiento

La base de conocimiento se estructura en el workspace bajo el siguiente esquema neutral:

```text
knowledge_base/
├── README.md                          # Guía de consulta para agentes posteriores
├── trends/                            # Entradas de tendencias tecnológicas
│   ├── TR-001_event_driven.json
│   ├── TR-002_hybrid_agents.json
│   └── TR-003_token_economy.json
├── patterns/                          # Patrones funcionales y de seguridad
│   ├── PT-001_idempotency.json
│   ├── PT-002_pii_sanitization.json
│   └── PT-003_anti_hallucination.json
├── channels/                          # Fichas técnicas neutrales de canales
│   ├── CH-001_email.json
│   ├── CH-002_instant_messaging.json
│   ├── CH-003_webchat.json
│   └── CH-004_sms.json
├── infrastructure/                    # Opciones de hosting y ejecución
│   ├── INF-001_nocode_managed.json
│   ├── INF-002_serverless.json
│   └── INF-003_modular_code.json
├── risks/                             # Catálogo de riesgos y controles
│   └── RSK-001_operational_risks.json
└── decision_criteria/                 # Variables y cuestionarios para agentes
    └── CRIT-001_stack_evaluation.json
```

---

## 13. Modelo de Datos para Representar Conocimiento

Cada entidad de conocimiento se almacena conforme al siguiente esquema JSON estandarizado:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": [
    "id",
    "tipo",
    "titulo",
    "descripcion",
    "hecho_o_tendencia",
    "aplica_cuando",
    "capacidades",
    "limitaciones",
    "requisitos",
    "riesgos",
    "costos_o_cuotas",
    "dependencias",
    "preguntas_de_decision",
    "nivel_de_confianza",
    "fecha_de_verificacion",
    "fuentes"
  ],
  "properties": {
    "id": { "type": "string" },
    "tipo": {
      "enum": [
        "trend",
        "communication_channel",
        "architecture_pattern",
        "infrastructure_category",
        "constraint",
        "risk",
        "governance_requirement",
        "decision_variable",
        "open_question",
        "source"
      ]
    },
    "titulo": { "type": "string" },
    "descripcion": { "type": "string" },
    "hecho_o_tendencia": { "enum": ["hecho_comprobado", "tendencia", "supuesto"] },
    "aplica_cuando": { "type": "string" },
    "capacidades": { "type": "array", "items": { "type": "string" } },
    "limitaciones": { "type": "array", "items": { "type": "string" } },
    "requisitos": { "type": "array", "items": { "type": "string" } },
    "riesgos": { "type": "array", "items": { "type": "string" } },
    "costos_o_cuotas": { "type": "string" },
    "dependencias": { "type": "array", "items": { "type": "string" } },
    "preguntas_de_decision": { "type": "array", "items": { "type": "string" } },
    "nivel_de_confianza": { "enum": ["alto", "medio", "bajo"] },
    "fecha_de_verificacion": { "type": "string", "format": "date" },
    "fuentes": { "type": "array", "items": { "type": "string" } }
  }
}
```

---

## 14. Plan de Etapas Posteriores (Sin Ejecutarlas)

- **Etapa 1: Ingesta de Restricciones del Negocio**: El agente decisor recibe los datos concretos del volumen, presupuesto, equipo y acuerdos comerciales del producto.
- **Etapa 2: Evaluación Multicriterio de Alternativas**: Cruce de la base de conocimiento con las restricciones para puntuar matrices de decisión (Trade-off Matrix).
- **Etapa 3: Definición del Stack y Arquitectura (ADR)**: Registro formal de decisiones de arquitectura, selección justificada de proveedores y diseño detallado de interfaces.
- **Etapa 4: Prototipado y Validación en Simulación**: Construcción de pruebas de concepto con stubs, fixtures y modo `SIMULATION` para validar el flujo sin costos externos.
- **Etapa 5: Implementación Controlada y Puertas de Calidad**: Construcción incremental guiada por ciclos Kaizen con pruebas de carga, seguridad y observabilidad.

---

## 15. Criterios de Aceptación de la Etapa 0

1. **Neutralidad Tecnológica Absoluta**: El documento no favorece a ningún proveedor comercial ni predetermina el lenguaje o la plataforma.
2. **Cobertura Completa del Alcance**: Se han cubierto las 6 áreas temáticas (tendencias, patrones, canales, infraestructura, riesgos y criterios).
3. **Cero Acciones Destructivas o Ejecución Prematura**: Ningún componente de infraestructura ha sido instanciado, ni se han creado credenciales.
4. **Estructura Consultable**: Existencia de un modelo de datos formal y un índice claro de navegación para agentes posteriores.
5. **Alineación con Gobernanza**: Conservación estricta de `operatingMode: SIMULATION` y preservación de la integridad del workspace.

---

## 16. Fuentes Consultadas

- **Documentación de Protocolos y APIs Oficiales**:
  - RFC 5321 / RFC 5322 (Internet Message Format & SMTP).
  - RFC 7230 / RFC 7540 / RFC 9110 (HTTP/1.1, HTTP/2 y Semántica HTTP).
  - W3C Webhooks and Server-Sent Events Specifications.
  - Documentación de políticas para desarrolladores de Meta / WhatsApp Business API (Verificado: Octubre 2026).
- **Estándares de Seguridad y Privacidad**:
  - Ley 1581 de 2012 y Decreto 1377 de 2013, República de Colombia (Superintendencia de Industria y Comercio).
  - OWASP Top 10 for Large Language Model Applications (OWASP Foundation, 2025/2026).
  - Cloud Native Computing Foundation (CNCF) — Cloud Native Patterns & Interactive Landscape.
