# Handoff Report — Worker M1.2: Remediation of VULN-M1-01 (Checkpoint Clobbering)

**Role**: teamwork_preview_worker (Worker M1.2)  
**Target Milestone**: Milestone 1 (M1 - Core Pipeline Checkpoint Clobbering Remediation)  
**Date/Time**: 2026-10-29T03:28:00Z  

---

## 1. Observation

1. **Defect Location**:
   In `app_build/internal/core/pipeline.go` lines 78–91 (prior to fix):
   ```go
   78: 	th, err := ch.Read(ctx, sess, conv)
   79: 	if err != nil {
   80: 		return outcome, err
   81: 	}
   82: 	_ = p.Engine.store.SaveCheckpointState(conv.ID, "cp1_lectura:ok")
   83: 	outcome.CheckpointsOK++
   84: 
   85: 	// Paso 2: Evaluar reglas de omision (propio, hash duplicado, > 9 semanas, kill switch)
   86: 	hash, err := p.Engine.ShouldProcessThread(ctx, th, now)
   87: 	if err != nil {
   88: 		outcome.Skipped = true
   89: 		outcome.SkipReason = err.Error()
   90: 		return outcome, nil
   91: 	}
   ```
   `SaveCheckpointState(conv.ID, "cp1_lectura:ok")` at line 82 executed immediately after reading the channel thread, prior to calling `ShouldProcessThread` (line 86).

2. **Observed Vulnerability (`VULN-M1-01`)**:
   When replaying a duplicate message or processing an already completed thread (`cp7_crm:ok`), line 82 unconditionally clobbered the stored checkpoint state back to `"cp1_lectura:ok"` before line 86 returned `ErrAlreadyProcessed` and skipped the message.
   Empirical test observation prior to fix:
   ```
   adversarial_challenge_test.go:464: Observed checkpoint state after concurrent submission: ok=true, cp="cp1_lectura:ok"
   adversarial_challenge_test.go:466: FINDING [VULNERABILITY]: Checkpoint clobbered to "cp1_lectura:ok" by trailing skipped duplicate goroutine at pipeline.go:82
   adversarial_challenge_test.go:513: EMPIRICAL EVIDENCE — Checkpoint before duplicate: "cp7_crm:ok", Checkpoint after duplicate: "cp1_lectura:ok" (ok=true)
   adversarial_challenge_test.go:516: CONFIRMED BUG: CP1 checkpoint (pipeline.go:82) unconditionally clobbers completed conversation checkpoint on duplicate replay
   ```

3. **Remediation Applied**:
   In `app_build/internal/core/pipeline.go`, relocated `SaveCheckpointState(conv.ID, "cp1_lectura:ok")` and `outcome.CheckpointsOK++` to strictly follow the successful return of `p.Engine.ShouldProcessThread(ctx, th, now)`:
   ```go
   	// CP1: Lectura
   	th, err := ch.Read(ctx, sess, conv)
   	if err != nil {
   		return outcome, err
   	}

   	// Paso 2: Evaluar reglas de omision (propio, hash duplicado, > 9 semanas, kill switch)
   	hash, err := p.Engine.ShouldProcessThread(ctx, th, now)
   	if err != nil {
   		outcome.Skipped = true
   		outcome.SkipReason = err.Error()
   		return outcome, nil
   	}
   	_ = p.Engine.store.SaveCheckpointState(conv.ID, "cp1_lectura:ok")
   	outcome.CheckpointsOK++
   ```

4. **Verification Command & Output**:
   Command: `cd app_build; go test -v -count=1 ./internal/core -run "TestAdversarial_CheckpointClobberOnDuplicateReplay|TestAdversarial_DuplicateReplay_ConcurrentSubmission"`
   Output:
   ```
   === RUN   TestAdversarial_DuplicateReplay_ConcurrentSubmission
       adversarial_challenge_test.go:453: Concurrent duplicate submission (20 goroutines): Processed=2, Skipped=18, Errors=0
       adversarial_challenge_test.go:464: Observed checkpoint state after concurrent submission: ok=true, cp="cp7_crm:ok"
   --- PASS: TestAdversarial_DuplicateReplay_ConcurrentSubmission (0.00s)
   === RUN   TestAdversarial_CheckpointClobberOnDuplicateReplay
       adversarial_challenge_test.go:513: EMPIRICAL EVIDENCE — Checkpoint before duplicate: "cp7_crm:ok", Checkpoint after duplicate: "cp7_crm:ok" (ok=true)
   --- PASS: TestAdversarial_CheckpointClobberOnDuplicateReplay (0.00s)
   PASS
   ok  	buyacar.com.co/omnicanal/internal/core	2.202s
   ```
   Notice that the vulnerability log message (`FINDING [VULNERABILITY]: Checkpoint clobbered`) is completely eliminated, and `cp` remains `"cp7_crm:ok"`.

   Full Test Suite Run:
   Command: `cd app_build; go test -count=1 ./...`
   Output:
   ```
   ok  	buyacar.com.co/omnicanal/internal/ai	2.197s
   ok  	buyacar.com.co/omnicanal/internal/channel	1.040s
   ok  	buyacar.com.co/omnicanal/internal/core	2.326s
   ok  	buyacar.com.co/omnicanal/internal/crm	1.851s
   ok  	buyacar.com.co/omnicanal/internal/fast	2.345s
   ok  	buyacar.com.co/omnicanal/internal/guard	2.308s
   ok  	buyacar.com.co/omnicanal/internal/inventory	3.564s
   ok  	buyacar.com.co/omnicanal/internal/obs	1.415s
   ok  	buyacar.com.co/omnicanal/internal/store	1.051s
   ok  	buyacar.com.co/omnicanal/test/e2e	1.684s
   ```
   100% of tests pass across all packages with zero regressions.

---

## 2. Logic Chain

1. In the BuyaCAR omnicanal architecture, checkpoint states (`cp1_lectura:ok` through `cp7_crm:ok`) record the progress of actively processed message workflows.
2. Inbound messages that trigger omission rules in `ShouldProcessThread` (duplicate messages, messages sent by the bot itself, or threads inactive for > 9 weeks) are aborted without advancing the conversation pipeline.
3. Placing `SaveCheckpointState(conv.ID, "cp1_lectura:ok")` prior to `ShouldProcessThread` caused every skipped inbound event to erroneously record an active CP1 state, overwriting any terminal checkpoint (`cp7_crm:ok`) previously established for that conversation.
4. Moving `SaveCheckpointState(conv.ID, "cp1_lectura:ok")` and `outcome.CheckpointsOK++` to line 90 (immediately following `ShouldProcessThread`'s successful return) guarantees that:
   - Skipped messages immediately exit with `outcome.Skipped = true`, without modifying the store's checkpoint state.
   - Genuine, unprocessed messages that pass all omission rules advance to CP1 and persist `cp1_lectura:ok`.
   - Completed conversations retain `cp7_crm:ok` even under high-concurrency duplicate bursts.

---

## 3. Caveats

- No caveats. The fix strictly alters ordering within `ProcessConversation` in `pipeline.go` without introducing new dependencies or changing public API signatures.

---

## 4. Conclusion

Defect `VULN-M1-01` is completely and cleanly remediated. Duplicate and skipped messages no longer clobber completed checkpoint states. All tests in `app_build/` pass 100% with clean `go vet` and `gofmt` compliance.

---

## 5. Verification Method

To independently verify this remediation:
1. Inspect lines 77–93 of `app_build/internal/core/pipeline.go` to confirm `SaveCheckpointState(conv.ID, "cp1_lectura:ok")` is located strictly after `ShouldProcessThread`.
2. Run targeted adversarial tests:
   ```powershell
   cd app_build
   go test -v -count=1 ./internal/core -run "TestAdversarial_CheckpointClobberOnDuplicateReplay|TestAdversarial_DuplicateReplay_ConcurrentSubmission"
   ```
   Verify that `TestAdversarial_CheckpointClobberOnDuplicateReplay` logs `Checkpoint before duplicate: "cp7_crm:ok", Checkpoint after duplicate: "cp7_crm:ok"` and does NOT log `CONFIRMED BUG`.
3. Run the full test suite:
   ```powershell
   cd app_build
   go test -count=1 ./...
   ```
   Verify that all packages pass without errors.
