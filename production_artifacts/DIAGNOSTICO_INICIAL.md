# Diagnóstico Inicial del Sistema — BuyaCAR Omnicanal
Fecha: 2026-10-24
Modo operativo: SIMULATION

## (a) Versión de Go objetivo y hallazgos de documentación oficial
- Versión objetivo: Go 1.23+ (o Go 1.24 según disponibilidad de toolchain oficial).
- Entorno de ejecución: Windows PowerShell. El binario `go` no se encuentra en el PATH actual del sistema operativo. Se requiere instalar o configurar la ruta de Go en PATH para la compilación y ejecución de tests nativos en las siguientes fases.
- Hallazgos de arquitectura: Go provee capacidades nativas para concurrencia segura (`sync`, `atomic`, canales), structured logging (`log/slog`), y pruebas con race detection (`-race`).

## (b) Arquitectura resumida
- Núcleo desacoplado con Clean Architecture / Hexagonal.
- Entradas: Adaptadores de canal en modo SIMULATION (fixtures de mensajes).
- Capa de Procesamiento: Triage determinista -> Fast-Path -> Pipeline de IA (Gemini con fallback a Ollama y escalamiento humano).
- Capa de Control y Protección: Guard anti-alucinación, sanitización de PII, allowlist estricto de URLs y validación estricta de inventario verificado.
- Observabilidad: Trazas de 7 checkpoints, auditoría de consumo de tokens y kill switch global.

## (c) Plan de ciclos ordenado (Alineado a MVP Roadmap)
- Ciclo 0 (Actual): Preparación del control plane, SPEC inicial, ADR base, catálogo de casos dorados y entorno de evaluación.
- Ciclo 1: Núcleo Go (tipos, configuración segura, store idempotente, eventos).
- Ciclo 2: Módulo de inventario con fixture, caché determinista y coincidencia exacta.
- Ciclo 3: Fast-Path y motor de triage determinista.
- Ciclo 4: Guards de seguridad (PII, URLs, números, prompt injection).
- Ciclo 5: Integración con proveedores IA estructurados (Gemini / Ollama / Humano stubs).
- Ciclo 6: Orquestación end-to-end y persistencia de conversación.
- Ciclo 7: Observabilidad completa (7 checkpoints y auditoría de tokens).
- Ciclo 8: Consolidación MVP y demo reproducible en SIMULATION.

## (d) Preguntas bloqueantes
1. Ninguna pregunta técnica o funcional bloquea la preparación del Ciclo 0 (Control Plane).
2. Nota operativa no bloqueante para fases de compilación: Se requerirá Go 1.23+ en PATH antes de compilar código Go en `app_build/`.

## (e) Riesgos técnicos, legales y operativos
- Técnico: Falsos positivos en respuestas automáticas o alucinación de inventario/precios (mitigado con inventario verificado y guards deterministas).
- Legal: Tratamiento de datos personales de usuarios (Habeas Data / Ley 1581 de 2012 Colombia). Mitigado con política de cero PII en logs y respuestas.
- Operativo: Activación accidental de canales reales. Mitigado bloqueando de forma permanente el modo `SIMULATION` y requiriendo kill switch.
