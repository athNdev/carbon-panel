// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package snapshot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

func newSnapshotTestEngine(t *testing.T, maxSnapshots int) (*Engine, *storage.Store) {
	t.Helper()

	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	store, err := storage.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{
			Path:           fmt.Sprintf("file:%s?mode=memory&cache=shared", dbName),
			AutoMigrate:    true,
			MaxConnections: 5,
		},
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	engine := NewEngine(store, nil, nil, logger.New(), Config{BackupDir: t.TempDir(), MaxSnapshots: maxSnapshots})
	return engine, store
}

func TestListSnapshots_NilStoreAndEmpty(t *testing.T) {
	bare := &Engine{}
	if _, err := bare.ListSnapshots(context.Background(), "srv-1"); err == nil {
		t.Error("nil store listed without error")
	}

	e, _ := newSnapshotTestEngine(t, 5)
	listed, err := e.ListSnapshots(context.Background(), "srv-1")
	if err != nil || len(listed) != 0 {
		t.Errorf("empty list = %v, %v", listed, err)
	}
}

func TestDeleteSnapshot_MissingAndNilStore(t *testing.T) {
	bare := &Engine{}
	if err := bare.DeleteSnapshot(context.Background(), "x"); err == nil {
		t.Error("nil store delete without error")
	}

	e, _ := newSnapshotTestEngine(t, 5)
	if err := e.DeleteSnapshot(context.Background(), "missing"); err == nil {
		t.Error("missing snapshot deleted without error")
	}

	// Present snapshot with a real file: both row and file go away.
	dir := t.TempDir()
	file := filepath.Join(dir, "snap.zip")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := e.store.CreateSnapshot(ctx, &storage.ServerSnapshot{
		ID: "snap-1", ServerID: "srv-1", Name: "s1",
		FilePath: file, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("failed to insert snapshot: %v", err)
	}
	if err := e.DeleteSnapshot(ctx, "snap-1"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Error("snapshot file not removed")
	}
	if _, err := e.store.GetSnapshot(ctx, "snap-1"); err == nil {
		t.Error("snapshot row not removed")
	}
}

func TestPruneSnapshots_Retention(t *testing.T) {
	e, store := newSnapshotTestEngine(t, 2)
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		if err := store.CreateSnapshot(ctx, &storage.ServerSnapshot{
			ID: fmt.Sprintf("snap-%d", i), ServerID: "srv-1",
			Name: fmt.Sprintf("s%d", i), CreatedAt: time.Now().Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatalf("failed to insert snapshot: %v", err)
		}
	}

	e.pruneSnapshots(ctx, "srv-1")

	listed, err := e.ListSnapshots(ctx, "srv-1")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(listed) != 2 {
		t.Errorf("after prune = %d snapshots, want 2", len(listed))
	}

	// Disabled retention (<=0) and nil store are safe no-ops.
	(&Engine{}).pruneSnapshots(ctx, "srv-1")
	off := NewEngine(store, nil, nil, logger.New(), Config{MaxSnapshots: 0})
	off.pruneSnapshots(ctx, "srv-1")
	if listed, _ := e.ListSnapshots(ctx, "srv-1"); len(listed) != 2 {
		t.Errorf("disabled retention pruned: %d snapshots", len(listed))
	}
}
