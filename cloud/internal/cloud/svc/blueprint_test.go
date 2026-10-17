package svc

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/stretchr/testify/require"
)

func TestBlueprintService_BuiltinsAndCRUD(t *testing.T) {
	t.Parallel()
	bundle := testBundle(t)
	bpSvc := bundle.Blueprint
	ctx := orgCtx()

	// 1. List builtins
	listResp, err := bpSvc.ListBlueprints(ctx, connect.NewRequest(&v1.ListBlueprintsRequest{}))
	require.NoError(t, err)
	require.NotEmpty(t, listResp.Msg.Blueprints)

	// Filter by loader
	filterResp, err := bpSvc.ListBlueprints(ctx, connect.NewRequest(&v1.ListBlueprintsRequest{Loader: "paper"}))
	require.NoError(t, err)
	require.Len(t, filterResp.Msg.Blueprints, 1)
	require.Equal(t, "paper", filterResp.Msg.Blueprints[0].Loader)

	// 2. Get builtin
	getResp, err := bpSvc.GetBlueprint(ctx, connect.NewRequest(&v1.GetBlueprintRequest{Id: "paper"}))
	require.NoError(t, err)
	require.Equal(t, "PaperMC", getResp.Msg.Blueprint.Name)
	require.Equal(t, int64(4096), getResp.Msg.Blueprint.DefaultMemoryMb)
	require.True(t, getResp.Msg.Blueprint.Builtin)

	// 3. Prevent creating blueprint shadowing a builtin
	_, err = bpSvc.CreateBlueprint(ctx, connect.NewRequest(&v1.CreateBlueprintRequest{
		Name:   "paper",
		Loader: "paper",
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeAlreadyExists, connect.CodeOf(err))

	// 4. Create custom blueprint
	createResp, err := bpSvc.CreateBlueprint(ctx, connect.NewRequest(&v1.CreateBlueprintRequest{
		Name:                 "RLCraft Heavy Pack",
		Description:          "Custom heavy RPG modpack",
		Loader:               "forge",
		MinecraftVersion:     "1.12.2",
		DefaultMemoryMb:      8192,
		DefaultCpuMillicores: 4000,
		DefaultEnv: map[string]string{
			"MODPACK_URL": "https://example.com/rlcraft.zip",
		},
		DefaultJvmFlags: []string{"-XX:+UseZGC"},
	}))
	require.NoError(t, err)
	customID := createResp.Msg.Blueprint.Id
	require.NotEmpty(t, customID)
	require.Equal(t, "RLCraft Heavy Pack", createResp.Msg.Blueprint.Name)
	require.False(t, createResp.Msg.Blueprint.Builtin)

	// 5. Get custom blueprint
	getCustom, err := bpSvc.GetBlueprint(ctx, connect.NewRequest(&v1.GetBlueprintRequest{Id: customID}))
	require.NoError(t, err)
	require.Equal(t, "RLCraft Heavy Pack", getCustom.Msg.Blueprint.Name)
	require.Equal(t, int64(8192), getCustom.Msg.Blueprint.DefaultMemoryMb)
	require.Equal(t, "https://example.com/rlcraft.zip", getCustom.Msg.Blueprint.DefaultEnv["MODPACK_URL"])

	// 6. Update custom blueprint
	newName := "RLCraft Definitive"
	newMem := int64(10240)
	updateResp, err := bpSvc.UpdateBlueprint(ctx, connect.NewRequest(&v1.UpdateBlueprintRequest{
		Id:              customID,
		Name:            &newName,
		DefaultMemoryMb: &newMem,
	}))
	require.NoError(t, err)
	require.Equal(t, "RLCraft Definitive", updateResp.Msg.Blueprint.Name)
	require.Equal(t, int64(10240), updateResp.Msg.Blueprint.DefaultMemoryMb)

	// 7. Cannot update or delete builtin
	_, err = bpSvc.UpdateBlueprint(ctx, connect.NewRequest(&v1.UpdateBlueprintRequest{Id: "paper"}))
	require.Error(t, err)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

	_, err = bpSvc.DeleteBlueprint(ctx, connect.NewRequest(&v1.DeleteBlueprintRequest{Id: "paper"}))
	require.Error(t, err)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

	// 8. Delete custom blueprint
	_, err = bpSvc.DeleteBlueprint(ctx, connect.NewRequest(&v1.DeleteBlueprintRequest{Id: customID}))
	require.NoError(t, err)

	_, err = bpSvc.GetBlueprint(ctx, connect.NewRequest(&v1.GetBlueprintRequest{Id: customID}))
	require.Error(t, err)
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}

func TestWorkloadCreationFromBlueprint(t *testing.T) {
	t.Parallel()
	bundle := testBundle(t)
	wlSvc := bundle.Workload
	bpSvc := bundle.Blueprint
	ctx := orgCtx()

	// Join a node
	tok, err := bundle.Node.CreateJoinToken(ctx, connect.NewRequest(&v1.CreateJoinTokenRequest{
		Name:       "bp-test-node-tok",
		TtlSeconds: 3600,
	}))
	require.NoError(t, err)

	joined, err := bundle.Agent.JoinNode(context.Background(), connect.NewRequest(&v1.JoinNodeRequest{
		Token: tok.Msg.Secret,
		Capacity: &v1.NodeCapacity{
			Vcpu: 8, RamMb: 32768, DiskGb: 500,
		},
		Hostname:     "bp-node.example.com",
		AgentVersion: "0.1.0",
	}))
	require.NoError(t, err)
	nodeID := joined.Msg.Identity.NodeId

	// 1. Create workload referencing builtin 'paper' blueprint
	createResp, err := wlSvc.CreateWorkload(ctx, connect.NewRequest(&v1.CreateWorkloadRequest{
		Name:   "paper-server",
		NodeId: nodeID,
		Spec: &v1.WorkloadSpec{
			BlueprintId: "paper",
		},
	}))
	require.NoError(t, err)
	wl := createResp.Msg.Workload
	require.Equal(t, "paper", wl.Spec.Loader)
	require.Equal(t, "1.21.4", wl.Spec.MinecraftVersion)
	require.Equal(t, int64(4096), wl.Spec.MemoryMb)
	require.Equal(t, int64(2000), wl.Spec.CpuMillicores)
	require.Equal(t, "PAPER", wl.Spec.Env["TYPE"])
	require.Equal(t, "TRUE", wl.Spec.Env["EULA"])
	require.Contains(t, wl.Spec.JvmFlags, "-XX:+UseG1GC")

	// 2. Create workload referencing builtin with explicit caller overrides
	overrideResp, err := wlSvc.CreateWorkload(ctx, connect.NewRequest(&v1.CreateWorkloadRequest{
		Name:   "custom-paper",
		NodeId: nodeID,
		Spec: &v1.WorkloadSpec{
			BlueprintId:      "paper",
			MemoryMb:         8192,     // override memory
			MinecraftVersion: "1.20.4", // override version
			Env:              map[string]string{"MOTD": "Welcome to Overridden Paper"},
		},
	}))
	require.NoError(t, err)
	wlOverride := overrideResp.Msg.Workload
	require.Equal(t, "paper", wlOverride.Spec.Loader)
	require.Equal(t, "1.20.4", wlOverride.Spec.MinecraftVersion)
	require.Equal(t, int64(8192), wlOverride.Spec.MemoryMb)
	require.Equal(t, "Welcome to Overridden Paper", wlOverride.Spec.Env["MOTD"])
	require.Equal(t, "PAPER", wlOverride.Spec.Env["TYPE"]) // merged from blueprint!

	// 3. Create custom blueprint and instantiate workload from it
	createBp, err := bpSvc.CreateBlueprint(ctx, connect.NewRequest(&v1.CreateBlueprintRequest{
		Name:                 "fabric-perf",
		Loader:               "fabric",
		MinecraftVersion:     "1.21.1",
		DockerImage:          "ghcr.io/myorg/custom-fabric:latest",
		DefaultMemoryMb:      6144,
		DefaultCpuMillicores: 3000,
		DefaultEnv:           map[string]string{"OPTIMIZE": "true"},
	}))
	require.NoError(t, err)

	fromCustomResp, err := wlSvc.CreateWorkload(ctx, connect.NewRequest(&v1.CreateWorkloadRequest{
		Name:   "fabric-live",
		NodeId: nodeID,
		Spec: &v1.WorkloadSpec{
			BlueprintId: createBp.Msg.Blueprint.Id,
		},
	}))
	require.NoError(t, err)
	wlCustom := fromCustomResp.Msg.Workload
	require.Equal(t, "fabric", wlCustom.Spec.Loader)
	require.Equal(t, "1.21.1", wlCustom.Spec.MinecraftVersion)
	require.Equal(t, int64(6144), wlCustom.Spec.MemoryMb)
	require.Equal(t, int64(3000), wlCustom.Spec.CpuMillicores)
	require.Equal(t, "true", wlCustom.Spec.Env["OPTIMIZE"])
	require.Equal(t, "ghcr.io/myorg/custom-fabric:latest", wlCustom.Spec.Env["DOCKER_IMAGE"])

	// 4. Invalid blueprint ID returns NotFound
	_, err = wlSvc.CreateWorkload(ctx, connect.NewRequest(&v1.CreateWorkloadRequest{
		Name:   "bad-blueprint-server",
		NodeId: nodeID,
		Spec: &v1.WorkloadSpec{
			BlueprintId: "nonexistent-blueprint",
		},
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}
