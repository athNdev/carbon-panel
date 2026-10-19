package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/internal/auth"
	appconfig "github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/rbac"
	"github.com/athNdev/carbon-panel/pkg/files"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func newBackupRecord(id, serverID, path string) *storage.BackupRecord {
	return &storage.BackupRecord{ID: id, ServerID: serverID, Name: id + ".zip", Path: path, Status: "complete"}
}

func TestBackupService_ListDeleteLock(t *testing.T) {
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()
	ctx := context.Background()
	log := logger.New()
	svc := NewBackupService(store, nil, nil, appconfig.S3Config{}, log)
	// Backup delete/lock/restore are authorized against the OWNING server, so
	// the service needs an enforcer and an authenticated user in context.
	enforcer, err := rbac.NewEnforcer(store.DB())
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	if err := enforcer.SeedDefaultPolicies(false); err != nil {
		t.Fatalf("SeedDefaultPolicies: %v", err)
	}
	svc.SetEnforcer(enforcer)
	ctx = auth.WithUser(ctx, &auth.AuthenticatedUser{ID: "u-admin", Username: "admin", Roles: []string{"admin"}})

	dir := t.TempDir()
	archive := filepath.Join(dir, "srv_20200101-000000.zip")
	if err := os.WriteFile(archive, []byte("zip"), 0644); err != nil {
		t.Fatal(err)
	}
	serverID := "srv-bak"
	for _, id := range []string{"b1", "b2"} {
		if err := store.CreateBackupRecord(ctx, newBackupRecord(id, serverID, archive)); err != nil {
			t.Fatal(err)
		}
	}

	list, err := svc.ListBackups(ctx, connect.NewRequest(&v1.ListBackupsRequest{ServerId: serverID}))
	if err != nil || len(list.Msg.Backups) != 2 {
		t.Fatalf("list: %v %+v", err, list)
	}

	// Lock b1, then delete must fail.
	if _, err := svc.SetBackupLocked(ctx, connect.NewRequest(&v1.SetBackupLockedRequest{Id: "b1", Locked: true})); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if _, err := svc.DeleteBackup(ctx, connect.NewRequest(&v1.DeleteBackupRequest{Id: "b1"})); err == nil {
		t.Fatalf("locked backup deleted")
	}
	// Unlock and delete works, archive removed from disk.
	if _, err := svc.SetBackupLocked(ctx, connect.NewRequest(&v1.SetBackupLockedRequest{Id: "b1", Locked: false})); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if _, err := svc.DeleteBackup(ctx, connect.NewRequest(&v1.DeleteBackupRequest{Id: "b2"})); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Fatalf("archive not removed")
	}
	list, err = svc.ListBackups(ctx, connect.NewRequest(&v1.ListBackupsRequest{ServerId: serverID}))
	if err != nil || len(list.Msg.Backups) != 1 {
		t.Fatalf("expected 1 remaining, got %+v err=%v", list, err)
	}
}

func TestBackupService_DeleteBackup_RemoteKeyHandled(t *testing.T) {
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()
	ctx := context.Background()
	log := logger.New()

	s3cfg := appconfig.S3Config{
		Enabled:   true,
		Bucket:    "test-bucket",
		AccessKey: "test-ak",
		SecretKey: "test-sk",
	}

	svc := NewBackupService(store, nil, nil, s3cfg, log)
	enforcer, err := rbac.NewEnforcer(store.DB())
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	_ = enforcer.SeedDefaultPolicies(false)
	svc.SetEnforcer(enforcer)
	ctx = auth.WithUser(ctx, &auth.AuthenticatedUser{ID: "u-admin", Username: "admin", Roles: []string{"admin"}})

	dir := t.TempDir()
	archive := filepath.Join(dir, "remote_backup.zip")
	_ = os.WriteFile(archive, []byte("data"), 0644)

	rec := newBackupRecord("b-remote", "srv-1", archive)
	rec.RemoteKey = "srv-1/remote_backup.zip"
	if err := store.CreateBackupRecord(ctx, rec); err != nil {
		t.Fatal(err)
	}

	// Deletion succeeds locally even if remote S3 service is mocked/offline (fails soft with warning)
	resp, err := svc.DeleteBackup(ctx, connect.NewRequest(&v1.DeleteBackupRequest{Id: "b-remote"}))
	if err != nil || !resp.Msg.Success {
		t.Fatalf("expected delete to succeed, got %v", err)
	}
	if _, err := store.GetBackupRecord(ctx, "b-remote"); err == nil {
		t.Fatalf("record should be deleted from store")
	}
}

func TestBackupService_RestoreBackup_IntegrityVerification(t *testing.T) {
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()
	ctx := context.Background()
	log := logger.New()

	svc := NewBackupService(store, nil, nil, appconfig.S3Config{}, log)
	enforcer, err := rbac.NewEnforcer(store.DB())
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	_ = enforcer.SeedDefaultPolicies(false)
	svc.SetEnforcer(enforcer)
	ctx = auth.WithUser(ctx, &auth.AuthenticatedUser{ID: "u-admin", Username: "admin", Roles: []string{"admin"}})

	serverDataDir := t.TempDir()
	serverID := "srv-restore-test"
	if err := store.CreateServer(ctx, &storage.Server{
		ID:       serverID,
		Name:     "restore-test",
		DataPath: serverDataDir,
		NodeID:   "local",
	}); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}

	backupDir := t.TempDir()

	// Case A: Corrupted zip file
	corruptZip := filepath.Join(backupDir, "corrupt.zip")
	if err := os.WriteFile(corruptZip, []byte("not-a-valid-zip-content"), 0644); err != nil {
		t.Fatal(err)
	}
	recA := newBackupRecord("b-corrupt", serverID, corruptZip)
	if err := store.CreateBackupRecord(ctx, recA); err != nil {
		t.Fatal(err)
	}

	_, err = svc.RestoreBackup(ctx, connect.NewRequest(&v1.RestoreBackupRequest{Id: "b-corrupt"}))
	if err == nil {
		t.Fatal("expected corrupt archive to fail restore, got nil")
	}
	if connect.CodeOf(err) != connect.CodeDataLoss {
		t.Errorf("expected CodeDataLoss for corrupt archive, got %v", connect.CodeOf(err))
	}
	updatedA, err := store.GetBackupRecord(ctx, "b-corrupt")
	if err != nil || updatedA.Status != "failed" {
		t.Errorf("expected status failed, got %v (record: %+v)", err, updatedA)
	}

	// Case B: SHA256 mismatch
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "server.properties"), []byte("motd=Carbon\n"), 0644); err != nil {
		t.Fatal(err)
	}
	validZip := filepath.Join(backupDir, "valid.zip")
	if _, err := files.CreateZipArchive([]string{"server.properties"}, srcDir, validZip, true); err != nil {
		t.Fatal(err)
	}
	recB := newBackupRecord("b-bad-sha", serverID, validZip)
	recB.SHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	if err := store.CreateBackupRecord(ctx, recB); err != nil {
		t.Fatal(err)
	}

	_, err = svc.RestoreBackup(ctx, connect.NewRequest(&v1.RestoreBackupRequest{Id: "b-bad-sha"}))
	if err == nil {
		t.Fatal("expected SHA mismatch to fail restore, got nil")
	}
	if connect.CodeOf(err) != connect.CodeDataLoss {
		t.Errorf("expected CodeDataLoss for SHA mismatch, got %v", connect.CodeOf(err))
	}
	updatedB, err := store.GetBackupRecord(ctx, "b-bad-sha")
	if err != nil || updatedB.Status != "failed" {
		t.Errorf("expected status failed, got %v (record: %+v)", err, updatedB)
	}

	// Case C: Valid zip and matching SHA256
	realSum, err := files.SHA256File(validZip)
	if err != nil {
		t.Fatal(err)
	}
	recC := newBackupRecord("b-valid", serverID, validZip)
	recC.SHA256 = realSum
	if err := store.CreateBackupRecord(ctx, recC); err != nil {
		t.Fatal(err)
	}

	resp, err := svc.RestoreBackup(ctx, connect.NewRequest(&v1.RestoreBackupRequest{Id: "b-valid"}))
	if err != nil || !resp.Msg.Success {
		t.Fatalf("expected successful restore, got err=%v resp=%+v", err, resp)
	}
	updatedC, err := store.GetBackupRecord(ctx, "b-valid")
	if err != nil || updatedC.Status != "complete" {
		t.Errorf("expected status complete, got %v (record: %+v)", err, updatedC)
	}
	restoredFile := filepath.Join(serverDataDir, "server.properties")
	content, err := os.ReadFile(restoredFile)
	if err != nil || string(content) != "motd=Carbon\n" {
		t.Errorf("restored content = %q, err=%v", string(content), err)
	}
}

func TestBackupService_RestoreBackup_ConcurrencyAndRestoringStatus(t *testing.T) {
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()
	ctx := context.Background()
	log := logger.New()

	svc := NewBackupService(store, nil, nil, appconfig.S3Config{}, log)
	enforcer, err := rbac.NewEnforcer(store.DB())
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	_ = enforcer.SeedDefaultPolicies(false)
	svc.SetEnforcer(enforcer)
	ctx = auth.WithUser(ctx, &auth.AuthenticatedUser{ID: "u-admin", Username: "admin", Roles: []string{"admin"}})

	serverDataDir := t.TempDir()
	serverID := "srv-concurrency-test"
	if err := store.CreateServer(ctx, &storage.Server{
		ID:       serverID,
		Name:     "concurrency-test",
		DataPath: serverDataDir,
		NodeID:   "local",
	}); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}

	backupDir := t.TempDir()
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "server.properties"), []byte("motd=Carbon\n"), 0644); err != nil {
		t.Fatal(err)
	}
	validZip := filepath.Join(backupDir, "valid.zip")
	if _, err := files.CreateZipArchive([]string{"server.properties"}, srcDir, validZip, true); err != nil {
		t.Fatal(err)
	}
	realSum, err := files.SHA256File(validZip)
	if err != nil {
		t.Fatal(err)
	}

	rec := newBackupRecord("b-restoring", serverID, validZip)
	rec.SHA256 = realSum
	rec.Status = "restoring"
	if err := store.CreateBackupRecord(ctx, rec); err != nil {
		t.Fatal(err)
	}

	// 1. Attempting to restore a backup that has Status == "restoring" should fail with CodeFailedPrecondition
	_, err = svc.RestoreBackup(ctx, connect.NewRequest(&v1.RestoreBackupRequest{Id: "b-restoring"}))
	if err == nil {
		t.Fatal("expected restore of 'restoring' backup to fail, got nil")
	}
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("expected CodeFailedPrecondition, got %v", connect.CodeOf(err))
	}

	// 2. Attempting to delete a backup that has Status == "restoring" should fail with CodeFailedPrecondition
	_, err = svc.DeleteBackup(ctx, connect.NewRequest(&v1.DeleteBackupRequest{Id: "b-restoring"}))
	if err == nil {
		t.Fatal("expected delete of 'restoring' backup to fail, got nil")
	}
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("expected CodeFailedPrecondition, got %v", connect.CodeOf(err))
	}

	// 3. Concurrent restore simulation: if another restore is actively in progress for serverID
	svc.restoringMu.Lock()
	svc.restoringServers[serverID] = true
	svc.restoringMu.Unlock()

	rec2 := newBackupRecord("b-concurrent", serverID, validZip)
	rec2.SHA256 = realSum
	rec2.Status = "complete"
	if err := store.CreateBackupRecord(ctx, rec2); err != nil {
		t.Fatal(err)
	}

	_, err = svc.RestoreBackup(ctx, connect.NewRequest(&v1.RestoreBackupRequest{Id: "b-concurrent"}))
	if err == nil {
		t.Fatal("expected concurrent restore to fail, got nil")
	}
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("expected CodeFailedPrecondition, got %v", connect.CodeOf(err))
	}

	svc.restoringMu.Lock()
	delete(svc.restoringServers, serverID)
	svc.restoringMu.Unlock()

	// Once unlocked, restore should succeed
	resp, err := svc.RestoreBackup(ctx, connect.NewRequest(&v1.RestoreBackupRequest{Id: "b-concurrent"}))
	if err != nil || !resp.Msg.Success {
		t.Fatalf("expected restore to succeed after release, got err=%v", err)
	}
}
