# Termómetro de Afinidad con el Ecosistema Google

Este documento registra todas las tecnologías elegidas para BuyaCAR Omnicanal, clasificadas por su afinidad con el ecosistema Google, conforme a la Regla de Desarrollo #3.

| Tecnología / Componente | Rol en la Arquitectura | Nivel de Afinidad (0 a 3) | Justificación / ADR Asociado |
|---|---|---|---|
| **Go (Golang) 1.23+** | Lenguaje principal del daemon y motor concurrente | **3 (Nativo Google)** | Creado y mantenido por Google (`go.dev`). Tipado estático, concurrencia segura y `log/slog` estándar. |
| **Google Gemini API** | Proveedor primario de IA generativa con Structured Output | **3 (Nativo Google)** | Motor principal para consultas complejas con JSON Schema estricto. |
| **`log/slog` (Go stdlib)** | Observabilidad y logging estructurado sin PII | **3 (Nativo Google)** | Biblioteca estándar oficial del equipo de Go en Google. |
| **`testing` & `fuzzing` (Go stdlib)** | Pruebas unitarias, benchmarks y fuzzing nativo | **3 (Nativo Google)** | Herramientas integradas en el toolchain oficial de Go. |
| **Ollama (`llama3.2:3b`)** | Proveedor secundario (fallback local) de IA | **1 (Excepción Aprobada)** | Exigido explícitamente por la especificación canónica como fallback local ante fallo o cuota de Gemini. |
| **SQLite / In-Memory Store** | Almacenamiento de idempotencia, enlaces, leads y métricas | **1 (Excepción Aprobada)** | Exigido por la especificación para operación local embebida sin dependencias externas pesadas. |

## Escala de Afinidad
- **3**: Producto o lenguaje oficial de Google.
- **2**: Proyecto open-source liderado o fuertemente respaldado por Google.
- **1**: Estándar de industria compatible o requerido por especificación (requiere registro).
- **0**: Tecnología ajena sin integración directa (requiere ADR explícito).
