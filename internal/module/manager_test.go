package module

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/google/uuid"
	"github.com/athNdev/mineserver/internal/config"
	storage "github.com/athNdev/mineserver/internal/db"
	"github.com/athNdev/mineserver/internal/proxy"
	"github.com/athNdev/mineserver/pkg/logger"
	v1 "github.com/athNdev/mineserver/pkg/proto/mineserver/v1"
)

type mockDockerClient struct {
	client.CommonAPIClient
	inspectFunc func(ctx context.Context, containerID string) (types.ContainerJSON, error)
}

func (m *mockDockerClient) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	if m.inspectFunc != nil {
		return m.inspectFunc(ctx, containerID)
	}
	return types.ContainerJSON{}, nil
}

func setupTestStore(t *testing.T) *storage.Store {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Path:           ":memory:",
			AutoMigrate:    true,
			MaxConnections: 1,
		},
	}
	store, err := storage.NewSQLiteStore(cfg)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}
	return store
}

func TestModuleManager_RestoreProxyRoutesOnStart(t *testing.T) {
	mockCli := &mockDockerClient{
		inspectFunc: func(ctx context.Context, containerID string) (types.ContainerJSON, error) {
			return types.ContainerJSON{
				NetworkSettings: &types.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"test-net": {
							IPAddress: "172.28.0.25",
						},
					},
				},
			}, nil
		},
	}

	store := setupTestStore(t)
	defer store.Close()

	cfg := &config.Config{
		Module: config.ModuleConfig{
			Enabled: true,
		},
		Proxy: config.ProxyConfig{
			Enabled: true,
		},
		Docker: config.DockerConfig{
			NetworkName: "test-net",
		},
	}
	log := logger.New()

	proxyMgr := proxy.NewManager(store, cfg, log, mockCli)
	defer proxyMgr.Stop()

	// Create server
	server := &storage.Server{
		ID:            uuid.New().String(),
		Name:          "survival",
		ProxyHostname: "play.example.com",
		ContainerID:   "srv-container",
	}
	if err := store.CreateServer(context.Background(), server); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Create running module with proxy-enabled port (e.g. Geyser UDP)
	runningMod := &storage.Module{
		ID:          uuid.New().String(),
		ServerID:    server.ID,
		Name:        "geyser",
		ContainerID: "geyser-container",
		Status:      storage.ModuleStatusRunning,
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
	if err := store.CreateModule(context.Background(), runningMod); err != nil {
		t.Fatalf("failed to create running module: %v", err)
	}

	// Create stopped module with proxy-enabled port
	stoppedMod := &storage.Module{
		ID:          uuid.New().String(),
		ServerID:    server.ID,
		Name:        "bluemap",
		ContainerID: "bluemap-container",
		Status:      storage.ModuleStatusStopped,
		Ports: []*v1.ModulePort{
			{
				Name:          "web",
				HostPort:      8100,
				ContainerPort: 8100,
				Protocol:      "tcp",
				ProxyEnabled:  true,
			},
		},
	}
	if err := store.CreateModule(context.Background(), stoppedMod); err != nil {
		t.Fatalf("failed to create stopped module: %v", err)
	}

	// Initialize and start module manager
	modMgr := NewManager(store, nil, nil, cfg, proxyMgr, log)
	if err := modMgr.Start(); err != nil {
		t.Fatalf("failed to start module manager: %v", err)
	}

	// Verify running module's route was restored
	routes := proxyMgr.GetRoutes()
	route, found := routes["udp"]
	if !found {
		t.Fatalf("expected running module's UDP route to be restored on startup, got routes: %+v", routes)
	}
	if route.BackendHost != "172.28.0.25" {
		t.Errorf("expected BackendHost 172.28.0.25, got %s", route.BackendHost)
	}
	if route.BackendPort != 19132 {
		t.Errorf("expected BackendPort 19132, got %d", route.BackendPort)
	}

	// Verify stopped module's route was NOT added
	for key, r := range routes {
		if r.BackendPort == 8100 {
			t.Errorf("expected stopped module route (port 8100) not to be restored, found under key %s", key)
		}
	}
}
