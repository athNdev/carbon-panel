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
	if err := requireLocalRestoreStorage(ctx, store, "local-storage"); err != nil {
		t.Fatalf("local restore storage should be accepted: %v", err)
	}
	if err := requireLocalRestoreStorage(ctx, store, "local"); err != nil {
		t.Fatalf("legacy local alias should be accepted: %v", err)
	}
	if err := requireLocalRestoreStorage(ctx, store, "remote-storage"); err == nil || !strings.Contains(err.Error(), "node-aware storage") {
		t.Fatalf("remote storage should be refused explicitly, got %v", err)
	}
	if err := requireLocalRestoreStorage(ctx, store, "missing-node"); err == nil {
		t.Fatal("unknown storage owner must fail closed")
	}
}
