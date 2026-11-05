# Plan persistente — Canal Facebook Marketplace (BuyaCAR)

- **Estado**: EJECUTADO EN `SIMULATION` (Hitos M0, M1, M2 y Torneo Kaizen de Autopilotaje Fases 1–4 completados el 2026-11-04).
- **Modo**: `SIMULATION` por defecto (`BUYACAR_OPERATING_MODE=DRAFT` o `AUTOPILOT` bajo orden humana explícita).
- **Creado**: 2026-11-04. Actualizar este archivo al cerrar cada hito (no crear duplicados).

## 1. Objetivo y métrica primaria

Que todo mensaje rutinario de Marketplace reciba una respuesta exacta, rápida y 100 % respaldada por datos reales, escalando únicamente cuando hay negociación humana, permuta, vehículo vendido o ambigüedad genuina.

| Métrica | Meta | Estado medido (`TestAutopilotTournament_Compare4Possibilities`) |
|---|---|---|
| Tiempo a primera respuesta p95 (motor local) | < 5 ms | **`54 µs` (`A2_rag_synth`) / `73 µs` (`A4_adaptive`)** |
| Resolución autónoma rutinaria (0 humano) | ≥ 88 % | **`100.0 % (22/22)` en casos rutinarios, multi-turno, RAG y compuestos** |
| Cifras no respaldadas o PII en la salida | 0 | **`0` violaciones CP5 / Ley 1581** |
| Escalamiento correcto (vendido, ambiguo, permuta, regateo, prompt injection, queja) | 100 % de los casos dorados | **`100.0 % (8/8)`** |
| Tokens por conversación (`A2_rag_synth` / `A4_adaptive` offline) | 0 en Fast-Path + RAG-Direct | **`0` tokens (`A2` / `A4` offline) / `67.5` tokens prom. (`A4` con LLM)** |

## 2. Decisión de ruta (hechos y descartes)

**Hechos verificados (fuentes públicas, 2026-11-04):**
- Meta no ofrece una API abierta para leer Marketplace en general.
- Cuando un comprador escribe a un anuncio publicado **por una Página**, se abre un hilo normal de Messenger en la bandeja de esa Página. Se puede recibir con el webhook `messages` de la Página (Messenger Platform), con los permisos `pages_messaging` y `pages_manage_metadata` en Advanced Access.
- El inventario automotriz se puede subir como catálogo (Catalog/Commerce API). **Supuesto a verificar:** que esto esté disponible para concesionarios en Colombia.

**Descartado (corrige la propuesta anterior):**
- Robot Playwright/headless con cookies sobre el Marketplace de un perfil personal, y el "jitter para parecer humano ante Meta". Esto automatiza una cuenta sin permiso de Meta e intenta esquivar su detección. Choca con sus términos de servicio, con `30-channels.md` ("navegador sólo como último recurso y respetando ToS") y con `00-safety.md`. Lo más probable es que bloqueen la cuenta, que es justo lo que queremos evitar.

## 3. Arquitectura en 3 niveles (todos legítimos; el N1 funciona hoy)

```
N1 Copiloto (hoy)          N2 Bandeja de Página (oficial)        Puente
asesor pega mensaje   ->   webhook messages (meta_omnichannel)  -> WhatsApp Cloud API
consola /copilot           DRAFT + aprobación 1 clic               (ya implementado,
pipeline CP1–CP7 + RAG     Send API dentro de ventana 24 h          con consentimiento)
botón "Copiar"  -> asesor envía en Marketplace / Business Suite
```

- **N1 Copiloto humano**: no necesita aprobación de Meta ni credenciales. El asesor sigue respondiendo desde su cuenta y el sistema le redacta en segundos la respuesta con datos del inventario y el RAG, ya pasada por los guardrails.
- **N2 Página oficial**: se usa sólo si los vehículos se publican desde la Página de BuyaCAR y Meta entrega esos hilos al webhook. Reutiliza `internal/channel/meta_omnichannel.go`.
- **Puente a WhatsApp**: el llamado a la acción lleva al cliente al canal oficial donde se cierra la venta.

## 4. Prerrequisito bloqueante: calidad del RAG (RESUELTO — Hito M0 completado el 2026-11-04)

| Consulta | Resultado anterior | Resultado actual tras M0 (`cmd/demo_rag` y `test/rag_golden_test.go`) |
|---|---|---|
| "Toyota Hilux 2022 … precio" | Devolvió **Renault Logan 2022** | **RESUELTO**: Devuelve `Toyota Hilux SRX 2.8 4x4 2022` ($185.000.000 COP, 42.000 km) como #1 (Score `0.8554`). Catálogos `internal/inventory/stub.go` y `fixtures/rag/` unificados + guardia de marca/modelo en `internal/rag/engine.go`. |
| "Ferrari Testarossa 2022" (inexistente) | Sustituía por otro vehículo 2022 | **RESUELTO**: Devuelve `0` fragmentos (`nil`), evitando sustituir vehículos inexistentes por coincidencia de año. |
| "ubicación y horario en Boyacá" | Devolvió el proceso de garantías | **RESUELTO**: Devuelve `Horarios de Atención y Ubicación de Sedes` (`Av. Universitaria # 45-12, frente a Unicentro Tunja`) como #1 (Score `0.8217`) mediante stemming morfológico en español (`bm25.go`) y peso 3x en títulos. |
| "garantía motor y caja" | Devolvió el peritaje de 150 puntos | **RESUELTO**: Devuelve `Cobertura de Garantía Mecánica Certificada` (`12 meses o 20.000 km`) como #1 (Score `0.8141`). |
| Scores y bug de `cmd/demo_rag` | Planos (~0.016 RRF) e índices `%!s(int=0)` | **RESUELTO**: Fusión ponderada `FusionWeighted (0.80 BM25 / 0.20 Vector)` con scores diferenciados (`0.81–0.85`), loop corregido (`for _, num := range backed`) y `RAGEngine` + canal `facebook_marketplace` conectados en `cmd/buyacar/main.go`. |

## 5. Hitos (una variable por ciclo, regla Kaizen)

| Hito | Entregable | Puerta de calidad | Estado |
|---|---|---|---|
| **M0** Calidad RAG y conexión en binario principal | Unificación de catálogo (`Hilux 2022`, `CX-5 2021/2023`, `Duster`, `Stepway`, `Logan`, `Tracker`, `CX-30`), guardia determinista de marca/modelo (0 sustitución de vehículos inexistentes), stemming español + 3x título en BM25, y conexión de `RAGEngine` + canal `facebook_marketplace` en `cmd/buyacar/main.go`. | `TestRAGGolden_M0_MarketplaceCourseCorrection` + suite completa 18/18 paquetes en verde | **COMPLETADO (VERDE)** |
| **M1** Intents Marketplace | Triage determinista en CP2: disponible (`"¿Sigue disponible?"`), precio, permuta/retoma, financiación/riesgo, ubicación, visita/prueba de ruta, regateo. Plantillas ≤ 500 caracteres con puente a WhatsApp `3151212223`. | `TestFastPath_M1_MarketplaceGoldenPhrases` (30 frases reales como casos dorados) | **COMPLETADO (VERDE)** |
| **M2** Copiloto y Procesador Autónomo en Consola | `POST /api/copilot/draft` + panel interactivo en `http://127.0.0.1:8080` (textarea → borrador → Copiar 1-Clic) con síntesis autónoma respaldada por RAG y validación CP5 + Ley 1581. | `TestRunCLI_MarketplaceCopilotEndpoint_M2` (E2E en SIMULATION, 0 PII en respuestas/logs) | **COMPLETADO (VERDE)** |
| **M3** Conector Event-Driven de Página + Torneo Kaizen de Autopilotaje (Fases 1–4) | Memoria de hilo (`ClassifyWithContext`), extracción de `referral.ad_title`, sintetizador RAG-Direct (`SynthesizeFromRAGChunk`), enriquecimiento de consultas compuestas, Torneo Kaizen (`test/autopilot_tournament_test.go`), `/webhooks/meta` event-driven y `/api/killswitch`. | `TestAutopilotTournament_Compare4Possibilities` + `TestRunCLI_Phase4_MetaWebhookAndKillSwitch` | **COMPLETADO EN CÓDIGO (VERDE)** |
| **M4** Piloto DRAFT (2 semanas) | Medición en vivo con credenciales oficiales de Meta (`META_PAGE_ID`, `META_ACCESS_TOKEN`, `META_APP_SECRET`) | Criterios de salida de la §1 | PENDIENTE (Requiere credenciales Meta) |

## 5.1 Torneo Experimental Kaizen de Posibilidades de Autopilotaje (`test/autopilot_tournament_test.go`)

Se evaluaron 4 arquitecturas candidatas sobre un corpus de **30 conversaciones reales de Facebook Marketplace** (22 rutinarias/multi-turno/RAG/compuestas + 8 escalamientos obligatorios):

| Posibilidad / Arquitectura | Resolución Autónoma (meta ≥ 88 %) | Exactitud Fáctica (meta 100 %) | Precisión de Escalamiento (8/8) | Violaciones CP5 / PII | Tokens Totales | Latencia Promedio |
|---|---|---|---|---|---|---|
| **A1** (`A1_fast_only` — Sólo Fast-Path sin memoria de hilo ni RAG-Synthesizer) | 50.0 % (11/22) | 31.8 % (7/22) | 100.0 % (8/8) | 0 | 0 | `17 µs` |
| **A2** (`A2_rag_synth` — Fast-Path + Memoria de Hilo + RAG Compuesto + RAG-Direct Synthesizer, 0 LLM) | **100.0 % (22/22)** | **100.0 % (22/22)** | **100.0 % (8/8)** | **0** | **0** | **`54 µs`** |
| **A3** (`A3_llm_only` — Dependiente de LLM externo cuando está offline) | 72.7 % (16/22) | 72.7 % (16/22) | 100.0 % (8/8) | 0 | 0 | `88 µs` |
| **A4** (`A4_adaptive` — Cascada Adaptativa con fallback automático a `A2` cuando LLM está offline) | **100.0 % (22/22)** | **100.0 % (22/22)** | **100.0 % (8/8)** | **0** | **0** | **`73 µs`** |
| **A4+LLM** (`A4_adaptive_with_llm` — Cascada Adaptativa con LLM activo) | **100.0 % (22/22)** | **100.0 % (22/22)** | **100.0 % (8/8)** | **0** | 2026 | `105 µs` |

**Ganador seleccionado en producción (`cmd/buyacar/main.go`)**:
- Estrategia **`A2_rag_synth`** por defecto cuando no hay `GEMINI_API_KEY` (resuelve el **100 %** de consultas rutinarias, multi-turno, normativas RAG y compuestas en **`54 µs`** con **`0` tokens**), y conmutación automática a **`A4_adaptive`** cuando `GEMINI_API_KEY` está configurada.

## 6. Decisiones pendientes del humano

1. ¿Los vehículos se publican desde la **Página de BuyaCAR** o desde perfiles personales de asesores? Esto define si el N2 es posible.
2. Datos reales de sedes, horarios y política de permutas. No se inventan: los aporta el negocio.
3. Aprobar este plan como ADR para arrancar M0.

## 7. Nota de verificación

En esta máquina `go test -race` **no se puede ejecutar** (requiere CGO/gcc, que no está instalado). Hasta instalar un compilador C, cualquier afirmación de "0 data races" en los artefactos queda como **no verificada**.
