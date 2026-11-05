# Task Assignment — Worker M1.2: Fix Checkpoint Clobbering (VULN-M1-01)

**Role**: teamwork_preview_worker
**Working Directory**: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\worker_m1_2
**Authoritative Request**: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\ORIGINAL_REQUEST.md
**Project Plan**: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\PROJECT.md

## Exclusive File Ownership
You exclusively own:
- `app_build/internal/core/pipeline.go`

## Defect Report from Challengers M1.1 & M1.2 (`VULN-M1-01`)
In `app_build/internal/core/pipeline.go` around line 82:
```go
	_ = p.Engine.store.SaveCheckpointState(conv.ID, "cp1_lectura:ok")
	outcome.CheckpointsOK++

	// Paso 2: Evaluar reglas de omision (propio, hash duplicado, > 9 semanas, kill switch)
	hash, err := p.Engine.ShouldProcessThread(ctx, th, now)
	if err != nil {
		outcome.Skipped = true
		outcome.SkipReason = err.Error()
		return outcome, nil
	}
```
`SaveCheckpointState(conv.ID, "cp1_lectura:ok")` is executed BEFORE evaluating `ShouldProcessThread`. When a duplicate message arrives for a conversation that already finished CP7 (`cp7_crm:ok`), it overwrites the checkpoint back to `cp1_lectura:ok` before returning.
This causes `TestAdversarial_DuplicateReplay_ConcurrentSubmission` to fail:
`adversarial_challenge_test.go:465: expected checkpoint cp7_crm:ok, got cp1_lectura:ok`.

## Objective
1. In `app_build/internal/core/pipeline.go`, move `SaveCheckpointState(conv.ID, "cp1_lectura:ok")` and `outcome.CheckpointsOK++` to AFTER `ShouldProcessThread` validation succeeds. If `ShouldProcessThread` returns an error/skip reason, do NOT overwrite `SaveCheckpointState`.
2. Run `go test -v ./internal/core` and confirm `TestAdversarial_DuplicateReplay_ConcurrentSubmission` passes.
3. Run `go test -v ./...` in `app_build/` and confirm 100% of all tests pass.
4. Deliver `handoff.md` and notify orchestrator via `send_message`.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.


## 2026-10-29T03:23:08Z
You are Worker M1.2 for BuyaCAR Omnicanal.
Your working directory is: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\worker_m1_2
Read your DISPATCH.md and c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\ORIGINAL_REQUEST.md.
Fix defect VULN-M1-01 in app_build/internal/core/pipeline.go: move SaveCheckpointState("cp1_lectura:ok") after ShouldProcessThread passes so that duplicate messages do not overwrite completed checkpoint states.
Run `go test -v ./internal/core` and `go test -v ./...` in app_build/.
Ensure 100% test pass.
Deliver handoff.md in your working directory and notify caller via send_message.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.
