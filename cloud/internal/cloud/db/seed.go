package db

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// defaultNodeTypes is the shared node-type catalog. The table belongs to this
// lane; the values are shared with the terraform and nodetype lanes.
func defaultNodeTypes() []NodeType {
	mk := func(id, name string, vcpu, ramMB, diskGB int, price float64, desc string, order int, instances map[string]string) NodeType {
		t := NodeType{
			ID: id, Name: name,
			VCPU: vcpu, RAMMB: ramMB, DiskGB: diskGB,
			MonthlyPriceUSD: price, Description: desc,
			SortOrder: order, Enabled: true,
		}
		t.SetInstanceTypes(instances)
		return t
	}
	return []NodeType{
		mk("nano", "Nano", 1, 2048, 20, 6, "Single small Minecraft server", 10, map[string]string{
			"hetzner": "cx22", "aws": "t3.small", "gcp": "e2-small",
			"digitalocean": "s-1vcpu-2gb", "proxmox": "custom", "generic": "custom",
		}),
		mk("small", "Small", 2, 4096, 40, 12, "Small community server", 20, map[string]string{
			"hetzner": "cx32", "aws": "t3.medium", "gcp": "e2-medium",
			"digitalocean": "s-2vcpu-4gb", "proxmox": "custom", "generic": "custom",
		}),
		mk("medium", "Medium", 4, 8192, 80, 24, "Medium server with headroom", 30, map[string]string{
			"hetzner": "cx42", "aws": "t3.large", "gcp": "e2-standard-2",
			"digitalocean": "s-4vcpu-8gb", "proxmox": "custom", "generic": "custom",
		}),
		mk("large", "Large", 8, 16384, 160, 48, "Large network hub", 40, map[string]string{
			"hetzner": "cx52", "aws": "m6i.xlarge", "gcp": "e2-standard-4",
			"digitalocean": "s-8vcpu-16gb", "proxmox": "custom", "generic": "custom",
		}),
		mk("xlarge", "XLarge", 16, 32768, 320, 96, "Flagship capacity", 50, map[string]string{
			"hetzner": "ccx33", "aws": "m6i.2xlarge", "gcp": "e2-standard-8",
			"digitalocean": "s-16vcpu-32gb", "proxmox": "custom", "generic": "custom",
		}),
	}
}

// SeedDefaults upserts the default node-type catalog by id. It is idempotent
// and never clobbers operator edits: price and enabled are only set on
// insert, and non-empty columns on an existing row are left alone.
func (s *Store) SeedDefaults(_ context.Context) error {
	for _, def := range defaultNodeTypes() {
		var cur NodeType
		err := s.db.Where("id = ?", def.ID).First(&cur).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := s.db.Create(&def).Error; err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		patch := map[string]any{}
		if cur.Name == "" {
			patch["name"] = def.Name
		}
		if cur.Description == "" {
			patch["description"] = def.Description
		}
		if cur.InstanceTypes == "" {
			patch["instance_types"] = def.InstanceTypes
		}
		if cur.VCPU == 0 {
			patch["vcpu"] = def.VCPU
		}
		if cur.RAMMB == 0 {
			patch["ram_mb"] = def.RAMMB
		}
		if cur.DiskGB == 0 {
			patch["disk_gb"] = def.DiskGB
		}
		if cur.SortOrder == 0 {
			patch["sort_order"] = def.SortOrder
		}
		// MonthlyPriceUSD and Enabled are deliberately never patched:
		// they are operator-owned.
		if len(patch) > 0 {
			if err := s.db.Model(&NodeType{}).Where("id = ?", def.ID).Updates(patch).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
