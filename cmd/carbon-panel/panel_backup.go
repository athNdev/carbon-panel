package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

// startPanelDBBackup writes a daily VACUUM INTO snapshot of the panel
// database. database.panel_backup=off skips it. Restore is manual: stop the
// panel, replace the database file with the snapshot, and start again.
func startPanelDBBackup(ctx context.Context, store *db.Store, cfg *config.Config, log *logger.Logger) {
	schedule := strings.ToLower(strings.TrimSpace(cfg.Database.PanelBackup))
	if schedule == "" || schedule == "off" || schedule == "disabled" {
		log.Info("Panel database backup is off")
		return
	}
	if schedule != "daily" {
		log.Error("database.panel_backup must be daily or off, got %q", schedule)
		return
	}
	dir := filepath.Join(cfg.Storage.DataDir, "backups")
	keep := cfg.Database.PanelBackupKeep
	go func() {
		run := func() {
			path, err := store.RunPanelBackup(ctx, dir, keep, time.Now().UTC())
			if err != nil {
				log.Error("Panel database backup failed: %v", err)
				return
			}
			log.Info("Panel database backup written to %s", path)
		}
		run()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				today := filepath.Join(dir, db.PanelBackupFilename(time.Now().UTC()))
				if _, err := os.Stat(today); err == nil {
					continue
				}
				run()
			}
		}
	}()
}
