package svc

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestWorkloadMetrics_CachedAndOnDemand(t *testing.T) {
	t.Parallel()
	svcs := testBundle(t)
	ctx := orgCtx()

	// 1. Join node
	tok, err := svcs.Node.CreateJoinToken(ctx, connect.NewRequest(&v1.CreateJoinTokenRequest{
		Name:       "metrics-node-token",
		TtlSeconds: 3600,
	}))
	require.NoError(t, err)

	joined, err := svcs.Agent.JoinNode(context.Background(), connect.NewRequest(&v1.JoinNodeRequest{
		Token: tok.Msg.Secret,
		Capacity: &v1.NodeCapacity{
			Vcpu: 4, RamMb: 8192, DiskGb: 100,
		},
		Hostname:     "metrics-test-node",
		AgentVersion: "0.1.0",
	}))
	require.NoError(t, err)
	nodeID := joined.Msg.Identity.NodeId

	sess := svcs.Dispatcher.Register(nodeID)
	defer svcs.Dispatcher.Unregister(nodeID)

	// 2. Create Workload
	wResp, err := svcs.Workload.CreateWorkload(ctx, connect.NewRequest(&v1.CreateWorkloadRequest{
		NodeId: nodeID,
		Name:   "metrics-workload",
	}))
	require.NoError(t, err)
	workloadID := wResp.Msg.Workload.Id

	// 3. Test on-demand dispatch simulator
	go func() {
		for msg := range sess.NextMessage() {
			if req := msg.GetGetWorkloadMetrics(); req != nil {
				svcs.Dispatcher.ResolveMetrics(&v1.AgentGetWorkloadMetricsResult{
					CommandId:  req.CommandId,
					WorkloadId: req.WorkloadId,
					Success:    true,
					Metrics: &v1.WorkloadMetrics{
						WorkloadId:     req.WorkloadId,
						CpuPercent:     45.2,
						MemoryUsedMb:   1024.0,
						MemoryLimitMb:  2048.0,
						DiskUsedBytes:  52428800,
						NetworkRxBytes: 1048576,
						NetworkTxBytes: 2097152,
						PlayersOnline:  3,
						MaxPlayers:     20,
						Tps:            19.8,
						PlayerSample:   []string{"Alice", "Bob", "Charlie"},
						UpdatedAt:      timestamppb.Now(),
					},
				})
			}
		}
	}()

	// 4. Query on-demand
	resp, err := svcs.Workload.GetWorkloadMetrics(ctx, connect.NewRequest(&v1.GetWorkloadMetricsRequest{
		Id: workloadID,
	}))
	require.NoError(t, err)
	require.NotNil(t, resp.Msg.Metrics)
	require.Equal(t, 45.2, resp.Msg.Metrics.CpuPercent)
	require.Equal(t, 1024.0, resp.Msg.Metrics.MemoryUsedMb)
	require.Equal(t, int32(3), resp.Msg.Metrics.PlayersOnline)
	require.Equal(t, 19.8, resp.Msg.Metrics.Tps)
	require.Equal(t, []string{"Alice", "Bob", "Charlie"}, resp.Msg.Metrics.PlayerSample)

	// 5. Test cache hit: update cached metrics directly as if from a heartbeat
	svcs.Dispatcher.UpdateWorkloadMetrics([]*v1.WorkloadMetrics{
		{
			WorkloadId:    workloadID,
			CpuPercent:    12.0,
			MemoryUsedMb:  800.0,
			MemoryLimitMb: 2048.0,
			PlayersOnline: 5,
			Tps:           20.0,
			UpdatedAt:     timestamppb.Now(),
		},
	})

	cachedResp, err := svcs.Workload.GetWorkloadMetrics(ctx, connect.NewRequest(&v1.GetWorkloadMetricsRequest{
		Id: workloadID,
	}))
	require.NoError(t, err)
	require.NotNil(t, cachedResp.Msg.Metrics)
	require.Equal(t, 12.0, cachedResp.Msg.Metrics.CpuPercent)
	require.Equal(t, 800.0, cachedResp.Msg.Metrics.MemoryUsedMb)
	require.Equal(t, int32(5), cachedResp.Msg.Metrics.PlayersOnline)
}
