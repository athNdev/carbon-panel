// Package billing provides plan definitions, resource quota enforcement,
// usage metering, and invoicing abstractions for Carbon Cloud tenants.
package billing

import (
	"fmt"
	"strings"
)

// Plan defines the commercial limits and capabilities of an organization tier.
type Plan struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Description         string `json:"description"`
	PriceCentsMonth     int64  `json:"price_cents_month"`
	MaxNodes            int    `json:"max_nodes"`          // -1 = unlimited
	MaxManagedNodes     int    `json:"max_managed_nodes"`  // -1 = unlimited
	MaxWorkloads        int    `json:"max_workloads"`      // -1 = unlimited
	MaxRAMMB            int64  `json:"max_ram_mb"`         // -1 = unlimited
	MaxCPUMillicores    int64  `json:"max_cpu_millicores"` // -1 = unlimited
	AllowsCustomDomain  bool   `json:"allows_custom_domain"`
	AllowsPriorityQueue bool   `json:"allows_priority_queue"`
}

var defaultPlans = map[string]Plan{
	"free": {
		ID:                  "free",
		Name:                "Community / Free",
		Description:         "Single BYON node, 2 workloads for individuals and hobbyists",
		PriceCentsMonth:     0,
		MaxNodes:            1,
		MaxManagedNodes:     0,
		MaxWorkloads:        2,
		MaxRAMMB:            4096,
		MaxCPUMillicores:    2000,
		AllowsCustomDomain:  false,
		AllowsPriorityQueue: false,
	},
	"pro": {
		ID:                  "pro",
		Name:                "Pro",
		Description:         "Up to 5 nodes with automated cloud provisioning and custom domains",
		PriceCentsMonth:     2900, // $29/mo
		MaxNodes:            5,
		MaxManagedNodes:     3,
		MaxWorkloads:        15,
		MaxRAMMB:            32768, // 32 GB
		MaxCPUMillicores:    16000, // 16 vCPU
		AllowsCustomDomain:  true,
		AllowsPriorityQueue: false,
	},
	"team": {
		ID:                  "team",
		Name:                "Team",
		Description:         "Up to 20 nodes with multi-cloud provisioning and priority queues",
		PriceCentsMonth:     9900, // $99/mo
		MaxNodes:            20,
		MaxManagedNodes:     10,
		MaxWorkloads:        60,
		MaxRAMMB:            131072, // 128 GB
		MaxCPUMillicores:    64000,  // 64 vCPU
		AllowsCustomDomain:  true,
		AllowsPriorityQueue: true,
	},
	"enterprise": {
		ID:                  "enterprise",
		Name:                "Enterprise",
		Description:         "Unlimited nodes, workloads, and resources with dedicated support",
		PriceCentsMonth:     49900, // $499/mo base
		MaxNodes:            -1,
		MaxManagedNodes:     -1,
		MaxWorkloads:        -1,
		MaxRAMMB:            -1,
		MaxCPUMillicores:    -1,
		AllowsCustomDomain:  true,
		AllowsPriorityQueue: true,
	},
}

// Catalog manages registered plans.
type Catalog struct {
	plans map[string]Plan
}

// NewCatalog initializes a catalog with default tiers.
func NewCatalog() *Catalog {
	c := &Catalog{
		plans: make(map[string]Plan),
	}
	for k, v := range defaultPlans {
		c.plans[k] = v
	}
	return c
}

// RegisterPlan registers or overrides a plan.
func (c *Catalog) RegisterPlan(p Plan) {
	c.plans[strings.ToLower(p.ID)] = p
}

// Get retrieves a plan by ID, falling back to 'free' if not found.
func (c *Catalog) Get(id string) (Plan, bool) {
	if id == "" {
		id = "free"
	}
	p, ok := c.plans[strings.ToLower(id)]
	return p, ok
}

// GetOrDefault retrieves a plan or returns the 'free' tier if nonexistent.
func (c *Catalog) GetOrDefault(id string) Plan {
	if p, ok := c.Get(id); ok {
		return p
	}
	return defaultPlans["free"]
}

// List returns all registered plans in ascending price order.
func (c *Catalog) List() []Plan {
	var list []Plan
	order := []string{"free", "pro", "team", "enterprise"}
	for _, id := range order {
		if p, ok := c.plans[id]; ok {
			list = append(list, p)
		}
	}
	// append any custom plans not in standard order
	for k, v := range c.plans {
		found := false
		for _, o := range order {
			if o == k {
				found = true
				break
			}
		}
		if !found {
			list = append(list, v)
		}
	}
	return list
}

func (p Plan) String() string {
	return fmt.Sprintf("Plan(%s: $%d/mo, nodes=%d, managed=%d, workloads=%d)",
		p.ID, p.PriceCentsMonth/100, p.MaxNodes, p.MaxManagedNodes, p.MaxWorkloads)
}
