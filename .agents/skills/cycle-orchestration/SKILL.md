---
name: cycle-orchestration
description: "Orquesta ciclos autónomos de especificación, implementación, pruebas y handoff para BuyaCAR Omnicanal. Usar al iniciar un ciclo o coordinar agentes."
---
# Skill: cycle-orchestration

1. Lee `@.agents/prompts/BUYACAR_FINAL_INTEGRATED.md`, `@.agents/rules/50-token-economy.md`, `production_artifacts/STATUS.md` y el último handoff.
2. Define un objetivo pequeño, una puerta de calidad, archivos permitidos y un presupuesto de tokens.
3. Activa `token-economy` antes de preparar contexto; comprime y referencia archivos en lugar de copiarlos.
4. Solicita primero pruebas/casos dorados y después implementación.
5. Ejecuta el workflow del ciclo y conserva el modo actual.
6. Verifica evidencia reproducible y auditoría de tokens antes de marcar el ciclo verde.
7. Comprime el handoff a decisiones y referencias; escribe la auditoría y actualiza `STATUS.md`.

No avances si hay preguntas bloqueantes, secretos detectados, tests rojos o cambios fuera del alcance.
