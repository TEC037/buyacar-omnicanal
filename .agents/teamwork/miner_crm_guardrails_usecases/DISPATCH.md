## 2026-11-06T16:38:22Z
Identidad: Eres un Spec Miner (`teamwork_spec_miner`) asignado al Frente 3 de BuyaCAR Omnicanal: Seguridad Comercial, Guardrails & CRM Minimalista.
Tu directorio de trabajo exclusivo es: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\miner_crm_guardrails_usecases\

Obligatorio: Lee primero c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\ORIGINAL_REQUEST.md.

Misión:
Auditar e inventariar exhaustivamente, uno a uno, todos los casos de uso que el sistema DEBE tener y TIENE en la capa de Seguridad Comercial (Guardrails anti-alucinación CP5, Protección de PII Ley 1581) y el Panel CRM Minimalista (Agendamiento de peritaje en Tunja, Asignación de asesor humano, Reserva/separación de vehículos), bajo la filosofía de Antoine de Saint-Exupéry ("La perfection est atteinte non quand il n'y a plus rien à ajouter, mais quand il n'y a plus rien à retrancher").

Fuentes a inspeccionar:
- Documentación: `docs/SPEC.md`, `docs/MVP_DOCUMENTATION.md`, `docs/OPERATING_GUIDE.md`, `instructivo.md`.
- Código fuente: `app_build/internal/guard/`, `app_build/internal/crm/`, `app_build/internal/escalate/`.

Tareas:
1. Mapear cada caso de uso:
   - Guardrails numéricos anti-alucinación CP5:
     * Detección y bloqueo de precios inventados por la IA o inconsistentes con el catálogo de usados certificados.
     * Validación de kilometrajes y años reales de los vehículos en vitrina.
     * Prevención de promesas no autorizadas (descuentos inventados, regalos de papeles, trámites gratis).
   - Redacción de PII y Protección de Datos (Ley Estatutaria 1581 de 2012 de Colombia):
     * Ofuscación de cédulas de ciudadanía, teléfonos personales y tarjetas en logs y traces.
     * Manejo del consentimiento de Habeas Data antes de almacenar el prospecto en CRM.
     * Retención y depuración de datos sensibles.
   - CRM Comercial Minimalista para el Concesionario en Tunja:
     * Agendamiento de cita presencial para peritaje técnico de vehículo (para compra o retoma/retoma de usados).
     * Asignación de asesor comercial humano con notificación instantánea.
     * Solicitud de reserva o apartado de vehículo específico del catálogo.
     * Gestión del estado del prospecto (Nuevo, En Contacto, Cita Agendada, Negociación, Separado, Cerrado/Perdido).
     * Escalamiento a humano cuando el cliente expresa frustración o complejidad comercial.
2. Contrastar minuciosamente:
   - ¿Qué casos de uso DEBE TENER el concesionario BuyaCAR en Tunja?
   - ¿Qué casos de uso TIENE actualmente implementados en `app_build`?
3. Aplicar el principio de Saint-Exupéry:
   - ¿Qué sobra? (Burocracia innecesaria en el CRM, estados superfluos que nadie en Tunja usa, filtros de seguridad redundantes o regex infladas).
   - ¿Qué falta o debe pulirse para que sea un CRM espartano, eficaz y comercialmente letal?
4. Entregar el reporte completo en: `c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\miner_crm_guardrails_usecases\report.md`
   y un `handoff.md` en el mismo directorio con el resumen ejecutivo.
5. Al finalizar, envía un mensaje a tu orquestador avisando de la entrega.
