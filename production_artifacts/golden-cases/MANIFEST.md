# Manifiesto y Catálogo de Casos Dorados (Fase 0 / MVP)

Modo operativo: `SIMULATION`

Este catálogo define las 64 categorías y fixtures de casos dorados para validar el pipeline omnicanal sin llamadas a APIs externas ni cuentas reales.

## Estructura de Suites Planificadas

1. **Suite A: Fast-Path Determinista (Casos 01 al 16)**
   - Horarios de atención comercial (Tunja, Boyacá).
   - Ubicación física verificada (Av. Oriental - Cra. 6 #52-23, frente a Toyota).
   - Líneas de contacto oficiales (WhatsApp exclusivo 3151212223 / PBX 3022866969).
   - Requisitos de recepción de peritaje / consignación.

2. **Suite B: Consulta de Inventario Verificado (Casos 17 al 32)**
   - Coincidencia exacta de vehículo disponible en stock.
   - Consulta de marca/modelo con precio y kilometraje verificado.
   - Vehículo no disponible en stock (respuesta determinista sin inventar).
   - Filtro por rango de presupuesto y año.

3. **Suite C: Guards de Seguridad y Anti-Alucinación (Casos 33 al 48)**
   - Inyección de instrucciones en mensaje entrante (Prompt Injection / Jailbreak).
   - Detección y ofuscación de PII (cédulas, tarjetas, correos).
   - Intentos de extracción de URLs fuera de allowlist (SSRF / Phishing).
   - Forzado de alucinación de descuentos inexistentes.

4. **Suite D: Escalamiento y Triage Humano (Casos 49 al 64)**
   - Solicitudes de crédito complejas.
   - Negociaciones de precio fuera de política.
   - Mensajes ambiguos o fuera de dominio.
   - Activación de kill switch y fallback a asesor humano.

Todos los casos consumen fixtures en formato JSON en `production_artifacts/golden-cases/`.
