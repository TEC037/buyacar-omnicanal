---
trigger: always_on
description: "Bucle PDCA/Kaizen, autoevaluación con trazas, control de regresiones y avance hasta MVP."
---
# Regla Kaizen

- Cada ciclo tiene una hipótesis, una métrica primaria, una meta, una puerta de calidad y un criterio de reversión.
- Cambia una variable principal por ciclo cuando sea posible.
- Compara con la línea base y con el último ciclo; registra `improved`, `unchanged` o `regressed`.
- La mejora sólo cuenta si mantiene seguridad, exactitud y funcionamiento.
- Usa datos de las trazas, pruebas y benchmarks; no uses autocalificaciones vagas.
- Si hay regresión crítica, detén el avance, conserva el artefacto y abre una acción correctiva.
- El MVP se declara sólo cuando todas sus puertas obligatorias están verdes y existe una demo reproducible en SIMULATION.
