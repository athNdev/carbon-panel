package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/athNdev/carbon-panel/pkg/proto/cloud/v1/cloudv1connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type mockSystemServer struct {
	cloudv1connect.UnimplementedSystemServiceHandler
}

func (m *mockSystemServer) GetBuildInfo(context.Context, *connect.Request[v1.GetBuildInfoRequest]) (*connect.Response[v1.GetBuildInfoResponse], error) {
	return connect.NewResponse(&v1.GetBuildInfoResponse{
		Build: &v1.BuildInfo{
			Version:        "v1.0.0-test",
			Commit:         "abc1234",
			BuildTime:      "2026-09-23T12:00:00Z",
			GoVersion:      "go1.25.13",
			DatabaseDriver: "sqlite",
		},
	}), nil
}

func (m *mockSystemServer) GetCapabilities(context.Context, *connect.Request[v1.GetCapabilitiesRequest]) (*connect.Response[v1.GetCapabilitiesResponse], error) {
	return connect.NewResponse(&v1.GetCapabilitiesResponse{
		Capabilities: []*v1.Capability{
			{Id: "multi_tenant", Enabled: true},
			{Id: "provisioning", Enabled: true},
		},
	}), nil
}

type mockNodeServer struct {
	cloudv1connect.UnimplementedNodeServiceHandler
}

func (m *mockNodeServer) ListNodes(context.Context, *connect.Request[v1.ListNodesRequest]) (*connect.Response[v1.ListNodesResponse], error) {
	return connect.NewResponse(&v1.ListNodesResponse{
		Nodes: []*v1.Node{
			{
				Id:       "node_123",
				Name:     "test-node-1",
				Status:   v1.NodeStatus_NODE_STATUS_ONLINE,
				Origin:   v1.NodeOrigin_NODE_ORIGIN_BYO,
				PublicIp: "192.0.2.1",
				Capacity: &v1.NodeCapacity{
					RamMb: 4096,
					Vcpu:  2,
				},
				Allocation: &v1.NodeAllocation{
					RamMb:         1024,
					CpuMillicores: 500,
				},
			},
		},
	}), nil
}

func (m *mockNodeServer) GetNode(ctx context.Context, req *connect.Request[v1.GetNodeRequest]) (*connect.Response[v1.GetNodeResponse], error) {
	if req.Msg.Id != "node_123" {
		return nil, connect.NewError(connect.CodeNotFound, os.ErrNotExist)
	}
	return connect.NewResponse(&v1.GetNodeResponse{
		Node: &v1.Node{
			Id:       "node_123",
			Name:     "test-node-1",
			Status:   v1.NodeStatus_NODE_STATUS_ONLINE,
			Origin:   v1.NodeOrigin_NODE_ORIGIN_BYO,
			PublicIp: "192.0.2.1",
		},
	}), nil
}

func (m *mockNodeServer) DrainNode(ctx context.Context, req *connect.Request[v1.DrainNodeRequest]) (*connect.Response[v1.DrainNodeResponse], error) {
	return connect.NewResponse(&v1.DrainNodeResponse{
		Node: &v1.Node{
			Id:     req.Msg.Id,
			Status: v1.NodeStatus_NODE_STATUS_DRAINING,
		},
	}), nil
}

func (m *mockNodeServer) CreateJoinToken(ctx context.Context, req *connect.Request[v1.CreateJoinTokenRequest]) (*connect.Response[v1.CreateJoinTokenResponse], error) {
	return connect.NewResponse(&v1.CreateJoinTokenResponse{
		Token: &v1.JoinToken{
			Id:        "tok_abc",
			Name:      req.Msg.Name,
			ExpiresAt: timestamppb.Now(),
		},
		Secret: "ccj_secret_12345",
	}), nil
}

type mockWorkloadServer struct {
	cloudv1connect.UnimplementedWorkloadServiceHandler
}

func (m *mockWorkloadServer) CreateWorkload(ctx context.Context, req *connect.Request[v1.CreateWorkloadRequest]) (*connect.Response[v1.CreateWorkloadResponse], error) {
	return connect.NewResponse(&v1.CreateWorkloadResponse{
		Workload: &v1.Workload{
			Id:     "wl_created_123",
			Name:   req.Msg.Name,
			Status: v1.WorkloadStatus_WORKLOAD_STATUS_PENDING,
			Spec:   req.Msg.Spec,
		},
	}), nil
}

func (m *mockWorkloadServer) RestartWorkload(ctx context.Context, req *connect.Request[v1.RestartWorkloadRequest]) (*connect.Response[v1.RestartWorkloadResponse], error) {
	return connect.NewResponse(&v1.RestartWorkloadResponse{
		Workload: &v1.Workload{
			Id:     req.Msg.Id,
			Status: v1.WorkloadStatus_WORKLOAD_STATUS_RUNNING,
		},
	}), nil
}

func (m *mockWorkloadServer) SendWorkloadCommand(ctx context.Context, req *connect.Request[v1.SendWorkloadCommandRequest]) (*connect.Response[v1.SendWorkloadCommandResponse], error) {
	return connect.NewResponse(&v1.SendWorkloadCommandResponse{
		Output: "Command output for: " + req.Msg.Command,
	}), nil
}

func (m *mockWorkloadServer) ListWorkloadEvents(ctx context.Context, req *connect.Request[v1.ListWorkloadEventsRequest]) (*connect.Response[v1.ListWorkloadEventsResponse], error) {
	return connect.NewResponse(&v1.ListWorkloadEventsResponse{
		Events: []*v1.WorkloadEvent{
			{
				Id:         "ev_1",
				WorkloadId: req.Msg.Id,
				Kind:       "restart",
				Message:    "Restarted successfully",
				CreatedAt:  timestamppb.Now(),
			},
		},
	}), nil
}

func (m *mockWorkloadServer) GetWorkloadNetworking(ctx context.Context, req *connect.Request[v1.GetWorkloadNetworkingRequest]) (*connect.Response[v1.GetWorkloadNetworkingResponse], error) {
	return connect.NewResponse(&v1.GetWorkloadNetworkingResponse{
		WorkloadId:     req.Msg.Id,
		NodeId:         "node_123",
		NodeAddress:    "192.168.0.102",
		HostPort:       25566,
		ContainerPort:  25565,
		Hostname:       "beta.example.com",
		PrimaryAddress: "192.168.0.102:25566",
		SrvRecord:      "_minecraft._tcp.beta.example.com. 3600 IN SRV 0 5 25566 192.168.0.102.",
	}), nil
}

func (m *mockWorkloadServer) ListNodePorts(ctx context.Context, req *connect.Request[v1.ListNodePortsRequest]) (*connect.Response[v1.ListNodePortsResponse], error) {
	return connect.NewResponse(&v1.ListNodePortsResponse{
		NodeId:         req.Msg.NodeId,
		PortRangeMin:   25565,
		PortRangeMax:   25700,
		AvailablePorts: 135,
		Allocations: []*v1.NodePortAllocation{
			{
				Port:         25565,
				WorkloadId:   "wl_123",
				WorkloadName: "survival",
				Status:       "running",
				Hostname:     "play.example.com",
			},
		},
	}), nil
}

func (m *mockWorkloadServer) StreamWorkloadLogs(ctx context.Context, req *connect.Request[v1.StreamWorkloadLogsRequest], stream *connect.ServerStream[v1.WorkloadLogLine]) error {
	_ = stream.Send(&v1.WorkloadLogLine{
		Line:      "Starting minecraft server",
		Timestamp: timestamppb.Now(),
	})
	_ = stream.Send(&v1.WorkloadLogLine{
		Line:      "Server warning on startup",
		Stderr:    true,
		Timestamp: timestamppb.Now(),
	})
	return nil
}

type mockProvisionServer struct {
	cloudv1connect.UnimplementedProvisionServiceHandler
}

func (m *mockProvisionServer) ApplyProvision(ctx context.Context, req *connect.Request[v1.ApplyProvisionRequest]) (*connect.Response[v1.ApplyProvisionResponse], error) {
	return connect.NewResponse(&v1.ApplyProvisionResponse{
		Provision: &v1.Provision{
			Id:     req.Msg.Id,
			Status: v1.ProvisionStatus_PROVISION_STATUS_APPLYING,
		},
	}), nil
}


type mockBlueprintServer struct {
	cloudv1connect.UnimplementedBlueprintServiceHandler
}

func (m *mockBlueprintServer) ListBlueprints(context.Context, *connect.Request[v1.ListBlueprintsRequest]) (*connect.Response[v1.ListBlueprintsResponse], error) {
	return connect.NewResponse(&v1.ListBlueprintsResponse{
		Blueprints: []*v1.Blueprint{
			{
				Id:                   "paper",
				Name:                 "PaperMC",
				Loader:               "paper",
				MinecraftVersion:     "1.21.4",
				DefaultMemoryMb:      4096,
				DefaultCpuMillicores: 2000,
				Builtin:              true,
			},
		},
	}), nil
}

func (m *mockBlueprintServer) GetBlueprint(ctx context.Context, req *connect.Request[v1.GetBlueprintRequest]) (*connect.Response[v1.GetBlueprintResponse], error) {
	return connect.NewResponse(&v1.GetBlueprintResponse{
		Blueprint: &v1.Blueprint{
			Id:                   req.Msg.Id,
			Name:                 "PaperMC",
			Description:          "High performance server",
			Loader:               "paper",
			MinecraftVersion:     "1.21.4",
			DockerImage:          "itzg/minecraft-server:latest",
			DefaultMemoryMb:      4096,
			DefaultCpuMillicores: 2000,
			Builtin:              true,
			DefaultEnv:           map[string]string{"TYPE": "PAPER"},
			DefaultJvmFlags:      []string{"-XX:+UseG1GC"},
		},
	}), nil
}

func (m *mockBlueprintServer) CreateBlueprint(ctx context.Context, req *connect.Request[v1.CreateBlueprintRequest]) (*connect.Response[v1.CreateBlueprintResponse], error) {
	return connect.NewResponse(&v1.CreateBlueprintResponse{
		Blueprint: &v1.Blueprint{
			Id:                   "custom-bp-123",
			Name:                 req.Msg.Name,
			Loader:               req.Msg.Loader,
			MinecraftVersion:     req.Msg.MinecraftVersion,
			DefaultMemoryMb:      req.Msg.DefaultMemoryMb,
			DefaultCpuMillicores: req.Msg.DefaultCpuMillicores,
		},
	}), nil
}

func (m *mockBlueprintServer) DeleteBlueprint(ctx context.Context, req *connect.Request[v1.DeleteBlueprintRequest]) (*connect.Response[v1.DeleteBlueprintResponse], error) {
	return connect.NewResponse(&v1.DeleteBlueprintResponse{}), nil
}

func setupTestServer(t *testing.T) (*httptest.Server, *Client) {
	t.Helper()
	mux := http.NewServeMux()
	sysPath, sysHandler := cloudv1connect.NewSystemServiceHandler(&mockSystemServer{})
	nodePath, nodeHandler := cloudv1connect.NewNodeServiceHandler(&mockNodeServer{})
	workloadPath, workloadHandler := cloudv1connect.NewWorkloadServiceHandler(&mockWorkloadServer{})
	provisionPath, provisionHandler := cloudv1connect.NewProvisionServiceHandler(&mockProvisionServer{})
	bpPath, bpHandler := cloudv1connect.NewBlueprintServiceHandler(&mockBlueprintServer{})

	mux.Handle(sysPath, sysHandler)
	mux.Handle(nodePath, nodeHandler)
	mux.Handle(workloadPath, workloadHandler)
	mux.Handle(provisionPath, provisionHandler)
	mux.Handle(bpPath, bpHandler)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL, "test-token", "org_test", srv.Client())
	return srv, client
}

func TestCLI_HelpAndStatus(t *testing.T) {
	_, client := setupTestServer(t)

	t.Run("help", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		cli := &CLI{
			Stdout: &stdout,
			Stderr: &stderr,
			Client: client,
		}
		err := cli.Run(context.Background(), []string{"help"})
		require.NoError(t, err)
		require.Contains(t, stdout.String(), "cloudctl - Carbon Cloud management CLI")
	})

	t.Run("status table", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		cli := &CLI{
			Stdout: &stdout,
			Stderr: &stderr,
			Client: client,
		}
		err := cli.Run(context.Background(), []string{"status"})
		require.NoError(t, err)
		out := stdout.String()
		require.Contains(t, out, "Version:    v1.0.0-test")
		require.Contains(t, out, "multi_tenant: enabled")
	})

	t.Run("status json", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		cli := &CLI{
			Stdout: &stdout,
			Stderr: &stderr,
			Client: client,
		}
		err := cli.Run(context.Background(), []string{"--json", "status"})
		require.NoError(t, err)
		out := stdout.String()
		require.Contains(t, out, `"version": "v1.0.0-test"`)
		require.Contains(t, out, `"multi_tenant"`)
	})
}

func TestCLI_NodesAndTokens(t *testing.T) {
	_, client := setupTestServer(t)

	t.Run("nodes list", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		cli := &CLI{
			Stdout: &stdout,
			Stderr: &stderr,
			Client: client,
		}
		err := cli.Run(context.Background(), []string{"nodes", "list"})
		require.NoError(t, err)
		require.Contains(t, stdout.String(), "node_123")
		require.Contains(t, stdout.String(), "test-node-1")
	})

	t.Run("nodes get", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		cli := &CLI{
			Stdout: &stdout,
			Stderr: &stderr,
			Client: client,
		}
		err := cli.Run(context.Background(), []string{"nodes", "get", "node_123"})
		require.NoError(t, err)
		require.Contains(t, stdout.String(), "Node: test-node-1 (node_123)")
	})

	t.Run("nodes drain", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		cli := &CLI{
			Stdout: &stdout,
			Stderr: &stderr,
			Client: client,
		}
		err := cli.Run(context.Background(), []string{"nodes", "drain", "node_123"})
		require.NoError(t, err)
		require.Contains(t, stdout.String(), "is now draining")
	})

	t.Run("nodes ports", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		cli := &CLI{
			Stdout: &stdout,
			Stderr: &stderr,
			Client: client,
		}
		err := cli.Run(context.Background(), []string{"nodes", "ports", "node_123"})
		require.NoError(t, err)
		require.Contains(t, stdout.String(), "Port Allocations for Node node_123:")
		require.Contains(t, stdout.String(), "25565")
		require.Contains(t, stdout.String(), "survival")
	})

	t.Run("tokens create", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		cli := &CLI{
			Stdout: &stdout,
			Stderr: &stderr,
			Client: client,
		}
		err := cli.Run(context.Background(), []string{"tokens", "create", "-name=edge-node"})
		require.NoError(t, err)
		require.Contains(t, stdout.String(), "Join Token Created:")
		require.Contains(t, stdout.String(), "ccj_secret_12345")
		require.Contains(t, stdout.String(), "curl -fsSL")
	})
}

func TestCLI_WorkloadsAndProvisions(t *testing.T) {
	_, client := setupTestServer(t)

	t.Run("workloads create", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		cli := &CLI{
			Stdout: &stdout,
			Stderr: &stderr,
			Client: client,
		}
		err := cli.Run(context.Background(), []string{"workloads", "create", "-name=survival-world", "-version=1.20.4", "-loader=paper"})
		require.NoError(t, err)
		require.Contains(t, stdout.String(), "Workload created: ID wl_created_123")
	})

	t.Run("workloads restart", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		cli := &CLI{
			Stdout: &stdout,
			Stderr: &stderr,
			Client: client,
		}
		err := cli.Run(context.Background(), []string{"workloads", "restart", "wl_123"})
		require.NoError(t, err)
		require.Contains(t, stdout.String(), "Workload wl_123 restarted")
	})

	t.Run("workloads exec", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		cli := &CLI{
			Stdout: &stdout,
			Stderr: &stderr,
			Client: client,
		}
		err := cli.Run(context.Background(), []string{"workloads", "exec", "wl_123", "say", "hello"})
		require.NoError(t, err)
		require.Contains(t, stdout.String(), "Command output for: say hello")
	})

	t.Run("workloads events", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		cli := &CLI{
			Stdout: &stdout,
			Stderr: &stderr,
			Client: client,
		}
		err := cli.Run(context.Background(), []string{"workloads", "events", "wl_123"})
		require.NoError(t, err)
		require.Contains(t, stdout.String(), "ev_1")
		require.Contains(t, stdout.String(), "restart")
		require.Contains(t, stdout.String(), "Restarted successfully")
	})

	t.Run("workloads logs", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		cli := &CLI{
			Stdout: &stdout,
			Stderr: &stderr,
			Client: client,
		}
		err := cli.Run(context.Background(), []string{"workloads", "logs", "wl_123", "--tail=50"})
		require.NoError(t, err)
		require.Contains(t, stdout.String(), "Starting minecraft server")
		require.Contains(t, stderr.String(), "[stderr] Server warning on startup")
	})

	t.Run("workloads networking", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		cli := &CLI{
			Stdout: &stdout,
			Stderr: &stderr,
			Client: client,
		}
		err := cli.Run(context.Background(), []string{"workloads", "networking", "wl_123"})
		require.NoError(t, err)
		require.Contains(t, stdout.String(), "Networking for Workload: wl_123")
		require.Contains(t, stdout.String(), "192.168.0.102:25566")
		require.Contains(t, stdout.String(), "SRV")
	})

	t.Run("provisions apply", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		cli := &CLI{
			Stdout: &stdout,
			Stderr: &stderr,
			Client: client,
		}
		err := cli.Run(context.Background(), []string{"provisions", "apply", "prov_999", "--plan-hash=abc"})
		require.NoError(t, err)
		require.Contains(t, stdout.String(), "Provision applied: ID prov_999")
	})
}

func TestCLI_Config(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")

	var stdout, stderr bytes.Buffer
	cli := &CLI{
		Stdout:     &stdout,
		Stderr:     &stderr,
		ConfigPath: cfgPath,
	}

	// View initial default config
	err := cli.Run(context.Background(), []string{"config", "view"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Endpoint: http://127.0.0.1:8080")

	// Set endpoint
	stdout.Reset()
	err = cli.Run(context.Background(), []string{"config", "set", "endpoint", "https://cloud.carbon.dev"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Config updated: endpoint = https://cloud.carbon.dev")

	// Set API key
	stdout.Reset()
	err = cli.Run(context.Background(), []string{"config", "set", "api_key", "cca_live_1234567890"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Config updated: api_key = cca_live_1234567890")

	// Read saved config
	cfg, err := loadConfig(cfgPath)
	require.NoError(t, err)
	require.Equal(t, "https://cloud.carbon.dev", cfg.Endpoint)
	require.Equal(t, "cca_live_1234567890", cfg.APIKey)
}

func TestCLI_Blueprints(t *testing.T) {
	_, client := setupTestServer(t)

	t.Run("blueprints list", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		cli := &CLI{Stdout: &stdout, Stderr: &stderr, Client: client}
		err := cli.Run(context.Background(), []string{"blueprints", "list"})
		require.NoError(t, err)
		out := stdout.String()
		require.Contains(t, out, "paper")
		require.Contains(t, out, "PaperMC")
		require.Contains(t, out, "4096M")
	})

	t.Run("blueprints get", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		cli := &CLI{Stdout: &stdout, Stderr: &stderr, Client: client}
		err := cli.Run(context.Background(), []string{"blueprints", "get", "paper"})
		require.NoError(t, err)
		out := stdout.String()
		require.Contains(t, out, "Blueprint: paper")
		require.Contains(t, out, "PaperMC")
		require.Contains(t, out, "TYPE: PAPER")
		require.Contains(t, out, "-XX:+UseG1GC")
	})

	t.Run("blueprints create", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		cli := &CLI{Stdout: &stdout, Stderr: &stderr, Client: client}
		err := cli.Run(context.Background(), []string{"blueprints", "create", "-name", "My Custom Preset", "-loader", "fabric"})
		require.NoError(t, err)
		out := stdout.String()
		require.Contains(t, out, "Blueprint created: ID custom-bp-123")
	})

	t.Run("blueprints delete", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		cli := &CLI{Stdout: &stdout, Stderr: &stderr, Client: client}
		err := cli.Run(context.Background(), []string{"blueprints", "delete", "custom-bp-123"})
		require.NoError(t, err)
		require.Contains(t, stdout.String(), "Blueprint custom-bp-123 deleted")
	})

	t.Run("workloads create with blueprint", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		cli := &CLI{Stdout: &stdout, Stderr: &stderr, Client: client}
		err := cli.Run(context.Background(), []string{"workloads", "create", "-name", "preset-srv", "-blueprint", "paper"})
		require.NoError(t, err)
		require.Contains(t, stdout.String(), "Workload created:")
	})
}
