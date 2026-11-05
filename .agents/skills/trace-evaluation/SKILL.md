---
name: trace-evaluation
description: "Diseña y revisa trazas, métricas y autoevaluaciones de conversaciones y ciclos; usar al validar rendimiento, calidad, costes y regresiones."
---
# Skill: trace-evaluation

- Usa timestamp UTC, `cycle_id`, `request_id` y PII redactada.
- Cada conversación registra siete checkpoints: lectura, triage, inventario, respuesta, guard, envío y CRM.
- Cada checkpoint debe indicar estado, duración, error sanitizado y evidencia.
- Separa métricas de producto, seguridad, operación y coste.
- No conviertas la evaluación del agente en una prueba de calidad del producto: la prueba debe ser reproducible desde artefactos.
- Si una métrica no fue instrumentada, marca `not_measured`; nunca la estimes.
