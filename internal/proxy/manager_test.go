package proxy

import (
	"context"
	"fmt"
	"net/netip"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

type mockDockerClient struct {
	inspectFunc func(ctx context.Context, containerID string) (container.InspectResponse, error)
	closed      bool
}

func (m *mockDockerClient) ContainerInspect(ctx context.Context, containerID string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	if m.inspectFunc != nil {
		res, err := m.inspectFunc(ctx, containerID)
		return client.ContainerInspectResult{Container: res}, err
	}
	return client.ContainerInspectResult{}, fmt.Errorf("not implemented")
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
		inspectFunc: func(ctx context.Context, containerID string) (container.InspectResponse, error) {
			return container.InspectResponse{
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"bridge": {
							IPAddress: netip.MustParseAddr("172.17.0.2"),
						},
						"carbon-panel-net": {
							IPAddress: netip.MustParseAddr("172.20.0.5"),
						},
					},
				},
			}, nil
		},
	}

	ip, err := GetContainerIP(mockCli, "container-123", "carbon-panel-net")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip != "172.20.0.5" {
		t.Errorf("expected IP 172.20.0.5, got %s", ip)
	}
}

func TestGetContainerIP_FallbackNetwork(t *testing.T) {
	mockCli := &mockDockerClient{
		inspectFunc: func(ctx context.Context, containerID string) (container.InspectResponse, error) {
			return container.InspectResponse{
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"other-net": {
							IPAddress: netip.MustParseAddr("10.0.0.2"),
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
		inspectFunc: func(ctx context.Context, containerID string) (container.InspectResponse, error) {
			return container.InspectResponse{
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"empty-net": {
							IPAddress: netip.Addr{},
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
		inspectFunc: func(ctx context.Context, containerID string) (container.InspectResponse, error) {
			return container.InspectResponse{
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"test-net": {
							IPAddress: netip.MustParseAddr("192.168.1.100"),
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
