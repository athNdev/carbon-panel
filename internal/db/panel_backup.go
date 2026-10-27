// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package db

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/glebarez/sqlite"
)

// PanelBackupFilename is the snapshot name for a calendar day.
func PanelBackupFilename(now time.Time) string {
	return "panel-" + now.UTC().Format("20060102") + ".sqlite"
}

// BackupTo writes a consistent snapshot of the panel database to dest.
// The source stays open and usable. A failed integrity check removes only
// the new file and leaves any previous snapshot at dest in place.
func (s *Store) BackupTo(ctx context.Context, dest string) error {
	if isInMemorySQLitePath(s.cfg.Database.Path) {
		return fmt.Errorf("panel backup requires a file database")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	partial := dest + ".partial"
	_ = os.Remove(partial)
	if err := s.db.WithContext(ctx).Exec("VACUUM INTO ?", partial).Error; err != nil {
		_ = os.Remove(partial)
		return fmt.Errorf("VACUUM INTO %s: %w", partial, err)
	}
	if err := sqliteIntegrity(partial); err != nil {
		_ = os.Remove(partial)
		return err
	}
	if err := replaceFile(partial, dest); err != nil {
		_ = os.Remove(partial)
		return err
	}
	return nil
}

// RunPanelBackup writes today's snapshot into dir and then drops older
// daily files beyond keep. keep < 1 retains every snapshot.
func (s *Store) RunPanelBackup(ctx context.Context, dir string, keep int, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, PanelBackupFilename(now))
	if err := s.BackupTo(ctx, dest); err != nil {
		return "", err
	}
	if err := PrunePanelBackups(dir, keep); err != nil {
		return dest, err
	}
	return dest, nil
}

// PrunePanelBackups deletes daily snapshots beyond keep, oldest first.
func PrunePanelBackups(dir string, keep int) error {
	if keep < 1 {
		return nil
	}
	matches, err := filepath.Glob(filepath.Join(dir, "panel-*.sqlite"))
	if err != nil {
		return err
	}
	var names []string
	for _, path := range matches {
		base := filepath.Base(path)
		if !isDailyPanelBackup(base) {
			continue
		}
		names = append(names, path)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	if len(names) <= keep {
		return nil
	}
	for _, path := range names[keep:] {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func isDailyPanelBackup(name string) bool {
	rest, ok := strings.CutPrefix(name, "panel-")
	if !ok || !strings.HasSuffix(rest, ".sqlite") {
		return false
	}
	day := strings.TrimSuffix(rest, ".sqlite")
	if len(day) != 8 {
		return false
	}
	for _, c := range day {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func sqliteIntegrity(path string) error {
	dsn := sqliteReadOnlyDSN(path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	var result string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil {
		return fmt.Errorf("integrity_check %s: %w", path, err)
	}
	if result != "ok" {
		return fmt.Errorf("integrity_check %s: %s", path, result)
	}
	return nil
}

func sqliteReadOnlyDSN(path string) string {
	slash := filepath.ToSlash(path)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	return "file://" + slash + "?mode=ro"
}

// replaceFile moves src onto dest. dest is moved aside first so a failed
// rename puts the previous snapshot back.
func replaceFile(src, dest string) error {
	bak := dest + ".replacing"
	_ = os.Remove(bak)
	hadDest := false
	if _, err := os.Stat(dest); err == nil {
		hadDest = true
		if err := os.Rename(dest, bak); err != nil {
			return err
		}
	}
	if err := os.Rename(src, dest); err != nil {
		if hadDest {
			_ = os.Rename(bak, dest)
		}
		return err
	}
	_ = os.Remove(bak)
	return nil
}

// RestorePanelDatabase replaces the panel database at dbPath with the snapshot
// at backupPath. The panel must be stopped: this operates on plain files, not
// on a live Store.
//
// It verifies the snapshot first (a corrupt backup never replaces a good
// database), keeps the previous database at dbPath+".pre-restore", and purges
// stale WAL sidecars left by the previous database — a leftover -wal would be
// replayed over the restored file and corrupt it on the next start.
func RestorePanelDatabase(dbPath, backupPath string) error {
	if isInMemorySQLitePath(dbPath) {
		return fmt.Errorf("panel restore requires a file database")
	}
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("backup not readable: %w", err)
	}
	// Refuse to install a snapshot that does not pass its own integrity check.
	if err := sqliteIntegrity(backupPath); err != nil {
		return fmt.Errorf("refusing to restore: %w", err)
	}

	// Copy the snapshot beside the target first: the swap must be a rename on
	// the same filesystem, and the snapshot file itself stays in place.
	installed := dbPath + ".restoring"
	_ = os.Remove(installed)
	if err := copyFileContents(backupPath, installed); err != nil {
		_ = os.Remove(installed)
		return fmt.Errorf("staging restore: %w", err)
	}

	// Swap the old database aside instead of deleting it so the operator can
	// still recover from a mistaken restore.
	previous := dbPath + ".pre-restore"
	_ = os.Remove(previous)
	hadDB := false
	if _, err := os.Stat(dbPath); err == nil {
		hadDB = true
		if err := os.Rename(dbPath, previous); err != nil {
			_ = os.Remove(installed)
			return fmt.Errorf("moving previous database aside: %w", err)
		}
	}
	if err := os.Rename(installed, dbPath); err != nil {
		if hadDB {
			_ = os.Rename(previous, dbPath)
		}
		_ = os.Remove(installed)
		return fmt.Errorf("installing restored database: %w", err)
	}

	// The old WAL sidecars describe the previous database, not the restored
	// one. Remove them after the swap; failing to remove them is an error the
	// operator must resolve before starting the panel.
	for _, sidecar := range []string{dbPath + "-wal", dbPath + "-shm"} {
		if err := os.Remove(sidecar); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("restored database in place but stale %s could not be removed: %w", sidecar, err)
		}
	}

	// Final check on the installed file, so a failure here is caught before
	// the panel ever opens it.
	if err := sqliteIntegrity(dbPath); err != nil {
		return fmt.Errorf("restored database failed integrity check: %w", err)
	}
	return nil
}

// copyFileContents copies src to dest, truncating dest if it exists.
func copyFileContents(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
