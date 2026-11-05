# Handoff Ciclo 1: Núcleo Modular en Go (Fase 1)

- **Agente emisor**: @go-engineer / @orchestrator
- **Fecha**: 2026-10-26
- **Modo operativo**: `SIMULATION`
- **Objetivo**: Implementar la capa de núcleo en `app_build/` conforme a la arquitectura canónica (`internal/channel`, `internal/core`, `internal/store`, `internal/links`, `cmd/buyacar`) y documentar `docs/TERMOMETRO_GOOGLE.md`.
- **Entrada**: `docs/SPEC.md`, `.agents/prompts/BUYACAR_FINAL_INTEGRATED.md`.
- **Cambios realizados**:
  1. `docs/TERMOMETRO_GOOGLE.md`: Registro de tecnologías por afinidad Google.
  2. `app_build/go.mod`: Módulo `buyacar.com.co/omnicanal` sin dependencias externas (100% Go stdlib).
  3. `app_build/internal/channel/channel.go` y `stub.go`: Interfaz `Channel` exacta y stub thread-safe en `SIMULATION`.
  4. `app_build/internal/store/store.go`: Store de idempotencia SHA-256 (`canal|conversacion|mensaje`) y checkpoints para reanudación.
  5. `app_build/internal/links/links.go`: Repositorio editable con validación estricta HTTPS, allowlist de dominios y filtro de activos.
  6. `app_build/internal/core/core.go` y `core_test.go`: Motor con kill switch atómico (< 1s), registro desacoplado de canales y reglas de omisión (propio, duplicado, > 9 semanas).
  7. `app_build/cmd/buyacar/main.go`: Daemon inicializado con `log/slog` en `SIMULATION`.
- **Pruebas**: Suite unitaria escrita en `core_test.go` (ejecución nativa pendiente de binario `go` en PATH del host).
- **Siguiente acción**: Implementar Fase 2 / Ciclo 2 (`internal/inventory` con caché, frescura y estados de coincidencia exacta).
