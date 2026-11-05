---
trigger: model_decision
description: "Aplicar al diseñar proveedores Gemini/Ollama, prompts, salidas JSON y guardas post-IA."
---
# Contratos de IA

- Separa instrucciones del sistema, tarea, datos y herramientas.
- Usa Structured Output/JSON Schema para resultados procesables.
- Rechaza claves desconocidas y permite un único reintento ante JSON inválido.
- No pidas cadenas de pensamiento privadas; pide decisiones y evidencias breves.
- No envíes PII innecesaria ni inventario completo: prefiltra uno a tres vehículos.
- Si falta un dato verificable, devuelve incertidumbre o escala; nunca adivines.
