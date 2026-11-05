---
name: kaizen-mvp
description: "Planifica, ejecuta y evalúa ciclos Kaizen PDCA hasta el MVP de BuyaCAR, usando métricas, trazas, puertas de calidad y acciones correctivas. Usar en cada ciclo y antes de declarar MVP."
---
# Skill: kaizen-mvp

## Preparar — PLAN
1. Lee `docs/MVP_ROADMAP.md`, `production_artifacts/STATUS.md`, el último handoff y la última evaluación.
2. Formula una hipótesis comprobable: `si hacemos X, entonces Y mejorará de A a B sin romper C`.
3. Declara una métrica primaria, métricas de seguridad, archivos permitidos, criterio de reversión y puerta de calidad.

## Ejecutar — DO
4. Haz el cambio mínimo que prueba la hipótesis.
5. Mantén `SIMULATION`; usa stubs y fixtures para proveedores/canales.
6. Captura trazas de lectura, triage, inventario, respuesta, guard, envío y CRM.

## Comprobar — CHECK
7. Ejecuta pruebas, fuzzing, race detector y benchmark que correspondan.
8. Compara línea base, ciclo anterior y resultado actual.
9. Clasifica cada objetivo como `improved`, `unchanged`, `regressed`, `not_measured`.
10. Escribe `production_artifacts/evaluations/cycle-N.json` conforme al schema de evaluación.

## Ajustar — ACT
11. Decide `keep`, `iterate`, `rollback` o `stop`.
12. Propón exactamente una mejora prioritaria para el siguiente ciclo.
13. Actualiza `STATUS.md`, el handoff y, si procede, un ADR.

No declares MVP por cantidad de archivos o por una respuesta subjetivamente buena. Exige las puertas del roadmap y una demostración reproducible.
