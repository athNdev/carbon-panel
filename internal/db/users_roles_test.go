package db

import (
	"context"
	"testing"
)

func TestUser_CRUD(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	if err := store.DB().Create(&User{ID: "u1", Username: "alice", AuthProvider: "local", IsActive: true}).Error; err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	if err := store.DB().Create(&User{ID: "u2", Username: "bob", AuthProvider: "local", IsActive: true}).Error; err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	users, err := store.ListUsers(ctx)
	if err != nil || len(users) != 2 {
		t.Fatalf("list = %d, %v", len(users), err)
	}
	n, err := store.CountUsers(ctx)
	if err != nil || n != 2 {
		t.Fatalf("count = %d, %v", n, err)
	}

	u, err := store.GetUser(ctx, "u1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	u.Username = "alice2"
	if err := store.UpdateUser(ctx, u); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	got, err := store.GetUser(ctx, "u1")
	if err != nil || got.Username != "alice2" {
		t.Fatalf("after update = %+v, %v", got, err)
	}

	if _, err := store.GetUserByOIDCSubject(ctx, "sub-1"); err == nil {
		t.Error("missing OIDC subject matched")
	}

	if err := store.DeleteUser(ctx, "u2"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if n, _ := store.CountUsers(ctx); n != 1 {
		t.Errorf("count after delete = %d", n)
	}
}

func TestRole_CRUD(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	if err := store.DB().Create(&Role{ID: "r1", Name: "operator"}).Error; err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	got, err := store.GetRole(ctx, "r1")
	if err != nil || got.Name != "operator" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := store.GetRole(ctx, "missing"); err == nil {
		t.Error("missing role fetched")
	}

	listed, err := store.ListRoles(ctx)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	found := false
	for _, r := range listed {
		if r.ID == "r1" {
			found = true
		}
	}
	if !found {
		t.Errorf("created role missing from list of %d", len(listed))
	}

	defs, err := store.GetDefaultRoles(ctx)
	if err != nil {
		t.Fatalf("defaults failed: %v", err)
	}
	_ = defs

	got.Description = "ops team"
	if err := store.UpdateRole(ctx, got); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	after, err := store.GetRole(ctx, "r1")
	if err != nil || after.Description != "ops team" {
		t.Fatalf("after update = %+v, %v", got, err)
	}

	if err := store.DeleteRole(ctx, "r1"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := store.GetRole(ctx, "r1"); err == nil {
		t.Error("deleted role fetched")
	}
}

func TestUserRole_AssignUnassign(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	if err := store.DB().Create(&User{ID: "u1", Username: "alice", AuthProvider: "local", IsActive: true}).Error; err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	if err := store.AssignRole(ctx, "u1", "admin", "test"); err != nil {
		t.Fatalf("assign failed: %v", err)
	}
	// Idempotent re-assign.
	if err := store.AssignRole(ctx, "u1", "admin", "test"); err != nil {
		t.Fatalf("re-assign failed: %v", err)
	}
	names, err := store.GetUserRoleNames(ctx, "u1")
	if err != nil || len(names) != 1 || names[0] != "admin" {
		t.Fatalf("roles = %v, %v", names, err)
	}

	if err := store.AssignRole(ctx, "u1", "user", "test"); err != nil {
		t.Fatalf("second assign failed: %v", err)
	}
	names, err = store.GetUserRoleNames(ctx, "u1")
	if err != nil || len(names) != 2 {
		t.Fatalf("roles = %v, %v", names, err)
	}

	if err := store.UnassignRole(ctx, "u1", "admin"); err != nil {
		t.Fatalf("unassign failed: %v", err)
	}
	names, err = store.GetUserRoleNames(ctx, "u1")
	if err != nil || len(names) != 1 || names[0] != "user" {
		t.Fatalf("after unassign = %v, %v", names, err)
	}
	// Unknown user has no roles, not an error.
	names, err = store.GetUserRoleNames(ctx, "ghost")
	if err != nil || len(names) != 0 {
		t.Fatalf("ghost roles = %v, %v", names, err)
	}
}
