// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package rbac

import (
	"fmt"
	"strings"
	"testing"

	"github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/internal/db"
)

func newTestEnforcer(t *testing.T, anonymousEnabled bool) *Enforcer {
	t.Helper()

	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	store, err := db.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{
			Path:           fmt.Sprintf("file:%s?mode=memory&cache=shared", dbName),
			AutoMigrate:    true,
			MaxConnections: 5,
		},
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	enforcer, err := NewEnforcer(store.DB())
	if err != nil {
		t.Fatalf("failed to create enforcer: %v", err)
	}
	if err := enforcer.SeedDefaultPolicies(anonymousEnabled); err != nil {
		t.Fatalf("failed to seed policies: %v", err)
	}
	return enforcer
}

func TestEnforce_AllowDenyMatrix(t *testing.T) {
	e := newTestEnforcer(t, false)

	cases := []struct {
		name     string
		roles    []string
		resource string
		action   string
		object   string
		want     bool
	}{
		{"admin wildcard", []string{"admin"}, ResourceServers, ActionDelete, "srv-1", true},
		{"admin unknown resource", []string{"admin"}, "nope", "nope", "x", true},
		{"user read server", []string{"user"}, ResourceServers, ActionRead, "srv-1", true},
		{"user start server", []string{"user"}, ResourceServers, ActionStart, "srv-1", true},
		{"user delete server denied", []string{"user"}, ResourceServers, ActionDelete, "srv-1", false},
		{"user manage users denied", []string{"user"}, ResourceUsers, ActionRead, "*", false},
		{"anonymous read server (seeds read-only)", []string{"anonymous"}, ResourceServers, ActionRead, "srv-1", true},
		{"anonymous start denied", []string{"anonymous"}, ResourceServers, ActionStart, "srv-1", false},
		{"unknown role denied", []string{"ghost"}, ResourceServers, ActionRead, "srv-1", false},
		{"no roles denied", nil, ResourceServers, ActionRead, "srv-1", false},
		{"unknown resource denied", []string{"user"}, "nope", ActionRead, "srv-1", false},
		{"multi-role first match wins", []string{"ghost", "user"}, ResourceServers, ActionRead, "srv-1", true},
		{"multi-role all denied", []string{"ghost", "nobody"}, ResourceServers, ActionDelete, "srv-1", false},
		{"object scoping wildcard", []string{"user"}, ResourceServers, ActionRead, "any-object-id", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.Enforce(tc.roles, tc.resource, tc.action, tc.object)
			if err != nil {
				t.Fatalf("Enforce returned error: %v", err)
			}
			if got != tc.want {
				t.Errorf("Enforce(%v, %q, %q, %q) = %v, want %v",
					tc.roles, tc.resource, tc.action, tc.object, got, tc.want)
			}
		})
	}
}

func TestEnforce_ObjectScoping(t *testing.T) {
	e := newTestEnforcer(t, false)

	// Restrict user to a single object, then verify scoping.
	if err := e.SetPermissionsForRole("user", []Permission{
		{Resource: ResourceServers, Action: ActionRead, ObjectID: "srv-allowed"},
	}); err != nil {
		t.Fatalf("SetPermissionsForRole failed: %v", err)
	}

	allowed, err := e.Enforce([]string{"user"}, ResourceServers, ActionRead, "srv-allowed")
	if err != nil || !allowed {
		t.Errorf("scoped object allowed = %v, %v; want true, nil", allowed, err)
	}
	denied, err := e.Enforce([]string{"user"}, ResourceServers, ActionRead, "srv-other")
	if err != nil || denied {
		t.Errorf("other object allowed = %v, %v; want false, nil", denied, err)
	}
}

func TestSetPermissionsForRole_AdminImmutable(t *testing.T) {
	e := newTestEnforcer(t, false)

	if err := e.SetPermissionsForRole("admin", nil); err == nil {
		t.Fatal("expected error modifying admin role, got nil")
	}
	if err := e.SetPermissionsForRole("ADMIN", nil); err == nil {
		t.Fatal("expected error modifying ADMIN role (case-insensitive), got nil")
	}

	// Admin still omnipotent afterwards.
	ok, err := e.Enforce([]string{"admin"}, "anything", "anything", "anything")
	if err != nil || !ok {
		t.Errorf("admin allowed = %v, %v; want true, nil", ok, err)
	}
}

func TestSetPermissionsForRole_RoundTrip(t *testing.T) {
	e := newTestEnforcer(t, false)

	want := []Permission{
		{Resource: ResourceServers, Action: ActionRead, ObjectID: "*"},
		{Resource: ResourceServers, Action: ActionStart, ObjectID: "srv-1"},
		{Resource: ResourceServers, Action: ActionStop}, // empty object normalizes to *
	}
	if err := e.SetPermissionsForRole("operator", want); err != nil {
		t.Fatalf("SetPermissionsForRole failed: %v", err)
	}

	got := e.GetPermissionsForRole("operator")
	if len(got) != len(want) {
		t.Fatalf("got %d permissions, want %d", len(got), len(want))
	}
	byKey := make(map[string]bool, len(got))
	for _, p := range got {
		byKey[p.Resource+"/"+p.Action+"/"+p.ObjectID] = true
		if p.ObjectID == "" {
			t.Errorf("permission not normalized: %+v", p)
		}
	}
	for _, p := range want {
		obj := p.ObjectID
		if obj == "" {
			obj = "*"
		}
		if !byKey[p.Resource+"/"+p.Action+"/"+obj] {
			t.Errorf("missing permission %+v (normalized)", p)
		}
	}

	// Unknown role reads back empty, never nil-panics.
	if perms := e.GetPermissionsForRole("ghost"); len(perms) != 0 {
		t.Errorf("unknown role permissions = %v, want empty", perms)
	}
}

func TestGetPermissionMatrix_CoversSeededRoles(t *testing.T) {
	e := newTestEnforcer(t, false)

	matrix := e.GetPermissionMatrix()
	for _, role := range []string{"admin", "user", "anonymous"} {
		perms, ok := matrix[role]
		if !ok || len(perms) == 0 {
			t.Errorf("matrix missing role %q", role)
		}
	}

	// Seeding twice must not duplicate policies.
	if err := e.SeedDefaultPolicies(false); err != nil {
		t.Fatalf("reseed failed: %v", err)
	}
	reseeded := e.GetPermissionMatrix()
	for role, perms := range matrix {
		if len(reseeded[role]) != len(perms) {
			t.Errorf("role %q count changed after reseed: %d -> %d", role, len(perms), len(reseeded[role]))
		}
	}
}
