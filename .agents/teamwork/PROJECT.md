# Project: BuyaCAR Omnicanal
## Estado de Casos de Uso y Gobernanza Arquitectónica

### Filosofía: Antoine de Saint-Exupéry
> "Il semble que la perfection soit atteinte non quand il n'y a plus rien à ajouter, mais quand il n'y a plus rien à retrancher."

## Architecture
- **Lenguaje**: Go 1.23 estándar, sin CGO, sin dependencias externas, sin n8n.
- **Entorno comercial**: Concesionario de vehículos usados certificados en Tunja, Boyacá (Av. Oriental - Cra. 6 #52-23, frente a Toyota).
- **Componentes Consolidados**:
  1. `channel`: Meta (Facebook Marketplace, Messenger, Instagram) y WhatsApp Cloud API oficial.
  2. `triage`: Clasificación determinista en < 50 µs, captura de teléfono celular, detección de permutas/regateo y escalamiento.
  3. `fast`: Fast-Path determinista en 0 tokens y ~2.5 µs (Ubicación, Horarios, Teléfonos, Catálogo, Garantía y Crédito).
  4. `inventory`: Catálogo de usados en memoria con ETag y congelación de stock en reserva.
  5. `guard`: CP5 anti-alucinación numérica, PII anonimizada (Ley 1581) y Kill Switch.
  6. `crm`: Embudo de 6 etapas, deduplicación por teléfono y panel web 1-clic para Tunja.

## Feature Inventory & Milestones
El inventario completo y detallado caso por caso está documentado en:
`.agents/teamwork/INVENTARIO_MAESTRO_CASOS_DE_USO_SAINT_EXUPERY.md`

| # | Milestone | Scope | Dependencies | Status |
|---|---|---|---|---|
| M1 | Auditoría de Casos de Uso | Inventariar DEBE vs TIENE y qué sobra bajo Saint-Exupéry | none | **DONE** |
| M2 | Poda Quirúrgica de Código Accidental | Retirar ~3.450 líneas superfluas (MercadoLibre, Playwright, email, WAL, etc.) | M1 | PLANNED |
| M3 | Blindaje de CP5 y Ley 1581 | Normalizar puntuación en precios y cédulas antes de regex | M1 | PLANNED |
| M4 | Elevación Fast-Path 0 Tokens | Promover Garantía y Crédito a fast.go (>85% resolución sin IA) | M1 | PLANNED |
| M5 | Refinamiento Comercial CRM | Congelar inventario al reservar ($2M) y deduplicar por teléfono | M1 | PLANNED |

## Code Layout (Post-Poda Canónica)
```
app_build/
├── cmd/buyacar/main.go
├── fixtures/rag/
├── internal/
│   ├── channel/ (meta_omnichannel.go, whatsapp_cloud.go, resilience.go, rate_controller.go)
│   ├── core/ (types, session, pipeline)
│   ├── crm/ (lead_scoring.go, crm.go)
│   ├── fast/ (fast.go)
│   ├── guard/ (guard.go)
│   ├── inventory/ (inventory.go, cache.go)
│   ├── obs/ (metrics.go, traces.go)
│   ├── rag/ (bm25.go, engine.go)
│   ├── server/ (dashboard_handler.go, server.go)
│   └── triage/ (triage.go)
```
