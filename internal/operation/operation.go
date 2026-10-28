// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Package operation recovers server mutations that were interrupted by a panel
// crash or restart. A disconnected Docker request has an unknown outcome: the
// operation journal keeps the server locked until startup has observed the
// container, instead of allowing a blind retry.
package operation

import (
	"context"
	"fmt"

	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

// Store is the journal subset that recovery needs. *db.Store satisfies it.
type Store interface {
	ListActiveServerOperations(ctx context.Context) ([]db.ServerOperation, error)
	AdvanceServerOperation(ctx context.Context, id, from, to, detail string) error
	NoteActiveServerOperation(ctx context.Context, id, detail string) error
}

// Observed is the container state recovery sees for an interrupted operation.
type Observed struct {
	Exists      bool
	Running     bool
	ContainerID string
}

// Observer resolves the current container state for a server. Implementations
// must return an error when the state cannot be established (for example the
// node is unreachable); recovery treats that as "leave the lock in place".
type Observer interface {
	Observe(ctx context.Context, serverID, containerID string) (Observed, error)
}

// Summary reports what startup recovery did.
type Summary struct {
	Scanned     int
	Recovered   int
	NeedsReview int
	// ReviewIDs are the active operations whose container state could not be
	// established. A later pass may observe them once the daemon is reachable.
	ReviewIDs []string
}

// RecoverInterrupted observes every operation that still owns a server lock and
// finalizes it when the container state is known. Operations whose state cannot be
// observed keep their lock and are reported as requiring review; their server stays
// blocked until an operator or a later startup resolves them.
func RecoverInterrupted(ctx context.Context, store Store, observer Observer, log *logger.Logger) (Summary, error) {
	ops, err := store.ListActiveServerOperations(ctx)
	if err != nil {
		return Summary{}, fmt.Errorf("list interrupted server operations: %w", err)
	}

	summary := Summary{Scanned: len(ops)}
	for _, op := range ops {
		observed, observeErr := observer.Observe(ctx, op.ServerID, op.ContainerID)
		if observeErr != nil {
			summary.NeedsReview++
			if keepsWorldLock(op.Kind) || op.Kind == "backup" {
				detail := nextAction(op.Kind) + " Container state could not be read."
				if op.Kind == "backup" {
					detail = nextAction("backup") + " Container state could not be read, so save-on was not sent."
				}
				noteHeldWorld(ctx, store, log, op, detail)
				continue
			}
			summary.ReviewIDs = append(summary.ReviewIDs, op.ID)
			log.Error("Interrupted %s operation %s for server %s requires manual recovery: cannot observe container: %v", op.Kind, op.ID, op.ServerID, observeErr)
			continue
		}

		detail := recoveryDetail(op.Kind, observed)
		if op.Kind == "backup" && observed.Running {
			// save-off is a live JVM command. Releasing this lock while the
			// container is still running would leave saves off until something
			// else sends save-on. ResumeInterruptedBackups does that once a
			// command sender exists; until then the lock stays.
			noteHeldWorld(ctx, store, log, op, nextAction("backup")+" World saves may still be off; save-on has not been sent.")
			summary.NeedsReview++
			continue
		}
		if keepsWorldLock(op.Kind) {
			noteHeldWorld(ctx, store, log, op, detail)
			summary.NeedsReview++
			continue
		}
		if err := store.AdvanceServerOperation(ctx, op.ID, op.Phase, db.ServerOperationRecovered, detail); err != nil {
			summary.NeedsReview++
			summary.ReviewIDs = append(summary.ReviewIDs, op.ID)
			log.Error("Interrupted %s operation %s for server %s requires manual recovery: %v", op.Kind, op.ID, op.ServerID, err)
			continue
		}

		summary.Recovered++
		log.Warn("Recovered interrupted %s operation %s for server %s: %s", op.Kind, op.ID, op.ServerID, detail)
	}

	return summary, nil
}

// SaveResumer turns world saves back on after an interrupted backup. save-off
// does not survive a stopped JVM, so callers send it only while the container
// is still running.
type SaveResumer interface {
	ResumeWorldSaves(ctx context.Context, serverID string) error
}

// ResumeInterruptedBackups sends save-on for an interrupted backup whose
// container is still running, then releases that lock. A failed save-on leaves
// the lock in place. A stopped container is released without a command,
// because the next boot starts with saves enabled and this pass must not
// start the server.
func ResumeInterruptedBackups(ctx context.Context, store Store, observer Observer, saves SaveResumer, log *logger.Logger) (Summary, error) {
	ops, err := store.ListActiveServerOperations(ctx)
	if err != nil {
		return Summary{}, fmt.Errorf("list interrupted server operations: %w", err)
	}

	summary := Summary{}
	for _, op := range ops {
		if op.Kind != "backup" {
			continue
		}
		summary.Scanned++
		observed, observeErr := observer.Observe(ctx, op.ServerID, op.ContainerID)
		if observeErr != nil {
			summary.NeedsReview++
			noteHeldWorld(ctx, store, log, op, nextAction("backup")+" Container state could not be read, so save-on was not sent.")
			continue
		}
		if observed.Running {
			if saves == nil {
				summary.NeedsReview++
				noteHeldWorld(ctx, store, log, op, nextAction("backup")+" World saves may still be off; save-on has not been sent.")
				continue
			}
			if saveErr := saves.ResumeWorldSaves(ctx, op.ServerID); saveErr != nil {
				summary.NeedsReview++
				noteHeldWorld(ctx, store, log, op, "world saves may still be off; run save-on or restart the server. "+saveErr.Error())
				continue
			}
		}
		detail := recoveryDetail(op.Kind, observed)
		if err := store.AdvanceServerOperation(ctx, op.ID, op.Phase, db.ServerOperationRecovered, detail); err != nil {
			summary.NeedsReview++
			if log != nil {
				log.Error("Interrupted backup %s for server %s requires manual recovery: %v", op.ID, op.ServerID, err)
			}
			continue
		}
		summary.Recovered++
		if log != nil {
			log.Warn("Recovered interrupted backup %s for server %s: %s", op.ID, op.ServerID, detail)
		}
	}
	return summary, nil
}

// keepsWorldLock reports operations whose container state does not prove the
// world is safe to start. Those locks stay until an operator resolves them.
func keepsWorldLock(kind string) bool {
	switch kind {
	case "delete", "restore", "migrate":
		return true
	default:
		return false
	}
}

func noteHeldWorld(ctx context.Context, store Store, log *logger.Logger, op db.ServerOperation, detail string) {
	if err := store.NoteActiveServerOperation(ctx, op.ID, detail); err != nil && log != nil {
		log.Error("Interrupted %s operation %s for server %s stays locked, and the next action could not be recorded: %v", op.Kind, op.ID, op.ServerID, err)
		return
	}
	if log != nil {
		log.Warn("Interrupted %s operation %s for server %s stays locked until an operator resolves it: %s", op.Kind, op.ID, op.ServerID, detail)
	}
}

func nextAction(kind string) string {
	switch kind {
	case "backup":
		return "backup was interrupted before the archive was finalized; run the backup again."
	case "restore":
		return "restore was interrupted; confirm the world before starting the server."
	case "delete":
		return "delete was interrupted; confirm the container and the world before trying again."
	case "migrate":
		return "migration was interrupted; confirm which node holds the world before starting the server."
	default:
		return ""
	}
}

func recoveryDetail(kind string, observed Observed) string {
	state := describeObservation(observed)
	if action := nextAction(kind); action != "" {
		return action + " " + state
	}
	return state
}

func describeObservation(observed Observed) string {
	state := "container absent"
	if observed.Exists {
		state = "container stopped"
		if observed.Running {
			state = "container running"
		}
		if observed.ContainerID != "" {
			state = fmt.Sprintf("%s (%s)", state, shortID(observed.ContainerID))
		}
	}
	return state + "; intent outcome observed at startup and reconciliation will converge status"
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
