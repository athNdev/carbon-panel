// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package scheduler

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/internal/backupcrypt"
	appconfig "github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

// fakeWorldSender records console commands and injects quiesce failures.
type fakeWorldSender struct {
	mu          sync.Mutex
	cmds        []string
	saveOffFail bool
	flushFail   bool
	saveOnFail  bool
}

func (f *fakeWorldSender) SendCommand(_ context.Context, _, command string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cmds = append(f.cmds, command)
	switch {
	case command == "save-off" && f.saveOffFail:
		return "", errFakeCommand
	case command == "save-all flush" && f.flushFail:
		return "", errFakeCommand
	case command == "save-on" && f.saveOnFail:
		return "", errFakeCommand
	}
	return "ok", nil
}

func (f *fakeWorldSender) sent(command string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.cmds {
		if c == command {
			return true
		}
	}
	return false
}

var errFakeCommand = errors.New("rcon command failed")

func consistencyFixture(t *testing.T) (*Scheduler, *storage.Server, *fakeWorldSender) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.NewSQLiteStore(&appconfig.Config{
		Database: appconfig.DatabaseConfig{Path: filepath.Join(dir, "panel.sqlite")},
	})
	if err != nil {
		t.Fatal(err)
	}
	requireNoErr(t, store.Migrate())
	t.Cleanup(func() { _ = store.Close() })

	dataPath := filepath.Join(dir, "servers", "srv-data")
	if err := os.MkdirAll(filepath.Join(dataPath, "world"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataPath, "world", "level.dat"), []byte("level"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := &Scheduler{
		store: store,
		appConfig: &appconfig.Config{
			Storage: appconfig.StorageConfig{
				DataDir:   dir,
				BackupDir: filepath.Join(dir, "backups"),
			},
		},
		log: logger.New(),
	}
	requireNoErr(t, store.CreateNode(context.Background(), &storage.Node{
		ID: "node-1", Name: "node-1", Host: "unix:///var/run/docker.sock", Enabled: true, IsLocal: true,
	}))
	// A running server: quiesce rules apply.
	server := &storage.Server{ID: "srv-1", NodeID: "node-1", DataPath: dataPath,
		Status: storage.StatusRunning, ContainerID: "c-1"}
	return s, server, &fakeWorldSender{}
}

func requireNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// TestBackupTaskConsistencyVerifiedQuiesced pins the honest consistency
// label: a verified save-off plus flush may claim "verified quiesced", and
// saves are re-enabled afterwards (OSS-235).
func TestBackupTaskConsistencyVerifiedQuiesced(t *testing.T) {
	s, server, sender := consistencyFixture(t)
	s.sender = sender

	out, err := s.executeBackupTask(context.Background(), server, &storage.ScheduledTask{Name: "nightly"})
	requireNoErr(t, err)
	if !strings.Contains(out, "consistency: verified quiesced") {
		t.Fatalf("output must claim verified quiesced, got %q", out)
	}
	if !sender.sent("save-off") || !sender.sent("save-all flush") || !sender.sent("save-on") {
		t.Fatalf("quiesce commands missing: %v", sender.cmds)
	}
}

// TestBackupTaskConsistencyCrashConsistentOnFlushFailure: a failed flush is
// never silently reported as consistent.
func TestBackupTaskConsistencyCrashConsistentOnFlushFailure(t *testing.T) {
	s, server, sender := consistencyFixture(t)
	sender.flushFail = true
	s.sender = sender

	out, err := s.executeBackupTask(context.Background(), server, &storage.ScheduledTask{Name: "nightly"})
	requireNoErr(t, err)
	if !strings.Contains(out, "consistency: crash-consistent") {
		t.Fatalf("failed flush must be labeled crash-consistent, got %q", out)
	}
	if strings.Contains(out, "verified quiesced") {
		t.Fatalf("failed flush must not claim quiesced, got %q", out)
	}
}

// TestBackupTaskConsistencyCrashConsistentOnSaveOffFailure: when saves cannot
// be frozen at all the archive is crash-consistent and no save-on is sent.
func TestBackupTaskConsistencyCrashConsistentOnSaveOffFailure(t *testing.T) {
	s, server, sender := consistencyFixture(t)
	sender.saveOffFail = true
	s.sender = sender

	out, err := s.executeBackupTask(context.Background(), server, &storage.ScheduledTask{Name: "nightly"})
	requireNoErr(t, err)
	if !strings.Contains(out, "consistency: crash-consistent") {
		t.Fatalf("failed save-off must be labeled crash-consistent, got %q", out)
	}
	if sender.sent("save-on") {
		t.Fatal("save-on must not be sent when saves were never disabled")
	}
}

// TestBackupTaskResumeFailureIsReported: a world left in save-off is never
// silent (OSS-235).
func TestBackupTaskResumeFailureIsReported(t *testing.T) {
	s, server, sender := consistencyFixture(t)
	sender.saveOnFail = true
	s.sender = sender

	out, err := s.executeBackupTask(context.Background(), server, &storage.ScheduledTask{Name: "nightly"})
	requireNoErr(t, err)
	if !strings.Contains(out, "WARNING") || !strings.Contains(out, "still OFF") {
		t.Fatalf("failed resume must be reported in the output, got %q", out)
	}
}

// TestBackupTaskEncryptsStoredArtifact pins the runtime wiring (OSS-236
// repro): a backup through the public task path stores a non-plain-zip
// artifact and records its key metadata.
func TestBackupTaskEncryptsStoredArtifact(t *testing.T) {
	s, server, _ := consistencyFixture(t)
	s.appConfig.Storage.BackupEncryption = appconfig.BackupEncryptionConfig{
		KeyID: "key-2026",
		Key:   strings.Repeat("ab", 32),
	}

	out, err := s.executeBackupTask(context.Background(), server, &storage.ScheduledTask{Name: "nightly"})
	requireNoErr(t, err)
	if !strings.Contains(out, "encrypted (key key-2026)") {
		t.Fatalf("output must report the encryption state, got %q", out)
	}

	recs, err := s.store.ListBackupRecords(context.Background(), server.ID, 0)
	if err != nil || len(recs) != 1 {
		t.Fatalf("expected one backup record, got %v, %v", recs, err)
	}
	rec := recs[0]
	if !rec.Encrypted || rec.EncryptionKeyID != "key-2026" {
		t.Fatalf("record must carry encryption metadata, got %+v", rec)
	}
	stored, err := os.ReadFile(rec.Path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(stored, []byte("PK")) {
		t.Fatal("stored artifact must not be a plain zip")
	}
	isEnc, err := backupcrypt.IsEncrypted(rec.Path)
	if err != nil || !isEnc {
		t.Fatalf("stored artifact must carry the encryption magic, got %v, %v", isEnc, err)
	}
}

// TestPruneKeepsLastVerifiedCopy: retention must never remove the last
// verified copy even when newer records exist but are not usable (OSS-236).
func TestPruneKeepsLastVerifiedCopy(t *testing.T) {
	s, server, _ := consistencyFixture(t)
	ctx := context.Background()

	good := &storage.BackupRecord{
		ID: "b-good", ServerID: server.ID, Name: "good.zip", Path: filepath.Join(t.TempDir(), "good.zip"),
		Status: "complete", CreatedAt: time.Now().AddDate(0, 0, -30),
	}
	if err := s.store.CreateBackupRecord(ctx, good); err != nil {
		t.Fatal(err)
	}
	failed := &storage.BackupRecord{
		ID: "b-failed", ServerID: server.ID, Name: "failed.zip", Path: "",
		Status: "failed", CreatedAt: time.Now(),
	}
	if err := s.store.CreateBackupRecord(ctx, failed); err != nil {
		t.Fatal(err)
	}

	if _, err := s.pruneServerBackups(ctx, server, t.TempDir(), "", 1, 0, 0); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if _, err := s.store.GetBackupRecord(ctx, "b-good"); err != nil {
		t.Fatalf("the last verified copy must survive retention: %v", err)
	}
}
