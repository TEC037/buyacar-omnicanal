# BuyaCAR Omnicanal — Gobernanza del Workspace (Post-MVP Simulation)

Este workspace contiene el MVP completo en `app_build/` y la gobernanza de control, evidencias y transición hacia `DRAFT` / `AUTOPILOT` bajo supervisión humana.

## Fuentes canónicas

- Especificación base: `@.agents/prompts/BUYACAR_FINAL_INTEGRATED.md`.
- Instructivo maestro de transición (`SIMULATION` -> `DRAFT` -> `AUTOPILOT`): `@instructivo.md`.

## Descubrimiento

- Equipo actualizado: `@.agents/AGENTS.md`.
- Reglas: `.agents/rules/`.
- Skills: `.agents/skills/`.
- Workflows: `.agents/workflows/`.
- Documentación del MVP: `@docs/MVP_DOCUMENTATION.md` y `@docs/SPEC.md`.
- Estado actual: `@production_artifacts/STATUS.md`.

## Contrato operativo

Haz una prevalidación de máximo diez líneas y ejecuta inmediatamente la primera tarea no bloqueada solicitada por el usuario. Mantén `SIMULATION` por defecto hasta que una orden humana explícita autorice `DRAFT`. Seguridad y exactitud son innegociables; después prioriza economía de tokens. Usa `app_build/` para el producto y `production_artifacts/` para evidencias. Mantén activos los crons de mejora continua Kaizen PDCA y vigilancia de puertas de calidad del MVP con ejecución compacta y económica en tokens.
