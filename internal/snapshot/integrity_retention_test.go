// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package snapshot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshot_IntegrityVerificationAndChecksum(t *testing.T) {
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	log := logger.New()

	serverDir := t.TempDir()
	testFile := filepath.Join(serverDir, "server.properties")
	require.NoError(t, os.WriteFile(testFile, []byte("port=25565\nlevel-name=world\n"), 0644))

	server := &storage.Server{
		ID:       uuid.New().String(),
		Name:     "integrity-test-server",
		DataPath: serverDir,
		Status:   storage.StatusStopped,
	}
	require.NoError(t, store.CreateServer(ctx, server))

	engine := NewEngine(store, nil, nil, log, Config{
		BackupDir:    "",
		MaxSnapshots: 5,
	})

	snap, err := engine.CreatePreUpdateSnapshot(ctx, server, "baseline integrity test")
	require.NoError(t, err)

	// Verify zip file and .sha256 sidecar exist
	assert.FileExists(t, snap.FilePath)
	sidecarPath := snap.FilePath + ".sha256"
	assert.FileExists(t, sidecarPath)

	sidecarData, err := os.ReadFile(sidecarPath)
	require.NoError(t, err)
	assert.NotEmpty(t, string(sidecarData))

	// 1. Mutate test file in server dir
	require.NoError(t, os.WriteFile(testFile, []byte("port=25577\nmodified=true\n"), 0644))

	// 2. Corrupt checksum sidecar with bogus hash
	require.NoError(t, os.WriteFile(sidecarPath, []byte("0000000000000000000000000000000000000000000000000000000000000000  bogus.zip\n"), 0644))

	// Rollback must fail due to checksum mismatch
	err = engine.Rollback(ctx, server, snap.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")

	// Server test file should still be modified (rollback aborted before extraction)
	content, err := os.ReadFile(testFile)
	require.NoError(t, err)
	assert.Contains(t, string(content), "modified=true")

	// 3. Fix sidecar but corrupt the zip file itself
	require.NoError(t, os.WriteFile(sidecarPath, sidecarData, 0644))
	// Truncate or overwrite zip with random bytes
	require.NoError(t, os.WriteFile(snap.FilePath, []byte("not-a-valid-zip-content"), 0644))

	// Rollback must fail due to zip integrity failure or checksum
	err = engine.Rollback(ctx, server, snap.ID)
	require.Error(t, err)

	// 4. Test DeleteSnapshot removes both .zip and .sha256
	err = engine.DeleteSnapshot(ctx, snap.ID)
	require.NoError(t, err)
	assert.NoFileExists(t, snap.FilePath)
	assert.NoFileExists(t, sidecarPath)
}

func TestSnapshot_Rollback_LifecycleConcurrencyGuard(t *testing.T) {
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	log := logger.New()

	serverDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(serverDir, "server.properties"), []byte("port=25565\n"), 0644))

	server := &storage.Server{
		ID:       uuid.New().String(),
		Name:     "concurrency-guard-server",
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

	// Simulate concurrent transition lock held by another process
	acquired := docker.TryBeginServerTransition(server.ID)
	require.True(t, acquired, "failed to acquire initial server transition lock")

	// Attempt rollback: must fail because transition is in progress
	err = engine.Rollback(ctx, server, snap.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "currently undergoing another transition")

	// Release lock and rollback must now succeed
	docker.EndServerTransition(server.ID)
	err = engine.Rollback(ctx, server, snap.ID)
	require.NoError(t, err)
}

func TestSnapshot_RetentionByAgeAndPreserveLatest(t *testing.T) {
	e, store := newSnapshotTestEngine(t, 10)
	// Set retention to 7 days
	e.retentionDays = 7
	ctx := context.Background()

	serverID := "srv-retention-age"
	tempDir := t.TempDir()

	// Create 3 snapshots:
	// snap-oldest: 14 days old (should be pruned)
	// snap-middle: 10 days old (should be pruned)
	// snap-recent: 2 days old (should be kept)
	oldestFile := filepath.Join(tempDir, "oldest.zip")
	middleFile := filepath.Join(tempDir, "middle.zip")
	recentFile := filepath.Join(tempDir, "recent.zip")

	require.NoError(t, os.WriteFile(oldestFile, []byte("data"), 0644))
	require.NoError(t, os.WriteFile(oldestFile+".sha256", []byte("hash oldest"), 0644))
	require.NoError(t, os.WriteFile(middleFile, []byte("data"), 0644))
	require.NoError(t, os.WriteFile(middleFile+".sha256", []byte("hash middle"), 0644))
	require.NoError(t, os.WriteFile(recentFile, []byte("data"), 0644))
	require.NoError(t, os.WriteFile(recentFile+".sha256", []byte("hash recent"), 0644))

	now := time.Now()
	// Store in newest-to-oldest order
	require.NoError(t, store.CreateSnapshot(ctx, &storage.ServerSnapshot{
		ID: "snap-recent", ServerID: serverID, Name: "recent",
		FilePath: recentFile, CreatedAt: now.Add(-2 * 24 * time.Hour),
	}))
	require.NoError(t, store.CreateSnapshot(ctx, &storage.ServerSnapshot{
		ID: "snap-middle", ServerID: serverID, Name: "middle",
		FilePath: middleFile, CreatedAt: now.Add(-10 * 24 * time.Hour),
	}))
	require.NoError(t, store.CreateSnapshot(ctx, &storage.ServerSnapshot{
		ID: "snap-oldest", ServerID: serverID, Name: "oldest",
		FilePath: oldestFile, CreatedAt: now.Add(-14 * 24 * time.Hour),
	}))

	pruned, err := e.PruneSnapshots(ctx, serverID)
	require.NoError(t, err)
	assert.Equal(t, 2, pruned)

	// Verify oldest and middle files + sha256 were deleted
	assert.NoFileExists(t, oldestFile)
	assert.NoFileExists(t, oldestFile+".sha256")
	assert.NoFileExists(t, middleFile)
	assert.NoFileExists(t, middleFile+".sha256")

	// Recent snapshot must remain
	assert.FileExists(t, recentFile)
	assert.FileExists(t, recentFile+".sha256")

	remaining, err := e.ListSnapshots(ctx, serverID)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	assert.Equal(t, "snap-recent", remaining[0].ID)

	// Now test preservation of the latest snapshot even if ALL snapshots are older than retention cutoff!
	// Update snap-recent to 30 days old
	snapRecent, err := store.GetSnapshot(ctx, "snap-recent")
	require.NoError(t, err)
	snapRecent.CreatedAt = now.Add(-30 * 24 * time.Hour)
	require.NoError(t, store.DB().Save(snapRecent).Error)

	prunedAgain, err := e.PruneSnapshots(ctx, serverID)
	require.NoError(t, err)
	assert.Equal(t, 0, prunedAgain, "most recent snapshot should be protected from deletion even if older than retention limit")
	assert.FileExists(t, recentFile)
}

func TestSnapshot_PruneAllSnapshots(t *testing.T) {
	e, store := newSnapshotTestEngine(t, 2)
	ctx := context.Background()

	serverA := &storage.Server{ID: "srv-a", Name: "Server A", Status: storage.StatusStopped}
	serverB := &storage.Server{ID: "srv-b", Name: "Server B", Status: storage.StatusStopped}
	require.NoError(t, store.CreateServer(ctx, serverA))
	require.NoError(t, store.CreateServer(ctx, serverB))

	dir := t.TempDir()
	now := time.Now()

	// Add 4 snapshots for srv-a (max is 2, so 2 should be pruned)
	for i := 0; i < 4; i++ {
		f := filepath.Join(dir, fmt.Sprintf("snap-a-%d.zip", i))
		require.NoError(t, os.WriteFile(f, []byte("test"), 0644))
		require.NoError(t, store.CreateSnapshot(ctx, &storage.ServerSnapshot{
			ID: fmt.Sprintf("snap-a-%d", i), ServerID: serverA.ID, Name: fmt.Sprintf("a-%d", i),
			FilePath: f, CreatedAt: now.Add(time.Duration(i) * time.Minute),
		}))
	}

	// Add 3 snapshots for srv-b (max is 2, so 1 should be pruned)
	for i := 0; i < 3; i++ {
		f := filepath.Join(dir, fmt.Sprintf("snap-b-%d.zip", i))
		require.NoError(t, os.WriteFile(f, []byte("test"), 0644))
		require.NoError(t, store.CreateSnapshot(ctx, &storage.ServerSnapshot{
			ID: fmt.Sprintf("snap-b-%d", i), ServerID: serverB.ID, Name: fmt.Sprintf("b-%d", i),
			FilePath: f, CreatedAt: now.Add(time.Duration(i) * time.Minute),
		}))
	}

	totalPruned, err := e.PruneAllSnapshots(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, totalPruned)

	snapsA, err := e.ListSnapshots(ctx, serverA.ID)
	require.NoError(t, err)
	assert.Len(t, snapsA, 2)

	snapsB, err := e.ListSnapshots(ctx, serverB.ID)
	require.NoError(t, err)
	assert.Len(t, snapsB, 2)
}
