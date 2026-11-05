# BuyaCAR Omnicanal — workspace final para Antigravity

Este directorio es un único proyecto de configuración y construcción para Gemini 4 Argon en Antigravity. No contiene aún la aplicación: Gemini la generará en `app_build/` por ciclos Kaizen.

## Arranque único

Abre esta carpeta como workspace y escribe:

```text
Obedece @.agents/prompts/BUYACAR_FINAL_INTEGRATED.md. Haz una prevalidación de máximo diez líneas y ejecuta inmediatamente la primera tarea no bloqueada del ciclo actual. Mantén SIMULATION, prioriza economía de tokens y deja evidencia en production_artifacts/.
```

No hace falta pegar otro prompt. Antigravity descubrirá `AGENTS.md`, agentes, reglas, skills y workflows automáticamente.

## Estructura compacta

- `AGENTS.md`: bootstrap único.
- `.agents/prompts/BUYACAR_FINAL_INTEGRATED.md`: fuente canónica integrada.
- `.agents/agents.md`: roles especializados.
- `.agents/rules/`: invariantes, seguridad, Go, IA, canales, Kaizen y tokens.
- `.agents/skills/`: procedimientos progresivos sin duplicar la especificación.
- `.agents/workflows/`: `/kaizen-cycle`, `/quality-gate` y `/next-cycle`.
- `app_build/`: código que generará Gemini.
- `production_artifacts/`: estado, handoffs, evaluaciones y auditorías.
- `docs/`: roadmap MVP y playbook.
- `config/` y `schemas/`: parámetros y contratos validables.

## Comportamiento esperado

El agente debe planificar brevemente y ejecutar en el mismo ciclo la primera tarea no bloqueada. Sólo debe detenerse por seguridad, permisos, decisión irreversible o bloqueo material. El modo inicial es `SIMULATION`; las cuentas reales y `AUTOPILOT` quedan fuera del MVP.
