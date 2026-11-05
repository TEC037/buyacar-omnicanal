---
name: go-quality
description: "Implementa y valida Go con arquitectura modular, pruebas, fuzzing, race detector y benchmarks. Usar al modificar app_build."
---
# Skill: go-quality

- Trabaja sólo en `app_build/` y documentación asociada al ciclo.
- Respeta interfaces y adaptadores; no acoples el core a canales.
- Añade prueba junto al cambio.
- Ejecuta formato, tests, race, vet, staticcheck y benchmark si afecta Fast-Path.
- Reporta comandos exactos y resultados; nunca declares verde sin evidencia.
