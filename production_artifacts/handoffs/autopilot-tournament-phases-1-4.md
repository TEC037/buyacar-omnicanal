# Handoff — Torneo Kaizen de Autopilotaje y Ejecución Fases 1–4 (Facebook Marketplace)

- **Objetivo**: Ejecutar el plan aprobado (`autopilot_marketplace_plan.md`, Fases 1–4) para lograr procesamiento autónomo (`0` intervención humana en consultas rutinarias) en Facebook Marketplace / Messenger / Omnicanal, comparando experimentalmente 4 arquitecturas candidatas.
- **Entrada**: `docs/MARKETPLACE_PLAN.md` (Hitos M0, M1, M2 completados) y `autopilot_marketplace_plan.md` aprobado.
- **Cambios**:
  - **Fase 1 (Memoria de Hilo + Anuncio Implícito)**: `ClassifyWithContext` en `app_build/internal/triage/triage.go` + limpieza de signos de puntuación (`[`, `]`, `(`, `)`, `:`, `;`) y guardia `!isPolicyOrWorkshopQuery`.
  - **Fase 2 (Sintetizador RAG-Direct + Consultas Compuestas)**: `SynthesizeFromRAGChunk`, `retrieveCompoundPolicyClause` y soporte de estrategias (`A1_fast_only`, `A2_rag_synth`, `A3_llm_only`, `A4_adaptive`) en `app_build/internal/core/pipeline.go`.
  - **Fase 3 (Torneo Kaizen de 4 Posibilidades)**: `TestAutopilotTournament_Compare4Possibilities` en `app_build/test/autopilot_tournament_test.go` sobre 30 conversaciones reales de Marketplace.
  - **Fase 4 (Webhook Event-Driven + Kill Switch)**: Extracción de `referral.ad_title` y callback `SetOnInboundConversation` en `app_build/internal/channel/meta_omnichannel.go`, rutas `/webhooks/meta` y `/api/killswitch` en `app_build/internal/server/dashboard_handler.go` y cableado en `app_build/cmd/buyacar/main.go`.
- **Pruebas**:
  - `go test -v -count=1 ./test -run TestAutopilotTournament_Compare4Possibilities`: **PASS (`0.04s`)** (`A2_rag_synth`: 100 % autonomía rutinaria 22/22, 100 % exactitud, 100 % escalamiento 8/8, 0 violaciones CP5/PII, 0 tokens, `54 µs` prom.; `A4_adaptive`: 100 % autonomía, `73 µs` offline / `105 µs` con LLM).
  - `go test -v -count=1 ./cmd/buyacar -run TestRunCLI_Phase4_MetaWebhookAndKillSwitch`: **PASS (`0.16s`)**.
  - `go test -count=1 ./...` (18 paquetes): **100 % PASS**.
- **Riesgos**: `go test -race` requiere CGO/`gcc` en Windows (registrado en `STATUS.md`).
- **Decisiones**: Adoptar `A2_rag_synth` como estrategia por defecto cuando `GEMINI_API_KEY` no está definida (`0` tokens, `54 µs`) y conmutar automáticamente a `A4_adaptive` cuando `GEMINI_API_KEY` está presente.
- **Siguiente acción**: Conectar credenciales oficiales de Meta (`META_PAGE_ID`, `META_ACCESS_TOKEN`, `META_APP_SECRET`) cuando el usuario decida pasar el canal de Página a piloto `DRAFT` / `AUTOPILOT`.
