package auth

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/athNdev/carbon-panel/internal/config"
)

func TestRecoveryKeyStableAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.AuthConfig{
		JWTSecret:       "test-secret-value-0123456789",
		RecoveryKeyFile: filepath.Join(dir, "recovery.key"),
		Local:           config.LocalConfig{Enabled: true},
	}
	m1, store := newTestManager(t, cfg)
	key := m1.GetRecoveryKey()
	if key == "" || !m1.RecoveryKeyCreated() {
		t.Fatal("fresh manager must generate one recovery key")
	}
	body, err := os.ReadFile(cfg.RecoveryKeyFile)
	if err != nil {
		t.Fatalf("read key file: %v", err)
	}
	if string(body) != key {
		t.Fatalf("file key = %q, want %q", body, key)
	}

	m2, err := NewManager(store, nil, cfg)
	if err != nil {
		t.Fatalf("second NewManager: %v", err)
	}
	if m2.GetRecoveryKey() != key {
		t.Fatalf("restart changed recovery key")
	}
	if m2.RecoveryKeyCreated() {
		t.Fatal("restart must not report a newly generated key")
	}
	if err := m2.UseRecoveryKey(context.Background(), key); err != nil {
		t.Fatalf("stored key must still be accepted: %v", err)
	}
}

func TestRecoveryKeyAdoptsExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recovery.key")
	const existing = "abc123existingkey"
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.AuthConfig{
		JWTSecret:       "test-secret-value-0123456789",
		RecoveryKeyFile: path,
		Local:           config.LocalConfig{Enabled: true},
	}
	m, _ := newTestManager(t, cfg)
	if m.GetRecoveryKey() != existing {
		t.Fatalf("key = %q, want the on-disk key", m.GetRecoveryKey())
	}
	if m.RecoveryKeyCreated() {
		t.Fatal("adopting a file must not count as generation")
	}
}

func TestRotateRecoveryKeyInvalidatesOld(t *testing.T) {
	cfg := &config.AuthConfig{
		JWTSecret: "test-secret-value-0123456789",
		Local:     config.LocalConfig{Enabled: true},
	}
	m, _ := newTestManager(t, cfg)
	old := m.GetRecoveryKey()
	next, err := m.RotateRecoveryKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if next == "" || next == old {
		t.Fatalf("rotate returned %q", next)
	}
	if err := m.UseRecoveryKey(context.Background(), old); err == nil {
		t.Fatal("previous recovery key still accepted")
	}
	if err := m.UseRecoveryKey(context.Background(), next); err != nil {
		t.Fatalf("rotated key rejected: %v", err)
	}
}
