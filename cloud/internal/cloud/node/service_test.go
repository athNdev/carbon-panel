package node

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/keys"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/secrets"
	"github.com/stretchr/testify/require"
)

// testPepper is an obvious fake used only to exercise the HMAC path.
var testPepper = []byte("test-only-fake-pepper-not-a-secret")

func openStore(t *testing.T) *db.Store {
	t.Helper()
	s, err := db.Open(db.Options{
		Driver:      "sqlite",
		DSN:         filepath.Join(t.TempDir(), "test.db") + "?_pragma=busy_timeout(5000)",
		AutoMigrate: true,
	})
	require.NoError(t, err)
	require.NoError(t, s.SeedDefaults(t.Context()))
	return s
}

func orgCtx(t *testing.T, s *db.Store) (context.Context, string) {
	t.Helper()
	org := db.Org{Name: "Acme", Slug: "acme-" + sanitize(t.Name()) + "-" + newUUID()[:8]}
	require.NoError(t, s.Unscoped().Create(&org).Error)
	ctx := principal.WithPrincipal(context.Background(), principal.Principal{
		Kind:  principal.KindSession,
		OrgID: org.ID,
	})
	return ctx, org.ID
}

func sanitize(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '/' || c == ' ' {
			c = '-'
		}
		out = append(out, c)
	}
	return string(out)
}

func deps(s *db.Store) Deps {
	return Deps{Store: s, Pepper: testPepper}
}

func TestRegisterGetList(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	ctx, _ := orgCtx(t, s)
	svc := NewService(deps(s))

	n, err := svc.Register(ctx, RegisterRequest{
		Name:       "node-1",
		Origin:     "byo",
		Provider:   "generic",
		NodeTypeID: "small",
		Region:     "fsn1",
		Capacity:   db.NodeCapacity{VCPU: 2, RAMMB: 4096, DiskGB: 40},
		Labels:     map[string]string{"env": "test"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, n.ID)
	require.NotNil(t, n.AgentFingerprint)
	require.NotEmpty(t, *n.AgentFingerprint)
	require.NotNil(t, n.LastHeartbeat)
	require.Equal(t, "active", n.Status)
	require.Equal(t, db.NodeCapacity{VCPU: 2, RAMMB: 4096, DiskGB: 40}, n.CapacityDecoded())
	require.Equal(t, map[string]string{"env": "test"}, n.LabelMap())

	got, err := svc.Get(ctx, n.ID)
	require.NoError(t, err)
	require.Equal(t, n.ID, got.ID)
	require.Equal(t, "active", got.Status)

	all, err := svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)
}

func TestRegisterRequiresNameAndOrg(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	ctx, _ := orgCtx(t, s)
	svc := NewService(deps(s))

	_, err := svc.Register(ctx, RegisterRequest{})
	require.Error(t, err)

	// No org in context refuses on every registry call.
	bare := context.Background()
	_, err = svc.Register(bare, RegisterRequest{Name: "x"})
	require.ErrorIs(t, err, db.ErrNoOrg)
	_, err = svc.Get(bare, "whatever")
	require.ErrorIs(t, err, db.ErrNoOrg)
	_, err = svc.List(bare)
	require.ErrorIs(t, err, db.ErrNoOrg)
	_, err = svc.Heartbeat(bare, "whatever")
	require.ErrorIs(t, err, db.ErrNoOrg)
	_, err = svc.Update(bare, "whatever", NodePatch{})
	require.ErrorIs(t, err, db.ErrNoOrg)
	_, err = svc.Drain(bare, "whatever")
	require.ErrorIs(t, err, db.ErrNoOrg)
	_, err = svc.Resume(bare, "whatever")
	require.ErrorIs(t, err, db.ErrNoOrg)
	require.ErrorIs(t, svc.Delete(bare, "whatever"), db.ErrNoOrg)
}

func TestGetUnknownNode(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	ctx, _ := orgCtx(t, s)
	svc := NewService(deps(s))
	_, err := svc.Get(ctx, "missing")
	require.ErrorIs(t, err, ErrNodeNotFound)
}

func TestHeartbeatAssignsFingerprintAndTime(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	ctx, _ := orgCtx(t, s)
	svc := NewService(deps(s))

	n, err := svc.Register(ctx, RegisterRequest{Name: "hb"})
	require.NoError(t, err)

	// Clear fingerprint + heartbeat in the row, heartbeat must restore both.
	require.NoError(t, s.Unscoped().Model(&db.Node{}).Where("id = ?", n.ID).
		Updates(map[string]any{"agent_fingerprint": nil, "last_heartbeat": nil}).Error)

	before := time.Now().UTC().Add(-time.Second)
	got, err := svc.Heartbeat(ctx, n.ID)
	require.NoError(t, err)
	require.NotNil(t, got.AgentFingerprint)
	require.NotEmpty(t, *got.AgentFingerprint)
	require.NotNil(t, got.LastHeartbeat)
	require.True(t, !got.LastHeartbeat.Before(before))
	require.Equal(t, "active", got.Status)

	_, err = svc.Heartbeat(ctx, "missing")
	require.ErrorIs(t, err, ErrNodeNotFound)
}

func TestStalenessBoundary(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	ctx, _ := orgCtx(t, s)
	stale := time.Minute
	frozen := time.Now().UTC()
	svc := NewService(Deps{Store: s, Pepper: testPepper, StaleAfter: stale, Now: func() time.Time { return frozen }})

	n, err := svc.Register(ctx, RegisterRequest{Name: "stale"})
	require.NoError(t, err)

	// Exact boundary is pure computation (DB timestamps truncate sub-second
	// precision, so the boundary itself is asserted in memory).
	now := frozen
	at := now.Add(-stale)
	exact := db.Node{Status: "active", LastHeartbeat: &at}
	require.Equal(t, "active", svc.EffectiveStatus(exact))
	past := at.Add(-time.Nanosecond)
	over := db.Node{Status: "active", LastHeartbeat: &past}
	require.Equal(t, "offline", svc.EffectiveStatus(over))

	// DB-backed: fresh registration reads live; a long-silenced node reads
	// offline without mutating its stored row.
	got, err := svc.Get(ctx, n.ID)
	require.NoError(t, err)
	require.Equal(t, "active", got.Status)

	old := time.Now().UTC().Add(-(stale + 2*time.Second))
	require.NoError(t, s.Unscoped().Model(&db.Node{}).Where("id = ?", n.ID).
		Update("last_heartbeat", &old).Error)
	got, err = svc.Get(ctx, n.ID)
	require.NoError(t, err)
	require.Equal(t, "offline", got.Status)

	// Read did not mutate the row: stored status is still active.
	var raw db.Node
	require.NoError(t, s.Unscoped().Where("id = ?", n.ID).First(&raw).Error)
	require.Equal(t, "active", raw.Status)
}

func TestDrainResumeDelete(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	ctx, _ := orgCtx(t, s)
	svc := NewService(deps(s))

	n, err := svc.Register(ctx, RegisterRequest{Name: "drain"})
	require.NoError(t, err)

	d, err := svc.Drain(ctx, n.ID)
	require.NoError(t, err)
	require.True(t, d.Draining)
	require.Equal(t, "draining", d.Status)

	r, err := svc.Resume(ctx, n.ID)
	require.NoError(t, err)
	require.False(t, r.Draining)
	require.Equal(t, "active", r.Status)

	require.NoError(t, svc.Delete(ctx, n.ID))
	_, err = svc.Get(ctx, n.ID)
	require.ErrorIs(t, err, ErrNodeNotFound)
	require.ErrorIs(t, svc.Delete(ctx, n.ID), ErrNodeNotFound)
}

func TestCrossOrgIsolation(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	ctxA, _ := orgCtx(t, s)
	svc := NewService(deps(s))

	n, err := svc.Register(ctxA, RegisterRequest{Name: "iso"})
	require.NoError(t, err)

	// Second org sees nothing of the first.
	s2ctx, _ := orgCtx(t, s)
	_ = s2ctx
	ctxB := principal.WithPrincipal(context.Background(), principal.Principal{OrgID: "other-org-id"})
	_, err = svc.Get(ctxB, n.ID)
	require.ErrorIs(t, err, ErrNodeNotFound)
	all, err := svc.List(ctxB)
	require.NoError(t, err)
	require.Empty(t, all)
}

func TestUpdatePatch(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	ctx, _ := orgCtx(t, s)
	svc := NewService(deps(s))

	n, err := svc.Register(ctx, RegisterRequest{Name: "patch", Region: "a"})
	require.NoError(t, err)
	region := "b"
	cap := db.NodeCapacity{VCPU: 4, RAMMB: 8192, DiskGB: 80}
	labels := map[string]string{"tier": "hot"}
	got, err := svc.Update(ctx, n.ID, NodePatch{Region: &region, Capacity: &cap, Labels: &labels})
	require.NoError(t, err)
	require.Equal(t, "b", got.Region)
	require.Equal(t, cap, got.CapacityDecoded())
	require.Equal(t, labels, got.LabelMap())
	// Name untouched by a partial patch.
	require.Equal(t, "patch", got.Name)

	_, err = svc.Update(ctx, "missing", NodePatch{})
	require.ErrorIs(t, err, ErrNodeNotFound)
}

// stubSecrets resolves one pepper value through the keys constant path.
type stubSecrets struct{ v string }

func (s stubSecrets) Kind() string { return "stub" }
func (s stubSecrets) Get(_ context.Context, key string) (string, error) {
	if key == keys.JoinTokenSecret {
		return s.v, nil
	}
	return "", secrets.ErrNotFound
}

func (s stubSecrets) Health(_ context.Context) error { return nil }

func TestPepperFromSecretsProvider(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	ctx, _ := orgCtx(t, s)
	jt := NewJoinTokenService(Deps{Store: s, Secrets: stubSecrets{v: "stub-pepper-value"}})
	secret, _, err := jt.Issue(ctx, IssueRequest{Name: "t"})
	require.NoError(t, err)
	_, err = jt.Redeem(context.Background(), secret)
	require.NoError(t, err)
}
