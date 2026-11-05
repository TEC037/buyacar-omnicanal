# Handoff Ciclo 4: Guard de Seguridad y Anti-Alucinación (Fase 4)

- **Agente emisor**: @security-qa / @go-engineer
- **Fecha**: 2026-10-26
- **Modo operativo**: `SIMULATION`
- **Objetivo**: Implementar `internal/guard` cumpliendo todos los requisitos de la sección `<GUARD>` de la especificación canónica y prueba de fuzzing.
- **Entrada**: `production_artifacts/handoffs/cycle-3.md`, `docs/MVP_ROADMAP.md`.
- **Cambios realizados**:
  1. `app_build/internal/guard/guard.go`: Validador de cifras de >= 5 dígitos contra inventario relevante y líneas oficiales (`3151212223`, `3022866969`, `3160502717`), allowlist HTTPS (`buyacar.com.co`), bloqueo de frases prohibidas, redacción de PII (Ley 1581 de 2012), filtro de ruido de Marketplace y recorte seguro por canal (`tiktok:400`, `instagram:450`, `facebook_marketplace:500`, `whatsapp:650`) sin truncar jamás precios ni teléfonos.
  2. `app_build/internal/guard/guard_test.go`: Pruebas unitarias exhaustivas + `FuzzGuardValidateOutbox`.
- **Riesgos y decisiones**: Cero alucinaciones numéricas permitidas; ante cualquier fallo en `ValidateOutbox`, se activa `MustEscalate = true`.
- **Siguiente acción**: Ejecutar Ciclo 5 / Fase 5 (`internal/ai` e `internal/skills`: cadena Gemini -> Ollama -> humano con JSON Schema estricto, un único reintento en JSON inválido, circuit breaker, cuota y ahorro de tokens).
