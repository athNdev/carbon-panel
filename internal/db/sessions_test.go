// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package db

import (
	"context"
	"testing"
	"time"
)

func TestSession_CRUD(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	sess := &Session{
		ID: "sess-1", UserID: "user-1", Token: "tok-1",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := store.CreateSession(ctx, sess); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if sess.ID == "" {
		t.Error("ID not defaulted")
	}

	got, err := store.GetSession(ctx, "tok-1")
	if err != nil || got.UserID != "user-1" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := store.GetSession(ctx, "missing"); err == nil {
		t.Error("missing session fetched")
	}

	// Expired sessions read back as missing.
	expired := &Session{
		ID: "sess-exp", UserID: "user-1", Token: "tok-exp",
		ExpiresAt: time.Now().Add(-time.Minute),
	}
	if err := store.CreateSession(ctx, expired); err != nil {
		t.Fatalf("create expired failed: %v", err)
	}
	if _, err := store.GetSession(ctx, "tok-exp"); err == nil {
		t.Error("expired session fetched")
	}

	if err := store.DeleteSession(ctx, "tok-1"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := store.GetSession(ctx, "tok-1"); err == nil {
		t.Error("deleted session fetched")
	}
}

func TestDeleteUserSessionsExcept(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	mk := func(id, user, token string) {
		if err := store.CreateSession(ctx, &Session{
			ID: id, UserID: user, Token: token,
			ExpiresAt: time.Now().Add(time.Hour),
		}); err != nil {
			t.Fatalf("create %s failed: %v", id, err)
		}
	}
	mk("s1", "u1", "t1")
	mk("s2", "u1", "t2")
	mk("s3", "u2", "t3")

	if err := store.DeleteUserSessionsExcept(ctx, "u1", "t2"); err != nil {
		t.Fatalf("delete-except failed: %v", err)
	}
	if _, err := store.GetSession(ctx, "t1"); err == nil {
		t.Error("u1/t1 survived")
	}
	if _, err := store.GetSession(ctx, "t2"); err != nil {
		t.Errorf("excepted session removed: %v", err)
	}
	if _, err := store.GetSession(ctx, "t3"); err != nil {
		t.Errorf("other user session removed: %v", err)
	}

	// Empty exceptToken removes everything for the user.
	if err := store.DeleteUserSessionsExcept(ctx, "u1", ""); err != nil {
		t.Fatalf("delete-all failed: %v", err)
	}
	if _, err := store.GetSession(ctx, "t2"); err == nil {
		t.Error("u1/t2 survived delete-all")
	}
}

func TestCleanSessions(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	mk := func(id, token string, expired bool) {
		exp := time.Now().Add(time.Hour)
		if expired {
			exp = time.Now().Add(-time.Hour)
		}
		if err := store.CreateSession(ctx, &Session{
			ID: id, UserID: "u", Token: token, ExpiresAt: exp,
		}); err != nil {
			t.Fatalf("create %s failed: %v", id, err)
		}
	}
	mk("old", "t-old", true)
	mk("fresh", "t-fresh", false)

	if err := store.CleanExpiredSessions(ctx); err != nil {
		t.Fatalf("clean expired failed: %v", err)
	}
	if _, err := store.GetSession(ctx, "t-old"); err == nil {
		t.Error("expired session survives sweep")
	}
	if _, err := store.GetSession(ctx, "t-fresh"); err != nil {
		t.Errorf("fresh session swept: %v", err)
	}

	if err := store.CleanAllSessions(ctx); err != nil {
		t.Fatalf("clean all failed: %v", err)
	}
	if _, err := store.GetSession(ctx, "t-fresh"); err == nil {
		t.Error("session survives clean-all")
	}
}
