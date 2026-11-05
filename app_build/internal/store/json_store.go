package store

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// JSONLogEntry modela cada linea persistida en el log JSON plano de contingencia.
type JSONLogEntry struct {
	Type       string             `json:"type"` // "processed", "sealed", "checkpoint"
	Record     *IdempotencyRecord `json:"record,omitempty"`
	Hash       string             `json:"hash,omitempty"`
	SendStatus string             `json:"send_status,omitempty"`
	ETag       string             `json:"etag,omitempty"`
	CRMTaskID  string             `json:"crm_task_id,omitempty"`
	ConvID     string             `json:"conv_id,omitempty"`
	Checkpoint string             `json:"checkpoint,omitempty"`
	Timestamp  time.Time          `json:"timestamp"`
}

// JSONStore implementa Store con persistencia en memoria y respaldo en archivo plano JSON (JSON lines).
// Actua como fallback automatico ante fallos en el motor transaccional primario.
type JSONStore struct {
	mu          sync.RWMutex
	filePath    string
	file        *os.File
	processed   map[string]IdempotencyRecord
	checkpoints map[string]string
}

// NewJSONStore inicializa un JSONStore y recupera el estado previo si el archivo ya existe.
func NewJSONStore(filePath string) (*JSONStore, error) {
	js := &JSONStore{
		filePath:    filePath,
		processed:   make(map[string]IdempotencyRecord),
		checkpoints: make(map[string]string),
	}

	if filePath != "" {
		if err := os.MkdirAll(filepath.Dir(filePath), 0750); err != nil {
			return nil, fmt.Errorf("json_store: failed to create directory: %w", err)
		}

		if err := js.replay(); err != nil {
			return nil, fmt.Errorf("json_store: failed to replay log: %w", err)
		}

		f, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return nil, fmt.Errorf("json_store: failed to open file for append: %w", err)
		}
		js.file = f
	}

	return js, nil
}

func (js *JSONStore) replay() error {
	if _, err := os.Stat(js.filePath); os.IsNotExist(err) {
		return nil
	}

	f, err := os.Open(js.filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry JSONLogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue // tolera lineas incompletas si hubo corte abruptly
		}

		switch entry.Type {
		case "processed":
			if entry.Record != nil {
				js.processed[entry.Record.Hash] = *entry.Record
			}
		case "sealed":
			if rec, ok := js.processed[entry.Hash]; ok {
				rec.SendStatus = entry.SendStatus
				rec.ETag = entry.ETag
				rec.CRMTaskID = entry.CRMTaskID
				js.processed[entry.Hash] = rec
			} else {
				js.processed[entry.Hash] = IdempotencyRecord{
					Hash:       entry.Hash,
					SendStatus: entry.SendStatus,
					ETag:       entry.ETag,
					CRMTaskID:  entry.CRMTaskID,
					CreatedAt:  entry.Timestamp,
				}
			}
		case "checkpoint":
			if entry.ConvID != "" {
				js.checkpoints[entry.ConvID] = entry.Checkpoint
			}
		}
	}

	return scanner.Err()
}

func (js *JSONStore) appendEntryLocked(entry JSONLogEntry) error {
	if js.file == nil {
		return nil
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if _, err := js.file.Write(data); err != nil {
		return err
	}
	return js.file.Sync()
}

func (js *JSONStore) ComputeHash(channelID, convID, lastIncomingMessage string) string {
	h := sha256.New()
	h.Write([]byte(channelID + "|" + convID + "|" + lastIncomingMessage))
	return hex.EncodeToString(h.Sum(nil))
}

func (js *JSONStore) HasProcessed(hash string) (bool, IdempotencyRecord) {
	js.mu.RLock()
	defer js.mu.RUnlock()
	rec, ok := js.processed[hash]
	return ok, rec
}

func (js *JSONStore) MarkProcessed(record IdempotencyRecord) error {
	js.mu.Lock()
	defer js.mu.Unlock()

	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}

	js.processed[record.Hash] = record

	entry := JSONLogEntry{
		Type:      "processed",
		Record:    &record,
		Timestamp: time.Now().UTC(),
	}
	return js.appendEntryLocked(entry)
}

func (js *JSONStore) SealIdempotency(hash string, sendStatus string, etag string, crmTaskID string) error {
	js.mu.Lock()
	defer js.mu.Unlock()

	rec, ok := js.processed[hash]
	if !ok {
		rec = IdempotencyRecord{
			Hash:      hash,
			CreatedAt: time.Now().UTC(),
		}
	}
	rec.SendStatus = sendStatus
	rec.ETag = etag
	rec.CRMTaskID = crmTaskID
	js.processed[hash] = rec

	entry := JSONLogEntry{
		Type:       "sealed",
		Hash:       hash,
		SendStatus: sendStatus,
		ETag:       etag,
		CRMTaskID:  crmTaskID,
		Timestamp:  time.Now().UTC(),
	}
	return js.appendEntryLocked(entry)
}

func (js *JSONStore) SaveCheckpointState(convID string, checkpoint string) error {
	js.mu.Lock()
	defer js.mu.Unlock()

	js.checkpoints[convID] = checkpoint

	entry := JSONLogEntry{
		Type:       "checkpoint",
		ConvID:     convID,
		Checkpoint: checkpoint,
		Timestamp:  time.Now().UTC(),
	}
	return js.appendEntryLocked(entry)
}

func (js *JSONStore) GetCheckpointState(convID string) (string, bool) {
	js.mu.RLock()
	defer js.mu.RUnlock()
	cp, ok := js.checkpoints[convID]
	return cp, ok
}

// Close cierra el descriptor de archivo subyacente.
func (js *JSONStore) Close() error {
	js.mu.Lock()
	defer js.mu.Unlock()
	if js.file != nil {
		err := js.file.Close()
		js.file = nil
		return err
	}
	return nil
}

// CopySnapshot retorna una copia del estado en memoria para sincronizacion de recuperacion.
func (js *JSONStore) CopySnapshot() (map[string]IdempotencyRecord, map[string]string) {
	js.mu.RLock()
	defer js.mu.RUnlock()

	records := make(map[string]IdempotencyRecord, len(js.processed))
	for k, v := range js.processed {
		records[k] = v
	}

	checkpoints := make(map[string]string, len(js.checkpoints))
	for k, v := range js.checkpoints {
		checkpoints[k] = v
	}

	return records, checkpoints
}
