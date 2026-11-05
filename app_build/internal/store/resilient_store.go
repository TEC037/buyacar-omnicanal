package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"buyacar.com.co/omnicanal/internal/escalate"
)

var (
	ErrPrimaryFailed = errors.New("resilient_store: primary store failed")
)

// ResilientStoreImpl administra el motor transaccional primario con conmutacion transparente
// a almacenamiento de contingencia y emision de alertas a escalate.Queue.
type ResilientStoreImpl struct {
	mu            sync.RWMutex
	primary       Store
	fallback      Store
	queue         *escalate.Queue
	usingFallback bool
	lastError     error
	failoverCount int64
}

// NewResilientStore construye una instancia de ResilientStore con primario, fallback y cola de escalamiento.
func NewResilientStore(primary Store, fallback Store, queue *escalate.Queue) *ResilientStoreImpl {
	if fallback == nil {
		fallback = NewMemoryStore()
	}
	return &ResilientStoreImpl{
		primary:  primary,
		fallback: fallback,
		queue:    queue,
	}
}

// IsUsingFallback indica si actualmente el almacen esta operando en modo contingencia.
func (r *ResilientStoreImpl) IsUsingFallback() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.usingFallback
}

// ForceFallback activa de forma forzada el modo de contingencia (util para pruebas y mantenimiento).
func (r *ResilientStoreImpl) ForceFallback() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.usingFallback = true
}

func (r *ResilientStoreImpl) triggerFallbackLocked(err error) {
	r.usingFallback = true
	r.lastError = err
	r.failoverCount++

	if r.queue != nil {
		r.queue.Enqueue(
			"store",
			"resilient_store",
			"store_disk_failover",
			fmt.Sprintf("Primary store failed (%v). Conmutado a contingencia en memoria + JSON plano.", err),
			time.Now().UTC(),
		)
	}
}

// RecoverPrimary intenta restablecer el almacenamiento primario sincronizando deltas acumulados.
func (r *ResilientStoreImpl) RecoverPrimary(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	if !r.usingFallback {
		return nil
	}

	// Probar si el primario responde adecuadamente
	testHash := r.primary.ComputeHash("healthcheck", "ping", "test")
	testRec := IdempotencyRecord{
		Hash:       testHash,
		ChannelID:  "healthcheck",
		ConvID:     "ping",
		ReplyText:  "health_probe",
		CreatedAt:  time.Now().UTC(),
		SendStatus: "simulated_probe",
		ETag:       "health_etag",
	}

	if err := r.primary.MarkProcessed(testRec); err != nil {
		return fmt.Errorf("resilient_store: primary still unhealthy: %w", err)
	}

	// Sincronizar datos desde el fallback si es un JSONStore con CopySnapshot
	if js, ok := r.fallback.(*JSONStore); ok {
		records, checkpoints := js.CopySnapshot()
		for _, rec := range records {
			if rec.Hash != testHash {
				_ = r.primary.MarkProcessed(rec)
			}
		}
		for convID, cp := range checkpoints {
			_ = r.primary.SaveCheckpointState(convID, cp)
		}
	}

	r.usingFallback = false
	r.lastError = nil
	return nil
}

func (r *ResilientStoreImpl) ComputeHash(channelID, convID, lastIncomingMessage string) string {
	h := sha256.New()
	h.Write([]byte(channelID + "|" + convID + "|" + lastIncomingMessage))
	return hex.EncodeToString(h.Sum(nil))
}

func (r *ResilientStoreImpl) HasProcessed(hash string) (bool, IdempotencyRecord) {
	r.mu.RLock()
	fallbackMode := r.usingFallback
	r.mu.RUnlock()

	if fallbackMode {
		if ok, rec := r.fallback.HasProcessed(hash); ok {
			return true, rec
		}
		if r.primary != nil {
			return r.primary.HasProcessed(hash)
		}
		return false, IdempotencyRecord{}
	}

	if r.primary != nil {
		ok, rec := r.primary.HasProcessed(hash)
		if ok {
			return true, rec
		}
	}
	return r.fallback.HasProcessed(hash)
}

func (r *ResilientStoreImpl) MarkProcessed(record IdempotencyRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.usingFallback && r.primary != nil {
		if err := r.primary.MarkProcessed(record); err != nil {
			r.triggerFallbackLocked(err)
			return r.fallback.MarkProcessed(record)
		}
		return nil
	}

	return r.fallback.MarkProcessed(record)
}

func (r *ResilientStoreImpl) SealIdempotency(hash string, sendStatus string, etag string, crmTaskID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.usingFallback && r.primary != nil {
		if err := r.primary.SealIdempotency(hash, sendStatus, etag, crmTaskID); err != nil {
			r.triggerFallbackLocked(err)
			return r.fallback.SealIdempotency(hash, sendStatus, etag, crmTaskID)
		}
		return nil
	}

	return r.fallback.SealIdempotency(hash, sendStatus, etag, crmTaskID)
}

func (r *ResilientStoreImpl) SaveCheckpointState(convID string, checkpoint string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.usingFallback && r.primary != nil {
		if err := r.primary.SaveCheckpointState(convID, checkpoint); err != nil {
			r.triggerFallbackLocked(err)
			return r.fallback.SaveCheckpointState(convID, checkpoint)
		}
		return nil
	}

	return r.fallback.SaveCheckpointState(convID, checkpoint)
}

func (r *ResilientStoreImpl) GetCheckpointState(convID string) (string, bool) {
	r.mu.RLock()
	fallbackMode := r.usingFallback
	r.mu.RUnlock()

	if fallbackMode {
		if cp, ok := r.fallback.GetCheckpointState(convID); ok {
			return cp, true
		}
		if r.primary != nil {
			return r.primary.GetCheckpointState(convID)
		}
		return "", false
	}

	if r.primary != nil {
		cp, ok := r.primary.GetCheckpointState(convID)
		if ok {
			return cp, true
		}
	}
	return r.fallback.GetCheckpointState(convID)
}
