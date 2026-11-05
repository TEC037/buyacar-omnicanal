# Guía de Operación y Arquitectura — BuyaCAR Control Plane & Motor RAG

**Versión del Sistema**: BuyaCAR Omnicanal v1.1 — RAG Enabled (Go 1.23 Standard Library)  
**Modo Operativo por Defecto**: `SIMULATION` (Inmutable sin intervención humana explícita)  
**Ubicación del Código**: `app_build/`

---

## 1. Arquitectura del Sistema con Motor RAG

El control plane de BuyaCAR Omnicanal integra un motor RAG local (Retrieval-Augmented Generation) en Go puro dentro de la tubería secuencial determinista de 7 checkpoints (**CP1 a CP7**):

```
[Inbound Webhook / Canal]
          │
          ▼
   ┌──────────────┐
   │ CP1: Lectura │  ── Sanitización anti-injection XML/JSON
   └──────┬───────┘
          │
          ▼
   ┌──────────────┐
   │ CP2: Triage  │  ── Descarte inmediato de spam / mensajes propios (0 tokens)
   └──────┬───────┘
          │
          ▼
   ┌───────────────────┐
   │ CP3: Fast-Path /  │ ── Coincidencia exacta en memoria (RWMutex) a ~2.59 µs
   │      Inventario   │    (Unidades vendidas/ambigüedad escalan a humano)
   └──────┬────────────┘
          │ Coincidencia Parcial / Consulta Compleja / Normativa
          ▼
   ┌───────────────────┐
   │ CP4: Capa         │ ── Inyección Semántica RAG (internal/rag/):
   │      Cognitiva    │    BM25 + Similitud Coseno + Fallback Ollama (127.0.0.1)
   │      & RAG        │    Fixtures embebidos (Garantía, Crédito Boyacá, FAQs)
   │                   │    Cascada: Ollama -> Groq Free -> Fast-Path determinista
   └──────┬────────────┘    Presupuesto estricto <= 150 tokens por chunk
          │
          ▼
   ┌───────────────────┐
   │ CP5: Guardrails   │ ── Filtro anti-alucinación numérica (ValidateOutboxWithBackedNumbers)
   │      Deterministas│    Cifras de 5+ dígitos cruzadas contra catálogo y RAG indexado
   │                   │    Allowlist URL https://buyacar.com.co | Redacción PII Ley 1581
   └──────┬────────────┘
          │
          ▼
   ┌───────────────────┐
   │ CP6: Despacho     │ ── Revalidación inventario TTL < 30m | Rate controller (120/h, 250/d)
   │      Protegido    │    Pausas con jitter SHA-256 | Modo SIMULATION / DRAFT / AUTOPILOT
   └──────┬────────────┘
          │
          ▼
   ┌───────────────────┐
   │ CP7: Sellado CRM  │ ── Persistencia ACID WAL (CRC32/fsync) con fallback a JSON
   │      & Idempotent │    Exportación saliente con firma HMAC SHA-256 a CRM externo
   └───────────────────┘
```

---

## 2. Componentes del Motor RAG (`internal/rag/`)

- **Búsqueda Léxica BM25** (`bm25.go`): Indexador en memoria basado en Okapi BM25 optimizado para consultas en español sin dependencias externas.
- **Similitud Coseno Vectorial** (`vector.go`): Espacio vectorial local con cálculo de similitud coseno puro en Go.
- **Conector Local Ollama** (`ollama_embed.go`): Proveedor para generación de embeddings locales vía `nomic-embed-text` en `http://127.0.0.1:11434` con fallback transparente al motor local si no está activo.
- **Re-ranker Híbrido** (`hybrid_ranker.go`): Fusión ponderada de puntuaciones léxicas y vectoriales con latencia p99 medida de **251 µs**.
- **Fixtures de Conocimiento Embebidos** (`fixtures/rag/`):
  - `vehicles.json` / `vehicles.md`: Fichas técnicas del catálogo vehicular de BuyaCAR.
  - `warranty_policy.md`: Políticas de garantía legal, cobertura de motor, caja y peritaje.
  - `credit_requirements.md`: Requisitos y condiciones financieras para clientes en Boyacá y Tunja.
  - `faq_boyaca.md`: Preguntas frecuentes sobre ubicación, horarios y sedes en Tunja.
- **Chunking Semántico Determinista** (`chunker.go`): Fragmentación acotada con extracción automática de números respaldados para alimentar los guardrails anti-alucinación de CP5.

---

## 3. Guía de Ejecución y Comandos Operativos

Todos los comandos se ejecutan desde el directorio `app_build`:

```powershell
cd app_build
```

### 1. Ejecución de Pruebas Completas (100% Uncached)
```powershell
go test -count=1 ./...
```
*Verifica que los 18 paquetes de Go compilen y pasen sus suites unitarias e integrales en verde.*

### 2. Ejecución de Casos Dorados de RAG
```powershell
go test -v -count=1 ./test -run TestRAGGolden
```
*Valida las 7 pruebas doradas de recuperación semántica, anti-alucinación con RAG y redacción de PII.*

### 3. Arrancar Servidor de Consola Web Local
```powershell
go run ./cmd/buyacar --server -port 8080
```
- Consola Web de Asesores: `http://127.0.0.1:8080`
- Métricas Prometheus: `http://127.0.0.1:8080/metrics`

### 4. Diagnóstico de Salud CLI (Modo Inspect)
```powershell
go run ./cmd/buyacar --inspect
```

### 5. Inspección de Métricas CLI
```powershell
go run ./cmd/buyacar --metrics
```

---

## 4. Matriz de Variables de Entorno

| Variable | Valores Posibles | Por Defecto | Descripción |
| :--- | :--- | :--- | :--- |
| `BUYACAR_OPERATING_MODE` | `SIMULATION`, `DRAFT`, `AUTOPILOT` | `SIMULATION` | Modo operativo maestro de seguridad. |
| `PORT` | Entero (ej. `8080`) | `8080` | Puerto para la consola web embebida. |
| `OLLAMA_HOST` | URL (ej. `http://127.0.0.1:11434`) | `http://127.0.0.1:11434` | Endpoint local de Ollama (aislado a loopback) para inferencia y embeddings. |
| `WHATSAPP_CLOUD_TOKEN` | Token secreto | *(vacío)* | Token de acceso para WhatsApp Cloud API oficial. |
| `MERCADOLIBRE_CLIENT_SECRET` | Secreto HMAC | *(vacío)* | Secreto para validar webhooks de MercadoLibre. |
| `ERP_WEBHOOK_SECRET` | Secreto HMAC | *(vacío)* | Secreto para endpoint de invalidación de inventario. |
| `CRM_WEBHOOK_URL` | URL HTTPS | *(vacío)* | URL del CRM externo (HubSpot, Salesforce, etc.). |
