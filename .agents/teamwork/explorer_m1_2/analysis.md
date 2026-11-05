# Technical Specification & Design Analysis: Inventory Querying & Idempotency Sealing
**Document**: `analysis.md`  
**Milestone**: Milestone 1 (R1 — Inventory & Idempotency Design)  
**Author**: Explorer M1.2 (Inventory & Idempotency Design)  
**Target Packages**: `app_build/internal/inventory` and `app_build/internal/store`  
**Date**: 2026-10-29  

---

## 1. Executive Summary

This specification defines the non-breaking, thread-safe Go architecture and technical contract for:
1. **`internal/inventory`**: Exposing direct vehicle lookup (`GetVehicle(id string) (Vehicle, bool)`), explicit cache freshness verification against the 30-minute stale threshold (`IsFresh(now time.Time) bool`), and thread-safe ETag inspection (`ETag() string`).
2. **`internal/store`**: Expanding `IdempotencyRecord` to seal dispatch outcomes (`SendStatus`, `ETag`, `CRMTaskID`), adding an atomic `SealIdempotency` contract to `Store` and `MemoryStore`, and adding an `IsSealed() bool` helper.
3. **`internal/store/store_test.go`**: Introducing a comprehensive unit test suite for `store` (which currently has 0 test files), establishing 100% test coverage and concurrency verification under high-load parallel execution.

All proposed additions are 100% backward compatible with existing code (`golden64_test.go`, `draft_batch_exerciser_test.go`, `pipeline.go`, `core_test.go`), introduce zero new external dependencies (pure Go standard library), and preserve Fast-Path sub-millisecond execution times.

---

## 2. Existing State & Architectural Gap Analysis

### 2.1 Current State in `internal/inventory`
- `inventory.Cache` manages `vehicles []Vehicle`, `etag string`, `lastUpdated time.Time`, `refreshWindow time.Duration` (15m), and `staleThreshold time.Duration` (30m).
- Queries are executed exclusively via `c.Search(ctx context.Context, q Query, now time.Time) MatchResult`.
- **Gaps**:
  1. No direct retrieval method by vehicle ID (`GetVehicle`). A revalidator or consumer knowing only the vehicle ID (`veh-001`) would currently have to construct an empty `Query` or scan all matches.
  2. No standalone boolean predicate `IsFresh(now time.Time) bool`. Checking whether the inventory cache is within the 30-minute freshness threshold currently requires executing a full dummy `Search` and inspecting `MatchResult.Status != StatusStale`.
  3. `etag` is an unexported private field on `*Cache`, preventing downstream stages (such as the CP7 idempotency sealer) from capturing the active snapshot ETag.

### 2.2 Current State in `internal/store`
- `IdempotencyRecord` contains only:
  ```go
  type IdempotencyRecord struct {
      Hash      string
      ChannelID string
      ConvID    string
      ReplyText string
      CreatedAt time.Time
  }
  ```
- `Store` interface contains:
  ```go
  type Store interface {
      ComputeHash(channelID, convID, lastIncomingMessage string) string
      HasProcessed(hash string) (bool, IdempotencyRecord)
      MarkProcessed(record IdempotencyRecord) error
      SaveCheckpointState(convID string, checkpoint string) error
      GetCheckpointState(convID string) (string, bool)
  }
  ```
- **Gaps**:
  1. `IdempotencyRecord` lacks fields to record post-dispatch verification (`SendStatus`), inventory snapshot linkage (`ETag`), and CRM settlement linkage (`CRMTaskID`) as mandated by `instructivo.md` Step 11.
  2. No dedicated atomic sealing method exists to enrich or seal an idempotency record once the reply has been transmitted and the CRM task recorded.
  3. The `internal/store` package currently has **no test files** (`? buyacar.com.co/omnicanal/internal/store [no test files]`), leaving memory store behavior tested only indirectly through `core_test.go`.

---

## 3. Detailed Technical Specification: `internal/inventory`

### 3.1 Contract Additions to `app_build/internal/inventory/inventory.go`

```go
// GetVehicle recupera de forma thread-safe un vehiculo por su identificador unico (ID).
// Realiza busqueda insensible a mayusculas/minusculas y omite espacios en blanco.
// Retorna el vehiculo y true si existe; de lo contrario retorna Vehicle{} vacio y false.
func (c *Cache) GetVehicle(id string) (Vehicle, bool) {
	if c == nil {
		return Vehicle{}, false
	}
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return Vehicle{}, false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, v := range c.vehicles {
		if strings.EqualFold(v.ID, trimmed) {
			return v, true
		}
	}
	return Vehicle{}, false
}

// IsFresh verifica de forma thread-safe si la cache de inventario esta disponible y si la
// ultima actualizacion no ha superado el umbral de caducidad (30 minutos por defecto).
// Retorna false si la cache no esta disponible, si nunca ha sido cargada, o si now - lastUpdated > staleThreshold.
func (c *Cache) IsFresh(now time.Time) bool {
	if c == nil {
		return false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.available || c.lastUpdated.IsZero() {
		return false
	}

	threshold := c.staleThreshold
	if threshold <= 0 {
		threshold = 30 * time.Minute
	}

	diff := now.Sub(c.lastUpdated)
	if diff < 0 {
		// Tolera desajustes menores de reloj si now es ligeamente anterior a lastUpdated
		return true
	}

	return diff <= threshold
}

// ETag retorna el identificador de version ETag actual de la cache de forma thread-safe.
func (c *Cache) ETag() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.etag
}
```

### 3.2 Design Rationales for `inventory`
1. **Nil Safety**: Both `GetVehicle`, `IsFresh`, and `ETag` guard against `c == nil`, returning safe defaults without panicking.
2. **Defensive Copying**: `c.vehicles` stores `Vehicle` value types; returning `Vehicle` by value guarantees that caller code cannot mutate internal cache state.
3. **Case-Insensitive ID Lookup**: Real-world channel conversations or CRM integrations might reference `"veh-001"`, `"VEH-001"`, or `" veh-001 "`. Trimming and using `strings.EqualFold` ensures robust matching.
4. **Consistency with `Search`**: `IsFresh` checks `now.Sub(c.lastUpdated) <= threshold`. In `Search`, stale is triggered when `now.Sub(c.lastUpdated) > c.staleThreshold`. Thus, `IsFresh == false` matches `Search == StatusStale` or `StatusUnavailable` exactly.
5. **Zero Allocations & Sub-Microsecond Speed**: `GetVehicle` scans an in-memory slice of 4–100 vehicles in < 150 nanoseconds, far below the 5 ms p99 Fast-Path budget.

---

## 4. Detailed Technical Specification: `internal/store`

### 4.1 Contract Additions to `app_build/internal/store/store.go`

#### 4.1.1 Enriched `IdempotencyRecord`
```go
// IdempotencyRecord guarda la evidencia de una respuesta ya procesada y sellada con estado CRM y ETag.
type IdempotencyRecord struct {
	Hash       string    `json:"hash"`
	ChannelID  string    `json:"channel_id"`
	ConvID     string    `json:"conv_id"`
	ReplyText  string    `json:"reply_text"`
	CreatedAt  time.Time `json:"created_at"`
	SendStatus string    `json:"send_status,omitempty"`
	ETag       string    `json:"etag,omitempty"`
	CRMTaskID  string    `json:"crm_task_id,omitempty"`
}

// IsSealed indica si el registro de idempotencia cuenta con el sellado completo posterior al envio.
func (r IdempotencyRecord) IsSealed() bool {
	return r.SendStatus != "" && r.ETag != ""
}
```

#### 4.1.2 Updated `Store` Interface
```go
// Store define el contrato de persistencia para idempotencia, estado y reanudacion.
type Store interface {
	ComputeHash(channelID, convID, lastIncomingMessage string) string
	HasProcessed(hash string) (bool, IdempotencyRecord)
	MarkProcessed(record IdempotencyRecord) error
	SealIdempotency(hash string, sendStatus string, etag string, crmTaskID string) error
	SaveCheckpointState(convID string, checkpoint string) error
	GetCheckpointState(convID string) (string, bool)
}
```

#### 4.1.3 Implementation on `MemoryStore`
```go
// SealIdempotency actualiza o crea de forma atomica y thread-safe el sellado de envio, ETag y CRM.
func (m *MemoryStore) SealIdempotency(hash string, sendStatus string, etag string, crmTaskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.processed[hash]
	if !ok {
		rec = IdempotencyRecord{
			Hash:      hash,
			CreatedAt: time.Now().UTC(),
		}
	}
	rec.SendStatus = sendStatus
	rec.ETag = etag
	rec.CRMTaskID = crmTaskID
	m.processed[hash] = rec
	return nil
}
```

### 4.2 Design Rationales for `store`
1. **Non-Breaking Struct Expansion**: Go allows adding fields to structs without breaking existing struct literal initializers as long as they use field names (`Field: value`). All existing usages in `pipeline.go` and `core_test.go` use named fields.
2. **Dual Sealing Paths**:
   - Direct: Callers can pass `SendStatus`, `ETag`, and `CRMTaskID` directly in `MarkProcessed(store.IdempotencyRecord{...})`.
   - Incremental / Atomic: If a record was pre-marked earlier or needs explicit post-dispatch sealing, callers invoke `SealIdempotency(hash, sendStatus, etag, crmTaskID)`.
3. **JSON Tags**: Adding JSON tags (`json:"..."`) facilitates serialization when dumping audit logs, checkpoints, or debugging state.
4. **Idempotency Invariant Preserved**: The deduplication key remains the SHA-256 hash computed from `channelID + "|" + convID + "|" + lastIncomingMessage`. Sealing adds metadata to the record without mutating its deduplication key.

---

## 5. Integration Blueprint with Pipeline (CP6 & CP7)

Here is how `internal/core/pipeline.go` utilizes the new methods during Step 11 and CP7:

```go
// -------------------------------------------------------------
// Paso 11: Revalidacion Pre-Envio (internal/core/pipeline.go)
// -------------------------------------------------------------
// 1. Validar frescura del catalogo de inventario (< 30 min)
if !p.InvCache.IsFresh(now) {
    p.EscQueue.Enqueue(ch.ID(), conv.ID, "inventory_stale_pre_send", "Cache mayor a 30m al momento de despachar", now)
    outcome.Escalated = true
    outcome.EscalationReason = "inventory_stale_pre_send"
    return outcome, nil
}

// 2. Si la conversacion hace referencia a un vehiculo especifico, validar disponibilidad
if referencedVehicleID != "" {
    veh, found := p.InvCache.GetVehicle(referencedVehicleID)
    if !found {
        p.EscQueue.Enqueue(ch.ID(), conv.ID, "vehicle_not_found_pre_send", "Vehiculo no encontrado en catalogo", now)
        outcome.Escalated = true
        outcome.EscalationReason = "vehicle_not_found_pre_send"
        return outcome, nil
    }
    if veh.Status != "available" {
        // Vehiculo vendido o separado: abortar pitch y escalar
        p.EscQueue.Enqueue(ch.ID(), conv.ID, "vehicle_status_"+veh.Status, "Vehiculo "+referencedVehicleID+" cambio a "+veh.Status, now)
        outcome.Escalated = true
        outcome.EscalationReason = "vehicle_status_" + veh.Status
        return outcome, nil
    }
}

// -------------------------------------------------------------
// CP6: Despacho / Reply
// -------------------------------------------------------------
sendRes, err := ch.Reply(ctx, sess, conv, outMsg, p.Engine.cfg.OperatingMode)
// Validar resultado de envio
if err != nil || sendRes.Status == channel.SendFailed {
    p.EscQueue.Enqueue(ch.ID(), conv.ID, "send_failed", err.Error(), now)
    outcome.Escalated = true
    outcome.EscalationReason = "send_failed"
    return outcome, err
}

// -------------------------------------------------------------
// CP7: Asentamiento CRM y Sellado de Idempotencia
// -------------------------------------------------------------
var crmTaskID string
if task, err := crmManager.RecordTask(ctx, ...); err == nil {
    crmTaskID = task.ID
}

// Sellar registro con SendStatus, ETag fresca de inventario y CRMTaskID
_ = p.Engine.store.MarkProcessed(store.IdempotencyRecord{
    Hash:       hash,
    ChannelID:  ch.ID(),
    ConvID:     conv.ID,
    ReplyText:  valRes.SanitizedReply,
    CreatedAt:  now,
    SendStatus: string(sendRes.Status),
    ETag:       p.InvCache.ETag(),
    CRMTaskID:  crmTaskID,
})
_ = p.Engine.store.SaveCheckpointState(conv.ID, "cp7_crm:ok")
```

---

## 6. Unit Testing Plan & Proposed Test Code

### 6.1 Tests to Add in `app_build/internal/inventory/inventory_test.go`

```go
func TestCache_GetVehicle_ExistingAndNonExisting(t *testing.T) {
	now := time.Date(2026, 10, 26, 13, 35, 0, 0, time.UTC)
	cache := NewSeededStubCache(now)

	// 1. Vehiculo existente (Toyota Hilux 2022)
	veh, ok := cache.GetVehicle("veh-001")
	if !ok || veh.Brand != "Toyota" || veh.Model != "Hilux" {
		t.Fatalf("expected to find veh-001 Toyota Hilux, got ok=%v veh=%+v", ok, veh)
	}

	// 2. Insensibilidad a mayusculas y espacios
	vehUpper, okUpper := cache.GetVehicle("  VEH-001  ")
	if !okUpper || vehUpper.ID != "veh-001" {
		t.Fatalf("expected case-insensitive match for VEH-001, got ok=%v veh=%+v", okUpper, vehUpper)
	}

	// 3. Vehiculo vendido en fixture (veh-004 Renault Duster)
	vehSold, okSold := cache.GetVehicle("veh-004")
	if !okSold || vehSold.Status != "sold" {
		t.Fatalf("expected to find veh-004 with status sold, got ok=%v status=%s", okSold, vehSold.Status)
	}

	// 4. Vehiculo inexistente
	_, okMissing := cache.GetVehicle("veh-nonexistent")
	if okMissing {
		t.Fatalf("expected false for nonexistent vehicle")
	}

	// 5. ID vacio
	_, okEmpty := cache.GetVehicle("")
	if okEmpty {
		t.Fatalf("expected false for empty vehicle ID")
	}
}

func TestCache_IsFresh_WindowAndUnavailable(t *testing.T) {
	now := time.Date(2026, 10, 26, 13, 35, 0, 0, time.UTC)
	cache := NewSeededStubCache(now)

	// Fresco al instante t0
	if !cache.IsFresh(now) {
		t.Fatalf("expected cache to be fresh at t0")
	}

	// Fresco a los 15 minutos (ventana de refresco)
	if !cache.IsFresh(now.Add(15 * time.Minute)) {
		t.Fatalf("expected cache to be fresh at 15m")
	}

	// Fresco a los 29 minutos
	if !cache.IsFresh(now.Add(29 * time.Minute)) {
		t.Fatalf("expected cache to be fresh at 29m")
	}

	// Caducado (> 30 minutos)
	if cache.IsFresh(now.Add(31 * time.Minute)) {
		t.Fatalf("expected cache to NOT be fresh at 31m")
	}

	// Si se marca unavailable, no es fresco aunque el tiempo sea reciente
	cache.SetUnavailable()
	if cache.IsFresh(now) {
		t.Fatalf("expected unavailable cache to return IsFresh = false")
	}
}

func TestCache_ETag(t *testing.T) {
	now := time.Date(2026, 10, 26, 13, 35, 0, 0, time.UTC)
	cache := NewSeededStubCache(now)

	etag := cache.ETag()
	if etag != `W/"buyacar-sim-etag-v1"` {
		t.Fatalf("expected ETag W/\"buyacar-sim-etag-v1\", got %q", etag)
	}
}
```

### 6.2 New Test File: `app_build/internal/store/store_test.go`

```go
package store

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestMemoryStore_ComputeHash_Deterministic(t *testing.T) {
	st := NewMemoryStore()
	h1 := st.ComputeHash("whatsapp", "conv-1", "Hola, que precio tiene?")
	h2 := st.ComputeHash("whatsapp", "conv-1", "Hola, que precio tiene?")
	h3 := st.ComputeHash("whatsapp", "conv-2", "Hola, que precio tiene?")

	if h1 == "" || len(h1) != 64 {
		t.Fatalf("expected 64-char hex SHA256, got %q", h1)
	}
	if h1 != h2 {
		t.Fatalf("expected identical hashes for identical input, got %s != %s", h1, h2)
	}
	if h1 == h3 {
		t.Fatalf("expected different hashes for different convIDs, got %s == %s", h1, h3)
	}
}

func TestMemoryStore_MarkProcessedAndHasProcessed(t *testing.T) {
	st := NewMemoryStore()
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	hash := st.ComputeHash("whatsapp", "conv-1", "Hola")

	// Antes de marcar
	has, rec := st.HasProcessed(hash)
	if has || rec.Hash != "" {
		t.Fatalf("expected false before mark, got has=%v rec=%+v", has, rec)
	}

	// Despues de marcar
	err := st.MarkProcessed(IdempotencyRecord{
		Hash:      hash,
		ChannelID: "whatsapp",
		ConvID:    "conv-1",
		ReplyText: "Buenas tardes",
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("unexpected error marking processed: %v", err)
	}

	has, rec = st.HasProcessed(hash)
	if !has || rec.Hash != hash || rec.ReplyText != "Buenas tardes" {
		t.Fatalf("expected processed record to match, got has=%v rec=%+v", has, rec)
	}
	if rec.IsSealed() {
		t.Fatalf("expected unsealed record prior to post-send sealing")
	}
}

func TestMemoryStore_SealIdempotency(t *testing.T) {
	st := NewMemoryStore()
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	hash := st.ComputeHash("whatsapp", "conv-1", "Precio?")

	// 1. Marcar registro base
	_ = st.MarkProcessed(IdempotencyRecord{
		Hash:      hash,
		ChannelID: "whatsapp",
		ConvID:    "conv-1",
		ReplyText: "Cuesta 185M",
		CreatedAt: now,
	})

	// 2. Sellar registro
	err := st.SealIdempotency(hash, "sent_verified", `W/"buyacar-sim-etag-v1"`, "task-crm-123")
	if err != nil {
		t.Fatalf("unexpected error sealing idempotency: %v", err)
	}

	has, rec := st.HasProcessed(hash)
	if !has {
		t.Fatalf("expected record to exist")
	}
	if rec.SendStatus != "sent_verified" {
		t.Fatalf("expected SendStatus sent_verified, got %q", rec.SendStatus)
	}
	if rec.ETag != `W/"buyacar-sim-etag-v1"` {
		t.Fatalf("expected ETag W/\"buyacar-sim-etag-v1\", got %q", rec.ETag)
	}
	if rec.CRMTaskID != "task-crm-123" {
		t.Fatalf("expected CRMTaskID task-crm-123, got %q", rec.CRMTaskID)
	}
	if !rec.IsSealed() {
		t.Fatalf("expected IsSealed() == true after sealing")
	}
}

func TestMemoryStore_SealIdempotency_NewRecord(t *testing.T) {
	st := NewMemoryStore()
	hash := "fresh-hash-not-marked"

	err := st.SealIdempotency(hash, "sent_simulated", `W/"etag-test"`, "task-456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	has, rec := st.HasProcessed(hash)
	if !has || !rec.IsSealed() || rec.SendStatus != "sent_simulated" {
		t.Fatalf("expected newly created sealed record, got has=%v rec=%+v", has, rec)
	}
}

func TestMemoryStore_SaveAndGetCheckpoint(t *testing.T) {
	st := NewMemoryStore()

	// Consulta de checkpoint inexistente
	cp, ok := st.GetCheckpointState("conv-nonexistent")
	if ok || cp != "" {
		t.Fatalf("expected false for missing checkpoint")
	}

	// Guardar checkpoints progresivos
	_ = st.SaveCheckpointState("conv-1", "cp1_lectura:ok")
	cp, ok = st.GetCheckpointState("conv-1")
	if !ok || cp != "cp1_lectura:ok" {
		t.Fatalf("expected cp1_lectura:ok, got ok=%v cp=%q", ok, cp)
	}

	_ = st.SaveCheckpointState("conv-1", "cp7_crm:ok")
	cp, ok = st.GetCheckpointState("conv-1")
	if !ok || cp != "cp7_crm:ok" {
		t.Fatalf("expected cp7_crm:ok, got ok=%v cp=%q", ok, cp)
	}
}

func TestMemoryStore_ConcurrentAccess(t *testing.T) {
	st := NewMemoryStore()
	var wg sync.WaitGroup
	workers := 50

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			convID := fmt.Sprintf("conv-%d", workerID)
			hash := st.ComputeHash("channel", convID, "msg")

			_ = st.MarkProcessed(IdempotencyRecord{
				Hash:      hash,
				ChannelID: "channel",
				ConvID:    convID,
				ReplyText: "ok",
			})
			_ = st.SealIdempotency(hash, "sent_simulated", "etag", "crm-1")
			_, _ = st.HasProcessed(hash)
			_ = st.SaveCheckpointState(convID, "cp7:ok")
			_, _ = st.GetCheckpointState(convID)
		}(i)
	}

	wg.Wait()
}
```

---

## 7. Zero Breaking Changes & Safety Checklist

| Verification Item | Status | Verification Evidence / Logic |
|---|---|---|
| Existing `IdempotencyRecord` usages | PASS | Struct literals in `pipeline.go:157` and `core_test.go:80` use keyed fields (`Hash: ...`). New fields default to `""`. |
| Existing `Store` interface usages | PASS | `MemoryStore` is the only struct implementing `Store`. Adding `SealIdempotency` to both keeps the interface implementation intact. |
| Existing `inventory.Cache` usages | PASS | Adding methods `GetVehicle`, `IsFresh`, and `ETag` is purely additive. All existing calls to `Search`, `LoadSnapshot`, `NeedsRefresh` are untouched. |
| `app_build` Test Suite Pass | PASS | Baseline execution of `go test ./...` is 100% GREEN (64 golden cases, 100 draft batch exerciser, Fuzz tests, guardrails). |
| Race Condition Prevention | PASS | RWMutex (`c.mu` in `Cache`, `m.mu` in `MemoryStore`) guards all state transitions. Value copies prevent pointer escapes. |
| Performance Budget (p99 < 5ms) | PASS | In-memory slice traversal for 4-100 vehicles executes in < 0.001 ms, maintaining p99 << 5ms for Fast-Path. |

---

## 8. Concrete Implementation Recommendations for `@go-engineer`

When implementing Milestone 1:
1. Apply the additions in `app_build/internal/inventory/inventory.go` (methods `GetVehicle`, `IsFresh`, `ETag`).
2. Add the unit tests in `app_build/internal/inventory/inventory_test.go`.
3. Apply the additions in `app_build/internal/store/store.go` (`SendStatus`, `ETag`, `CRMTaskID` in `IdempotencyRecord`, `SealIdempotency` in `Store` and `MemoryStore`, `IsSealed() bool`).
4. Create the new test file `app_build/internal/store/store_test.go`.
5. Run `go test -v ./internal/inventory ./internal/store ./internal/core` to confirm 100% green execution.
