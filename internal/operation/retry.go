// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package operation

import (
	"context"
	"time"

	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

// ReviewStore is the journal subset used to retry locks that could not be
// observed at startup. *db.Store satisfies it.
type ReviewStore interface {
	GetServerOperation(ctx context.Context, id string) (*db.ServerOperation, error)
	AdvanceServerOperation(ctx context.Context, id, from, to, detail string) error
}

// RetryReview re-observes operations whose container state was unknown at
// startup. It stops when every id has been released or ctx is cancelled.
// Operations that are not in the list are left alone, so an in-flight
// mutation is never finalized by this loop.
func RetryReview(ctx context.Context, store ReviewStore, observer Observer, log *logger.Logger, ids []string, every time.Duration) {
	if len(ids) == 0 {
		return
	}
	if every <= 0 {
		every = 15 * time.Second
	}
	pending := append([]string(nil), ids...)
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for len(pending) > 0 {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pending = retryOnce(ctx, store, observer, log, pending)
		}
	}
}

func retryOnce(ctx context.Context, store ReviewStore, observer Observer, log *logger.Logger, ids []string) []string {
	var still []string
	for _, id := range ids {
		if ctx.Err() != nil {
			still = append(still, id)
			continue
		}
		op, err := store.GetServerOperation(ctx, id)
		if err != nil || op == nil {
			// A lookup failure leaves the lock in place. Keep retrying so a
			// transient database error does not drop the review until restart.
			still = append(still, id)
			continue
		}
		if op.ActiveServer == nil {
			continue
		}
		if keepsWorldLock(op.Kind) {
			if log != nil {
				log.Warn("Interrupted %s operation %s for server %s stays locked until an operator resolves it", op.Kind, op.ID, op.ServerID)
			}
			continue
		}
		observed, observeErr := observer.Observe(ctx, op.ServerID, op.ContainerID)
		if observeErr != nil {
			still = append(still, id)
			continue
		}
		detail := describeObservation(observed) + "; observed after the daemon became reachable"
		if err := store.AdvanceServerOperation(ctx, op.ID, op.Phase, db.ServerOperationRecovered, detail); err != nil {
			still = append(still, id)
			if log != nil {
				log.Error("Interrupted %s operation %s for server %s still requires review: %v", op.Kind, op.ID, op.ServerID, err)
			}
			continue
		}
		if log != nil {
			log.Warn("Recovered interrupted %s operation %s for server %s after the daemon became reachable: %s", op.Kind, op.ID, op.ServerID, detail)
		}
	}
	return still
}
