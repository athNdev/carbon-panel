package billing

import (
	"errors"
	"fmt"
)

var (
	ErrQuotaExceededNodes        = errors.New("organization node limit exceeded for current plan")
	ErrQuotaExceededManagedNodes = errors.New("organization managed cloud node limit exceeded for current plan")
	ErrQuotaExceededWorkloads    = errors.New("organization workload limit exceeded for current plan")
	ErrQuotaExceededRAM          = errors.New("organization memory quota exceeded for current plan")
	ErrQuotaExceededCPU          = errors.New("organization CPU quota exceeded for current plan")
)

// Usage snapshots an organization's currently committed resources.
type Usage struct {
	NodeCount        int   `json:"node_count"`
	ManagedNodeCount int   `json:"managed_node_count"`
	WorkloadCount    int   `json:"workload_count"`
	AllocatedRAMMB   int64 `json:"allocated_ram_mb"`
	AllocatedCPUm    int64 `json:"allocated_cpu_m"`
}

// Add combines two usage counts.
func (u Usage) Add(delta Usage) Usage {
	return Usage{
		NodeCount:        u.NodeCount + delta.NodeCount,
		ManagedNodeCount: u.ManagedNodeCount + delta.ManagedNodeCount,
		WorkloadCount:    u.WorkloadCount + delta.WorkloadCount,
		AllocatedRAMMB:   u.AllocatedRAMMB + delta.AllocatedRAMMB,
		AllocatedCPUm:    u.AllocatedCPUm + delta.AllocatedCPUm,
	}
}

// QuotaError provides structured quota violation details.
type QuotaError struct {
	Resource string
	Current  int64
	Delta    int64
	Limit    int64
	Err      error
}

func (e *QuotaError) Error() string {
	return fmt.Sprintf("%s: %s (current: %d, requesting: +%d, limit: %d)",
		e.Err.Error(), e.Resource, e.Current, e.Delta, e.Limit)
}

func (e *QuotaError) Unwrap() error {
	return e.Err
}

// QuotaEnforcer evaluates tenant requests against plan constraints.
type QuotaEnforcer struct {
	catalog *Catalog
}

// NewQuotaEnforcer constructs an enforcer with a catalog.
func NewQuotaEnforcer(catalog *Catalog) *QuotaEnforcer {
	if catalog == nil {
		catalog = NewCatalog()
	}
	return &QuotaEnforcer{catalog: catalog}
}

// Catalog returns the plan catalog.
func (q *QuotaEnforcer) Catalog() *Catalog {
	if q == nil || q.catalog == nil {
		return NewCatalog()
	}
	return q.catalog
}

// Check verifies whether adding delta to current is allowed under plan.
func (q *QuotaEnforcer) Check(plan Plan, current, delta Usage) error {
	projected := current.Add(delta)

	// Check total nodes
	if plan.MaxNodes >= 0 && projected.NodeCount > plan.MaxNodes {
		return &QuotaError{
			Resource: "nodes",
			Current:  int64(current.NodeCount),
			Delta:    int64(delta.NodeCount),
			Limit:    int64(plan.MaxNodes),
			Err:      ErrQuotaExceededNodes,
		}
	}

	// Check managed cloud nodes
	if plan.MaxManagedNodes >= 0 && projected.ManagedNodeCount > plan.MaxManagedNodes {
		return &QuotaError{
			Resource: "managed_nodes",
			Current:  int64(current.ManagedNodeCount),
			Delta:    int64(delta.ManagedNodeCount),
			Limit:    int64(plan.MaxManagedNodes),
			Err:      ErrQuotaExceededManagedNodes,
		}
	}

	// Check workloads
	if plan.MaxWorkloads >= 0 && projected.WorkloadCount > plan.MaxWorkloads {
		return &QuotaError{
			Resource: "workloads",
			Current:  int64(current.WorkloadCount),
			Delta:    int64(delta.WorkloadCount),
			Limit:    int64(plan.MaxWorkloads),
			Err:      ErrQuotaExceededWorkloads,
		}
	}

	// Check RAM in MB
	if plan.MaxRAMMB >= 0 && projected.AllocatedRAMMB > plan.MaxRAMMB {
		return &QuotaError{
			Resource: "ram_mb",
			Current:  current.AllocatedRAMMB,
			Delta:    delta.AllocatedRAMMB,
			Limit:    plan.MaxRAMMB,
			Err:      ErrQuotaExceededRAM,
		}
	}

	// Check CPU in millicores
	if plan.MaxCPUMillicores >= 0 && projected.AllocatedCPUm > plan.MaxCPUMillicores {
		return &QuotaError{
			Resource: "cpu_millicores",
			Current:  current.AllocatedCPUm,
			Delta:    delta.AllocatedCPUm,
			Limit:    plan.MaxCPUMillicores,
			Err:      ErrQuotaExceededCPU,
		}
	}

	return nil
}
