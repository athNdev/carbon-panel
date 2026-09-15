package services

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func TestServerService_MigrateServer_Validation(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

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
	store := setupTestStore(t)
	defer store.Close()

	log := logger.New()
	svc := NewServerService(store, nil, nil, nil, nil, nil, nil, nil, nil, log, nil, nil, nil)
	ctx := context.Background()

	// Create test nodes
	node1 := &db.Node{
		ID:           "node-1",
		Name:         "Worker Node 1",
		Host:         "tcp://192.168.0.10:2376",
		AdvertisedIP: "192.168.0.10",
		Enabled:      true,
		Status:       db.NodeStatusOnline,
	}
	if err := store.CreateNode(ctx, node1); err != nil {
		t.Fatalf("failed to create node1: %v", err)
	}

	node2 := &db.Node{
		ID:           "node-2",
		Name:         "Worker Node 2",
		Host:         "tcp://192.168.0.20:2376",
		AdvertisedIP: "192.168.0.20",
		Enabled:      true,
		Status:       db.NodeStatusOnline,
		MaxServers:   5,
		MaxMemoryMB:  16384,
	}
	if err := store.CreateNode(ctx, node2); err != nil {
		t.Fatalf("failed to create node2: %v", err)
	}

	// Create test server on node-1
	server := &db.Server{
		ID:          "server-1",
		Name:        "Survival World",
		ModLoader:   db.ModLoaderPaper,
		MCVersion:   "1.21.1",
		NodeID:      "node-1",
		ContainerID: "container-old-123",
		Status:      db.StatusStopped,
		Port:        25565,
		Memory:      4096,
		DataPath:    "/tmp/test-srv-1",
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Also create ServerConfig
	srvConfig := &db.ServerConfig{
		ID:       "cfg-1",
		ServerID: "server-1",
	}
	if err := store.SaveServerConfig(ctx, srvConfig); err != nil {
		t.Fatalf("failed to create server config: %v", err)
	}

	// Test 1: Migrate to same node should fail
	_, err := svc.MigrateServer(ctx, connect.NewRequest(&v1.MigrateServerRequest{
		Id:           "server-1",
		TargetNodeId: "node-1",
	}))
	if err == nil {
		t.Fatal("expected error when migrating to the same node, got nil")
	}

	// Test 2: Migrate to disabled node should fail
	disabledNode := &db.Node{
		ID:      "node-disabled",
		Name:    "Disabled Node",
		Host:    "tcp://192.168.0.30:2376",
		Enabled: true,
		Status:  db.NodeStatusOffline,
	}
	if err := store.CreateNode(ctx, disabledNode); err != nil {
		t.Fatalf("failed to create disabled node: %v", err)
	}
	disabledNode.Enabled = false
	if err := store.UpdateNode(ctx, disabledNode); err != nil {
		t.Fatalf("failed to update disabled node: %v", err)
	}
	_, err = svc.MigrateServer(ctx, connect.NewRequest(&v1.MigrateServerRequest{
		Id:           "server-1",
		TargetNodeId: "node-disabled",
	}))
	if err == nil {
		t.Fatal("expected error when migrating to disabled node, got nil")
	}

	// Test 3: Successful offline migration from node-1 to node-2
	res, err := svc.MigrateServer(ctx, connect.NewRequest(&v1.MigrateServerRequest{
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

	// Verify database record is updated
	updatedServer, err := store.GetServer(ctx, "server-1")
	if err != nil {
		t.Fatalf("failed to get updated server: %v", err)
	}
	if updatedServer.NodeID != "node-2" {
		t.Errorf("expected server.NodeID in DB to be node-2, got %s", updatedServer.NodeID)
	}
	if updatedServer.ContainerID != "" {
		t.Errorf("expected server.ContainerID to be cleared on offline migration, got %s", updatedServer.ContainerID)
	}
}

func TestServerService_MigrateServer_CapacityChecks(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	log := logger.New()
	svc := NewServerService(store, nil, nil, nil, nil, nil, nil, nil, nil, log, nil, nil, nil)
	ctx := context.Background()

	// Target node with strict capacity limits: max 1 server, max 2048 MB memory
	tightNode := &db.Node{
		ID:          "node-tight",
		Name:        "Tight Node",
		Host:        "tcp://192.168.0.50:2376",
		Enabled:     true,
		Status:      db.NodeStatusOnline,
		MaxServers:  1,
		MaxMemoryMB: 2048,
	}
	if err := store.CreateNode(ctx, tightNode); err != nil {
		t.Fatalf("failed to create tight node: %v", err)
	}

	// Server on default node needing 4096 MB memory
	largeServer := &db.Server{
		ID:        "server-large",
		Name:      "Heavy Modpack",
		ModLoader: db.ModLoaderForge,
		MCVersion: "1.20.1",
		NodeID:    "default",
		Status:    db.StatusStopped,
		Port:      25565,
		Memory:    4096,
		DataPath:  "/tmp/srv-large",
	}
	if err := store.CreateServer(ctx, largeServer); err != nil {
		t.Fatalf("failed to create large server: %v", err)
	}
	_ = store.SaveServerConfig(ctx, &db.ServerConfig{ID: "cfg-lg", ServerID: "server-large"})

	// Migration should fail without force due to memory exceeding limit (4096MB > 2048MB)
	_, err := svc.MigrateServer(ctx, connect.NewRequest(&v1.MigrateServerRequest{
		Id:           "server-large",
		TargetNodeId: "node-tight",
		Force:        boolPtr(false),
	}))
	if err == nil {
		t.Fatal("expected error due to memory capacity exhaustion, got nil")
	}

	// Migration with force=true should succeed despite capacity limits
	res, err := svc.MigrateServer(ctx, connect.NewRequest(&v1.MigrateServerRequest{
		Id:           "server-large",
		TargetNodeId: "node-tight",
		Force:        boolPtr(true),
	}))
	if err != nil {
		t.Fatalf("expected forced migration to succeed, got: %v", err)
	}
	if !res.Msg.Success {
		t.Errorf("expected res.Msg.Success to be true")
	}
}

