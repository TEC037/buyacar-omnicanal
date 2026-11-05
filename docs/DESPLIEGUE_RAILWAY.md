# Guía Rápida: Despliegue en Railway (BuyaCAR Omnicanal + n8n)

Con el **Camino A**, tu sistema y n8n correrán en la nube 24/7 sin consumir recursos ni batería de tu computador, con certificado HTTPS automático y sin configurar Ngrok ni Docker localmente.

---

## Paso 1: Subir el repositorio a tu GitHub

1. Entra a [github.com/new](https://github.com/new) y crea un repositorio llamado `buyacar-omnicanal` (puede ser Privado o Público).
2. Abre una ventana de PowerShell en esta carpeta y ejecuta estos dos comandos (reemplazando tu usuario de GitHub):
   ```powershell
   git remote add origin https://github.com/TU_USUARIO/buyacar-omnicanal.git
   git push -u origin main
   ```

---

## Paso 2: Desplegar en Railway (2 Clics)

1. Entra a **[railway.app](https://railway.app)** e inicia sesión con tu cuenta de GitHub.
2. Haz clic en **+ New Project** $\rightarrow$ **Deploy from GitHub repo**.
3. Selecciona tu repositorio `buyacar-omnicanal`.
4. Railway detectará automáticamente el archivo [`Dockerfile`](file:///c:/Users/Buyac/OneDrive/Documentos/I&D/buyacar-omnicanal-agent-workspace/Dockerfile) y [`railway.json`](file:///c:/Users/Buyac/OneDrive/Documentos/I&D/buyacar-omnicanal-agent-workspace/railway.json) y comenzará la compilación.
5. En la pestaña **Settings** del servicio en Railway, haz clic en **Generate Domain** para obtener tu URL HTTPS pública (ejemplo: `buyacar-production.up.railway.app`).
6. En la pestaña **Variables**, agrega:
   - `GEMINI_API_KEY`: Tu clave de Google AI Studio.
   - `BUYACAR_OPERATING_MODE`: `AUTOPILOT` (o `SIMULATION` para pruebas).

---

## Paso 3: Agregar n8n en el mismo proyecto de Railway (1 Clic)

1. En el mismo panel de tu proyecto en Railway, haz clic en **+ New Service** $\rightarrow$ **Template**.
2. Escribe `n8n` y selecciona la plantilla oficial de n8n.
3. Haz clic en **Deploy**.
4. ¡Listo! Tendrás:
   - Tu motor BuyaCAR corriendo en `https://tu-app.up.railway.app`.
   - Tu n8n corriendo en `https://tu-n8n.up.railway.app`.
5. Entras a tu n8n en la nube, le das a **Import from File** y cargas [`n8n/buyacar_bridge_workflow.json`](file:///c:/Users/Buyac/OneDrive/Documentos/I&D/buyacar-omnicanal-agent-workspace/n8n/buyacar_bridge_workflow.json) o [`n8n/buyacar_standalone_gemini_workflow.json`](file:///c:/Users/Buyac/OneDrive/Documentos/I&D/buyacar-omnicanal-agent-workspace/n8n/buyacar_standalone_gemini_workflow.json).
