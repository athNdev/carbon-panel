// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package operation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

type reviewFake struct {
	op       *db.ServerOperation
	getErr   error
	advances []struct{ id, from, to, detail string }
}

func (f *reviewFake) GetServerOperation(context.Context, string) (*db.ServerOperation, error) {
	if f.getErr != nil {
		err := f.getErr
		f.getErr = nil
		return nil, err
	}
	if f.op == nil {
		return nil, errors.New("missing")
	}
	copy := *f.op
	return &copy, nil
}

func (f *reviewFake) AdvanceServerOperation(_ context.Context, id, from, to, detail string) error {
	f.advances = append(f.advances, struct{ id, from, to, detail string }{id, from, to, detail})
	f.op.Phase = to
	f.op.ActiveServer = nil
	return nil
}

type flakyObserver struct {
	fails    int
	observed Observed
}

func (f *flakyObserver) Observe(context.Context, string, string) (Observed, error) {
	if f.fails > 0 {
		f.fails--
		return Observed{}, errors.New("node unreachable")
	}
	return f.observed, nil
}

func TestRetryReviewLeavesABackupLockHeld(t *testing.T) {
	serverID := "srv"
	store := &reviewFake{op: &db.ServerOperation{
		ID: "op-backup", ServerID: serverID, Kind: "backup", Phase: db.ServerOperationExecuting, ActiveServer: &serverID,
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		RetryReview(ctx, store, &flakyObserver{observed: Observed{Exists: true, Running: true}}, logger.New(), []string{"op-backup"}, 10*time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("retry did not finish")
	}
	if len(store.advances) != 0 || store.op.ActiveServer == nil {
		t.Fatalf("backup lock was released: advances=%+v active=%v", store.advances, store.op.ActiveServer)
	}
}

func TestRetryReviewLeavesADeleteLockHeld(t *testing.T) {
	serverID := "srv"
	store := &reviewFake{op: &db.ServerOperation{
		ID: "op-delete", ServerID: serverID, Kind: "delete", Phase: db.ServerOperationExecuting, ActiveServer: &serverID,
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		RetryReview(ctx, store, &flakyObserver{observed: Observed{Exists: false}}, logger.New(), []string{"op-delete"}, 10*time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("retry did not finish")
	}
	if len(store.advances) != 0 {
		t.Fatalf("delete lock was released: %+v", store.advances)
	}
	if store.op.ActiveServer == nil {
		t.Fatal("delete lock was cleared")
	}
}

func TestRetryReviewReleasesWhenObservationSucceeds(t *testing.T) {
	serverID := "srv"
	store := &reviewFake{op: &db.ServerOperation{
		ID: "op-1", ServerID: serverID, Kind: "start", Phase: db.ServerOperationExecuting, ActiveServer: &serverID,
	}}
	obs := &flakyObserver{fails: 1, observed: Observed{Exists: true, Running: true, ContainerID: "abc"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		RetryReview(ctx, store, obs, logger.New(), []string{"op-1"}, 10*time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("retry did not finish")
	}
	if len(store.advances) != 1 || store.advances[0].to != db.ServerOperationRecovered {
		t.Fatalf("advances = %+v", store.advances)
	}
}

func TestRetryReviewKeepsIDWhenLookupFails(t *testing.T) {
	serverID := "srv"
	store := &reviewFake{
		getErr: errors.New("database locked"),
		op: &db.ServerOperation{
			ID: "op-1", ServerID: serverID, Kind: "start", Phase: db.ServerOperationExecuting, ActiveServer: &serverID,
		},
	}
	obs := &flakyObserver{observed: Observed{Exists: true, Running: false, ContainerID: "abc"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		RetryReview(ctx, store, obs, logger.New(), []string{"op-1"}, 10*time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("retry did not finish after the lookup recovered")
	}
	if len(store.advances) != 1 {
		t.Fatalf("advances = %+v", store.advances)
	}
}

func TestRecoverInterruptedRecordsReviewIDs(t *testing.T) {
	store := &fakeStore{ops: []db.ServerOperation{
		{ID: "op-1", ServerID: "srv-1", Kind: "start", Phase: db.ServerOperationExecuting},
	}}
	summary, err := RecoverInterrupted(context.Background(), store, &fakeObserver{err: errors.New("down")}, logger.New())
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.ReviewIDs) != 1 || summary.ReviewIDs[0] != "op-1" {
		t.Fatalf("ReviewIDs = %v", summary.ReviewIDs)
	}
}
