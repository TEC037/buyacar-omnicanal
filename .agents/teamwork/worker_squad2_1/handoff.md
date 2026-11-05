# Handoff Report — Milestone 2 (Squad 2: Persistencia, DMS & CRM)

## 1. Observation
- Target packages exclusively modified:
  - `app_build/internal/store/`
  - `app_build/internal/crm/`
  - `app_build/internal/inventory/`
- Implemented Features:
  - Feature 11: Pure Go ACID transactional persistence engine (`app_build/internal/store/wal_store.go`). Binary WAL format with opcode, length prefix, CRC32 checksum, and `file.Sync()` (fsync) per operation for full ACID durability.
  - Feature 12: Automatic fallback to JSON flat-file append log (`app_build/internal/store/json_store.go`) and resilient manager (`app_build/internal/store/resilient_store.go`). Detects primary disk errors, immediately routes in-flight mutations to fallback without blocking requests, alerts `escalate.Queue` (`store_disk_failover`), and supports recovery with `RecoverPrimary(ctx)`.
  - Feature 13: Periodic DMS inventory synchronization worker (`app_build/internal/inventory/dms_sync.go`). Background ticker running < 30m (default 15m), conditional HTTP GET with `If-None-Match`, and handling of `304 Not Modified` via `cache.TouchRefresh()`.
  - Feature 14: Real-time inventory invalidation webhook (`app_build/internal/inventory/invalidation_webhook.go`). HTTP POST endpoint enforcing `MAX_BODY_BYTES = 256 KB`, validating HMAC-SHA256 (`X-BuyaCAR-Signature-256`), updating vehicle price/status in cache, recalculating ETag, and triggering waitlist when returned to `available`.
  - Feature 15: Waitlist SHA-256 next-entry alert engine (`app_build/internal/inventory/waitlist.go`). Customer reservation management with SHA-256 seal (`hex(sha256(vID|cID|channel|timestamp))`). When vehicle returns to `available`, notifies pending entries and generates commercial alerts via decoupled `TaskRecorder`.
  - Feature 16 & 17: Outbound CRM webhook dispatcher with HMAC-SHA256 and exponential backoff (`app_build/internal/crm/webhook_exporter.go`). Dispatches task exports signed with HMAC-SHA256 in `X-BuyaCAR-Signature-256`, handles retries with exponential backoff (`delay = BaseDelay * (1 << attempt)`), and enqueues alert to `escalate.Queue` on retry exhaustion (`crm_webhook_export_exhausted`).
- Test execution output:
  ```text
  cd app_build; go test -v -count=1 ./internal/store/... ./internal/crm/... ./internal/inventory/...
  ok   buyacar.com.co/omnicanal/internal/store       1.800s (9/9 passed)
  ok   buyacar.com.co/omnicanal/internal/crm         3.218s (12/12 passed)
  ok   buyacar.com.co/omnicanal/internal/inventory   3.308s (12/12 passed)
  ```
  Total: 33 tests passed across Squad 2 packages, 0 failures.
  `go vet ./internal/store/... ./internal/crm/... ./internal/inventory/...` exited with code 0 (clean).

## 2. Logic Chain
1. *Observation*: System required pure Go transactional persistence with WAL semantics, ACID durability, and automatic failover upon disk failure to memory + JSON flat append log.
   *Reasoning*: `wal_store.go` was constructed with binary WAL entries protected by CRC32 checksums and atomic fsync flushes, guaranteeing state recovery across restarts without CGO/SQLite external dependencies. `resilient_store.go` encapsulates primary and fallback stores; upon any disk error, it triggers fallback mode and enqueues an escalation ticket to `escalate.Queue` while maintaining continuous service.
2. *Observation*: A potential circular dependency was identified between `internal/inventory` and `internal/crm` (`inventory -> crm -> triage -> inventory`).
   *Reasoning*: `waitlist.go` was decoupled from `crm.Manager` by defining a minimal `TaskRecorder` interface in `internal/inventory`. `crm.MemoryManager` implements `RecordWaitlistAlert` structurally, allowing seamless composition by downstream orchestrators while keeping package dependencies strictly acyclic.
3. *Observation*: DMS synchronization required ETag handling and loop interval < 30m; Webhook required 256 KB max payload and HMAC verification.
   *Reasoning*: `dms_sync.go` clamps intervals to < 30m and executes conditional GET requests, tracking 304 responses in metrics. `invalidation_webhook.go` enforces `io.LimitReader(r.Body, 256*1024)`, verifies HMAC-SHA256 against configured secrets, updates `Cache`, and triggers waitlist alerts.
4. *Observation*: Outbound CRM export required HMAC signatures and exponential backoff falling back to `escalate.Queue`.
   *Reasoning*: `webhook_exporter.go` implements HTTP POST signing via `ComputeSignature()`, retry loops with exponential backoff, and enqueues tickets to `escalate.Queue` when retries are exhausted.

## 3. Caveats
- The local Windows workstation environment does not have a C compiler (`gcc`), preventing execution with `go test -race` locally. However, all structs and mutable state in `store`, `crm`, and `inventory` are guarded by `sync.RWMutex` / `sync.Mutex` and have been stressed with concurrent test suites.
- Outbound CRM webhook dispatcher uses a configurable `HTTPClient` (defaults to 10s timeout, max 30s) to allow offline testing with `httptest.Server` without hitting real external networks.

## 4. Conclusion
Milestone 2 (Squad 2: Persistencia, DMS & CRM) is completely implemented, verified, and adheres strictly to the 7 Mandamientos Anti-Regresión, Go standard library conventions, zero external dependencies, and exclusive directory write boundaries. All 33 unit tests pass cleanly with zero regressions.

## 5. Verification Method
Execute the following verification command from workspace root:
```powershell
cd app_build
go test -v -count=1 ./internal/store/... ./internal/crm/... ./internal/inventory/...
go vet ./internal/store/... ./internal/crm/... ./internal/inventory/...
```
Invalidation conditions:
- Any test failure in `internal/store`, `internal/crm`, or `internal/inventory`.
- Any cyclic import error during compilation.
- Any modification to files outside `app_build/internal/store/`, `app_build/internal/crm/`, or `app_build/internal/inventory/`.
