package node

import (
	"context"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
)

// Capacity is admission accounting for workload placement on a node.
//
// Allocations are the org's workloads pinned to the node: each workload's
// spec may carry a resource request under the NodeCapacity JSON names
// ("vcpu", "ram_mb", "disk_gb", with "ramMB"/"diskGB" accepted as well).
// Workloads without a request consume nothing. BYO and managed nodes
// account identically; draining nodes refuse all placement.
type Capacity struct {
	deps Deps
}

// NewCapacity returns admission accounting over deps.
func NewCapacity(deps Deps) *Capacity {
	return &Capacity{deps: deps}
}

// RequestOf extracts the resource request from a workload spec.
func RequestOf(w db.Workload) db.NodeCapacity {
	var out db.NodeCapacity
	for k, v := range w.SpecMap() {
		n, ok := asInt(v)
		if !ok {
			continue
		}
		switch k {
		case "vcpu":
			out.VCPU = n
		case "ram_mb", "ramMB", "ram":
			out.RAMMB = n
		case "disk_gb", "diskGB", "disk":
			out.DiskGB = n
		}
	}
	if out.VCPU < 0 {
		out.VCPU = 0
	}
	if out.RAMMB < 0 {
		out.RAMMB = 0
	}
	if out.DiskGB < 0 {
		out.DiskGB = 0
	}
	return out
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}

// Usage sums allocations on the node and returns (used, total).
func (c *Capacity) Usage(ctx context.Context, nodeID string) (used, total db.NodeCapacity, err error) {
	q, err := c.deps.Store.Org(ctx)
	if err != nil {
		return db.NodeCapacity{}, db.NodeCapacity{}, err
	}
	var n db.Node
	if err := q.Where("id = ?", nodeID).First(&n).Error; err != nil {
		return db.NodeCapacity{}, db.NodeCapacity{}, ErrNodeNotFound
	}
	total = n.CapacityDecoded()
	wq, err := c.deps.Store.Org(ctx)
	if err != nil {
		return db.NodeCapacity{}, db.NodeCapacity{}, err
	}
	var workloads []db.Workload
	if err := wq.Where("node_id = ?", nodeID).Find(&workloads).Error; err != nil {
		return db.NodeCapacity{}, db.NodeCapacity{}, err
	}
	for _, w := range workloads {
		r := RequestOf(w)
		used.VCPU += r.VCPU
		used.RAMMB += r.RAMMB
		used.DiskGB += r.DiskGB
	}
	return used, total, nil
}

// Remaining returns total minus used, floored at zero per dimension.
func (c *Capacity) Remaining(ctx context.Context, nodeID string) (db.NodeCapacity, error) {
	used, total, err := c.Usage(ctx, nodeID)
	if err != nil {
		return db.NodeCapacity{}, err
	}
	out := db.NodeCapacity{
		VCPU:   total.VCPU - used.VCPU,
		RAMMB:  total.RAMMB - used.RAMMB,
		DiskGB: total.DiskGB - used.DiskGB,
	}
	if out.VCPU < 0 {
		out.VCPU = 0
	}
	if out.RAMMB < 0 {
		out.RAMMB = 0
	}
	if out.DiskGB < 0 {
		out.DiskGB = 0
	}
	return out, nil
}

// Check admits a placement request: the node must exist, must not be
// draining, and the remaining capacity must cover req in every dimension.
// A request exactly equal to the remainder is accepted.
func (c *Capacity) Check(ctx context.Context, nodeID string, req db.NodeCapacity) error {
	q, err := c.deps.Store.Org(ctx)
	if err != nil {
		return err
	}
	var n db.Node
	if err := q.Where("id = ?", nodeID).First(&n).Error; err != nil {
		return ErrNodeNotFound
	}
	if n.Draining {
		return ErrNodeDraining
	}
	rem, err := c.Remaining(ctx, nodeID)
	if err != nil {
		return err
	}
	if req.VCPU > rem.VCPU || req.RAMMB > rem.RAMMB || req.DiskGB > rem.DiskGB {
		return ErrCapacityExceeded
	}
	return nil
}
