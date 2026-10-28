// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package scheduler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

func TestStopTaskRefusesWhileALifecycleTransitionOwnsTheServer(t *testing.T) {
	s, store := newTriggerTestScheduler(t)
	ctx := context.Background()
	server := &storage.Server{
		ID:          "srv-stop",
		Name:        "stop",
		Status:      storage.StatusRunning,
		ContainerID: "container-1",
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("create server: %v", err)
	}
	held, err := docker.TryBeginServerTransition(server.ID)
	if err != nil {
		t.Fatalf("claim transition: %v", err)
	}
	t.Cleanup(held.Release)

	_, err = s.executeStopTask(ctx, server, &storage.ScheduledTask{})
	if !errors.Is(err, docker.ErrTransitionInFlight) {
		t.Fatalf("stop error = %v, want the lifecycle transition", err)
	}
	got, err := store.GetServer(ctx, server.ID)
	if err != nil {
		t.Fatalf("reload server: %v", err)
	}
	if got.Status != storage.StatusRunning {
		t.Fatalf("status = %s, want running", got.Status)
	}
}

func TestModpackSyncRefusesWhileALifecycleTransitionOwnsTheServer(t *testing.T) {
	s := &Scheduler{log: logger.New()}
	src := t.TempDir()
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "level.dat"), []byte("level"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := &storage.Server{ID: "srv-sync", DataPath: dest}
	held, err := docker.TryBeginServerTransition(server.ID)
	if err != nil {
		t.Fatalf("claim transition: %v", err)
	}
	t.Cleanup(held.Release)

	_, err = s.syncModpackFiles(context.Background(), server, src)
	if !errors.Is(err, docker.ErrTransitionInFlight) {
		t.Fatalf("sync error = %v, want the lifecycle transition", err)
	}
	if _, statErr := os.Stat(filepath.Join(dest, "level.dat")); !os.IsNotExist(statErr) {
		t.Fatalf("world file stat = %v, want absent", statErr)
	}

	held.Release()
	if _, err := s.syncModpackFiles(context.Background(), server, src); err != nil {
		t.Fatalf("sync after release: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dest, "level.dat"))
	if err != nil || string(body) != "level" {
		t.Fatalf("synced world = %q, %v", body, err)
	}
}
