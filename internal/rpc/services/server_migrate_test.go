// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

// migrationFixture builds two nodes on distinct storage mounts with a seeded
// source world at the same logical data path — the two-host shape the OSS-234
// repro seam calls for.
type migrationFixture struct {
	svc    *ServerService
	store  *db.Store
	server *db.Server
	mountA string
	mountB string
	worldA string
	worldB string
}

func newMigrationFixture(t *testing.T) *migrationFixture {
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

	svc := NewServerService(store, nil, nil, &config.Config{
		Storage: config.StorageConfig{DataDir: filepath.Join(base, "panel-data")},
	}, nil, nil, nil, nil, nil, logger.New(), nil, nil, nil)

	node1 := &db.Node{
		ID: "node-1", Name: "Node 1", Host: "tcp://192.168.0.10:2376",
		AdvertisedIP: "192.168.0.10", Enabled: true, Status: db.NodeStatusOnline,
		StorageRoot: mountA,
	}
	if err := store.CreateNode(ctx, node1); err != nil {
		t.Fatalf("failed to create node-1: %v", err)
	}
	node2 := &db.Node{
		ID: "node-2", Name: "Node 2", Host: "tcp://192.168.0.20:2376",
		AdvertisedIP: "192.168.0.20", Enabled: true, Status: db.NodeStatusOnline,
		StorageRoot: mountB,
	}
	if err := store.CreateNode(ctx, node2); err != nil {
		t.Fatalf("failed to create node-2: %v", err)
	}

	dataPath := filepath.Join(base, "logical", "servers", "srv-data")
	server := &db.Server{
		ID: "server-1", Name: "Survival World", ModLoader: db.ModLoaderPaper, MCVersion: "1.21.1",
		NodeID: "node-1", ContainerID: "container-old-123", Status: db.StatusStopped,
		Port: 25565, Memory: 4096, DataPath: dataPath,
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	if err := store.SaveServerConfig(ctx, &db.ServerConfig{ID: "cfg-1", ServerID: "server-1"}); err != nil {
		t.Fatalf("failed to create server config: %v", err)
	}

	worldA := filepath.Join(mountA, filepath.Base(dataPath))
	worldB := filepath.Join(mountB, filepath.Base(dataPath))
	if err := os.MkdirAll(filepath.Join(worldA, "world"), 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, content := range map[string]string{
		"world/level.dat":   "level-1",
		"server.properties": "motd=hi",
	} {
		if err := os.WriteFile(filepath.Join(worldA, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return &migrationFixture{svc: svc, store: store, server: server, mountA: mountA, mountB: mountB, worldA: worldA, worldB: worldB}
}

func (f *migrationFixture) worldFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, rel := range []string{"world/level.dat", "server.properties"} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		out[rel] = string(data)
	}
	return out
}

func TestServerService_MigrateServer_Validation(t *testing.T) {
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()

	log := logger.New()
	svc := NewServerService(store, nil, nil, nil, nil, nil, nil, nil, nil, log, nil, nil, nil)
	ctx := context.Background()

	// 1. Missing IDs
	_, err := svc.MigrateServer(ctx, connect.NewRequest(&v1.MigrateServerRequest{
		Id:           "",
		TargetNodeId: "node-2",
	}))
	if err == nil {
		t.Fatal("expected error for empty server ID, got nil")
	}

	_, err = svc.MigrateServer(ctx, connect.NewRequest(&v1.MigrateServerRequest{
		Id:           "srv-1",
		TargetNodeId: "",
	}))
	if err == nil {
		t.Fatal("expected error for empty target node ID, got nil")
	}

	// 2. Non-existent server
	_, err = svc.MigrateServer(ctx, connect.NewRequest(&v1.MigrateServerRequest{
		Id:           "nonexistent-server",
		TargetNodeId: "node-2",
	}))
	if err == nil {
		t.Fatal("expected error for nonexistent server, got nil")
	}
}

func TestServerService_MigrateServer_OfflineMigration(t *testing.T) {
	f := newMigrationFixture(t)
	ctx := context.Background()

	// Migrate to the same node must fail.
	_, err := f.svc.MigrateServer(ctx, connect.NewRequest(&v1.MigrateServerRequest{
		Id:           "server-1",
		TargetNodeId: "node-1",
	}))
	if err == nil {
		t.Fatal("expected error when migrating to the same node, got nil")
	}

	// Migrate to a disabled node must fail.
	disabledNode := &db.Node{
		ID: "node-disabled", Name: "Disabled Node", Host: "tcp://192.168.0.30:2376",
		Enabled: true, Status: db.NodeStatusOnline,
	}
	if err := f.store.CreateNode(ctx, disabledNode); err != nil {
		t.Fatalf("failed to create disabled node: %v", err)
	}
	disabledNode.Enabled = false
	if err := f.store.UpdateNode(ctx, disabledNode); err != nil {
		t.Fatalf("failed to update disabled node: %v", err)
	}
	_, err = f.svc.MigrateServer(ctx, connect.NewRequest(&v1.MigrateServerRequest{
		Id:           "server-1",
		TargetNodeId: "node-disabled",
	}))
	if err == nil {
		t.Fatal("expected error when migrating to disabled node, got nil")
	}

	// Full offline migration: the world must be transferred and verified.
	res, err := f.svc.MigrateServer(ctx, connect.NewRequest(&v1.MigrateServerRequest{
		Id:           "server-1",
		TargetNodeId: "node-2",
		Force:        boolPtr(true),
	}))
	if err != nil {
		t.Fatalf("unexpected error during offline migration: %v", err)
	}
	if !res.Msg.Success {
		t.Errorf("expected success to be true, got %v", res.Msg.Success)
	}
	if res.Msg.TargetNodeId != "node-2" {
		t.Errorf("expected TargetNodeId to be node-2, got %s", res.Msg.TargetNodeId)
	}
	if res.Msg.SourceNodeId != "node-1" {
		t.Errorf("expected SourceNodeId to be node-1, got %s", res.Msg.SourceNodeId)
	}
	if strings.Contains(res.Msg.Message, "live") {
		t.Errorf("migration must not claim live migration, got %q", res.Msg.Message)
	}
	if !strings.Contains(res.Msg.Message, "world transfer verified") {
		t.Errorf("migration message must report verified transfer, got %q", res.Msg.Message)
	}

	// The target world equals the source world.
	want := map[string]string{"world/level.dat": "level-1", "server.properties": "motd=hi"}
	got := f.worldFiles(t, f.worldB)
	for rel, content := range want {
		if got[rel] != content {
			t.Errorf("target world %s = %q, want %q", rel, got[rel], content)
		}
	}
	// The source world is preserved for recovery.
	src := f.worldFiles(t, f.worldA)
	for rel, content := range want {
		if src[rel] != content {
			t.Errorf("source world %s = %q, want preserved %q", rel, src[rel], content)
		}
	}

	updatedServer, err := f.store.GetServer(ctx, "server-1")
	if err != nil {
		t.Fatalf("failed to get updated server: %v", err)
	}
	if updatedServer.NodeID != "node-2" {
		t.Errorf("expected server.NodeID in DB to be node-2, got %s", updatedServer.NodeID)
	}
	if updatedServer.ContainerID != "" {
		t.Errorf("expected server.ContainerID to be cleared on offline migration, got %s", updatedServer.ContainerID)
	}
	if err := f.store.AssertNoServerOperation(ctx, "server-1"); err != nil {
		t.Errorf("migration must release the operation lock: %v", err)
	}
}

// TestServerService_MigrateServerRefusesUntransferableWorld is the OSS-234
// red regression: a world that cannot be read and written through node-aware
// storage must not be called a successful migration. The source stays fully
// intact and nothing claims the server.
func TestServerService_MigrateServerRefusesUntransferableWorld(t *testing.T) {
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()
	ctx := context.Background()

	svc := NewServerService(store, nil, nil, &config.Config{
		Storage: config.StorageConfig{DataDir: t.TempDir()},
	}, nil, nil, nil, nil, nil, logger.New(), nil, nil, nil)

	// Remote nodes without reachable storage: identical-path assumptions must
	// not silently skip the world transfer.
	for _, n := range []*db.Node{
		{ID: "node-1", Name: "Node 1", Host: "tcp://192.168.0.10:2376", Enabled: true, Status: db.NodeStatusOnline},
		{ID: "node-2", Name: "Node 2", Host: "tcp://192.168.0.20:2376", Enabled: true, Status: db.NodeStatusOnline},
	} {
		if err := store.CreateNode(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	server := &db.Server{
		ID: "server-1", Name: "Survival", ModLoader: db.ModLoaderPaper, MCVersion: "1.21.1",
		NodeID: "node-1", ContainerID: "container-old-123", Status: db.StatusStopped,
		Port: 25565, Memory: 4096, DataPath: filepath.Join("logical", "servers", "srv-data"),
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveServerConfig(ctx, &db.ServerConfig{ID: "cfg-1", ServerID: "server-1"}); err != nil {
		t.Fatal(err)
	}

	_, err := svc.MigrateServer(ctx, connect.NewRequest(&v1.MigrateServerRequest{
		Id:           "server-1",
		TargetNodeId: "node-2",
		Force:        boolPtr(true),
	}))
	if err == nil {
		t.Fatal("migration without transferable storage must be refused")
	}

	updated, err := store.GetServer(ctx, "server-1")
	if err != nil {
		t.Fatal(err)
	}
	if updated.NodeID != "node-1" || updated.ContainerID != "container-old-123" {
		t.Fatalf("refused migration must leave ownership untouched, got node=%s container=%s", updated.NodeID, updated.ContainerID)
	}
}

// TestServerService_MigrateServerTransferFailureKeepsSourceWorld: when the
// target storage is not writable (missing mount), the migration fails and the
// source world remains the one authoritative copy.
func TestServerService_MigrateServerTransferFailureKeepsSourceWorld(t *testing.T) {
	f := newMigrationFixture(t)
	ctx := context.Background()

	// Remove the target mount: the hidden-mount guard must refuse to create
	// data under it, so the transfer aborts before any ownership change.
	if err := os.RemoveAll(f.mountB); err != nil {
		t.Fatal(err)
	}

	_, err := f.svc.MigrateServer(ctx, connect.NewRequest(&v1.MigrateServerRequest{
		Id:           "server-1",
		TargetNodeId: "node-2",
		Force:        boolPtr(true),
	}))
	if err == nil {
		t.Fatal("expected transfer failure when target storage is missing")
	}

	src := f.worldFiles(t, f.worldA)
	if src["world/level.dat"] != "level-1" {
		t.Fatalf("source world must be preserved after failed transfer, got %v", src)
	}
	updated, err := f.store.GetServer(ctx, "server-1")
	if err != nil {
		t.Fatal(err)
	}
	if updated.NodeID != "node-1" || updated.ContainerID != "container-old-123" {
		t.Fatalf("failed transfer must leave ownership untouched, got node=%s container=%s", updated.NodeID, updated.ContainerID)
	}
	if err := f.store.AssertNoServerOperation(ctx, "server-1"); err != nil {
		t.Errorf("failed migration must release the operation lock: %v", err)
	}
}

// TestServerService_MigrateServerSharedStorageNeedsNoCopy: nodes sharing one
// storage root migrate without copying; the world stays the single copy.
func TestServerService_MigrateServerSharedStorageNeedsNoCopy(t *testing.T) {
	f := newMigrationFixture(t)
	ctx := context.Background()

	node2, err := f.store.GetNode(ctx, "node-2")
	if err != nil {
		t.Fatal(err)
	}
	node2.StorageRoot = f.mountA
	if err := f.store.UpdateNode(ctx, node2); err != nil {
		t.Fatal(err)
	}

	res, err := f.svc.MigrateServer(ctx, connect.NewRequest(&v1.MigrateServerRequest{
		Id:           "server-1",
		TargetNodeId: "node-2",
		Force:        boolPtr(true),
	}))
	if err != nil {
		t.Fatalf("shared-storage migration failed: %v", err)
	}
	if !res.Msg.Success {
		t.Fatalf("expected success, got %v", res.Msg.Success)
	}
	got := f.worldFiles(t, f.worldA)
	if got["world/level.dat"] != "level-1" {
		t.Fatalf("shared world must be untouched, got %v", got)
	}
	if _, err := os.Stat(f.worldB); !os.IsNotExist(err) {
		t.Fatalf("no copy may appear outside the shared mount, stat err = %v", err)
	}
}

// TestServerService_MigrateServerConcurrentMutationRefused: the journal's
// per-server lock refuses a migration while another lifecycle mutation owns
// the world (OSS-234 acceptance).
func TestServerService_MigrateServerConcurrentMutationRefused(t *testing.T) {
	f := newMigrationFixture(t)
	ctx := context.Background()

	// Another lifecycle mutation holds the server lock.
	if _, _, err := f.store.BeginServerOperation(ctx, "server-1", "node-1", "container-old-123", "other-op", "recreate", "digest"); err != nil {
		t.Fatal(err)
	}

	_, err := f.svc.MigrateServer(ctx, connect.NewRequest(&v1.MigrateServerRequest{
		Id:           "server-1",
		TargetNodeId: "node-2",
		Force:        boolPtr(true),
	}))
	connErr, ok := err.(*connect.Error)
	if !ok || connErr.Code() != connect.CodeAborted {
		t.Fatalf("MigrateServer error = %T %v, want CodeAborted", err, err)
	}
	updated, err := f.store.GetServer(ctx, "server-1")
	if err != nil {
		t.Fatal(err)
	}
	if updated.NodeID != "node-1" {
		t.Fatalf("interleaved migration must not change ownership, got %s", updated.NodeID)
	}
}

// TestServerService_MigrateServer_CapacityChecks preserves the capacity
// guard: tight targets refuse oversized servers unless forced.
func TestServerService_MigrateServer_CapacityChecks(t *testing.T) {
	f := newMigrationFixture(t)
	ctx := context.Background()

	node2, err := f.store.GetNode(ctx, "node-2")
	if err != nil {
		t.Fatal(err)
	}
	node2.MaxServers = 1
	node2.MaxMemoryMB = 2048
	if err := f.store.UpdateNode(ctx, node2); err != nil {
		t.Fatal(err)
	}

	// Without force the 4096 MB server exceeds the target's 2048 MB limit.
	_, err = f.svc.MigrateServer(ctx, connect.NewRequest(&v1.MigrateServerRequest{
		Id:           "server-1",
		TargetNodeId: "node-2",
		Force:        boolPtr(false),
	}))
	if err == nil {
		t.Fatal("expected error due to memory capacity exhaustion, got nil")
	}

	res, err := f.svc.MigrateServer(ctx, connect.NewRequest(&v1.MigrateServerRequest{
		Id:           "server-1",
		TargetNodeId: "node-2",
		Force:        boolPtr(true),
	}))
	if err != nil {
		t.Fatalf("expected forced migration to succeed, got: %v", err)
	}
	if !res.Msg.Success {
		t.Errorf("expected res.Msg.Success to be true")
	}
}

func TestWaitForPlayable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	if err := waitForPlayable(context.Background(), ln.Addr().String(), 2*time.Second); err != nil {
		t.Fatalf("playable endpoint must be detected: %v", err)
	}
	if err := waitForPlayable(context.Background(), "127.0.0.1:1", 300*time.Millisecond); err == nil {
		t.Fatal("unreachable endpoint must fail the playability check")
	}
}

func TestGameEndpoint(t *testing.T) {
	node := &db.Node{AdvertisedIP: "192.168.0.20", Host: "tcp://10.0.0.5:2376"}
	server := &db.Server{Port: 25565}
	if got := gameEndpoint(server, node, 0); got != "192.168.0.20:25565" {
		t.Fatalf("gameEndpoint = %q", got)
	}
	nodeNoIP := &db.Node{Host: "tcp://10.0.0.5:2376"}
	if got := gameEndpoint(server, nodeNoIP, 0); got != "10.0.0.5:25565" {
		t.Fatalf("gameEndpoint host fallback = %q", got)
	}
	proxied := &db.Server{Port: 25565, ProxyHostname: "play.example.test"}
	if got := gameEndpoint(proxied, node, 25565); got != "play.example.test:25565" {
		t.Fatalf("gameEndpoint proxy = %q", got)
	}
}
