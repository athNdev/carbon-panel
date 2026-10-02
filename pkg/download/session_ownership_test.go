package download

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestInitSessionForRecordsOwnership pins the fields the stream handler now
// authorizes on. Without a server binding the handler can only enforce a
// global files:read permission.
func TestInitSessionForRecordsOwnership(t *testing.T) {
	m := newTestManager(t, time.Hour)

	path := filepath.Join(t.TempDir(), "f.zip")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := m.InitSessionFor(path, "f.zip", 1, false, "srv-1", "user-1")
	if s.ServerID != "srv-1" {
		t.Fatalf("ServerID = %q, want srv-1", s.ServerID)
	}
	if s.CreatorUserID != "user-1" {
		t.Fatalf("CreatorUserID = %q, want user-1", s.CreatorUserID)
	}

	got, err := m.GetSession(s.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.ServerID != "srv-1" || got.CreatorUserID != "user-1" {
		t.Fatalf("ownership lost through GetSession: %+v", got)
	}
}

// TestInitSessionLeavesOwnershipEmpty covers the legacy shim: sessions created
// through InitSession have no server binding, which the handler treats as
// "fall back to the global check" rather than hard-failing every pre-existing
// in-flight session.
func TestInitSessionLeavesOwnershipEmpty(t *testing.T) {
	m := newTestManager(t, time.Hour)

	path := filepath.Join(t.TempDir(), "f.zip")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := m.InitSession(path, "f.zip", 1, false)
	if s.ServerID != "" || s.CreatorUserID != "" {
		t.Fatalf("expected empty ownership, got server=%q user=%q", s.ServerID, s.CreatorUserID)
	}
	if s.ID == "" {
		t.Fatal("session id must be set")
	}
}

// TestSessionsAreIsolated guards that two sessions for different servers do not
// share an id or bleed ownership into each other.
func TestSessionsAreIsolated(t *testing.T) {
	m := newTestManager(t, time.Hour)

	dir := t.TempDir()
	p1 := filepath.Join(dir, "a.zip")
	p2 := filepath.Join(dir, "b.zip")
	for _, p := range []string{p1, p2} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	s1 := m.InitSessionFor(p1, "a.zip", 1, false, "srv-a", "user-a")
	s2 := m.InitSessionFor(p2, "b.zip", 1, false, "srv-b", "user-b")

	if s1.ID == s2.ID {
		t.Fatal("session ids must be unique")
	}

	g1, err := m.GetSession(s1.ID)
	if err != nil {
		t.Fatalf("GetSession s1: %v", err)
	}
	if g1.ServerID != "srv-a" {
		t.Fatalf("s1 ServerID = %q, want srv-a", g1.ServerID)
	}

	g2, err := m.GetSession(s2.ID)
	if err != nil {
		t.Fatalf("GetSession s2: %v", err)
	}
	if g2.ServerID != "srv-b" {
		t.Fatalf("s2 ServerID = %q, want srv-b", g2.ServerID)
	}
}
