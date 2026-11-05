# Especificación de Arquitectura y Producto: BuyaCAR Omnicanal
- **Versión**: 1.1.0
- **Modo Operativo Activo**: `DRAFT` (Etapa B bajo supervisión humana; `SIMULATION` estricto en suites de pruebas)
- **Estado**: MVP Fases 0–8 Completadas + Etapa B (`DRAFT`) Activa
- **Fecha**: 2026-10-28

## 1. Misión y Alcance del Producto
BuyaCAR Omnicanal es un daemon modular y autónomo para el procesamiento de conversaciones comerciales sobre vehículos usados certificados en Tunja, Boyacá (Av. Oriental - Cra. 6 #52-23). 
El sistema clasifica las solicitudes entrantes, responde deterministamente consultas frecuentes con datos oficiales (Fast-Path), delega consultas semánticas complejas a un proveedor de IA (Gemini con fallback a Ollama) mediante JSON estructurado estricto, y escala a asesores humanos cuando se detectan riesgos, ambigüedad o falta de inventario verificado.

## 2. Principios Innegociables
1. **Modo `SIMULATION` Estricto**: Cero llamadas externas reales en pruebas, fixtures locales para inventario y canales.
2. **Cero Alucinación**: Jamás inventar vehículos, precios, teléfonos ni disponibilidades. Solo datos confirmados en caché oficial.
3. **Cero PII en Trazas y Prompts**: Detección y ofuscación de nombres, teléfonos, cédulas y correos antes de registros o llamadas a IA.
4. **Economía de Tokens**: Prioridad al Fast-Path determinista para resolver el mayor volumen de consultas con 0 tokens de LLM.
5. **Idempotencia y Resiliencia**: Procesamiento de mensajes deduplicado con TTL y ventana de reintentos exponencial.

## 3. Arquitectura Modular (Hexagonal / Clean)
- `cmd/buyacar`: Daemon y ejecución reproducible en `SIMULATION`.
- `internal/channel`: Interfaz canónica `Channel` y adaptador stub thread-safe.
- `internal/core`: Motor con kill switch atómico, reglas de omisión, pipeline de 12 pasos y suite de 64 casos dorados.
- `internal/store`: Persistencia de idempotencia SHA-256 y checkpoints de reanudación.
- `internal/links`: Repositorio editable con allowlist HTTPS (`buyacar.com.co`).
- `internal/inventory`: Caché ETag en memoria, ventanas de 15m/30m y los 6 estados deterministas.
- `internal/triage`: Normalización sub-milisegundo, clasificación fácil/difícil y causales de escalamiento.
- `internal/fast`: Motor determinista con confianza >= 0.80 y textos editables (`BusinessTexts`).
- `internal/guard`: Validador post-IA y pre-envío (cifras >= 5 dígitos, URLs HTTPS, PII, frases prohibidas y límites por canal).
- `internal/skills`: Compresión de contexto (intención + 1 a 3 vehículos + resumen) y caché aprobada.
- `internal/ai`: Cadena `Cache -> Gemini -> Ollama -> Humano` con JSON Schema estricto (`DisallowUnknownFields`).
- `internal/escalate`: Cola humana con redacción automática de PII.
- `internal/obs`: Trazas de 7 checkpoints exportables a Markdown y colector de métricas p50/p95/p99.

## 4. Contrato de Datos de Negocio Verificados
- **Ubicación**: Av. Oriental - Cra. 6 #52-23, frente a la Toyota, Tunja, Boyacá.
- **Canal Oficial WhatsApp**: 3151212223
- **Teléfonos Oficiales**: 3022866969 y 3160502717
- **Horario**: Lunes a Viernes 8:00 AM - 6:00 PM | Sábados 8:00 AM - 2:00 PM.
- **Sitio Web Oficial**: https://buyacar.com.co

## 5. Puertas de Calidad y Criterios de Aceptación
- Compilación limpia en Go (`app_build/`) sin advertencias.
- Cobertura de tests unitarios y validación de concurrencia (`-race`).
- Evaluación de 64 casos dorados planificados sin desvíos.
- Traza de 7 checkpoints (CP1 a CP7) sin fuga de PII.
