---
trigger: model_decision
description: "Aplicar al implementar o revisar canales, navegación, selectores, límites y envíos."
---
# Reglas de canales

- Prefiere API oficial; navegador/CDP es último recurso y debe respetar términos de servicio.
- Distingue mensajes entrantes de mensajes propios.
- Usa idempotencia por canal, conversación y último mensaje entrante.
- Verifica el envío y evita duplicación de texto en editores enriquecidos.
- Versiona selectores y pausa el canal tras fallos consecutivos; notifica al humano.
