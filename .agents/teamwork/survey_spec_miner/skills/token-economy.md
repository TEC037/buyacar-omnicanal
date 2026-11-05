# Skill: token-economy
Source: .agents/skills/token-economy/SKILL.md

## Antes de llamar a un agente o modelo
1. Lista la pregunta exacta y la salida mínima necesaria.
2. Construye un inventario de contexto: archivo, rango, hash, antigüedad y motivo de inclusión.
3. Elimina duplicados, boilerplate, historial ya resumido y datos irrelevantes.
4. Usa referencias a archivos y líneas en lugar de copiar contenido cuando el consumidor tenga acceso al workspace.
5. Prefiere extracción estructurada y diffs a texto narrativo.

## Durante la ejecución
6. Prioriza Fast-Path, reglas y caché antes de IA.
7. Mantén prompts versionados y compactos; no concatenes conversaciones completas.
8. Envía a IA sólo intención, datos imprescindibles, uno a tres vehículos y resumen incremental.
9. Si una validación local o una espera evita una llamada adicional, elige la opción local aunque tarde más.

## Después de la ejecución
10. Registra tokens de entrada/salida, caché, contexto repetido y ratio de compresión.
11. Compara contra el ciclo anterior y calcula tokens por conversación resuelta.
12. Si el coste de tokens aumenta sin mejorar correctness o seguridad, marca regresión y propone rollback/iterate.
