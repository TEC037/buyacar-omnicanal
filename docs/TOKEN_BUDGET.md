# Presupuesto y política de tokens

## Prioridad

La prioridad operacional es minimizar tokens. Se acepta mayor latencia, más validación local, más lecturas selectivas y más pasos de compresión. Esta prioridad nunca permite degradar seguridad, exactitud, privacidad o cumplimiento.

## Presupuestos iniciales

| Superficie | Presupuesto inicial | Regla |
|---|---:|---|
| IA operativa por conversación | 1.500 entrada / 300 salida | Sólo si Fast-Path y caché no resuelven |
| Contexto del agente por ciclo | 8.000 tokens | Referencias y rangos antes que copias |
| Salida de informe de ciclo | 500 tokens | Objetivo, cambios, pruebas, riesgos, siguiente paso |
| Handoff | 700 tokens | Sin repetir código ni especificación |
| Prompt persistente always-on | mínimo viable | Mover detalle a skills model-decision |

Los valores se pueden ajustar sólo con medición y ADR.

## Métricas obligatorias

- `input_tokens`, `output_tokens`;
- `repeated_context_tokens`;
- `compressed_context_tokens`;
- `cache_hits`, `cache_misses`;
- `tokens_saved_by_fast_path`;
- `tokens_per_resolved_conversation`;
- `tokens_per_cycle`;
- `tool_calls_avoided`;
- coste estimado, si hay precio verificable.

## Puerta de economía

Un ciclo está `GREEN` cuando mantiene correctness y security, mide tokens y no aumenta tokens por resultado resuelto sin una justificación registrada. Si no hay medición, el estado es `YELLOW`, nunca `GREEN`.
