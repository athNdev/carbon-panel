// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package scheduler

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/google/uuid"
)

func writeTestZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	w := zip.NewWriter(f)
	for name, content := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatalf("zip entry: %v", err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("file close: %v", err)
	}
}

func TestVerifyZipArchive(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.zip")
	writeTestZip(t, good, map[string]string{"world/level.dat": "data", "server.properties": "x=1"})
	if err := verifyZipArchive(good); err != nil {
		t.Fatalf("valid archive rejected: %v", err)
	}
	if err := verifyZipArchive(filepath.Join(dir, "missing.zip")); err == nil {
		t.Fatalf("missing archive accepted")
	}
	bad := filepath.Join(dir, "bad.zip")
	if err := os.WriteFile(bad, []byte("not a zip"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := verifyZipArchive(bad); err == nil {
		t.Fatalf("corrupt archive accepted")
	}
}

func TestSha256File(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	if err := os.WriteFile(p, []byte("abc"), 0644); err != nil {
		t.Fatal(err)
	}
	sum, err := sha256File(p)
	if err != nil {
		t.Fatal(err)
	}
	if sum != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("unexpected sha256: %s", sum)
	}
}

func TestPruneBackupsLockedExempt(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-72 * time.Hour)
	mk := func(name string, age time.Duration) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		ts := base.Add(age)
		if err := os.Chtimes(p, ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	mk("srv_20200101-000000.zip", 0)
	mk("srv_20200102-000000.zip", time.Hour)
	mk("srv_20200103-000000.locked.zip", 2*time.Hour)
	n, err := pruneBackups(dir, "srv_", 1, 1, 1)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 pruned, got %d", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "srv_20200103-000000.locked.zip")); err != nil {
		t.Fatalf("locked backup pruned: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "srv_20200102-000000.zip")); err != nil {
		t.Fatalf("minBackups survivor pruned: %v", err)
	}
}

func TestPruneBackupsLockedNamesAndSha256Cleaned(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-72 * time.Hour)
	mk := func(name string, age time.Duration) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p+".sha256", []byte("sum"), 0644); err != nil {
			t.Fatal(err)
		}
		ts := base.Add(age)
		if err := os.Chtimes(p, ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	mk("srv_1.zip", 0)
	mk("srv_2.zip", time.Hour)
	mk("srv_3.zip", 2*time.Hour)

	locked := map[string]bool{"srv_1.zip": true}
	n, err := pruneBackups(dir, "srv_", 1, 1, 1, locked)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 pruned, got %d", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "srv_1.zip")); err != nil {
		t.Fatalf("locked backup pruned: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "srv_1.zip.sha256")); err != nil {
		t.Fatalf("locked backup sha256 removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "srv_2.zip")); !os.IsNotExist(err) {
		t.Fatalf("expected srv_2.zip to be pruned")
	}
	if _, err := os.Stat(filepath.Join(dir, "srv_2.zip.sha256")); !os.IsNotExist(err) {
		t.Fatalf("expected srv_2.zip.sha256 to be pruned")
	}
}

func TestPruneServerBackups_StoreAndFiles(t *testing.T) {
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()
	dir := t.TempDir()
	serverID := uuid.New().String()
	server := &storage.Server{ID: serverID, Name: "test-server", DataPath: filepath.Join(dir, "data")}
	if err := store.CreateServer(context.Background(), server); err != nil {
		t.Fatal(err)
	}

	appCfg := &config.Config{}
	s := NewScheduler(store, nil, nil, appCfg, nil, logger.New())

	ctx := context.Background()
	now := time.Now()

	// 1. Locked backup (oldest)
	p1 := filepath.Join(dir, "srv_1.zip")
	_ = os.WriteFile(p1, []byte("zip1"), 0644)
	_ = os.WriteFile(p1+".sha256", []byte("sum1"), 0644)
	r1 := &storage.BackupRecord{
		ID: "b1", ServerID: serverID, Name: "srv_1.zip", Path: p1,
		Status: "complete", Locked: true, CreatedAt: now.Add(-3 * time.Hour),
	}
	if err := store.CreateBackupRecord(ctx, r1); err != nil {
		t.Fatal(err)
	}

	// 2. Unlocked old backup
	p2 := filepath.Join(dir, "srv_2.zip")
	_ = os.WriteFile(p2, []byte("zip2"), 0644)
	_ = os.WriteFile(p2+".sha256", []byte("sum2"), 0644)
	r2 := &storage.BackupRecord{
		ID: "b2", ServerID: serverID, Name: "srv_2.zip", Path: p2,
		Status: "complete", Locked: false, CreatedAt: now.Add(-2 * time.Hour),
	}
	if err := store.CreateBackupRecord(ctx, r2); err != nil {
		t.Fatal(err)
	}

	// 3. Unlocked newer backup
	p3 := filepath.Join(dir, "srv_3.zip")
	_ = os.WriteFile(p3, []byte("zip3"), 0644)
	_ = os.WriteFile(p3+".sha256", []byte("sum3"), 0644)
	r3 := &storage.BackupRecord{
		ID: "b3", ServerID: serverID, Name: "srv_3.zip", Path: p3,
		Status: "complete", Locked: false, CreatedAt: now.Add(-1 * time.Hour),
	}
	if err := store.CreateBackupRecord(ctx, r3); err != nil {
		t.Fatal(err)
	}

	// 4. Unlocked newest backup
	p4 := filepath.Join(dir, "srv_4.zip")
	_ = os.WriteFile(p4, []byte("zip4"), 0644)
	_ = os.WriteFile(p4+".sha256", []byte("sum4"), 0644)
	r4 := &storage.BackupRecord{
		ID: "b4", ServerID: serverID, Name: "srv_4.zip", Path: p4,
		Status: "complete", Locked: false, CreatedAt: now,
	}
	if err := store.CreateBackupRecord(ctx, r4); err != nil {
		t.Fatal(err)
	}

	// Prune with maxBackups = 2 (unlocked). Since there are 3 unlocked (b2, b3, b4),
	// b2 should be pruned. b1 is locked and must survive.
	pruned, err := s.pruneServerBackups(ctx, server, dir, "srv_", 0, 1, 2)
	if err != nil {
		t.Fatalf("pruneServerBackups: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("expected 1 pruned, got %d", pruned)
	}

	// b1 must still exist
	if _, err := store.GetBackupRecord(ctx, "b1"); err != nil {
		t.Errorf("locked record b1 was pruned from store: %v", err)
	}
	if _, err := os.Stat(p1); err != nil {
		t.Errorf("locked file p1 was pruned: %v", err)
	}

	// b2 must be deleted from store and disk
	if _, err := store.GetBackupRecord(ctx, "b2"); err == nil {
		t.Errorf("expected b2 to be pruned from store")
	}
	if _, err := os.Stat(p2); !os.IsNotExist(err) {
		t.Errorf("expected p2 to be removed from disk")
	}
	if _, err := os.Stat(p2 + ".sha256"); !os.IsNotExist(err) {
		t.Errorf("expected p2.sha256 to be removed from disk")
	}

	// b3 and b4 must survive
	if _, err := store.GetBackupRecord(ctx, "b3"); err != nil {
		t.Errorf("b3 was pruned from store: %v", err)
	}
	if _, err := store.GetBackupRecord(ctx, "b4"); err != nil {
		t.Errorf("b4 was pruned from store: %v", err)
	}
}

func TestPruneServerBackups_S3Only_DeleteLocalCopy(t *testing.T) {
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()
	dir := t.TempDir()
	serverID := uuid.New().String()
	server := &storage.Server{ID: serverID, Name: "s3-server", DataPath: filepath.Join(dir, "data")}
	if err := store.CreateServer(context.Background(), server); err != nil {
		t.Fatal(err)
	}

	appCfg := &config.Config{}
	s := NewScheduler(store, nil, nil, appCfg, nil, logger.New())

	ctx := context.Background()
	now := time.Now()

	// 3 S3-only backups with empty Path (DeleteLocalCopy=true)
	for i := 1; i <= 3; i++ {
		r := &storage.BackupRecord{
			ID: fmt.Sprintf("s3-b%d", i), ServerID: serverID,
			Name: fmt.Sprintf("srv_%d.zip", i), Path: "",
			RemoteKey: fmt.Sprintf("backups/%s/srv_%d.zip", serverID, i),
			Status: "complete", Locked: false,
			CreatedAt: now.Add(time.Duration(-4+i) * time.Hour),
		}
		if err := store.CreateBackupRecord(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	// Prune with maxBackups = 2
	pruned, err := s.pruneServerBackups(ctx, server, dir, "srv_", 0, 1, 2)
	if err != nil {
		t.Fatalf("pruneServerBackups: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("expected 1 pruned, got %d", pruned)
	}

	// Oldest s3-b1 should be pruned from store
	if _, err := store.GetBackupRecord(ctx, "s3-b1"); err == nil {
		t.Errorf("expected s3-b1 to be pruned from store")
	}
	// Newer s3-b2 and s3-b3 must survive
	if _, err := store.GetBackupRecord(ctx, "s3-b2"); err != nil {
		t.Errorf("s3-b2 should survive: %v", err)
	}
	if _, err := store.GetBackupRecord(ctx, "s3-b3"); err != nil {
		t.Errorf("s3-b3 should survive: %v", err)
	}
}
