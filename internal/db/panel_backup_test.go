package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackupToKeepsSourceReadable(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	if err := store.db.Exec("CREATE TABLE panel_backup_probe (id TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	if err := store.db.Exec("INSERT INTO panel_backup_probe (id) VALUES (?)", "kept").Error; err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	dest := filepath.Join(dir, PanelBackupFilename(time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)))
	if err := store.BackupTo(ctx, dest); err != nil {
		t.Fatal(err)
	}

	var fromSource string
	if err := store.db.Raw("SELECT id FROM panel_backup_probe").Scan(&fromSource).Error; err != nil {
		t.Fatal(err)
	}
	if fromSource != "kept" {
		t.Fatalf("source row = %q", fromSource)
	}

	copy, err := sql.Open("sqlite", sqliteReadOnlyDSN(dest))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = copy.Close() }()
	var fromCopy string
	if err := copy.QueryRow("SELECT id FROM panel_backup_probe").Scan(&fromCopy); err != nil {
		t.Fatal(err)
	}
	if fromCopy != "kept" {
		t.Fatalf("snapshot row = %q", fromCopy)
	}
}

func TestBackupToLeavesPreviousSnapshotWhenIntegrityFails(t *testing.T) {
	dir := t.TempDir()
	previous := filepath.Join(dir, "panel-20261019.sqlite")
	if err := os.WriteFile(previous, []byte("previous-good"), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "not-a-database")
	if err := os.WriteFile(bad, []byte("not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sqliteIntegrity(bad); err == nil {
		t.Fatal("garbage file passed integrity_check")
	}
	got, err := os.ReadFile(previous)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "previous-good" {
		t.Fatalf("previous snapshot changed: %q", got)
	}
}

func TestPrunePanelBackupsKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"panel-20261018.sqlite",
		"panel-20261019.sqlite",
		"panel-20261020.sqlite",
		"panel-notes.txt",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := PrunePanelBackups(dir, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "panel-20261018.sqlite")); !os.IsNotExist(err) {
		t.Fatalf("oldest snapshot still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "panel-20261020.sqlite")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "panel-notes.txt")); err != nil {
		t.Fatal(err)
	}
}
