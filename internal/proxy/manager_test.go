// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package proxy

import (
	"context"
	"net/netip"
	"fmt"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/google/uuid"
	"github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

type mockDockerClient struct {
	DockerInspector
	inspectFunc func(ctx context.Context, containerID string) (container.InspectResponse, error)
	closed      bool
}

func (m *mockDockerClient) ContainerInspect(ctx context.Context, containerID string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	if m.inspectFunc != nil {
		resp, err := m.inspectFunc(ctx, containerID)
		return client.ContainerInspectResult{Container: resp}, err
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
						"empty-net": {},
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

func TestManager_AddAndRemoveModuleRoute(t *testing.T) {
	mockCli := &mockDockerClient{
		inspectFunc: func(ctx context.Context, containerID string) (container.InspectResponse, error) {
			return container.InspectResponse{
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"test-net": {
							IPAddress: netip.MustParseAddr("172.28.0.10"),
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
