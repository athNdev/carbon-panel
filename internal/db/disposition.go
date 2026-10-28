// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package db

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	DispositionDelete   = "delete"
	DispositionKeepData = "keep_data"
	DispositionDetach   = "detach"

	DispositionPending     = "pending"
	DispositionDataSettled = "data_settled"
	DispositionSettled     = "settled"
)

// ServerDisposition records what a delete chose to do with a server's
// container and world. It outlives the panel row so a finished delete is
// still accounted for, and an unfinished one can be retried.
type ServerDisposition struct {
	ID          string `gorm:"primaryKey"`
	ServerID    string `gorm:"index"`
	Name        string
	NodeID      string
	ContainerID string
	DataPath    string
	Port        int
	Mode        string
	Phase       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// BeginServerDisposition inserts the disposition, or returns the unfinished
// row for this server so a retry continues the same record.
func (s *Store) BeginServerDisposition(ctx context.Context, row *ServerDisposition) (*ServerDisposition, error) {
	if row == nil || row.ServerID == "" || row.Mode == "" {
		return nil, errors.New("disposition requires a server and a mode")
	}
	var existing ServerDisposition
	err := s.db.WithContext(ctx).
		Where("server_id = ? AND phase <> ?", row.ServerID, DispositionSettled).
		Order("created_at desc").
		First(&existing).Error
	if err == nil {
		existing.Name = row.Name
		existing.NodeID = row.NodeID
		existing.ContainerID = row.ContainerID
		existing.DataPath = row.DataPath
		existing.Port = row.Port
		existing.Mode = row.Mode
		existing.UpdatedAt = time.Now().UTC()
		if err := s.db.WithContext(ctx).Save(&existing).Error; err != nil {
			return nil, err
		}
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	row.ID = uuid.NewString()
	row.Phase = DispositionPending
	row.CreatedAt = time.Now().UTC()
	row.UpdatedAt = row.CreatedAt
	if err := s.db.WithContext(ctx).Create(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

func (s *Store) SetServerDispositionPhase(ctx context.Context, id, phase string) error {
	if id == "" || phase == "" {
		return errors.New("disposition id and phase are required")
	}
	res := s.db.WithContext(ctx).Model(&ServerDisposition{}).Where("id = ?", id).
		Updates(map[string]any{"phase": phase, "updated_at": time.Now().UTC()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return errors.New("disposition not found")
	}
	return nil
}

func (s *Store) ListUnsettledDispositions(ctx context.Context) ([]ServerDisposition, error) {
	var rows []ServerDisposition
	err := s.db.WithContext(ctx).Where("phase <> ?", DispositionSettled).Order("created_at asc").Find(&rows).Error
	return rows, err
}

func (s *Store) LatestServerDisposition(ctx context.Context, serverID string) (*ServerDisposition, error) {
	var row ServerDisposition
	err := s.db.WithContext(ctx).Where("server_id = ?", serverID).Order("created_at desc").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}
