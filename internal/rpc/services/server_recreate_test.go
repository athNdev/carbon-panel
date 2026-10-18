package services

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func TestUpdateServerReturnsErrorAndPersistsRecoveryStateWhenRecreationFails(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)
	log := logger.New()
	dockerClient, err := docker.NewClient("tcp://127.0.0.1:1", log)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = dockerClient.Close() }()

	server := &storage.Server{
		ID: "update-recreate-failure", Name: "Update Recreate Failure", NodeID: "local",
		Port: 25573, Memory: 2048, MaxPlayers: 20, ContainerID: "still-tracked", Status: storage.StatusRunning,
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	if err := store.SaveServerConfig(ctx, store.CreateDefaultServerConfig(server.ID)); err != nil {
		t.Fatalf("SaveServerConfig: %v", err)
	}

	svc := &ServerService{store: store, docker: dockerClient, config: &config.Config{}, log: log}
	_, err = svc.UpdateServer(ctx, connect.NewRequest(&v1.UpdateServerRequest{
		Id: server.ID, MaxPlayers: 50,
	}))
	if err == nil {
		t.Fatal("UpdateServer() error = nil after Docker recreation failed")
	}
	connErr, ok := err.(*connect.Error)
	if !ok || connErr.Code() != connect.CodeInternal {
		t.Fatalf("UpdateServer() error = %T %v, want CodeInternal", err, err)
	}

	updated, err := store.GetServer(ctx, server.ID)
	if err != nil {
		t.Fatalf("GetServer after failed recreation: %v", err)
	}
	if updated.Status != storage.StatusError {
		t.Fatalf("stored status = %q, want error", updated.Status)
	}
	if updated.ContainerID != "still-tracked" {
		t.Fatalf("stored container ID = %q, want original identity retained", updated.ContainerID)
	}
	if updated.MaxPlayers != 50 {
		t.Fatalf("stored max players = %d, want requested setting retained", updated.MaxPlayers)
	}
}

func TestStatusAfterContainerRecreationPreservesDeepSleep(t *testing.T) {
	tests := []struct {
		name       string
		previous   storage.ServerStatus
		wasRunning bool
		want       storage.ServerStatus
	}{
		{name: "deep sleep remains deep sleep", previous: storage.StatusDeepSleep, want: storage.StatusDeepSleep},
		{name: "paused replacement becomes stopped", previous: storage.StatusPaused, want: storage.StatusStopped},
		{name: "running replacement remains running", previous: storage.StatusRunning, wasRunning: true, want: storage.StatusRunning},
		{name: "stopped replacement remains stopped", previous: storage.StatusStopped, want: storage.StatusStopped},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := statusAfterContainerRecreation(tt.previous, tt.wasRunning); got != tt.want {
				t.Fatalf("statusAfterContainerRecreation(%q, %v) = %q, want %q", tt.previous, tt.wasRunning, got, tt.want)
			}
		})
	}
}
