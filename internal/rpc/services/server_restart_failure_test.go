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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRestartServer_SetsStatusErrorOnFailure(t *testing.T) {
	store := setupTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	log := logger.New()

	cfg := &config.Config{
		Storage: config.StorageConfig{DataDir: t.TempDir()},
	}

	ctx := context.Background()

	require.NoError(t, store.CreateNode(ctx, &db.Node{
		ID:      "test-node",
		Name:    "Test Node",
		Host:    "tcp://127.0.0.1:2375",
		Enabled: true,
		Status:  db.NodeStatusOnline,
	}))

	// Unreachable docker endpoint ensures restart/recreate calls fail
	dockerCli, err := docker.NewClient("tcp://127.0.0.1:1", log)
	require.NoError(t, err)

	svc := NewServerService(store, dockerCli, nil, cfg, nil, nil, nil, nil, nil, log, nil, nil, nil)
	svc.hostMemoryMB = func(context.Context, string) (int64, error) { return 16 * 1024, nil }

	server := &db.Server{
		ID:          "srv-restart-fail",
		Name:        "Restart Fail Server",
		NodeID:      "test-node",
		Port:        25565,
		Memory:      1024,
		Status:      db.StatusRunning,
		ContainerID: "deadbeef-fake",
		DataPath:    t.TempDir(),
	}
	require.NoError(t, store.CreateServer(ctx, server))

	// Attempt RestartServer
	_, err = svc.RestartServer(ctx, connect.NewRequest(&v1.RestartServerRequest{Id: server.ID}))
	require.Error(t, err)

	// Verify server status was updated to Error in the database
	updated, err := store.GetServer(ctx, server.ID)
	require.NoError(t, err)
	assert.Equal(t, db.StatusError, updated.Status)
}
