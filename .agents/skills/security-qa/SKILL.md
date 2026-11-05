---
name: security-qa
description: "Audita seguridad, privacidad, prompt injection, SSRF, secretos, idempotencia y guardas de BuyaCAR. Usar antes de cada handoff o release."
---
# Skill: security-qa

Revisa: secretos y PII, allowlist HTTPS, bind local, límites de cuerpo/tiempo, kill switch, logs redactados, cifras inventadas, URLs no autorizadas, duplicados, entradas maliciosas y fallbacks de IA.

Clasifica cada hallazgo como `blocker`, `high`, `medium` o `low`, con evidencia, impacto, corrección y prueba de regresión. Un `blocker` impide el handoff.
