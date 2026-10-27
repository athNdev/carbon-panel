// SPDX-License-Identifier: MIT AND MPL-2.0
// This file includes DiscoPanel material under the MIT License,
// Copyright (c) 2025 Nicholas W. Heyer. The MIT license text is in LICENSE
// and still applies to that inherited material.
// Maintainer changes are licensed under the Mozilla Public License, v. 2.0.
// If a copy of the MPL was not distributed with this file, You can obtain
// one at https://mozilla.org/MPL/2.0/.

package scheduler

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	s3uploader "github.com/athNdev/carbon-panel/internal/s3"
	"github.com/athNdev/carbon-panel/internal/worldstore"
	"github.com/athNdev/carbon-panel/pkg/files"
	"github.com/google/uuid"
)

// BackupTaskConfig represents configuration for backup tasks
type BackupTaskConfig struct {
	BackupName    string   `json:"backup_name"`
	Paths         []string `json:"paths"`
	Compress      bool     `json:"compress"`
	RetentionDays int      `json:"retention_days"`
	MaxBackups    int      `json:"max_backups"`
	MinBackups    int      `json:"min_backups"`
	// Exclude is extra globs on top of defaultBackupExcludes.
	Exclude []string `json:"exclude"`
	// StopForBackup stops a running or unhealthy server for the archive and
	// starts it again afterwards, including when the archive fails. A failed
	// restart is returned with the backup error so the server is not left
	// stopped silently.
	StopForBackup bool `json:"stop_for_backup"`
}

func secureBackupArtifact(path string) error {
	return os.Chmod(path, 0600)
}

// defaultBackupExcludes drops lock files, rotated logs, map tiles, and
// profiler temp files from every scheduled archive.
var defaultBackupExcludes = []string{
	"session.lock",
	"logs/*.gz",
	"dynmap/web/tiles",
	"spark/tmp",
}

// Archives server data into the configured backup directory.
// When the server is running, world saves are paused and flushed over RCON
// first so the files on disk are consistent, then re-enabled afterwards.
func (s *Scheduler) executeBackupTask(ctx context.Context, server *storage.Server, task *storage.ScheduledTask) (output string, err error) {
	var config BackupTaskConfig
	if task.Config != "" {
		if err := json.Unmarshal([]byte(task.Config), &config); err != nil {
			return "", fmt.Errorf("invalid backup config: %w", err)
		}
	}

	if s.appConfig == nil || s.appConfig.Storage.BackupDir == "" {
		return "", fmt.Errorf("backup directory is not configured")
	}
	if server.DataPath == "" {
		return "", fmt.Errorf("server has no data directory")
	}

	// Explicit path requests validate their configured paths up front; the
	// default backup covers the full state tree through the world-data seam.
	var paths, missing []string
	if len(config.Paths) > 0 {
		var pathErr error
		paths, missing, pathErr = resolveBackupPaths(server.DataPath, config.Paths)
		if pathErr != nil {
			return "", pathErr
		}
	}

	// Backups are grouped per server using the unique server data directory name
	destDir := filepath.Join(s.appConfig.Storage.BackupDir, filepath.Base(server.DataPath))
	if err := os.MkdirAll(destDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}
	if err := os.Chmod(destDir, 0700); err != nil {
		return "", fmt.Errorf("failed to secure backup directory: %w", err)
	}

	backupName := config.BackupName
	if backupName == "" {
		backupName = task.Name
	}
	prefix := files.SanitizePathName(backupName)
	destPath := filepath.Join(destDir, fmt.Sprintf("%s_%s.zip", prefix, time.Now().UTC().Format("20060102-150405")))

	resumeSaves := s.pauseWorldSaves(ctx, server)
	defer resumeSaves()

	restart := false
	if config.StopForBackup {
		restart, err = s.stopServerForBackup(ctx, server)
		if err != nil {
			return "", err
		}
	}
	if restart {
		defer func() {
			if startErr := s.startServerAfterBackup(ctx, server); startErr != nil {
				if err != nil {
					err = fmt.Errorf("%w; restart after backup failed: %v", err, startErr)
					return
				}
				err = fmt.Errorf("backup written but restart failed: %w", startErr)
			}
		}()
	}

	start := time.Now()
	excludes := append(append([]string{}, defaultBackupExcludes...), config.Exclude...)
	var count int
	if len(config.Paths) == 0 {
		// Default backups run through the world-data seam (worldstore): a
		// deterministic manifest archive covering the full server state tree
		// minus excludes, streamed to disk. See ADR 0004.
		owner, oerr := docker.LoadOrCreateInstallationID(s.appConfig.Storage.DataDir)
		if oerr != nil {
			return "", fmt.Errorf("installation identity: %w", oerr)
		}
		ws, werr := worldstore.OpenServerWorlds(owner, server.ID, server.NodeID,
			filepath.Join(s.appConfig.Storage.DataDir, "worldstore-journal"), server.DataPath)
		if werr != nil {
			return "", fmt.Errorf("open world store: %w", werr)
		}
		defer func() { _ = ws.Close() }()
		if rerr := ws.Recover(); rerr != nil {
			return "", fmt.Errorf("world store recovery: %w", rerr)
		}
		if _, cerr := ws.BackupTo(ws.Identity(), destPath, func(rel string) bool {
			return files.PathExcluded(rel, excludes)
		}); cerr != nil {
			return "", fmt.Errorf("failed to create backup archive: %w", cerr)
		}
		if zr, zerr := zip.OpenReader(destPath); zerr == nil {
			for _, f := range zr.File {
				if strings.TrimSuffix(f.Name, "/") != ".worldstore-manifest.json" {
					count++
				}
			}
			_ = zr.Close()
		}
	} else {
		// Explicit path requests keep the legacy zip pipeline so existing
		// custom-path tasks behave exactly as before (ADR 0004).
		var cerr error
		count, cerr = files.CreateZipArchive(paths, server.DataPath, destPath, config.Compress, func(rel string) bool {
			return files.PathExcluded(rel, excludes)
		})
		if cerr != nil {
			return "", fmt.Errorf("failed to create backup archive: %w", cerr)
		}
	}
	if err := secureBackupArtifact(destPath); err != nil {
		return "", fmt.Errorf("failed to secure backup archive: %w", err)
	}

	var size int64
	if info, err := os.Stat(destPath); err == nil {
		size = info.Size()
	}

	// Integrity proof (MINE-146): checksum sidecar + CRC verification.
	sum, sumErr := sha256File(destPath)
	if sumErr != nil {
		return "", fmt.Errorf("failed to checksum backup archive: %w", sumErr)
	}
	sidecarPath := destPath + ".sha256"
	if err := os.WriteFile(sidecarPath, []byte(sum+"  "+filepath.Base(destPath)+"\n"), 0600); err != nil {
		return "", fmt.Errorf("failed to write backup checksum: %w", err)
	}
	if err := verifyZipArchive(destPath); err != nil {
		return "", fmt.Errorf("backup archive failed verification: %w", err)
	}

	// Track the archive in the database (MINE-138). A missing record must
	// never fail the backup itself.
	rec := &storage.BackupRecord{
		ID: uuid.New().String(), ServerID: server.ID,
		Name: filepath.Base(destPath), Path: destPath,
		SizeBytes: size, SHA256: sum, Status: "complete",
	}
	if recErr := s.store.CreateBackupRecord(ctx, rec); recErr != nil {
		s.log.Error("Backup: failed to record backup %s: %v", destPath, recErr)
	}

	// Offsite upload (MINE-138 slice 2). Upload failures are reported in
	// the output but never fail the local backup.
	remoteNote := ""
	if s3uploader.Enabled(s.appConfig.Storage.S3) {
		if key, uerr := s.uploadBackupToS3(ctx, server, rec, destPath); uerr != nil {
			remoteNote = fmt.Sprintf("; S3 upload failed: %v", uerr)
			s.log.Error("Backup %s: %v", destPath, uerr)
		} else {
			remoteNote = fmt.Sprintf("; uploaded to S3 (%s)", key)
		}
	}

	pruned, pruneErr := s.pruneServerBackups(ctx, server, destDir, prefix+"_", config.RetentionDays, config.MinBackups, config.MaxBackups)

	output = fmt.Sprintf("backup created: %s (%d files, %s, sha256:%s, took %s)%s",
		filepath.Base(destPath), count, formatBytes(size), sum[:12], time.Since(start).Round(time.Millisecond), remoteNote)
	if len(missing) > 0 {
		output += fmt.Sprintf("; skipped missing paths: %s", strings.Join(missing, ", "))
	}
	if pruned > 0 {
		output += fmt.Sprintf("; pruned %d old backup(s)", pruned)
	}
	if pruneErr != nil {
		output += fmt.Sprintf("; prune warning: %v", pruneErr)
	}
	return output, nil
}

// Validates the requested paths against the server data
// directory and returns the relative paths to archive plus any that were
// configured but do not exist. An empty request means the server's world directory.
func resolveBackupPaths(dataPath string, requested []string) (paths []string, missing []string, err error) {
	cleaned := make([]string, 0, len(requested))
	for _, p := range requested {
		rel := filepath.Clean(strings.TrimSpace(p))
		if rel == "" || rel == "." {
			continue
		}
		if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, nil, fmt.Errorf("invalid backup path %q: must be relative to the server directory", p)
		}
		cleaned = append(cleaned, rel)
	}

	if len(cleaned) == 0 {
		// Default backup: archive the server's world directory and any dimension siblings
		worldDirs, err := files.FindWorldDirs(dataPath)
		if err != nil {
			return nil, nil, fmt.Errorf("no world directory found to back up (configure explicit paths for a custom layout): %w", err)
		}
		for _, dir := range worldDirs {
			rel, err := filepath.Rel(dataPath, dir)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to resolve world directory: %w", err)
			}
			paths = append(paths, rel)
		}
		return paths, nil, nil
	}

	for _, rel := range cleaned {
		if _, err := os.Stat(filepath.Join(dataPath, rel)); err != nil {
			missing = append(missing, rel)
			continue
		}
		paths = append(paths, rel)
	}
	if len(paths) == 0 {
		return nil, nil, fmt.Errorf("none of the configured backup paths exist in the server directory")
	}
	return paths, missing, nil
}

// Disables auto-saving and flushes pending world writes so the files on disk are consistent while they are archived.
// The returned function re-enables saving and is safe to call even when the server was offline or the save commands failed.
func (s *Scheduler) pauseWorldSaves(ctx context.Context, server *storage.Server) func() {
	if server.Status != storage.StatusRunning || server.ContainerID == "" {
		return func() {}
	}

	pauseCtx, pauseCancel := context.WithTimeout(ctx, 30*time.Second)
	defer pauseCancel()

	if _, err := s.sender.SendCommand(pauseCtx, server.ID, "save-off"); err != nil {
		s.log.Warn("Backup: failed to disable world saves on server %s (continuing anyway): %v", server.Name, err)
		return func() {}
	}

	if _, err := s.sender.SendCommand(pauseCtx, server.ID, "save-all flush"); err != nil {
		s.log.Warn("Backup: failed to flush world saves on server %s: %v", server.Name, err)
	} else {
		// Give the server a moment to finish writing chunks to disk
		select {
		case <-pauseCtx.Done():
		case <-time.After(3 * time.Second):
		}
	}

	return func() {
		// Use a fresh context so saves are re-enabled even if the task was cancelled or timed out
		resumeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := s.sender.SendCommand(resumeCtx, server.ID, "save-on"); err != nil {
			s.log.Error("Backup: failed to re-enable world saves on server %s: %v", server.Name, err)
		}
	}
}

// pruneServerBackups enforces backup retention rules (retentionDays, minBackups, maxBackups)
// across both database records (including remote S3 copies) and local disk files.
// Files and records marked locked or ending in .locked.zip are sticky and never pruned.
func (s *Scheduler) pruneServerBackups(ctx context.Context, server *storage.Server, destDir, prefix string, retentionDays, minBackups, maxBackups int) (int, error) {
	if retentionDays <= 0 && maxBackups <= 0 {
		return 0, nil
	}

	var lockedNames map[string]bool
	totalPruned := 0
	var firstErr error

	if s.store != nil && server != nil {
		recs, err := s.store.ListBackupRecords(ctx, server.ID, 0)
		if err != nil {
			s.log.Warn("Failed to list backup records for pruning: %v", err)
		} else {
			lockedNames = make(map[string]bool)
			var eligible []*storage.BackupRecord

			for _, r := range recs {
				isLocked := r.Locked || strings.HasSuffix(r.Name, ".locked.zip")
				if isLocked {
					lockedNames[r.Name] = true
					if r.Path != "" {
						lockedNames[filepath.Base(r.Path)] = true
					}
					continue
				}
				if prefix != "" && !strings.HasPrefix(r.Name, prefix) && (r.Path == "" || !strings.HasPrefix(filepath.Base(r.Path), prefix)) {
					continue
				}
				eligible = append(eligible, r)
			}

			// ListBackupRecords is newest-first (Order("created_at DESC")).
			toDelete := make(map[string]*storage.BackupRecord)
			if maxBackups > 0 && len(eligible) > maxBackups {
				for _, r := range eligible[maxBackups:] {
					toDelete[r.ID] = r
				}
			}
			if retentionDays > 0 {
				cutoff := time.Now().AddDate(0, 0, -retentionDays)
				protected := max(minBackups, 1)
				if len(eligible) > protected {
					for _, r := range eligible[protected:] {
						if r.CreatedAt.Before(cutoff) {
							toDelete[r.ID] = r
						}
					}
				}
			}

			var up *s3uploader.Uploader
			if s3uploader.Enabled(s.appConfig.Storage.S3) {
				up, _ = s3uploader.NewUploader(s.appConfig.Storage.S3)
			}

			for _, r := range toDelete {
				// Delete local archive and sha256 sidecar if present
				if r.Path != "" {
					_ = os.Remove(r.Path)
					_ = os.Remove(r.Path + ".sha256")
				}
				if destDir != "" && r.Name != "" {
					p := filepath.Join(destDir, r.Name)
					_ = os.Remove(p)
					_ = os.Remove(p + ".sha256")
				}
				// Delete remote S3 backup if present
				if r.RemoteKey != "" && up != nil {
					if err := up.Delete(ctx, r.RemoteKey); err != nil && s.log != nil {
						s.log.Warn("Failed to delete remote S3 backup %s during pruning: %v", r.RemoteKey, err)
					}
				}
				// Delete database record
				if err := s.store.DeleteBackupRecord(ctx, r.ID); err != nil {
					if firstErr == nil {
						firstErr = fmt.Errorf("failed to delete backup record: %w", err)
					}
				} else {
					totalPruned++
				}
			}
		}
	}

	// Clean up any unrecorded or legacy files on disk matching the prefix
	diskPruned, diskErr := pruneBackups(destDir, prefix, retentionDays, minBackups, maxBackups, lockedNames)
	if diskErr != nil && firstErr == nil {
		firstErr = diskErr
	}
	if s.store == nil {
		totalPruned = diskPruned
	}

	return totalPruned, firstErr
}

// Removes old backups matching prefix in dir, keeping at most maxBackups (0 = unlimited) and dropping any older than retentionDays.
// Age-based expiry never reduces the backup count below minBackups (at minimum the most recent backup is always kept),
// while maxBackups is a hard cap that takes precedence over minBackups.
// Files ending in .locked.zip are sticky: never pruned (MINE-146).
// Returns the number of backups removed.
func pruneBackups(dir, prefix string, retentionDays, minBackups, maxBackups int, lockedNames ...map[string]bool) (int, error) {
	if retentionDays <= 0 && maxBackups <= 0 {
		return 0, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("failed to read backup directory: %w", err)
	}

	type backupFile struct {
		name    string
		modTime time.Time
	}
	var backups []backupFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), ".zip") {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".locked.zip") {
			continue // sticky backup: exempt from pruning
		}
		locked := false
		for _, lm := range lockedNames {
			if lm != nil && (lm[entry.Name()] || lm[strings.TrimSuffix(entry.Name(), ".zip")]) {
				locked = true
				break
			}
		}
		if locked {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		backups = append(backups, backupFile{name: entry.Name(), modTime: info.ModTime()})
	}

	// Newest first
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].modTime.After(backups[j].modTime)
	})

	toDelete := make(map[string]bool)
	if maxBackups > 0 {
		for _, b := range backups[min(maxBackups, len(backups)):] {
			toDelete[b.name] = true
		}
	}
	if retentionDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -retentionDays)
		// Keep the newest minBackups regardless of age (always at least the most recent backup)
		protected := max(minBackups, 1)
		for _, b := range backups[min(protected, len(backups)):] {
			if b.modTime.Before(cutoff) {
				toDelete[b.name] = true
			}
		}
	}

	pruned := 0
	var firstErr error
	for name := range toDelete {
		p := filepath.Join(dir, name)
		if err := os.Remove(p); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		_ = os.Remove(p + ".sha256")
		pruned++
	}
	return pruned, firstErr
}

// uploadBackupToS3 offloads a finished archive and records the object key.
// With DeleteLocalCopy it removes the local files after a verified upload.
func (s *Scheduler) uploadBackupToS3(ctx context.Context, server *storage.Server, rec *storage.BackupRecord, destPath string) (string, error) {
	cfg := s.appConfig.Storage.S3
	up, err := s3uploader.NewUploader(cfg)
	if err != nil {
		return "", err
	}
	key, err := up.Upload(ctx, server.ID, destPath)
	if err != nil {
		return "", err
	}
	rec.RemoteKey = key
	if err := s.store.CommitBackupRemote(ctx, rec.ID, key, cfg.DeleteLocalCopy); err != nil {
		return "", fmt.Errorf("s3 object %s stored but the backup row was not updated; local copy kept: %w", key, err)
	}
	if cfg.DeleteLocalCopy {
		_ = os.Remove(destPath)
		_ = os.Remove(destPath + ".sha256")
		rec.Path = ""
	}
	return key, nil
}

// stopServerForBackup stops a running or unhealthy container. A stopped
// server is left alone. The caller starts it again after the archive,
// including on failure.
func (s *Scheduler) stopServerForBackup(ctx context.Context, server *storage.Server) (bool, error) {
	if server.ContainerID == "" {
		return false, nil
	}
	dockerCli, err := s.dockerForNode(server.NodeID)
	if err != nil {
		return false, fmt.Errorf("stop_for_backup: %w", err)
	}
	if dockerCli == nil {
		return false, fmt.Errorf("stop_for_backup: docker is unavailable")
	}
	status, err := dockerCli.GetContainerStatus(ctx, server.ContainerID)
	if err != nil {
		return false, fmt.Errorf("stop_for_backup: %w", err)
	}
	if status != storage.StatusRunning && status != storage.StatusUnhealthy {
		return false, nil
	}
	if _, err := dockerCli.StopContainer(ctx, server.ContainerID); err != nil {
		return false, fmt.Errorf("stop_for_backup: %w", err)
	}
	return true, nil
}

func (s *Scheduler) startServerAfterBackup(ctx context.Context, server *storage.Server) error {
	dockerCli, err := s.dockerForNode(server.NodeID)
	if err != nil {
		return fmt.Errorf("docker is unavailable: %w", err)
	}
	if dockerCli == nil || server.ContainerID == "" {
		return fmt.Errorf("docker is unavailable")
	}
	if err := dockerCli.StartContainer(ctx, server.ContainerID); err != nil {
		return err
	}
	server.Status = storage.StatusStarting
	return s.store.UpdateServer(ctx, server)
}

// sha256File returns the hex SHA256 of a file.
func sha256File(path string) (string, error) {
	return files.SHA256File(path)
}

// verifyZipArchive opens the archive and fully reads every entry so stored
// CRCs are validated (MINE-146 integrity proof).
func verifyZipArchive(path string) error {
	return files.VerifyZipArchive(path)
}

func formatBytes(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(div), "KMGTPE"[exp])
}
