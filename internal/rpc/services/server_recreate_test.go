// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


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

func TestStartServer_PausedContainerIsRefrozenWhenStatusPersistenceFails(t *testing.T) {
	var mu sync.Mutex
	paused := true
	var unpauseCalls, pauseCalls int
	dockerAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			w.Header().Set("API-Version", "1.41")
			_, _ = w.Write([]byte("OK"))
		case strings.HasSuffix(r.URL.Path, "/containers/paused/unpause"):
			mu.Lock()
			paused = false
			unpauseCalls++
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/containers/paused/pause"):
			mu.Lock()
			paused = true
			pauseCalls++
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/containers/paused/json"):
			mu.Lock()
			isPaused := paused
			mu.Unlock()
			state := "running"
			if isPaused {
				state = "paused"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Id":    "paused",
				"State": map[string]any{"Status": state, "Running": true, "Paused": isPaused},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer dockerAPI.Close()

	svc, _ := newDockerPrivilegeTestService(t)
	if err := svc.docker.Close(); err != nil {
		t.Fatalf("close default Docker client: %v", err)
	}
	t.Setenv("DOCKER_HOST", "")
	dockerClient, err := docker.NewClient(strings.Replace(dockerAPI.URL, "http://", "tcp://", 1), svc.log)
	if err != nil {
		t.Fatalf("create Docker client: %v", err)
	}
	t.Cleanup(func() { _ = dockerClient.Close() })
	svc.docker = dockerClient
	svc.pool = nil

	server := &storage.Server{
		ID: "resume-persistence-failure", Name: "Resume Persistence Failure", NodeID: "test-node",
		Port: 25671, Memory: 2048, ContainerID: "paused", Status: storage.StatusPaused,
	}
	if err := svc.store.CreateServer(context.Background(), server); err != nil {
		t.Fatalf("create server fixture: %v", err)
	}
	if err := svc.store.SaveServerConfig(context.Background(), svc.store.CreateDefaultServerConfig(server.ID)); err != nil {
		t.Fatalf("save server config fixture: %v", err)
	}
	if err := svc.store.DB().Exec(`CREATE TRIGGER fail_running_status BEFORE UPDATE OF status ON servers
		WHEN NEW.status = 'running' BEGIN SELECT RAISE(ABORT, 'injected persistence failure'); END`).Error; err != nil {
		t.Fatalf("create persistence failure trigger: %v", err)
	}

	_, err = svc.StartServer(ctxWithRole("operator"), connect.NewRequest(&v1.StartServerRequest{Id: server.ID}))
	if err == nil {
		t.Fatal("StartServer() error = nil, want the injected status persistence failure")
	}
	mu.Lock()
	defer mu.Unlock()
	if unpauseCalls != 1 {
		t.Fatalf("unpause calls = %d, want 1", unpauseCalls)
	}
	if !paused || pauseCalls != 1 {
		t.Fatalf("container paused=%v after status persistence failure, pause calls=%d; want confirmed refreeze", paused, pauseCalls)
	}
	stored, err := svc.store.GetServer(context.Background(), server.ID)
	if err != nil {
		t.Fatalf("reload server: %v", err)
	}
	if stored.Status != storage.StatusPaused {
		t.Fatalf("stored status = %q, want paused after successful refreeze", stored.Status)
	}
}

func TestStartServer_PausedContainerStaysFrozenWhenStagedConfigReadFails(t *testing.T) {
	var mu sync.Mutex
	paused := true
	unpauseCalls := 0
	dockerAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			w.Header().Set("API-Version", "1.41")
			_, _ = w.Write([]byte("OK"))
		case strings.HasSuffix(r.URL.Path, "/containers/paused/unpause"):
			mu.Lock()
			paused = false
			unpauseCalls++
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer dockerAPI.Close()

	svc, _ := newDockerPrivilegeTestService(t)
	if err := svc.docker.Close(); err != nil {
		t.Fatalf("close default Docker client: %v", err)
	}
	t.Setenv("DOCKER_HOST", "")
	dockerClient, err := docker.NewClient(strings.Replace(dockerAPI.URL, "http://", "tcp://", 1), svc.log)
	if err != nil {
		t.Fatalf("create Docker client: %v", err)
	}
	t.Cleanup(func() { _ = dockerClient.Close() })
	svc.docker = dockerClient
	svc.pool = nil

	lastStarted := time.Now().Add(-time.Minute)
	server := &storage.Server{
		ID: "staged-config-read-failure", Name: "Staged Config Read Failure", NodeID: "test-node",
		Port: 25672, Memory: 2048, ContainerID: "paused", Status: storage.StatusPaused, LastStarted: &lastStarted,
	}
	if err := svc.store.CreateServer(context.Background(), server); err != nil {
		t.Fatalf("create server fixture: %v", err)
	}
	if err := svc.store.SaveServerConfig(context.Background(), svc.store.CreateDefaultServerConfig(server.ID)); err != nil {
		t.Fatalf("save server config fixture: %v", err)
	}
	staged, err := svc.store.StageConfigChange(context.Background(), server.ID, map[string]any{"motd": "next boot"}, storage.StagedApplyOnRestart, "")
	if err != nil {
		t.Fatalf("stage config change: %v", err)
	}
	if _, err := svc.store.ApplyStagedConfigChange(context.Background(), staged.ID); err != nil {
		t.Fatalf("apply staged change: %v", err)
	}
	if err := svc.store.DB().Exec("DROP TABLE server_configs").Error; err != nil {
		t.Fatalf("inject staged config read failure: %v", err)
	}

	_, err = svc.StartServer(ctxWithRole("operator"), connect.NewRequest(&v1.StartServerRequest{Id: server.ID}))
	if err == nil {
		t.Fatal("StartServer() error = nil, want staged config read failure")
	}
	mu.Lock()
	defer mu.Unlock()
	if !paused || unpauseCalls != 0 {
		t.Fatalf("container paused=%v after config read failure, unpause calls=%d; want no premature unpause", paused, unpauseCalls)
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
	t.Cleanup(func() {
		if err := dockerClient.Close(); err != nil {
			t.Errorf("close Docker client: %v", err)
		}
	})
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

func TestCreateServer_RemovesUntrackedContainerWhenIdentityPersistenceFails(t *testing.T) {
	var mu sync.Mutex
	removed := 0
	dockerAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			_ = json.NewEncoder(w).Encode(map[string]any{"Id": "sha256:test-image"})
		case strings.Contains(r.URL.Path, "/containers/json"):
			_, _ = w.Write([]byte("[]"))
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/containers/created-container"):
			mu.Lock()
			removed++
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create"):
			_ = json.NewEncoder(w).Encode(map[string]any{"Id": "created-container"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer dockerAPI.Close()

	svc, _ := newDockerPrivilegeTestService(t)
	if err := svc.docker.Close(); err != nil {
		t.Fatalf("close default Docker client: %v", err)
	}
	dockerClient, err := docker.NewClient(strings.Replace(dockerAPI.URL, "http://", "tcp://", 1), svc.log)
	if err != nil {
		t.Fatalf("create Docker client: %v", err)
	}
	t.Cleanup(func() { _ = dockerClient.Close() })
	svc.pool = nil
	svc.docker = dockerClient
	if err := svc.store.DB().Exec(`CREATE TRIGGER fail_container_id BEFORE UPDATE OF container_id ON servers
		WHEN NEW.container_id = 'created-container' BEGIN SELECT RAISE(ABORT, 'injected container identity persistence failure'); END`).Error; err != nil {
		t.Fatalf("create persistence failure trigger: %v", err)
	}

	resp, err := svc.CreateServer(ctxWithRole("operator"), connect.NewRequest(&v1.CreateServerRequest{
		Name: "cleanup-on-persist-failure", McVersion: "1.20.1", DockerImage: "java21",
		NodeId: "test-node", Port: 25673, Memory: 2048,
	}))
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	waitForServerTransition(t, resp.Msg.Server.Id)

	mu.Lock()
	gotRemoved := removed
	mu.Unlock()
	if gotRemoved != 1 {
		t.Fatalf("container removal calls = %d, want 1 after identity persistence failure", gotRemoved)
	}
	stored, err := svc.store.GetServer(context.Background(), resp.Msg.Server.Id)
	if err != nil {
		t.Fatalf("reload server: %v", err)
	}
	if stored.ContainerID != "" {
		t.Fatalf("stored container ID = %q, want empty after cleanup", stored.ContainerID)
	}
	if stored.Status != storage.StatusError {
		t.Fatalf("stored status = %q, want error after async creation cleanup", stored.Status)
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
