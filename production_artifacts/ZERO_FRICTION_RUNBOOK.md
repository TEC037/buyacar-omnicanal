# BuyaCAR Omnicanal — Protocolo de Recepción de Órdenes (Runbook de Cero Fricción)

Este protocolo estandariza la recepción y ejecución inmediata de cualquier orden de trabajo para el agente de desarrollo en este workspace.

## 1. Verificación Previa Inmediata (Checklist de Entrada)
Antes de ejecutar una orden:
1. Confirmar `operatingMode: SIMULATION`.
2. Verificar que no se expongan secretos ni PII.
3. Confirmar que el presupuesto de tokens del ciclo esté definido (máx 8.000 tokens).
4. Verificar que `app_build/` solo sea modificado por el rol `@go-engineer` bajo una orden aprobada.

## 2. Puntos de Grounding Oficial y Canónico
- **Especificación integral**: `.agents/prompts/BUYACAR_FINAL_INTEGRATED.md`
- **Gobernanza del equipo**: `.agents/AGENTS.md`
- **Roadmap Kaizen**: `docs/MVP_ROADMAP.md`
- **Configuración base**: `config/example.yaml`
- **Políticas de calidad**: `config/quality-gates.yaml` y `config/token-policy.yaml`

## 3. Protocolo de Transición de Ciclo
Al recibir una nueva orden:
1. Leer `production_artifacts/STATUS.md` y el último handoff en `production_artifacts/handoffs/`.
2. Realizar prevalidación de máximo 10 líneas.
3. Ejecutar la primera tarea no bloqueada.
4. Generar la traza de los 7 checkpoints en `production_artifacts/traces/`.
5. Ejecutar la auditoría de tokens y la evaluación Kaizen en `production_artifacts/evaluations/`.
6. Emitir handoff formal en `production_artifacts/handoffs/` y actualizar `production_artifacts/STATUS.md`.
