// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestResolveServerOperationReleasesLockWithoutDeletingHistory(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	op, _, err := store.BeginServerOperation(ctx, "srv-1", "node-1", "cont-1", "key-1", "start", "digest")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ResolveServerOperation(ctx, op.ID, "operator confirmed the container is stopped"); err != nil {
		t.Fatal(err)
	}
	if err := store.AssertNoServerOperation(ctx, "srv-1"); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetServerOperation(ctx, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != ServerOperationRecovered || got.ActiveServer != nil {
		t.Fatalf("resolved op = %+v", got)
	}
	if !strings.Contains(got.LastError, "operator confirmed") {
		t.Fatalf("note = %q", got.LastError)
	}
	if err := store.ResolveServerOperation(ctx, op.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.ResolveServerOperation(ctx, "missing", ""); err == nil {
		t.Fatal("expected a missing operation to fail")
	}
}

func TestPruneServerOperationsKeepsActiveLocks(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	finished, _, err := store.BeginServerOperation(ctx, "srv-old", "node", "", "key-old", "stop", "digest")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AdvanceServerOperation(ctx, finished.ID, ServerOperationPrepared, ServerOperationExecuting, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.AdvanceServerOperation(ctx, finished.ID, ServerOperationExecuting, ServerOperationComplete, ""); err != nil {
		t.Fatal(err)
	}
	active, _, err := store.BeginServerOperation(ctx, "srv-live", "node", "", "key-live", "start", "digest")
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-31 * 24 * time.Hour)
	if err := store.db.Model(&ServerOperation{}).Where("id IN ?", []string{finished.ID, active.ID}).UpdateColumn("updated_at", past).Error; err != nil {
		t.Fatal(err)
	}

	pruned, err := store.PruneServerOperations(ctx, time.Now().UTC().Add(-ServerOperationRetention))
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 1 {
		t.Fatalf("pruned = %d, want 1", pruned)
	}
	if _, err := store.GetServerOperation(ctx, finished.ID); err == nil {
		t.Fatal("finished operation was kept")
	}
	if _, err := store.GetServerOperation(ctx, active.ID); err != nil {
		t.Fatalf("active operation was pruned: %v", err)
	}
}

func TestClaimIdempotencyKeyReturnsOriginalServer(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	owner, claimed, err := store.ClaimIdempotencyKey(ctx, "req-1", "srv-1", "create")
	if err != nil || !claimed || owner != "srv-1" {
		t.Fatalf("first claim = %q claimed=%v err=%v", owner, claimed, err)
	}
	owner, claimed, err = store.ClaimIdempotencyKey(ctx, "req-1", "srv-2", "create")
	if err != nil || claimed || owner != "srv-1" {
		t.Fatalf("repeat claim = %q claimed=%v err=%v", owner, claimed, err)
	}
	if _, _, err := store.ClaimIdempotencyKey(ctx, "req-1", "srv-2", "start"); !errors.Is(err, ErrRequestIDReused) {
		t.Fatalf("kind mismatch = %v", err)
	}
	owner, claimed, err = store.ClaimIdempotencyKey(ctx, "", "srv-3", "create")
	if err != nil || !claimed || owner != "srv-3" {
		t.Fatalf("empty claim = %q claimed=%v err=%v", owner, claimed, err)
	}
	if err := store.ReleaseIdempotencyKey(ctx, "req-1"); err != nil {
		t.Fatal(err)
	}
	owner, claimed, err = store.ClaimIdempotencyKey(ctx, "req-1", "srv-9", "create")
	if err != nil || !claimed || owner != "srv-9" {
		t.Fatalf("reclaim = %q claimed=%v err=%v", owner, claimed, err)
	}
}

func TestIdempotencyTableIsAddedOnUpgrade(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	cfg := store.cfg
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := sql.Open("sqlite", cfg.Database.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("DROP TABLE IF EXISTS idempotency_keys"); err != nil {
		_ = raw.Close()
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := NewSQLiteStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = upgraded.Close() }()
	if _, claimed, err := upgraded.ClaimIdempotencyKey(ctx, "after-upgrade", "srv", "create"); err != nil || !claimed {
		t.Fatalf("claim after upgrade: claimed=%v err=%v", claimed, err)
	}
}
