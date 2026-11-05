package store

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	ErrCorruptWAL = errors.New("wal_store: corrupt WAL entry detected")
)

const (
	opMarkProcessed   byte = 0x01
	opSealIdempotency byte = 0x02
	opSaveCheckpoint  byte = 0x03
)

// WALSealPayload estructura la mutacion de sellado para el log binario.
type WALSealPayload struct {
	Hash       string `json:"hash"`
	SendStatus string `json:"send_status"`
	ETag       string `json:"etag"`
	CRMTaskID  string `json:"crm_task_id"`
}

// WALCheckpointPayload estructura el checkpoint para el log binario.
type WALCheckpointPayload struct {
	ConvID     string `json:"conv_id"`
	Checkpoint string `json:"checkpoint"`
}

// WALStore implementa un motor transaccional ACID en Go puro con semantica WAL (Write-Ahead Logging).
// Ofrece Durabilidad mediante fsync, Aislamiento con sync.RWMutex y Consistencia mediante sumas CRC32.
type WALStore struct {
	mu          sync.RWMutex
	dir         string
	walPath     string
	file        *os.File
	processed   map[string]IdempotencyRecord
	checkpoints map[string]string
	fault       error // para inyeccion de fallos en pruebas de resiliencia
}

// NewWALStore abre o inicializa un WALStore en el directorio indicado.
func NewWALStore(dir string) (*WALStore, error) {
	if dir == "" {
		return nil, errors.New("wal_store: directory path cannot be empty")
	}

	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("wal_store: failed to create dir: %w", err)
	}

	walPath := filepath.Join(dir, "store.wal")
	ws := &WALStore{
		dir:         dir,
		walPath:     walPath,
		processed:   make(map[string]IdempotencyRecord),
		checkpoints: make(map[string]string),
	}

	if err := ws.replay(); err != nil {
		return nil, fmt.Errorf("wal_store: failed to replay wal: %w", err)
	}

	f, err := os.OpenFile(walPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("wal_store: failed to open wal: %w", err)
	}
	ws.file = f

	return ws, nil
}

// SetFault inyecta un error artificial para simular fallos de disco o SQLite.
func (ws *WALStore) SetFault(err error) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.fault = err
}

func (ws *WALStore) replay() error {
	if _, err := os.Stat(ws.walPath); os.IsNotExist(err) {
		return nil
	}

	f, err := os.Open(ws.walPath)
	if err != nil {
		return err
	}
	defer f.Close()

	header := make([]byte, 9) // 1 byte op + 4 bytes len + 4 bytes crc32
	for {
		_, err := io.ReadFull(f, header)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return err
		}

		op := header[0]
		payloadLen := binary.BigEndian.Uint32(header[1:5])
		expectedCRC := binary.BigEndian.Uint32(header[5:9])

		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(f, payload); err != nil {
			break // corte abrupto en fin de archivo
		}

		actualCRC := crc32.ChecksumIEEE(payload)
		if actualCRC != expectedCRC {
			// entrada corrupta; detener lectura en el punto seguro
			break
		}

		switch op {
		case opMarkProcessed:
			var rec IdempotencyRecord
			if err := json.Unmarshal(payload, &rec); err == nil {
				ws.processed[rec.Hash] = rec
			}
		case opSealIdempotency:
			var seal WALSealPayload
			if err := json.Unmarshal(payload, &seal); err == nil {
				rec, ok := ws.processed[seal.Hash]
				if !ok {
					rec = IdempotencyRecord{Hash: seal.Hash, CreatedAt: time.Now().UTC()}
				}
				rec.SendStatus = seal.SendStatus
				rec.ETag = seal.ETag
				rec.CRMTaskID = seal.CRMTaskID
				ws.processed[seal.Hash] = rec
			}
		case opSaveCheckpoint:
			var cp WALCheckpointPayload
			if err := json.Unmarshal(payload, &cp); err == nil {
				ws.checkpoints[cp.ConvID] = cp.Checkpoint
			}
		}
	}

	return nil
}

func (ws *WALStore) writeEntryLocked(op byte, payload []byte) error {
	if ws.fault != nil {
		return ws.fault
	}
	if ws.file == nil {
		return errors.New("wal_store: file is not open")
	}

	payloadLen := uint32(len(payload))
	crc := crc32.ChecksumIEEE(payload)

	header := make([]byte, 9)
	header[0] = op
	binary.BigEndian.PutUint32(header[1:5], payloadLen)
	binary.BigEndian.PutUint32(header[5:9], crc)

	if _, err := ws.file.Write(header); err != nil {
		return err
	}
	if _, err := ws.file.Write(payload); err != nil {
		return err
	}

	// ACID: Durabilidad mediante fsync
	return ws.file.Sync()
}

func (ws *WALStore) ComputeHash(channelID, convID, lastIncomingMessage string) string {
	h := sha256.New()
	h.Write([]byte(channelID + "|" + convID + "|" + lastIncomingMessage))
	return hex.EncodeToString(h.Sum(nil))
}

func (ws *WALStore) HasProcessed(hash string) (bool, IdempotencyRecord) {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	rec, ok := ws.processed[hash]
	return ok, rec
}

func (ws *WALStore) MarkProcessed(record IdempotencyRecord) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}

	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}

	if err := ws.writeEntryLocked(opMarkProcessed, payload); err != nil {
		return err
	}

	ws.processed[record.Hash] = record
	return nil
}

func (ws *WALStore) SealIdempotency(hash string, sendStatus string, etag string, crmTaskID string) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	seal := WALSealPayload{
		Hash:       hash,
		SendStatus: sendStatus,
		ETag:       etag,
		CRMTaskID:  crmTaskID,
	}

	payload, err := json.Marshal(seal)
	if err != nil {
		return err
	}

	if err := ws.writeEntryLocked(opSealIdempotency, payload); err != nil {
		return err
	}

	rec, ok := ws.processed[hash]
	if !ok {
		rec = IdempotencyRecord{
			Hash:      hash,
			CreatedAt: time.Now().UTC(),
		}
	}
	rec.SendStatus = sendStatus
	rec.ETag = etag
	rec.CRMTaskID = crmTaskID
	ws.processed[hash] = rec

	return nil
}

func (ws *WALStore) SaveCheckpointState(convID string, checkpoint string) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	cp := WALCheckpointPayload{
		ConvID:     convID,
		Checkpoint: checkpoint,
	}

	payload, err := json.Marshal(cp)
	if err != nil {
		return err
	}

	if err := ws.writeEntryLocked(opSaveCheckpoint, payload); err != nil {
		return err
	}

	ws.checkpoints[convID] = checkpoint
	return nil
}

func (ws *WALStore) GetCheckpointState(convID string) (string, bool) {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	cp, ok := ws.checkpoints[convID]
	return cp, ok
}

// Close cierra el descriptor de archivo de WAL.
func (ws *WALStore) Close() error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.file != nil {
		err := ws.file.Close()
		ws.file = nil
		return err
	}
	return nil
}
