package proxy

import (
	"context"
	"fmt"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/google/uuid"
	"github.com/athNdev/mineserver/internal/config"
	"github.com/athNdev/mineserver/internal/db"
	"github.com/athNdev/mineserver/pkg/logger"
	v1 "github.com/athNdev/mineserver/pkg/proto/mineserver/v1"
)

type mockDockerClient struct {
	client.CommonAPIClient
	inspectFunc func(ctx context.Context, containerID string) (types.ContainerJSON, error)
	closed      bool
}

func (m *mockDockerClient) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	if m.inspectFunc != nil {
		return m.inspectFunc(ctx, containerID)
	}
	return types.ContainerJSON{}, fmt.Errorf("not implemented")
}

func (m *mockDockerClient) Close() error {
	m.closed = true
	return nil
}

func setupTestStore(t *testing.T) *db.Store {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Path:           ":memory:",
			AutoMigrate:    true,
			MaxConnections: 1,
		},
	}
	store, err := db.NewSQLiteStore(cfg)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}
	return store
}

func TestGetContainerIP_SpecificNetwork(t *testing.T) {
	mockCli := &mockDockerClient{
		inspectFunc: func(ctx context.Context, containerID string) (types.ContainerJSON, error) {
			return types.ContainerJSON{
				NetworkSettings: &types.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"bridge": {
							IPAddress: "172.17.0.2",
						},
						"MINESERVER-net": {
							IPAddress: "172.20.0.5",
						},
					},
				},
			}, nil
		},
	}

	ip, err := GetContainerIP(mockCli, "container-123", "MINESERVER-net")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip != "172.20.0.5" {
		t.Errorf("expected IP 172.20.0.5, got %s", ip)
	}
}

func TestGetContainerIP_FallbackNetwork(t *testing.T) {
	mockCli := &mockDockerClient{
		inspectFunc: func(ctx context.Context, containerID string) (types.ContainerJSON, error) {
			return types.ContainerJSON{
				NetworkSettings: &types.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"other-net": {
							IPAddress: "10.0.0.2",
						},
					},
				},
			}, nil
		},
	}

	ip, err := GetContainerIP(mockCli, "container-123", "nonexistent-net")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip != "10.0.0.2" {
		t.Errorf("expected fallback IP 10.0.0.2, got %s", ip)
	}
}

func TestGetContainerIP_NoIPFound(t *testing.T) {
	mockCli := &mockDockerClient{
		inspectFunc: func(ctx context.Context, containerID string) (types.ContainerJSON, error) {
			return types.ContainerJSON{
				NetworkSettings: &types.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"empty-net": {
							IPAddress: "",
						},
					},
				},
			}, nil
		},
	}

	_, err := GetContainerIP(mockCli, "container-123", "empty-net")
	if err == nil {
		t.Errorf("expected error when no IP found, got nil")
	}
}

func TestManager_GetContainerIP_UsesConfiguredClient(t *testing.T) {
	mockCli := &mockDockerClient{
		inspectFunc: func(ctx context.Context, containerID string) (types.ContainerJSON, error) {
			return types.ContainerJSON{
				NetworkSettings: &types.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"test-net": {
							IPAddress: "192.168.1.100",
						},
					},
				},
			}, nil
		},
	}

	store := setupTestStore(t)
	defer store.Close()

	cfg := &config.Config{
		Docker: config.DockerConfig{
			NetworkName: "test-net",
		},
	}
	log := logger.New()

	mgr := NewManager(store, cfg, log, mockCli)
	ip, err := mgr.GetContainerIP("container-456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip != "192.168.1.100" {
		t.Errorf("expected IP 192.168.1.100, got %s", ip)
	}
}

func TestManager_AddAndRemoveModuleRoute(t *testing.T) {
	mockCli := &mockDockerClient{
		inspectFunc: func(ctx context.Context, containerID string) (types.ContainerJSON, error) {
			return types.ContainerJSON{
				NetworkSettings: &types.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"test-net": {
							IPAddress: "172.28.0.10",
						},
					},
				},
			}, nil
		},
	}

	store := setupTestStore(t)
	defer store.Close()

	cfg := &config.Config{
		Proxy: config.ProxyConfig{
			Enabled: true,
		},
		Docker: config.DockerConfig{
			NetworkName: "test-net",
		},
	}
	log := logger.New()

	mgr := NewManager(store, cfg, log, mockCli)

	server := &db.Server{
		ID:            uuid.New().String(),
		Name:          "survival",
		ProxyHostname: "survival.mc.example.com",
		ContainerID:   "server-container-id",
	}
	if err := store.CreateServer(context.Background(), server); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	module := &db.Module{
		ID:          uuid.New().String(),
		ServerID:    server.ID,
		Name:        "geyser",
		ContainerID: "module-container-id",
		Ports: []*v1.ModulePort{
			{
				Name:          "bedrock",
				HostPort:      19132,
				ContainerPort: 19132,
				Protocol:      "udp",
				ProxyEnabled:  true,
			},
		},
	}
	if err := store.CreateModule(context.Background(), module); err != nil {
		t.Fatalf("failed to create module in store: %v", err)
	}

	defer mgr.Stop()

	// Add module route
	if err := mgr.AddModuleRoute(module, server); err != nil {
		t.Fatalf("AddModuleRoute failed: %v", err)
	}

	// Verify route was added
	routes := mgr.GetRoutes()
	route, found := routes["udp"]
	if !found {
		t.Fatalf("expected route 'udp' to be present in routes: %+v", routes)
	}
	if route.BackendHost != "172.28.0.10" {
		t.Errorf("expected BackendHost 172.28.0.10, got %s", route.BackendHost)
	}
	if route.BackendPort != 19132 {
		t.Errorf("expected BackendPort 19132, got %d", route.BackendPort)
	}

	// Remove module route
	if err := mgr.RemoveModuleRoute(module.ID); err != nil {
		t.Fatalf("RemoveModuleRoute failed: %v", err)
	}

	routes = mgr.GetRoutes()
	if len(routes) != 0 {
		t.Errorf("expected routes to be empty after removal, got %+v", routes)
	}
}
