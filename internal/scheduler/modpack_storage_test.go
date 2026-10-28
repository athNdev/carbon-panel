// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package scheduler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/nodestorage"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/google/uuid"
)

func TestModpackSyncWritesTheMappedWorld(t *testing.T) {
	store := setupTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	dir := t.TempDir()
	mount := filepath.Join(dir, "mount")
	controller := filepath.Join(dir, "controller", "srv-data")
	if err := os.MkdirAll(mount, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(controller, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(controller, "config", "modpack.json"), []byte("from-controller"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateNode(ctx, &storage.Node{
		ID: "remote-pack", Name: "remote-pack", Host: "tcp://10.5.0.5:2375", Enabled: true,
		StorageRoot: mount,
	}); err != nil {
		t.Fatal(err)
	}
	server := &storage.Server{
		ID: uuid.New().String(), Name: "pack-remote", NodeID: "remote-pack",
		DataPath: controller, Status: storage.StatusStopped,
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}

	origClone := cloneFn
	t.Cleanup(func() { cloneFn = origClone })
	cloneFn = func(_ context.Context, _ string, _ string, dest string) ([]byte, error) {
		if err := os.MkdirAll(filepath.Join(dest, "config"), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(dest, "config", "modpack.json"), []byte("from-git"), 0o644); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Join(dest, ".git"), 0o755); err != nil {
			return nil, err
		}
		return nil, nil
	}

	cfg, err := json.Marshal(ModpackUpdateTaskConfig{
		GitURL: "https://example.invalid/modpack.git", Branch: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	task := &storage.ScheduledTask{
		ID: uuid.New().String(), ServerID: server.ID, Name: "sync",
		TaskType: storage.TaskTypeModpackUpdate, Config: string(cfg),
	}
	if _, err := NewScheduler(store, nil, nil, nil, nil, logger.New()).executeModpackUpdateTask(ctx, server, task); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(mount, "srv-data", "config", "modpack.json"))
	if err != nil || string(got) != "from-git" {
		t.Fatalf("mapped modpack = %q, %v", got, err)
	}
	kept, err := os.ReadFile(filepath.Join(controller, "config", "modpack.json"))
	if err != nil || string(kept) != "from-controller" {
		t.Fatalf("controller copy = %q, %v", kept, err)
	}
	if _, err := os.Stat(filepath.Join(mount, "srv-data", ".carbon-panel_modpack_git", "config", "modpack.json")); err != nil {
		t.Fatalf("clone cache on mapped root: %v", err)
	}
}

func TestModpackSyncRefusesAnUnreachableRemoteNode(t *testing.T) {
	store := setupTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	controller := filepath.Join(t.TempDir(), "controller", "srv-data")
	if err := store.CreateNode(ctx, &storage.Node{
		ID: "remote-bare", Name: "remote-bare", Host: "tcp://10.5.0.6:2375", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	server := &storage.Server{
		ID: uuid.New().String(), Name: "pack-bare", NodeID: "remote-bare",
		DataPath: controller, Status: storage.StatusStopped,
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}

	origClone := cloneFn
	t.Cleanup(func() { cloneFn = origClone })
	cloneFn = func(context.Context, string, string, string) ([]byte, error) {
		t.Fatal("clone ran for an unreachable node")
		return nil, nil
	}

	cfg, err := json.Marshal(ModpackUpdateTaskConfig{
		GitURL: "https://example.invalid/modpack.git", Branch: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	task := &storage.ScheduledTask{
		ID: uuid.New().String(), ServerID: server.ID, Name: "sync",
		TaskType: storage.TaskTypeModpackUpdate, Config: string(cfg),
	}
	_, err = NewScheduler(store, nil, nil, nil, nil, logger.New()).executeModpackUpdateTask(ctx, server, task)
	if !nodestorage.IsUnavailable(err) {
		t.Fatalf("sync error = %v, want unavailable", err)
	}
	if _, statErr := os.Stat(controller); !os.IsNotExist(statErr) {
		t.Fatalf("controller data path %s: %v", controller, statErr)
	}
}
