package audit

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	"github.com/stretchr/testify/require"
)

func openTestStore(t *testing.T) *db.Store {
	t.Helper()
	s, err := db.Open(db.Options{
		Driver:      "sqlite",
		DSN:         filepath.Join(t.TempDir(), "test.db"),
		AutoMigrate: true,
	})
	require.NoError(t, err)
	return s
}

func ctxFor(org, user string) context.Context {
	return principal.WithPrincipal(context.Background(), principal.Principal{
		Kind:   principal.KindSession,
		UserID: user,
		OrgID:  org,
		Role:   "owner",
	})
}

func TestAppendAndGetRoundTrip(t *testing.T) {
	t.Parallel()
	st := NewGormStore(openTestStore(t))
	ctx := ctxFor("org-a", "user-1")
	require.NoError(t, st.Append(ctx, Event{
		Action:       "nodes.delete",
		ResourceType: "nodes",
		ResourceID:   "node-1",
		Result:       ResultOK,
		Detail:       map[string]any{"reason": "retired"},
		IP:           "10.0.0.9",
	}))
	events, next, err := st.List(ctx, Filter{})
	require.NoError(t, err)
	require.Empty(t, next)
	require.Len(t, events, 1)
	got := events[0]
	require.Equal(t, "org-a", got.OrgID)
	require.Equal(t, "user-1", got.ActorUserID)
	require.Equal(t, "session", got.ActorKind)
	require.Equal(t, "nodes.delete", got.Action)
	require.Equal(t, "retired", got.Detail["reason"])

	byID, err := st.Get(ctx, got.ID)
	require.NoError(t, err)
	require.Equal(t, got.ID, byID.ID)
}

func TestAppendNoOrgRefuses(t *testing.T) {
	t.Parallel()
	st := NewGormStore(openTestStore(t))
	err := st.Append(context.Background(), Event{OrgID: "org-a", Action: "nodes.delete"})
	require.ErrorIs(t, err, db.ErrNoOrg)
}

func TestAppendNoActorRefuses(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	st := NewGormStore(s)
	// Org in context via principal, but anonymous: no attributable actor.
	ctx := principal.WithPrincipal(context.Background(), principal.Principal{OrgID: "org-a"})
	err := st.Append(ctx, Event{Action: "nodes.delete"})
	require.ErrorIs(t, err, ErrNoActor)
}

func TestAppendEmptyActionRefuses(t *testing.T) {
	t.Parallel()
	st := NewGormStore(openTestStore(t))
	require.ErrorIs(t, st.Append(ctxFor("org-a", "u1"), Event{}), ErrNoAction)
}

func TestOrgScoping(t *testing.T) {
	t.Parallel()
	st := NewGormStore(openTestStore(t))
	ctxA := ctxFor("org-a", "user-1")
	ctxB := ctxFor("org-b", "user-2")
	require.NoError(t, st.Append(ctxA, Event{Action: "nodes.delete"}))

	events, _, err := st.List(ctxB, Filter{})
	require.NoError(t, err)
	require.Empty(t, events, "org B must not read org A events")

	aEvents, _, err := st.List(ctxA, Filter{})
	require.NoError(t, err)
	require.Len(t, aEvents, 1)
	_, err = st.Get(ctxB, aEvents[0].ID)
	require.Error(t, err, "org B must not fetch org A event by id")

	_, _, err = st.List(ctxB, Filter{OrgID: "org-a"})
	require.ErrorIs(t, err, ErrOrgMismatch)
}

func TestAppendOnlyNoUpdateDeleteAPI(t *testing.T) {
	t.Parallel()
	iface := reflect.TypeOf((*Store)(nil)).Elem()
	require.Equal(t, 3, iface.NumMethod())
	names := []string{iface.Method(0).Name, iface.Method(1).Name, iface.Method(2).Name}
	require.ElementsMatch(t, []string{"Append", "List", "Get"}, names)
}

func TestListFiltersAndPaging(t *testing.T) {
	t.Parallel()
	st := NewGormStore(openTestStore(t))
	ctx := ctxFor("org-a", "user-1")
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		require.NoError(t, st.Append(ctx, Event{
			Action:    "nodes.delete",
			Result:    ResultOK,
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		}))
	}
	require.NoError(t, st.Append(ctx, Event{Action: "workloads.exec", Result: ResultError, CreatedAt: now.Add(5 * time.Second)}))

	byAction, _, err := st.List(ctx, Filter{Action: "workloads.exec"})
	require.NoError(t, err)
	require.Len(t, byAction, 1)

	byResult, _, err := st.List(ctx, Filter{Result: ResultError})
	require.NoError(t, err)
	require.Len(t, byResult, 1)

	byTime, _, err := st.List(ctx, Filter{Since: now.Add(3 * time.Second)})
	require.NoError(t, err)
	require.Len(t, byTime, 3) // i=3,4 plus the workloads.exec event

	page1, next, err := st.List(ctx, Filter{Limit: 2})
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.NotEmpty(t, next)
	page2, next2, err := st.List(ctx, Filter{Limit: 2, Cursor: next})
	require.NoError(t, err)
	require.Len(t, page2, 2)
	require.NotEmpty(t, next2)
	_, _, err = st.List(ctx, Filter{Cursor: "bogus"})
	require.Error(t, err)
}

func TestAppendRedactsOnWrite(t *testing.T) {
	t.Parallel()
	const fake = "FAKESECRET-write-path-123"
	st := NewGormStore(openTestStore(t))
	ctx := ctxFor("org-a", "user-1")
	require.NoError(t, st.Append(ctx, Event{
		Action: "apikeys.rotate",
		Detail: map[string]any{"api_key": fake},
	}))
	events, _, err := st.List(ctx, Filter{})
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.NotContains(t, events[0].Detail["api_key"], fake)
}

func TestNewEventDerivesActorFromPrincipal(t *testing.T) {
	t.Parallel()
	ctx := principal.WithPrincipal(context.Background(), principal.Principal{
		Kind:     principal.KindAPIKey,
		UserID:   "user-9",
		OrgID:    "org-z",
		APIKeyID: "key-1",
	})
	e := NewEvent(ctx, RequestMeta{
		Procedure: "/cloud.v1.NodeService/DeleteNode",
		Result:    ResultOK,
	})
	require.Equal(t, "org-z", e.OrgID)
	require.Equal(t, "user-9", e.ActorUserID)
	require.Equal(t, "key-1", e.ActorAPIKeyID)
	require.Equal(t, "nodes.delete", e.Action)
	require.False(t, e.CreatedAt.IsZero())
	require.NotEmpty(t, e.ID)
}
