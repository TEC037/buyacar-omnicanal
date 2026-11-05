# Roadmap Kaizen hasta MVP

El producto aún no está implementado. Este roadmap limita el trabajo de Gemini y convierte cada etapa en una hipótesis verificable.

## Definición de MVP

El MVP es una demostración reproducible en `SIMULATION` que procesa un flujo de conversación de inventario y escalamiento con datos ficticios o fixtures, sin cuentas reales, y demuestra: inventario verificable, Fast-Path, triage, proveedor Gemini/Ollama/humano con stubs, guard anti-alucinación, idempotencia, persistencia de estado, trazas de siete checkpoints, métricas y kill switch.

El MVP no incluye todavía automatización real de WhatsApp, Meta, TikTok, correo, CRM, follow-ups comerciales ni `AUTOPILOT`.

## Fases y puertas

| Fase | Resultado demostrable | Puerta de salida |
|---|---|---|
| 0. Control plane | SPEC, ADR iniciales, 64 casos planificados y pipeline de evaluación | workspace descubierto, simulación definida |
| 1. Núcleo | tipos, configuración, eventos, store y reanudación | build/test/race limpios |
| 2. Inventario | fixture/API stub, caché, frescura y coincidencia exacta | cero adivinanzas en casos dorados |
| 3. Fast-Path y triage | normalización, confianza, respuestas deterministas | p99 medido; ambigüedad escala |
| 4. Guard | cifras, URLs, PII, expresiones y límites | fuzzing sin bypass conocido |
| 5. IA | Gemini → Ollama → humano con JSON Schema | timeout/cuota/error probados |
| 6. Orquestación | flujo completo en SIMULATION | idempotencia y reanudación probadas |
| 7. Observabilidad | siete checkpoints, métricas, coste y evaluación | trazas exportables y sin PII |
| 8. MVP | demo reproducible end-to-end con fixtures | todas las puertas anteriores verdes |

## Regla de avance

Una fase no avanza por tiempo transcurrido. Avanza sólo cuando su puerta está `GREEN`, su evidencia está guardada y la evaluación Kaizen propone `keep` o `iterate` sin regresión crítica.

## Fuera del MVP

Canales reales, navegación contra cuentas reales, despliegue productivo, campañas, seguimiento automático, aprendizaje autónomo sin aprobación humana y `AUTOPILOT`.
