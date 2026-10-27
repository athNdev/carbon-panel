// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package operation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/google/uuid"
)

// Journal is the operation journal subset that RunJournaled needs.
// *db.Store satisfies it.
type Journal interface {
	Store
	BeginServerOperation(ctx context.Context, serverID, nodeID, containerID, key, kind, digest string) (*db.ServerOperation, bool, error)
}

// RunJournaled persists the intent of a server mutation before it is
// attempted, advances the journal through the attempt, and finalizes it.
//
// If the process dies between the external effect and finalization, the entry
// stays active and startup recovery observes the container before releasing
// the per-server lock (see RecoverInterrupted). The active lock is the shared
// coordinator: no two lifecycle mutations of one world can interleave. A
// mutation error finalizes as failed so the user can retry without restarting
// the panel.
func RunJournaled(ctx context.Context, store Journal, log *logger.Logger, server *db.Server, kind string, mutate func() error) error {
	digest := Digest(kind, server)
	op, _, err := store.BeginServerOperation(ctx, server.ID, server.NodeID, server.ContainerID, uuid.NewString(), kind, digest)
	if err != nil {
		// Failing to persist intent means we must not perform the mutation.
		return fmt.Errorf("record %s intent: %w", kind, err)
	}

	if err := store.AdvanceServerOperation(ctx, op.ID, op.Phase, db.ServerOperationExecuting, ""); err != nil {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = store.AdvanceServerOperation(releaseCtx, op.ID, op.Phase, db.ServerOperationFailed, "could not enter executing phase: "+err.Error())
		return fmt.Errorf("record %s attempt: %w", kind, err)
	}

	if mutateErr := mutate(); mutateErr != nil {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if advErr := store.AdvanceServerOperation(releaseCtx, op.ID, db.ServerOperationExecuting, db.ServerOperationFailed, mutateErr.Error()); advErr != nil {
			log.Error("Failed to finalize %s operation %s after error: %v", kind, op.ID, advErr)
		}
		return mutateErr
	}

	finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := store.AdvanceServerOperation(finalizeCtx, op.ID, db.ServerOperationExecuting, db.ServerOperationComplete, ""); err != nil {
		// The mutation succeeded. The entry stays active and startup recovery
		// observes the container before releasing the lock, rather than a retry
		// acting on an unknown outcome.
		log.Error("Failed to record completion of %s operation %s: %v", kind, op.ID, err)
	}
	return nil
}

// Digest is the request payload fingerprint for a journaled mutation.
func Digest(kind string, server *db.Server) string {
	sum := sha256.Sum256([]byte(kind + "|" + server.ID + "|" + server.NodeID + "|" + server.ContainerID))
	return hex.EncodeToString(sum[:])
}
