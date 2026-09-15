package scheduler

import (
	"context"
	"time"

	storage "github.com/athNdev/carbon-panel/internal/db"
)

// applyDueStagedConfigs applies staged config changes whose cron schedule has
// fired (one-shot semantics: each record applies once, then flips to
// "applied"). It runs on every scheduler tick before task dispatch so
// scheduled rollouts land without operator intervention. Failures are logged
// per record and never abort the tick.
//
// Wiring: called from checkAndRunDueTasks.
func (s *Scheduler) applyDueStagedConfigs(ctx context.Context) {
	due, err := s.store.ListDueScheduledStagedChanges(ctx, time.Now())
	if err != nil {
		s.log.Error("Failed to list due staged config changes: %v", err)
		return
	}
	for _, staged := range due {
		applied, err := s.store.ApplyStagedConfigChange(ctx, staged.ID)
		if err != nil {
			s.log.Error("Failed to apply staged config change %s for server %s: %v", staged.ID, staged.ServerID, err)
			continue
		}
		s.log.Info("Applied scheduled staged config change %s to server %s", applied.ID, applied.ServerID)
	}
}

// stagedConfigStore is the subset of storage.Store used for staged rollouts,
// kept as an interface so the restart hook is unit-testable without a DB.
type stagedConfigStore interface {
	ListStagedConfigChanges(ctx context.Context, serverID string, status storage.StagedStatus) ([]*storage.StagedConfigChange, error)
	ApplyStagedConfigChange(ctx context.Context, id string) (*storage.StagedConfigChange, error)
}

func applyStagedByMode(ctx context.Context, store stagedConfigStore, serverID string, mode storage.StagedApplyMode) (int, error) {
	changes, err := store.ListStagedConfigChanges(ctx, serverID, storage.StagedStatusStaged)
	if err != nil {
		return 0, err
	}
	applied := 0
	for _, c := range changes {
		if c.ApplyMode != mode {
			continue
		}
		if _, err := store.ApplyStagedConfigChange(ctx, c.ID); err != nil {
			return applied, err
		}
		applied++
	}
	return applied, nil
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
	return applyStagedByMode(ctx, store, serverID, storage.StagedApplyOnRestart)
}
