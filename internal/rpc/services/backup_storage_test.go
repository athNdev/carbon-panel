// SPDX-License-Identifier: MPL-2.0
package services

import (
	"context"
	"strings"
	"testing"

	"github.com/athNdev/carbon-panel/internal/db"
)

func TestRequireLocalRestoreStorage(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()
	if err := store.CreateNode(ctx, &db.Node{ID: "local-storage", Name: "Local", Host: "unix:///var/run/docker.sock", Enabled: true, IsLocal: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateNode(ctx, &db.Node{ID: "remote-storage", Name: "Remote", Host: "tcp://127.0.0.1:2375", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateNode(ctx, &db.Node{ID: "remote-shared", Name: "RemoteShared", Host: "tcp://127.0.0.1:2376", Enabled: true, StorageRoot: "/srv/shared"}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateNode(ctx, &db.Node{ID: "remote-nfs", Name: "RemoteNFS", Host: "tcp://127.0.0.1:2377", Enabled: true, ControllerStorageRoot: "/mnt/nfs"}); err != nil {
		t.Fatal(err)
	}
	if err := requireLocalRestoreStorage(ctx, store, "local-storage"); err != nil {
		t.Fatalf("local restore storage should be accepted: %v", err)
	}
	if err := requireLocalRestoreStorage(ctx, store, "local"); err != nil {
		t.Fatalf("legacy local alias should be accepted: %v", err)
	}
	if err := requireLocalRestoreStorage(ctx, store, "remote-storage"); err == nil || !strings.Contains(err.Error(), "node-aware storage") {
		t.Fatalf("remote storage should be refused explicitly, got %v", err)
	}
	if err := requireLocalRestoreStorage(ctx, store, "remote-shared"); err != nil {
		t.Fatalf("remote node on shared storage should be accepted: %v", err)
	}
	if err := requireLocalRestoreStorage(ctx, store, "remote-nfs"); err != nil {
		t.Fatalf("remote node with a controller-side mount should be accepted: %v", err)
	}
	if err := requireLocalRestoreStorage(ctx, store, "missing-node"); err == nil {
		t.Fatal("unknown storage owner must fail closed")
	}
}

func TestStorageNodeInfo(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()
	for _, alias := range []string{"", "local", "default"} {
		info, err := storageNodeInfo(ctx, store, alias)
		if err != nil || !info.IsLocal {
			t.Fatalf("alias %q must map to local storage, got %+v, %v", alias, info, err)
		}
	}
	if err := store.CreateNode(ctx, &db.Node{ID: "n1", Name: "n1", Host: "tcp://127.0.0.1:2375", Enabled: true, StorageRoot: "/srv/shared", ControllerStorageRoot: "/mnt/shared"}); err != nil {
		t.Fatal(err)
	}
	info, err := storageNodeInfo(ctx, store, "n1")
	if err != nil || info.IsLocal || info.StorageRoot != "/srv/shared" || info.ControllerStorageRoot != "/mnt/shared" {
		t.Fatalf("node storage view not passed through: %+v, %v", info, err)
	}
	if _, err := storageNodeInfo(ctx, store, "missing"); err == nil {
		t.Fatal("unknown node must fail closed")
	}
}
