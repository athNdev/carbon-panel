// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package download

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/pkg/logger"
)

func TestDownloadSession_Lifecycle(t *testing.T) {
	m := NewManager(t.TempDir(), time.Hour, logger.New())
	defer m.Stop()

	if got := m.TempDir(); got == "" {
		t.Error("empty temp dir")
	}

	file := filepath.Join(t.TempDir(), "payload.zip")
	if err := os.WriteFile(file, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := m.InitSession(file, "payload.zip", 7, false)
	if sess.ID == "" || sess.TotalSize != 7 {
		t.Errorf("session = %+v", sess)
	}

	got, err := m.GetSession(sess.ID)
	if err != nil || got.Filename != "payload.zip" {
		t.Errorf("get = %+v, %v", got, err)
	}
	if _, err := m.GetSession("missing"); err != ErrSessionNotFound {
		t.Errorf("missing = %v, want ErrSessionNotFound", err)
	}

	m.CleanupSession(sess.ID)
	if _, err := m.GetSession(sess.ID); err != ErrSessionNotFound {
		t.Errorf("after cleanup = %v, want ErrSessionNotFound", err)
	}
	// File stays: DeleteAfter was false.
	if _, err := os.Stat(file); err != nil {
		t.Errorf("source file removed: %v", err)
	}
	// Unknown cleanup is a safe no-op.
	m.CleanupSession("missing")
}

func TestDownloadSession_ExpiryAndDeleteAfter(t *testing.T) {
	m := NewManager(t.TempDir(), time.Hour, logger.New())
	defer m.Stop()

	file := filepath.Join(t.TempDir(), "ephemeral.zip")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := m.InitSession(file, "ephemeral.zip", 1, true)
	sess.ExpiresAt = time.Now().Add(-time.Minute)

	if _, err := m.GetSession(sess.ID); err != ErrSessionExpired {
		t.Errorf("expired = %v, want ErrSessionExpired", err)
	}

	m.CleanupSession(sess.ID)
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("delete-after file survives: %v", err)
	}
}

func TestCleanupExpired_RemovesStaleKeepsFresh(t *testing.T) {
	m := NewManager(t.TempDir(), time.Hour, logger.New())
	defer m.Stop()

	staleFile := filepath.Join(t.TempDir(), "stale.zip")
	if err := os.WriteFile(staleFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := m.InitSession(staleFile, "stale.zip", 1, true)
	stale.ExpiresAt = time.Now().Add(-time.Minute)

	fresh := m.InitSession(staleFile, "fresh.zip", 1, false)
	fresh.ExpiresAt = time.Now().Add(time.Hour)

	m.cleanupExpired()

	if _, err := m.GetSession(stale.ID); err != ErrSessionExpired && err != ErrSessionNotFound {
		t.Errorf("stale session = %v", err)
	}
	if _, err := m.GetSession(fresh.ID); err != nil {
		t.Errorf("fresh session swept: %v", err)
	}
	if _, err := os.Stat(staleFile); !os.IsNotExist(err) {
		t.Error("delete-after file survives expiry sweep")
	}
}
