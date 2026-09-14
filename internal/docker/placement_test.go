package docker

import (
	"context"
	"fmt"
	"testing"

	models "github.com/athNdev/carbon-panel/internal/db"
)

// mockPlacementStore implements PlacementStore for unit tests
type mockPlacementStore struct {
	nodes []*models.Node
}

func (m *mockPlacementStore) ListNodesWithStats(ctx context.Context) ([]*models.Node, error) {
	return m.nodes, nil
}

func (m *mockPlacementStore) ListNodes(ctx context.Context) ([]*models.Node, error) {
	return m.nodes, nil
}

func (m *mockPlacementStore) GetNodeStats(ctx context.Context, nodeID string) (int64, int, int, error) {
	for _, n := range m.nodes {
		if n.ID == nodeID {
			return n.AllocatedMemoryMB, n.ServerCount, n.RunningCount, nil
		}
	}
	return 0, 0, 0, fmt.Errorf("node not found")
}

func TestFilterEligibleNodes(t *testing.T) {
	nodes := []*models.Node{
		{
			ID:                "node-healthy",
			Enabled:           true,
			Status:            models.NodeStatusOnline,
			MaxMemoryMB:       8192,
			AllocatedMemoryMB: 2048,
			MaxServers:        10,
			ServerCount:       2,
		},
		{
			ID:                "node-offline",
			Enabled:           true,
			Status:            models.NodeStatusOffline,
			MaxMemoryMB:       8192,
			AllocatedMemoryMB: 1024,
			MaxServers:        10,
			ServerCount:       1,
		},
		{
			ID:                "node-disabled",
			Enabled:           false,
			Status:            models.NodeStatusOnline,
			MaxMemoryMB:       8192,
			AllocatedMemoryMB: 1024,
			MaxServers:        10,
			ServerCount:       1,
		},
		{
			ID:                "node-servers-full",
			Enabled:           true,
			Status:            models.NodeStatusOnline,
			MaxMemoryMB:       8192,
			AllocatedMemoryMB: 2048,
			MaxServers:        2,
			ServerCount:       2,
		},
		{
			ID:                "node-mem-full",
			Enabled:           true,
			Status:            models.NodeStatusOnline,
			MaxMemoryMB:       4096,
			AllocatedMemoryMB: 4096,
			MaxServers:        10,
			ServerCount:       1,
		},
		{
			ID:                "node-unlimited",
			Enabled:           true,
			Status:            models.NodeStatusOnline,
			MaxMemoryMB:       0,
			AllocatedMemoryMB: 16384,
			MaxServers:        0,
			ServerCount:       20,
		},
	}

	// Case 1: reqMemoryMB = 2048
	eligible := FilterEligibleNodes(nodes, 2048)
	if len(eligible) != 2 {
		t.Fatalf("expected 2 eligible nodes, got %d", len(eligible))
	}
	if eligible[0].ID != "node-healthy" || eligible[1].ID != "node-unlimited" {
		t.Fatalf("unexpected eligible nodes: %v, %v", eligible[0].ID, eligible[1].ID)
	}

	// Case 2: reqMemoryMB = 7000 (exceeds node-healthy's remaining 6144 MB)
	eligibleHighMem := FilterEligibleNodes(nodes, 7000)
	if len(eligibleHighMem) != 1 {
		t.Fatalf("expected 1 eligible node (unlimited), got %d", len(eligibleHighMem))
	}
	if eligibleHighMem[0].ID != "node-unlimited" {
		t.Fatalf("expected node-unlimited, got %s", eligibleHighMem[0].ID)
	}
}

func TestSelectLeastMemory(t *testing.T) {
	nodes := []*models.Node{
		{ID: "node-1", AllocatedMemoryMB: 4096, ServerCount: 2},
		{ID: "node-2", AllocatedMemoryMB: 1024, ServerCount: 1},
		{ID: "node-3", AllocatedMemoryMB: 2048, ServerCount: 3},
	}

	selected := SelectLeastMemory(nodes)
	if selected == nil || selected.ID != "node-2" {
		t.Fatalf("expected node-2 with least memory, got %v", selected)
	}

	// Test tie-breaking by server count
	tieNodes := []*models.Node{
		{ID: "node-a", AllocatedMemoryMB: 2048, ServerCount: 3},
		{ID: "node-b", AllocatedMemoryMB: 2048, ServerCount: 1},
	}
	selectedTie := SelectLeastMemory(tieNodes)
	if selectedTie == nil || selectedTie.ID != "node-b" {
		t.Fatalf("expected node-b (least servers in tie), got %v", selectedTie)
	}

	// Test empty
	if SelectLeastMemory(nil) != nil {
		t.Fatalf("expected nil for empty slice")
	}
}

func TestSelectLeastServers(t *testing.T) {
	nodes := []*models.Node{
		{ID: "node-1", RunningCount: 4, ServerCount: 5, AllocatedMemoryMB: 8192},
		{ID: "node-2", RunningCount: 1, ServerCount: 2, AllocatedMemoryMB: 4096},
		{ID: "node-3", RunningCount: 2, ServerCount: 3, AllocatedMemoryMB: 2048},
	}

	selected := SelectLeastServers(nodes)
	if selected == nil || selected.ID != "node-2" {
		t.Fatalf("expected node-2 with least running servers, got %v", selected)
	}

	// Tie break: least total servers, then memory
	tieNodes := []*models.Node{
		{ID: "node-a", RunningCount: 2, ServerCount: 4, AllocatedMemoryMB: 8192},
		{ID: "node-b", RunningCount: 2, ServerCount: 2, AllocatedMemoryMB: 4096},
	}
	selectedTie := SelectLeastServers(tieNodes)
	if selectedTie == nil || selectedTie.ID != "node-b" {
		t.Fatalf("expected node-b in tie break, got %v", selectedTie)
	}

	if SelectLeastServers(nil) != nil {
		t.Fatalf("expected nil for empty slice")
	}
}

func TestSelectRoundRobin(t *testing.T) {
	engine := NewPlacementEngine(nil)

	nodes := []*models.Node{
		{ID: "node-c"},
		{ID: "node-a"},
		{ID: "node-b"},
	}

	// RoundRobin sorts deterministically by ID: node-a, node-b, node-c
	expected := []string{"node-a", "node-b", "node-c", "node-a", "node-b", "node-c", "node-a"}
	for i, exp := range expected {
		sel := engine.SelectRoundRobin(nodes)
		if sel == nil || sel.ID != exp {
			t.Fatalf("step %d: expected %s, got %v", i, exp, sel)
		}
	}

	if engine.SelectRoundRobin(nil) != nil {
		t.Fatalf("expected nil for empty slice")
	}
}

func TestPlacementEngine_SelectNode(t *testing.T) {
	ctx := context.Background()

	store := &mockPlacementStore{
		nodes: []*models.Node{
			{
				ID:                "node-1",
				Enabled:           true,
				Status:            models.NodeStatusOnline,
				MaxMemoryMB:       16384,
				AllocatedMemoryMB: 2048,
				ServerCount:       2,
				RunningCount:      2,
			},
			{
				ID:                "node-2",
				Enabled:           true,
				Status:            models.NodeStatusOnline,
				MaxMemoryMB:       8192,
				AllocatedMemoryMB: 4096,
				ServerCount:       1,
				RunningCount:      1,
			},
			{
				ID:                "node-offline",
				Enabled:           true,
				Status:            models.NodeStatusOffline,
				MaxMemoryMB:       16384,
				AllocatedMemoryMB: 0,
				ServerCount:       0,
				RunningCount:      0,
			},
		},
	}

	engine := NewPlacementEngine(store)

	// Test StrategyLeastMemory -> should pick node-1 (2048 MB vs 4096 MB)
	nodeMem, err := engine.SelectNode(ctx, StrategyLeastMemory, 1024)
	if err != nil {
		t.Fatalf("SelectNode(LeastMemory) failed: %v", err)
	}
	if nodeMem.ID != "node-1" {
		t.Fatalf("expected node-1, got %s", nodeMem.ID)
	}

	// Test StrategyLeastServers -> should pick node-2 (1 running vs 2 running)
	nodeSrv, err := engine.SelectNode(ctx, StrategyLeastServers, 1024)
	if err != nil {
		t.Fatalf("SelectNode(LeastServers) failed: %v", err)
	}
	if nodeSrv.ID != "node-2" {
		t.Fatalf("expected node-2, got %s", nodeSrv.ID)
	}

	// Test StrategyRoundRobin
	nodeRR, err := engine.SelectNode(ctx, StrategyRoundRobin, 1024)
	if err != nil {
		t.Fatalf("SelectNode(RoundRobin) failed: %v", err)
	}
	if nodeRR.ID != "node-1" && nodeRR.ID != "node-2" {
		t.Fatalf("unexpected node for round robin: %s", nodeRR.ID)
	}

	// Test memory limit exceeded on all nodes
	_, err = engine.SelectNode(ctx, StrategyLeastMemory, 30000)
	if err == nil {
		t.Fatalf("expected error for excessive memory requirement, got nil")
	}

	// Test empty store
	emptyEngine := NewPlacementEngine(&mockPlacementStore{nodes: nil})
	_, err = emptyEngine.SelectNode(ctx, StrategyLeastMemory, 1024)
	if err == nil {
		t.Fatalf("expected error for empty store, got nil")
	}
}
