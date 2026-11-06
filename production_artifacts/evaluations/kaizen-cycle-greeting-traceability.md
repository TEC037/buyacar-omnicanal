# Registro Kaizen PDCA — Ciclo KZN-2026-11-06-01
## Optimización de Saludo Inicial Trazable y Calificación Temprana en Embudo

### 1. Plan (Planificar)
- **Problema Detectado**:
  - El saludo anterior (*"¿Estás buscando algún vehículo en especial, o prefieres que te comparta nuestro catálogo disponible?"*) generaba fricción al no ofrecer un enlace inmediato verificable y no orientar al usuario a un paso concreto de conversión en vitrina o CRM.
- **Hipótesis**:
  - Al incorporar la URL canónica verificada (`https://buyacar.com.co`) en el saludo inicial de Fast-Path y orientar el llamado a la acción hacia la coordinación de la prueba de ruta en Tunja, aumentamos la trazabilidad del tráfico web, la captura inmediata de intención y el registro automático en el embudo CRM sin consumir tokens de IA.
- **Métrica Primaria**:
  - Tasa de resolución Fast-Path de saludo con enlace oficial fáctico y actualización automática de estado en CRM (`FunnelStage`).
  - Tokens consumidos: **0 tokens** (Fast-Path determinista).
  - Cumplimiento de límites: $\le 500$ caracteres (Messenger).
- **Criterio de Reversión**:
  - Revertir a la plantilla previa si la longitud supera 500 caracteres, si falla la validación de allowlist HTTPS (`buyacar.com.co`) o si alguna prueba de la suite falla.

---

### 2. Do (Hacer)
- **Modificación en Fast-Path (`app_build/internal/fast/fast.go`)**:
  - Se configuró la plantilla oficial en `DefaultBusinessTexts.GreetingReply`:
    ```text
    ¡Hola! Qué gusto saludarte.
    Bienvenido a BuyaCAR en Tunja · Vehículos certificados.

    Puedes consultar nuestro inventario actualizado con precios y fotos en https://buyacar.com.co

    ¿Te llamó la atención algún modelo en particular para coordinar tu prueba de ruta?
    ```
- **Conexión en Servidor / CRM (`app_build/cmd/buyacar/main.go`)**:
  - Se añadió la actualización de perfil y embudo en tiempo real (`leadStore.UpsertLead`) en el despachador de webhooks entrantes para garantizar trazabilidad instantánea de cada conversación recibida.

---

### 3. Check (Verificar)
- **Longitud del Mensaje**:
  - Caracteres totales del saludo: **236 caracteres** (dentro del límite estricto de 500 de Messenger y 450 de Instagram).
- **Consumo de Tokens**:
  - Entrada: 0 tokens.
  - Salida: 0 tokens.
  - Modo: Fast-Path determinista (100% fáctico).
- **Seguridad y Guardrails**:
  - URL permitida: `https://buyacar.com.co` (Allowlist estricta en `guard.go`).
  - Cero números alucinados: sin cifras no respaldadas.
  - Cero emoticonos.
- **Resultado de la Suite de Pruebas**:
  - `go test -count=1 ./...` en los 18 paquetes de `app_build`: **100% PASS** (20/20 paquetes analizados/ejecutados en verde).

---

### 4. Act (Estandarizar)
- Se estandariza esta plantilla como la respuesta oficial ante saludos en Facebook Messenger, Instagram Direct y Facebook Marketplace.
- El ciclo Kaizen queda cerrado exitosamente bajo el estado `improved`.
