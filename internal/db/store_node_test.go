package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/athNdev/carbon-panel/internal/config"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Path:        dbPath,
			AutoMigrate: true,
		},
		Docker: config.DockerConfig{
			Host: "unix:///var/run/docker.sock",
		},
	}

	store, err := NewSQLiteStore(cfg)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}

	return store
}

func TestStore_NodeCRUD(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	// 1. Check default node seeded
	defNode, err := store.GetNode(ctx, "default")
	if err != nil {
		t.Fatalf("failed to get default node: %v", err)
	}
	if defNode.ID != "default" || !defNode.IsLocal {
		t.Fatalf("unexpected default node: %+v", defNode)
	}

	// 2. Cannot delete default node
	if err := store.DeleteNode(ctx, "default"); err == nil {
		t.Fatalf("expected error deleting default node, got nil")
	}

	// 3. Create a custom remote node
	newNode := &Node{
		ID:           "node-remote-1",
		Name:         "Worker 1",
		Host:         "tcp://192.168.1.100:2376",
		AdvertisedIP: "192.168.1.100",
		MaxMemoryMB:  16384,
		MaxServers:   10,
		Enabled:      true,
		Status:       NodeStatusOnline,
		IsLocal:      false,
	}
	if err := store.CreateNode(ctx, newNode); err != nil {
		t.Fatalf("failed to create node: %v", err)
	}

	// 4. Get the node
	fetched, err := store.GetNode(ctx, "node-remote-1")
	if err != nil {
		t.Fatalf("failed to get node: %v", err)
	}
	if fetched.Name != "Worker 1" || fetched.MaxMemoryMB != 16384 {
		t.Fatalf("fetched node mismatch: %+v", fetched)
	}

	// 5. Update the node
	fetched.MaxServers = 20
	fetched.Status = NodeStatusOffline
	if err := store.UpdateNode(ctx, fetched); err != nil {
		t.Fatalf("failed to update node: %v", err)
	}

	updated, err := store.GetNode(ctx, "node-remote-1")
	if err != nil {
		t.Fatalf("failed to get updated node: %v", err)
	}
	if updated.MaxServers != 20 || updated.Status != NodeStatusOffline {
		t.Fatalf("updated node mismatch: %+v", updated)
	}

	// 6. List nodes
	nodes, err := store.ListNodes(ctx)
	if err != nil {
		t.Fatalf("failed to list nodes: %v", err)
	}
	if len(nodes) < 2 {
		t.Fatalf("expected at least 2 nodes, got %d", len(nodes))
	}

	// 7. Attach a server to this node and check DeleteNode prevention
	server := &Server{
		ID:        "server-node-1",
		Name:      "Test Server",
		ModLoader: ModLoaderVanilla,
		MCVersion: "1.20.4",
		NodeID:    "node-remote-1",
		Status:    StatusRunning,
		Memory:    4096,
		DataPath:  "/tmp/server-data",
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	if err := store.DeleteNode(ctx, "node-remote-1"); err == nil {
		t.Fatalf("expected error deleting node with attached servers, got nil")
	}

	// Check Node Stats
	mem, count, running, err := store.GetNodeStats(ctx, "node-remote-1")
	if err != nil {
		t.Fatalf("GetNodeStats failed: %v", err)
	}
	if mem != 4096 || count != 1 || running != 1 {
		t.Fatalf("unexpected node stats: mem=%d, count=%d, running=%d", mem, count, running)
	}

	// Check ListNodesWithStats
	nodesWithStats, err := store.ListNodesWithStats(ctx)
	if err != nil {
		t.Fatalf("ListNodesWithStats failed: %v", err)
	}
	var foundRemote bool
	for _, n := range nodesWithStats {
		if n.ID == "node-remote-1" {
			foundRemote = true
			if n.AllocatedMemoryMB != 4096 || n.ServerCount != 1 || n.RunningCount != 1 {
				t.Fatalf("node-remote-1 stats mismatch in ListNodesWithStats: %+v", n)
			}
		}
	}
	if !foundRemote {
		t.Fatalf("node-remote-1 not found in ListNodesWithStats")
	}

	// Delete the server, then deleting node should succeed
	if err := store.DeleteServer(ctx, "server-node-1"); err != nil {
		t.Fatalf("failed to delete server: %v", err)
	}

	if err := store.DeleteNode(ctx, "node-remote-1"); err != nil {
		t.Fatalf("failed to delete node after removing server: %v", err)
	}

	// Verify it's gone
	_, err = store.GetNode(ctx, "node-remote-1")
	if err == nil {
		t.Fatalf("expected error getting deleted node, got nil")
	}
}
