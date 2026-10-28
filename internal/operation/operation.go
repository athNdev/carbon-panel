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
			summary.ReviewIDs = append(summary.ReviewIDs, op.ID)
			log.Error("Interrupted %s operation %s for server %s requires manual recovery: cannot observe container: %v", op.Kind, op.ID, op.ServerID, observeErr)
			continue
		}

		detail := describeObservation(observed)
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
