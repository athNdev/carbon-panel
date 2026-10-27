// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/internal/auth"
	"github.com/athNdev/carbon-panel/internal/backupcrypt"
	appconfig "github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/rbac"
	"github.com/athNdev/carbon-panel/pkg/files"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func restoreFixture(t *testing.T) (*BackupService, *storage.Store, string, string) {
	t.Helper()
	store := setupTestStore(t)
	t.Cleanup(func() { _ = store.Close() })

	svc := NewBackupService(store, nil, nil, appconfig.S3Config{}, logger.New())
	enforcer, err := rbac.NewEnforcer(store.DB())
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	_ = enforcer.SeedDefaultPolicies(false)
	svc.SetEnforcer(enforcer)

	serverDataDir := t.TempDir()
	serverID := "srv-restore-consistency"
	if err := store.CreateServer(context.Background(), &storage.Server{
		ID:       serverID,
		Name:     "restore-consistency",
		DataPath: serverDataDir,
		NodeID:   "local",
	}); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	return svc, store, serverID, serverDataDir
}

func restoreContext(t *testing.T) context.Context {
	t.Helper()
	return auth.WithUser(context.Background(), &auth.AuthenticatedUser{ID: "u-admin", Username: "admin", Roles: []string{"admin"}})
}

// TestRestoreBackupLegacyZipPurgesStaleFiles pins the clean-restore rule at
// the caller seam (OSS-235): files removed between backup and restore never
// survive a successful restore, and the response reports verified recovery
// evidence rather than bare success.
func TestRestoreBackupLegacyZipPurgesStaleFiles(t *testing.T) {
	svc, store, serverID, serverDataDir := restoreFixture(t)
	ctx := restoreContext(t)

	// The live tree carries a stale file that is not in the archive.
	if err := os.WriteFile(filepath.Join(serverDataDir, "stale.txt"), []byte("left over"), 0o644); err != nil {
		t.Fatal(err)
	}

	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "server.properties"), []byte("motd=Carbon\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	backupDir := t.TempDir()
	archive := filepath.Join(backupDir, "clean.zip")
	if _, err := files.CreateZipArchive([]string{"server.properties"}, srcDir, archive, true); err != nil {
		t.Fatal(err)
	}
	sum, err := files.SHA256File(archive)
	if err != nil {
		t.Fatal(err)
	}
	rec := newBackupRecord("b-clean", serverID, archive)
	rec.SHA256 = sum
	if err := store.CreateBackupRecord(ctx, rec); err != nil {
		t.Fatal(err)
	}

	resp, err := svc.RestoreBackup(ctx, connect.NewRequest(&v1.RestoreBackupRequest{Id: "b-clean"}))
	if err != nil || !resp.Msg.Success {
		t.Fatalf("restore failed: %v %+v", err, resp)
	}
	if _, err := os.Stat(filepath.Join(serverDataDir, "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("stale file survived a successful restore, stat err = %v", err)
	}
	if !strings.Contains(resp.Msg.Message, "swapped in cleanly") || !strings.Contains(resp.Msg.Message, "world verified") {
		t.Fatalf("restore must report verified recovery evidence, got %q", resp.Msg.Message)
	}
}

// TestRestoreBackupEncryptedArtifact verifies the full pipeline (OSS-236
// repro): an encrypted artifact restores with the right key and is rejected
// with wrong or missing keys before any data mutation.
func TestRestoreBackupEncryptedArtifact(t *testing.T) {
	svc, store, serverID, serverDataDir := restoreFixture(t)
	ctx := restoreContext(t)

	key := strings.Repeat("ab", 32)
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "server.properties"), []byte("motd=Secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plainZip := filepath.Join(t.TempDir(), "plain.zip")
	if _, err := files.CreateZipArchive([]string{"server.properties"}, srcDir, plainZip, true); err != nil {
		t.Fatal(err)
	}

	backupDir := t.TempDir()
	artifact := filepath.Join(backupDir, "secret.zip")
	testKey, err := backupcrypt.ParseKey("key-2026", key)
	if err != nil {
		t.Fatal(err)
	}
	if err := backupcrypt.EncryptFile(plainZip, artifact, testKey); err != nil {
		t.Fatal(err)
	}
	sum, err := files.SHA256File(artifact)
	if err != nil {
		t.Fatal(err)
	}

	// Wrong and missing keys must be refused before any data mutation.
	rec := newBackupRecord("b-secret", serverID, artifact)
	rec.SHA256 = sum
	rec.Encrypted = true
	rec.EncryptionKeyID = "key-2026"
	if err := store.CreateBackupRecord(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(serverDataDir, "stale.txt"), []byte("untouched"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Missing key.
	_, err = svc.RestoreBackup(ctx, connect.NewRequest(&v1.RestoreBackupRequest{Id: "b-secret"}))
	if err == nil || connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("missing key must be refused with CodeFailedPrecondition, got %v", err)
	}
	assertUntouched := func(stage string) {
		t.Helper()
		data, rerr := os.ReadFile(filepath.Join(serverDataDir, "stale.txt"))
		if rerr != nil || string(data) != "untouched" {
			t.Fatalf("%s: world must be untouched, got %q, %v", stage, data, rerr)
		}
	}
	assertUntouched("missing key")

	// Wrong key.
	if err := svc.SetBackupEncryption("key-2026", strings.Repeat("cd", 32)); err != nil {
		t.Fatal(err)
	}
	_, err = svc.RestoreBackup(ctx, connect.NewRequest(&v1.RestoreBackupRequest{Id: "b-secret"}))
	if err == nil {
		t.Fatal("wrong key must be refused")
	}
	assertUntouched("wrong key")

	// Right key restores the world.
	if err := svc.SetBackupEncryption("key-2026", key); err != nil {
		t.Fatal(err)
	}
	resp, err := svc.RestoreBackup(ctx, connect.NewRequest(&v1.RestoreBackupRequest{Id: "b-secret"}))
	if err != nil || !resp.Msg.Success {
		t.Fatalf("restore with the right key failed: %v %+v", err, resp)
	}
	data, err := os.ReadFile(filepath.Join(serverDataDir, "server.properties"))
	if err != nil || string(data) != "motd=Secret\n" {
		t.Fatalf("restored content = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(serverDataDir, "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("stale file survived the encrypted restore, stat err = %v", err)
	}
}

// TestRestoreBackupRefusedWhileOperationOwnsWorld pins the shared
// coordination acceptance (OSS-235): no restore writes while another
// lifecycle mutation owns the world.
func TestRestoreBackupRefusedWhileOperationOwnsWorld(t *testing.T) {
	svc, store, serverID, _ := restoreFixture(t)
	ctx := restoreContext(t)

	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "server.properties"), []byte("motd=Carbon\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "clean.zip")
	if _, err := files.CreateZipArchive([]string{"server.properties"}, srcDir, archive, true); err != nil {
		t.Fatal(err)
	}
	sum, err := files.SHA256File(archive)
	if err != nil {
		t.Fatal(err)
	}
	rec := newBackupRecord("b-journaled", serverID, archive)
	rec.SHA256 = sum
	if err := store.CreateBackupRecord(ctx, rec); err != nil {
		t.Fatal(err)
	}

	// Another lifecycle mutation (e.g. a recreate) owns the world.
	if _, _, err := store.BeginServerOperation(ctx, serverID, "local", "c-1", "other-op", "recreate", "digest"); err != nil {
		t.Fatal(err)
	}

	_, err = svc.RestoreBackup(ctx, connect.NewRequest(&v1.RestoreBackupRequest{Id: "b-journaled"}))
	if err == nil {
		t.Fatal("restore must refuse to interleave with another lifecycle mutation")
	}
	if !strings.Contains(err.Error(), "restore") {
		t.Fatalf("refusal must name the operation, got %v", err)
	}
}
