package reconcile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/notify"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *db.Store {
	t.Helper()
	s, err := db.Open(db.Options{Driver: "sqlite", DSN: ":memory:", AutoMigrate: true})
	require.NoError(t, err)
	return s
}

func TestReconciler_ReconcileNodesAndWorkloads(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	// Create test HTTP server for webhook verification
	var receivedEvents []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dispatcher := notify.NewDispatcher(srv.Client())
	dispatcher.Register(&notify.WebhookSubscription{
		ID:        "sub_reconcile",
		OrgID:     "org_reconcile",
		TargetURL: srv.URL,
		Secret:    "secret123",
		Events:    []string{"node.*", "workload.*"},
		Enabled:   true,
	})

	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	staleThreshold := 5 * time.Minute

	// Setup nodes in db:
	// Node 1: active, heartbeated 1 min ago -> should stay active
	recentHeartbeat := now.Add(-1 * time.Minute)
	nActive := db.Node{
		TenantBase:    db.TenantBase{ID: "node_active", OrgID: "org_reconcile"},
		Name:          "active-node",
		Status:        "active",
		LastHeartbeat: &recentHeartbeat,
	}
	require.NoError(t, store.DB().Create(&nActive).Error)

	// Node 2: active, heartbeated 10 mins ago -> should be reaped (marked offline)
	oldHeartbeat := now.Add(-10 * time.Minute)
	nStale := db.Node{
		TenantBase:    db.TenantBase{ID: "node_stale", OrgID: "org_reconcile"},
		Name:          "stale-node",
		Status:        "active",
		LastHeartbeat: &oldHeartbeat,
	}
	require.NoError(t, store.DB().Create(&nStale).Error)

	// Workload 1 on nActive -> should remain running
	w1 := db.Workload{
		TenantBase: db.TenantBase{ID: "wl_healthy", OrgID: "org_reconcile"},
		NodeID:     nActive.ID,
		Name:       "healthy-workload",
		Status:     "running",
	}
	require.NoError(t, store.DB().Create(&w1).Error)

	// Workload 2 on nStale -> should transition to degraded
	w2 := db.Workload{
		TenantBase: db.TenantBase{ID: "wl_abandoned", OrgID: "org_reconcile"},
		NodeID:     nStale.ID,
		Name:       "abandoned-workload",
		Status:     "running",
	}
	require.NoError(t, store.DB().Create(&w2).Error)

	rec := New(Options{
		Store:              store,
		Notifier:           dispatcher,
		StaleNodeThreshold: staleThreshold,
		Now:                func() time.Time { return now },
	})

	reaped, degraded, err := rec.ReconcileNodes(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, reaped)
	require.Equal(t, 1, degraded)

	// Verify nActive is still active
	var gotActive db.Node
	require.NoError(t, store.DB().Where("id = ?", nActive.ID).First(&gotActive).Error)
	require.Equal(t, "active", gotActive.Status)

	// Verify nStale is now offline
	var gotStale db.Node
	require.NoError(t, store.DB().Where("id = ?", nStale.ID).First(&gotStale).Error)
	require.Equal(t, "offline", gotStale.Status)

	// Verify w1 is still running
	var gotW1 db.Workload
	require.NoError(t, store.DB().Where("id = ?", w1.ID).First(&gotW1).Error)
	require.Equal(t, "running", gotW1.Status)

	// Verify w2 is now degraded
	var gotW2 db.Workload
	require.NoError(t, store.DB().Where("id = ?", w2.ID).First(&gotW2).Error)
	require.Equal(t, "degraded", gotW2.Status)
	require.Contains(t, gotW2.StatusDetail, "heartbeat timeout")

	// Verify workload breadcrumb event was recorded
	var events []db.WorkloadEvent
	require.NoError(t, store.DB().Where("workload_id = ?", w2.ID).Find(&events).Error)
	require.Len(t, events, 1)
	require.Equal(t, "degraded", events[0].Kind)

	// Verify webhook deliveries occurred for node.offline and workload.degraded
	attempts := dispatcher.RecentAttempts()
	require.Len(t, attempts, 2)
	_ = receivedEvents
}

func TestReconciler_PurgeExpiredTokens(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	retention := 24 * time.Hour

	// 1. Fresh active token (expires in 1h) -> keep
	future := now.Add(1 * time.Hour)
	tFresh := db.JoinToken{
		TenantBase: db.TenantBase{ID: "tok_fresh", OrgID: "org_1"},
		Name:       "fresh-token",
		SecretHash: "hash_fresh",
		ExpiresAt:  &future,
	}
	require.NoError(t, store.DB().Create(&tFresh).Error)

	// 2. Expired recently (expires 1h ago, within retention of 24h) -> keep
	expiredRecent := now.Add(-1 * time.Hour)
	tExpiredRecent := db.JoinToken{
		TenantBase: db.TenantBase{ID: "tok_expired_recent", OrgID: "org_1"},
		Name:       "expired-recent-token",
		SecretHash: "hash_recent",
		ExpiresAt:  &expiredRecent,
	}
	require.NoError(t, store.DB().Create(&tExpiredRecent).Error)

	// 3. Expired past retention (expires 48h ago) -> purge
	expiredOld := now.Add(-48 * time.Hour)
	tExpiredOld := db.JoinToken{
		TenantBase: db.TenantBase{ID: "tok_expired_old", OrgID: "org_1"},
		Name:       "expired-old-token",
		SecretHash: "hash_old",
		ExpiresAt:  &expiredOld,
	}
	require.NoError(t, store.DB().Create(&tExpiredOld).Error)

	// 4. Redeemed past retention (used 48h ago) -> purge
	usedOld := now.Add(-48 * time.Hour)
	tUsedOld := db.JoinToken{
		TenantBase: db.TenantBase{ID: "tok_used_old", OrgID: "org_1"},
		Name:       "used-old-token",
		SecretHash: "hash_used",
		UsedAt:     &usedOld,
	}
	require.NoError(t, store.DB().Create(&tUsedOld).Error)

	rec := New(Options{
		Store:                 store,
		ExpiredTokenRetention: retention,
		Now:                   func() time.Time { return now },
	})

	purged, err := rec.PurgeExpiredTokens(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(2), purged)

	// Verify surviving tokens
	var tokens []db.JoinToken
	require.NoError(t, store.DB().Find(&tokens).Error)
	require.Len(t, tokens, 2)
	ids := map[string]bool{tokens[0].ID: true, tokens[1].ID: true}
	require.True(t, ids["tok_fresh"])
	require.True(t, ids["tok_expired_recent"])
}

func TestReconciler_FlushOutbox(t *testing.T) {
	ctx := context.Background()
	fakeSender := notify.NewFakeEmailSender()
	outbox := notify.NewOutbox(fakeSender)

	outbox.Enqueue("org_1", "dev@example.com", "Test 1", "Body 1", "")
	outbox.Enqueue("org_1", "admin@example.com", "Test 2", "Body 2", "")

	rec := New(Options{
		Outbox: outbox,
	})

	sent, err := rec.FlushOutbox(ctx, 50)
	require.NoError(t, err)
	require.Equal(t, 2, sent)
	require.Equal(t, 0, outbox.PendingCount())
	require.Len(t, fakeSender.Sent, 2)
}

func TestReconciler_RunAndStop(t *testing.T) {
	store := newTestStore(t)
	fakeSender := notify.NewFakeEmailSender()
	outbox := notify.NewOutbox(fakeSender)
	outbox.Enqueue("org_1", "test@example.com", "Subject", "Body", "")

	rec := New(Options{
		Store:  store,
		Outbox: outbox,
	})

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})

	go func() {
		rec.Run(ctx, 10*time.Millisecond)
		close(doneCh)
	}()

	time.Sleep(25 * time.Millisecond)
	cancel()

	select {
	case <-doneCh:
		// Clean shutdown verified
	case <-time.After(500 * time.Millisecond):
		t.Fatal("reconciler did not stop on ctx cancel")
	}

	require.Equal(t, 0, outbox.PendingCount())
	require.Len(t, fakeSender.Sent, 1)
}
