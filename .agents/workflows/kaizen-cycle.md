# /kaizen-cycle

Ejecuta un ciclo PDCA completo hasta el MVP.

1. Lee `@AGENTS.md`, `@.agents/agents.md`, `@.agents/prompts/BUYACAR_FINAL_INTEGRATED.md`, `@docs/MVP_ROADMAP.md`, `@production_artifacts/STATUS.md`, el último handoff y la última evaluación.
2. Activa `kaizen-mvp`, `trace-evaluation` y `token-economy` antes de cualquier otra skill.
3. Haz una preauditoría de contexto: elimina duplicados, resume historial y fija el presupuesto de tokens.
4. Produce después el plan del ciclo: hipótesis, métrica primaria, meta, alcance, archivos permitidos, reversión y puerta.
5. Si falta una decisión bloqueante, detente. Si no, ejecuta sólo el alcance aprobado en `SIMULATION`.
6. Ejecuta `/quality-gate` y reúne evidencias reproducibles.
7. Crea la evaluación JSON y la auditoría de tokens, actualiza el estado y escribe el handoff.
8. Decide `keep`, `iterate`, `rollback` o `stop`; propone una sola mejora siguiente.

No avances de fase ni declares MVP dentro del mismo ciclo si una puerta crítica está amarilla o roja.
