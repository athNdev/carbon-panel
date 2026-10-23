// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

func newOperationTestService(t *testing.T) (*ServerService, *storage.Store) {
	t.Helper()

	store, err := storage.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{
			Path:        filepath.Join(t.TempDir(), "operations.db"),
			AutoMigrate: true,
		},
		Docker: config.DockerConfig{Host: "unix:///var/run/docker.sock"},
	})
	if err != nil {
		t.Fatalf("create test store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	return &ServerService{store: store, log: logger.New()}, store
}

func TestRunJournaledMutationRecordsCompletion(t *testing.T) {
	service, store := newOperationTestService(t)
	server := &storage.Server{ID: "srv-1", NodeID: "node-1", ContainerID: "cont-1"}

	if err := service.runJournaledMutation(context.Background(), server, "start", func() error {
		return nil
	}); err != nil {
		t.Fatalf("runJournaledMutation: %v", err)
	}

	active, err := store.ListActiveServerOperations(context.Background())
	if err != nil {
		t.Fatalf("ListActiveServerOperations: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("completed mutation left an active operation: %+v", active)
	}
	if err := store.AssertNoServerOperation(context.Background(), server.ID); err != nil {
		t.Fatalf("server still blocked after completion: %v", err)
	}
}

func TestRunJournaledMutationReleasesOnMutationError(t *testing.T) {
	service, store := newOperationTestService(t)
	server := &storage.Server{ID: "srv-2", NodeID: "node-1", ContainerID: "cont-2"}
	mutationErr := errors.New("docker refused the start")

	err := service.runJournaledMutation(context.Background(), server, "start", func() error {
		return mutationErr
	})
	if !errors.Is(err, mutationErr) {
		t.Fatalf("error = %v, want %v", err, mutationErr)
	}

	// A definite mutation failure releases the lock so the user can retry
	// without restarting the panel.
	active, err := store.ListActiveServerOperations(context.Background())
	if err != nil {
		t.Fatalf("ListActiveServerOperations: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("failed mutation left an active operation: %+v", active)
	}
}

func TestRunJournaledMutationUsesDistinctKeysForRepeatedRuns(t *testing.T) {
	service, store := newOperationTestService(t)
	server := &storage.Server{ID: "srv-3", NodeID: "node-1", ContainerID: "cont-3"}

	for i := 0; i < 2; i++ {
		if err := service.runJournaledMutation(context.Background(), server, "stop", func() error {
			return nil
		}); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}

	active, err := store.ListActiveServerOperations(context.Background())
	if err != nil {
		t.Fatalf("ListActiveServerOperations: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("repeated runs left active operations: %+v", active)
	}
}
