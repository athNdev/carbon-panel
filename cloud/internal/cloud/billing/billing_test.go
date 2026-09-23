package billing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCatalog_GetAndList(t *testing.T) {
	cat := NewCatalog()

	// Default free tier
	free := cat.GetOrDefault("free")
	require.Equal(t, "free", free.ID)
	require.Equal(t, 1, free.MaxNodes)
	require.Equal(t, 0, free.MaxManagedNodes)
	require.Equal(t, 2, free.MaxWorkloads)

	// Non-existent falls back to free
	fallback := cat.GetOrDefault("unknown-plan")
	require.Equal(t, "free", fallback.ID)

	// Pro tier
	pro, ok := cat.Get("pro")
	require.True(t, ok)
	require.Equal(t, int64(2900), pro.PriceCentsMonth)
	require.Equal(t, 5, pro.MaxNodes)
	require.Equal(t, 3, pro.MaxManagedNodes)

	// List
	plans := cat.List()
	require.Len(t, plans, 4)
	require.Equal(t, "free", plans[0].ID)
	require.Equal(t, "pro", plans[1].ID)
	require.Equal(t, "team", plans[2].ID)
	require.Equal(t, "enterprise", plans[3].ID)
}

func TestQuotaEnforcer_Check(t *testing.T) {
	cat := NewCatalog()
	enforcer := NewQuotaEnforcer(cat)
	free := cat.GetOrDefault("free")

	// Free plan: 1 node, 0 managed, 2 workloads, 4096 RAM, 2000 CPU
	current := Usage{
		NodeCount:        1,
		ManagedNodeCount: 0,
		WorkloadCount:    1,
		AllocatedRAMMB:   2048,
		AllocatedCPUm:    1000,
	}

	t.Run("within quota delta succeeds", func(t *testing.T) {
		delta := Usage{
			WorkloadCount:  1,
			AllocatedRAMMB: 1024,
			AllocatedCPUm:  500,
		}
		err := enforcer.Check(free, current, delta)
		require.NoError(t, err)
	})

	t.Run("exceeding node count fails", func(t *testing.T) {
		delta := Usage{NodeCount: 1}
		err := enforcer.Check(free, current, delta)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrQuotaExceededNodes))
		var qErr *QuotaError
		require.True(t, errors.As(err, &qErr))
		require.Equal(t, "nodes", qErr.Resource)
		require.Equal(t, int64(1), qErr.Current)
		require.Equal(t, int64(1), qErr.Delta)
		require.Equal(t, int64(1), qErr.Limit)
	})

	t.Run("exceeding managed node count fails", func(t *testing.T) {
		delta := Usage{ManagedNodeCount: 1}
		err := enforcer.Check(free, current, delta)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrQuotaExceededManagedNodes))
	})

	t.Run("exceeding workload count fails", func(t *testing.T) {
		delta := Usage{WorkloadCount: 2} // 1 + 2 = 3 > 2
		err := enforcer.Check(free, current, delta)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrQuotaExceededWorkloads))
	})

	t.Run("exceeding RAM fails", func(t *testing.T) {
		delta := Usage{AllocatedRAMMB: 3000} // 2048 + 3000 = 5048 > 4096
		err := enforcer.Check(free, current, delta)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrQuotaExceededRAM))
	})

	t.Run("exceeding CPU fails", func(t *testing.T) {
		delta := Usage{AllocatedCPUm: 1500} // 1000 + 1500 = 2500 > 2000
		err := enforcer.Check(free, current, delta)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrQuotaExceededCPU))
	})

	t.Run("enterprise has unlimited quotas", func(t *testing.T) {
		ent := cat.GetOrDefault("enterprise")
		hugeDelta := Usage{
			NodeCount:        1000,
			ManagedNodeCount: 500,
			WorkloadCount:    5000,
			AllocatedRAMMB:   10000000,
			AllocatedCPUm:    50000000,
		}
		err := enforcer.Check(ent, current, hugeDelta)
		require.NoError(t, err)
	})
}

func TestMeter_TrackAndSummary(t *testing.T) {
	m := NewMeter()
	orgID := "org_tenant_1"

	t0 := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(30 * time.Minute)
	t2 := t0.Add(60 * time.Minute)

	// Start node 1 and stop after 30 mins
	m.StartResource(orgID, ResourceKindNode, "node_1", 4096, 2000, "hetzner", t0)
	record := m.StopResource(ResourceKindNode, "node_1", t1)
	require.NotNil(t, record)
	require.Equal(t, float64(1800), record.DurationSec)

	// Start workload 1 from t1 to t2
	m.StartResource(orgID, ResourceKindWorkload, "wl_1", 2048, 1000, "", t1)
	m.StopResource(ResourceKindWorkload, "wl_1", t2)

	summary := m.Summary(orgID, t0, t2)
	require.Equal(t, orgID, summary.OrgID)
	require.Equal(t, float64(1800), summary.TotalNodeSeconds)
	require.Equal(t, float64(1800), summary.TotalWorkloadSecs)
	// RAM-seconds: 1800 * 4096 + 1800 * 2048 = 7372800 + 3686400 = 11059200
	require.Equal(t, float64(11059200), summary.TotalRAMMBSeconds)
}

func TestFakeInvoicer(t *testing.T) {
	ctx := context.Background()
	invoicer := NewFakeInvoicer()

	orgID := "org_abc123"
	custID, err := invoicer.CreateCustomer(ctx, orgID, "admin@example.com", "Acme Org")
	require.NoError(t, err)
	require.Equal(t, "cust_org_abc123", custID)

	err = invoicer.SyncSubscription(ctx, orgID, "pro")
	require.NoError(t, err)

	inv, err := invoicer.IssueInvoice(ctx, orgID, 2900, "Pro Plan Subscription (Monthly)")
	require.NoError(t, err)
	require.Equal(t, int64(2900), inv.AmountCents)
	require.Equal(t, InvoiceStatusPaid, inv.Status)

	list, err := invoicer.ListInvoices(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, inv.ID, list[0].ID)
}
