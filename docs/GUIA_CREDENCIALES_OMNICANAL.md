# Guía de Personalización de Credenciales — Hub Omnicanal Total BuyaCAR

Este documento explica cómo configurar cada uno de los 4 canales de comunicación en el nuevo flujo:
`n8n/buyacar_hub_omnicanal_total.json`.

---

## 🟢 Canal 1: WhatsApp

### Opción A (Recomendada: Con tu WhatsApp Business actual vía QR / Evolution API / Z-API):
1. Si usas Evolution API o Z-API, en el nodo **Canal 1: WhatsApp Inbound** configuras la URL del Webhook que te da el proveedor.
2. En el nodo **Despacho WhatsApp**:
   - Cambias la URL por la de tu instancia (ejemplo: `https://tu-api.com/message/sendText/instancia`).
   - Pones el API Key de tu proveedor.

### Opción B (WhatsApp Cloud API Oficial de Meta):
1. En el nodo **Despacho WhatsApp**:
   - URL: `https://api.whatsapp.com/v21.0/TU_PHONE_NUMBER_ID/messages` (reemplazas `TU_PHONE_NUMBER_ID` por el ID de tu número en Meta Developers).
   - En Headers -> Authorization: `Bearer TU_META_ACCESS_TOKEN` (reemplazas por tu token permanente).

---

## 🔵 Canal 2: Facebook Marketplace / Messenger

1. En el nodo **Canal 2: Facebook / Messenger / Marketplace**:
   - Copias la Webhook URL que te muestra n8n y la pegas en [developers.facebook.com](https://developers.facebook.com) en la sección **Webhooks -> Messenger -> messages**.
2. En el nodo **Despacho Facebook / Marketplace**:
   - En Headers -> Authorization: Reemplazas `TU_META_PAGE_ACCESS_TOKEN` por el Token de acceso de la página de BuyaCAR.

---

## 🟣 Canal 3: Instagram Direct

1. En el nodo **Canal 3: Instagram Direct**:
   - Pegas la Webhook URL en Meta Developers en la sección **Instagram -> messages**.
2. En el nodo **Despacho Instagram Direct**:
   - En Headers -> Authorization: Reemplazas `TU_INSTAGRAM_ACCESS_TOKEN` por tu Token de Instagram.

---

## 🌐 Canal 4: Chat Web en buyacar.com.co (¡Sin credenciales!)

1. El nodo **Canal 4: Chat Web Widget** no necesita tokens ni configuraciones complejas.
2. n8n te entrega un código que se pega en WordPress (en el pie de página o con un plugin de encabezados/scripts).
3. Cualquier visitante de `buyacar.com.co` podrá chatear y recibir respuestas en vivo directamente desde el navegador.

---

## 👨‍💼 Escalamiento Humano (Descuentos / Quejas / Retomas)

Cuando un cliente pide rebaja o permuta, el flujo lo envía a la rama **Escalar a Humano**.
Allí puedes conectar:
- Un nodo de **Telegram** para que te llegue una notificación a tu celular.
- Un nodo de **Google Sheets** para registrar al cliente como prospecto pendiente.
- O un correo electrónico a tu equipo de ventas.
