# BRIEFING — 2026-10-29T13:52:00Z

## Mission
Milestone 4: Governance, CP5 Anti-Hallucination Backed Numbers, Golden Tests & E2E Validation.

## 🔒 My Identity
- Archetype: worker
- Roles: implementer, qa, specialist
- Working directory: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\worker_rag_m4_1
- Original parent: 741493ac-97f5-4d25-8e1a-fdb6647a5e86
- Milestone: M4

## 🔒 Key Constraints
- Strict backward compatibility for `ValidateOutbox`.
- Allow 5+ digit numbers backed by RAG fixtures and inventory; block unbacked numbers.
- HTTPS allowlist strictly to `https://buyacar.com.co`.
- PII redaction (Ley 1581 de 2012).
- Zero external dependencies beyond Go standard library.
- OperatingMode SIMULATION by default.
- Exclusive write ownership: `app_build/internal/guard/`, `app_build/internal/core/pipeline.go`, and `app_build/test/rag_golden_test.go`.

## Current Parent
- Conversation ID: 741493ac-97f5-4d25-8e1a-fdb6647a5e86
- Updated: 2026-10-29T13:52:00Z

## Task Summary
- **What to build**:
  1. `internal/guard/guard.go`: ValidateOutboxWithBackedNumbers with backward compatibility for ValidateOutbox.
  2. `internal/core/pipeline.go`: CP5 integration passing backed numbers from RAGEngine.GetBackedNumbers().
  3. `test/rag_golden_test.go`: Programmatic golden test suite for catalog, warranty, credit, FAQ, anti-hallucination, PII, concurrency.
- **Success criteria**: 100% tests passing in `./test/...`, `./internal/guard/...`, and `./...`.
- **Interface contracts**: PROJECT.md
- **Code layout**: PROJECT.md § Code Layout

## Key Decisions Made
- [TBD]

## Artifact Index
- DISPATCH.md — Assignment prompt
- go-quality-SKILL.md — Local dump of go-quality skill
- progress.md — Liveness heartbeat and step tracking
- handoff.md — Final handoff report

## Change Tracker
- **Files modified**: None yet
- **Build status**: pending initial run
- **Pending issues**: none

## Quality Status
- **Build/test result**: pending initial test
- **Lint status**: 0 violations
- **Tests added/modified**: none yet

## Loaded Skills
- **Source**: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\skills\go-quality\SKILL.md
- **Local copy**: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\worker_rag_m4_1\go-quality-SKILL.md
- **Core methodology**: Pure Go, modular architecture, interfaces/adapters, race detector, fuzzing, benchmarks, zero fake claims.
