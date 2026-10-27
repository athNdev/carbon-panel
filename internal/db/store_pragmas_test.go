// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package db

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/athNdev/carbon-panel/internal/config"
)

func TestNewSQLiteStoreConfiguresConcurrencyPragmas(t *testing.T) {
	store, err := NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{
			Path: filepath.Join(t.TempDir(), "panel.sqlite"),
		},
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	sqlDB, err := store.db.DB()
	if err != nil {
		t.Fatalf("store.db.DB: %v", err)
	}

	var journalMode string
	if err := sqlDB.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("query journal mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}

	var busyTimeout int
	if err := sqlDB.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("query busy timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("busy_timeout = %d, want 5000", busyTimeout)
	}

	var synchronous int
	if err := sqlDB.QueryRow("PRAGMA synchronous").Scan(&synchronous); err != nil {
		t.Fatalf("query synchronous: %v", err)
	}
	if synchronous != 1 { // NORMAL
		t.Fatalf("synchronous = %d, want %d (NORMAL)", synchronous, 1)
	}
}

// TestNewSQLiteStorePragmaOnEveryConnection guards the regression where the
// concurrency PRAGMAs only reached the first pooled connection: replacement
// connections opened by database/sql saw SQLite's instant-fail defaults.
func TestNewSQLiteStorePragmaOnEveryConnection(t *testing.T) {
	store, err := NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "panel.sqlite")},
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	sqlDB, err := store.db.DB()
	if err != nil {
		t.Fatalf("store.db.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(8)

	const workers = 8
	problems := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var busy int
			if err := sqlDB.QueryRow("PRAGMA busy_timeout").Scan(&busy); err != nil {
				problems <- err
				return
			}
			if busy != 5000 {
				problems <- fmt.Errorf("busy_timeout = %d on a pooled connection, want 5000", busy)
				return
			}
			var sync int
			if err := sqlDB.QueryRow("PRAGMA synchronous").Scan(&sync); err != nil {
				problems <- err
				return
			}
			if sync != 1 {
				problems <- fmt.Errorf("synchronous = %d on a pooled connection, want 1 (NORMAL)", sync)
			}
		}()
	}
	wg.Wait()
	close(problems)
	for err := range problems {
		t.Error(err)
	}
}

// TestNewSQLiteStoreConcurrentWritesSmoke is the behavioral regression for
// "record create intent: database is locked" (SQLITE_BUSY): contending writers
// on pooled connections must wait for the lock, never fail instantly.
func TestNewSQLiteStoreConcurrentWritesSmoke(t *testing.T) {
	store, err := NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "panel.sqlite")},
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	sqlDB, err := store.db.DB()
	if err != nil {
		t.Fatalf("store.db.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(8)
	if _, err := sqlDB.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)"); err != nil {
		t.Fatalf("create table: %v", err)
	}

	const writers = 16
	const inserts = 25
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < inserts; i++ {
				if _, err := sqlDB.Exec("INSERT INTO t (v) VALUES (?)", fmt.Sprintf("%d-%d", w, i)); err != nil {
					errs <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent write failed: %v", err)
	}
}
