---
trigger: always_on
description: "Prioridad primordial de economía de tokens: minimizar contexto, repeticiones y salida aunque aumente el tiempo, sin degradar seguridad ni exactitud."
---
# Economía de tokens — prioridad primordial

Orden de optimización obligatorio: **seguridad y exactitud > cumplimiento > tokens > latencia > comodidad del agente**. Se permite gastar más tiempo de cómputo, hacer más pasos locales y esperar más por una respuesta si reduce tokens transferidos o generados.

- No reenvíes contexto ya disponible: usa referencias a archivos, índices, hashes, resúmenes y checkpoints.
- Lee sólo las líneas o archivos necesarios; aplica recuperación progresiva y no cargues el repositorio completo.
- Resume historial y artefactos antes de pasarlos a otro agente; conserva el original en disco.
- Usa caché de inventario, prompts y respuestas validadas; mide hit/miss.
- Prefiere reglas deterministas y Fast-Path antes de llamar a IA.
- Reduce la salida a diffs, decisiones, errores reproducibles y el siguiente paso.
- No repitas instrucciones, resultados, código ni documentación en handoffs.
- Mide tokens de entrada, salida, contexto repetido, compresión, caché y tokens por tarea.
- Si no puedes medir el ahorro, marca `not_measured`; no afirmes que hubo optimización.
- Un ciclo puede quedar bloqueado por exceso de tokens aunque sus tests funcionales estén verdes.
