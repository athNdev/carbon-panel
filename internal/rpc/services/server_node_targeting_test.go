// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"archive/zip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/internal/auth"
	appconfig "github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/internal/rbac"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func TestGetServer_UnavailableRemoteDoesNotReadLocalDocker(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()

	// Keep the referenced node absent from the pool's store. With fallback
	// enabled, the legacy GetClient path would return the local default client.
	const remoteNodeID = "missing-remote-worker"

	var localInspectCalls atomic.Int32
	localAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/containers/sentinel/json") {
			localInspectCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Id":    "sentinel",
				"State": map[string]any{"Status": "running", "Running": true},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer localAPI.Close()

	localClient, err := docker.NewClient(strings.Replace(localAPI.URL, "http://", "tcp://", 1), logger.New())
	if err != nil {
		t.Fatalf("NewClient(local sentinel): %v", err)
	}
	defer func() { _ = localClient.Close() }()

	pool := docker.NewClientPool(store, localClient, logger.New())
	defer func() { _ = pool.Close() }()
	pool.SetFallbackEnabled(true)

	server := &storage.Server{
		ID: "remote-server", Name: "Remote server", NodeID: remoteNodeID,
		ContainerID: "sentinel", Status: storage.StatusStopped,
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}

	svc := &ServerService{store: store, docker: localClient, pool: pool, log: logger.New()}
	response, err := svc.GetServer(ctx, connect.NewRequest(&v1.GetServerRequest{Id: server.ID}))
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if got := response.Msg.Server.Status; got != v1.ServerStatus_SERVER_STATUS_STOPPED {
		t.Fatalf("GetServer status = %v, want stored stopped status while remote is unavailable", got)
	}
	if got := localInspectCalls.Load(); got != 0 {
		t.Fatalf("local sentinel Docker inspect calls = %d, want 0 for remote server", got)
	}
}

func TestGetDockerClientStrict_StandaloneClientRejectsRemoteNode(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()
	if err := store.CreateNode(ctx, &storage.Node{
		ID: "remote-worker", Name: "Remote worker", Host: "tcp://127.0.0.1:1", Enabled: true,
	}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	client, err := docker.NewClient("tcp://127.0.0.1:1", logger.New())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	svc := &ServerService{store: store, docker: client}
	if got, err := svc.getDockerClientStrict("remote-worker"); err == nil || got != nil {
		t.Fatalf("getDockerClientStrict(remote-worker) = (%v, %v), want unavailable error and nil client", got, err)
	}
	if got, err := svc.getDockerClientStrict("local"); err != nil || got != client {
		t.Fatalf("getDockerClientStrict(local) = (%v, %v), want standalone client", got, err)
	}
}

func TestRestoreBackup_UnavailableRemoteDoesNotUseLocalDocker(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()

	// A stale backup/server reference can outlive a deleted node. Pool fallback
	// must not restore its files through the local default daemon.
	const remoteNodeID = "missing-remote-worker"

	var localInspectCalls atomic.Int32
	localAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/containers/sentinel/json") {
			localInspectCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Id":    "sentinel",
				"State": map[string]any{"Status": "running", "Running": true},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer localAPI.Close()

	localClient, err := docker.NewClient(strings.Replace(localAPI.URL, "http://", "tcp://", 1), logger.New())
	if err != nil {
		t.Fatalf("NewClient(local sentinel): %v", err)
	}
	defer func() { _ = localClient.Close() }()

	pool := docker.NewClientPool(store, localClient, logger.New())
	defer func() { _ = pool.Close() }()
	pool.SetFallbackEnabled(true)

	dataPath := t.TempDir()
	server := &storage.Server{
		ID: "remote-server", Name: "Remote server", NodeID: remoteNodeID,
		ContainerID: "sentinel", Status: storage.StatusStopped, DataPath: dataPath,
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "backup.zip")
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	zipWriter := zip.NewWriter(archiveFile)
	entry, err := zipWriter.Create("world/sentinel.dat")
	if err != nil {
		t.Fatalf("create archive entry: %v", err)
	}
	if _, err := entry.Write([]byte("backup")); err != nil {
		t.Fatalf("write archive entry: %v", err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := archiveFile.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	if err := store.CreateBackupRecord(ctx, newBackupRecord("remote-backup", server.ID, archivePath)); err != nil {
		t.Fatalf("CreateBackupRecord: %v", err)
	}

	enforcer, err := rbac.NewEnforcer(store.DB())
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	if err := enforcer.SeedDefaultPolicies(false); err != nil {
		t.Fatalf("SeedDefaultPolicies: %v", err)
	}
	svc := NewBackupService(store, localClient, pool, appconfig.S3Config{}, logger.New())
	svc.SetEnforcer(enforcer)
	ctx = auth.WithUser(ctx, &auth.AuthenticatedUser{ID: "u-admin", Username: "admin", Roles: []string{"admin"}})
	_, err = svc.RestoreBackup(ctx, connect.NewRequest(&v1.RestoreBackupRequest{Id: "remote-backup"}))
	if err == nil {
		t.Fatal("RestoreBackup returned nil error for unavailable remote node")
	}
	connErr, ok := err.(*connect.Error)
	if !ok || connErr.Code() != connect.CodeUnavailable {
		t.Fatalf("RestoreBackup error = %T %v, want CodeUnavailable", err, err)
	}
	if got := localInspectCalls.Load(); got != 0 {
		t.Fatalf("local sentinel Docker inspect calls = %d, want 0 for remote backup", got)
	}
	if _, err := os.Stat(filepath.Join(dataPath, "world", "sentinel.dat")); !os.IsNotExist(err) {
		t.Fatalf("local restore target state = %v, want no extracted backup data", err)
	}
}

func TestRestoreBackup_UnknownDockerStatusDoesNotExtractArchive(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()

	var inspectCalls atomic.Int32
	remoteAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			w.Header().Set("Api-Version", "1.45")
			w.Header().Set("Ostype", "linux")
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/containers/sentinel/json"):
			inspectCalls.Add(1)
			http.Error(w, `{"message":"daemon temporarily unavailable"}`, http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer remoteAPI.Close()

	const remoteNodeID = "remote-worker"
	if err := store.CreateNode(ctx, &storage.Node{
		ID: remoteNodeID, Name: remoteNodeID,
		Host: strings.Replace(remoteAPI.URL, "http://", "tcp://", 1), Enabled: true,
	}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	pool := docker.NewClientPool(store, nil, logger.New())
	defer func() { _ = pool.Close() }()

	dataPath := t.TempDir()
	server := &storage.Server{
		ID: "remote-server-unknown-status", Name: "Remote server", NodeID: remoteNodeID,
		ContainerID: "sentinel", Status: storage.StatusRunning, DataPath: dataPath,
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "backup.zip")
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	zipWriter := zip.NewWriter(archiveFile)
	entry, err := zipWriter.Create("world/sentinel.dat")
	if err != nil {
		t.Fatalf("create archive entry: %v", err)
	}
	if _, err := entry.Write([]byte("backup")); err != nil {
		t.Fatalf("write archive entry: %v", err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := archiveFile.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	if err := store.CreateBackupRecord(ctx, newBackupRecord("unknown-status-backup", server.ID, archivePath)); err != nil {
		t.Fatalf("CreateBackupRecord: %v", err)
	}

	enforcer, err := rbac.NewEnforcer(store.DB())
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	if err := enforcer.SeedDefaultPolicies(false); err != nil {
		t.Fatalf("SeedDefaultPolicies: %v", err)
	}
	svc := NewBackupService(store, nil, pool, appconfig.S3Config{}, logger.New())
	svc.SetEnforcer(enforcer)
	ctx = auth.WithUser(ctx, &auth.AuthenticatedUser{ID: "u-admin", Username: "admin", Roles: []string{"admin"}})
	_, err = svc.RestoreBackup(ctx, connect.NewRequest(&v1.RestoreBackupRequest{Id: "unknown-status-backup"}))
	if err == nil {
		t.Fatal("RestoreBackup returned nil error for unknown Docker status")
	}
	connErr, ok := err.(*connect.Error)
	if !ok || connErr.Code() != connect.CodeUnavailable {
		t.Fatalf("RestoreBackup error = %T %v, want CodeUnavailable", err, err)
	}
	if got := inspectCalls.Load(); got != 1 {
		t.Fatalf("remote Docker inspect calls = %d, want 1", got)
	}
	if _, err := os.Stat(filepath.Join(dataPath, "world", "sentinel.dat")); !os.IsNotExist(err) {
		t.Fatalf("restore target state = %v, want archive not extracted", err)
	}
}
