package db

import (
	"context"
	"fmt"
)

// CreateSnapshot stores a new server snapshot record
func (s *Store) CreateSnapshot(ctx context.Context, snapshot *ServerSnapshot) error {
	if snapshot == nil {
		return fmt.Errorf("snapshot cannot be nil")
	}
	return s.db.WithContext(ctx).Create(snapshot).Error
}

// GetSnapshot retrieves a server snapshot by ID
func (s *Store) GetSnapshot(ctx context.Context, id string) (*ServerSnapshot, error) {
	var snapshot ServerSnapshot
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&snapshot).Error; err != nil {
		return nil, err
	}
	return &snapshot, nil
}

// ListSnapshots retrieves all snapshots for a server, newest first
func (s *Store) ListSnapshots(ctx context.Context, serverID string) ([]*ServerSnapshot, error) {
	var snapshots []*ServerSnapshot
	err := s.db.WithContext(ctx).
		Where("server_id = ?", serverID).
		Order("created_at desc").
		Find(&snapshots).Error
	if err != nil {
		return nil, err
	}
	return snapshots, nil
}

// DeleteSnapshot removes a snapshot record from the database
func (s *Store) DeleteSnapshot(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Where("id = ?", id).Delete(&ServerSnapshot{}).Error
}
