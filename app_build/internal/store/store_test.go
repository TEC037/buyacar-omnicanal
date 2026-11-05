package store

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"buyacar.com.co/omnicanal/internal/escalate"
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

func TestWALStore_ACIDPersistenceAndReplay(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Crear y escribir en WALStore
	ws, err := NewWALStore(tempDir)
	if err != nil {
		t.Fatalf("unexpected error creating WALStore: %v", err)
	}

	hash := ws.ComputeHash("whatsapp", "conv-wal-1", "Hola, precio?")
	now := time.Now().UTC().Truncate(time.Millisecond)

	rec := IdempotencyRecord{
		Hash:      hash,
		ChannelID: "whatsapp",
		ConvID:    "conv-wal-1",
		ReplyText: "El precio es 95M",
		CreatedAt: now,
	}

	if err := ws.MarkProcessed(rec); err != nil {
		t.Fatalf("failed MarkProcessed: %v", err)
	}

	if err := ws.SealIdempotency(hash, "sent_verified", `W/"etag-wal-1"`, "crm-wal-task-1"); err != nil {
		t.Fatalf("failed SealIdempotency: %v", err)
	}

	if err := ws.SaveCheckpointState("conv-wal-1", "cp7_crm:ok"); err != nil {
		t.Fatalf("failed SaveCheckpointState: %v", err)
	}

	if err := ws.Close(); err != nil {
		t.Fatalf("failed Close: %v", err)
	}

	// 2. Reabrir desde disco y verificar recuperacion ACID (Durabilidad)
	wsReopened, err := NewWALStore(tempDir)
	if err != nil {
		t.Fatalf("failed reopening WALStore: %v", err)
	}
	defer wsReopened.Close()

	has, recoveredRec := wsReopened.HasProcessed(hash)
	if !has {
		t.Fatalf("expected record to be recovered from WAL")
	}
	if recoveredRec.ReplyText != "El precio es 95M" {
		t.Fatalf("expected reply text 'El precio es 95M', got %q", recoveredRec.ReplyText)
	}
	if recoveredRec.SendStatus != "sent_verified" || recoveredRec.ETag != `W/"etag-wal-1"` {
		t.Fatalf("expected sealed record, got status=%s etag=%s", recoveredRec.SendStatus, recoveredRec.ETag)
	}
	if !recoveredRec.IsSealed() {
		t.Fatalf("expected IsSealed == true")
	}

	cp, ok := wsReopened.GetCheckpointState("conv-wal-1")
	if !ok || cp != "cp7_crm:ok" {
		t.Fatalf("expected checkpoint cp7_crm:ok, got ok=%v cp=%q", ok, cp)
	}
}

func TestJSONStore_PersistenceAndReplay(t *testing.T) {
	tempFile := filepath.Join(t.TempDir(), "fallback.jsonl")

	js, err := NewJSONStore(tempFile)
	if err != nil {
		t.Fatalf("failed creating JSONStore: %v", err)
	}

	hash := js.ComputeHash("messenger", "conv-json-1", "Disponibilidad?")
	rec := IdempotencyRecord{
		Hash:      hash,
		ChannelID: "messenger",
		ConvID:    "conv-json-1",
		ReplyText: "Disponible de inmediato",
		CreatedAt: time.Now().UTC(),
	}

	if err := js.MarkProcessed(rec); err != nil {
		t.Fatalf("failed MarkProcessed: %v", err)
	}
	if err := js.SealIdempotency(hash, "sent_drafted", `W/"etag-json-1"`, "task-json-1"); err != nil {
		t.Fatalf("failed SealIdempotency: %v", err)
	}
	if err := js.SaveCheckpointState("conv-json-1", "cp5_guard:ok"); err != nil {
		t.Fatalf("failed SaveCheckpointState: %v", err)
	}
	_ = js.Close()

	// Reabrir y verificar replay
	jsReopened, err := NewJSONStore(tempFile)
	if err != nil {
		t.Fatalf("failed reopening JSONStore: %v", err)
	}
	defer jsReopened.Close()

	has, recovered := jsReopened.HasProcessed(hash)
	if !has || !recovered.IsSealed() || recovered.CRMTaskID != "task-json-1" {
		t.Fatalf("expected recovered sealed record, got has=%v rec=%+v", has, recovered)
	}

	cp, ok := jsReopened.GetCheckpointState("conv-json-1")
	if !ok || cp != "cp5_guard:ok" {
		t.Fatalf("expected cp5_guard:ok, got ok=%v cp=%s", ok, cp)
	}
}

func TestResilientStore_AutomaticFailoverAndAlert(t *testing.T) {
	tempDir := t.TempDir()
	walPath := filepath.Join(tempDir, "wal")
	jsonPath := filepath.Join(tempDir, "fallback.jsonl")

	primary, err := NewWALStore(walPath)
	if err != nil {
		t.Fatalf("failed creating primary WALStore: %v", err)
	}
	defer primary.Close()

	fallback, err := NewJSONStore(jsonPath)
	if err != nil {
		t.Fatalf("failed creating fallback JSONStore: %v", err)
	}
	defer fallback.Close()

	queue := escalate.NewQueue()
	resStore := NewResilientStore(primary, fallback, queue)

	if resStore.IsUsingFallback() {
		t.Fatalf("expected not using fallback initially")
	}

	// 1. Operacion normal en primario
	h1 := resStore.ComputeHash("whatsapp", "conv-1", "msg1")
	err = resStore.MarkProcessed(IdempotencyRecord{
		Hash:      h1,
		ChannelID: "whatsapp",
		ConvID:    "conv-1",
		ReplyText: "Hola",
	})
	if err != nil {
		t.Fatalf("unexpected error on primary write: %v", err)
	}

	// 2. Inyectar fallo de disco en el primario
	primary.SetFault(fmt.Errorf("simulated disk I/O error"))

	// 3. Siguiente escritura debe activar failover automatico sin fallar y emitir alerta
	h2 := resStore.ComputeHash("whatsapp", "conv-2", "msg2")
	err = resStore.MarkProcessed(IdempotencyRecord{
		Hash:      h2,
		ChannelID: "whatsapp",
		ConvID:    "conv-2",
		ReplyText: "Respuesta en contingencia",
	})
	if err != nil {
		t.Fatalf("failover write should have succeeded on fallback, got: %v", err)
	}

	if !resStore.IsUsingFallback() {
		t.Fatalf("expected ResilientStore to be in fallback mode")
	}

	// Verificar alerta en escalate.Queue
	tickets := queue.Pending()
	if len(tickets) == 0 {
		t.Fatalf("expected escalation ticket on disk failover")
	}
	if tickets[0].Reason != "store_disk_failover" {
		t.Fatalf("expected reason store_disk_failover, got %q", tickets[0].Reason)
	}

	// 4. Lecturas deben funcionar consultando fallback y primario
	has1, _ := resStore.HasProcessed(h1)
	if !has1 {
		t.Fatalf("expected to read h1 originally written to primary")
	}
	has2, rec2 := resStore.HasProcessed(h2)
	if !has2 || rec2.ReplyText != "Respuesta en contingencia" {
		t.Fatalf("expected to read h2 written to fallback")
	}

	// 5. Recuperacion de primario (sanar disco)
	primary.SetFault(nil)
	ctx := context.Background()
	if err := resStore.RecoverPrimary(ctx); err != nil {
		t.Fatalf("expected successful recovery: %v", err)
	}

	if resStore.IsUsingFallback() {
		t.Fatalf("expected fallback to be deactivated after recovery")
	}

	// El registro h2 que estuvo en fallback ahora debe estar sincronizado en el primario
	hasInPrimary, _ := primary.HasProcessed(h2)
	if !hasInPrimary {
		t.Fatalf("expected record h2 to have been synced to primary during recovery")
	}
}
