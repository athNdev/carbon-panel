package svc

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/stretchr/testify/require"
)

func TestWorkloadNetworkingAndPortAllocation(t *testing.T) {
	t.Parallel()
	svcs := testBundle(t)
	ctx := orgCtx()

	// 1. Join node
	tok, err := svcs.Node.CreateJoinToken(ctx, connect.NewRequest(&v1.CreateJoinTokenRequest{
		Name:       "net-node-token",
		TtlSeconds: 3600,
	}))
	require.NoError(t, err)

	joined, err := svcs.Agent.JoinNode(context.Background(), connect.NewRequest(&v1.JoinNodeRequest{
		Token: tok.Msg.Secret,
		Capacity: &v1.NodeCapacity{
			Vcpu: 4, RamMb: 8192, DiskGb: 100,
		},
		Hostname:     "node-alpha.example.com",
		AgentVersion: "0.1.0",
	}))
	require.NoError(t, err)
	nodeID := joined.Msg.Identity.NodeId

	// 2. Create first workload with default port (0 -> should allocate 25565)
	resp1, err := svcs.Workload.CreateWorkload(ctx, connect.NewRequest(&v1.CreateWorkloadRequest{
		Name:   "server-alpha",
		NodeId: nodeID,
		Spec: &v1.WorkloadSpec{
			Loader:           "paper",
			MinecraftVersion: "1.21.4",
			MemoryMb:         2048,
			Hostname:         "play.example.com",
		},
	}))
	require.NoError(t, err)
	require.Equal(t, int32(25565), resp1.Msg.Workload.HostPort)

	// 3. Create second workload on same node with default port (0 -> should allocate 25566)
	resp2, err := svcs.Workload.CreateWorkload(ctx, connect.NewRequest(&v1.CreateWorkloadRequest{
		Name:   "server-beta",
		NodeId: nodeID,
		Spec: &v1.WorkloadSpec{
			Loader:           "fabric",
			MinecraftVersion: "1.21.4",
			MemoryMb:         2048,
			Hostname:         "beta.example.com",
		},
	}))
	require.NoError(t, err)
	require.Equal(t, int32(25566), resp2.Msg.Workload.HostPort)

	// 4. Collision check: creating third workload requesting occupied port 25565 should fail with CodeAlreadyExists
	_, err = svcs.Workload.CreateWorkload(ctx, connect.NewRequest(&v1.CreateWorkloadRequest{
		Name:   "server-gamma",
		NodeId: nodeID,
		Spec: &v1.WorkloadSpec{
			Loader:           "vanilla",
			MinecraftVersion: "1.21.4",
			MemoryMb:         1024,
			HostPort:         25565,
		},
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeAlreadyExists, connect.CodeOf(err))

	// 5. Test GetWorkloadNetworking for standard port 25565
	net1, err := svcs.Workload.GetWorkloadNetworking(ctx, connect.NewRequest(&v1.GetWorkloadNetworkingRequest{
		Id: resp1.Msg.Workload.Id,
	}))
	require.NoError(t, err)
	require.Equal(t, int32(25565), net1.Msg.HostPort)
	require.Equal(t, "play.example.com", net1.Msg.PrimaryAddress)
	require.Empty(t, net1.Msg.SrvRecord) // Standard port doesn't need SRV
	require.False(t, net1.Msg.PortConflict)

	// 6. Test GetWorkloadNetworking for non-standard port 25566
	net2, err := svcs.Workload.GetWorkloadNetworking(ctx, connect.NewRequest(&v1.GetWorkloadNetworkingRequest{
		Id: resp2.Msg.Workload.Id,
	}))
	require.NoError(t, err)
	require.Equal(t, int32(25566), net2.Msg.HostPort)
	require.Contains(t, net2.Msg.PrimaryAddress, ":25566")
	require.Contains(t, net2.Msg.SrvRecord, "_minecraft._tcp.beta.example.com. 3600 IN SRV 0 5 25566")
	require.False(t, net2.Msg.PortConflict)

	// 7. Test ListNodePorts
	portsResp, err := svcs.Workload.ListNodePorts(ctx, connect.NewRequest(&v1.ListNodePortsRequest{
		NodeId: nodeID,
	}))
	require.NoError(t, err)
	require.Equal(t, nodeID, portsResp.Msg.NodeId)
	require.Len(t, portsResp.Msg.Allocations, 2)
	require.Equal(t, int32(25565), portsResp.Msg.Allocations[0].Port)
	require.Equal(t, "server-alpha", portsResp.Msg.Allocations[0].WorkloadName)
	require.Equal(t, int32(25566), portsResp.Msg.Allocations[1].Port)
	require.Equal(t, "server-beta", portsResp.Msg.Allocations[1].WorkloadName)
}
