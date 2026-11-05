package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

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

// Store define el contrato de persistencia para idempotencia, estado y reanudacion.
type Store interface {
	ComputeHash(channelID, convID, lastIncomingMessage string) string
	HasProcessed(hash string) (bool, IdempotencyRecord)
	MarkProcessed(record IdempotencyRecord) error
	SealIdempotency(hash string, sendStatus string, etag string, crmTaskID string) error
	SaveCheckpointState(convID string, checkpoint string) error
	GetCheckpointState(convID string) (string, bool)
}

// ResilientStore extiende Store proporcionando tolerancia a fallos de disco y conmutacion automatica.
type ResilientStore interface {
	Store
	IsUsingFallback() bool
	ForceFallback()
	RecoverPrimary(ctx context.Context) error
}

// MemoryStore implementa Store de forma thread-safe para SIMULATION y pruebas.
type MemoryStore struct {
	mu          sync.RWMutex
	processed   map[string]IdempotencyRecord
	checkpoints map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		processed:   make(map[string]IdempotencyRecord),
		checkpoints: make(map[string]string),
	}
}

func (m *MemoryStore) ComputeHash(channelID, convID, lastIncomingMessage string) string {
	h := sha256.New()
	h.Write([]byte(channelID + "|" + convID + "|" + lastIncomingMessage))
	return hex.EncodeToString(h.Sum(nil))
}

func (m *MemoryStore) HasProcessed(hash string) (bool, IdempotencyRecord) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec, ok := m.processed[hash]
	return ok, rec
}

func (m *MemoryStore) MarkProcessed(record IdempotencyRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	m.processed[record.Hash] = record
	return nil
}

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

func (m *MemoryStore) SaveCheckpointState(convID string, checkpoint string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkpoints[convID] = checkpoint
	return nil
}

func (m *MemoryStore) GetCheckpointState(convID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp, ok := m.checkpoints[convID]
	return cp, ok
}
