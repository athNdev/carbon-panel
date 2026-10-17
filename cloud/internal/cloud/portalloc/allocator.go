package portalloc

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"gorm.io/gorm"
)

const (
	// DefaultMinPort is the standard Minecraft listen port and default start of assignable pool.
	DefaultMinPort = 25565
	// DefaultMaxPort is the default upper bound of the assignable pool.
	DefaultMaxPort = 25700
)

var (
	// ErrNoPortsAvailable is returned when all ports in the pool are occupied.
	ErrNoPortsAvailable = errors.New("no free ports available in node assignable range")
	// ErrPortConflict is returned when an explicitly requested port is already held by another active workload.
	ErrPortConflict = errors.New("requested port is already allocated to another active workload on this node")
	// ErrInvalidPort is returned when a port is not a valid TCP port.
	ErrInvalidPort = errors.New("port out of valid TCP port range (1-65535)")
)

// PortAllocation models a currently active port binding on a node.
type PortAllocation struct {
	Port         int
	WorkloadID   string
	WorkloadName string
	Status       string
	Hostname     string
}

// Allocator manages dynamic port leasing and collision avoidance per node.
type Allocator struct {
	minPort int
	maxPort int
}

// NewAllocator creates a port allocator with specified or default port bounds.
func NewAllocator(minPort, maxPort int) *Allocator {
	if minPort <= 0 {
		minPort = DefaultMinPort
	}
	if maxPort <= minPort {
		maxPort = DefaultMaxPort
	}
	return &Allocator{
		minPort: minPort,
		maxPort: maxPort,
	}
}

// PortRange returns the configured [min, max] port bounds.
func (a *Allocator) PortRange() (int, int) {
	return a.minPort, a.maxPort
}

// GetAllocations queries the DB for all non-terminated workloads with an allocated host port on a node.
func (a *Allocator) GetAllocations(ctx context.Context, tx *gorm.DB, nodeID string) ([]PortAllocation, error) {
	var workloads []db.Workload
	err := tx.Session(&gorm.Session{}).WithContext(ctx).
		Table("workloads").
		Where("node_id = ? AND status NOT IN ('terminated', 'deleted') AND host_port > 0", nodeID).
		Find(&workloads).Error
	if err != nil {
		return nil, fmt.Errorf("query node workload ports: %w", err)
	}

	allocs := make([]PortAllocation, 0, len(workloads))
	for _, w := range workloads {
		allocs = append(allocs, PortAllocation{
			Port:         w.HostPort,
			WorkloadID:   w.ID,
			WorkloadName: w.Name,
			Status:       w.Status,
			Hostname:     w.Hostname,
		})
	}
	sort.Slice(allocs, func(i, j int) bool {
		return allocs[i].Port < allocs[j].Port
	})
	return allocs, nil
}

// Allocate claims a port on nodeID.
// If requestedPort > 0, it validates that no other active workload holds it.
// If requestedPort == 0, it finds the first available port in [minPort, maxPort].
// excludeWorkloadID allows an existing workload to keep its port or update without self-conflicting.
func (a *Allocator) Allocate(ctx context.Context, tx *gorm.DB, nodeID string, requestedPort int, excludeWorkloadID string) (int, error) {
	allocs, err := a.GetAllocations(ctx, tx, nodeID)
	if err != nil {
		return 0, err
	}

	occupied := make(map[int]string, len(allocs))
	for _, al := range allocs {
		if excludeWorkloadID != "" && al.WorkloadID == excludeWorkloadID {
			continue
		}
		occupied[al.Port] = al.WorkloadID
	}

	if requestedPort > 0 {
		if requestedPort < 1 || requestedPort > 65535 {
			return 0, ErrInvalidPort
		}
		if ownerID, inUse := occupied[requestedPort]; inUse {
			return 0, fmt.Errorf("%w: port %d held by workload %s", ErrPortConflict, requestedPort, ownerID)
		}
		return requestedPort, nil
	}

	for p := a.minPort; p <= a.maxPort; p++ {
		if _, inUse := occupied[p]; !inUse {
			return p, nil
		}
	}

	return 0, ErrNoPortsAvailable
}

// FormatSRVRecord generates a DNS SRV record string according to RFC 2782.
// Example: "_minecraft._tcp.play.example.com. 3600 IN SRV 0 5 25566 node.domain.com."
func FormatSRVRecord(hostname, targetHost string, port int) string {
	if hostname == "" || targetHost == "" || port <= 0 {
		return ""
	}
	return fmt.Sprintf("_minecraft._tcp.%s. 3600 IN SRV 0 5 %d %s.", hostname, port, targetHost)
}
