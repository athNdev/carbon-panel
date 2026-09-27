package download

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/pkg/logger"
)

func newTestManager(t *testing.T, ttl time.Duration) *Manager {
	t.Helper()
	m := NewManager(filepath.Join(t.TempDir(), "dl"), ttl, logger.New())
	t.Cleanup(m.Stop)
	return m
}

func TestInitAndGetSession(t *testing.T) {
	m := newTestManager(t, time.Minute)
	s := m.InitSession("/tmp/world.zip", "world.zip", 1234, false)
	if s.ID == "" {
		t.Fatal("session ID must not be empty")
	}
	if s.ExpiresAt.Before(time.Now()) {
		t.Fatal("session should expire in the future")
	}
	got, err := m.GetSession(s.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.FilePath != "/tmp/world.zip" || got.Filename != "world.zip" || got.TotalSize != 1234 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	// IDs are unique.
	other := m.InitSession("/tmp/world.zip", "world.zip", 1234, false)
	if other.ID == s.ID {
		t.Fatal("session IDs must be unique")
	}
}

func TestGetSessionNotFound(t *testing.T) {
	m := newTestManager(t, time.Minute)
	if _, err := m.GetSession("does-not-exist"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err=%v want ErrSessionNotFound", err)
	}
}

func TestGetSessionExpired(t *testing.T) {
	m := newTestManager(t, 20*time.Millisecond)
	s := m.InitSession("/tmp/world.zip", "world.zip", 10, false)
	time.Sleep(60 * time.Millisecond)
	if _, err := m.GetSession(s.ID); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("err=%v want ErrSessionExpired", err)
	}
	// Expired session is evicted: second read reports not-found.
	if _, err := m.GetSession(s.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err=%v want ErrSessionNotFound after eviction", err)
	}
}

func TestCleanupSessionDeletesFile(t *testing.T) {
	m := newTestManager(t, time.Minute)
	f := filepath.Join(t.TempDir(), "archive.zip")
	if err := os.WriteFile(f, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	s := m.InitSession(f, "archive.zip", 4, true)
	m.CleanupSession(s.ID)
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Fatal("DeleteAfter file should be removed on cleanup")
	}
	if _, err := m.GetSession(s.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err=%v want ErrSessionNotFound after cleanup", err)
	}
}

func TestCleanupSessionKeepsFileWhenNotDeleteAfter(t *testing.T) {
	m := newTestManager(t, time.Minute)
	f := filepath.Join(t.TempDir(), "keep.zip")
	if err := os.WriteFile(f, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	s := m.InitSession(f, "keep.zip", 4, false)
	m.CleanupSession(s.ID)
	if _, err := os.Stat(f); err != nil {
		t.Fatalf("file should be kept: %v", err)
	}
}

func TestCleanupUnknownIsNoop(t *testing.T) {
	m := newTestManager(t, time.Minute)
	m.CleanupSession("nope") // must not panic
}

func TestStopClearsSessionsAndDeletes(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(filepath.Join(dir, "dl"), time.Minute, logger.New())
	f := filepath.Join(dir, "tmp.zip")
	if err := os.WriteFile(f, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	s := m.InitSession(f, "tmp.zip", 4, true)
	m.Stop()
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Fatal("Stop should delete DeleteAfter files")
	}
	if _, err := m.GetSession(s.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err=%v want ErrSessionNotFound after Stop", err)
	}
}

func TestTempDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dl")
	m := NewManager(dir, time.Minute, logger.New())
	t.Cleanup(m.Stop)
	if m.TempDir() != dir {
		t.Fatalf("TempDir=%q want %q", m.TempDir(), dir)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("temp dir should exist: %v", err)
	}
}
