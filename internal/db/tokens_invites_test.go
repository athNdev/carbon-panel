package db

import (
	"context"
	"testing"
)

func TestAPIToken_CRUD(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	if err := store.DB().Create(&User{ID: "u1", Username: "alice", AuthProvider: "local", IsActive: true}).Error; err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	tok := &APIToken{ID: "tok-1", UserID: "u1", Name: "ci", TokenHash: "hash-1"}
	if err := store.CreateAPIToken(ctx, tok); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	got, err := store.GetAPITokenByHash(ctx, "hash-1")
	if err != nil || got.Name != "ci" {
		t.Fatalf("by hash = %+v, %v", got, err)
	}
	if _, err := store.GetAPITokenByHash(ctx, "missing"); err == nil {
		t.Error("missing hash matched")
	}

	listed, err := store.ListAPITokensByUser(ctx, "u1")
	if err != nil || len(listed) != 1 {
		t.Fatalf("list = %v, %v", listed, err)
	}
	listed, err = store.ListAPITokensByUser(ctx, "ghost")
	if err != nil || len(listed) != 0 {
		t.Fatalf("ghost list = %v, %v", listed, err)
	}

	if err := store.UpdateAPITokenLastUsed(ctx, "tok-1"); err != nil {
		t.Fatalf("touch failed: %v", err)
	}
	got, err = store.GetAPITokenByHash(ctx, "hash-1")
	if err != nil || got.LastUsedAt == nil {
		t.Fatalf("last used not stamped: %+v, %v", got, err)
	}

	// Scoped delete refuses other users' tokens.
	if err := store.DeleteAPIToken(ctx, "tok-1", "u2"); err == nil {
		t.Error("cross-user delete succeeded")
	}
	if _, err := store.GetAPITokenByHash(ctx, "hash-1"); err != nil {
		t.Errorf("token removed by cross-user delete: %v", err)
	}

	if err := store.CreateAPIToken(ctx, &APIToken{ID: "tok-2", UserID: "u1", Name: "x", TokenHash: "hash-2"}); err != nil {
		t.Fatalf("recreate failed: %v", err)
	}
	if err := store.DeleteAPIToken(ctx, "tok-2", "u1"); err != nil {
		t.Fatalf("owner delete failed: %v", err)
	}
	if _, err := store.GetAPITokenByHash(ctx, "hash-2"); err == nil {
		t.Error("deleted token fetched")
	}

	if err := store.DeleteAPITokenByID(ctx, "missing"); err != nil {
		t.Errorf("delete-by-id missing failed: %v", err)
	}
}

func TestRegistrationInvite_CRUD(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	inv := &RegistrationInvite{
		ID: "inv-1", Code: "JOIN-123", Description: "friends",
		Roles: []string{"user"}, CreatedBy: "u1",
	}
	if err := store.CreateRegistrationInvite(ctx, inv); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	got, err := store.GetRegistrationInvite(ctx, "inv-1")
	if err != nil || got.Code != "JOIN-123" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := store.GetRegistrationInvite(ctx, "missing"); err == nil {
		t.Error("missing invite fetched")
	}

	got, err = store.GetRegistrationInviteByCode(ctx, "JOIN-123")
	if err != nil || got.ID != "inv-1" {
		t.Fatalf("by code = %+v, %v", got, err)
	}
	if _, err := store.GetRegistrationInviteByCode(ctx, "NOPE"); err == nil {
		t.Error("missing code matched")
	}

	listed, err := store.ListRegistrationInvites(ctx)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list = %v, %v", listed, err)
	}
}

func TestRole_CreateDirect(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	if err := store.CreateRole(ctx, &Role{ID: "r1", Name: "custom"}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	got, err := store.GetRole(ctx, "r1")
	if err != nil || got.Name != "custom" {
		t.Fatalf("get = %+v, %v", got, err)
	}
}
