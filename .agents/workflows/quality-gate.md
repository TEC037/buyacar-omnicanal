# /quality-gate

Audita el ciclo actual sin ampliar alcance.

- Lee el último handoff y los cambios de `app_build/`.
- Ejecuta las validaciones aplicables.
- Revisa seguridad, contratos JSON, idempotencia y métricas.
- Revisa `docs/TOKEN_BUDGET.md`, `config/token-policy.yaml` y `token-audit-cycle-N.json`.
- Marca `YELLOW` si no existe medición de tokens; marca `RED` si hay desperdicio repetido sin justificación.
- Compara contra la línea base, el ciclo anterior y la meta del roadmap.
- Verifica que exista la evaluación JSON del ciclo.
- Devuelve `GREEN`, `YELLOW` o `RED` con evidencias.
- Si es `RED`, crea una lista priorizada de correcciones y no habilites el siguiente ciclo.
