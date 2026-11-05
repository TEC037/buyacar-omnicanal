# /start-cycle

Inicia un ciclo autónomo controlado.

1. Lee `@AGENTS.md`, `@.agents/agents.md`, `@.agents/prompts/BUYACAR_FINAL_INTEGRATED.md`, `production_artifacts/STATUS.md` y el último handoff.
2. Activa `spec-first` si el alcance no está especificado; activa `cycle-orchestration`, `kaizen-mvp` y `trace-evaluation` siempre.
3. Presenta hipótesis, métrica primaria, línea base, meta, alcance, archivos permitidos, puerta de calidad, criterio de reversión y riesgos.
4. Si no hay bloqueo, ejecuta el ciclo en `SIMULATION`.
5. Usa `go-quality` para código y `security-qa` antes del handoff.
6. Ejecuta la autoevaluación del ciclo, escribe el handoff y actualiza el estado.

No conectes servicios reales ni cambies de modo automáticamente.
