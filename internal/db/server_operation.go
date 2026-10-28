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

// ServerOperation retains uncertain external effects across controller restarts.
// ActiveServer is released only by an explicit successful finalization or recovery.
type ServerOperation struct {
	ID         string `gorm:"primaryKey"`
	ServerID   string `gorm:"uniqueIndex:server_operation_request"`
	RequestKey string `gorm:"uniqueIndex:server_operation_request"`
	NodeID     string
	// EndpointGeneration is the node endpoint this operation was bound to.
	EndpointGeneration int64
	// DaemonID is the Docker daemon id accepted for that endpoint at the time
	// the operation was recorded. Empty when the node had not been observed yet.
	DaemonID     string
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
		lookupErr := tx.Where("server_id = ? AND request_key = ?", serverID, key).First(&op).Error
		binding, bindErr := lookupNodeBinding(tx, nodeID)
		if bindErr != nil {
			return bindErr
		}
		if lookupErr == nil {
			if op.PayloadHash != digest || op.Kind != kind || op.NodeID != nodeID || op.ContainerID != containerID || op.EndpointGeneration != binding.generation || daemonIdentityConflicts(op.DaemonID, binding.daemonID) {
				return errors.New("idempotency key conflicts with existing operation identity or payload")
			}
			reused = true
			return nil
		}
		if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			return lookupErr
		}
		op = ServerOperation{ID: uuid.NewString(), ServerID: serverID, NodeID: nodeID, ContainerID: containerID, EndpointGeneration: binding.generation, DaemonID: binding.daemonID, RequestKey: key, Kind: kind, PayloadHash: digest, Phase: ServerOperationPrepared, ActiveServer: &serverID}
		return tx.Create(&op).Error
	})
	return &op, reused, err
}

type nodeBinding struct {
	generation int64
	daemonID   string
}

func lookupNodeBinding(tx *gorm.DB, nodeID string) (nodeBinding, error) {
	if strings.TrimSpace(nodeID) == "" {
		return nodeBinding{}, nil
	}
	var node Node
	err := tx.Where("id = ?", nodeID).First(&node).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nodeBinding{}, nil
	}
	if err != nil {
		return nodeBinding{}, err
	}
	return nodeBinding{generation: node.EndpointGeneration, daemonID: node.DaemonID}, nil
}

func daemonIdentityConflicts(recorded, current string) bool {
	return recorded != "" && current != "" && recorded != current
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
