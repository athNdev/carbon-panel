// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/google/uuid"
)

const (
	opPhaseExecuting = storage.ServerOperationExecuting
	opPhaseComplete  = storage.ServerOperationComplete
	opPhaseFailed    = storage.ServerOperationFailed
)

// runJournaledMutation persists the intent of a Docker mutation before it is
// attempted, advances the journal through the attempt, and finalizes it.
//
// If the process dies between the external effect and finalization, the entry
// stays active and startup recovery observes the container before releasing the
// per-server lock (see internal/operation). A mutation error finalizes as failed
// so the user can retry without restarting the panel.
func (s *ServerService) runJournaledMutation(ctx context.Context, server *storage.Server, kind string, mutate func() error) error {
	digest := operationDigest(kind, server)
	op, _, err := s.store.BeginServerOperation(ctx, server.ID, server.NodeID, server.ContainerID, uuid.NewString(), kind, digest)
	if err != nil {
		// Failing to persist intent means we must not perform the mutation.
		return fmt.Errorf("record %s intent: %w", kind, err)
	}

	if err := s.store.AdvanceServerOperation(ctx, op.ID, op.Phase, opPhaseExecuting, ""); err != nil {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = s.store.AdvanceServerOperation(releaseCtx, op.ID, op.Phase, opPhaseFailed, "could not enter executing phase: "+err.Error())
		return fmt.Errorf("record %s attempt: %w", kind, err)
	}

	if mutateErr := mutate(); mutateErr != nil {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if advErr := s.store.AdvanceServerOperation(releaseCtx, op.ID, opPhaseExecuting, opPhaseFailed, mutateErr.Error()); advErr != nil {
			s.log.Error("Failed to finalize %s operation %s after error: %v", kind, op.ID, advErr)
		}
		return mutateErr
	}

	finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.store.AdvanceServerOperation(finalizeCtx, op.ID, opPhaseExecuting, opPhaseComplete, ""); err != nil {
		// The mutation succeeded. The entry stays active and startup recovery
		// observes the container before releasing the lock, rather than a retry
		// acting on an unknown outcome.
		s.log.Error("Failed to record completion of %s operation %s: %v", kind, op.ID, err)
	}
	return nil
}

func operationDigest(kind string, server *storage.Server) string {
	sum := sha256.Sum256([]byte(kind + "|" + server.ID + "|" + server.NodeID + "|" + server.ContainerID))
	return hex.EncodeToString(sum[:])
}
