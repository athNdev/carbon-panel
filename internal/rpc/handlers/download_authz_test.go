package handlers

import (
	"testing"

	"github.com/athNdev/carbon-panel/internal/auth"
	"github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/rbac"
	"github.com/athNdev/carbon-panel/pkg/download"
)

func newDownloadTestEnforcer(t *testing.T, perms []rbac.Permission) *rbac.Enforcer {
	t.Helper()
	store, err := db.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{
			Path:           "file:download-authz?mode=memory&cache=shared",
			AutoMigrate:    true,
			MaxConnections: 5,
		},
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	e, err := rbac.NewEnforcer(store.DB())
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	if err := e.SeedDefaultPolicies(false); err != nil {
		t.Fatalf("SeedDefaultPolicies: %v", err)
	}
	if len(perms) > 0 {
		if err := e.SetPermissionsForRole("dl-user", perms); err != nil {
			t.Fatalf("SetPermissionsForRole: %v", err)
		}
	}
	return e
}

// TestAuthorizeDownloadIsScopedToSessionServer is the regression test for the
// download stream authorization.
//
// The handler serves bytes straight off disk using only the session id. It
// previously enforced files:read against "*", so any user holding that
// permission anywhere could fetch any download session they learned the id of.
func TestAuthorizeDownloadIsScopedToSessionServer(t *testing.T) {
	e := newDownloadTestEnforcer(t, []rbac.Permission{
		{Resource: rbac.ResourceFiles, Action: rbac.ActionRead, ObjectID: "srv-mine"},
	})

	user := &auth.AuthenticatedUser{ID: "u1", Username: "u1", Roles: []string{"dl-user"}}

	t.Run("own server allowed", func(t *testing.T) {
		s := &download.Session{ID: "s1", ServerID: "srv-mine"}
		if !authorizeDownload(e, user, s) {
			t.Fatal("expected the owning server's download to be allowed")
		}
	})

	t.Run("other server denied", func(t *testing.T) {
		s := &download.Session{ID: "s2", ServerID: "srv-theirs"}
		if authorizeDownload(e, user, s) {
			t.Fatal("cross-server download was allowed")
		}
	})

	t.Run("legacy session uses the global check, not a bypass", func(t *testing.T) {
		// A session created before sessions carried a server id falls back to
		// the global ("*") permission. That is NOT a bypass: a role scoped to a
		// single server does not satisfy "*", so this user is denied. Only a
		// role holding a wildcard grant (e.g. admin) may fetch a legacy
		// session.
		legacy := &download.Session{ID: "s3"}
		if authorizeDownload(e, user, legacy) {
			t.Fatal("a server-scoped role must not pass the global legacy check")
		}
		admin := &auth.AuthenticatedUser{ID: "u2", Username: "u2", Roles: []string{"admin"}}
		if !authorizeDownload(e, admin, legacy) {
			t.Fatal("a wildcard role should still be able to fetch a legacy session")
		}
	})
}

// TestAuthorizeDownloadFailsSafe covers the degenerate inputs.
func TestAuthorizeDownloadFailsSafe(t *testing.T) {
	e := newDownloadTestEnforcer(t, nil)
	admin := &auth.AuthenticatedUser{ID: "a", Username: "a", Roles: []string{"admin"}}

	t.Run("nil enforcer denies", func(t *testing.T) {
		if authorizeDownload(nil, admin, &download.Session{ServerID: "s"}) {
			t.Fatal("a nil enforcer must not authorize")
		}
	})
	t.Run("nil user denies", func(t *testing.T) {
		if authorizeDownload(e, nil, &download.Session{ServerID: "s"}) {
			t.Fatal("a nil user must not authorize")
		}
	})
	t.Run("nil session denies", func(t *testing.T) {
		if authorizeDownload(e, admin, nil) {
			t.Fatal("a nil session must not authorize")
		}
	})
	t.Run("admin allowed", func(t *testing.T) {
		if !authorizeDownload(e, admin, &download.Session{ServerID: "any"}) {
			t.Fatal("admin should be authorized")
		}
	})
}
