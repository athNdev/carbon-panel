// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

// fakeNodeDaemon is a minimal Docker daemon for two-host migration tests.
type fakeNodeDaemon struct {
	mu          sync.Mutex
	calls       []string
	createFails bool
	startFails  bool
	stopFails   bool
}

// record stores "METHOD /path" with the negotiated API version prefix
// stripped so assertions can match stable paths.
func (f *fakeNodeDaemon) record(method, path string) {
	if i := strings.Index(path[1:], "/"); strings.HasPrefix(path, "/v1.") && i >= 0 {
		path = path[i+1:]
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method+" "+path)
}

func (f *fakeNodeDaemon) has(call string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if strings.Contains(c, call) {
			return true
		}
	}
	return false
}

func (f *fakeNodeDaemon) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.record(r.Method, r.URL.Path)
		switch {
		case r.URL.Path == "/_ping":
			w.Header().Set("Api-Version", "1.45")
			w.Header().Set("Ostype", "linux")
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"sha256:fixture"}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/old-1/json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"old-1","Name":"/carbon-panel-server-server-1","State":{"Status":"running","Running":true}}`))
		case strings.HasSuffix(r.URL.Path, "/containers/old-1/stop"):
			if f.stopFails {
				http.Error(w, `{"message":"stop failed"}`, http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/containers/old-1/kill"):
			http.Error(w, `{"message":"kill failed"}`, http.StatusInternalServerError)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create"):
			if f.createFails {
				http.Error(w, `{"message":"create failed"}`, http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"Id":"new-2","Warnings":[]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/new-2/start"):
			if f.startFails {
				http.Error(w, `{"message":"start failed"}`, http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/old-1/start"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}
}

// runningMigrationFixture builds the two-host shape for running-path
// migrations: two fake daemons, two storage mounts with a known source world,
// and a player-facing listener for the playability probe.
type runningMigrationFixture struct {
	svc      *ServerService
	store    *db.Store
	source   *fakeNodeDaemon
	target   *fakeNodeDaemon
	worldA   string
	listener net.Listener
}

func newRunningMigrationFixture(t *testing.T) *runningMigrationFixture {
	t.Helper()
	store := setupTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	base := t.TempDir()
	mountA := filepath.Join(base, "mount-a")
	mountB := filepath.Join(base, "mount-b")
	for _, m := range []string{mountA, mountB} {
		if err := os.MkdirAll(m, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// A local listener stands in for the target server's player endpoint.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, aerr := listener.Accept()
			if aerr != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	_, portStr, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}

	source := &fakeNodeDaemon{}
	target := &fakeNodeDaemon{}
	srcSrv := httptest.NewServer(source.handler())
	t.Cleanup(srcSrv.Close)
	dstSrv := httptest.NewServer(target.handler())
	t.Cleanup(dstSrv.Close)

	log := logger.New()
	dockerCli, err := docker.NewClient("tcp://127.0.0.1:1", log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dockerCli.Close() })

	svc := NewServerService(store, dockerCli, nil, &config.Config{
		Storage: config.StorageConfig{DataDir: filepath.Join(base, "panel-data")},
	}, nil, nil, nil, nil, nil, log, nil, nil, nil)

	// Both nodes are remote with reachable shared storage: the mapping model
	// from ADR 0005, with each host seeing its own mount at the same logical
	// data path.
	node1 := &db.Node{
		ID: "node-1", Name: "Node 1", Host: "tcp://" + strings.TrimPrefix(srcSrv.URL, "http://"), AdvertisedIP: "127.0.0.1",
		Enabled: true, Status: db.NodeStatusOnline, StorageRoot: mountA,
	}
	if err := store.CreateNode(ctx, node1); err != nil {
		t.Fatal(err)
	}
	node2 := &db.Node{
		ID: "node-2", Name: "Node 2", Host: "tcp://" + strings.TrimPrefix(dstSrv.URL, "http://"), AdvertisedIP: "127.0.0.1",
		Enabled: true, Status: db.NodeStatusOnline, StorageRoot: mountB,
	}
	if err := store.CreateNode(ctx, node2); err != nil {
		t.Fatal(err)
	}

	dataPath := filepath.Join(base, "logical", "servers", "srv-data")
	server := &db.Server{
		ID: "server-1", Name: "Survival World", ModLoader: db.ModLoaderPaper, MCVersion: "1.21.1",
		NodeID: "node-1", ContainerID: "old-1", Status: db.StatusRunning,
		Port: port, Memory: 4096, DataPath: dataPath,
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveServerConfig(ctx, &db.ServerConfig{ID: "cfg-1", ServerID: "server-1"}); err != nil {
		t.Fatal(err)
	}

	worldA := filepath.Join(mountA, filepath.Base(dataPath))
	if err := os.MkdirAll(filepath.Join(worldA, "world"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldA, "world", "level.dat"), []byte("level-1"), 0o644); err != nil {
		t.Fatal(err)
	}

	return &runningMigrationFixture{svc: svc, store: store, source: source, target: target, worldA: worldA, listener: listener}
}

func (f *runningMigrationFixture) migrate(t *testing.T) error {
	t.Helper()
	_, err := f.svc.MigrateServer(context.Background(), connect.NewRequest(&v1.MigrateServerRequest{
		Id:           "server-1",
		TargetNodeId: "node-2",
		Force:        boolPtr(true),
	}))
	return err
}

func TestMigrateServerRunningCutsOverAfterPlayability(t *testing.T) {
	f := newRunningMigrationFixture(t)

	if err := f.migrate(t); err != nil {
		t.Fatalf("running migration failed: %v", err)
	}
	if !f.source.has("POST /containers/old-1/stop") {
		t.Fatal("source container must be stopped before transfer")
	}
	if !f.target.has("POST /containers/create") || !f.target.has("POST /containers/new-2/start") {
		t.Fatal("target container must be created and started")
	}
	if !f.source.has("DELETE /containers/old-1") {
		t.Fatal("source container must be cleaned up after committed cutover")
	}
	updated, err := f.store.GetServer(context.Background(), "server-1")
	if err != nil {
		t.Fatal(err)
	}
	if updated.NodeID != "node-2" || updated.ContainerID != "new-2" || updated.Status != db.StatusRunning {
		t.Fatalf("ownership not committed: node=%s container=%s status=%s", updated.NodeID, updated.ContainerID, updated.Status)
	}
	if err := f.store.AssertNoServerOperation(context.Background(), "server-1"); err != nil {
		t.Errorf("migration must release the operation lock: %v", err)
	}
}

func TestMigrateServerStopFailureAbortsBeforeTransfer(t *testing.T) {
	f := newRunningMigrationFixture(t)
	f.source.stopFails = true

	if err := f.migrate(t); err == nil {
		t.Fatal("unclean source stop must abort the migration")
	}
	if f.target.has("POST /containers/create") {
		t.Fatal("no target container may be created while the source is not quiesced")
	}
	updated, err := f.store.GetServer(context.Background(), "server-1")
	if err != nil {
		t.Fatal(err)
	}
	if updated.NodeID != "node-1" {
		t.Fatalf("aborted migration must leave ownership on the source, got %s", updated.NodeID)
	}
}

func TestMigrateServerCreateFailureRestoresSource(t *testing.T) {
	f := newRunningMigrationFixture(t)
	f.target.createFails = true

	if err := f.migrate(t); err == nil {
		t.Fatal("target create failure must fail the migration")
	}
	if !f.source.has("POST /containers/old-1/start") {
		t.Fatal("source container must be restarted on rollback")
	}
	if f.source.has("DELETE /containers/old-1") {
		t.Fatal("source container must survive a failed migration")
	}
	updated, err := f.store.GetServer(context.Background(), "server-1")
	if err != nil {
		t.Fatal(err)
	}
	if updated.NodeID != "node-1" || updated.ContainerID != "old-1" {
		t.Fatalf("rollback must keep source ownership, got node=%s container=%s", updated.NodeID, updated.ContainerID)
	}
}

func TestMigrateServerStartFailureRemovesTargetAndRestoresSource(t *testing.T) {
	f := newRunningMigrationFixture(t)
	f.target.startFails = true

	if err := f.migrate(t); err == nil {
		t.Fatal("target start failure must fail the migration")
	}
	if !f.target.has("DELETE /containers/new-2") {
		t.Fatal("unusable target container must be removed on rollback")
	}
	if !f.source.has("POST /containers/old-1/start") {
		t.Fatal("source container must be restarted on rollback")
	}
	if f.source.has("DELETE /containers/old-1") {
		t.Fatal("source container must survive a failed migration")
	}
	updated, err := f.store.GetServer(context.Background(), "server-1")
	if err != nil {
		t.Fatal(err)
	}
	if updated.NodeID != "node-1" || updated.ContainerID != "old-1" {
		t.Fatalf("rollback must keep source ownership, got node=%s container=%s", updated.NodeID, updated.ContainerID)
	}
}

func TestMigrateServerPlayabilityTimeoutRollsBack(t *testing.T) {
	f := newRunningMigrationFixture(t)

	// Point the playability probe at a dead endpoint and bound the wait.
	_ = f.listener.Close()
	oldTimeout := migrationPlayabilityTimeout
	migrationPlayabilityTimeout = 300 * time.Millisecond
	t.Cleanup(func() { migrationPlayabilityTimeout = oldTimeout })

	if err := f.migrate(t); err == nil {
		t.Fatal("a target that never accepts players must not report success")
	}
	if !f.target.has("DELETE /containers/new-2") {
		t.Fatal("unplayable target container must be removed on rollback")
	}
	if !f.source.has("POST /containers/old-1/start") {
		t.Fatal("source container must be restarted on rollback")
	}
	updated, err := f.store.GetServer(context.Background(), "server-1")
	if err != nil {
		t.Fatal(err)
	}
	if updated.NodeID != "node-1" {
		t.Fatalf("rollback must keep source ownership, got %s", updated.NodeID)
	}
}
