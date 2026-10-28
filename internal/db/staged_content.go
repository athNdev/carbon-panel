// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AddStagedContent joins the server's one open batch and records a content
// edit. It does not copy bytes or touch the live tree; the caller writes the
// staged file first. A new batch uses the next-restart window and does not
// retarget a batch that is already open.
func (s *Store) AddStagedContent(ctx context.Context, row *StagedContentChange) (*StagedContentChange, error) {
	if row == nil || row.ServerID == "" || row.Kind == "" || row.Action == "" || row.TargetPath == "" {
		return nil, fmt.Errorf("staged content requires a server, kind, action, and target")
	}
	if row.Action != StagedActionDelete && row.StagePath == "" {
		return nil, fmt.Errorf("staged content file is required")
	}
	switch row.Kind {
	case StagedKindMod, StagedKindDatapack, StagedKindFile:
	default:
		return nil, fmt.Errorf("unknown staged content kind %q", row.Kind)
	}
	switch row.Action {
	case StagedActionInstall, StagedActionReplace, StagedActionDelete:
	default:
		return nil, fmt.Errorf("unknown staged content action %q", row.Action)
	}
	batch, err := s.ensureOpenBatch(ctx, row.ServerID, string(UpdateWindowNextRestart))
	if err != nil {
		return nil, err
	}
	if row.ID == "" {
		row.ID = uuid.New().String()
	}
	row.BatchID = batch.ID
	row.Status = StagedStatusStaged
	if err := s.db.WithContext(ctx).Create(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

// GetStagedContent loads one content row, including discarded rows.
func (s *Store) GetStagedContent(ctx context.Context, id string) (*StagedContentChange, error) {
	var row StagedContentChange
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// ListStagedContent returns content still waiting to be applied.
func (s *Store) ListStagedContent(ctx context.Context, serverID string) ([]StagedContentChange, error) {
	var rows []StagedContentChange
	err := s.db.WithContext(ctx).
		Where("server_id = ? AND status = ?", serverID, StagedStatusStaged).
		Order("created_at asc").
		Find(&rows).Error
	return rows, err
}

// DiscardStagedContent marks a staged row discarded. The caller removes any
// staged bytes first; this does not touch the filesystem.
func (s *Store) DiscardStagedContent(ctx context.Context, id string) (*StagedContentChange, error) {
	var row StagedContentChange
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&row).Error
	if err != nil {
		return nil, err
	}
	if row.Status != StagedStatusStaged {
		return nil, fmt.Errorf("staged content %s is %s", id, row.Status)
	}
	row.Status = StagedStatusDiscarded
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// ListOpenUpdateBatches returns every batch that can still receive or apply updates.
func (s *Store) ListOpenUpdateBatches(ctx context.Context) ([]ServerUpdateBatch, error) {
	var rows []ServerUpdateBatch
	err := s.db.WithContext(ctx).Where("status = ?", UpdateBatchOpen).Order("created_at asc").Find(&rows).Error
	return rows, err
}

// OpenUpdateBatch returns the server's open batch, or nil when there is none.
func (s *Store) OpenUpdateBatch(ctx context.Context, serverID string) (*ServerUpdateBatch, error) {
	var batch ServerUpdateBatch
	err := s.db.WithContext(ctx).Where("open_key = ?", serverID).First(&batch).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &batch, nil
}

// SetBatchIdleSince records when the current idle stretch started.
// A nil value clears it, which is what a player joining does.
func (s *Store) SetBatchIdleSince(ctx context.Context, id string, since *time.Time) error {
	return s.db.WithContext(ctx).Model(&ServerUpdateBatch{}).Where("id = ?", id).Update("idle_since", since).Error
}

// CloseUpdateBatch finishes the open batch so another one can be recorded.
func (s *Store) CloseUpdateBatch(ctx context.Context, id, status string) error {
	return s.db.WithContext(ctx).Model(&ServerUpdateBatch{}).Where("id = ? AND status = ?", id, UpdateBatchOpen).Updates(map[string]any{
		"status":   status,
		"open_key": nil,
	}).Error
}

// ListBatchContent returns staged content still waiting in one batch.
func (s *Store) ListBatchContent(ctx context.Context, batchID string) ([]StagedContentChange, error) {
	var rows []StagedContentChange
	err := s.db.WithContext(ctx).
		Where("batch_id = ? AND status = ?", batchID, StagedStatusStaged).
		Order("created_at asc").
		Find(&rows).Error
	return rows, err
}

// MarkStagedContentApplied records that a content row was committed to the live tree.
func (s *Store) MarkStagedContentApplied(ctx context.Context, id string) error {
	now := time.Now()
	res := s.db.WithContext(ctx).Model(&StagedContentChange{}).
		Where("id = ? AND status = ?", id, StagedStatusStaged).
		Updates(map[string]any{"status": StagedStatusApplied, "applied_at": now})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return fmt.Errorf("staged content %s is not waiting to be applied", id)
	}
	return nil
}

// MarkStagedContentStaged returns an applied content row to staged after a failed publish.
func (s *Store) MarkStagedContentStaged(ctx context.Context, id string) error {
	res := s.db.WithContext(ctx).Model(&StagedContentChange{}).
		Where("id = ? AND status = ?", id, StagedStatusApplied).
		Updates(map[string]any{"status": StagedStatusStaged, "applied_at": nil})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return fmt.Errorf("staged content %s is not applied", id)
	}
	return nil
}
