# Handoff Ciclo 5: Proveedores IA (Gemini -> Ollama -> Humano) y Economía de Tokens (Fase 5)

- **Agente emisor**: @ai-integrator / @go-engineer
- **Fecha**: 2026-10-26
- **Modo operativo**: `SIMULATION`
- **Objetivo**: Implementar `internal/skills` e `internal/ai` con el contrato JSON estricto (`DisallowUnknownFields`), cadena `Cache -> Gemini -> Ollama -> humano`, un único reintento ante JSON inválido, circuit breaker y presupuesto de 1.500 tokens de entrada / 300 de salida.
- **Entrada**: `production_artifacts/handoffs/cycle-4.md`, `docs/MVP_ROADMAP.md`.
- **Cambios realizados**:
  1. `app_build/internal/skills/skills.go`: `BuildCompactContext` (envía únicamente intención, resumen corto y de 1 a 3 vehículos disponibles) y `ApprovedCache` para respuestas frecuentes aprobadas por humanos (0 tokens).
  2. `app_build/internal/ai/ai.go`: Validador `ParseStrictJSON` con `DisallowUnknownFields`, chequeo RFC3339 y rango de confianza `[0,1]`, más `Orchestrator` con reintento único en JSON inválido, circuit breaker y fallback a Ollama y humano.
  3. `app_build/internal/ai/stub.go` y `ai_test.go`: Stubs programables y pruebas de cuota, timeout, JSON inválido y ahorro por caché.
- **Riesgos y decisiones**: Contexto minimizado antes de invocar IA; cero exposición de PII.
- **Siguiente acción**: Ejecutar Ciclo 6 / Fase 6 (`internal/escalate` y flujo completo de orquestación en `SIMULATION` con idempotencia y reanudación probadas).
