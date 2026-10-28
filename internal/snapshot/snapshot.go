// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package snapshot

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/athNdev/carbon-panel/internal/command"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/internal/nodestorage"
	"github.com/athNdev/carbon-panel/internal/operation"
	"github.com/athNdev/carbon-panel/pkg/files"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/google/uuid"
)

// Engine manages volume snapshots, pre-update backups, and rollbacks for servers (MINE-23)
type Engine struct {
	store         *storage.Store
	docker        *docker.Client
	pool          *docker.ClientPool
	sender        *command.Sender
	backupDir     string
	maxSnapshots  int
	retentionDays int
	log           *logger.Logger
}

// Config holds configuration for the SnapshotEngine
type Config struct {
	BackupDir     string
	MaxSnapshots  int // Max pre-update snapshots to retain per server (default: 5)
	RetentionDays int // Max age in days to retain pre-update snapshots (0 = unlimited age)
}

// NewEngine creates a new SnapshotEngine
func NewEngine(store *storage.Store, dockerClient *docker.Client, sender *command.Sender, log *logger.Logger, cfg ...Config) *Engine {
	return NewEngineWithPool(store, dockerClient, nil, sender, log, cfg...)
}

// NewEngineWithPool creates a new SnapshotEngine with a multi-node ClientPool.
// If pool is nil and dockerClient is non-nil, a single-node pool wrapping dockerClient is created.
func NewEngineWithPool(store *storage.Store, dockerClient *docker.Client, pool *docker.ClientPool, sender *command.Sender, log *logger.Logger, cfg ...Config) *Engine {
	backupDir := ""
	maxSnapshots := 5
	retentionDays := 0
	if len(cfg) > 0 {
		backupDir = cfg[0].BackupDir
		if cfg[0].MaxSnapshots > 0 {
			maxSnapshots = cfg[0].MaxSnapshots
		}
		if cfg[0].RetentionDays > 0 {
			retentionDays = cfg[0].RetentionDays
		}
	}
	if log == nil {
		log = logger.New()
	}
	if pool == nil && dockerClient != nil {
		pool = docker.NewClientPool(store, dockerClient, log)
	}

	return &Engine{
		store:         store,
		docker:        dockerClient,
		pool:          pool,
		sender:        sender,
		backupDir:     backupDir,
		maxSnapshots:  maxSnapshots,
		retentionDays: retentionDays,
		log:           log,
	}
}

// resolveDocker returns the Docker client for the given nodeID, preferring a
// strict node-specific client from the pool over the single default client so
// rollback operations for a remote node's server are not silently applied to
// the local Docker daemon.
//
// An unresolvable remote node is an error: rollback mutates a server's data and
// container state, so guessing the daemon is not safe.
func (e *Engine) resolveDocker(nodeID string) (*docker.Client, error) {
	cli, err := docker.NewLocator(e.pool, e.docker, e.store).For(context.Background(), nodeID)
	if errors.Is(err, docker.ErrNoClient) {
		// Preserve the caller's explicit "docker client is unavailable" path.
		return nil, nil
	}
	return cli, err
}

// CreatePreUpdateSnapshot creates an atomic volume snapshot before updating modpacks or configs
func (e *Engine) CreatePreUpdateSnapshot(ctx context.Context, server *storage.Server, reason string) (*storage.ServerSnapshot, error) {
	if server == nil || server.DataPath == "" {
		return nil, fmt.Errorf("server data path cannot be empty")
	}
	worldRoot, err := e.worldRoot(ctx, server, false)
	if err != nil {
		return nil, err
	}

	// Determine snapshot storage directory. The archive of a remote world is
	// taken from the mapped root. When no backup directory is configured, the
	// archive stays inside that same root.
	baseSnapshotsDir := ""
	if e.backupDir != "" {
		baseSnapshotsDir = filepath.Join(e.backupDir, filepath.Base(server.DataPath), "snapshots")
	} else {
		baseSnapshotsDir = filepath.Join(worldRoot, ".carbon-panel_snapshots")
	}

	if err := os.MkdirAll(baseSnapshotsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create snapshots directory: %w", err)
	}

	timestamp := time.Now().UTC().Format("20060102-150405")
	snapshotID := "snap-" + uuid.New().String()
	name := fmt.Sprintf("pre-update-%s", timestamp)
	archivePath := filepath.Join(baseSnapshotsDir, fmt.Sprintf("%s.zip", snapshotID))

	// Freeze world saves through the shared quiesce rules; the consistency
	// level says what this snapshot may claim, and a failed resume is
	// recorded because a world left in save-off stops persisting progress.
	resumeSaves, consistency := e.pauseWorldSaves(ctx, server)
	defer func() {
		if rerr := resumeSaves(); rerr != nil {
			e.log.Error("%v", rerr)
		}
	}()

	// Gather all files to include in volume snapshot
	paths, err := gatherVolumePaths(worldRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to gather volume files for snapshot: %w", err)
	}

	e.log.Info("Creating pre-update snapshot for server %s (%d files, consistency: %s) -> %s", server.Name, len(paths), consistency, archivePath)
	count, err := files.CreateZipArchive(paths, worldRoot, archivePath, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot archive: %w", err)
	}

	// Verify archive integrity before writing checksum or saving record
	if err := files.VerifyZipArchive(archivePath); err != nil {
		_ = os.Remove(archivePath)
		return nil, fmt.Errorf("snapshot archive failed verification: %w", err)
	}

	// Write SHA256 checksum sidecar
	sum, sumErr := files.SHA256File(archivePath)
	if sumErr != nil {
		_ = os.Remove(archivePath)
		return nil, fmt.Errorf("failed to checksum snapshot archive: %w", sumErr)
	}
	if err := os.WriteFile(archivePath+".sha256", []byte(sum+"  "+filepath.Base(archivePath)+"\n"), 0644); err != nil {
		_ = os.Remove(archivePath)
		return nil, fmt.Errorf("failed to write snapshot checksum: %w", err)
	}

	var size int64
	if info, err := os.Stat(archivePath); err == nil {
		size = info.Size()
	}

	snapshot := &storage.ServerSnapshot{
		ID:        snapshotID,
		ServerID:  server.ID,
		Name:      name,
		Reason:    reason,
		FilePath:  archivePath,
		SizeBytes: size,
		FileCount: count,
		CreatedAt: time.Now().UTC(),
	}

	if e.store != nil {
		if err := e.store.CreateSnapshot(ctx, snapshot); err != nil {
			e.log.Warn("Failed to persist snapshot record in database: %v", err)
		}
		// Prune older snapshots according to retention policy
		_, _ = e.PruneSnapshots(ctx, server.ID)
	}

	e.log.Info("Successfully created pre-update snapshot %s (%s, %d bytes) for server %s",
		snapshot.ID, snapshot.Name, snapshot.SizeBytes, server.Name)
	return snapshot, nil
}

// Rollback restores a server to the specified snapshot state
func (e *Engine) Rollback(ctx context.Context, server *storage.Server, snapshotID string) error {
	if server == nil || server.DataPath == "" {
		return fmt.Errorf("server or data path cannot be empty")
	}

	var snapshot *storage.ServerSnapshot
	var err error

	if snapshotID == "latest" || snapshotID == "" {
		if e.store != nil {
			snaps, err := e.store.ListSnapshots(ctx, server.ID)
			if err != nil || len(snaps) == 0 {
				return fmt.Errorf("no snapshots found for server %s: %w", server.ID, err)
			}
			snapshot = snaps[0]
		} else {
			return fmt.Errorf("store required to resolve latest snapshot")
		}
	} else {
		if e.store != nil {
			snapshot, err = e.store.GetSnapshot(ctx, snapshotID)
			if err != nil {
				return fmt.Errorf("failed to get snapshot %s: %w", snapshotID, err)
			}
		} else {
			return fmt.Errorf("store required to find snapshot %s", snapshotID)
		}
	}

	if _, err := os.Stat(snapshot.FilePath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("snapshot archive not found on disk at: %s", snapshot.FilePath)
		}
		return fmt.Errorf("failed to stat snapshot archive %s: %w", snapshot.FilePath, err)
	}

	// Verify checksum sidecar if present
	sidecarPath := snapshot.FilePath + ".sha256"
	if sidecarBytes, err := os.ReadFile(sidecarPath); err == nil {
		parts := strings.Fields(string(sidecarBytes))
		if len(parts) > 0 {
			expectedSum := parts[0]
			actualSum, err := files.SHA256File(snapshot.FilePath)
			if err != nil {
				return fmt.Errorf("failed to checksum snapshot archive: %w", err)
			}
			if !strings.EqualFold(actualSum, expectedSum) {
				return fmt.Errorf("cannot safely rollback: snapshot archive checksum mismatch (expected %s, got %s)", expectedSum, actualSum)
			}
		}
	}

	// Verify zip archive integrity before stopping container or modifying filesystem
	if err := files.VerifyZipArchive(snapshot.FilePath); err != nil {
		return fmt.Errorf("cannot safely rollback: snapshot archive failed integrity verification: %w", err)
	}

	worldRoot, err := e.worldRoot(ctx, server, true)
	if err != nil {
		return fmt.Errorf("cannot safely rollback: %w", err)
	}

	// Journal the mutation window (OSS-229/OSS-235): the per-server active
	// lock keeps this rollback from interleaving with other lifecycle
	// writers of the same world, and startup recovery observes interrupted
	// rollbacks instead of letting a retry guess.
	return operation.RunJournaled(ctx, e.store, e.log, server, "snapshot-rollback", "", func() error {
		// Lifecycle concurrency guard (prevent racing with user/reconciler operations)
		transition, err := docker.TryBeginServerTransition(server.ID)
		if err != nil {
			return fmt.Errorf("cannot safely rollback: server %s is currently undergoing another transition", server.ID)
		}
		defer transition.Release()

		dockerCli, err := e.resolveDocker(server.NodeID)
		if err != nil {
			// Abort before stopping the container or touching files: the
			// snapshot must not be applied while the server's node cannot be
			// reached.
			return fmt.Errorf("cannot safely rollback: %w", err)
		}

		// Establish the live container state before any write: the database
		// status is not evidence of what the daemon is doing (OSS-235).
		wasRunning := false
		if server.ContainerID != "" {
			if dockerCli == nil {
				return fmt.Errorf("cannot safely rollback: container %s exists but docker client is unavailable", server.ContainerID)
			}
			status, serr := dockerCli.GetContainerStatus(ctx, server.ContainerID)
			if serr != nil {
				return fmt.Errorf("cannot safely rollback: cannot verify server state: %w", serr)
			}
			if status == storage.StatusError {
				return fmt.Errorf("cannot safely rollback: server state is unknown")
			}
			if status == storage.StatusRunning || status == storage.StatusUnhealthy || status == storage.StatusStarting || status == storage.StatusPaused {
				wasRunning = true
				e.log.Info("Stopping container %s before rollback...", server.ContainerID)
				if _, err := dockerCli.StopContainer(ctx, server.ContainerID); err != nil {
					return fmt.Errorf("cannot safely rollback: failed to stop running container %s: %w", server.ContainerID, err)
				}
			}
		}

		e.log.Info("Rolling back server %s to snapshot %s (%s)...", server.Name, snapshot.ID, snapshot.Name)

		// Restore through a clean staged swap: stale files from the current
		// tree never survive the rollback, and a failed extract leaves it
		// untouched (OSS-235).
		extracted, err := files.ExtractArchiveAtomic(ctx, snapshot.FilePath, worldRoot)
		if err != nil {
			return fmt.Errorf("failed to extract snapshot archive: %w", err)
		}

		e.log.Info("Restored %d files to %s from snapshot %s", extracted, worldRoot, snapshot.ID)

		// Restart container if it was running before rollback
		if wasRunning && dockerCli != nil && server.ContainerID != "" {
			e.log.Info("Restarting container %s following snapshot rollback...", server.ContainerID)
			if err := dockerCli.StartContainer(ctx, server.ContainerID); err != nil {
				return fmt.Errorf("rollback succeeded but failed to restart container %s: %w", server.ContainerID, err)
			}
			server.Status = storage.StatusStarting
			now := time.Now().UTC()
			server.LastStarted = &now
			if e.store != nil {
				_ = e.store.UpdateServer(ctx, server)
			}
		}

		return nil
	})
}

// ListSnapshots returns all snapshots for a server
func (e *Engine) ListSnapshots(ctx context.Context, serverID string) ([]*storage.ServerSnapshot, error) {
	if e.store == nil {
		return nil, fmt.Errorf("store not available")
	}
	return e.store.ListSnapshots(ctx, serverID)
}

// DeleteSnapshot removes a snapshot from DB and disk
func (e *Engine) DeleteSnapshot(ctx context.Context, snapshotID string) error {
	if e.store == nil {
		return fmt.Errorf("store not available")
	}
	snap, err := e.store.GetSnapshot(ctx, snapshotID)
	if err != nil {
		return err
	}

	_ = os.Remove(snap.FilePath)
	_ = os.Remove(snap.FilePath + ".sha256")
	return e.store.DeleteSnapshot(ctx, snapshotID)
}

// PruneSnapshots enforces retention limits (max count and age) for a server.
func (e *Engine) PruneSnapshots(ctx context.Context, serverID string) (int, error) {
	if e.store == nil || (e.maxSnapshots <= 0 && e.retentionDays <= 0) {
		return 0, nil
	}
	snaps, err := e.store.ListSnapshots(ctx, serverID)
	if err != nil {
		return 0, err
	}
	if len(snaps) == 0 {
		return 0, nil
	}

	toDelete := make(map[string]*storage.ServerSnapshot)

	// Count-based pruning: keep newest maxSnapshots
	if e.maxSnapshots > 0 && len(snaps) > e.maxSnapshots {
		for _, s := range snaps[e.maxSnapshots:] {
			toDelete[s.ID] = s
		}
	}

	// Age-based pruning: keep at least 1 most recent snapshot regardless of age
	if e.retentionDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -e.retentionDays)
		for _, s := range snaps[1:] {
			if s.CreatedAt.Before(cutoff) {
				toDelete[s.ID] = s
			}
		}
	}

	pruned := 0
	var firstErr error
	for _, s := range toDelete {
		e.log.Info("Pruning expired snapshot %s (%s)", s.ID, s.Name)
		if err := os.Remove(s.FilePath); err != nil && !os.IsNotExist(err) {
			if firstErr == nil {
				firstErr = err
			}
		}
		_ = os.Remove(s.FilePath + ".sha256")
		if err := e.store.DeleteSnapshot(ctx, s.ID); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		} else {
			pruned++
		}
	}
	return pruned, firstErr
}

// pruneSnapshots is an internal helper retained for compatibility.
func (e *Engine) pruneSnapshots(ctx context.Context, serverID string) {
	_, _ = e.PruneSnapshots(ctx, serverID)
}

// PruneAllSnapshots runs retention pruning across all registered servers.
func (e *Engine) PruneAllSnapshots(ctx context.Context) (int, error) {
	if e.store == nil || (e.maxSnapshots <= 0 && e.retentionDays <= 0) {
		return 0, nil
	}
	servers, err := e.store.ListServers(ctx)
	if err != nil {
		return 0, err
	}
	totalPruned := 0
	var firstErr error
	for _, srv := range servers {
		pruned, err := e.PruneSnapshots(ctx, srv.ID)
		totalPruned += pruned
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return totalPruned, firstErr
}

// worldRoot is the controller-side directory that holds this server's files.
// Local nodes keep DataPath. Remote nodes use the shared-storage mount, and
// a remote node with no reachable storage is refused.
func (e *Engine) worldRoot(ctx context.Context, server *storage.Server, write bool) (string, error) {
	if nodestorage.IsLegacyLocalNodeID(server.NodeID) {
		return server.DataPath, nil
	}
	if e.store == nil {
		return "", fmt.Errorf("store is required to resolve node storage")
	}
	node, err := e.store.GetNode(ctx, server.NodeID)
	if err != nil {
		return "", fmt.Errorf("resolve storage node %q: %w", server.NodeID, err)
	}
	if write {
		return nodestorage.EnsureDataRoot(node.StorageInfo(), server.DataPath)
	}
	return nodestorage.MapDataPath(node.StorageInfo(), server.DataPath)
}

// gatherVolumePaths collects paths inside dataDir relative to dataDir, skipping temp/git caches
func gatherVolumePaths(dataDir string) ([]string, error) {
	var paths []string

	err := filepath.WalkDir(dataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == dataDir {
			return nil
		}

		name := d.Name()
		// Skip internal caches, git repos, temp dirs
		if d.IsDir() {
			if name == ".git" || name == ".carbon-panel_modpack_git" ||
				name == ".carbon-panel_modpack_staged" || name == ".carbon-panel_snapshots" ||
				name == "logs" || name == "crash-reports" {
				return fs.SkipDir
			}
		}

		rel, err := filepath.Rel(dataDir, path)
		if err != nil {
			return nil
		}
		paths = append(paths, rel)
		return nil
	})

	return paths, err
}

// pauseWorldSaves freezes world saves through the shared quiesce rules
// (QuiesceWorld); the consistency level says what the snapshot may claim.
func (e *Engine) pauseWorldSaves(ctx context.Context, server *storage.Server) (func() error, Consistency) {
	return QuiesceWorld(ctx, e.sender, e.log, server)
}
