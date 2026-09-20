package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	appconfig "github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
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
