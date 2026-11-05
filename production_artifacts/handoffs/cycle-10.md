# Handoff Ciclo 10: Activación de Modo `DRAFT` (Etapa B bajo Orden Humana Explícita)

- **Agente emisor**: @orchestrator / @go-engineer / @ai-integrator / @release-operator
- **Fecha**: 2026-10-28
- **Modo operativo activo**: `DRAFT` (autorizado por orden humana explícita: *"pasemos a DRAFT"*; `DefaultSafeConfig()` conserva `SIMULATION` para suites de regresión).
- **Objetivo**: Ejecutar la transición formal a la Etapa B (`DRAFT`) conforme a la Sección 4 de `instructivo.md`, conectando proveedores reales de IA con delimitación anti-prompt-injection (`GeminiProvider` por variable de entorno + `OllamaProvider` en `127.0.0.1:11434` con `llama3.2:3b`), el canal piloto de correo en modo borrador (`draft_recorded`), y la puerta cuantitativa de salida hacia `AUTOPILOT` (mínimo 100 conversaciones reales en borrador).
- **Entrada**: `instructivo.md` (Etapa B), `production_artifacts/handoffs/cycle-9.md`, orden directa del usuario.
- **Archivos creados/modificados**:
  1. `app_build/internal/ai/providers.go` & `ai_test.go`: Implementación de `BuildDelimitedPrompt` (separa instrucciones inmutables, `<INVENTARIO_VERIFICADO>` de 1 a 3 vehículos y `<DATOS_NO_CONFIABLES_CLIENTE>` con redacción previa de PII), `OllamaProvider` restringido estrictamente a loopback (`127.0.0.1:11434`, modelo `llama3.2:3b` verificado localmente) y `GeminiProvider` leyendo `GEMINI_API_KEY` solo de variables de entorno.
  2. `app_build/internal/channel/email_pilot.go`: Métodos `ReviewDraft` y `DraftGateMetrics` para auditar el porcentaje de borradores aprobados por asesores humanos sin edición (meta `>= 85%`).
  3. `app_build/internal/obs/obs.go` & `obs_test.go`: Estructura `DraftExitGateReport` y evaluador `EvaluateDraftExitGate` que verifica los 5 requisitos de salida de `DRAFT` (`>= 100` conversaciones, `0` cifras alucinadas, `0` duplicados, `>= 70%` Fast-Path sin IA y `>= 85%` borradores aprobados sin edición).
  4. `app_build/internal/core/core.go`: Función `DefaultDraftConfig()` (`OperatingMode: channel.ModeDraft`).
  5. `app_build/cmd/buyacar/main.go`: Daemon principal operando en modo `DRAFT` con el canal piloto de correo (`email`) activo, cliente HTTP ETag de inventario, orquestador `Cache -> Gemini -> Ollama (llama3.2:3b) -> Humano` y telemetría de la puerta de salida hacia `AUTOPILOT`.
- **Estado del entorno verificado**:
  - `ollama list`: modelo `llama3.2:3b` (`a80c4f17acd5`, 2.0 GB) instalado y disponible localmente en `127.0.0.1:11434`.
  - `GEMINI_API_KEY`: no definida aún en el entorno actual (`gemini_api_key_present: false`); el `Orchestrator` conmuta limpiamente a `OllamaProvider` (`llama3.2:3b`) cuando una consulta no es cubierta por Fast-Path.
- **Riesgos y bloqueos**:
  - Ningún mensaje se envía al cliente final en modo `DRAFT` (`SendDrafted = "draft_recorded"`); el modo `AUTOPILOT` permanece bloqueado por código (`ErrEmailAutopilotBlocked`) y por la puerta `ready_for_autopilot_gate: false` hasta alcanzar las 100 conversaciones reales revisadas.
- **Siguiente acción**: Acumular las primeras 100 conversaciones en borrador en el canal piloto de correo y opcionalmente exportar `GEMINI_API_KEY` en el entorno del sistema.
