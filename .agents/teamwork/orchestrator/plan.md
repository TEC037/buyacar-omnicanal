# Plan de Auditoría e Inventario de Casos de Uso — BuyaCAR Omnicanal
## Filosofía: Antoine de Saint-Exupéry
> "Il semble que la perfection soit atteinte non quand il n'y a plus rien à ajouter, mais quand il n'y a plus rien à retrancher."

### Objetivo Supremo
Identificar, mapear y auditar de forma exhaustiva todos y cada uno de los casos de uso esenciales de BuyaCAR Omnicanal para el concesionario en Tunja, Boyacá. Contrastar lo que **DEBE TENER** (visión comercial y operativa perfecta) contra lo que **TIENE** (implementado en Go 1.23 sin dependencias), clasificando con precisión quirúrgica qué sobra (sobre-ingeniería, redundancia) y qué falta o debe simplificarse.

---

### Descomposición del Batallón de Especialistas

#### Frente 1: Canales & Conversión Omnicanal (`miner_channels_usecases`)
- **Agente**: `teamwork_spec_miner`
- **Ámbito**: Canales Meta (Facebook Marketplace, Messenger, Instagram) y WhatsApp Cloud API / Webhook.
- **Preguntas clave**:
  - ¿Cómo se capta el prospecto en Marketplace?
  - ¿Cómo se extrae el número de teléfono y se migra la conversación a WhatsApp?
  - ¿Qué casos de uso existen de persistencia de canal, reactivación, catálogo y handoff?
  - ¿Qué sobra en la gestión de canales (ej. multiplexores innecesarios, payload wrappers excesivos)?

#### Frente 2: Núcleo Cognitivo & Fast-Path Determinista (`miner_cognitive_usecases`)
- **Agente**: `teamwork_spec_miner`
- **Ámbito**: Fast-Path determinista para consultas frecuentes de Tunja (ubicación Av. Oriental - Cra 6 #52-23 frente a Toyota, horarios, garantía de usados certificados, crédito/financiación), Triage de intenciones, RAG híbrido BM25/Cosine en catálogo de vehículos, Memoria contextual y session state.
- **Preguntas clave**:
  - ¿Cuáles son los casos de uso cubiertos por el Fast-Path sin llamar a la IA?
  - ¿Cuáles son los casos de uso donde interviene el RAG y cómo busca en el inventario?
  - ¿Qué casos de uso conversacionales y de memoria son realmente necesarios?
  - ¿Qué sobra en los pipelines de IA/RAG (complejidad matemática o heurística innecesaria)?

#### Frente 3: Seguridad Comercial, Guardrails & CRM Minimalista (`miner_crm_guardrails_usecases`)
- **Agente**: `teamwork_spec_miner`
- **Ámbito**: Guardrails anti-alucinación numéricos CP5 (precios reales del inventario sin inventar descuentos), Redacción PII (Ley 1581 de protección de datos en Colombia), y el CRM minimalista de decisiones comerciales (agendamiento de peritaje físico en Tunja, asignación de asesor comercial, reserva/apartado de vehículo).
- **Preguntas clave**:
  - ¿Cuáles son los casos de uso de protección de precios y stock?
  - ¿Cuáles son los casos de uso de privacidad de datos personales para prospectos en Colombia?
  - ¿Cuáles son los casos de uso del CRM minimalista (peritaje, asesor, reserva)?
  - ¿Qué sobra en el CRM o en los guardrails (campos muertos, flujos barrocos)?

#### Frente 4: Evaluación Crítica de Minimalismo Saint-Exupéry (`critic_saint_exupery`)
- **Agente**: `teamwork_critic`
- **Ámbito**: Auditoría integral de esencialidad sobre todo el sistema (`app_build/internal/...` y `docs/...`).
- **Preguntas clave**:
  - Aplicando la navaja de Saint-Exupéry: ¿Qué código, lógica o abstracción sobra en BuyaCAR?
  - ¿El sistema responde con elegancia espartana al negocio real de Tunja?
  - ¿Cuáles son los casos de uso "parásitos" o hipercomplejos que deberían eliminarse o consolidarse?

---

### Hitos de Ejecución
- [x] **Hito 0**: Setup de orquestación, plan y briefing.
- [ ] **Hito 1**: Despliegue simultáneo de los 4 agentes especialistas.
- [ ] **Hito 2**: Recopilación de reportes, análisis y síntesis cruzada.
- [ ] **Hito 3**: Elaboración del Inventario Maestro Unificado de Casos de Uso (DEBE TENER vs TIENE vs QUITAR).
- [ ] **Hito 4**: Handoff final al parent y declaración de completitud.
