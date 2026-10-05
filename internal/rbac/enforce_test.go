package rbac

import (
	"fmt"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newTestEnforcer(t *testing.T, anonymous bool) *Enforcer {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", name)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	e, err := NewEnforcer(db)
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	if err := e.SeedDefaultPolicies(anonymous); err != nil {
		t.Fatalf("SeedDefaultPolicies: %v", err)
	}
	return e
}

func mustEnforce(t *testing.T, e *Enforcer, roles []string, resource, action, obj string, want bool) {
	t.Helper()
	got, err := e.Enforce(roles, resource, action, obj)
	if err != nil {
		t.Fatalf("Enforce(%v,%s,%s,%s): %v", roles, resource, action, obj, err)
	}
	if got != want {
		t.Fatalf("Enforce(%v,%s,%s,%s)=%v want %v", roles, resource, action, obj, got, want)
	}
}

func TestEnforceMatrix(t *testing.T) {
	e := newTestEnforcer(t, false)

	// Admin bypasses everything via wildcards.
	mustEnforce(t, e, []string{"admin"}, ResourceServers, ActionRead, "srv-1", true)
	mustEnforce(t, e, []string{"admin"}, "anything", "delete", "x", true)

	// User: lifecycle + reads allowed, admin-only denied.
	mustEnforce(t, e, []string{"user"}, ResourceServers, ActionRead, "srv-1", true)
	mustEnforce(t, e, []string{"user"}, ResourceServers, ActionStart, "srv-1", true)
	mustEnforce(t, e, []string{"user"}, ResourceServers, ActionStop, "srv-1", true)
	mustEnforce(t, e, []string{"user"}, ResourceServers, ActionCommand, "srv-1", true)
	mustEnforce(t, e, []string{"user"}, ResourceFiles, ActionRead, "srv-1", true)
	mustEnforce(t, e, []string{"user"}, ResourceServers, ActionRead, "other-id", true) // obj wildcard
	mustEnforce(t, e, []string{"user"}, ResourceServers, "delete", "srv-1", false)
	mustEnforce(t, e, []string{"user"}, ResourceUsers, "read", "srv-1", false)

	// Unknown role / resource / action deny.
	mustEnforce(t, e, []string{"ghost"}, ResourceServers, ActionRead, "srv-1", false)
	mustEnforce(t, e, []string{"user"}, "nope", ActionRead, "srv-1", false)
	mustEnforce(t, e, []string{"user"}, ResourceServers, "nope", "srv-1", false)
	mustEnforce(t, e, nil, ResourceServers, ActionRead, "srv-1", false)

	// Multi-role: first match wins.
	mustEnforce(t, e, []string{"ghost", "user"}, ResourceServers, ActionRead, "srv-1", true)
}

func TestEnforceAnonymousSeed(t *testing.T) {
	// SeedDefaultPolicies seeds the anonymous role the same either way; the
	// anonymous role only matters when the panel enables anonymous access.
	for _, anon := range []bool{false, true} {
		e := newTestEnforcer(t, anon)
		mustEnforce(t, e, []string{"anonymous"}, ResourceServers, ActionRead, "srv-1", true)
		mustEnforce(t, e, []string{"anonymous"}, ResourceServers, ActionStart, "srv-1", false)
	}
}

func TestPermissionMutationRoundTrip(t *testing.T) {
	e := newTestEnforcer(t, false)

	before := e.GetPermissionsForRole("user")
	if len(before) == 0 {
		t.Fatal("seeded user role must have permissions")
	}
	if got := e.GetPermissionsForRole("nobody"); len(got) != 0 {
		t.Fatalf("unknown role perms=%v", got)
	}

	// Replace with a single narrow grant.
	narrow := []Permission{{Resource: ResourceServers, Action: ActionRead, ObjectID: "srv-9"}}
	if err := e.SetPermissionsForRole("user", narrow); err != nil {
		t.Fatalf("SetPermissionsForRole: %v", err)
	}
	got := e.GetPermissionsForRole("user")
	if len(got) != 1 || got[0].ObjectID != "srv-9" {
		t.Fatalf("perms=%+v", got)
	}
	mustEnforce(t, e, []string{"user"}, ResourceServers, ActionRead, "srv-9", true)
	mustEnforce(t, e, []string{"user"}, ResourceServers, ActionRead, "srv-1", false)
	mustEnforce(t, e, []string{"user"}, ResourceServers, ActionStart, "srv-9", false)

	// Empty object defaults to wildcard.
	if err := e.SetPermissionsForRole("user", []Permission{{Resource: ResourceServers, Action: ActionRead}}); err != nil {
		t.Fatalf("SetPermissionsForRole: %v", err)
	}
	mustEnforce(t, e, []string{"user"}, ResourceServers, ActionRead, "any", true)

	// Admin role is immutable.
	if err := e.SetPermissionsForRole("admin", narrow); err == nil {
		t.Fatal("admin mutation must fail")
	}
	if err := e.SetPermissionsForRole("ADMIN", narrow); err == nil {
		t.Fatal("admin mutation must fail case-insensitively")
	}
}

func TestPermissionMatrix(t *testing.T) {
	e := newTestEnforcer(t, false)
	m := e.GetPermissionMatrix()
	for _, role := range []string{"admin", "user", "anonymous"} {
		if len(m[role]) == 0 {
			t.Fatalf("matrix missing %s: %v", role, m)
		}
	}
}

func TestSeedIdempotent(t *testing.T) {
	e := newTestEnforcer(t, false)
	n1 := len(e.GetPermissionsForRole("user"))
	if err := e.SeedDefaultPolicies(false); err != nil {
		t.Fatalf("reseed: %v", err)
	}
	if n2 := len(e.GetPermissionsForRole("user")); n2 != n1 {
		t.Fatalf("reseed duplicated policies: %d -> %d", n1, n2)
	}
}
