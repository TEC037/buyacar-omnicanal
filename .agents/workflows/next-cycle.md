# /next-cycle

Sólo procede si `/quality-gate` está `GREEN` y la evaluación del ciclo decide `keep` o `iterate`. Si decide `rollback` o `stop`, corrige primero.

1. Lee `production_artifacts/STATUS.md`.
2. Selecciona el siguiente bloque de `ORDEN_DE_IMPLEMENTACION`.
3. Actualiza el alcance y los casos dorados antes de programar.
4. Ejecuta `/start-cycle` sin cambiar el modo operativo.
