# Dispatch History

## 2026-10-29T05:06:18Z
Implementar las capacidades evolutivas y de robustecimiento de producción para el control plane omnicanal de BuyaCAR en Go 1.23 estándar, integrando canales oficiales y webhooks, persistencia resiliente y conectores CRM/DMS, optimización cognitiva con inferencia gratuita y panel web local embebido con métricas, bajo modo SIMULATION y cumplimiento estricto de PII.

Working directory: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace
Integrity mode: development

## Contexto y Referencias
- Repositorio y código modular en `app_build/` (15 paquetes en Go 1.23 estándar, 100% tests en verde, 64 casos dorados, batería E2E multi-tier).
- Especificación y hoja de ruta: `TODO.md` (Punto 2: "Evoluciones Naturales"), `production_artifacts/STATUS.md`, `instructivo.md`.

## Requirements
### R1. Reglas Base, Gobernanza y Seguridad del Core
### R2. Canales y Conectores de Comunicación (Squad 1)
### R3. Persistencia, Sincronización DMS e Integración CRM (Squad 2)
### R4. Capa Cognitiva y Economía de Tokens con Fallbacks Gratuitos (Squad 3)
### R5. Consola Web Embebida y Observabilidad (Squad 4)

## Acceptance Criteria
- Core y Gobernanza
- Canales y Gateways
- Estado, DMS y CRM
- Cognitivo y Economía de Tokens
- Consola y Métricas
- Verificación y Calidad

## 2026-10-29T05:17:23Z
DIRECTIVA SUPREMA VINCULANTE E INMUTABLE DE ARQUITECTURA Y BLINDAJE BUyACAR (7 MANDAMIENTOS ANTI-REGRESIÓN):
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

## 2026-10-29T05:45:02Z
URGENTE - INSTRUCCIÓN DE REMEDIACIÓN DE TELEMETRÍA Y SANEAMIENTO DE GOBERNANZA (ANTI-VIBECODING):
1. SQUAD 1 (Telemetría e Integración del Pipeline en Go):
   - Conectar e importar formalmente `internal/obs` en `pipeline.go`.
   - Reemplazar descartes ciegos `_ = p.Engine.store.SaveCheckpointState(...)` por captura y trazabilidad de errores reales.
   - Instrumentar los 7 checkpoints (`lectura`, `triage`, `inventario`, `respuesta`, `guard`, `envio`, `crm`) usando `time.Now()` real y `time.Since(...)` efectivo.
   - Hook en `Pipeline` para exportar trazas reales a `production_artifacts/traces/`.
2. SQUAD 2 (Medición Real de Tokens, Caché y Anti-Automejora Ficticia):
   - Registrar mediciones reales de tokens o marcar `not_measured`. Prohibido 0 misses en cold start.
   - Instrumentar `internal/ai/semantic_cache.go` y `skills.ApprovedCache` para métricas reales Hit/Miss.
   - Eliminar fórmula matemática ficticia del 10% en `master_cron_self_optimizer.go`. Sin cambio medible, estado = `unchanged`.
3. SQUAD 3 (Desacople de Tests Tautológicos y Encoding UTF-8):
   - Refactorizar `draft_batch_exerciser_test.go`: desacoplar prueba de compuerta humana real. Prohibido bucle que auto-aprueba 100% para ReadyForAutopilot=true. Requerir entrada explícita o permanecer en `BLOCKED_PENDING_HUMAN_REVIEW`.
   - Actualizar `obs_test.go` para verificar progresión temporal real de checkpoints (timestamps monótonos crecientes).
   - Forzar UTF-8 sin BOM en volcados de evaluación.
4. SQUAD 4 (Gobernanza, Saneamiento de STATUS y Limpieza de Crones):
   - Estandarizar nomenclatura canónica de 7 checkpoints (`cp1_lectura`, etc.) y desaprobar Ciclo 0.
   - Purgar sobreingeniería burocrática de crones.
   - Reescribir `production_artifacts/STATUS.md`: retirar hipérboles ("VICTORY CONFIRMED") y basar estado técnico en `go test -v -count=1 ./...`.
