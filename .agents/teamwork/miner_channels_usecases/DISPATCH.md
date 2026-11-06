## 2026-11-06T16:38:02Z
Identidad: Eres un Spec Miner (`teamwork_spec_miner`) asignado al Frente 1 de BuyaCAR Omnicanal: Canales & Conversión Omnicanal.
Tu directorio de trabajo exclusivo es: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\miner_channels_usecases\

Obligatorio: Lee primero c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\ORIGINAL_REQUEST.md.

Misión:
Auditar e inventariar exhaustivamente, uno a uno, todos los casos de uso que el sistema DEBE tener y TIENE en la capa de Canales y Conversión Omnicanal (Facebook Marketplace, Messenger, Instagram, WhatsApp, captura de teléfono y enrutamiento), bajo la filosofía de Antoine de Saint-Exupéry ("La perfection est atteinte non quand il n'y a plus rien à ajouter, mais quand il n'y a plus rien à retrancher").

Fuentes a inspeccionar:
- Documentación: `docs/MARKETPLACE_PLAN.md`, `docs/SPEC.md`, `docs/MVP_DOCUMENTATION.md`, `docs/GUIA_CREDENCIALES_OMNICANAL.md`, `instructivo.md`.
- Código fuente: `app_build/internal/channel/`, `app_build/internal/server/`, `app_build/internal/triage/`.

Tareas:
1. Mapear cada caso de uso comercial para el concesionario en Tunja (Av. Oriental - Cra. 6 #52-23):
   - Ingestión de leads desde Marketplace (mensajes tipo "¿Sigue disponible?").
   - Captura inmediata de número celular en Marketplace para enrutar a WhatsApp.
   - Atención omnicanal en WhatsApp Cloud API (respuestas interactivas, catálogo, fotos).
   - Conversaciones en Messenger e Instagram DM.
   - Mantenimiento de contexto y sesión al saltar de un canal a otro (deduplicación de lead).
   - Detección de fallos o desconexión de canal y degradación elegante.
2. Contrastar minuciosamente:
   - ¿Qué casos de uso DEBE TENER un concesionario de autos usados en Tunja?
   - ¿Qué casos de uso TIENE actualmente implementados en el código de `app_build`?
3. Aplicar el principio de Saint-Exupéry:
   - ¿Qué sobra? (Redundancias, estructuras infladas, lógica superflua o sobre-ingeniería que no aporta valor real al concesionario).
   - ¿Qué falta o debe pulirse?
4. Entregar el reporte completo en: `c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\miner_channels_usecases\report.md`
   y un `handoff.md` en el mismo directorio con el resumen ejecutivo.
5. Al finalizar, envía un mensaje a tu orquestador avisando de la entrega.
