# Playbook de autoevaluación Kaizen

## Registro mínimo por ciclo

- `cycle_id` y fecha UTC.
- Hipótesis y variable cambiada.
- Línea base, meta y resultado.
- Evidencia: comandos, tests, benchmarks, fixtures y trazas.
- Estado por dimensión: correctness, security, latency, cost, operability y scope.
- Decisión: `keep`, `iterate`, `rollback` o `stop`.
- Una sola mejora prioritaria siguiente.

## Escala

- `0`: no medido o sin evidencia.
- `1`: falla o regresión crítica.
- `2`: funciona parcialmente o con deuda bloqueante.
- `3`: cumple la puerta del ciclo.
- `4`: supera la meta sin degradar otras dimensiones.

No promedies una dimensión crítica con las demás: cualquier `security=1`, `correctness=1` o `scope=1` bloquea el avance.
