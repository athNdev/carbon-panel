package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Server operation phases. Terminal phases release the per-server active lock.
const (
	ServerOperationPrepared  = "prepared"
	ServerOperationExecuting = "executing"
	ServerOperationComplete  = "complete"
	ServerOperationFailed    = "failed"
	ServerOperationRecovered = "recovered"
)

// ServerOperationRetention is how long a finished lifecycle journal row is kept.
const ServerOperationRetention = 30 * 24 * time.Hour

// ErrActiveOperation is returned when a server already has an operation row
// that still owns its lock. Callers must not mutate that world.
var ErrActiveOperation = errors.New("server already has an active operation")

// ServerOperation retains uncertain external effects across controller restarts.
// ActiveServer is released only by an explicit successful finalization or recovery.
type ServerOperation struct {
	ID           string `gorm:"primaryKey"`
	ServerID     string `gorm:"uniqueIndex:server_operation_request"`
	RequestKey   string `gorm:"uniqueIndex:server_operation_request"`
	NodeID       string
	ContainerID  string
	Kind         string
	PayloadHash  string
	Phase        string
	LastError    string
	ActiveServer *string `gorm:"uniqueIndex"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (s *Store) BeginServerOperation(ctx context.Context, serverID, nodeID, containerID, key, kind, digest string) (*ServerOperation, bool, error) {
	if serverID == "" || key == "" || digest == "" {
		return nil, false, errors.New("operation requires server, request key and payload digest")
	}
	var op ServerOperation
	reused := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Where("server_id = ? AND request_key = ?", serverID, key).First(&op).Error
		if err == nil {
			if op.PayloadHash != digest || op.Kind != kind || op.NodeID != nodeID || op.ContainerID != containerID {
				return errors.New("idempotency key conflicts with existing operation identity or payload")
			}
			reused = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		op = ServerOperation{ID: uuid.NewString(), ServerID: serverID, NodeID: nodeID, ContainerID: containerID, RequestKey: key, Kind: kind, PayloadHash: digest, Phase: ServerOperationPrepared, ActiveServer: &serverID}
		if err := tx.Create(&op).Error; err != nil {
			if strings.Contains(err.Error(), "active_server") {
				return ErrActiveOperation
			}
			return err
		}
		return nil
	})
	return &op, reused, err
}

// NoteActiveServerOperation records the next action on a lock that must stay
// held. It does not change the phase or release ActiveServer.
func (s *Store) NoteActiveServerOperation(ctx context.Context, id, detail string) error {
	r := s.db.WithContext(ctx).Model(&ServerOperation{}).
		Where("id = ? AND active_server IS NOT NULL", id).
		Updates(map[string]any{"last_error": detail, "updated_at": time.Now().UTC()})
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return fmt.Errorf("operation %s is no longer active", id)
	}
	return nil
}

func (s *Store) AdvanceServerOperation(ctx context.Context, id, from, to, detail string) error {
	updates := map[string]any{"phase": to, "last_error": detail, "updated_at": time.Now().UTC()}
	if to == ServerOperationComplete || to == ServerOperationRecovered || to == ServerOperationFailed {
		updates["active_server"] = nil
	}
	r := s.db.WithContext(ctx).Model(&ServerOperation{}).Where("id = ? AND phase = ? AND active_server IS NOT NULL", id, from).Updates(updates)
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return fmt.Errorf("operation %s phase changed; reconcile before retry", id)
	}
	return nil
}

// ListActiveServerOperations returns every operation that still owns a server lock.
// Startup recovery observes the container state for each one before any retry is
// allowed, instead of treating an interrupted mutation as if it never happened.
func (s *Store) ListActiveServerOperations(ctx context.Context) ([]ServerOperation, error) {
	var ops []ServerOperation
	err := s.db.WithContext(ctx).Where("active_server IS NOT NULL").Order("created_at asc").Find(&ops).Error
	return ops, err
}

func (s *Store) AssertNoServerOperation(ctx context.Context, serverID string) error {
	var op ServerOperation
	err := s.db.WithContext(ctx).Where("active_server = ?", serverID).First(&op).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("server operation %s (%s, phase %s) requires completion or recovery", op.ID, op.Kind, op.Phase)
}

// GetServerOperation returns one journal row by id.
func (s *Store) GetServerOperation(ctx context.Context, id string) (*ServerOperation, error) {
	var op ServerOperation
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&op).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("operation %s not found", id)
	}
	if err != nil {
		return nil, err
	}
	return &op, nil
}

// ListServerOperations returns the journal, oldest first, including finished rows.
func (s *Store) ListServerOperations(ctx context.Context) ([]ServerOperation, error) {
	var ops []ServerOperation
	err := s.db.WithContext(ctx).Order("created_at asc").Find(&ops).Error
	return ops, err
}

// ResolveServerOperation releases an active lifecycle lock without touching Docker.
// An already-released operation is left unchanged.
func (s *Store) ResolveServerOperation(ctx context.Context, id, note string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("operation id is required")
	}
	note = strings.TrimSpace(note)
	if note == "" {
		note = "operator released the lifecycle lock without changing containers"
	}
	if len(note) > 500 {
		note = note[:500]
	}
	op, err := s.GetServerOperation(ctx, id)
	if err != nil {
		return err
	}
	if op.ActiveServer == nil {
		return nil
	}
	return s.AdvanceServerOperation(ctx, op.ID, op.Phase, ServerOperationRecovered, note)
}

// PruneServerOperations deletes finished journal rows last updated before olderThan.
// Rows that still hold a server lock are kept.
func (s *Store) PruneServerOperations(ctx context.Context, olderThan time.Time) (int64, error) {
	res := s.db.WithContext(ctx).Where("active_server IS NULL AND updated_at < ?", olderThan).Delete(&ServerOperation{})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}
