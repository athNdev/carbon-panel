package db

import (
	"context"
	"database/sql"
	"fmt"
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
