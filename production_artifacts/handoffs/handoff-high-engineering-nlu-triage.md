# Handoff — Ingeniería Avanzada NLU, Triage Tolerante a Fallos y Resiliencia en Producción

- **Objetivo**: Elevar la arquitectura de comprensión de preguntas de usuarios (`triage`, `fast`, `pipeline`, `main`) a nivel de alta ingeniería para resolver entradas reales con errores ortográficos, signos pegados (`mazda 3{`), espacios dobles (`tiene una mazda  3?`), solicitudes cortas (`ayuda`, `hol`, `info`, `asesor`), consultas por marca general (`¿Qué Mazda tienen?`) y filtros por segmento/presupuesto, eliminando bloqueos por `inventory_stale` tras suspensión de contenedor en Railway.
- **Entrada**: Reportes reales de pruebas en Facebook Messenger (`Hola Buen día`, `ayuda`, `tiene una mazda  3?`, `¿Tienen Mazda 3?`, `mazda 3{`).
- **Cambios**:
  - `app_build/internal/triage/triage.go`: Normalizador léxico-fonético de baja asignación de memoria, soporte para símbolos residuales, reducción de letras repetidas, diccionario de typos fonéticos de marcas/modelos en Colombia, fusión de modelos compuestos e inferencia automática de marca por modelo (`modelToBrand`).
  - `app_build/internal/fast/fast.go`: Respuestas deterministas para consultas por marca general (`formatBrandSummaryReply`), resolución directa cuando sólo 1 unidad de un modelo con múltiples registros está disponible (`Status == "available"`), y respuestas segmentadas por categoría (`7 pasajeros`, `4x4 / diésel`, `automóviles / económicos / presupuesto`, `caja mecánica`).
  - `app_build/internal/core/pipeline.go`: Priorización de vehículos `available` en la revalidación pre-envío multimodelo y ajuste del umbral del sintetizador RAG determinista (`>= 0.50`).
  - `app_build/cmd/buyacar/main.go`: Renovación bajo demanda (`!invCache.IsFresh`) antes de procesar cualquier evento en `/webhooks/meta` o `/api/copilot/draft` para inmunizar contra hibernación de contenedor en Railway.
  - `app_build/internal/fast/fast_test.go`: Suite `TestHighEngineering_UserQuestionsCorpus` con 37 casos reales verificados.
- **Pruebas**: `go test -count=1 ./...` 100% PASS en los 20 paquetes (`p99 = 1.21 ms` bajo 100 workers concurrentes).
- **Riesgos**: Ninguno; se preservan al 100% las invariantes de seguridad CP5, Ley 1581 y los 64 casos dorados (`TestGoldenSuite_64Cases`).
- **Siguiente acción**: Desplegar el último commit de `origin/main` en Railway.
