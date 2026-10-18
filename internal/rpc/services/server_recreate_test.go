package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestCreateServer_HoldsTransitionUntilAsyncContainerCreationCompletes(t *testing.T) {
	createEntered := make(chan struct{})
	releaseCreate := make(chan struct{})
	var createOnce sync.Once
	dockerAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			_ = json.NewEncoder(w).Encode(map[string]any{"Id": "sha256:test-image"})
		case strings.Contains(r.URL.Path, "/containers/json"):
			_, _ = w.Write([]byte("[]"))
		case strings.Contains(r.URL.Path, "/containers/create"):
			createOnce.Do(func() { close(createEntered) })
			<-releaseCreate
			_ = json.NewEncoder(w).Encode(map[string]any{"Id": "created-container"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer dockerAPI.Close()

	svc, _ := newDockerPrivilegeTestService(t)
	if svc.docker != nil {
		_ = svc.docker.Close()
	}
	dockerClient, err := docker.NewClient(strings.Replace(dockerAPI.URL, "http://", "tcp://", 1), svc.log)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer dockerClient.Close()
	svc.pool = nil
	svc.docker = dockerClient

	resp, err := svc.CreateServer(ctxWithRole("operator"), connect.NewRequest(&v1.CreateServerRequest{
		Name: "async-create-transition", McVersion: "1.20.1", DockerImage: "java21",
		NodeId: "test-node", Port: 25641, Memory: 2048,
	}))
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	select {
	case <-createEntered:
	case <-time.After(5 * time.Second):
		close(releaseCreate)
		t.Fatal("async Docker container creation did not reach the blocked create call")
	}

	_, deleteErr := svc.DeleteServer(ctxWithRole("operator"), connect.NewRequest(&v1.DeleteServerRequest{Id: resp.Msg.Server.Id}))
	if deleteErr == nil {
		close(releaseCreate)
		t.Fatal("DeleteServer succeeded while async container creation was in flight")
	}
	connErr, ok := deleteErr.(*connect.Error)
	if !ok || connErr.Code() != connect.CodeAborted {
		close(releaseCreate)
		t.Fatalf("DeleteServer error = %T %v, want CodeAborted", deleteErr, deleteErr)
	}

	close(releaseCreate)
	waitForServerTransition(t, resp.Msg.Server.Id)
	server, err := svc.store.GetServer(context.Background(), resp.Msg.Server.Id)
	if err != nil {
		t.Fatalf("GetServer after async create: %v", err)
	}
	if server.ContainerID != "created-container" {
		t.Fatalf("ContainerID = %q, want created-container", server.ContainerID)
	}
}

func TestDeleteServer_NodeUnavailablePreservesServerAndData(t *testing.T) {
	ctx := context.Background()
	svc, _ := newDockerPrivilegeTestService(t)
	if svc.docker != nil {
		_ = svc.docker.Close()
	}
	svc.pool = nil
	svc.docker = nil

	dataPath := filepath.Join(t.TempDir(), "server-data")
	if err := os.MkdirAll(dataPath, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	marker := filepath.Join(dataPath, "world.dat")
	if err := os.WriteFile(marker, []byte("world"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	server := &storage.Server{
		ID: "delete-node-unavailable", Name: "Delete Node Unavailable", NodeID: "test-node",
		Port: 25642, Memory: 2048, ContainerID: "unreachable-container", DataPath: dataPath,
		Status: storage.StatusStopped,
	}
	if err := svc.store.CreateServer(ctx, server); err != nil {
		t.Fatalf("CreateServer fixture: %v", err)
	}

	_, err := svc.DeleteServer(ctx, connect.NewRequest(&v1.DeleteServerRequest{Id: server.ID}))
	if err == nil {
		t.Fatal("DeleteServer succeeded without reaching the Docker node")
	}
	connErr, ok := err.(*connect.Error)
	if !ok || connErr.Code() != connect.CodeUnavailable {
		t.Fatalf("DeleteServer error = %T %v, want CodeUnavailable", err, err)
	}
	if _, err := svc.store.GetServer(ctx, server.ID); err != nil {
		t.Fatalf("server row was removed after node failure: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("server data was removed after node failure: %v", err)
	}
}

func waitForServerTransition(t *testing.T, serverID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for docker.ServerTransitionInFlight(serverID) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if docker.ServerTransitionInFlight(serverID) {
		t.Fatalf("server transition for %s did not finish", serverID)
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
