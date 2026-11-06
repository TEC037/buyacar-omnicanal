# Handoff Report — Frente 3: Seguridad Comercial, Guardrails CP5 & CRM Minimalista

**Agente**: Spec Miner (`teamwork_spec_miner`)  
**Directorio**: `c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\miner_crm_guardrails_usecases\`  
**Fecha**: 2026-11-06T16:48:30Z  
**Tipo**: Hard Handoff (Tarea de Minería y Auditoría Completa)

---

## 1. Observation

Durante la auditoría exhaustiva del Frente 3 se inspeccionaron directamente las fuentes canónicas de documentación y el código fuente en Go de `app_build`:

1. **Guardrails CP5 (`app_build/internal/guard/guard.go`)**:
   - Línea 14: `reFiveOrMoreDigits = regexp.MustCompile(\`\\b\\d{5,}\\b\`)`.
   - Líneas 160-168: Se buscan todas las secuencias contiguas de 5+ dígitos en la respuesta y se contrastan contra `allowedNumbers`:
     ```go
     digitsFound := reFiveOrMoreDigits.FindAllString(trimmed, -1)
     for _, numStr := range digitsFound {
         if !allowedNumbers[numStr] {
             return ValidationResult{Approved: false, MustEscalate: true, RejectionReason: fmt.Sprintf("hallucinated_number_blocked:%s", numStr)}
         }
     }
     ```
   - Al ejecutar en PowerShell:
     `[regex]::Matches('Precio $150.000.000 COP', '\b\d{5,}\b').Count` $\rightarrow$ Resultado: `0`.
     Los separadores de miles (`.` y `,`) rompen la cadena en bloques de 3 dígitos (`150`, `000`, `000`), burlando el guardrail si la IA genera precios formateados.
   - Líneas 20-28: `ForbiddenPhrases` contiene únicamente 6 expresiones prohibidas centradas en aprobación de crédito y precio mínimo. No contiene expresiones comunes en Boyacá como `"papeles gratis"`, `"traspaso gratis"`, `"soat de regalo"` ni `"obsequio de papeles"`.
   - No existe inspección determinista en CP5 para números de 4 dígitos (años de modelo como `2022` o kilometrajes bajos como `4500` km).

2. **Protección de Datos Personales y Ley 1581 (`app_build/internal/guard/guard.go` y `crm/lead_scoring.go`)**:
   - Línea 17: `reCardOrID = regexp.MustCompile(\`\\b\\d{7,16}\\b\`)`.
   - Al evaluar:
     `[regex]::Matches('Mi cedula es 1.049.521.890 y tel 310-456-7890', '\b\d{7,16}\b').Count` $\rightarrow$ Resultado: `0`.
     `RedactPII` omite números de identificación o teléfonos personales si contienen puntos, espacios o guiones.
   - En `crm/lead_scoring.go:41-59`, `LeadProfile` almacena teléfono, canal, presupuesto, vehículo y notas, pero **carece de campo de consentimiento de Habeas Data** (`ConsentHabeasData`), marca de tiempo de autorización o enlace a política.
   - En `crm/lead_scoring.go:120-176`, `MemoryLeadStore` y en `crm/crm.go:63-75`, `MemoryManager`, no existe política de retención ni depuración (sin TTL, sin purga y sin método `DeleteLead`).

3. **CRM Comercial Minimalista (`app_build/internal/crm/` y `server/dashboard_handler.go`)**:
   - En `crm/lead_scoring.go:12-21`, el embudo de ventas cuenta con 6 etapas: `captura_t0`, `calificado`, `cita_peritaje`, `negociacion_humana`, `cierre_reserva`, `descartado`.
   - En `crm/crm.go:20-51`, existe en paralelo un sistema de tareas (`TaskScheduleTestDrive`, `TaskCommercialFollowup`, `TaskCreditApplication`, `TaskGeneralInquiry`) con estados `pending`, `completed`, `escalated`, `cancelled`.
   - En `crm/webhook_exporter.go:48-195`, existen 203 líneas de código para exportar tareas a un CRM externo con firmas HMAC-SHA256 (`X-BuyaCAR-Signature-256`), reintentos y backoff.
   - En `server/dashboard_handler.go:827-862`, cada petición `GET /api/crm/leads` inyecta recursivamente en memoria 3 prospectos mock de prueba ("Carlos Mendoza", "Mariana Silva", "Juan Carlos Pérez").
   - En `server/dashboard_handler.go:903-912`, la decisión `"agendar_cita"` hardcodea la fecha sumando estrictamente 24 horas (`time.Now().Add(24 * time.Hour)`) y fija una sola sede. La decisión `"cerrar_reserva"` actualiza `lead.Stage = crm.StageClosedWon`, pero no interactúa con `inventory.Cache` ni marca el vehículo como reservado.

4. **Escalamiento en Triage (`app_build/internal/triage/triage.go`)**:
   - Líneas 77-121: Detecta y escala deterministamente descuentos, permutas/retomas, Datacrédito, siniestros/Fasecolda y prompt injection.
   - Sin embargo, no incluye palabras clave de frustración del cliente con el bot (ej. `"humano"`, `"persona"`, `"asesor humano"`, `"no me entiende"`, `"robot"`). Estas solicitudes caen en `IntentComplexQuery` (confianza 0.55) y son enviadas a la IA en vez de escalar inmediatamente a un humano.

5. **Ejecución de Pruebas**:
   - Se ejecutó: `cd app_build; go test -v ./internal/guard ./internal/crm ./internal/escalate`.
   - Resultado: 13 pruebas unitarias y de fuzzing pasaron con `PASS` en 0.00s a 0.02s (`buyacar.com.co/omnicanal/internal/guard` y `buyacar.com.co/omnicanal/internal/crm`).
   - Se constató que `internal/escalate` no posee archivo de pruebas unitarias (`[no test files]`).

---

## 2. Logic Chain

1. **Premisa 1**: Si el regex `reFiveOrMoreDigits` solo busca dígitos contiguos y los precios en lenguaje natural suelen escribirse con puntos o comas (ej. `$150.000.000`), entonces cualquier modelo de IA que responda con separadores de miles evadirá CP5 y el sistema publicará un precio no respaldado (Observación 1).
2. **Premisa 2**: La Ley 1581 de 2012 exige autorización previa, expresa e informada del titular antes del tratamiento de sus datos y confidencialidad en trazas. Dado que `LeadProfile` no guarda el consentimiento y `RedactPII` no limpia separadores en cédulas como `1.049.521.890`, el sistema presenta riesgos de cumplimiento normativo (Observación 2).
3. **Premisa 3**: Bajo la filosofía de Saint-Exupéry ("la perfección se alcanza cuando no hay nada más que quitar"), el software debe erradicar la burocracia que no aporta a la venta en Tunja. Mantener `crm.Task` y `crm.LeadProfile` como dos subsistemas paralelos, junto a un exportador HMAC corporativo y la generación de tareas para simples saludos de Fast-Path, genera sobrecosto de código y mantenimiento sin beneficio comercial real (Observación 3).
4. **Premisa 4**: Un CRM comercial en Tunja debe ser letal: cuando se pulsa "Reservar ($2M)", el carro debe congelarse en inventario para que el bot no lo siga ofreciendo; y cuando el cliente pide un humano o muestra enojo, el sistema debe transferir en 0 ms en vez de delegar en un LLM (Observaciones 3 y 4).

---

## 3. Caveats

- **No invasión de código**: Conforme al rol estricto de Spec Miner, no se modificó ningún archivo de código fuente en `app_build/`. Todos los hallazgos son diagnósticos e inventarios sobre el estado actual.
- **Entorno Windows sin GCC**: La bandera `-race` no fue ejecutada en esta sesión debido a la ausencia de compilador CGO en el PATH del host, manteniéndose la validación con `go test -v` uncached estándar.
- **Fixtures RAG verificados**: Se constató que los fixtures RAG en `app_build/fixtures/rag/` contienen información fáctica exacta de Boyacá (20.000 km de garantía, peritaje de 150 puntos, retoma en 45 min y abono de $2M), sirviendo como autoridad de conocimiento para CP5.

---

## 4. Conclusion

El sistema BuyaCAR Omnicanal cuenta con una base arquitectónica en Go limpia, robusta y con excelente velocidad determinista (< 1 ms en Fast-Path y Triage). Sin embargo, para satisfacer a cabalidad las exigencias de Seguridad Comercial, Ley 1581 y CRM de Tunja bajo el principio de Antoine de Saint-Exupéry:

1. **Sobra**: El subsistema duplicado `crm.Task`, el exportador webhook HMAC externo `webhook_exporter.go`, la inyección de leads mock en el handler de producción, y la creación de tareas por consultas generales resueltas en Fast-Path.
2. **Falta / Debe pulirse**:
   - Normalizar puntuación antes de CP5 (`guard.go`) para evitar bypass de precios formateados.
   - Normalizar puntuación antes de `RedactPII` para cédulas y teléfonos con puntos o guiones.
   - Agregar validación de años (4 dígitos) y frases colombianas de trámites gratis (`ForbiddenPhrases`).
   - Agregar escalamiento inmediato ante frustración o petición de humano en Triage.
   - Conectar la decisión "Reservar ($2M)" con la congelación del vehículo en `inventory.Cache`.
   - Incorporar consentimiento de Habeas Data y política de depuración en `LeadProfile`.

El inventario completo de 30 capacidades y 14 casos de borde ha quedado documentado en `report.md`.

---

## 5. Verification Method

Para verificar independientemente todos los hallazgos y evidencias reportadas:

1. **Inspeccionar el reporte exhaustivo**:
   `c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\miner_crm_guardrails_usecases\report.md`
2. **Verificar comportamiento de regex en PowerShell**:
   ```powershell
   # Probar ceguera ante precios con puntos
   powershell -Command "[regex]::Matches('Precio $150.000.000 COP', '\b\d{5,}\b').Count"
   # Retorna: 0 (Bypass demostrado)

   # Probar ceguera ante cédula con puntos
   powershell -Command "[regex]::Matches('Mi cedula es 1.049.521.890', '\b\d{7,16}\b').Count"
   # Retorna: 0 (PII sin ofuscar demostrado)
   ```
3. **Ejecutar suite de pruebas de los módulos auditados**:
   ```powershell
   cd c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\app_build
   go test -v ./internal/guard ./internal/crm ./internal/escalate
   ```
   *Debe arrojar PASS en todas las pruebas unitarias y confirmar `[no test files]` en `internal/escalate`.*
