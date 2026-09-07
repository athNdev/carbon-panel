package proxy

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
	"github.com/google/uuid"
	"github.com/nickheyer/discopanel/internal/config"
	"github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/pkg/logger"
)

func TestResolveBackend_LocalServer(t *testing.T) {
	mockCli := &mockDockerClient{
		inspectFunc: func(ctx context.Context, containerID string) (types.ContainerJSON, error) {
			return types.ContainerJSON{
				NetworkSettings: &types.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"bridge": {
							IPAddress: "172.17.0.4",
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
		inspectFunc: func(ctx context.Context, containerID string) (types.ContainerJSON, error) {
			t.Fatal("docker inspect should not be called for remote node!")
			return types.ContainerJSON{}, nil
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
	route, exists := routes["crossnode.mc.test"]
	if !exists {
		t.Fatalf("expected route 'crossnode.mc.test' to exist in routes: %+v", routes)
	}
	if route.BackendHost != "192.168.5.10" {
		t.Errorf("expected BackendHost 192.168.5.10, got %s", route.BackendHost)
	}
	if route.BackendPort != 25572 {
		t.Errorf("expected BackendPort 25572, got %d", route.BackendPort)
	}

	// Now stop the server and update route
	server.Status = db.StatusStopped
	if err := store.UpdateServer(context.Background(), server); err != nil {
		t.Fatalf("failed to update server: %v", err)
	}
	if err := mgr.UpdateServerRoute(server); err != nil {
		t.Fatalf("UpdateServerRoute failed: %v", err)
	}

	// Verify route was removed on stopped
	routes = mgr.GetRoutes()
	if _, exists := routes["crossnode.mc.test"]; exists {
		t.Errorf("expected route to be removed after server stopped, but it still exists")
	}

	// Start the server again and update route
	server.Status = db.StatusRunning
	if err := store.UpdateServer(context.Background(), server); err != nil {
		t.Fatalf("failed to update server: %v", err)
	}
	if err := mgr.UpdateServerRoute(server); err != nil {
		t.Fatalf("UpdateServerRoute failed: %v", err)
	}

	routes = mgr.GetRoutes()
	route, exists = routes["crossnode.mc.test"]
	if !exists {
		t.Fatalf("expected route 'crossnode.mc.test' to exist again after start")
	}
	if route.BackendHost != "192.168.5.10" || route.BackendPort != 25572 {
		t.Errorf("unexpected route target: %s:%d", route.BackendHost, route.BackendPort)
	}

	// Remove route explicitly
	if err := mgr.RemoveServerRoute(server.ID); err != nil {
		t.Fatalf("RemoveServerRoute failed: %v", err)
	}
	routes = mgr.GetRoutes()
	if _, exists := routes["crossnode.mc.test"]; exists {
		t.Errorf("expected route to be removed after RemoveServerRoute")
	}
}
