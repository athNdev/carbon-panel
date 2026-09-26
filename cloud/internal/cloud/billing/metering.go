package billing

import (
	"sync"
	"time"
)

// ResourceKind indicates what is being metered.
type ResourceKind string

const (
	ResourceKindNode     ResourceKind = "node"
	ResourceKindWorkload ResourceKind = "workload"
)

// MeterRecord represents a discrete period of resource usage.
type MeterRecord struct {
	OrgID        string       `json:"org_id"`
	ResourceKind ResourceKind `json:"resource_kind"`
	ResourceID   string       `json:"resource_id"`
	StartedAt    time.Time    `json:"started_at"`
	EndedAt      time.Time    `json:"ended_at"`
	DurationSec  float64      `json:"duration_sec"`
	RAMMB        int64        `json:"ram_mb"`
	CPUm         int64        `json:"cpu_m"`
	Provider     string       `json:"provider,omitempty"`
}

// OrgUsageSummary aggregates billable consumption for an org in a billing cycle.
type OrgUsageSummary struct {
	OrgID              string    `json:"org_id"`
	PeriodStart        time.Time `json:"period_start"`
	PeriodEnd          time.Time `json:"period_end"`
	TotalNodeSeconds   float64   `json:"total_node_seconds"`
	TotalWorkloadSecs  float64   `json:"total_workload_seconds"`
	TotalRAMMBSeconds  float64   `json:"total_ram_mb_seconds"`
	TotalCPUmSeconds   float64   `json:"total_cpu_m_seconds"`
	ActiveManagedNodes int       `json:"active_managed_nodes"`
}

// ActiveItem tracks currently running nodes or workloads.
type ActiveItem struct {
	OrgID     string
	Kind      ResourceKind
	ID        string
	StartedAt time.Time
	RAMMB     int64
	CPUm      int64
	Provider  string
}

// Meter manages live resource tracking and historical usage rollups.
type Meter struct {
	mu      sync.RWMutex
	active  map[string]ActiveItem // keyed by "kind:id"
	records []MeterRecord
}

// NewMeter creates a new metering engine.
func NewMeter() *Meter {
	return &Meter{
		active: make(map[string]ActiveItem),
	}
}

func itemKey(kind ResourceKind, id string) string {
	return string(kind) + ":" + id
}

// StartResource starts tracking a resource's usage.
func (m *Meter) StartResource(orgID string, kind ResourceKind, id string, ramMB, cpuM int64, provider string, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if at.IsZero() {
		at = time.Now()
	}
	m.active[itemKey(kind, id)] = ActiveItem{
		OrgID:     orgID,
		Kind:      kind,
		ID:        id,
		StartedAt: at,
		RAMMB:     ramMB,
		CPUm:      cpuM,
		Provider:  provider,
	}
}

// StopResource ends tracking for a resource and flushes a record.
func (m *Meter) StopResource(kind ResourceKind, id string, at time.Time) *MeterRecord {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := itemKey(kind, id)
	item, ok := m.active[key]
	if !ok {
		return nil
	}
	delete(m.active, key)

	if at.IsZero() {
		at = time.Now()
	}
	duration := at.Sub(item.StartedAt).Seconds()
	if duration < 0 {
		duration = 0
	}

	record := MeterRecord{
		OrgID:        item.OrgID,
		ResourceKind: item.Kind,
		ResourceID:   item.ID,
		StartedAt:    item.StartedAt,
		EndedAt:      at,
		DurationSec:  duration,
		RAMMB:        item.RAMMB,
		CPUm:         item.CPUm,
		Provider:     item.Provider,
	}
	m.records = append(m.records, record)
	return &record
}

// Summary calculates the aggregated consumption for an organization across a time window.
func (m *Meter) Summary(orgID string, start, end time.Time) OrgUsageSummary {
	m.mu.RLock()
	defer m.mu.RUnlock()

	summary := OrgUsageSummary{
		OrgID:       orgID,
		PeriodStart: start,
		PeriodEnd:   end,
	}

	// 1. Process closed records
	for _, r := range m.records {
		if r.OrgID != orgID {
			continue
		}
		if r.EndedAt.Before(start) || r.StartedAt.After(end) {
			continue
		}
		// Clamp overlap with query window
		overlapStart := r.StartedAt
		if overlapStart.Before(start) {
			overlapStart = start
		}
		overlapEnd := r.EndedAt
		if overlapEnd.After(end) {
			overlapEnd = end
		}
		secs := overlapEnd.Sub(overlapStart).Seconds()
		if secs < 0 {
			secs = 0
		}

		if r.ResourceKind == ResourceKindNode {
			summary.TotalNodeSeconds += secs
		} else {
			summary.TotalWorkloadSecs += secs
		}
		summary.TotalRAMMBSeconds += secs * float64(r.RAMMB)
		summary.TotalCPUmSeconds += secs * float64(r.CPUm)
	}

	// 2. Process currently active resources
	now := time.Now()
	calcEnd := end
	if calcEnd.After(now) {
		calcEnd = now
	}

	for _, item := range m.active {
		if item.OrgID != orgID {
			continue
		}
		if item.StartedAt.After(calcEnd) {
			continue
		}
		overlapStart := item.StartedAt
		if overlapStart.Before(start) {
			overlapStart = start
		}
		secs := calcEnd.Sub(overlapStart).Seconds()
		if secs < 0 {
			secs = 0
		}

		if item.Kind == ResourceKindNode {
			summary.TotalNodeSeconds += secs
			if item.Provider != "" && item.Provider != "generic" {
				summary.ActiveManagedNodes++
			}
		} else {
			summary.TotalWorkloadSecs += secs
		}
		summary.TotalRAMMBSeconds += secs * float64(item.RAMMB)
		summary.TotalCPUmSeconds += secs * float64(item.CPUm)
	}

	return summary
}
