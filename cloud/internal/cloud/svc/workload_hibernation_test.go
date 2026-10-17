package svc

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkloadHibernationLifecycle(t *testing.T) {
	t.Parallel()
	svcs, store := testBundleCustom(t, nil)
	ctx := orgCtx()

	// 1. Join node
	tok, err := svcs.Node.CreateJoinToken(ctx, connect.NewRequest(&v1.CreateJoinTokenRequest{
		Name:       "hib-node-token",
		TtlSeconds: 3600,
	}))
	require.NoError(t, err)

	joined, err := svcs.Agent.JoinNode(context.Background(), connect.NewRequest(&v1.JoinNodeRequest{
		Token: tok.Msg.Secret,
		Capacity: &v1.NodeCapacity{
			Vcpu: 4, RamMb: 8192, DiskGb: 100,
		},
		Hostname:     "hib-node.example.com",
		AgentVersion: "0.1.0",
	}))
	require.NoError(t, err)
	nodeID := joined.Msg.Identity.NodeId

	// Connect fake agent session to dispatcher
	svcs.Dispatcher.Register(nodeID)
	defer svcs.Dispatcher.Unregister(nodeID)

	// 2. Create workload
	wlResp, err := svcs.Workload.CreateWorkload(ctx, connect.NewRequest(&v1.CreateWorkloadRequest{
		Name:   "idle-craft",
		NodeId: nodeID,
		Spec: &v1.WorkloadSpec{
			Loader:             "paper",
			MinecraftVersion:   "1.21.4",
			MemoryMb:           2048,
			Hostname:           "play.idlecraft.net",
			IdleTimeoutMinutes: 15,
			HibernationMode:    "pause",
		},
	}))
	require.NoError(t, err)
	workloadID := wlResp.Msg.Workload.Id
	assert.Equal(t, int32(15), wlResp.Msg.Workload.Spec.IdleTimeoutMinutes)
	assert.Equal(t, "pause", wlResp.Msg.Workload.Spec.HibernationMode)

	// Set status to running in DB
	q, err := store.Org(ctx)
	require.NoError(t, err)
	err = q.Model(&db.Workload{}).Where("id = ?", workloadID).Update("status", "running").Error
	require.NoError(t, err)

	// 3. Hibernate workload in background goroutine while resolving dispatcher response
	go func() {
		time.Sleep(50 * time.Millisecond)
		// Check waiters and resolve
		svcs.Dispatcher.hibWakeMu.Lock()
		for cmdID := range svcs.Dispatcher.hibernateWaiters {
			svcs.Dispatcher.hibWakeMu.Unlock()
			svcs.Dispatcher.ResolveHibernate(&v1.AgentHibernateResult{
				CommandId:  cmdID,
				WorkloadId: workloadID,
				Success:    true,
			})
			return
		}
		svcs.Dispatcher.hibWakeMu.Unlock()
	}()

	hibResp, err := svcs.Workload.HibernateWorkload(ctx, connect.NewRequest(&v1.HibernateWorkloadRequest{
		Id:     workloadID,
		Reason: "idle timeout reached",
	}))
	require.NoError(t, err)
	assert.True(t, hibResp.Msg.Success)
	assert.Equal(t, v1.WorkloadStatus_WORKLOAD_STATUS_HIBERNATED, hibResp.Msg.Status)

	// Verify workload in DB is marked hibernated
	getResp, err := svcs.Workload.GetWorkload(ctx, connect.NewRequest(&v1.GetWorkloadRequest{Id: workloadID}))
	require.NoError(t, err)
	assert.Equal(t, v1.WorkloadStatus_WORKLOAD_STATUS_HIBERNATED, getResp.Msg.Workload.Status)

	// 4. Test SyncIngressRoutes
	syncResp, err := svcs.Workload.SyncIngressRoutes(ctx, connect.NewRequest(&v1.SyncIngressRoutesRequest{
		WorkloadId: workloadID,
	}))
	require.NoError(t, err)
	assert.Equal(t, int32(1), syncResp.Msg.RoutesSynced)
	require.Len(t, syncResp.Msg.Routes, 1)
	assert.Equal(t, "play.idlecraft.net", syncResp.Msg.Routes[0].Hostname)
	assert.True(t, syncResp.Msg.Routes[0].Hibernated)
	assert.Equal(t, int32(25565), syncResp.Msg.Routes[0].ProxyPort)

	// 5. Wake workload
	go func() {
		time.Sleep(50 * time.Millisecond)
		svcs.Dispatcher.hibWakeMu.Lock()
		for cmdID := range svcs.Dispatcher.wakeWaiters {
			svcs.Dispatcher.hibWakeMu.Unlock()
			svcs.Dispatcher.ResolveWake(&v1.AgentWakeResult{
				CommandId:  cmdID,
				WorkloadId: workloadID,
				Success:    true,
			})
			return
		}
		svcs.Dispatcher.hibWakeMu.Unlock()
	}()

	wakeResp, err := svcs.Workload.WakeWorkload(ctx, connect.NewRequest(&v1.WakeWorkloadRequest{
		Id: workloadID,
	}))
	require.NoError(t, err)
	assert.True(t, wakeResp.Msg.Success)
	assert.Equal(t, v1.WorkloadStatus_WORKLOAD_STATUS_RUNNING, wakeResp.Msg.Status)

	// Verify workload in DB is marked running
	getResp2, err := svcs.Workload.GetWorkload(ctx, connect.NewRequest(&v1.GetWorkloadRequest{Id: workloadID}))
	require.NoError(t, err)
	assert.Equal(t, v1.WorkloadStatus_WORKLOAD_STATUS_RUNNING, getResp2.Msg.Workload.Status)

	// Verify route is no longer hibernated
	syncResp2, err := svcs.Workload.SyncIngressRoutes(ctx, connect.NewRequest(&v1.SyncIngressRoutesRequest{
		WorkloadId: workloadID,
	}))
	require.NoError(t, err)
	require.Len(t, syncResp2.Msg.Routes, 1)
	assert.False(t, syncResp2.Msg.Routes[0].Hibernated)
}
