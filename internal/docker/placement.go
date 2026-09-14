package docker

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	models "github.com/athNdev/mineserver/internal/db"
)

// PlacementStrategy specifies which algorithm to use when assigning a container to a node.
type PlacementStrategy string

const (
	StrategyLeastMemory  PlacementStrategy = "least_memory"
	StrategyLeastServers PlacementStrategy = "least_servers"
	StrategyRoundRobin   PlacementStrategy = "round_robin"
)

// PlacementStore defines the DB interface required by PlacementEngine
type PlacementStore interface {
	ListNodesWithStats(ctx context.Context) ([]*models.Node, error)
	ListNodes(ctx context.Context) ([]*models.Node, error)
	GetNodeStats(ctx context.Context, nodeID string) (allocatedMemMB int64, serverCount int, runningCount int, err error)
}

// PlacementEngine makes server placement decisions across Docker nodes.
type PlacementEngine struct {
	store PlacementStore
	rrIdx uint64
	mu    sync.Mutex
}

// NewPlacementEngine constructs a new PlacementEngine.
func NewPlacementEngine(store PlacementStore) *PlacementEngine {
	return &PlacementEngine{
		store: store,
	}
}

// SelectNode selects the best eligible Docker node based on strategy, required memory, and limits.
func (e *PlacementEngine) SelectNode(ctx context.Context, strategy PlacementStrategy, reqMemoryMB int64) (*models.Node, error) {
	if e.store == nil {
		return nil, fmt.Errorf("placement store is nil")
	}

	nodes, err := e.store.ListNodesWithStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query nodes: %w", err)
	}

	if len(nodes) == 0 {
		return nil, fmt.Errorf("no nodes registered in the system")
	}

	// Filter healthy, enabled nodes with sufficient capacity
	eligible := FilterEligibleNodes(nodes, reqMemoryMB)
	if len(eligible) == 0 {
		return nil, fmt.Errorf("no eligible nodes available (online, enabled, and within capacity limits for %d MB)", reqMemoryMB)
	}

	switch strategy {
	case StrategyLeastServers:
		return SelectLeastServers(eligible), nil
	case StrategyRoundRobin:
		return e.SelectRoundRobin(eligible), nil
	case StrategyLeastMemory:
		fallthrough
	default:
		return SelectLeastMemory(eligible), nil
	}
}

// FilterEligibleNodes filters nodes that are enabled, online, and within memory & server capacity limits.
func FilterEligibleNodes(nodes []*models.Node, reqMemoryMB int64) []*models.Node {
	var eligible []*models.Node
	for _, n := range nodes {
		if !n.Enabled {
			continue
		}
		if n.Status != models.NodeStatusOnline {
			continue
		}
		// Check server instance capacity
		if n.MaxServers > 0 && n.ServerCount >= n.MaxServers {
			continue
		}
		// Check memory capacity
		if n.MaxMemoryMB > 0 {
			if n.AllocatedMemoryMB >= n.MaxMemoryMB {
				continue
			}
			if reqMemoryMB > 0 && n.AllocatedMemoryMB+reqMemoryMB > n.MaxMemoryMB {
				continue
			}
		}
		eligible = append(eligible, n)
	}
	return eligible
}

// SelectLeastMemory chooses the node with the minimum allocated memory.
func SelectLeastMemory(eligible []*models.Node) *models.Node {
	if len(eligible) == 0 {
		return nil
	}
	best := eligible[0]
	for _, n := range eligible[1:] {
		if n.AllocatedMemoryMB < best.AllocatedMemoryMB {
			best = n
		} else if n.AllocatedMemoryMB == best.AllocatedMemoryMB {
			// Tie break: least total servers, then lowest ID
			if n.ServerCount < best.ServerCount {
				best = n
			} else if n.ServerCount == best.ServerCount && n.ID < best.ID {
				best = n
			}
		}
	}
	return best
}

// SelectLeastServers chooses the node with the fewest running instances.
func SelectLeastServers(eligible []*models.Node) *models.Node {
	if len(eligible) == 0 {
		return nil
	}
	best := eligible[0]
	for _, n := range eligible[1:] {
		if n.RunningCount < best.RunningCount {
			best = n
		} else if n.RunningCount == best.RunningCount {
			// Tie break: least total servers, then least memory, then lowest ID
			if n.ServerCount < best.ServerCount {
				best = n
			} else if n.ServerCount == best.ServerCount && n.AllocatedMemoryMB < best.AllocatedMemoryMB {
				best = n
			} else if n.ServerCount == best.ServerCount && n.AllocatedMemoryMB == best.AllocatedMemoryMB && n.ID < best.ID {
				best = n
			}
		}
	}
	return best
}

// SelectRoundRobin distributes placements in round-robin order across eligible nodes.
func (e *PlacementEngine) SelectRoundRobin(eligible []*models.Node) *models.Node {
	if len(eligible) == 0 {
		return nil
	}
	// Sort deterministically by ID
	nodes := make([]*models.Node, len(eligible))
	copy(nodes, eligible)
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})

	idx := atomic.AddUint64(&e.rrIdx, 1) - 1
	return nodes[idx%uint64(len(nodes))]
}
