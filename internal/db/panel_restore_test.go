// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/athNdev/carbon-panel/internal/config"
)

func writeRow(t *testing.T, path, value string) {
	t.Helper()
	handle, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = handle.Close() }()
	if _, err := handle.Exec("CREATE TABLE IF NOT EXISTS t (v TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Exec("INSERT INTO t (v) VALUES (?)", value); err != nil {
		t.Fatal(err)
	}
}

func readRows(t *testing.T, path string) map[string]bool {
	t.Helper()
	handle, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = handle.Close() }()
	rows, err := handle.Query("SELECT v FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		out[v] = true
	}
	return out
}

func makeSnapshot(t *testing.T, dir string) (dbPath, snapshot string) {
	t.Helper()
	dbPath = filepath.Join(dir, "panel.sqlite")
	store, err := NewSQLiteStore(&config.Config{Database: config.DatabaseConfig{Path: dbPath}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.DB(); err != nil {
		t.Fatal(err)
	}
	writeRow(t, dbPath, "before")
	snapshot = filepath.Join(dir, "panel-20261027.sqlite")
	if err := store.BackupTo(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	return dbPath, snapshot
}

func TestRestorePanelDatabaseRoundTrip(t *testing.T) {
	dir := t.TempDir()
	dbPath, snapshot := makeSnapshot(t, dir)

	// The live database moves on after the snapshot was taken.
	writeRow(t, dbPath, "after")

	if err := RestorePanelDatabase(dbPath, snapshot); err != nil {
		t.Fatalf("RestorePanelDatabase: %v", err)
	}

	rows := readRows(t, dbPath)
	if !rows["before"] || rows["after"] {
		t.Fatalf("restored rows = %v, want the snapshot contents", rows)
	}
	if _, err := os.Stat(dbPath + ".pre-restore"); err != nil {
		t.Fatalf("previous database should be kept at .pre-restore: %v", err)
	}
}

// TestRestorePanelDatabaseRemovesStaleWAL is the regression for the restore
// runbook pitfall: a leftover -wal from the previous database gets replayed
// over the restored file and corrupts it on the next start.
func TestRestorePanelDatabaseRemovesStaleWAL(t *testing.T) {
	dir := t.TempDir()
	dbPath, snapshot := makeSnapshot(t, dir)
	writeRow(t, dbPath, "after")

	for _, sidecar := range []string{dbPath + "-wal", dbPath + "-shm"} {
		if err := os.WriteFile(sidecar, []byte("stale sidecar"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := RestorePanelDatabase(dbPath, snapshot); err != nil {
		t.Fatalf("RestorePanelDatabase: %v", err)
	}
	for _, sidecar := range []string{dbPath + "-wal", dbPath + "-shm"} {
		if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
			t.Fatalf("stale sidecar %s should be removed, stat err = %v", sidecar, err)
		}
	}

	rows := readRows(t, dbPath)
	if !rows["before"] || rows["after"] {
		t.Fatalf("restored rows = %v, want the snapshot contents", rows)
	}
}

func TestRestorePanelDatabaseRejectsCorruptBackup(t *testing.T) {
	dir := t.TempDir()
	dbPath, _ := makeSnapshot(t, dir)
	writeRow(t, dbPath, "after")

	bad := filepath.Join(dir, "bad.sqlite")
	if err := os.WriteFile(bad, []byte("this is not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RestorePanelDatabase(dbPath, bad); err == nil {
		t.Fatal("want error restoring a corrupt backup")
	}

	rows := readRows(t, dbPath)
	if !rows["after"] || !rows["before"] {
		t.Fatalf("live rows = %v, must be untouched by a refused restore", rows)
	}
	if _, err := os.Stat(dbPath + ".pre-restore"); !os.IsNotExist(err) {
		t.Fatalf("a refused restore must not churn the live database, stat err = %v", err)
	}
}
