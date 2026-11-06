## 2026-11-06T16:38:12Z
Identidad: Eres un Spec Miner (`teamwork_spec_miner`) asignado al Frente 2 de BuyaCAR Omnicanal: Núcleo Cognitivo & Fast-Path Determinista.
Tu directorio de trabajo exclusivo es: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\miner_cognitive_usecases\

Obligatorio: Lee primero c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\ORIGINAL_REQUEST.md.

Misión:
Auditar e inventariar exhaustivamente, uno a uno, todos los casos de uso que el sistema DEBE tener y TIENE en el Núcleo Cognitivo, Fast-Path Determinista y Motor RAG (Fast-Path de preguntas frecuentes de Tunja, RAG híbrido BM25/Cosine, Triage de intenciones y Memoria contextual), bajo la filosofía de Antoine de Saint-Exupéry ("La perfection est atteinte non quand il n'y a plus rien à ajouter, mais quand il n'y a plus rien à retrancher").

Fuentes a inspeccionar:
- Documentación: `docs/SPEC.md`, `docs/MVP_DOCUMENTATION.md`, `docs/TOKEN_BUDGET.md`, `docs/KAIZEN_PLAYBOOK.md`, `instructivo.md`.
- Código fuente: `app_build/internal/fast/`, `app_build/internal/rag/`, `app_build/internal/triage/`, `app_build/internal/ai/`, `app_build/internal/store/`, `app_build/internal/inventory/`.

Tareas:
1. Mapear cada caso de uso:
   - Fast-Path determinista (cero costo de tokens IA, respuesta en <10ms):
     * Ubicación exacta de la vitrina en Tunja (Av. Oriental - Cra. 6 #52-23, frente a la Toyota).
     * Horarios de atención comercial y peritaje.
     * Política de garantía para vehículos usados certificados (1 año / 20.000 km, motor y caja).
     * Opciones de crédito y financiación bancaria (simulaciones aproximadas sin alucinar tasas).
     * Consulta de precios exactos de catálogo sin pasar por IA.
   - Motor RAG local híbrido BM25 + Cosine:
     * Indexación en memoria del catálogo de autos usados en Tunja.
     * Búsqueda por criterios (marca, modelo, año, presupuesto, kilometraje, tipo de caja/combustible).
     * Rankeo combinado BM25 léxico + Cosine semántico y deduplicación de chunks.
   - Triage de intenciones y Memoria contextual:
     * Detección de intención del comprador (comprar, financiar, permutar, queja, saludo).
     * Mantenimiento de memoria de sesión (vehículos de interés previo, preguntas anteriores).
2. Contrastar minuciosamente:
   - ¿Qué casos de uso DEBE TENER el motor cognitivo de BuyaCAR Tunja?
   - ¿Qué casos de uso TIENE implementados en el código de `app_build`?
3. Aplicar el principio de Saint-Exupéry:
   - ¿Qué sobra? (Complejidad matemática innecesaria, embeddings sobredimensionados, prompts gigantescos, heurísticas redundantes).
   - ¿Qué falta o debe pulirse para que sea rápido, infalible y elegante?
4. Entregar el reporte completo en: `c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\miner_cognitive_usecases\report.md`
   y un `handoff.md` en el mismo directorio con el resumen ejecutivo.
5. Al finalizar, envía un mensaje a tu orquestador avisando de la entrega.
