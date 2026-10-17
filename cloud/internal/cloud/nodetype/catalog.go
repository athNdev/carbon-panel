// Package nodetype is the global node-type catalog over db.NodeType.
//
// The table is global (NOT tenant-owned): reads go through Store.Unscoped()
// and never require an org in the context. Seeding is idempotent and
// delegates to db.SeedDefaults, which never clobbers operator edits.
package nodetype

import (
	"context"
	"errors"
	"sort"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
)

// ErrNodeTypeNotFound is returned when no enabled catalog entry matches.
var ErrNodeTypeNotFound = errors.New("nodetype: not found")

// Providers covered by every default catalog entry.
var Providers = []string{"hetzner", "aws", "gcp", "digitalocean", "proxmox", "generic"}

// Catalog reads the global node-type table.
type Catalog struct {
	store *db.Store
}

// NewCatalog returns a Catalog over store. Store must be non-nil.
func NewCatalog(store *db.Store) *Catalog {
	return &Catalog{store: store}
}

// EnsureSeeded idempotently seeds nano, small, medium, large, xlarge.
// Safe to call on every boot; operator price/enabled edits are preserved.
func (c *Catalog) EnsureSeeded(ctx context.Context) error {
	return c.store.SeedDefaults(ctx)
}

// List returns every catalog entry ordered by sort order, then id.
func (c *Catalog) List(_ context.Context) ([]db.NodeType, error) {
	var out []db.NodeType
	if err := c.store.Unscoped().Order("sort_order ASC").Order("id ASC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// Get returns one entry by id. Disabled entries are still returned;
// callers deciding placement should check Enabled themselves.
func (c *Catalog) Get(_ context.Context, id string) (db.NodeType, error) {
	var t db.NodeType
	if err := c.store.Unscoped().Where("id = ?", id).First(&t).Error; err != nil {
		return db.NodeType{}, ErrNodeTypeNotFound
	}
	return t, nil
}

// InstanceType returns the provider-specific instance/material name for a
// node-type kind (e.g. kind "medium", provider "hetzner" -> "cx42").
// The second result is false when the kind or the provider is unknown.
func (c *Catalog) InstanceType(ctx context.Context, kind, provider string) (string, bool) {
	t, err := c.Get(ctx, kind)
	if err != nil {
		return "", false
	}
	v, ok := t.InstanceTypeMap()[provider]
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// Upsert creates or replaces a custom catalog entry. Default ids may be
// overridden deliberately (e.g. in tests); production code should use new ids.
func (c *Catalog) Upsert(_ context.Context, t db.NodeType) (db.NodeType, error) {
	if t.ID == "" {
		return db.NodeType{}, ErrNodeTypeNotFound
	}
	if err := c.store.Unscoped().Save(&t).Error; err != nil {
		return db.NodeType{}, err
	}
	return t, nil
}

// Enabled returns the enabled entries in catalog order.
func (c *Catalog) Enabled(ctx context.Context) ([]db.NodeType, error) {
	all, err := c.List(ctx)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, t := range all {
		if t.Enabled {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SortOrder == out[j].SortOrder {
			return out[i].ID < out[j].ID
		}
		return out[i].SortOrder < out[j].SortOrder
	})
	return out, nil
}
