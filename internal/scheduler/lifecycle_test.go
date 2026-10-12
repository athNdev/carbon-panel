// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package scheduler

import (
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

func newLifecycleTestStore(t *testing.T) *storage.Store {
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
	return store
}

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1048576, "1.0 MiB"},
		{1073741824, "1.0 GiB"},
		{5 * 1073741824, "5.0 GiB"},
	}
	for _, tc := range cases {
		if got := formatBytes(tc.in); got != tc.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveBackupPaths(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "world"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "world", "level.dat"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Explicit relative paths; missing files are reported, not fatal.
	paths, missing, err := resolveBackupPaths(dataDir, []string{"world", "server.properties", "  ", "."})
	if err != nil {
		t.Fatalf("explicit paths failed: %v", err)
	}
	if len(paths) != 1 || paths[0] != "world" {
		t.Errorf("paths = %v, want [world]", paths)
	}
	if len(missing) != 1 || missing[0] != "server.properties" {
		t.Errorf("missing = %v, want [server.properties]", missing)
	}

	// Absolute and escaping paths are rejected.
	for _, bad := range []string{"/etc/passwd", "../escape", "a/../../escape"} {
		if _, _, err := resolveBackupPaths(dataDir, []string{bad}); err == nil {
			t.Errorf("path %q accepted, want rejection", bad)
		}
	}

	// Empty request defaults to the world directory.
	paths, _, err = resolveBackupPaths(dataDir, nil)
	if err != nil {
		t.Fatalf("default paths failed: %v", err)
	}
	if len(paths) == 0 {
		t.Error("default paths empty, want world dir")
	}

	// No world directory and no explicit paths: error, not an empty backup.
	if _, _, err := resolveBackupPaths(t.TempDir(), nil); err == nil {
		t.Error("expected error when no world dir exists")
	}
}

func TestScheduler_StartStopStatus(t *testing.T) {
	store := newLifecycleTestStore(t)
	s := NewScheduler(store, nil, nil, nil, nil, logger.New(), Config{CheckInterval: 50 * time.Millisecond})

	if s.IsRunning() {
		t.Fatal("IsRunning before Start")
	}
	status := s.GetStatus()
	if status.Running || status.ActiveTasks != 0 || status.RunningExecutions != 0 {
		t.Errorf("initial status = %+v", status)
	}

	if err := s.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if err := s.Start(); err == nil {
		t.Error("second Start succeeded, want already-running error")
	}
	if !s.IsRunning() {
		t.Error("IsRunning after Start = false")
	}
	time.Sleep(150 * time.Millisecond) // let a check tick run against the empty store

	if err := s.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("second Stop failed: %v", err)
	}
	if s.IsRunning() {
		t.Error("IsRunning after Stop = true")
	}

	s.SetSnapshotEngine(nil)
}
