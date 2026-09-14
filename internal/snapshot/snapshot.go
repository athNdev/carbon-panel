package snapshot

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/athNdev/mineserver/internal/command"
	storage "github.com/athNdev/mineserver/internal/db"
	"github.com/athNdev/mineserver/internal/docker"
	"github.com/athNdev/mineserver/pkg/files"
	"github.com/athNdev/mineserver/pkg/logger"
)

// Engine manages volume snapshots, pre-update backups, and rollbacks for servers (MINE-23)
type Engine struct {
	store        *storage.Store
	docker       *docker.Client
	sender       *command.Sender
	backupDir    string
	maxSnapshots int
	log          *logger.Logger
}

// Config holds configuration for the SnapshotEngine
type Config struct {
	BackupDir    string
	MaxSnapshots int // Max pre-update snapshots to retain per server (default: 5)
}

// NewEngine creates a new SnapshotEngine
func NewEngine(store *storage.Store, dockerClient *docker.Client, sender *command.Sender, log *logger.Logger, cfg ...Config) *Engine {
	backupDir := ""
	maxSnapshots := 5
	if len(cfg) > 0 {
		backupDir = cfg[0].BackupDir
		if cfg[0].MaxSnapshots > 0 {
			maxSnapshots = cfg[0].MaxSnapshots
		}
	}
	if log == nil {
		log = logger.New()
	}

	return &Engine{
		store:        store,
		docker:       dockerClient,
		sender:       sender,
		backupDir:    backupDir,
		maxSnapshots: maxSnapshots,
		log:          log,
	}
}

// CreatePreUpdateSnapshot creates an atomic volume snapshot before updating modpacks or configs
func (e *Engine) CreatePreUpdateSnapshot(ctx context.Context, server *storage.Server, reason string) (*storage.ServerSnapshot, error) {
	if server == nil || server.DataPath == "" {
		return nil, fmt.Errorf("server data path cannot be empty")
	}

	// Determine snapshot storage directory
	baseSnapshotsDir := ""
	if e.backupDir != "" {
		baseSnapshotsDir = filepath.Join(e.backupDir, filepath.Base(server.DataPath), "snapshots")
	} else {
		baseSnapshotsDir = filepath.Join(server.DataPath, ".mineserver_snapshots")
	}

	if err := os.MkdirAll(baseSnapshotsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create snapshots directory: %w", err)
	}

	timestamp := time.Now().UTC().Format("20060102-150405")
	snapshotID := "snap-" + uuid.New().String()
	name := fmt.Sprintf("pre-update-%s", timestamp)
	archivePath := filepath.Join(baseSnapshotsDir, fmt.Sprintf("%s.zip", snapshotID))

	// Pause world saves if server is currently running
	resumeSaves := e.pauseWorldSaves(ctx, server)
	defer resumeSaves()

	// Gather all files to include in volume snapshot
	paths, err := gatherVolumePaths(server.DataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to gather volume files for snapshot: %w", err)
	}

	e.log.Info("Creating pre-update snapshot for server %s (%d files) -> %s", server.Name, len(paths), archivePath)
	count, err := files.CreateZipArchive(paths, server.DataPath, archivePath, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot archive: %w", err)
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
		// Prune older snapshots
		e.pruneSnapshots(ctx, server.ID)
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

	if _, err := os.Stat(snapshot.FilePath); os.IsNotExist(err) {
		return fmt.Errorf("snapshot archive not found on disk at: %s", snapshot.FilePath)
	}

	wasRunning := server.Status == storage.StatusRunning || server.Status == storage.StatusStarting

	// Stop container if running
	if wasRunning && e.docker != nil && server.ContainerID != "" {
		e.log.Info("Stopping container %s before rollback...", server.ContainerID)
		if _, err := e.docker.StopContainer(ctx, server.ContainerID); err != nil {
			e.log.Warn("Failed to stop container %s gracefully before rollback: %v", server.ContainerID, err)
		}
	}

	e.log.Info("Rolling back server %s to snapshot %s (%s)...", server.Name, snapshot.ID, snapshot.Name)

	// Extract snapshot files into server.DataPath
	extracted, err := files.ExtractArchive(ctx, snapshot.FilePath, server.DataPath, nil)
	if err != nil {
		return fmt.Errorf("failed to extract snapshot archive: %w", err)
	}

	e.log.Info("Restored %d files to %s from snapshot %s", extracted, server.DataPath, snapshot.ID)

	// Restart container if it was running before rollback
	if wasRunning && e.docker != nil && server.ContainerID != "" {
		e.log.Info("Restarting container %s following snapshot rollback...", server.ContainerID)
		if err := e.docker.StartContainer(ctx, server.ContainerID); err != nil {
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
	return e.store.DeleteSnapshot(ctx, snapshotID)
}

// pruneSnapshots enforces retention limit
func (e *Engine) pruneSnapshots(ctx context.Context, serverID string) {
	if e.maxSnapshots <= 0 || e.store == nil {
		return
	}
	snaps, err := e.store.ListSnapshots(ctx, serverID)
	if err != nil || len(snaps) <= e.maxSnapshots {
		return
	}

	for i := e.maxSnapshots; i < len(snaps); i++ {
		toDelete := snaps[i]
		e.log.Info("Pruning excess snapshot %s (%s)", toDelete.ID, toDelete.Name)
		_ = os.Remove(toDelete.FilePath)
		_ = e.store.DeleteSnapshot(ctx, toDelete.ID)
	}
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
			if name == ".git" || name == ".mineserver_modpack_git" ||
				name == ".mineserver_modpack_staged" || name == ".mineserver_snapshots" ||
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

// pauseWorldSaves disables auto-saving over RCON while archiving
func (e *Engine) pauseWorldSaves(ctx context.Context, server *storage.Server) func() {
	if e.sender == nil || server.Status != storage.StatusRunning || server.ContainerID == "" {
		return func() {}
	}

	if _, err := e.sender.SendCommand(ctx, server.ID, "save-off"); err != nil {
		e.log.Warn("Snapshot: failed to disable world saves on server %s: %v", server.Name, err)
		return func() {}
	}

	if _, err := e.sender.SendCommand(ctx, server.ID, "save-all flush"); err != nil {
		e.log.Warn("Snapshot: failed to flush world saves on server %s: %v", server.Name, err)
	} else {
		select {
		case <-ctx.Done():
		case <-time.After(1 * time.Second):
		}
	}

	return func() {
		resumeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := e.sender.SendCommand(resumeCtx, server.ID, "save-on"); err != nil {
			e.log.Error("Snapshot: failed to re-enable world saves on server %s: %v", server.Name, err)
		}
	}
}
