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
				DataDir:   dir,
				BackupDir: filepath.Join(dir, "backups"),
			},
		},
		log: logger.New(),
	}
	// The fixture server lives on the local node; real deployments always have
	// a node row for every server (DeleteNode refuses nodes with servers).
	require.NoError(t, store.CreateNode(context.Background(), &storage.Node{
		ID: "node-1", Name: "node-1", Host: "unix:///var/run/docker.sock", Enabled: true, IsLocal: true,
	}))
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

// TestBackupTaskUnresolvableNodeFailsClosed pins the ADR 0005 fail-closed
// rule: a server whose owning node cannot be resolved never archives a
// controller-local path as if it were the server's data.
func TestBackupTaskUnresolvableNodeFailsClosed(t *testing.T) {
	s, server := backupTaskFixture(t)
	server.NodeID = "node-missing"

	out, err := s.executeBackupTask(context.Background(), server, &storage.ScheduledTask{Name: "nightly"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "resolve storage node")
	require.Empty(t, out)

	destDir := filepath.Join(s.appConfig.Storage.BackupDir, filepath.Base(server.DataPath))
	matches, err := filepath.Glob(filepath.Join(destDir, "*.zip"))
	require.NoError(t, err)
	require.Empty(t, matches, "no archive may be produced for an unresolvable node")
}

// TestBackupTaskRemoteWithoutSharedStorageFails pins the typed refusal: a
// remote node without reachable storage fails the task instead of archiving
// the controller-local path.
func TestBackupTaskRemoteWithoutSharedStorageFails(t *testing.T) {
	s, server := backupTaskFixture(t)
	ctx := context.Background()
	node, err := s.store.GetNode(ctx, "node-1")
	require.NoError(t, err)
	node.IsLocal = false
	node.StorageRoot = ""
	require.NoError(t, s.store.UpdateNode(ctx, node))

	_, err = s.executeBackupTask(ctx, server, &storage.ScheduledTask{Name: "nightly"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "node-aware storage")
}

// TestBackupTaskRemoteSharedStorageMapsControllerPath pins the ADR 0005
// mapping: a remote node on shared storage is archived through the
// controller's mount of that storage, and the archive restores through the
// same mapped path.
func TestBackupTaskRemoteSharedStorageMapsControllerPath(t *testing.T) {
	s, server := backupTaskFixture(t)
	ctx := context.Background()

	mount := filepath.Join(s.appConfig.Storage.DataDir, "shared-mount")
	require.NoError(t, os.MkdirAll(mount, 0o755))
	require.NoError(t, os.Rename(server.DataPath, filepath.Join(mount, "srv-data")))

	node, err := s.store.GetNode(ctx, "node-1")
	require.NoError(t, err)
	node.IsLocal = false
	node.StorageRoot = mount
	require.NoError(t, s.store.UpdateNode(ctx, node))

	// The server's data path is the node-side location; the controller only
	// sees the same tree through its mount of the shared storage.
	server.DataPath = filepath.Join(s.appConfig.Storage.DataDir, "node-side", "servers", "srv-data")

	out, err := s.executeBackupTask(ctx, server, &storage.ScheduledTask{Name: "nightly"})
	require.NoError(t, err)
	require.Contains(t, out, "backup created")

	destDir := filepath.Join(s.appConfig.Storage.BackupDir, "srv-data")
	matches, err := filepath.Glob(filepath.Join(destDir, "*.zip"))
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.True(t, worldstore.HasWorldstoreManifest(matches[0]), "mapped backup must stay a worldstore manifest archive")

	ws, err := worldstore.OpenServerWorlds("owner-1", server.ID, server.NodeID,
		filepath.Join(s.appConfig.Storage.DataDir, "worldstore-journal"), filepath.Join(mount, "srv-data"))
	require.NoError(t, err)
	defer func() { _ = ws.Close() }()
	require.NoError(t, ws.RestoreFrom(ws.Identity(), matches[0]))
	require.FileExists(t, filepath.Join(mount, "srv-data", "world", "level.dat"))
	require.FileExists(t, filepath.Join(mount, "srv-data", "server.properties"))
}
