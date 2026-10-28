// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package operation

import (
	"context"
	"errors"
	"testing"

	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

type fakeStore struct {
	ops        []db.ServerOperation
	listErr    error
	advanceErr error
	advances   []struct{ id, from, to, detail string }
}

func (f *fakeStore) ListActiveServerOperations(context.Context) ([]db.ServerOperation, error) {
	return f.ops, f.listErr
}

func (f *fakeStore) AdvanceServerOperation(_ context.Context, id, from, to, detail string) error {
	f.advances = append(f.advances, struct{ id, from, to, detail string }{id, from, to, detail})
	return f.advanceErr
}

type fakeObserver struct {
	observed map[string]Observed
	err      error
}

func (f *fakeObserver) Observe(_ context.Context, serverID, _ string) (Observed, error) {
	if f.err != nil {
		return Observed{}, f.err
	}
	return f.observed[serverID], nil
}

func TestRecoverInterruptedReleasesObservedOperations(t *testing.T) {
	store := &fakeStore{ops: []db.ServerOperation{
		{ID: "op-1", ServerID: "srv-running", Kind: "start", Phase: db.ServerOperationExecuting},
		{ID: "op-2", ServerID: "srv-absent", Kind: "stop", Phase: db.ServerOperationPrepared},
	}}
	observer := &fakeObserver{observed: map[string]Observed{
		"srv-running": {Exists: true, Running: true, ContainerID: "abcdef1234567890"},
		"srv-absent":  {Exists: false},
	}}

	summary, err := RecoverInterrupted(context.Background(), store, observer, logger.New())
	if err != nil {
		t.Fatalf("RecoverInterrupted: %v", err)
	}
	if summary.Scanned != 2 || summary.Recovered != 2 || summary.NeedsReview != 0 {
		t.Fatalf("summary = %+v, want 2 scanned, 2 recovered", summary)
	}
	if len(store.advances) != 2 {
		t.Fatalf("advances = %d, want 2", len(store.advances))
	}
	for _, advance := range store.advances {
		if advance.to != db.ServerOperationRecovered {
			t.Errorf("advance target = %q, want %q", advance.to, db.ServerOperationRecovered)
		}
	}
	if want := "running"; !contains(store.advances[0].detail, want) {
		t.Errorf("running detail %q does not mention %q", store.advances[0].detail, want)
	}
	if want := "absent"; !contains(store.advances[1].detail, want) {
		t.Errorf("absent detail %q does not mention %q", store.advances[1].detail, want)
	}
}

func TestRecoverInterruptedBackupNamesTheNextAction(t *testing.T) {
	store := &fakeStore{ops: []db.ServerOperation{
		{ID: "op-backup", ServerID: "srv-1", Kind: "backup", Phase: db.ServerOperationExecuting},
	}}
	observer := &fakeObserver{observed: map[string]Observed{
		"srv-1": {Exists: true, Running: true},
	}}

	summary, err := RecoverInterrupted(context.Background(), store, observer, logger.New())
	if err != nil {
		t.Fatalf("RecoverInterrupted: %v", err)
	}
	if summary.Recovered != 1 || len(store.advances) != 1 {
		t.Fatalf("summary = %+v, advances = %+v", summary, store.advances)
	}
	detail := store.advances[0].detail
	if !contains(detail, "run the backup again") || !contains(detail, "container running") {
		t.Fatalf("backup recovery detail = %q", detail)
	}
}

func TestRecoverInterruptedDeleteAndMigrationNameTheNextAction(t *testing.T) {
	store := &fakeStore{ops: []db.ServerOperation{
		{ID: "op-delete", ServerID: "srv-delete", Kind: "delete", Phase: db.ServerOperationExecuting},
		{ID: "op-migrate", ServerID: "srv-migrate", Kind: "migrate", Phase: db.ServerOperationExecuting},
	}}
	observer := &fakeObserver{observed: map[string]Observed{
		"srv-delete":  {Exists: false},
		"srv-migrate": {Exists: true, Running: false},
	}}

	summary, err := RecoverInterrupted(context.Background(), store, observer, logger.New())
	if err != nil {
		t.Fatalf("RecoverInterrupted: %v", err)
	}
	if summary.Recovered != 2 || len(store.advances) != 2 {
		t.Fatalf("summary = %+v, advances = %+v", summary, store.advances)
	}
	if !contains(store.advances[0].detail, "confirm the container and the world") {
		t.Fatalf("delete recovery detail = %q", store.advances[0].detail)
	}
	if !contains(store.advances[1].detail, "confirm which node holds the world") {
		t.Fatalf("migration recovery detail = %q", store.advances[1].detail)
	}
}

func TestRecoverInterruptedKeepsLockWhenObservationFails(t *testing.T) {
	store := &fakeStore{ops: []db.ServerOperation{
		{ID: "op-1", ServerID: "srv-1", Kind: "start", Phase: db.ServerOperationExecuting},
	}}
	observer := &fakeObserver{err: errors.New("node unreachable")}

	summary, err := RecoverInterrupted(context.Background(), store, observer, logger.New())
	if err != nil {
		t.Fatalf("RecoverInterrupted: %v", err)
	}
	if summary.NeedsReview != 1 || summary.Recovered != 0 {
		t.Fatalf("summary = %+v, want one operation needing review", summary)
	}
	if len(store.advances) != 0 {
		t.Fatalf("observed-state failure must not release the lock, advances = %+v", store.advances)
	}
}

func TestRecoverInterruptedKeepsLockWhenFinalizeFails(t *testing.T) {
	store := &fakeStore{
		ops:        []db.ServerOperation{{ID: "op-1", ServerID: "srv-1", Kind: "start", Phase: db.ServerOperationExecuting}},
		advanceErr: errors.New("phase changed"),
	}
	observer := &fakeObserver{observed: map[string]Observed{"srv-1": {Exists: true}}}

	summary, err := RecoverInterrupted(context.Background(), store, observer, logger.New())
	if err != nil {
		t.Fatalf("RecoverInterrupted: %v", err)
	}
	if summary.NeedsReview != 1 || summary.Recovered != 0 {
		t.Fatalf("summary = %+v, want one operation needing review", summary)
	}
}

func TestRecoverInterruptedListError(t *testing.T) {
	store := &fakeStore{listErr: errors.New("database unavailable")}
	if _, err := RecoverInterrupted(context.Background(), store, &fakeObserver{}, logger.New()); err == nil {
		t.Fatal("expected a list failure to be returned")
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
