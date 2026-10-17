package db

import (
	"context"
	"reflect"
	"testing"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	"github.com/stretchr/testify/require"
)

// tenantMarker is the structural contract every tenant-owned model honors.
type tenantMarker interface{ IsTenantOwned() bool }

func orgCtx(orgID string) context.Context {
	return principal.WithPrincipal(context.Background(), principal.Principal{
		Kind: principal.KindSession, UserID: "u1", OrgID: orgID, Role: "owner",
	})
}

func TestOrgRequiresContext(t *testing.T) {
	t.Parallel()
	s := OpenTest(t)

	_, err := s.Org(context.Background())
	require.ErrorIs(t, err, ErrNoOrg)

	_, err = s.Org(principal.WithPrincipal(context.Background(), principal.Principal{}))
	require.ErrorIs(t, err, ErrNoOrg)
}

// TestTenantMarkerReflection fails if a model with an OrgID field does not
// implement IsTenantOwned, or if a model without one (except the Org root
// record, which owns itself by ID) claims to be tenant-owned.
func TestTenantMarkerReflection(t *testing.T) {
	t.Parallel()
	models := append(tenantModels(), &NodeType{})
	for _, m := range models {
		typ := reflect.TypeOf(m).Elem()
		hasOrgID := false
		var walk func(rt reflect.Type)
		walk = func(rt reflect.Type) {
			for i := range rt.NumField() {
				f := rt.Field(i)
				if f.Anonymous {
					walk(f.Type)
					continue
				}
				if f.Name == "OrgID" {
					hasOrgID = true
				}
			}
		}
		walk(typ)
		_, implements := m.(tenantMarker)
		if hasOrgID {
			require.True(t, implements, "%s has OrgID but no IsTenantOwned marker", typ.Name())
		} else if typ.Name() != "Org" {
			require.False(t, implements, "%s has no OrgID but claims IsTenantOwned", typ.Name())
		}
	}
}

// TestCrossOrgIsolation creates two orgs with rows in each tenant table and
// asserts Org(ctxA) never sees org B's rows and cannot update them.
func TestCrossOrgIsolation(t *testing.T) {
	t.Parallel()
	s := OpenTest(t)
	ctx := context.Background()

	mkOrg := func(slug string) string {
		o := Org{Name: slug, Slug: slug}
		require.NoError(t, s.Unscoped().WithContext(ctx).Create(&o).Error)
		return o.ID
	}
	orgA, orgB := mkOrg("orga"), mkOrg("orgb")
	ctxA, ctxB := orgCtx(orgA), orgCtx(orgB)

	// Seed one row per tenant table in each org via the unscoped handle.
	nodeA := Node{TenantBase: TenantBase{OrgID: orgA}, Name: "node-1"}
	nodeB := Node{TenantBase: TenantBase{OrgID: orgB}, Name: "node-1"}
	require.NoError(t, s.Unscoped().Create(&nodeA).Error)
	require.NoError(t, s.Unscoped().Create(&nodeB).Error)

	wlA := Workload{TenantBase: TenantBase{OrgID: orgA}, Name: "wl-1", NodeID: nodeA.ID}
	wlB := Workload{TenantBase: TenantBase{OrgID: orgB}, Name: "wl-1", NodeID: nodeB.ID}
	require.NoError(t, s.Unscoped().Create(&wlA).Error)
	require.NoError(t, s.Unscoped().Create(&wlB).Error)

	// Reads are fenced.
	qA, err := s.Org(ctxA)
	require.NoError(t, err)
	var nodes []Node
	require.NoError(t, qA.Find(&nodes).Error)
	require.Len(t, nodes, 1)
	require.Equal(t, orgA, nodes[0].OrgID)

	qB, err := s.Org(ctxB)
	require.NoError(t, err)
	require.NoError(t, qB.Find(&nodes).Error)
	require.Len(t, nodes, 1)
	require.Equal(t, orgB, nodes[0].OrgID)

	// Point lookup of B's row through A's scope misses.
	qA2, _ := s.Org(ctxA)
	require.Error(t, qA2.Where("id = ?", nodeB.ID).First(&Node{}).Error)

	// Updates through A's scope cannot touch B's row: the org predicate is
	// part of the statement, so zero rows match.
	qA3, _ := s.Org(ctxA)
	res := qA3.Model(&Node{}).Where("id = ?", nodeB.ID).Update("status", "hijacked")
	require.NoError(t, res.Error)
	require.Zero(t, res.RowsAffected)
	var check Node
	require.NoError(t, s.Unscoped().Where("id = ?", nodeB.ID).First(&check).Error)
	require.Empty(t, check.Status)

	// Deletes through A's scope cannot remove B's row either.
	qA4, _ := s.Org(ctxA)
	del := qA4.Where("id = ?", wlB.ID).Delete(&Workload{})
	require.NoError(t, del.Error)
	require.Zero(t, del.RowsAffected)
	var still Workload
	require.NoError(t, s.Unscoped().Where("id = ?", wlB.ID).First(&still).Error)

	// Members / keys / tokens / provisions / events / audit / bindings fenced.
	require.NoError(t, s.Unscoped().Create(&Member{TenantBase: TenantBase{OrgID: orgB}, UserID: "bob"}).Error)
	qA5, _ := s.Org(ctxA)
	var members []Member
	require.NoError(t, qA5.Find(&members).Error)
	require.Empty(t, members)

	// Global table stays visible without an org.
	var types []NodeType
	require.NoError(t, s.Unscoped().Find(&types).Error)
	require.NotEmpty(t, types)
}

// TestOrgScopeUpdateOwnRow verifies the scoped handle can still mutate rows
// that actually belong to the caller's org (deny-by-default must not deny
// legitimate writes).
func TestOrgScopeUpdateOwnRow(t *testing.T) {
	t.Parallel()
	s := OpenTest(t)
	ctx := context.Background()
	o := Org{Name: "solo", Slug: "solo"}
	require.NoError(t, s.Unscoped().WithContext(ctx).Create(&o).Error)

	n := Node{TenantBase: TenantBase{OrgID: o.ID}, Name: "n1"}
	require.NoError(t, s.Unscoped().Create(&n).Error)

	q, err := s.Org(orgCtx(o.ID))
	require.NoError(t, err)
	require.NoError(t, q.Model(&Node{}).Where("id = ?", n.ID).Update("status", "online").Error)

	var got Node
	require.NoError(t, s.Unscoped().Where("id = ?", n.ID).First(&got).Error)
	require.Equal(t, "online", got.Status)
}
