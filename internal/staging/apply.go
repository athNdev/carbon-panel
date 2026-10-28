// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package staging

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/nodestorage"
	"github.com/athNdev/carbon-panel/internal/operation"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

// Publisher performs the one container mutation for a due batch.
type Publisher func(ctx context.Context, server *storage.Server, plan Plan) error

type fileSnap struct {
	target  string
	existed bool
	data    []byte
}

// Sweep updates idle clocks and publishes batches whose window has arrived.
// A lock held by another lifecycle mutation is left for the next sweep.
func Sweep(ctx context.Context, store *storage.Store, log *logger.Logger, now time.Time, players func(serverID string) (int, bool), publish Publisher) error {
	if store == nil {
		return nil
	}
	batches, err := store.ListOpenUpdateBatches(ctx)
	if err != nil {
		return err
	}
	var failed error
	for i := range batches {
		batch := batches[i]
		sample := PlayerSample{}
		if players != nil {
			count, known := players(batch.ServerID)
			sample = PlayerSample{Known: known, Count: count}
		}
		decision := Decide(now, batch, sample)
		if decision.SetIdle {
			if err := store.SetBatchIdleSince(ctx, batch.ID, decision.IdleSince); err != nil {
				failed = errors.Join(failed, err)
				continue
			}
		}
		if !decision.Due {
			continue
		}
		server, err := store.GetServer(ctx, batch.ServerID)
		if err != nil {
			failed = errors.Join(failed, err)
			continue
		}
		err = ApplyBatch(ctx, store, log, server, batch, publish)
		if lockHeld(err) {
			if log != nil {
				log.Warn("Staged batch %s for server %s waits because a lifecycle lock is held", batch.ID, server.ID)
			}
			continue
		}
		if err != nil {
			if log != nil {
				log.Error("Failed to apply staged batch %s for server %s: %v", batch.ID, server.ID, err)
			}
			failed = errors.Join(failed, err)
		}
	}
	return failed
}

// ApplyBatch publishes one due batch. Files and config are restored when the
// container mutation fails, and the batch stays open. A live config field that
// moved after staging stops the apply before anything is written.
func ApplyBatch(ctx context.Context, store *storage.Store, log *logger.Logger, server *storage.Server, batch storage.ServerUpdateBatch, publish Publisher) error {
	return operation.RunJournaled(ctx, store, log, server, "apply-batch", "", func() error {
		if err := store.StagedConfigConflict(ctx, server.ID, batch.ID); err != nil {
			return err
		}
		configs, err := store.ListBatchConfig(ctx, batch.ID)
		if err != nil {
			return err
		}
		content, err := store.ListBatchContent(ctx, batch.ID)
		if err != nil {
			return err
		}
		if err := stagedModuleConflict(ctx, store, server, content); err != nil {
			return err
		}
		var restore func() error
		if len(content) > 0 {
			acc, accErr := openAccessor(ctx, store, server)
			if accErr != nil {
				return accErr
			}
			snaps, commitErr := commitFiles(acc, content)
			_ = acc.Close()
			restore = func() error {
				again, openErr := openAccessor(ctx, store, server)
				if openErr != nil {
					return openErr
				}
				defer func() { _ = again.Close() }()
				return restoreFiles(again, snaps)
			}
			if commitErr != nil {
				if restore != nil {
					_ = restore()
				}
				return commitErr
			}
		}
		applied := make([]*storage.StagedConfigChange, 0, len(configs))
		for _, cfg := range configs {
			rec, applyErr := store.ApplyStagedConfigChange(ctx, cfg.ID)
			if applyErr != nil {
				_ = undoApply(ctx, store, restore, applied, nil)
				return applyErr
			}
			applied = append(applied, rec)
		}
		moduleIDs, undoModules, modErr := applyStagedModules(ctx, store, server, content)
		if modErr != nil {
			_ = undoApply(ctx, store, restore, applied, nil)
			return modErr
		}
		plan := PlanFor(server.Status, len(applied) > 0)
		plan.ModuleIDs = moduleIDs
		if plan.NeedsPublish() {
			if publish == nil {
				_ = undoApply(ctx, store, moduleRestore(undoModules, restore), applied, nil)
				return errors.New("staged batch needs a container publish")
			}
			if pubErr := publish(ctx, server, plan); pubErr != nil {
				_ = undoApply(ctx, store, moduleRestore(undoModules, restore), applied, nil)
				return pubErr
			}
		}
		for i := range content {
			if err := store.MarkStagedContentApplied(ctx, content[i].ID); err != nil {
				return err
			}
		}
		return store.CloseUpdateBatch(ctx, batch.ID, storage.UpdateBatchApplied)
	})
}

// CommitNextRestartFiles copies content for a next-restart batch onto the live
// tree. A batch waiting for a time or an idle window is left untouched, so an
// incidental restart does not publish it. recreateModule runs for a staged
// module whose container spec changed. The returned function puts the files
// and module fields back and returns the rows to staged.
func CommitNextRestartFiles(ctx context.Context, store *storage.Store, server *storage.Server, recreateModule func(context.Context, string) error) (func(), error) {
	batch, err := store.OpenUpdateBatch(ctx, server.ID)
	if err != nil || batch == nil || batch.Window != string(storage.UpdateWindowNextRestart) {
		return nil, err
	}
	content, err := store.ListBatchContent(ctx, batch.ID)
	if err != nil || len(content) == 0 {
		return nil, err
	}
	if err := stagedModuleConflict(ctx, store, server, content); err != nil {
		return nil, err
	}
	acc, err := openAccessor(ctx, store, server)
	if err != nil {
		return nil, err
	}
	snaps, err := commitFiles(acc, content)
	_ = acc.Close()
	restoreFilesNow := func() error {
		again, openErr := openAccessor(ctx, store, server)
		if openErr != nil {
			return openErr
		}
		defer func() { _ = again.Close() }()
		return restoreFiles(again, snaps)
	}
	if err != nil {
		_ = restoreFilesNow()
		return nil, err
	}
	moduleIDs, undoModules, err := applyStagedModules(ctx, store, server, content)
	if err != nil {
		_ = restoreFilesNow()
		return nil, err
	}
	for _, id := range moduleIDs {
		if recreateModule == nil {
			_ = moduleRestore(undoModules, restoreFilesNow)()
			return nil, errors.New("staged module needs a container recreate")
		}
		if err := recreateModule(ctx, id); err != nil {
			_ = moduleRestore(undoModules, restoreFilesNow)()
			return nil, err
		}
	}
	applied := make([]string, 0, len(content))
	for i := range content {
		if markErr := store.MarkStagedContentApplied(ctx, content[i].ID); markErr != nil {
			_ = undoApply(ctx, store, moduleRestore(undoModules, restoreFilesNow), nil, applied)
			return nil, markErr
		}
		applied = append(applied, content[i].ID)
	}
	return func() {
		_ = undoApply(ctx, store, moduleRestore(undoModules, restoreFilesNow), nil, applied)
	}, nil
}

// FinishNextRestartBatch closes a next-restart batch once nothing in it is
// still staged. Time and idle batches are left open.
func FinishNextRestartBatch(ctx context.Context, store *storage.Store, serverID string) error {
	batch, err := store.OpenUpdateBatch(ctx, serverID)
	if err != nil || batch == nil || batch.Window != string(storage.UpdateWindowNextRestart) {
		return err
	}
	content, err := store.ListBatchContent(ctx, batch.ID)
	if err != nil {
		return err
	}
	configs, err := store.ListBatchConfig(ctx, batch.ID)
	if err != nil {
		return err
	}
	if len(content) > 0 || len(configs) > 0 {
		return nil
	}
	return store.CloseUpdateBatch(ctx, batch.ID, storage.UpdateBatchApplied)
}

func moduleRestore(undoModules func() error, restore func() error) func() error {
	return func() error {
		var failed error
		if undoModules != nil {
			failed = undoModules()
		}
		if restore != nil {
			failed = errors.Join(failed, restore())
		}
		return failed
	}
}

func stagedModuleConflict(ctx context.Context, store *storage.Store, server *storage.Server, rows []storage.StagedContentChange) error {
	var moduleRows []storage.StagedContentChange
	for _, row := range rows {
		if row.Kind == storage.StagedKindModule {
			moduleRows = append(moduleRows, row)
		}
	}
	if len(moduleRows) == 0 {
		return nil
	}
	acc, err := openAccessor(ctx, store, server)
	if err != nil {
		return err
	}
	defer func() { _ = acc.Close() }()
	for _, row := range moduleRows {
		payload, err := readModulePayload(acc, row.StagePath)
		if err != nil {
			return err
		}
		module, err := store.GetModule(ctx, payload.ModuleID)
		if err != nil {
			return err
		}
		if module.ServerID != server.ID {
			return fmt.Errorf("staged module %s is not on this server", payload.ModuleID)
		}
		if err := checkModuleBase(module, payload.Fields); err != nil {
			return err
		}
	}
	return nil
}

func undoApply(ctx context.Context, store *storage.Store, restore func() error, configs []*storage.StagedConfigChange, contentIDs []string) error {
	var failed error
	if restore != nil {
		failed = restore()
	}
	for _, cfg := range configs {
		if _, err := store.RollbackStagedConfigChange(ctx, cfg.ID); err != nil {
			failed = errors.Join(failed, err)
		}
	}
	for _, id := range contentIDs {
		if err := store.MarkStagedContentStaged(ctx, id); err != nil {
			failed = errors.Join(failed, err)
		}
	}
	return failed
}

func commitFiles(acc nodestorage.Accessor, rows []storage.StagedContentChange) ([]fileSnap, error) {
	snaps := make([]fileSnap, 0, len(rows))
	for _, row := range rows {
		if row.Kind == storage.StagedKindModpack {
			more, err := snapshotModpack(acc, row)
			snaps = append(snaps, more...)
			if err != nil {
				return snaps, err
			}
			continue
		}
		if row.Kind == storage.StagedKindModule {
			continue
		}
		info, err := acc.Stat(row.TargetPath)
		if err != nil && !os.IsNotExist(err) {
			return snaps, err
		}
		if err == nil && info.IsDir() {
			return snaps, fmt.Errorf("staged path %s is a directory", row.TargetPath)
		}
		item := fileSnap{target: row.TargetPath}
		if err == nil {
			item.existed = true
			item.data, err = acc.ReadFile(row.TargetPath)
			if err != nil {
				return snaps, err
			}
		}
		snaps = append(snaps, item)
	}
	for _, row := range rows {
		if row.Kind == storage.StagedKindModpack {
			if err := writeModpack(acc, row); err != nil {
				_ = restoreFiles(acc, snaps)
				return snaps, err
			}
			continue
		}
		if row.Kind == storage.StagedKindModule {
			continue
		}
		switch row.Action {
		case storage.StagedActionDelete:
			if err := acc.Remove(row.TargetPath); err != nil && !os.IsNotExist(err) {
				_ = restoreFiles(acc, snaps)
				return snaps, err
			}
		default:
			data, err := acc.ReadFile(row.StagePath)
			if err != nil {
				_ = restoreFiles(acc, snaps)
				return snaps, err
			}
			if err := acc.WriteFile(row.TargetPath, data); err != nil {
				_ = restoreFiles(acc, snaps)
				return snaps, err
			}
		}
	}
	return snaps, nil
}

func restoreFiles(acc nodestorage.Accessor, snaps []fileSnap) error {
	var failed error
	for i := len(snaps) - 1; i >= 0; i-- {
		item := snaps[i]
		if item.existed {
			if err := acc.WriteFile(item.target, item.data); err != nil {
				failed = errors.Join(failed, err)
			}
			continue
		}
		if err := acc.Remove(item.target); err != nil && !os.IsNotExist(err) {
			failed = errors.Join(failed, err)
		}
	}
	return failed
}

func openAccessor(ctx context.Context, store *storage.Store, server *storage.Server) (nodestorage.Accessor, error) {
	if nodestorage.IsLegacyLocalNodeID(server.NodeID) {
		return nodestorage.NewLocal(server.DataPath)
	}
	node, err := store.GetNode(ctx, server.NodeID)
	if err != nil {
		return nil, err
	}
	return nodestorage.ForNode(node.StorageInfo(), server.DataPath)
}

func lockHeld(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, operation.ErrInProgress) {
		return true
	}
	return strings.Contains(strings.ToUpper(err.Error()), "UNIQUE")
}
