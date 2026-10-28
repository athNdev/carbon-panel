// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package scheduler

import (
	"context"
	"time"

	storage "github.com/athNdev/carbon-panel/internal/db"
)

// applyDueStagedConfigs used to merge a due cron diff into the live
// ServerConfig without restarting the container. That applied the database
// row while a running, frozen, or deep-sleeping process kept the old env.
// Rows now wait for an explicit apply window (OSS-283 / OSS-285).
func (s *Scheduler) applyDueStagedConfigs(context.Context) {}

// stagedConfigStore is the subset of storage.Store used for staged rollouts,
// kept as an interface so the restart hook is unit-testable without a DB.
type stagedConfigStore interface {
	ListStagedConfigChanges(ctx context.Context, serverID string, status storage.StagedStatus) ([]*storage.StagedConfigChange, error)
	ApplyStagedConfigChange(ctx context.Context, id string) (*storage.StagedConfigChange, error)
	RollbackStagedConfigChange(ctx context.Context, id string) (*storage.StagedConfigChange, error)
}

func applyStagedByMode(ctx context.Context, store stagedConfigStore, serverID string, mode storage.StagedApplyMode) ([]*storage.StagedConfigChange, error) {
	changes, err := store.ListStagedConfigChanges(ctx, serverID, storage.StagedStatusStaged)
	if err != nil {
		return nil, err
	}
	var applied []*storage.StagedConfigChange
	for _, c := range changes {
		if c.ApplyMode != mode {
			continue
		}
		rec, err := store.ApplyStagedConfigChange(ctx, c.ID)
		if err != nil {
			return applied, err
		}
		applied = append(applied, rec)
	}
	return applied, nil
}

// ApplyOnRestartStagedConfigRecords applies all "on_restart" staged changes for a
// server and returns the applied records (SCH-1).
func ApplyOnRestartStagedConfigRecords(ctx context.Context, store stagedConfigStore, serverID string) ([]*storage.StagedConfigChange, error) {
	return applyStagedByMode(ctx, store, serverID, storage.StagedApplyOnRestart)
}

// ApplyOnRestartStagedConfigs applies all "on_restart" staged changes for a
// server. Call this from the server start/restart path BEFORE the container
// is (re)created so the fresh container picks up the merged config, e.g.:
//
//	applied, err := scheduler.ApplyOnRestartStagedConfigs(ctx, store, server.ID)
//
// Container recreation itself stays the caller's responsibility (same split
// as ConfigService.UpdateServerConfig -> recreateContainer).
func ApplyOnRestartStagedConfigs(ctx context.Context, store stagedConfigStore, serverID string) (int, error) {
	records, err := ApplyOnRestartStagedConfigRecords(ctx, store, serverID)
	return len(records), err
}

// HasPendingRestartStagedConfigRecreate reports whether restart-only staged
// changes were committed after the server's last successful start. It closes
// the crash/retry window between applying DB changes and recreating the Docker
// container.
func HasPendingRestartStagedConfigRecreate(ctx context.Context, store stagedConfigStore, serverID string, lastStarted *time.Time) (bool, error) {
	changes, err := store.ListStagedConfigChanges(ctx, serverID, storage.StagedStatusApplied)
	if err != nil {
		return false, err
	}
	for _, c := range changes {
		if c.ApplyMode != storage.StagedApplyOnRestart || c.AppliedAt == nil {
			continue
		}
		if lastStarted == nil || c.AppliedAt.After(*lastStarted) {
			return true, nil
		}
	}
	return false, nil
}
