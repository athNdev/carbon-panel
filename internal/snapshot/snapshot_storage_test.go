// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package snapshot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/internal/nodestorage"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/google/uuid"
)

func TestSnapshotRestoresTheMappedWorld(t *testing.T) {
	store := setupTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	dir := t.TempDir()
	mount := filepath.Join(dir, "mount", "srv-data")
	controller := filepath.Join(dir, "controller", "srv-data")
	if err := os.MkdirAll(mount, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(controller, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mount, "server.properties"), []byte("from-mount"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(controller, "server.properties"), []byte("from-controller"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateNode(ctx, &storage.Node{
		ID: "remote-snap", Name: "remote-snap", Host: "tcp://10.4.0.5:2375", Enabled: true,
		StorageRoot: filepath.Join(dir, "mount"),
	}); err != nil {
		t.Fatal(err)
	}
	server := &storage.Server{
		ID: uuid.New().String(), Name: "snap-remote", NodeID: "remote-snap",
		DataPath: controller, Status: storage.StatusStopped,
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}

	dockerAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(dockerAPI.Close)
	cli, err := docker.NewClient(strings.Replace(dockerAPI.URL, "http://", "tcp://", 1), logger.New())
	if err != nil {
		t.Fatal(err)
	}
	pool := docker.NewClientPool(store, cli, logger.New())
	pool.RegisterClient("remote-snap", cli)
	t.Cleanup(func() { _ = pool.Close() })

	engine := NewEngineWithPool(store, cli, pool, nil, logger.New(), Config{BackupDir: filepath.Join(dir, "backups")})
	snap, err := engine.CreatePreUpdateSnapshot(ctx, server, "mapped")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mount, "server.properties"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := engine.Rollback(ctx, server, snap.ID); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(mount, "server.properties"))
	if err != nil || string(got) != "from-mount" {
		t.Fatalf("mapped world = %q, %v", got, err)
	}
	kept, err := os.ReadFile(filepath.Join(controller, "server.properties"))
	if err != nil || string(kept) != "from-controller" {
		t.Fatalf("controller copy = %q, %v", kept, err)
	}
}

func TestSnapshotRefusesAnUnreachableRemoteWorld(t *testing.T) {
	store := setupTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	controller := filepath.Join(t.TempDir(), "controller", "srv-data")
	if err := os.MkdirAll(controller, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(controller, "server.properties"), []byte("from-controller"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateNode(ctx, &storage.Node{
		ID: "remote-bare", Name: "remote-bare", Host: "tcp://10.4.0.6:2375", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	server := &storage.Server{
		ID: uuid.New().String(), Name: "snap-bare", NodeID: "remote-bare",
		DataPath: controller, Status: storage.StatusStopped,
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}

	engine := NewEngine(store, nil, nil, logger.New(), Config{})
	if _, err := engine.CreatePreUpdateSnapshot(ctx, server, "bare"); !nodestorage.IsUnavailable(err) {
		t.Fatalf("create error = %v, want unavailable", err)
	}
	if _, err := os.Stat(filepath.Join(controller, ".carbon-panel_snapshots")); !os.IsNotExist(err) {
		t.Fatalf("controller snapshot dir: %v", err)
	}
	kept, err := os.ReadFile(filepath.Join(controller, "server.properties"))
	if err != nil || string(kept) != "from-controller" {
		t.Fatalf("controller copy = %q, %v", kept, err)
	}
}
