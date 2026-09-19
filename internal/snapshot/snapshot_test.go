package snapshot

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestStore(t *testing.T) *storage.Store {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Path:           ":memory:",
			AutoMigrate:    true,
			MaxConnections: 1,
		},
	}
	store, err := storage.NewSQLiteStore(cfg)
	require.NoError(t, err)
	require.NoError(t, store.Migrate())
	return store
}

func TestSnapshot_CreateAndList(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	log := logger.New()

	serverDir, err := os.MkdirTemp("", "server-snap-*")
	require.NoError(t, err)
	defer os.RemoveAll(serverDir)

	// Create test server files
	require.NoError(t, os.MkdirAll(filepath.Join(serverDir, "mods"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(serverDir, "server.properties"), []byte("server-port=25565\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(serverDir, "mods", "sodium.jar"), []byte("dummy-jar-data"), 0644))

	server := &storage.Server{
		ID:       uuid.New().String(),
		Name:     "snapshot-test-server",
		DataPath: serverDir,
		Status:   storage.StatusStopped,
	}
	require.NoError(t, store.CreateServer(ctx, server))

	engine := NewEngine(store, nil, nil, log, Config{
		BackupDir:    "", // uses .carbon-panel_snapshots inside server dir
		MaxSnapshots: 5,
	})

	// 1. Create pre-update snapshot
	snap, err := engine.CreatePreUpdateSnapshot(ctx, server, "pre-update test")
	require.NoError(t, err)
	assert.NotEmpty(t, snap.ID)
	assert.Equal(t, server.ID, snap.ServerID)
	assert.Equal(t, 3, snap.FileCount)
	assert.Greater(t, snap.SizeBytes, int64(0))
	assert.FileExists(t, snap.FilePath)

	// 2. List snapshots
	snaps, err := engine.ListSnapshots(ctx, server.ID)
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.Equal(t, snap.ID, snaps[0].ID)
}

func TestSnapshot_Rollback(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	log := logger.New()

	serverDir, err := os.MkdirTemp("", "server-rollback-*")
	require.NoError(t, err)
	defer os.RemoveAll(serverDir)

	// Initial clean state
	require.NoError(t, os.MkdirAll(filepath.Join(serverDir, "config"), 0755))
	targetConfig := filepath.Join(serverDir, "config", "modpack.json")
	require.NoError(t, os.WriteFile(targetConfig, []byte(`{"version": "v1.0.0"}`), 0644))

	server := &storage.Server{
		ID:       uuid.New().String(),
		Name:     "rollback-server",
		DataPath: serverDir,
		Status:   storage.StatusStopped,
	}
	require.NoError(t, store.CreateServer(ctx, server))

	engine := NewEngine(store, nil, nil, log, Config{
		BackupDir:    "",
		MaxSnapshots: 5,
	})

	// Create snapshot of original state
	snap, err := engine.CreatePreUpdateSnapshot(ctx, server, "before broken modpack update")
	require.NoError(t, err)

	// Simulate broken modpack update / corruption
	require.NoError(t, os.WriteFile(targetConfig, []byte(`{"version": "v2.0.0-broken", "corrupt": true}`), 0644))

	// Verify file is modified
	corruptContent, err := os.ReadFile(targetConfig)
	require.NoError(t, err)
	assert.Contains(t, string(corruptContent), "broken")

	// Execute Rollback
	err = engine.Rollback(ctx, server, snap.ID)
	require.NoError(t, err)

	// Verify clean state is restored
	restoredContent, err := os.ReadFile(targetConfig)
	require.NoError(t, err)
	assert.Equal(t, `{"version": "v1.0.0"}`, string(restoredContent))
}

func TestSnapshot_Rollback_RunningContainerRequiresStop(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	log := logger.New()

	serverDir, err := os.MkdirTemp("", "server-running-rollback-*")
	require.NoError(t, err)
	defer os.RemoveAll(serverDir)

	// Clean file
	testFile := filepath.Join(serverDir, "server.properties")
	require.NoError(t, os.WriteFile(testFile, []byte("port=25565\n"), 0644))

	server := &storage.Server{
		ID:       uuid.New().String(),
		Name:     "running-rollback-server",
		DataPath: serverDir,
		Status:   storage.StatusStopped,
	}
	require.NoError(t, store.CreateServer(ctx, server))

	engine := NewEngine(store, nil, nil, log, Config{
		BackupDir:    "",
		MaxSnapshots: 5,
	})

	snap, err := engine.CreatePreUpdateSnapshot(ctx, server, "baseline")
	require.NoError(t, err)

	// Now simulate server is running with a container ID, but no docker client is available to stop it
	server.Status = storage.StatusRunning
	server.ContainerID = "container-xyz-123"

	// Overwrite file with modifications
	require.NoError(t, os.WriteFile(testFile, []byte("port=25577\nmodified=true\n"), 0644))

	// Attempt rollback: must fail because container cannot be stopped safely (DEP-2)
	err = engine.Rollback(ctx, server, snap.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot safely rollback: container container-xyz-123 is running but docker client is unavailable")

	// Verify file was NOT overwritten by rollback extraction
	content, err := os.ReadFile(testFile)
	require.NoError(t, err)
	assert.Contains(t, string(content), "modified=true")
}

func TestSnapshot_Pruning(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	log := logger.New()

	serverDir, err := os.MkdirTemp("", "server-pruning-*")
	require.NoError(t, err)
	defer os.RemoveAll(serverDir)

	require.NoError(t, os.WriteFile(filepath.Join(serverDir, "data.txt"), []byte("hello"), 0644))

	server := &storage.Server{
		ID:       uuid.New().String(),
		Name:     "pruning-server",
		DataPath: serverDir,
		Status:   storage.StatusStopped,
	}
	require.NoError(t, store.CreateServer(ctx, server))

	// Keep at most 2 snapshots
	engine := NewEngine(store, nil, nil, log, Config{
		BackupDir:    "",
		MaxSnapshots: 2,
	})

	snap1, err := engine.CreatePreUpdateSnapshot(ctx, server, "snap 1")
	require.NoError(t, err)

	snap2, err := engine.CreatePreUpdateSnapshot(ctx, server, "snap 2")
	require.NoError(t, err)

	snap3, err := engine.CreatePreUpdateSnapshot(ctx, server, "snap 3")
	require.NoError(t, err)

	// List snapshots; should only retain snap3 and snap2
	snaps, err := engine.ListSnapshots(ctx, server.ID)
	require.NoError(t, err)
	assert.Len(t, snaps, 2)
	assert.Equal(t, snap3.ID, snaps[0].ID)
	assert.Equal(t, snap2.ID, snaps[1].ID)

	// snap1 archive should be deleted from disk
	assert.NoFileExists(t, snap1.FilePath)
}
