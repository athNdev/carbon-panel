package svc

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/stretchr/testify/require"
)

func TestWorkloadBackup_Lifecycle(t *testing.T) {
	t.Parallel()
	svcs := testBundle(t)
	ctx := orgCtx()

	// 1. Join node
	tok, err := svcs.Node.CreateJoinToken(ctx, connect.NewRequest(&v1.CreateJoinTokenRequest{
		Name:       "backup-node-token",
		TtlSeconds: 3600,
	}))
	require.NoError(t, err)

	joined, err := svcs.Agent.JoinNode(context.Background(), connect.NewRequest(&v1.JoinNodeRequest{
		Token: tok.Msg.Secret,
		Capacity: &v1.NodeCapacity{
			Vcpu: 4, RamMb: 8192, DiskGb: 100,
		},
		Hostname:     "backup-test-node",
		AgentVersion: "0.1.0",
	}))
	require.NoError(t, err)
	nodeID := joined.Msg.Identity.NodeId

	sess := svcs.Dispatcher.Register(nodeID)
	defer svcs.Dispatcher.Unregister(nodeID)

	// 2. Create Workload
	wResp, err := svcs.Workload.CreateWorkload(ctx, connect.NewRequest(&v1.CreateWorkloadRequest{
		NodeId: nodeID,
		Name:   "backup-workload",
	}))
	require.NoError(t, err)
	workloadID := wResp.Msg.Workload.Id

	// 3. Node simulator goroutine
	go func() {
		for msg := range sess.NextMessage() {
			if create := msg.GetCreateBackup(); create != nil {
				svcs.Dispatcher.ResolveCreateBackup(&v1.AgentCreateBackupResult{
					CommandId:  create.CommandId,
					WorkloadId: create.WorkloadId,
					BackupId:   create.BackupId,
					Success:    true,
					SizeBytes:  1048576,
					Sha256:     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
				})
			} else if restore := msg.GetRestoreBackup(); restore != nil {
				svcs.Dispatcher.ResolveRestoreBackup(&v1.AgentRestoreBackupResult{
					CommandId:  restore.CommandId,
					WorkloadId: restore.WorkloadId,
					BackupId:   restore.BackupId,
					Success:    true,
				})
			} else if del := msg.GetDeleteBackup(); del != nil {
				svcs.Dispatcher.ResolveDeleteBackup(&v1.AgentDeleteBackupResult{
					CommandId:  del.CommandId,
					WorkloadId: del.WorkloadId,
					BackupId:   del.BackupId,
					Success:    true,
				})
			}
		}
	}()

	// 4. Create backup
	createResp, err := svcs.Workload.CreateWorkloadBackup(ctx, connect.NewRequest(&v1.CreateWorkloadBackupRequest{
		Id:   workloadID,
		Name: "initial-snapshot",
	}))
	require.NoError(t, err)
	backup := createResp.Msg.Backup
	require.NotNil(t, backup)
	require.Equal(t, "initial-snapshot", backup.Name)
	require.Equal(t, "completed", backup.Status)
	require.Equal(t, int64(1048576), backup.SizeBytes)
	require.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", backup.Sha256)
	require.False(t, backup.Locked)

	// 5. List backups
	listResp, err := svcs.Workload.ListWorkloadBackups(ctx, connect.NewRequest(&v1.ListWorkloadBackupsRequest{
		Id: workloadID,
	}))
	require.NoError(t, err)
	require.Len(t, listResp.Msg.Backups, 1)
	require.Equal(t, backup.Id, listResp.Msg.Backups[0].Id)

	// 6. Lock backup
	lockResp, err := svcs.Workload.SetWorkloadBackupLocked(ctx, connect.NewRequest(&v1.SetWorkloadBackupLockedRequest{
		Id:       workloadID,
		BackupId: backup.Id,
		Locked:   true,
	}))
	require.NoError(t, err)
	require.True(t, lockResp.Msg.Backup.Locked)

	// 7. Attempt delete locked backup -> expect failure
	_, err = svcs.Workload.DeleteWorkloadBackup(ctx, connect.NewRequest(&v1.DeleteWorkloadBackupRequest{
		Id:       workloadID,
		BackupId: backup.Id,
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))

	// 8. Restore backup
	restoreResp, err := svcs.Workload.RestoreWorkloadBackup(ctx, connect.NewRequest(&v1.RestoreWorkloadBackupRequest{
		Id:       workloadID,
		BackupId: backup.Id,
	}))
	require.NoError(t, err)
	require.True(t, restoreResp.Msg.Success)

	// 9. Unlock and delete backup
	_, err = svcs.Workload.SetWorkloadBackupLocked(ctx, connect.NewRequest(&v1.SetWorkloadBackupLockedRequest{
		Id:       workloadID,
		BackupId: backup.Id,
		Locked:   false,
	}))
	require.NoError(t, err)

	_, err = svcs.Workload.DeleteWorkloadBackup(ctx, connect.NewRequest(&v1.DeleteWorkloadBackupRequest{
		Id:       workloadID,
		BackupId: backup.Id,
	}))
	require.NoError(t, err)

	// 10. List should now be empty
	listResp2, err := svcs.Workload.ListWorkloadBackups(ctx, connect.NewRequest(&v1.ListWorkloadBackupsRequest{
		Id: workloadID,
	}))
	require.NoError(t, err)
	require.Empty(t, listResp2.Msg.Backups)
}
