// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package operation

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

func openJournalStore(t *testing.T) *db.Store {
	t.Helper()
	store, err := db.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{
			Path:        filepath.Join(t.TempDir(), "journal.db"),
			AutoMigrate: true,
		},
		Docker: config.DockerConfig{Host: "unix:///var/run/docker.sock"},
	})
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestRunJournaledReplaysCompletedRequest(t *testing.T) {
	store := openJournalStore(t)
	server := &db.Server{ID: "srv", NodeID: "node", ContainerID: "cont"}
	calls := 0
	mutate := func() error {
		calls++
		return nil
	}
	ctx := context.Background()
	if err := RunJournaled(ctx, store, logger.New(), server, "start", "req-1", mutate); err != nil {
		t.Fatal(err)
	}
	if err := RunJournaled(ctx, store, logger.New(), server, "start", "req-1", mutate); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("mutate calls = %d, want 1", calls)
	}
}

func TestRunJournaledDoesNotReplayFailedRequest(t *testing.T) {
	store := openJournalStore(t)
	server := &db.Server{ID: "srv", NodeID: "node", ContainerID: "cont"}
	calls := 0
	mutate := func() error {
		calls++
		return errors.New("disk full")
	}
	ctx := context.Background()
	err := RunJournaled(ctx, store, logger.New(), server, "start", "req-fail", mutate)
	if err == nil || err.Error() != "disk full" {
		t.Fatalf("first error = %v", err)
	}
	err = RunJournaled(ctx, store, logger.New(), server, "start", "req-fail", mutate)
	if !errors.Is(err, ErrAlreadyFailed) {
		t.Fatalf("replay error = %v, want ErrAlreadyFailed", err)
	}
	if calls != 1 {
		t.Fatalf("mutate calls = %d, want 1", calls)
	}
}

func TestRunJournaledInProgressDoesNotStartSecondMutation(t *testing.T) {
	store := openJournalStore(t)
	server := &db.Server{ID: "srv", NodeID: "node", ContainerID: "cont"}
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	go func() {
		_ = RunJournaled(context.Background(), store, logger.New(), server, "stop", "req-live", func() error {
			close(started)
			<-release
			return nil
		})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("mutation did not start")
	}
	err := RunJournaled(context.Background(), store, logger.New(), server, "stop", "req-live", func() error {
		t.Fatal("second mutation ran")
		return nil
	})
	if !errors.Is(err, ErrInProgress) {
		t.Fatalf("second call = %v, want ErrInProgress", err)
	}
}

func TestRunJournaledEmptyRequestIDIsNotIdempotent(t *testing.T) {
	store := openJournalStore(t)
	server := &db.Server{ID: "srv", NodeID: "node", ContainerID: "cont"}
	calls := 0
	for i := 0; i < 2; i++ {
		if err := RunJournaled(context.Background(), store, logger.New(), server, "start", "", func() error {
			calls++
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("mutate calls = %d, want 2", calls)
	}
}

func TestRunJournaledSameRequestIDDifferentKindsBothRun(t *testing.T) {
	store := openJournalStore(t)
	server := &db.Server{ID: "srv", NodeID: "node", ContainerID: "cont"}
	var kinds []string
	for _, kind := range []string{"stop", "start"} {
		kind := kind
		if err := RunJournaled(context.Background(), store, logger.New(), server, kind, "req-shared", func() error {
			kinds = append(kinds, kind)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if len(kinds) != 2 || kinds[0] != "stop" || kinds[1] != "start" {
		t.Fatalf("kinds = %v", kinds)
	}
}

func TestValidateRequestIDRejectsOversizedKey(t *testing.T) {
	id := strings.Repeat("a", 129)
	if err := ValidateRequestID(id); err == nil {
		t.Fatal("expected an oversized request id to be rejected")
	}
	store := openJournalStore(t)
	err := RunJournaled(context.Background(), store, logger.New(), &db.Server{ID: "srv"}, "start", id, func() error {
		t.Fatal("mutate ran")
		return nil
	})
	if err == nil {
		t.Fatal("expected RunJournaled to reject the request id")
	}
}
