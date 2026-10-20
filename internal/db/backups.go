// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package db

import (
	"context"
)

// CreateBackupRecord inserts a backup row.
func (s *Store) CreateBackupRecord(ctx context.Context, rec *BackupRecord) error {
	return s.db.WithContext(ctx).Create(rec).Error
}

// GetBackupRecord fetches one backup by ID.
func (s *Store) GetBackupRecord(ctx context.Context, id string) (*BackupRecord, error) {
	var rec BackupRecord
	if err := s.db.WithContext(ctx).First(&rec, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}

// ListBackupRecords returns newest-first backups for a server.
func (s *Store) ListBackupRecords(ctx context.Context, serverID string, limit int) ([]*BackupRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var out []*BackupRecord
	return out, s.db.WithContext(ctx).
		Where("server_id = ?", serverID).
		Order("created_at DESC").Limit(limit).Find(&out).Error
}

// SetBackupLocked toggles the sticky flag.
func (s *Store) SetBackupLocked(ctx context.Context, id string, locked bool) error {
	return s.db.WithContext(ctx).Model(&BackupRecord{}).
		Where("id = ?", id).Update("locked", locked).Error
}

// SetBackupStatus updates the status field.
func (s *Store) SetBackupStatus(ctx context.Context, id, status string) error {
	return s.db.WithContext(ctx).Model(&BackupRecord{}).
		Where("id = ?", id).Update("status", status).Error
}

// SetBackupRemoteKey records the S3 object key.
func (s *Store) SetBackupRemoteKey(ctx context.Context, id, key string) error {
	return s.db.WithContext(ctx).Model(&BackupRecord{}).
		Where("id = ?", id).Update("remote_key", key).Error
}

// CommitBackupRemote records the object key and, when clearLocal is set,
// clears the local path in the same write. Callers delete local files only
// after this returns nil so a failed row update cannot drop the only copy.
func (s *Store) CommitBackupRemote(ctx context.Context, id, key string, clearLocal bool) error {
	updates := map[string]any{"remote_key": key}
	if clearLocal {
		updates["path"] = ""
	}
	return s.db.WithContext(ctx).Model(&BackupRecord{}).Where("id = ?", id).Updates(updates).Error
}

// DeleteBackupRecord removes a backup row.
func (s *Store) DeleteBackupRecord(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Where("id = ?", id).Delete(&BackupRecord{}).Error
}
