// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package operation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/google/uuid"
)

// ErrInProgress is returned when the same request id is already running.
var ErrInProgress = errors.New("matching lifecycle operation is already in progress")

// ErrAlreadyFailed is returned when the same request id already failed.
// A new request id is required to try again.
var ErrAlreadyFailed = errors.New("matching lifecycle operation already failed")

// ValidateRequestID rejects client request ids that do not fit the journal.
func ValidateRequestID(id string) error {
	if len(strings.TrimSpace(id)) > 128 {
		return errors.New("request_id must be at most 128 characters")
	}
	return nil
}

// StorageKey is the journal key for a client request id and mutation kind.
// An empty request id is unique to this call, so it is not idempotent.
func StorageKey(requestID, kind string) string {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return uuid.NewString()
	}
	return requestID + ":" + kind
}

// Journal is the operation journal subset that RunJournaled needs.
// *db.Store satisfies it.
type Journal interface {
	Store
	BeginServerOperation(ctx context.Context, serverID, nodeID, containerID, key, kind, digest string) (*db.ServerOperation, bool, error)
}

// RunJournaled persists the intent of a server mutation before it is
// attempted, advances the journal through the attempt, and finalizes it.
// requestID is a client idempotency key: a repeat of a completed key does
// not run mutate again, and a repeat of a key that is still running returns
// ErrInProgress. An empty requestID is unique to this call.
//
// If the process dies between the external effect and finalization, the entry
// stays active and startup recovery observes the container before releasing
// the per-server lock (see RecoverInterrupted). The active lock is the shared
// coordinator: no two lifecycle mutations of one world can interleave. A
// mutation error finalizes as failed so the user can retry without restarting
// the panel.
func RunJournaled(ctx context.Context, store Journal, log *logger.Logger, server *db.Server, kind, requestID string, mutate func() error) error {
	if err := ValidateRequestID(requestID); err != nil {
		return err
	}
	digest := Digest(kind, server)
	op, reused, err := store.BeginServerOperation(ctx, server.ID, server.NodeID, server.ContainerID, StorageKey(requestID, kind), kind, digest)
	if err != nil {
		// Failing to persist intent means we must not perform the mutation.
		return fmt.Errorf("record %s intent: %w", kind, err)
	}
	if reused {
		switch op.Phase {
		case db.ServerOperationComplete:
			return nil
		case db.ServerOperationFailed:
			if op.LastError != "" {
				return fmt.Errorf("%w: %s", ErrAlreadyFailed, op.LastError)
			}
			return ErrAlreadyFailed
		case db.ServerOperationPrepared, db.ServerOperationExecuting:
			return fmt.Errorf("%w: %s", ErrInProgress, op.ID)
		default:
			return fmt.Errorf("lifecycle operation %s is in phase %s; retry with a new request_id", op.ID, op.Phase)
		}
	}

	if err := store.AdvanceServerOperation(ctx, op.ID, op.Phase, db.ServerOperationExecuting, ""); err != nil {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = store.AdvanceServerOperation(releaseCtx, op.ID, op.Phase, db.ServerOperationFailed, "could not enter executing phase: "+err.Error())
		return fmt.Errorf("record %s attempt: %w", kind, err)
	}
	return finishMutation(ctx, store, log, op.ID, kind, mutate)
}

func finishMutation(ctx context.Context, store Journal, log *logger.Logger, id, kind string, mutate func() error) error {
	if mutateErr := mutate(); mutateErr != nil {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if advErr := store.AdvanceServerOperation(releaseCtx, id, db.ServerOperationExecuting, db.ServerOperationFailed, mutateErr.Error()); advErr != nil {
			if log != nil {
				log.Error("Failed to finalize %s operation %s after error: %v", kind, id, advErr)
			}
		}
		return mutateErr
	}

	finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := store.AdvanceServerOperation(finalizeCtx, id, db.ServerOperationExecuting, db.ServerOperationComplete, ""); err != nil {
		// The mutation succeeded. The entry stays active and startup recovery
		// observes the container before releasing the lock, rather than a retry
		// acting on an unknown outcome.
		if log != nil {
			log.Error("Failed to record completion of %s operation %s: %v", kind, id, err)
		}
	}
	return nil
}

// Digest is the request payload fingerprint for a journaled mutation.
func Digest(kind string, server *db.Server) string {
	sum := sha256.Sum256([]byte(kind + "|" + server.ID + "|" + server.NodeID + "|" + server.ContainerID))
	return hex.EncodeToString(sum[:])
}
