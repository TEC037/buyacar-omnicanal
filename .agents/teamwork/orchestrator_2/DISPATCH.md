## 2026-10-29T12:42:13Z

Planificar, diseñar e implementar una arquitectura RAG (Retrieval-Augmented Generation) local en Go 1.23 estándar para producción en BuyaCAR Omnicanal, indexando catálogo vehicular y documentos normativos para enriquecer semánticamente la inferencia en CP4 bajo estricto control de tokens y mandamientos de seguridad.

Working directory: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace
Integrity mode: demo
The team must NOT: Use pre-built libraries or frameworks for core functionality

## Requirements

### R1. Motor RAG Local y Estrategia de Recuperación
Implementar el motor RAG local y gratuito en Go 1.23 estándar sin frameworks ni dependencias de terceros pesadas. Debe operar primariamente en memoria mediante indexación y recuperación léxica/vectorial pura en Go (BM25 / TF-IDF y similitud coseno), con conmutación y fallback transparente a embeddings locales mediante Ollama (`nomic-embed-text`) cuando el servicio esté disponible en localhost.

### R2. Indexación y Chunking de Documentos
Crear y estructurar fixtures locales en Markdown y JSON dentro del repositorio para fichas técnicas de vehículos, políticas de garantía, requisitos de crédito y preguntas frecuentes de BuyaCAR Boyacá/Tunja. Implementar chunking semántico acotado y determinista para segmentar dichos documentos.

### R3. Integración en CP4 (IA Estructurada) y Presupuesto de Tokens
Integrar la recuperación semántica en CP4 (IA Estructurada) inyectando los fragmentos más relevantes y re-rankeados dentro del contexto de inferencia, respetando estrictamente el presupuesto máximo de tokens asignado y manteniendo latencias óptimas.

### R4. Gobernanza, Seguridad y Mandamientos Anti-Regresión
Preservar el modo SIMULATION por defecto, la redacción obligatoria de PII bajo la Ley 1581 de 2012 en todos los fragmentos procesados y devueltos, el filtro determinista anti-alucinación (bloqueo de cifras de 5+ dígitos no sustentadas en inventario o documentos indexados y allowlist HTTPS estricta a buyacar.com.co), y asegurar cero dependencias no autorizadas.

## Acceptance Criteria

### Arquitectura RAG y Recuperación
- [ ] El motor RAG opera localmente en Go 1.23 estándar sin librerías externas ni frameworks pesados.
- [ ] La recuperación híbrida funciona con motor primario en memoria (BM25 / TF-IDF y similitud coseno pura en Go) y conmutación a Ollama (`nomic-embed-text`) si está disponible.
- [ ] El sistema indexa y recupera fragmentos pertinentes de fichas de vehículos, políticas de garantía, requisitos de crédito y FAQ de BuyaCAR Boyacá/Tunja.
- [ ] La inyección de contexto en CP4 respeta el presupuesto de tokens configurado sin desbordar el contexto de inferencia ni degradar la latencia.

### Gobernanza y Seguridad
- [ ] El sistema opera bajo modo `SIMULATION` por defecto.
- [ ] Se preserva la redacción de PII conforme a la Ley 1581 de 2012 en todos los fragmentos y respuestas.
- [ ] El filtro determinista anti-alucinación bloquea cifras de 5+ dígitos no respaldadas en los datos y restringe URLs a `https://buyacar.com.co`.

### Verificación y Calidad
- [ ] 100% de la suite de pruebas pasa en verde sin caché (`go test -count=1 ./...`).
- [ ] La suite completa pasa con el detector de carreras activado (`go test -race ./...`) sin data races.
- [ ] Se implementan casos dorados de prueba programática que validan la relevancia y precisión de recuperación para consultas representativas de usuarios.

## 2026-10-29T12:44:28Z

INSTRUCCIÓN DIRECTA DEL USUARIO / CALLER:
Aplicar estrictamente economía de tokens (regla 50-token-economy.md):
1. Pasar DIRECTAMENTE a la fase de implementación de los hitos RAG sin encuestas ni exploraciones redundantes.
2. No retransmitir prompts, requisitos ni especificaciones completas a subagentes; usar rutas de archivos.
3. Informar avances en formato ultra-compacto: Estado actual (1 línea), archivos modificados y resultado de tests (1 línea).

## 2026-10-29T12:45:12Z

ORDEN ADICIONAL DEL USUARIO:
Incorporar a la cuadrilla un subagente Kaizen de mejora continua autoevaluativo para el ciclo RAG:
- Evaluar métricas objetivas: latencia p99 de recuperación (µs), hit-rate de chunks relevantes, tokens inyectados vs límite.
- Detección de regresiones con datos reales de benchmarks y tests (cero autocalificaciones ficticias).
- Reportes ultra-compactos (1 línea por métrica/evaluación).
