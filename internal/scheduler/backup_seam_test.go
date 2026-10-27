// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	appconfig "github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/worldstore"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/stretchr/testify/require"
)

func backupTaskFixture(t *testing.T) (*Scheduler, *storage.Server) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.NewSQLiteStore(&appconfig.Config{
		Database: appconfig.DatabaseConfig{Path: filepath.Join(dir, "panel.sqlite")},
	})
	require.NoError(t, err)
	require.NoError(t, store.Migrate())
	t.Cleanup(func() { _ = store.Close() })

	dataPath := filepath.Join(dir, "servers", "srv-data")
	for rel, content := range map[string]string{
		"world/level.dat":     "level",
		"server.properties":   "motd=hello",
		"logs/latest.log":     "noisy",
		"cache/scratch.cache": "ephemeral",
	} {
		p := filepath.Join(dataPath, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o700))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	}

	s := &Scheduler{
		store: store,
		appConfig: &appconfig.Config{
			Storage: appconfig.StorageConfig{
				DataDir:    dir,
				BackupDir:  filepath.Join(dir, "backups"),
			},
		},
		log: logger.New(),
	}
	server := &storage.Server{ID: "srv-1", NodeID: "node-1", DataPath: dataPath}
	return s, server
}

// TestBackupTaskDefaultUsesWorldDataSeam pins the ADR 0004 consolidation:
// default backups are worldstore manifest archives covering the full state
// tree minus excludes, restorable through the atomic seam.
func TestBackupTaskDefaultUsesWorldDataSeam(t *testing.T) {
	s, server := backupTaskFixture(t)

	out, err := s.executeBackupTask(context.Background(), server, &storage.ScheduledTask{Name: "nightly"})
	require.NoError(t, err)
	require.Contains(t, out, "backup created")

	destDir := filepath.Join(s.appConfig.Storage.BackupDir, filepath.Base(server.DataPath))
	matches, err := filepath.Glob(filepath.Join(destDir, "*.zip"))
	require.NoError(t, err)
	require.Len(t, matches, 1)
	archive := matches[0]
	require.True(t, worldstore.HasWorldstoreManifest(archive), "default backup must be a worldstore manifest archive")

	// The archive covers the full state tree minus excludes, and the whole
	// tree is restorable through the seam.
	s2, server2 := backupTaskFixture(t)
	ws, err := worldstore.OpenServerWorlds("owner-1", server2.ID, server2.NodeID,
		filepath.Join(s2.appConfig.Storage.DataDir, "worldstore-journal"), server2.DataPath)
	require.NoError(t, err)
	defer func() { _ = ws.Close() }()
	require.NoError(t, ws.RestoreFrom(ws.Identity(), archive))
	require.FileExists(t, filepath.Join(server2.DataPath, "world", "level.dat"))
	require.FileExists(t, filepath.Join(server2.DataPath, "server.properties"))
}

// TestBackupTaskExplicitPathsKeepLegacyArchive preserves the documented
// compatibility path: custom-path tasks still produce plain zips.
func TestBackupTaskExplicitPathsKeepLegacyArchive(t *testing.T) {
	s, server := backupTaskFixture(t)

	_, err := s.executeBackupTask(context.Background(), server, &storage.ScheduledTask{
		Name:   "world-only",
		Config: `{"paths":["world"]}`,
	})
	require.NoError(t, err)

	destDir := filepath.Join(s.appConfig.Storage.BackupDir, filepath.Base(server.DataPath))
	matches, err := filepath.Glob(filepath.Join(destDir, "*.zip"))
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.False(t, worldstore.HasWorldstoreManifest(matches[0]), "custom-path backups stay on the legacy pipeline")
}
