// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
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
