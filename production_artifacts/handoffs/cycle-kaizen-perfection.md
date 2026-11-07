# Handoff — Ciclo Kaizen de Perfección Omnicanal (`cycle-kaizen-perfection`)

- **Objetivo**: Eliminar cualquier brecha de silencio o respuesta genérica ante modelos frecuentes del mercado colombiano no presentes en vitrina y ante consultas que requieren desambiguación o escalamiento comercial.
- **Entrada**: Auditoría bit a bit previa (`3e5fdd3`) y solicitud de Kaizen de perfección.
- **Cambios**:
  1. `app_build/internal/triage/triage.go`: Expansión del lexicón automotriz colombiano (`Suzuki`, `Mitsubishi`, `BMW`, `Jeep`, `Subaru`, `Dodge`, `Citroen`, `Chery`, `BYD`, `JAC` y >50 modelos como `Prado`, `TXL`, `Corolla`, `RAV4`, `Mazda2`, `Mazda6`, `BT-50`, `Koleos`, `Kwid`, `Oroch`, `Onix`, `Sail`, `D-Max`, `Vitara`, `Swift`, `Jimny`, `Frontier`, `Versa`, `Sentra`, `Rio`, `Cerato`, `Sorento`, `Jetta`, `T-Cross`, `Amarok`, `Ranger`, `Explorer`, `Tucson`, `Montero`, `Forester`, `Compass`, `X3`, `Journey`).
  2. `app_build/internal/fast/fast.go`: Implementación de `BuildEscalationHandoffReply` para desambiguar múltiples años disponibles de un mismo modelo (`Mazda CX-5 2021` vs `2023`), ofrecer alternativas verificadas de la misma marca cuando se consulta una referencia agotada (`Toyota Prado` -> muestra `Hilux 2022` y `Fortuner 2024`; `Mazda 2` -> muestra `CX-5`, `CX-30`, `Mazda 3`), y responder al instante con handoff comercial en permuta/retoma, precio mínimo, estudio de crédito y Fasecolda (manteniendo silencio absoluto ante prompt injection).
  3. `app_build/cmd/buyacar/main.go`: Conexión de `BuildEscalationHandoffReply` y síntesis RAG tanto en webhooks reales de Meta (`/webhooks/meta`) como en `/api/copilot/draft`, validando cada cifra con `ValidateOutboxWithBackedNumbers`.
- **Pruebas**: Suite completa `go test -count=1 ./...` 100% en verde (`TestKaizenPerfection_ColombianMarketAndEscalationHandoffs`, `TestGoldenSuite_64Cases`, `TestRunCLI_*`).
- **Riesgos**: Ninguno; `out.Escalated` permanece en `true` para trazabilidad del asesor humano y compatibilidad con n8n (`should_send=false` en regateo).
- **Siguiente acción**: Desplegar en Railway (`production`) y verificar en vivo.
