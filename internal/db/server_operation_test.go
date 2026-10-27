// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package db

import (
	"context"
	"testing"
)

func TestServerOperationJournalLifecycle(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	op, reused, err := store.BeginServerOperation(ctx, "srv-1", "node-1", "cont-1", "key-1", "start", "digest-1")
	if err != nil {
		t.Fatalf("BeginServerOperation: %v", err)
	}
	if reused {
		t.Fatal("first begin must not report reuse")
	}
	if op.Phase != ServerOperationPrepared || op.ActiveServer == nil {
		t.Fatalf("new operation = phase %q active %v, want prepared and active", op.Phase, op.ActiveServer)
	}

	// The same request key and payload resolves to the same operation.
	again, reused, err := store.BeginServerOperation(ctx, "srv-1", "node-1", "cont-1", "key-1", "start", "digest-1")
	if err != nil {
		t.Fatalf("repeated BeginServerOperation: %v", err)
	}
	if !reused || again.ID != op.ID {
		t.Fatalf("repeat = (%s, reused=%v), want (%s, reused=true)", again.ID, reused, op.ID)
	}

	// A reused key with a different payload is a conflict, not a second operation.
	if _, _, err := store.BeginServerOperation(ctx, "srv-1", "node-1", "cont-1", "key-1", "start", "digest-2"); err == nil {
		t.Fatal("expected a changed payload with the same request key to conflict")
	}

	// One server can only have one active operation.
	if _, _, err := store.BeginServerOperation(ctx, "srv-1", "node-1", "cont-1", "key-2", "stop", "digest-3"); err == nil {
		t.Fatal("expected a second active operation for the same server to be rejected")
	}

	// The active operation blocks the server until it is finalized.
	if err := store.AssertNoServerOperation(ctx, "srv-1"); err == nil {
		t.Fatal("expected an active operation to be reported")
	}
	active, err := store.ListActiveServerOperations(ctx)
	if err != nil {
		t.Fatalf("ListActiveServerOperations: %v", err)
	}
	if len(active) != 1 || active[0].ID != op.ID {
		t.Fatalf("active operations = %+v, want just %s", active, op.ID)
	}

	if err := store.AdvanceServerOperation(ctx, op.ID, ServerOperationPrepared, ServerOperationExecuting, ""); err != nil {
		t.Fatalf("advance to executing: %v", err)
	}
	if err := store.AdvanceServerOperation(ctx, op.ID, ServerOperationExecuting, ServerOperationComplete, ""); err != nil {
		t.Fatalf("advance to complete: %v", err)
	}

	if err := store.AssertNoServerOperation(ctx, "srv-1"); err != nil {
		t.Fatalf("completed operation still blocks the server: %v", err)
	}
	active, err = store.ListActiveServerOperations(ctx)
	if err != nil {
		t.Fatalf("ListActiveServerOperations after completion: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("active operations after completion = %+v, want none", active)
	}
}

func TestAdvanceServerOperationRejectsStalePhase(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	op, _, err := store.BeginServerOperation(ctx, "srv-2", "node-1", "", "key-1", "start", "digest")
	if err != nil {
		t.Fatalf("BeginServerOperation: %v", err)
	}

	// The caller must advance from the phase it believes the operation is in;
	// a stale from-phase is rejected so two attempts cannot both finalize.
	if err := store.AdvanceServerOperation(ctx, op.ID, ServerOperationExecuting, ServerOperationComplete, ""); err == nil {
		t.Fatal("expected a stale from-phase to be rejected")
	}
	if err := store.AdvanceServerOperation(ctx, op.ID, ServerOperationPrepared, ServerOperationExecuting, ""); err != nil {
		t.Fatalf("advance from the current phase: %v", err)
	}
}
