# ADR 0001: Implementación Modular en Go, Modo SIMULATION y Prioridad de Economía de Tokens

- **Fecha**: 2026-10-24
- **Estado**: Aceptado
- **Decisores**: @orchestrator, @architect, @go-engineer

## Contexto
El producto BuyaCAR Omnicanal requiere alta disponibilidad, latencia predecible, ausencia de fugas de memoria o condiciones de carrera, y una gobernanza estricta sobre el consumo de tokens y datos personales.

## Decisión
1. Desarrollar el daemon en Go estructurado en `app_build/` bajo arquitectura hexagonal para permitir stubs inmediatos.
2. Forzar por defecto `operatingMode: SIMULATION`. Todo mensaje saliente en simulación genera un registro en fixture/log sin tocar APIs reales.
3. Anteponer un motor de Fast-Path determinista antes de cualquier invocación a modelos generativos (Gemini/Ollama) para maximizar la economía de tokens.
4. Implementar un Guard de salida con allowlist de URLs y verificación contra inventario en caché para impedir alucinaciones.

## Consecuencias
- **Positivas**: Cero riesgo de costos imprevistos, cero llamadas externas accidentales, p99 determinista para consultas frecuentes, máxima testabilidad con `go test -race`.
- **Negativas / Mitigación**: El código en Go requiere compilación explícita; se mantendrán stubs e interfaces desacopladas para pruebas instantáneas.
