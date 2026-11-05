# Handoff Report — Adversarial Review & Integrity Verification (Reviewer 2)

**Author**: Reviewer 2 (`reviewer_critic`)  
**Working Directory**: `c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\reviewer_2\`  
**Target Codebase**: `app_build/` (BuyaCAR Omnicanal Control Plane Evolution R1–R5)  
**Parent Agent**: Orchestrator (Conversation ID: `84a866f8-986b-4c92-a496-9363b3a17b7f`)  
**Verdict**: **`REQUEST_CHANGES`**  

---

## 1. Observation

### Obs 1: Default Operating Mode Inversion in `main.go`
- **File**: `app_build/cmd/buyacar/main.go`
- **Lines**: 56–60
- **Verbatim Code**:
```go
	// 1. Inicializar configuración operativa (DRAFT por defecto, conmutable por variable de entorno)
	cfg := core.DefaultDraftConfig()
	if envMode := strings.ToUpper(strings.TrimSpace(os.Getenv("BUYACAR_OPERATING_MODE"))); envMode == string(channel.ModeSimulation) {
		cfg.OperatingMode = channel.ModeSimulation
	}
```
The entrypoint initializes with `core.DefaultDraftConfig()` (`OperatingMode = channel.ModeDraft`). It only operates in `SIMULATION` if the user explicitly defines the environment variable `BUYACAR_OPERATING_MODE=SIMULATION`. This contradicts `core.DefaultSafeConfig()` (`OperatingMode = channel.ModeSimulation`), `00-safety.md`, and `ORIGINAL_REQUEST.md` R1.

### Obs 2: Self-Certifying Auto-Approval Loop in `main.go` (Remnant of Tautological Gate)
- **File**: `app_build/cmd/buyacar/main.go`
- **Lines**: 256–264
- **Verbatim Code**:
```go
	for _, d := range emailPilot.RecordedDrafts() {
		_ = emailPilot.ReviewDraft(d.DraftID, "asesor-humano-tunja", false)
	}

	noAI, noHuman := obsCollector.Ratios()
	_, _, p99 := obsCollector.FastPathPercentiles()
	totalDrafts, _, _, uneditedPct := emailPilot.DraftGateMetrics()
	exitGate := obsCollector.EvaluateDraftExitGate(uneditedPct)
```
In default batch execution mode, `main.go` executes an automated `for` loop over `emailPilot.RecordedDrafts()`, approving all drafts programmatically under the synthetic identity `"asesor-humano-tunja"`. It then evaluates `obsCollector.EvaluateDraftExitGate(uneditedPct)` (which yields `uneditedPct = 100.0%`) and logs `"ready_for_autopilot_gate", exitGate.ReadyForAutopilot` as `true` (lines 277–278). While Squad 3 successfully removed this pattern from `draft_batch_exerciser_test.go`, the self-approving loop remains embedded in the core binary CLI.

### Obs 3: Cosmetic / Seed Metrics in `NewMetricsExporter`
- **File**: `app_build/internal/server/metrics_exporter.go`
- **Lines**: 83–86
- **Verbatim Code**:
```go
	// Valores iniciales por defecto representativos
	me.SetFastPathLatency(2.10, 2.59)
	me.cacheHits.Store(42)
	me.cacheMisses.Store(8)
```
Upon construction, `NewMetricsExporter` seeds arbitrary non-zero values (42 cache hits, 8 misses, 2.10/2.59 µs latency) before any requests or evaluations have occurred.

### Obs 4: Build & Test Toolchain Execution
- Command `cd app_build; go vet ./...`
  - Output: Exit code 0, 0 warnings/errors.
- Command `cd app_build; go test -v -count=1 ./...`
  - Output: Exit code 0 across 13 packages (`cmd/buyacar`, `internal/ai`, `internal/channel`, `internal/core`, `internal/crm`, `internal/fast`, `internal/guard`, `internal/inventory`, `internal/obs`, `internal/server`, `internal/skills`, `internal/store`, `test/e2e`). Total execution time: ~4.02s.
- Command `cd app_build; go test -race ./...`
  - Output: `go: -race requires cgo; enable cgo by setting CGO_ENABLED=1`. (Standard behavior on Windows host lacking gcc/clang; verifies 100% pure standard library Go without CGO dependencies).

### Obs 5: Verified Technical Strengths Across R1–R5
- **Go 1.23 Stdlib & Zero External Dependencies**: `app_build/go.mod` contains exclusively `module buyacar.com.co/omnicanal` and `go 1.23` with 0 external packages.
- **Pure Go ACID Persistence & Recovery**: `internal/store/wal_store.go` implements Write-Ahead Logging with binary framing, IEEE CRC32 checksums, replay recovery, and `file.Sync()` (`fsync`).
- **PII Ley 1581 & Anti-Hallucination**: `internal/guard/guard.go` redacts PII and enforces strict filtering for 5+ digit numbers against active inventory and authorized dealership lines (`3151212223`, `3022866969`, `3160502717`). Allowlist is strictly locked to HTTPS `https://buyacar.com.co`.
- **Selector Failover & Rate Limiting**: `internal/channel/resilience.go` pauses channels on 3 consecutive failures, suppresses alert storms, and isolates channel faults. `rate_controller.go` enforces 120/h and 250/d sliding windows with SHA-256 deterministic jitter.
- **Cascade Engine & Token Economy**: `internal/ai/cascade.go` evaluates L1 (Ollama loopback 127.0.0.1) -> L2 (Groq) -> L3 (Deterministic Fast-Path) with single-flight deduplication and semantic caching.
- **Embedded Web Console & Metrics**: `internal/server/server.go` serves web supervisor dashboard natively via `net/http` and `embed.FS`, exposing Prometheus `/metrics` and 1-click draft approval.

---

## 2. Logic Chain

1. **Step 1 (Mandate Alignment)**:
   - Rule `00-safety.md`, `AGENTS.md`, and `ORIGINAL_REQUEST.md` R1 mandate: "Arranca y conserva operatingMode=SIMULATION", and "Mantén SIMULATION por defecto hasta que una orden humana explícita autorice DRAFT".
   - `Obs 1` shows that `cmd/buyacar/main.go:57` sets `cfg := core.DefaultDraftConfig()`.
   - Running `./buyacar` without environment variables boots in `DRAFT` rather than `SIMULATION`. The safety polarity is inverted: it requires an environment variable to enforce `SIMULATION`, rather than requiring an explicit flag/env to enter `DRAFT`.

2. **Step 2 (Integrity Violation Assessment)**:
   - The System Instruction strictly dictates:
     > "When reviewing work, actively check for integrity violations:
     > - Evidence of self-certifying work without genuine independent verification
     > - Shortcuts that bypass the intended task...
     > If you detect ANY of these patterns, your verdict MUST be REQUEST_CHANGES with a Critical finding tagged as INTEGRITY VIOLATION. Do NOT approve work that cheats, regardless of test scores."
   - `Obs 2` shows lines 256–258 in `cmd/buyacar/main.go`:
     ```go
     for _, d := range emailPilot.RecordedDrafts() {
         _ = emailPilot.ReviewDraft(d.DraftID, "asesor-humano-tunja", false)
     }
     ```
   - In default batch mode, the binary automatically simulates an assessor named `"asesor-humano-tunja"`, approving 100% of generated drafts, thereby artificially inflating `uneditedPct` to 100% and logging `ready_for_autopilot_gate: true`.
   - While `draft_batch_exerciser_test.go` removed this loop, leaving it in `main.go` creates self-certifying behavior that simulates human authorization.

3. **Step 3 (Cosmetic Observability Assessment)**:
   - The Follow-up instruction ("ANTI-VIBECODING") ordered the eradication of cosmetic simulation and vanity metrics.
   - `Obs 3` shows that `NewMetricsExporter` hardcodes 42 cache hits, 8 misses, and 2.10/2.59 µs latency as constructor seed data before any real metrics are recorded.

---

## 3. Caveats

- Tests in `test/e2e/` and `internal/core/` are robust, genuine, and do NOT fail; all 58 E2E scenarios and unit test suites pass with 100% clean assertions.
- The `draft_batch_exerciser_test.go` unit test was properly refactored to decouple the technical batch exerciser from human authorization and correctly asserts `BLOCKED_PENDING_HUMAN_REVIEW`.
- The issues identified in `cmd/buyacar/main.go` do not cause test crashes because tests in `cmd/buyacar/main_test.go` test `--metrics`, `--inspect`, `--server`, and default batch without asserting whether `exitGate.ReadyForAutopilot` was genuinely authorized by a real human.

---

## 4. Conclusion

**Verdict**: **`REQUEST_CHANGES`**

### Critical Findings (Must Fix):
1. **[Critical - INTEGRITY VIOLATION] Self-Certifying Auto-Approval Loop in CLI Batch Mode**:
   - **Location**: `app_build/cmd/buyacar/main.go:256–264`
   - **Issue**: In default batch mode, a `for` loop auto-approves all recorded drafts using the synthetic identity `"asesor-humano-tunja"`, fabricating a 100% approval rate and emitting `ready_for_autopilot_gate: true`.
   - **Required Action**: Remove the automated `emailPilot.ReviewDraft` loop from `main.go`. In batch mode, generated drafts must remain pending in `RecordedDrafts()`, and `EvaluateDraftExitGate(uneditedPct)` must reflect `uneditedPct: 0.0%` with `ReadyForAutopilot: false` (`BLOCKED_PENDING_HUMAN_REVIEW`) until an explicit human operator approves drafts via the web dashboard or CLI command.

2. **[Critical - Safety Guardrail] Inverted Operating Mode Polarity in `main.go`**:
   - **Location**: `app_build/cmd/buyacar/main.go:56–60`
   - **Issue**: Defaults to `core.DefaultDraftConfig()` (`ModeDraft`), requiring `BUYACAR_OPERATING_MODE=SIMULATION` to activate simulation.
   - **Required Action**: Default to `core.DefaultSafeConfig()` (`ModeSimulation`). Require an explicit environment variable (e.g., `BUYACAR_OPERATING_MODE=DRAFT`) or flag to transition to `DRAFT`.

### Minor Findings (Should Fix):
3. **[Minor - Telemetry Cleanliness] Pre-seeded Vanity Metrics in Exporter**:
   - **Location**: `app_build/internal/server/metrics_exporter.go:83–86`
   - **Issue**: Pre-seeds 42 hits, 8 misses, and 2.10/2.59 µs latency upon construction.
   - **Required Action**: Initialize all metric counters to zero (`0`) for true cold-start accuracy.

4. **[Minor - Resource Hygiene] `time.After` in Webhook Exporter Retry Loop**:
   - **Location**: `app_build/internal/crm/webhook_exporter.go:165`
   - **Issue**: `time.After` in a loop retains timers until expiration.
   - **Required Action**: Use `time.NewTimer` with `defer timer.Stop()`.

---

## 5. Verification Method

To verify whether the requested changes resolve these findings:

1. **Verify Default SIMULATION Mode**:
   ```powershell
   cd app_build
   # Run without BUYACAR_OPERATING_MODE set
   go run ./cmd/buyacar --inspect
   # In JSON output, verify: "operating_mode": "SIMULATION" and "active_mode": "SIMULATION"
   ```

2. **Verify Autopilot Gate Blocked in Batch Execution**:
   ```powershell
   cd app_build
   go run ./cmd/buyacar
   # In stderr log output, verify:
   # "email_pilot_unedited_approval_pct": 0
   # "ready_for_autopilot_gate": false
   ```

3. **Verify All Tests and Vet**:
   ```powershell
   cd app_build
   go vet ./...
   go test -v -count=1 ./...
   ```
