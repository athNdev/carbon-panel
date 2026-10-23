package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ServerOperation retains uncertain external effects across controller restarts.
// ActiveServer is released only by an explicit successful finalization or recovery.
type ServerOperation struct {
	ID string `gorm:"primaryKey"`
	ServerID string `gorm:"uniqueIndex:server_operation_request"`
	RequestKey string `gorm:"uniqueIndex:server_operation_request"`
	NodeID string
	ContainerID string
	Kind string
	PayloadHash string
	Phase string
	LastError string
	ActiveServer *string `gorm:"uniqueIndex"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (s *Store) BeginServerOperation(ctx context.Context, serverID, nodeID, containerID, key, kind, digest string) (*ServerOperation, bool, error) {
	if serverID == "" || key == "" || digest == "" { return nil, false, errors.New("operation requires server, request key and payload digest") }
	var op ServerOperation
	reused := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Where("server_id = ? AND request_key = ?", serverID, key).First(&op).Error
		if err == nil {
			if op.PayloadHash != digest || op.Kind != kind || op.NodeID != nodeID || op.ContainerID != containerID { return errors.New("idempotency key conflicts with existing operation identity or payload") }
			reused = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) { return err }
		op = ServerOperation{ID:uuid.NewString(), ServerID:serverID, NodeID:nodeID, ContainerID:containerID, RequestKey:key, Kind:kind, PayloadHash:digest, Phase:"prepared", ActiveServer:&serverID}
		return tx.Create(&op).Error
	})
	return &op,reused,err
}

func (s *Store) AdvanceServerOperation(ctx context.Context, id, from, to, detail string) error {
	updates := map[string]any{"phase":to,"last_error":detail,"updated_at":time.Now().UTC()}
	if to == "complete" || to == "recovered" { updates["active_server"] = nil }
	r := s.db.WithContext(ctx).Model(&ServerOperation{}).Where("id = ? AND phase = ? AND active_server IS NOT NULL",id,from).Updates(updates)
	if r.Error != nil { return r.Error }; if r.RowsAffected != 1 { return fmt.Errorf("operation %s phase changed; reconcile before retry",id) }; return nil
}

func (s *Store) AssertNoServerOperation(ctx context.Context, serverID string) error {
	var op ServerOperation
	err := s.db.WithContext(ctx).Where("active_server = ?",serverID).First(&op).Error
	if errors.Is(err,gorm.ErrRecordNotFound) { return nil }; if err != nil {return err}
	return fmt.Errorf("server operation %s (%s, phase %s) requires completion or recovery",op.ID,op.Kind,op.Phase)
}
