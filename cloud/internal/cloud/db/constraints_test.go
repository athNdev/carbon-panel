package db

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUniqueOrgSlug(t *testing.T) {
	t.Parallel()
	s := OpenTest(t)
	require.NoError(t, s.Unscoped().Create(&Org{Name: "A", Slug: "dup"}).Error)
	require.Error(t, s.Unscoped().Create(&Org{Name: "B", Slug: "dup"}).Error)
}

func TestUniqueClerkOrgID(t *testing.T) {
	t.Parallel()
	s := OpenTest(t)
	c1, c2 := "clerk_1", "clerk_1"
	require.NoError(t, s.Unscoped().Create(&Org{Name: "A", Slug: "u-a", ClerkOrgID: &c1}).Error)
	require.Error(t, s.Unscoped().Create(&Org{Name: "B", Slug: "u-b", ClerkOrgID: &c2}).Error)
	// NULL clerk ids may repeat (orgs created before Clerk linking).
	require.NoError(t, s.Unscoped().Create(&Org{Name: "C", Slug: "u-c"}).Error)
	require.NoError(t, s.Unscoped().Create(&Org{Name: "D", Slug: "u-d"}).Error)
}

func TestUniqueMemberOrgUser(t *testing.T) {
	t.Parallel()
	s := OpenTest(t)
	o := Org{Name: "M", Slug: "mem-org"}
	require.NoError(t, s.Unscoped().Create(&o).Error)
	require.NoError(t, s.Unscoped().Create(&Member{TenantBase: TenantBase{OrgID: o.ID}, UserID: "u1"}).Error)
	require.Error(t, s.Unscoped().Create(&Member{TenantBase: TenantBase{OrgID: o.ID}, UserID: "u1"}).Error)
}

func TestUniqueNodeNamePerOrg(t *testing.T) {
	t.Parallel()
	s := OpenTest(t)
	mk := func(slug string) string {
		o := Org{Name: slug, Slug: slug}
		require.NoError(t, s.Unscoped().Create(&o).Error)
		return o.ID
	}
	a, b := mk("node-org-a"), mk("node-org-b")
	require.NoError(t, s.Unscoped().Create(&Node{TenantBase: TenantBase{OrgID: a}, Name: "shared"}).Error)
	// Same name in the same org conflicts...
	require.Error(t, s.Unscoped().Create(&Node{TenantBase: TenantBase{OrgID: a}, Name: "shared"}).Error)
	// ...but another org may reuse it.
	require.NoError(t, s.Unscoped().Create(&Node{TenantBase: TenantBase{OrgID: b}, Name: "shared"}).Error)
}

func TestUniqueWorkloadNamePerOrg(t *testing.T) {
	t.Parallel()
	s := OpenTest(t)
	mk := func(slug string) string {
		o := Org{Name: slug, Slug: slug}
		require.NoError(t, s.Unscoped().Create(&o).Error)
		return o.ID
	}
	a, b := mk("wl-org-a"), mk("wl-org-b")
	require.NoError(t, s.Unscoped().Create(&Workload{TenantBase: TenantBase{OrgID: a}, Name: "mc"}).Error)
	require.Error(t, s.Unscoped().Create(&Workload{TenantBase: TenantBase{OrgID: a}, Name: "mc"}).Error)
	require.NoError(t, s.Unscoped().Create(&Workload{TenantBase: TenantBase{OrgID: b}, Name: "mc"}).Error)
}

func TestUniqueApiKeyHashAndJoinSecret(t *testing.T) {
	t.Parallel()
	s := OpenTest(t)
	o := Org{Name: "K", Slug: "key-org"}
	require.NoError(t, s.Unscoped().Create(&o).Error)
	require.NoError(t, s.Unscoped().Create(&ApiKey{TenantBase: TenantBase{OrgID: o.ID}, Name: "k1", Hash: "h1"}).Error)
	require.Error(t, s.Unscoped().Create(&ApiKey{TenantBase: TenantBase{OrgID: o.ID}, Name: "k2", Hash: "h1"}).Error)
	require.NoError(t, s.Unscoped().Create(&JoinToken{TenantBase: TenantBase{OrgID: o.ID}, Name: "j1", SecretHash: "s1"}).Error)
	require.Error(t, s.Unscoped().Create(&JoinToken{TenantBase: TenantBase{OrgID: o.ID}, Name: "j2", SecretHash: "s1"}).Error)
}

func TestSeedDefaultsIdempotent(t *testing.T) {
	t.Parallel()
	s := OpenTest(t)
	require.NoError(t, s.SeedDefaults(t.Context()))
	require.NoError(t, s.SeedDefaults(t.Context()))

	var types []NodeType
	require.NoError(t, s.Unscoped().Order("sort_order").Find(&types).Error)
	require.Len(t, types, 5)
	require.Equal(t, "nano", types[0].ID)
	require.Equal(t, "xlarge", types[4].ID)

	got := map[string]NodeType{}
	for _, nt := range types {
		got[nt.ID] = nt
	}
	require.Equal(t, 1, got["nano"].VCPU)
	require.Equal(t, 2048, got["nano"].RAMMB)
	require.Equal(t, 20, got["nano"].DiskGB)
	require.Equal(t, 6.0, got["nano"].MonthlyPriceUSD)
	require.Equal(t, 16, got["xlarge"].VCPU)
	require.Equal(t, 32768, got["xlarge"].RAMMB)
	med := got["medium"]
	require.Equal(t, "cx42", med.InstanceTypeMap()["hetzner"])
	require.Equal(t, "t3.large", med.InstanceTypeMap()["aws"])
	require.Equal(t, "e2-standard-2", med.InstanceTypeMap()["gcp"])
	require.Equal(t, "s-4vcpu-8gb", med.InstanceTypeMap()["digitalocean"])
	require.Equal(t, "custom", med.InstanceTypeMap()["proxmox"])
	require.Equal(t, "custom", med.InstanceTypeMap()["generic"])
	xl := got["xlarge"]
	require.Equal(t, "ccx33", xl.InstanceTypeMap()["hetzner"])
	require.True(t, got["small"].Enabled)
}

// TestSeedDefaultsNeverClobbersOperatorEdits proves price/enabled edits
// survive re-seeding, while blank columns still get backfilled.
func TestSeedDefaultsNeverClobbersOperatorEdits(t *testing.T) {
	t.Parallel()
	s := OpenTest(t)

	require.NoError(t, s.Unscoped().Model(&NodeType{}).Where("id = ?", "small").
		Updates(map[string]any{"monthly_price_usd": 99.0, "enabled": false}).Error)
	require.NoError(t, s.Unscoped().Model(&NodeType{}).Where("id = ?", "nano").
		Update("instance_types", "").Error)

	require.NoError(t, s.SeedDefaults(t.Context()))

	var small NodeType
	require.NoError(t, s.Unscoped().Where("id = ?", "small").First(&small).Error)
	require.Equal(t, 99.0, small.MonthlyPriceUSD)
	require.False(t, small.Enabled)

	var nano NodeType
	require.NoError(t, s.Unscoped().Where("id = ?", "nano").First(&nano).Error)
	require.NotEmpty(t, nano.InstanceTypes)
	require.Equal(t, "cx22", nano.InstanceTypeMap()["hetzner"])
}
