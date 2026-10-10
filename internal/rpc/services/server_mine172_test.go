package services

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

// TestStartServer_FailureDoesNotSwallowError ensures that when container start fails
// and container recreation also fails, StartServer returns an error and sets the server
// status to StatusError instead of masking the failure as StatusStarting (MINE-172).
func TestStartServer_FailureDoesNotSwallowError(t *testing.T) {
	store := setupTestStore(t)
	log := logger.New()

	cfg := &config.Config{
		Storage: config.StorageConfig{DataDir: t.TempDir()},
	}

	dockerCli, err := docker.NewClient("tcp://127.0.0.1:1", log)
	if err != nil {
		t.Fatalf("failed to create docker client stub: %v", err)
	}

	svc := NewServerService(store, dockerCli, nil, cfg, nil, nil, nil, nil, nil, log, nil, nil, nil)
	ctx := context.Background()

	// Seed a test server in stopped state with a container ID
	server := &db.Server{
		ID:          "test-srv-1",
		Name:        "Vanilla Server",
		Port:        25565,
		Status:      db.StatusStopped,
		ContainerID: "dead-container-id",
		ModLoader:   db.ModLoaderVanilla,
		MCVersion:   "1.21",
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("failed to seed server: %v", err)
	}

	// Seed server config
	serverConfig := &db.ServerConfig{
		ID:       server.ID,
		ServerID: server.ID,
	}
	if err := store.SaveServerConfig(ctx, serverConfig); err != nil {
		t.Fatalf("failed to seed server config: %v", err)
	}

	// StartServer should fail because dockerCli cannot connect to 127.0.0.1:1
	resp, err := svc.StartServer(ctx, connect.NewRequest(&v1.StartServerRequest{
		Id: server.ID,
	}))
	if err == nil {
		t.Fatalf("expected StartServer to fail, but it returned success with status %v", resp.Msg.GetStatus())
	}

	// Server status in DB must NOT be starting
	updated, err := store.GetServer(ctx, server.ID)
	if err != nil {
		t.Fatalf("failed to fetch updated server: %v", err)
	}
	if updated.Status != db.StatusError {
		t.Fatalf("expected server status to be %q, got %q", db.StatusError, updated.Status)
	}
}

// TestGetNextAvailablePort_ExcludesConfiguredPorts ensures that GetNextAvailablePort
// reserves proxy ports and handles empty / populated stores.
func TestGetNextAvailablePort_ExcludesConfiguredPorts(t *testing.T) {
	store := setupTestStore(t)
	log := logger.New()

	cfg := &config.Config{
		Storage: config.StorageConfig{DataDir: t.TempDir()},
		Proxy: config.ProxyConfig{
			Enabled:     true,
			ListenPorts: []int{25565, 25566},
		},
	}

	svc := NewServerService(store, nil, nil, cfg, nil, nil, nil, nil, nil, log, nil, nil, nil)
	ctx := context.Background()

	resp, err := svc.GetNextAvailablePort(ctx, connect.NewRequest(&v1.GetNextAvailablePortRequest{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Since 25565 and 25566 are reserved by proxy, the next port must be 25567
	if resp.Msg.Port != 25567 {
		t.Fatalf("expected next port to be 25567, got %d", resp.Msg.Port)
	}
}
