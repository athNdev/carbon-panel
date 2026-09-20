package db

import (
	"context"
	"testing"
)

func TestSubuserCRUD(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()
	ctx := context.Background()

	srv := &Server{ID: "srv-sub", Name: "sub-test", DataPath: t.TempDir()}
	if err := store.CreateServer(ctx, srv); err != nil {
		t.Fatalf("create server: %v", err)
	}
	user := &User{ID: "u-sub", Username: "sub", AuthProvider: "local"}
	if err := store.CreateUser(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := store.UpsertSubuser(ctx, "srv-sub", "u-sub", []string{"servers.start", "servers.read"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := store.UpsertSubuser(ctx, "srv-sub", "u-sub", []string{"no-dot"}); err == nil {
		t.Fatalf("invalid permission accepted")
	}
	if err := store.UpsertSubuser(ctx, "nope", "u-sub", []string{"servers.read"}); err == nil {
		t.Fatalf("unknown server accepted")
	}
	perms := store.SubuserPermissions(ctx, "srv-sub", "u-sub")
	if len(perms) != 2 || perms[0] != "servers.start" {
		t.Fatalf("unexpected perms: %v", perms)
	}
	list, err := store.ListSubusers(ctx, "srv-sub")
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %+v", err, list)
	}
	if err := store.RemoveSubuser(ctx, "srv-sub", "u-sub"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if perms := store.SubuserPermissions(ctx, "srv-sub", "u-sub"); len(perms) != 0 {
		t.Fatalf("grant survived removal: %v", perms)
	}
}
