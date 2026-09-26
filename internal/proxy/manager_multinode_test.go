package proxy

import (
	"context"
	"fmt"
	"net/netip"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/google/uuid"
	"github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

func TestResolveBackend_LocalServer(t *testing.T) {
	mockCli := &mockDockerClient{
		inspectFunc: func(ctx context.Context, containerID string) (container.InspectResponse, error) {
			return container.InspectResponse{
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"bridge": {
							IPAddress: netip.MustParseAddr("172.17.0.4"),
						},
					},
				},
			}, nil
		},
	}

	store := setupTestStore(t)
	defer store.Close()

	cfg := &config.Config{
		Proxy: config.ProxyConfig{Enabled: true},
	}
	log := logger.New()
	mgr := NewManager(store, cfg, log, mockCli)

	server := &db.Server{
		ID:          uuid.New().String(),
		Name:        "local-server",
		NodeID:      "default",
		ContainerID: "local-container-1",
		Port:        25565,
	}

	host, port, err := mgr.resolveBackend(server)
	if err != nil {
		t.Fatalf("resolveBackend failed: %v", err)
	}
	if host != "172.17.0.4" {
		t.Errorf("expected container IP 172.17.0.4, got %s", host)
	}
	if port != 25565 {
		t.Errorf("expected port 25565, got %d", port)
	}
}

func TestResolveBackend_RemoteServer(t *testing.T) {
	// mockCli inspectFunc will fail if called, verifying remote nodes bypass Docker inspect
	mockCli := &mockDockerClient{
		inspectFunc: func(ctx context.Context, containerID string) (container.InspectResponse, error) {
			t.Fatal("docker inspect should not be called for remote node!")
			return container.InspectResponse{}, nil
		},
	}

	store := setupTestStore(t)
	defer store.Close()

	remoteNode := &db.Node{
		ID:           "node-remote-1",
		Name:         "eu-worker-1",
		Host:         "tcp://192.168.1.100:2376",
		AdvertisedIP: "192.168.1.100",
		Enabled:      true,
		IsLocal:      false,
		Status:       db.NodeStatusOnline,
	}
	if err := store.CreateNode(context.Background(), remoteNode); err != nil {
		t.Fatalf("failed to create remote node: %v", err)
	}

	cfg := &config.Config{
		Proxy: config.ProxyConfig{Enabled: true},
	}
	log := logger.New()
	mgr := NewManager(store, cfg, log, mockCli)

	server := &db.Server{
		ID:     uuid.New().String(),
		Name:   "remote-server",
		NodeID: "node-remote-1",
		Port:   25567,
	}

	host, port, err := mgr.resolveBackend(server)
	if err != nil {
		t.Fatalf("resolveBackend failed: %v", err)
	}
	if host != "192.168.1.100" {
		t.Errorf("expected advertised IP 192.168.1.100, got %s", host)
	}
	if port != 25567 {
		t.Errorf("expected server port 25567, got %d", port)
	}
}

func TestResolveBackend_RemoteServer_FallbackToHost(t *testing.T) {
	mockCli := &mockDockerClient{}

	store := setupTestStore(t)
	defer store.Close()

	remoteNode := &db.Node{
		ID:           "node-remote-2",
		Name:         "us-worker-2",
		Host:         "tcp://10.20.30.40:2376",
		AdvertisedIP: "", // empty, should fall back to Host IP
		Enabled:      true,
		IsLocal:      false,
		Status:       db.NodeStatusOnline,
	}
	if err := store.CreateNode(context.Background(), remoteNode); err != nil {
		t.Fatalf("failed to create remote node: %v", err)
	}

	cfg := &config.Config{
		Proxy: config.ProxyConfig{Enabled: true},
	}
	log := logger.New()
	mgr := NewManager(store, cfg, log, mockCli)

	server := &db.Server{
		ID:     uuid.New().String(),
		Name:   "fallback-server",
		NodeID: "node-remote-2",
		Port:   25570,
	}

	host, port, err := mgr.resolveBackend(server)
	if err != nil {
		t.Fatalf("resolveBackend failed: %v", err)
	}
	if host != "10.20.30.40" {
		t.Errorf("expected host IP 10.20.30.40, got %s", host)
	}
	if port != 25570 {
		t.Errorf("expected server port 25570, got %d", port)
	}
}

func TestManager_UpdateServerRoute_RemoteNode(t *testing.T) {
	mockCli := &mockDockerClient{}

	store := setupTestStore(t)
	defer store.Close()

	// 1. Create remote node
	remoteNode := &db.Node{
		ID:           "node-remote-proxy",
		Name:         "remote-proxy-node",
		Host:         "tcp://192.168.5.10:2376",
		AdvertisedIP: "192.168.5.10",
		Enabled:      true,
		IsLocal:      false,
		Status:       db.NodeStatusOnline,
	}
	if err := store.CreateNode(context.Background(), remoteNode); err != nil {
		t.Fatalf("failed to create remote node: %v", err)
	}

	// 2. Create proxy listener
	listener := &db.ProxyListener{
		ID:      "listener-1",
		Port:    25565,
		Enabled: true,
	}
	if err := store.CreateProxyListener(context.Background(), listener); err != nil {
		t.Fatalf("failed to create proxy listener: %v", err)
	}

	// 3. Create server on remote node
	server := &db.Server{
		ID:              uuid.New().String(),
		Name:            "crossnode-srv",
		NodeID:          remoteNode.ID,
		ProxyHostname:   "crossnode.mc.test",
		ProxyListenerID: listener.ID,
		Port:            25572,
		Status:          db.StatusRunning,
	}
	if err := store.CreateServer(context.Background(), server); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	cfg := &config.Config{
		Proxy: config.ProxyConfig{
			Enabled:     true,
			ListenPorts: []int{25565},
		},
	}
	log := logger.New()
	mgr := NewManager(store, cfg, log, mockCli)

	// Start manager
	if err := mgr.Start(); err != nil {
		t.Fatalf("mgr.Start failed: %v", err)
	}
	defer mgr.Stop()

	// Verify route was added by Start
	routes := mgr.GetRoutes()
	r, exists := routes["crossnode.mc.test"]
	if !exists {
		t.Fatalf("expected route 'crossnode.mc.test' to exist: %+v", routes)
	}
	target := fmt.Sprintf("%s:%d", r.BackendHost, r.BackendPort)
	if target != "192.168.5.10:25572" {
		t.Errorf("expected target 192.168.5.10:25572, got %s", target)
	}
}
