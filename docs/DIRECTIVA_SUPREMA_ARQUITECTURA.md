# Directiva Suprema de Arquitectura y Blindaje BuyaCAR
**Destino:** `buyacar-omnicanal-agent-workspace` (Go 1.23 Standard Library / CP1-CP7 Pipeline)

Esta directiva complementa y blinda la ejecución de las capacidades descritas en `TODO.md` §2 para todos los Squads.

---

### MANDAMIENTO 1: Cero Dependencias de DOM Inestable (API-First o Selectores Versionados con Auto-Kill)
1. Ingesta vía Webhook HTTP oficial con validación criptográfica de firma HMAC-SHA256 (`X-Hub-Signature-256`).
2. Contingencias gráficas usan obligatoriamente `VersionedSelector` con failover ordenado.
3. Auto-pausa inmediata al 3er fallo consecutivo de selector/despacho y emisión de ticket a `escalate.Queue`. Prohibido el reintento infinito.

---

### MANDAMIENTO 2: Pipeline Rígido de 7 Checkpoints (CP1–CP7) sin Atajos
- **CP1 (Lectura/Ingesta):** Sanitización contra Indirect Prompt Injection (escape de delimitadores XML/JSON).
- **CP2 (Triage & Descarte):** Descarte inmediato de mensajes propios (`outgoing`), sistema o spam sin consumir tokens ni memoria.
- **CP3 (Fast-Path / Inventario Atómico):** Búsqueda en memoria protegida (`sync.RWMutex`). Coincidencia exacta (`exact_match`) responde en < 5 ms sin IA. Unidades vendidas/reservadas o ambigüedad escalan inmediatamente a humano.
- **CP4 (IA Estructurada & Contexto Estricto):** Presupuesto estricto de tokens (máximo 3 vehículos relevantes). Salida obligatoriamente en JSON tipado estricto bajo contrato de persistencia.
- **CP5 (Guardrails Deterministas Post-Generación):**
  - **Filtro Anti-Alucinación Numérica:** Validación de cada cifra de 5+ dígitos contra el catálogo de precios y teléfonos autorizados (`3151212223`, `3022866969`, `3160502717`). Bloqueo y escalación inmediata si hay anomalías.
  - **Allowlist de URLs:** Enlaces estrictamente limitados a `https://buyacar.com.co`.
  - **Presupuesto de Caracteres:** WhatsApp: 650, Facebook: 500, Instagram: 450, TikTok: 400.
  - **Privacidad Ley 1581 de 2012:** Redacción determinista obligatoria de PII en logs y colas humanas.
- **CP6 (Despacho Protegido):** Revalidación pre-envío de inventario (`TTL < 30 min`), Rate Controller con ventana deslizante (120 msg/h, 250 msg/d) y jitter SHA-256. Modo `SIMULATION` por defecto.
- **CP7 (Sellado CRM e Idempotencia ACID):** Hash SHA-256 único de conversación/mensaje para ledger de idempotencia y registro de tarea comercial en CRM interno/externo con reintentos offline.

---

### MANDAMIENTO 3: Aislamiento Concurrente y Tolerancia a Reinicios
1. Goroutines con `context.Context` y timeouts estrictos (< 30s upstream). Ningún canal bloquea a otro.
2. Todo estado mutable crítico (idempotencia, tareas pendientes, rate limiting) reside en estructuras thread-safe persistentes contra reinicios.

---

### MANDAMIENTO 4: Seguridad del Entorno Local y Respeto de Fronteras
1. Ollama local exclusivo a `127.0.0.1` con cabecera interna. Límite de payload (`MAX_BODY_BYTES = 256 KB`) y timeout de cancelación de 30s.
2. Webhooks externos requieren validación estricta de firma HMAC / Bearer Token.

---

### MANDAMIENTO 5: Protocolo de Salida y Registro Forense
1. Detención inmediata ante cualquier fallo o condición de carrera (`go test -race`). Cero parches superficiales.
2. Pruebas unitarias sin red externa, adición a `TestGoldenSuite`, y verificación formal de ausencia de alucinaciones y redacción PII para cada nueva capacidad.
