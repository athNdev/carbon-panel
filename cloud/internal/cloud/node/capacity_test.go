package node

import (
	"context"
	"testing"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	"github.com/stretchr/testify/require"
)

func placeWorkload(t *testing.T, s *db.Store, ctx context.Context, nodeID, name string, req db.NodeCapacity) {
	t.Helper()
	q, err := s.Org(ctx)
	require.NoError(t, err)
	w := db.Workload{NodeID: nodeID, Name: name, Status: "running"}
	w.TenantBase = db.TenantBase{ID: newUUID(), OrgID: principal.OrgID(ctx)}
	w.SetSpec(map[string]any{"vcpu": req.VCPU, "ram_mb": req.RAMMB, "disk_gb": req.DiskGB})
	require.NoError(t, q.Create(&w).Error)
}

func TestCapacityExactBoundary(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	ctx, _ := orgCtx(t, s)
	svc := NewService(deps(s))
	cap := NewCapacity(deps(s))

	n, err := svc.Register(ctx, RegisterRequest{
		Name:     "cap",
		Capacity: db.NodeCapacity{VCPU: 4, RAMMB: 8192, DiskGB: 80},
	})
	require.NoError(t, err)

	// Exact fit accepted.
	require.NoError(t, cap.Check(ctx, n.ID, db.NodeCapacity{VCPU: 4, RAMMB: 8192, DiskGB: 80}))

	placeWorkload(t, s, ctx, n.ID, "w1", db.NodeCapacity{VCPU: 3, RAMMB: 4096, DiskGB: 40})
	used, total, err := cap.Usage(ctx, n.ID)
	require.NoError(t, err)
	require.Equal(t, db.NodeCapacity{VCPU: 3, RAMMB: 4096, DiskGB: 40}, used)
	require.Equal(t, db.NodeCapacity{VCPU: 4, RAMMB: 8192, DiskGB: 80}, total)

	// Remainder is exactly 1/4096/40: fits, and one unit over does not.
	require.NoError(t, cap.Check(ctx, n.ID, db.NodeCapacity{VCPU: 1, RAMMB: 4096, DiskGB: 40}))
	require.ErrorIs(t, cap.Check(ctx, n.ID, db.NodeCapacity{VCPU: 2, RAMMB: 1, DiskGB: 1}), ErrCapacityExceeded)
	require.ErrorIs(t, cap.Check(ctx, n.ID, db.NodeCapacity{VCPU: 1, RAMMB: 4097, DiskGB: 1}), ErrCapacityExceeded)
	require.ErrorIs(t, cap.Check(ctx, n.ID, db.NodeCapacity{VCPU: 1, RAMMB: 1, DiskGB: 41}), ErrCapacityExceeded)

	rem, err := cap.Remaining(ctx, n.ID)
	require.NoError(t, err)
	require.Equal(t, db.NodeCapacity{VCPU: 1, RAMMB: 4096, DiskGB: 40}, rem)
}

func TestCapacityDrainRefuses(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	ctx, _ := orgCtx(t, s)
	svc := NewService(deps(s))
	cap := NewCapacity(deps(s))

	n, err := svc.Register(ctx, RegisterRequest{
		Name:     "drain-cap",
		Capacity: db.NodeCapacity{VCPU: 8, RAMMB: 16384, DiskGB: 160},
	})
	require.NoError(t, err)
	_, err = svc.Drain(ctx, n.ID)
	require.NoError(t, err)

	require.ErrorIs(t, cap.Check(ctx, n.ID, db.NodeCapacity{VCPU: 1}), ErrNodeDraining)
}

func TestCapacityUnknownNodeAndNoOrg(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	ctx, _ := orgCtx(t, s)
	cap := NewCapacity(deps(s))

	require.ErrorIs(t, cap.Check(ctx, "missing", db.NodeCapacity{VCPU: 1}), ErrNodeNotFound)
	_, _, err := cap.Usage(ctx, "missing")
	require.ErrorIs(t, err, ErrNodeNotFound)

	bare := context.Background()
	require.ErrorIs(t, cap.Check(bare, "x", db.NodeCapacity{}), db.ErrNoOrg)
	_, _, err = cap.Usage(bare, "x")
	require.ErrorIs(t, err, db.ErrNoOrg)
}

func TestRequestOfIgnoresUnshapedSpecs(t *testing.T) {
	t.Parallel()
	w := db.Workload{}
	require.Equal(t, db.NodeCapacity{}, RequestOf(w))
	w.SetSpec(map[string]any{"image": "paper", "vcpu": "lots"})
	require.Equal(t, db.NodeCapacity{}, RequestOf(w))
}
