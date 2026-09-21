package services

import (
	"context"
	"fmt"
	"os"

	"connectrpc.com/connect"

	appconfig "github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	s3uploader "github.com/athNdev/carbon-panel/internal/s3"
	"github.com/athNdev/carbon-panel/pkg/files"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
	"github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1/carbonpanelv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var _ carbonpanelv1connect.BackupServiceHandler = (*BackupService)(nil)

// BackupService manages on-demand backup records (MINE-138).
type BackupService struct {
	store  *storage.Store
	docker *docker.Client
	pool   *docker.ClientPool
	s3cfg  appconfig.S3Config
	log    *logger.Logger
}

func NewBackupService(store *storage.Store, dockerCli *docker.Client, pool *docker.ClientPool, s3cfg appconfig.S3Config, log *logger.Logger) *BackupService {
	return &BackupService{store: store, docker: dockerCli, pool: pool, s3cfg: s3cfg, log: log}
}

func (s *BackupService) dockerFor(nodeID string) *docker.Client {
	if s.pool != nil {
		if cli, err := s.pool.GetClient(nodeID); err == nil && cli != nil {
			return cli
		}
	}
	return s.docker
}

func dbBackupToProto(b *storage.BackupRecord) *v1.BackupRecord {
	return &v1.BackupRecord{
		Id: b.ID, ServerId: b.ServerID, Name: b.Name,
		SizeBytes: b.SizeBytes, Sha256: b.SHA256, Locked: b.Locked,
		Status: b.Status, CreatedAt: timestamppb.New(b.CreatedAt),
	}
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
	if rec.Locked {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("backup is locked"))
	}
	if err := os.Remove(rec.Path); err != nil && !os.IsNotExist(err) {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete archive: %w", err))
	}
	_ = os.Remove(rec.Path + ".sha256")
	if err := s.store.DeleteBackupRecord(ctx, rec.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete backup record"))
	}
	return connect.NewResponse(&v1.DeleteBackupResponse{Success: true}), nil
}

func (s *BackupService) SetBackupLocked(ctx context.Context, req *connect.Request[v1.SetBackupLockedRequest]) (*connect.Response[v1.SetBackupLockedResponse], error) {
	if _, err := s.store.GetBackupRecord(ctx, req.Msg.Id); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("backup not found"))
	}
	if err := s.store.SetBackupLocked(ctx, req.Msg.Id, req.Msg.Locked); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update lock"))
	}
	rec, err := s.store.GetBackupRecord(ctx, req.Msg.Id)
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
	server, err := s.store.GetServer(ctx, rec.ServerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}
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
		tmp := archivePath
		if tmp == "" {
			tmp = rec.Name
		}
		if err := up.Download(ctx, rec.RemoteKey, tmp); err != nil {
			done("failed")
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("s3 download failed: %w", err))
		}
		archivePath = tmp
	}

	dockerCli := s.dockerFor(server.NodeID)
	wasRunning := false
	if server.ContainerID != "" && dockerCli != nil {
		if status, err := dockerCli.GetContainerStatus(ctx, server.ContainerID); err == nil && status == storage.StatusRunning {
			wasRunning = true
			if _, err := dockerCli.StopContainer(ctx, server.ContainerID); err != nil {
				done("failed")
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to stop server: %w", err))
			}
		}
	}

	if _, err := files.ExtractArchive(ctx, archivePath, server.DataPath, nil); err != nil {
		done("failed")
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to extract backup: %w", err))
	}

	if wasRunning && server.ContainerID != "" && dockerCli != nil {
		if err := dockerCli.StartContainer(ctx, server.ContainerID); err != nil {
			done("failed")
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("backup restored but restart failed: %w", err))
		}
		server.Status = storage.StatusStarting
		_ = s.store.UpdateServer(ctx, server)
	}
	done("complete")
	return connect.NewResponse(&v1.RestoreBackupResponse{Success: true, Message: "backup restored"}), nil
}
