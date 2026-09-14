package services

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func setupTestStore(t *testing.T) *db.Store {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Path:           ":memory:",
			AutoMigrate:    true,
			MaxConnections: 1,
		},
	}
	store, err := db.NewSQLiteStore(cfg)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}
	return store
}

func strPtr(s string) *string { return &s }
func int64Ptr(i int64) *int64 { return &i }
func int32Ptr(i int32) *int32 { return &i }
func boolPtr(b bool) *bool    { return &b }

func TestNodeService_CreateAndGetNode(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	log := logger.New()
	svc := NewNodeService(store, nil, log)
	ctx := context.Background()

	// 1. Test validation on empty fields
	_, err := svc.CreateNode(ctx, connect.NewRequest(&v1.CreateNodeRequest{
		Name: "",
		Host: "tcp://192.168.1.50:2376",
	}))
	if err == nil {
		t.Fatal("expected error on empty name, got nil")
	}

	_, err = svc.CreateNode(ctx, connect.NewRequest(&v1.CreateNodeRequest{
		Name: "node-1",
		Host: "",
	}))
	if err == nil {
		t.Fatal("expected error on empty host, got nil")
	}

	// 2. Successful creation
	createRes, err := svc.CreateNode(ctx, connect.NewRequest(&v1.CreateNodeRequest{
		Name:         "worker-1",
		Host:         "tcp://192.168.1.50:2376",
		AdvertisedIp: "192.168.1.50",
		MaxMemoryMb:  16384,
		MaxServers:   10,
		Enabled:      true,
	}))
	if err != nil {
		t.Fatalf("CreateNode failed: %v", err)
	}
	if createRes.Msg.Node == nil {
		t.Fatal("expected Node in response, got nil")
	}
	created := createRes.Msg.Node
	if created.Id == "" {
		t.Error("expected generated ID for node, got empty")
	}
	if created.Name != "worker-1" {
		t.Errorf("expected name worker-1, got %s", created.Name)
	}
	if created.AdvertisedIp != "192.168.1.50" {
		t.Errorf("expected advertised IP 192.168.1.50, got %s", created.AdvertisedIp)
	}
	if created.MaxMemoryMb != 16384 {
		t.Errorf("expected max memory 16384, got %d", created.MaxMemoryMb)
	}

	// 3. GetNode existing
	getRes, err := svc.GetNode(ctx, connect.NewRequest(&v1.GetNodeRequest{
		Id: created.Id,
	}))
	if err != nil {
		t.Fatalf("GetNode failed: %v", err)
	}
	if getRes.Msg.Node.Id != created.Id {
		t.Errorf("expected ID %s, got %s", created.Id, getRes.Msg.Node.Id)
	}

	// 4. GetNode non-existing
	_, err = svc.GetNode(ctx, connect.NewRequest(&v1.GetNodeRequest{
		Id: "non-existent-id",
	}))
	if err == nil {
		t.Fatal("expected error for non-existent node ID, got nil")
	}
}

func TestNodeService_ListNodes(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	log := logger.New()
	svc := NewNodeService(store, nil, log)
	ctx := context.Background()

	// Initial list contains the seeded local/default node
	listRes, err := svc.ListNodes(ctx, connect.NewRequest(&v1.ListNodesRequest{}))
	if err != nil {
		t.Fatalf("ListNodes failed: %v", err)
	}
	initialCount := len(listRes.Msg.Nodes)

	// Create two nodes
	_, err = svc.CreateNode(ctx, connect.NewRequest(&v1.CreateNodeRequest{
		Name: "node-a",
		Host: "tcp://10.0.0.1:2376",
	}))
	if err != nil {
		t.Fatalf("CreateNode node-a failed: %v", err)
	}

	_, err = svc.CreateNode(ctx, connect.NewRequest(&v1.CreateNodeRequest{
		Name: "node-b",
		Host: "tcp://10.0.0.2:2376",
	}))
	if err != nil {
		t.Fatalf("CreateNode node-b failed: %v", err)
	}

	// List again
	listRes, err = svc.ListNodes(ctx, connect.NewRequest(&v1.ListNodesRequest{}))
	if err != nil {
		t.Fatalf("ListNodes failed: %v", err)
	}
	if len(listRes.Msg.Nodes) != initialCount+2 {
		t.Fatalf("expected %d nodes, got %d", initialCount+2, len(listRes.Msg.Nodes))
	}
}

func TestNodeService_UpdateNode(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	log := logger.New()
	svc := NewNodeService(store, nil, log)
	ctx := context.Background()

	createRes, err := svc.CreateNode(ctx, connect.NewRequest(&v1.CreateNodeRequest{
		Name:         "original-node",
		Host:         "tcp://192.168.1.10:2376",
		AdvertisedIp: "192.168.1.10",
		MaxMemoryMb:  8192,
		MaxServers:   5,
		Enabled:      true,
	}))
	if err != nil {
		t.Fatalf("CreateNode failed: %v", err)
	}
	nodeID := createRes.Msg.Node.Id

	// Update node
	updateRes, err := svc.UpdateNode(ctx, connect.NewRequest(&v1.UpdateNodeRequest{
		Id:           nodeID,
		Name:         strPtr("updated-node"),
		Host:         strPtr("tcp://192.168.1.20:2376"),
		AdvertisedIp: strPtr("192.168.1.20"),
		MaxMemoryMb:  int64Ptr(32768),
		MaxServers:   int32Ptr(20),
		Enabled:      boolPtr(false),
	}))
	if err != nil {
		t.Fatalf("UpdateNode failed: %v", err)
	}
	if updateRes.Msg.Node.Name != "updated-node" {
		t.Errorf("expected updated name, got %s", updateRes.Msg.Node.Name)
	}
	if updateRes.Msg.Node.AdvertisedIp != "192.168.1.20" {
		t.Errorf("expected updated advertised IP, got %s", updateRes.Msg.Node.AdvertisedIp)
	}
	if updateRes.Msg.Node.Enabled != false {
		t.Errorf("expected enabled=false, got true")
	}

	// Update non-existent node
	_, err = svc.UpdateNode(ctx, connect.NewRequest(&v1.UpdateNodeRequest{
		Id:   "does-not-exist",
		Name: strPtr("test"),
		Host: strPtr("tcp://1.1.1.1:2376"),
	}))
	if err == nil {
		t.Fatal("expected error updating non-existent node, got nil")
	}
}

func TestNodeService_DeleteNode(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	log := logger.New()
	svc := NewNodeService(store, nil, log)
	ctx := context.Background()

	createRes, err := svc.CreateNode(ctx, connect.NewRequest(&v1.CreateNodeRequest{
		Name: "to-delete",
		Host: "tcp://10.0.0.99:2376",
	}))
	if err != nil {
		t.Fatalf("CreateNode failed: %v", err)
	}
	nodeID := createRes.Msg.Node.Id

	// Delete existing
	delRes, err := svc.DeleteNode(ctx, connect.NewRequest(&v1.DeleteNodeRequest{
		Id: nodeID,
	}))
	if err != nil {
		t.Fatalf("DeleteNode failed: %v", err)
	}
	if !delRes.Msg.Success {
		t.Errorf("expected success true, got false")
	}

	// Verify it is gone
	_, err = svc.GetNode(ctx, connect.NewRequest(&v1.GetNodeRequest{
		Id: nodeID,
	}))
	if err == nil {
		t.Fatal("expected error getting deleted node, got nil")
	}

	// Delete non-existent node
	_, err = svc.DeleteNode(ctx, connect.NewRequest(&v1.DeleteNodeRequest{
		Id: "already-deleted",
	}))
	if err == nil {
		t.Fatal("expected error deleting non-existent node, got nil")
	}
}

func TestNodeService_PingNode(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	log := logger.New()
	svc := NewNodeService(store, nil, log)
	ctx := context.Background()

	createRes, err := svc.CreateNode(ctx, connect.NewRequest(&v1.CreateNodeRequest{
		Name: "ping-target",
		Host: "tcp://10.0.0.5:2376",
	}))
	if err != nil {
		t.Fatalf("CreateNode failed: %v", err)
	}
	nodeID := createRes.Msg.Node.Id

	// Ping existing node with nil pool returns OFFLINE
	pingRes, err := svc.PingNode(ctx, connect.NewRequest(&v1.PingNodeRequest{
		Id: nodeID,
	}))
	if err != nil {
		t.Fatalf("PingNode failed: %v", err)
	}
	if pingRes.Msg.Status == v1.NodeStatus_NODE_STATUS_ONLINE {
		t.Errorf("expected status offline with nil pool, got online")
	}

	// Ping non-existent node returns not found
	_, err = svc.PingNode(ctx, connect.NewRequest(&v1.PingNodeRequest{
		Id: "invalid-id",
	}))
	if err == nil {
		t.Fatal("expected error pinging non-existent node, got nil")
	}
}
