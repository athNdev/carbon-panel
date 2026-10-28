// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package scheduler

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/operation"
)

func TestBackupJournalIsActiveBeforeTheArchiveStarts(t *testing.T) {
	s, server, sender := consistencyFixture(t)
	s.sender = sender
	ctx := context.Background()

	orig := beforeBackupArchive
	t.Cleanup(func() { beforeBackupArchive = orig })
	sawExecuting := false
	beforeBackupArchive = func() error {
		active, err := s.store.ListActiveServerOperations(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(active) != 1 || active[0].Kind != "backup" || active[0].Phase != storage.ServerOperationExecuting {
			t.Fatalf("active operations before archive = %+v", active)
		}
		if active[0].ActiveServer == nil || *active[0].ActiveServer != server.ID {
			t.Fatalf("backup lock = %v, want server %s", active[0].ActiveServer, server.ID)
		}
		sawExecuting = true
		return nil
	}

	if _, err := s.executeBackupTask(ctx, server, &storage.ScheduledTask{Name: "nightly"}); err != nil {
		t.Fatal(err)
	}
	if !sawExecuting {
		t.Fatal("archive started before the journal row was executing")
	}
	active, err := s.store.ListActiveServerOperations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("finished backup still holds %d lock(s)", len(active))
	}
	ops, err := s.store.ListServerOperations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || ops[0].Kind != "backup" || ops[0].Phase != storage.ServerOperationComplete {
		t.Fatalf("journal = %+v", ops)
	}
}

func TestBackupJournalFailureReleasesTheLockBeforeTheArchive(t *testing.T) {
	s, server, sender := consistencyFixture(t)
	s.sender = sender
	ctx := context.Background()

	orig := beforeBackupArchive
	t.Cleanup(func() { beforeBackupArchive = orig })
	beforeBackupArchive = func() error {
		return errors.New("archive interrupted")
	}

	_, err := s.executeBackupTask(ctx, server, &storage.ScheduledTask{Name: "nightly"})
	if err == nil || err.Error() != "archive interrupted" {
		t.Fatalf("backup error = %v", err)
	}
	if sender.sent("save-off") {
		t.Fatal("saves were paused after the journal refused the archive")
	}
	matches, globErr := filepath.Glob(filepath.Join(s.appConfig.Storage.BackupDir, "*", "*.zip"))
	if globErr != nil {
		t.Fatal(globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("archive files = %v", matches)
	}
	active, err := s.store.ListActiveServerOperations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("failed backup still holds %d lock(s)", len(active))
	}
	ops, err := s.store.ListServerOperations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || ops[0].Kind != "backup" || ops[0].Phase != storage.ServerOperationFailed {
		t.Fatalf("journal = %+v", ops)
	}
}

func TestBackupDoesNotStartWhenAJournalLockIsAlreadyHeld(t *testing.T) {
	s, server, sender := consistencyFixture(t)
	s.sender = sender
	ctx := context.Background()

	if _, _, err := s.store.BeginServerOperation(ctx, server.ID, server.NodeID, server.ContainerID, "restore-key", "restore", operation.Digest("restore", server)); err != nil {
		t.Fatal(err)
	}
	orig := beforeBackupArchive
	t.Cleanup(func() { beforeBackupArchive = orig })
	beforeBackupArchive = func() error {
		t.Fatal("backup mutated the world while another journal row owned the server")
		return nil
	}

	if _, err := s.executeBackupTask(ctx, server, &storage.ScheduledTask{Name: "nightly"}); err == nil {
		t.Fatal("backup started while a journal lock was held")
	}
	if sender.sent("save-off") {
		t.Fatal("saves were paused while a journal lock was held")
	}
	matches, err := filepath.Glob(filepath.Join(s.appConfig.Storage.BackupDir, "*", "*.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("archive files = %v", matches)
	}
	active, err := s.store.ListActiveServerOperations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].Kind != "restore" {
		t.Fatalf("active operations = %+v", active)
	}
}
