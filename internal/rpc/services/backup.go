// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"connectrpc.com/connect"

	"github.com/athNdev/carbon-panel/internal/auth"
	"github.com/athNdev/carbon-panel/internal/backupcrypt"
	appconfig "github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/internal/nodestorage"
	"github.com/athNdev/carbon-panel/internal/operation"
	"github.com/athNdev/carbon-panel/internal/rbac"
	s3uploader "github.com/athNdev/carbon-panel/internal/s3"
	"github.com/athNdev/carbon-panel/internal/worldstore"
	"github.com/athNdev/carbon-panel/pkg/files"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
	"github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1/carbonpanelv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var _ carbonpanelv1connect.BackupServiceHandler = (*BackupService)(nil)

// BackupService manages on-demand backup records (MINE-138).
type BackupService struct {
	store            *storage.Store
	docker           *docker.Client
	pool             *docker.ClientPool
	s3cfg            appconfig.S3Config
	log              *logger.Logger
	enforcer         *rbac.Enforcer
	restoringMu      sync.Mutex
	restoringServers map[string]bool
	worldDataDir     string
	// backupKey is the authenticated-encryption key for backup artifacts
	// (OSS-236). Nil is the explicit disabled-encryption state.
	backupKey *backupcrypt.Key
	// remoteDelete overrides the S3 delete in tests. Production leaves it nil.
	remoteDelete func(ctx context.Context, key string) error
}

// deleteRemoteBackup removes the offsite object before the local archive and
// the database row. With no remote key, or with S3 disabled and no test
// override, it is a no-op.
func (s *BackupService) deleteRemoteBackup(ctx context.Context, key string) error {
	if key == "" {
		return nil
	}
	if s.remoteDelete != nil {
		return s.remoteDelete(ctx, key)
	}
	if !s3uploader.Enabled(s.s3cfg) {
		return nil
	}
	up, err := s3uploader.NewUploader(s.s3cfg)
	if err != nil {
		return err
	}
	return up.Delete(ctx, key)
}

func NewBackupService(store *storage.Store, dockerCli *docker.Client, pool *docker.ClientPool, s3cfg appconfig.S3Config, log *logger.Logger) *BackupService {
	return &BackupService{
		store:            store,
		docker:           dockerCli,
		pool:             pool,
		s3cfg:            s3cfg,
		log:              log,
		restoringServers: make(map[string]bool),
	}
}

// SetEnforcer injects the RBAC enforcer used for per-server authorization on
// backup records. Kept as a setter so the constructor signature (and therefore
// every existing call site) stays stable.
// SetWorldDataDir configures the world-data seam for manifest'd backups:

// SetBackupEncryption configures the authenticated-encryption key for backup
// artifacts (OSS-236). Empty key material is the explicit disabled state.
func (s *BackupService) SetBackupEncryption(keyID, key string) error {
	resolved, err := backupcrypt.ResolveKey(keyID, key)
	if err != nil {
		return err
	}
	s.backupKey = resolved
	return nil
}

// the controller data directory that owns installation identity and the
// worldstore journal. Production must set it; without it, restores of
// worldstore-format archives are refused rather than silently degraded.
func (s *BackupService) SetWorldDataDir(dataDir string) {
	s.worldDataDir = dataDir
}

func (s *BackupService) SetEnforcer(e *rbac.Enforcer) {
	s.enforcer = e
}

// authorizeServerBackup enforces that the caller may perform `action` on the
// SERVER that owns the given backup.
//
// Why this exists: DeleteBackup, RestoreBackup and SetBackupLocked carry a
// backup id in their request, not a server id, so the interceptor cannot
// resolve a per-server ObjectIDField and falls back to enforcing against "*".
// That means a caller holding `backups:delete` anywhere could delete or restore
// a backup belonging to a server they have no rights on - a cross-server
// escalation. This resolves the owning server from the record and enforces
// against it, including the same subuser-grant fallback the interceptor applies
// for server-scoped procedures (MINE-139), so a legitimately granted subuser
// still works.
func (s *BackupService) authorizeServerBackup(ctx context.Context, serverID string, action string) error {
	if s.enforcer == nil {
		// Fail closed, matching the interceptor: an unavailable enforcer must
		// not silently skip the check.
		return connect.NewError(connect.CodePermissionDenied,
			errors.New("rbac enforcer unavailable"))
	}
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	if serverID == "" {
		return connect.NewError(connect.CodeInternal, errors.New("backup record has no server id"))
	}

	allowed, err := s.enforcer.Enforce(user.Roles, rbac.ResourceBackups, action, serverID)
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("rbac enforcement error: %w", err))
	}
	if !allowed {
		// Subuser grant fallback, mirroring the interceptor's server_id path.
		want := rbac.ResourceBackups + "." + action
		for _, p := range s.store.SubuserPermissions(ctx, serverID, user.ID) {
			if p == want {
				allowed = true
				break
			}
		}
	}
	if !allowed {
		return connect.NewError(connect.CodePermissionDenied,
			fmt.Errorf("insufficient permissions for %s/%s on this server", rbac.ResourceBackups, action))
	}
	return nil
}

func (s *BackupService) dockerFor(nodeID string) (*docker.Client, error) {
	return docker.NewLocator(s.pool, s.docker, s.store).For(context.Background(), nodeID)
}

func dbBackupToProto(b *storage.BackupRecord) *v1.BackupRecord {
	return &v1.BackupRecord{
		Id: b.ID, ServerId: b.ServerID, Name: b.Name,
		SizeBytes: b.SizeBytes, Sha256: b.SHA256, Locked: b.Locked,
		Status: b.Status, CreatedAt: timestamppb.New(b.CreatedAt),
	}
}

// newBackupDownloadPath creates a private, uniquely named staging directory
// for a remote restore. Do not derive this path from a database-controlled
// backup name: a malformed record must never influence where S3 writes.
func newBackupDownloadPath() (string, func(), error) {
	dir, err := os.MkdirTemp("", "carbon-panel-backup-")
	if err != nil {
		return "", nil, err
	}
	return filepath.Join(dir, "archive.zip"), func() {
		_ = os.RemoveAll(dir)
	}, nil
}

func (s *BackupService) ListBackups(ctx context.Context, req *connect.Request[v1.ListBackupsRequest]) (*connect.Response[v1.ListBackupsResponse], error) {
	recs, err := s.store.ListBackupRecords(ctx, req.Msg.ServerId, clampLimit(int(req.Msg.Limit)))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list backups"))
	}
	out := make([]*v1.BackupRecord, 0, len(recs))
	for _, r := range recs {
		out = append(out, dbBackupToProto(r))
	}
	return connect.NewResponse(&v1.ListBackupsResponse{Backups: out}), nil
}

func (s *BackupService) DeleteBackup(ctx context.Context, req *connect.Request[v1.DeleteBackupRequest]) (*connect.Response[v1.DeleteBackupResponse], error) {
	rec, err := s.store.GetBackupRecord(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("backup not found"))
	}
	// Resolve the owning server BEFORE touching the archive, so a caller with
	// no rights on that server cannot delete its backup.
	if err := s.authorizeServerBackup(ctx, rec.ServerID, rbac.ActionDelete); err != nil {
		return nil, err
	}
	if rec.Locked {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("backup is locked"))
	}
	if rec.Status == "restoring" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("backup is currently being restored"))
	}
	// Remote delete first. A transport or permission error must keep the row
	// and the local archive so the operator can retry (MINE-183). A missing
	// object is success: S3 DeleteObject is idempotent.
	if err := s.deleteRemoteBackup(ctx, rec.RemoteKey); err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("remote backup was kept: %w", err))
	}
	if rec.Path != "" {
		if err := os.Remove(rec.Path); err != nil && !os.IsNotExist(err) {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete archive: %w", err))
		}
		_ = os.Remove(rec.Path + ".sha256")
	}
	if err := s.store.DeleteBackupRecord(ctx, rec.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete backup record"))
	}
	return connect.NewResponse(&v1.DeleteBackupResponse{Success: true}), nil
}

func (s *BackupService) SetBackupLocked(ctx context.Context, req *connect.Request[v1.SetBackupLockedRequest]) (*connect.Response[v1.SetBackupLockedResponse], error) {
	rec, err := s.store.GetBackupRecord(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("backup not found"))
	}
	if err := s.authorizeServerBackup(ctx, rec.ServerID, rbac.ActionUpdate); err != nil {
		return nil, err
	}
	if err := s.store.SetBackupLocked(ctx, req.Msg.Id, req.Msg.Locked); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update lock"))
	}
	rec, err = s.store.GetBackupRecord(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to read backup"))
	}
	return connect.NewResponse(&v1.SetBackupLockedResponse{Backup: dbBackupToProto(rec)}), nil
}

func (s *BackupService) RestoreBackup(ctx context.Context, req *connect.Request[v1.RestoreBackupRequest]) (*connect.Response[v1.RestoreBackupResponse], error) {
	rec, err := s.store.GetBackupRecord(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("backup not found"))
	}
	// Authorize against the owning server before starting a restore, which
	// overwrites that server's data.
	if err := s.authorizeServerBackup(ctx, rec.ServerID, rbac.ActionUpdate); err != nil {
		return nil, err
	}
	server, err := s.store.GetServer(ctx, rec.ServerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}
	if err := requireLocalRestoreStorage(ctx, s.store, server.NodeID); err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, err)
	}
	// Restores land in the server's data root on the owning node; map it to the
	// controller-side path for nodes on shared storage (ADR 0005).
	nodeInfo, err := storageNodeInfo(ctx, s.store, server.NodeID)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, err)
	}
	worldDataPath, err := nodestorage.EnsureDataRoot(nodeInfo, server.DataPath)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, err)
	}
	transition, err := docker.TryBeginServerTransition(server.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeAborted, err)
	}
	defer transition.Release()

	s.restoringMu.Lock()
	if s.restoringServers == nil {
		s.restoringServers = make(map[string]bool)
	}
	if s.restoringServers[rec.ServerID] || rec.Status == "restoring" {
		s.restoringMu.Unlock()
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("a backup restore is already in progress for server %s", rec.ServerID))
	}
	s.restoringServers[rec.ServerID] = true
	s.restoringMu.Unlock()

	defer func() {
		s.restoringMu.Lock()
		delete(s.restoringServers, rec.ServerID)
		s.restoringMu.Unlock()
	}()

	if err := s.store.SetBackupStatus(ctx, rec.ID, "restoring"); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to mark restoring"))
	}
	done := func(status string) {
		if err := s.store.SetBackupStatus(context.Background(), rec.ID, status); err != nil {
			s.log.Error("Backup restore: failed to set status: %v", err)
		}
	}

	// S3 round-trip (MINE-138 slice 2): fetch the archive when only the
	// remote copy remains (e.g. DeleteLocalCopy was enabled).
	archivePath := rec.Path
	var cleanupDownloaded func()
	if _, err := os.Stat(archivePath); err != nil {
		if rec.RemoteKey == "" || !s3uploader.Enabled(s.s3cfg) {
			done("failed")
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("backup archive not found locally and no remote copy"))
		}
		up, err := s3uploader.NewUploader(s.s3cfg)
		if err != nil {
			done("failed")
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("s3 unavailable: %w", err))
		}
		tmp, cleanup, err := newBackupDownloadPath()
		if err != nil {
			done("failed")
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create secure backup staging path: %w", err))
		}
		if err := up.Download(ctx, rec.RemoteKey, tmp); err != nil {
			cleanup()
			done("failed")
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("s3 download failed: %w", err))
		}
		archivePath = tmp
		cleanupDownloaded = cleanup
	}

	defer func() {
		if cleanupDownloaded != nil {
			cleanupDownloaded()
			_ = os.Remove(archivePath + ".sha256")
		}
	}()

	// Verify archive integrity before taking down container or touching data files
	if rec.SHA256 != "" {
		sum, err := files.SHA256File(archivePath)
		if err != nil {
			done("failed")
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to compute backup checksum: %w", err))
		}
		if !strings.EqualFold(sum, rec.SHA256) {
			done("failed")
			return nil, connect.NewError(connect.CodeDataLoss, fmt.Errorf("backup archive checksum mismatch: expected %s, got %s", rec.SHA256, sum))
		}
	}

	// Authenticated-encrypted artifacts decrypt and authenticate before any
	// world mutation (OSS-236). Wrong or missing keys are refused up front and
	// plaintext only ever lands in secure staging.
	if isEnc, _ := backupcrypt.IsEncrypted(archivePath); isEnc {
		if s.backupKey == nil {
			done("failed")
			return nil, connect.NewError(connect.CodeFailedPrecondition, backupcrypt.ErrKeyMissing)
		}
		plainPath, cleanupPlain, perr := newBackupDownloadPath()
		if perr != nil {
			done("failed")
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create secure backup staging path: %w", perr))
		}
		if derr := backupcrypt.DecryptFile(archivePath, plainPath, s.backupKey); derr != nil {
			cleanupPlain()
			done("failed")
			return nil, connect.NewError(connect.CodeDataLoss, fmt.Errorf("cannot decrypt backup: %w", derr))
		}
		previousCleanup := cleanupDownloaded
		cleanupDownloaded = func() {
			if previousCleanup != nil {
				previousCleanup()
			}
			cleanupPlain()
		}
		archivePath = plainPath
	}

	if err := files.VerifyZipArchive(archivePath); err != nil {
		done("failed")
		return nil, connect.NewError(connect.CodeDataLoss, fmt.Errorf("backup archive corrupt or invalid: %w", err))
	}

	// The per-server operation lock serializes this restore against every
	// other lifecycle mutation of the world (OSS-229/OSS-235).
	var restoreResp *v1.RestoreBackupResponse
	jerr := operation.RunJournaled(ctx, s.store, s.log, server, "restore", "", func() error {
		var rerr error
		restoreResp, rerr = s.restoreBackupNow(ctx, server, rec, archivePath, worldDataPath, done)
		return rerr
	})
	if jerr != nil {
		done("failed")
		return nil, jerr
	}
	return connect.NewResponse(restoreResp), nil
}

// restoreBackupNow runs the journaled mutation window of a restore: verify
// the server is stopped, restore the world through the world-data seam or a
// clean staged swap, restart per the preserved intent, and verify players can
// join before calling the restore successful.
func (s *BackupService) restoreBackupNow(ctx context.Context, server *storage.Server, rec *storage.BackupRecord, archivePath, worldDataPath string, done func(string)) (*v1.RestoreBackupResponse, error) {
	dockerCli, dockerErr := s.dockerFor(server.NodeID)
	if server.ContainerID != "" && dockerErr != nil {
		done("failed")
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("docker node %q is unavailable for restore: %w", server.NodeID, dockerErr))
	}
	if server.ContainerID != "" && dockerCli == nil {
		done("failed")
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("docker node %q is unavailable for restore", server.NodeID))
	}
	wasRunning := false
	if server.ContainerID != "" && dockerCli != nil {
		status, err := dockerCli.GetContainerStatus(ctx, server.ContainerID)
		if err != nil {
			done("failed")
			return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("cannot verify server is stopped before restore: %w", err))
		}
		if status == storage.StatusError {
			done("failed")
			return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("cannot verify server state before restore"))
		}
		if status == storage.StatusRunning || status == storage.StatusUnhealthy || status == storage.StatusStarting || status == storage.StatusPaused {
			wasRunning = true
			if _, err := dockerCli.StopContainer(ctx, server.ContainerID); err != nil {
				done("failed")
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to stop server: %w", err))
			}
			status, err = dockerCli.GetContainerStatus(ctx, server.ContainerID)
			if err != nil || status != storage.StatusStopped {
				done("failed")
				if err != nil {
					return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("cannot confirm server stopped before restore: %w", err))
				}
				return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("server state is %q after stop; refusing to restore", status))
			}
		}
	}

	// Manifest'd archives are restored through the world-data seam
	// (worldstore): verified contents and one journaled atomic swap shared
	// with the CLI. Legacy zips get a clean staged swap (ADR 0004/OSS-235).
	var verifyNote string
	if worldstore.HasWorldstoreManifest(archivePath) {
		if s.worldDataDir == "" {
			done("failed")
			return nil, connect.NewError(connect.CodeInternal, errors.New("world-data seam is not configured"))
		}
		owner, oerr := docker.LoadOrCreateInstallationID(s.worldDataDir)
		if oerr != nil {
			done("failed")
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("installation identity: %w", oerr))
		}
		ws, werr := worldstore.OpenServerWorlds(owner, server.ID, server.NodeID,
			filepath.Join(s.worldDataDir, "worldstore-journal"), worldDataPath)
		if werr != nil {
			done("failed")
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("open world store: %w", werr))
		}
		defer func() { _ = ws.Close() }()
		if rerr := ws.Recover(); rerr != nil {
			done("failed")
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("world store recovery: %w", rerr))
		}
		if rerr := ws.RestoreFrom(ws.Identity(), archivePath); rerr != nil {
			done("failed")
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to restore backup: %w", rerr))
		}
		verifyNote = "world verified against the archive's per-file sha256"
	} else {
		// Legacy archives restore through a clean staged swap: stale files
		// never survive a successful restore, and a failed extract leaves the
		// current world untouched.
		count, xerr := files.ExtractArchiveAtomic(ctx, archivePath, worldDataPath)
		if xerr != nil {
			done("failed")
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to extract backup: %w", xerr))
		}
		verifyNote = fmt.Sprintf("world verified: %d files extracted with matching CRC and swapped in cleanly", count)
	}

	if wasRunning && server.ContainerID != "" && dockerCli != nil {
		if err := dockerCli.StartContainer(ctx, server.ContainerID); err != nil {
			done("failed")
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("backup restored but restart failed: %w", err))
		}
		server.Status = storage.StatusStarting
		_ = s.store.UpdateServer(ctx, server)
	}

	// Recovery evidence, not just a checksum: report how the world data was
	// verified and whether players can join the restored server (OSS-235).
	msg := "backup restored; " + verifyNote
	if wasRunning && server.ContainerID != "" {
		node, _ := s.store.GetNode(ctx, server.NodeID)
		if node == nil {
			node = &storage.Node{}
		}
		endpoint := gameEndpoint(server, node, 0)
		if perr := waitForPlayable(ctx, endpoint, restorePlayabilityTimeout); perr != nil {
			msg += fmt.Sprintf("; player connections not yet verified on %s (server may still be starting)", endpoint)
		} else {
			msg += "; accepting player connections on " + endpoint
		}
	}
	done("complete")
	return &v1.RestoreBackupResponse{Success: true, Message: msg}, nil
}

// requireLocalRestoreStorage prevents a controller-local archive extraction
// from being mistaken for a restore on a remote Docker host without reachable
// storage. Local nodes and remote nodes on shared storage (ADR 0005) pass;
// other remote nodes need their storage adapter wired first.
func requireLocalRestoreStorage(ctx context.Context, store *storage.Store, nodeID string) error {
	// Preserve the explicit legacy aliases accepted by getDockerClientStrict.
	// They designate the configured local daemon, whose storage is the
	// controller's DataPath on the standalone/local deployment path.
	if nodeID == "local" || nodeID == "default" {
		return nil
	}
	if store == nil || nodeID == "" {
		return fmt.Errorf("cannot identify owning storage for node %q; restore refused", nodeID)
	}
	node, err := store.GetNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("cannot identify owning storage for node %q: %w", nodeID, err)
	}
	if !node.IsLocal && node.StorageRoot == "" && node.ControllerStorageRoot == "" {
		return fmt.Errorf("backup restore for remote node %q is unavailable until node-aware storage is configured", nodeID)
	}
	return nil
}
