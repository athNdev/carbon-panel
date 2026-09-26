package svc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/google/uuid"
)

func backupToProto(b *db.WorkloadBackup) *v1.WorkloadBackup {
	if b == nil {
		return nil
	}
	return &v1.WorkloadBackup{
		Id:         b.ID,
		WorkloadId: b.WorkloadID,
		NodeId:     b.NodeID,
		Name:       b.Name,
		SizeBytes:  b.SizeBytes,
		Sha256:     b.SHA256,
		Locked:     b.Locked,
		Status:     b.Status,
		Error:      b.Error,
		CreatedAt:  ts(b.CreatedAt),
	}
}

// CreateWorkloadBackup creates a point-in-time snapshot of the workload on the host node.
func (s *WorkloadService) CreateWorkloadBackup(ctx context.Context, req *connect.Request[v1.CreateWorkloadBackupRequest]) (*connect.Response[v1.CreateWorkloadBackupResponse], error) {
	if req.Msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload id is required"))
	}

	w, err := s.load(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}
	if w.NodeID == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("workload is not placed on any node"))
	}
	if s.dispatcher == nil || !s.dispatcher.IsConnected(w.NodeID) {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("node agent is offline"))
	}

	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	backupID := uuid.NewString()
	name := strings.TrimSpace(req.Msg.Name)
	if name == "" {
		name = fmt.Sprintf("backup-%s", time.Now().UTC().Format("2006-01-02-150405"))
	}

	backup := &db.WorkloadBackup{
		TenantBase: db.TenantBase{
			ID:    backupID,
			OrgID: w.OrgID,
		},
		WorkloadID: w.ID,
		NodeID:     w.NodeID,
		Name:       name,
		Status:     "in_progress",
	}

	if err := q.Create(backup).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create backup record: %w", err))
	}

	cmdID := uuid.NewString()
	waiter := s.dispatcher.ExpectCreateBackup(cmdID)
	defer s.dispatcher.CancelCreateBackup(cmdID)

	ok := s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
		Payload: &v1.ControlMessage_CreateBackup{
			CreateBackup: &v1.ControlCreateBackup{
				CommandId:  cmdID,
				WorkloadId: w.ID,
				BackupId:   backupID,
				Name:       name,
			},
		},
	})
	if !ok {
		backup.Status = "failed"
		backup.Error = "failed to dispatch backup request to node agent"
		_ = s.deps.Store.Unscoped().WithContext(ctx).Model(&db.WorkloadBackup{}).Where("id = ? AND org_id = ?", backup.ID, w.OrgID).Updates(map[string]any{"status": backup.Status, "error": backup.Error})
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("failed to dispatch backup command"))
	}

	select {
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
	case <-time.After(5 * time.Minute):
		backup.Status = "failed"
		backup.Error = "timeout waiting for node agent to create backup"
		_ = s.deps.Store.Unscoped().WithContext(ctx).Model(&db.WorkloadBackup{}).Where("id = ? AND org_id = ?", backup.ID, w.OrgID).Updates(map[string]any{"status": backup.Status, "error": backup.Error})
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("backup creation timed out"))
	case res := <-waiter:
		if !res.Success {
			backup.Status = "failed"
			backup.Error = res.Error
			_ = q.Model(&db.WorkloadBackup{}).Where("id = ?", backup.ID).Updates(map[string]any{
				"status": backup.Status,
				"error":  backup.Error,
			})
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("backup failed: %s", res.Error))
		}
		backup.Status = "completed"
		backup.SizeBytes = res.SizeBytes
		backup.SHA256 = res.Sha256
		if err := s.deps.Store.Unscoped().WithContext(ctx).Model(&db.WorkloadBackup{}).Where("id = ? AND org_id = ?", backup.ID, w.OrgID).Updates(map[string]any{
			"status":     backup.Status,
			"size_bytes": backup.SizeBytes,
			"sha256":     backup.SHA256,
		}).Error; err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("update backup record: %w", err))
		}
	}

	s.recordEvent(ctx, w.ID, "backup_created", fmt.Sprintf("created backup %s (%s, %d bytes)", backup.Name, backup.ID, backup.SizeBytes))

	return connect.NewResponse(&v1.CreateWorkloadBackupResponse{
		Backup: backupToProto(backup),
	}), nil
}

// ListWorkloadBackups lists all snapshots for a workload.
func (s *WorkloadService) ListWorkloadBackups(ctx context.Context, req *connect.Request[v1.ListWorkloadBackupsRequest]) (*connect.Response[v1.ListWorkloadBackupsResponse], error) {
	if req.Msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload id is required"))
	}

	w, err := s.load(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}

	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	var backups []db.WorkloadBackup
	if err := q.Where("workload_id = ?", w.ID).Order("created_at desc").Find(&backups).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list backups: %w", err))
	}

	protoBackups := make([]*v1.WorkloadBackup, 0, len(backups))
	for i := range backups {
		protoBackups = append(protoBackups, backupToProto(&backups[i]))
	}

	return connect.NewResponse(&v1.ListWorkloadBackupsResponse{
		Backups: protoBackups,
	}), nil
}

// RestoreWorkloadBackup rolls back the workload's data to a previous snapshot.
func (s *WorkloadService) RestoreWorkloadBackup(ctx context.Context, req *connect.Request[v1.RestoreWorkloadBackupRequest]) (*connect.Response[v1.RestoreWorkloadBackupResponse], error) {
	if req.Msg.Id == "" || req.Msg.BackupId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload id and backup id are required"))
	}

	w, err := s.load(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}
	if w.NodeID == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("workload is not placed on any node"))
	}
	if s.dispatcher == nil || !s.dispatcher.IsConnected(w.NodeID) {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("node agent is offline"))
	}

	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	var backup db.WorkloadBackup
	if err := q.Where("id = ? AND workload_id = ?", req.Msg.BackupId, w.ID).First(&backup).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("backup not found"))
	}

	// Dispatch restore to node
	cmdID := uuid.NewString()
	waiter := s.dispatcher.ExpectRestoreBackup(cmdID)
	defer s.dispatcher.CancelRestoreBackup(cmdID)

	ok := s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
		Payload: &v1.ControlMessage_RestoreBackup{
			RestoreBackup: &v1.ControlRestoreBackup{
				CommandId:  cmdID,
				WorkloadId: w.ID,
				BackupId:   backup.ID,
			},
		},
	})
	if !ok {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("failed to dispatch restore request to node agent"))
	}

	select {
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
	case <-time.After(5 * time.Minute):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("restore timed out on node"))
	case res := <-waiter:
		if !res.Success {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("restore failed on node: %s", res.Error))
		}
	}

	s.recordEvent(ctx, w.ID, "backup_restored", fmt.Sprintf("restored backup %s (%s)", backup.Name, backup.ID))

	return connect.NewResponse(&v1.RestoreWorkloadBackupResponse{
		Success: true,
		Message: fmt.Sprintf("Successfully restored backup %s", backup.Name),
	}), nil
}

// DeleteWorkloadBackup deletes a snapshot record and its on-disk archive.
func (s *WorkloadService) DeleteWorkloadBackup(ctx context.Context, req *connect.Request[v1.DeleteWorkloadBackupRequest]) (*connect.Response[v1.DeleteWorkloadBackupResponse], error) {
	if req.Msg.Id == "" || req.Msg.BackupId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload id and backup id are required"))
	}

	w, err := s.load(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}

	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	var backup db.WorkloadBackup
	if err := q.Where("id = ? AND workload_id = ?", req.Msg.BackupId, w.ID).First(&backup).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("backup not found"))
	}

	if backup.Locked {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("cannot delete locked backup; unlock it first"))
	}

	// Dispatch delete to node if connected
	if w.NodeID != "" && s.dispatcher != nil && s.dispatcher.IsConnected(w.NodeID) {
		cmdID := uuid.NewString()
		waiter := s.dispatcher.ExpectDeleteBackup(cmdID)
		defer s.dispatcher.CancelDeleteBackup(cmdID)

		if ok := s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
			Payload: &v1.ControlMessage_DeleteBackup{
				DeleteBackup: &v1.ControlDeleteBackup{
					CommandId:  cmdID,
					WorkloadId: w.ID,
					BackupId:   backup.ID,
				},
			},
		}); ok {
			select {
			case <-time.After(30 * time.Second):
			case <-waiter:
			}
		}
	}

	if err := q.Where("id = ?", backup.ID).Delete(&db.WorkloadBackup{}).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("delete backup record: %w", err))
	}

	s.recordEvent(ctx, w.ID, "backup_deleted", fmt.Sprintf("deleted backup %s (%s)", backup.Name, backup.ID))

	return connect.NewResponse(&v1.DeleteWorkloadBackupResponse{}), nil
}

// SetWorkloadBackupLocked toggles the locked protection status of a backup.
func (s *WorkloadService) SetWorkloadBackupLocked(ctx context.Context, req *connect.Request[v1.SetWorkloadBackupLockedRequest]) (*connect.Response[v1.SetWorkloadBackupLockedResponse], error) {
	if req.Msg.Id == "" || req.Msg.BackupId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload id and backup id are required"))
	}

	w, err := s.load(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}

	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	var backup db.WorkloadBackup
	if err := q.Where("id = ? AND workload_id = ?", req.Msg.BackupId, w.ID).First(&backup).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("backup not found"))
	}

	backup.Locked = req.Msg.Locked
	if err := s.deps.Store.Unscoped().WithContext(ctx).Model(&db.WorkloadBackup{}).Where("id = ? AND org_id = ?", backup.ID, w.OrgID).Update("locked", req.Msg.Locked).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("update backup lock status: %w", err))
	}

	lockText := "unlocked"
	if backup.Locked {
		lockText = "locked"
	}
	s.recordEvent(ctx, w.ID, "backup_lock_toggled", fmt.Sprintf("%s backup %s (%s)", lockText, backup.Name, backup.ID))

	return connect.NewResponse(&v1.SetWorkloadBackupLockedResponse{
		Backup: backupToProto(&backup),
	}), nil
}
