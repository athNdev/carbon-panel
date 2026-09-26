// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package command

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

type fakeExecutor struct {
	output string
	err    error
	calls  int
	lastID string
}

func (f *fakeExecutor) ExecCommand(_ context.Context, containerID string, _ string) (string, error) {
	f.calls++
	f.lastID = containerID
	return f.output, f.err
}

func newSenderTestStore(t *testing.T) *storage.Store {
	t.Helper()

	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	store, err := storage.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{
			Path:           fmt.Sprintf("file:%s?mode=memory&cache=shared", dbName),
			AutoMigrate:    true,
			MaxConnections: 5,
		},
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return store
}

func TestSendCommand_MissingServer(t *testing.T) {
	s := NewSender(newSenderTestStore(t), &config.Config{}, &fakeExecutor{}, nil)

	if _, err := s.SendCommand(context.Background(), "nope", "list"); err == nil {
		t.Error("missing server: want error")
	}
}

func TestSendCommand_ServerWithoutContainer(t *testing.T) {
	store := newSenderTestStore(t)
	ctx := context.Background()

	if err := store.DB().Create(&storage.Server{ID: "srv-noc", Name: "noc", DataPath: t.TempDir()}).Error; err != nil {
		t.Fatalf("failed to insert server: %v", err)
	}
	s := NewSender(store, &config.Config{}, &fakeExecutor{}, nil)

	if _, err := s.SendCommand(ctx, "srv-noc", "list"); err == nil {
		t.Error("containerless server: want error")
	}
}

func TestSendCommand_RCONDisabledFallsBackToExec(t *testing.T) {
	store := newSenderTestStore(t)
	ctx := context.Background()

	if err := store.DB().Create(&storage.Server{ID: "srv-1", Name: "s1", DataPath: t.TempDir(), ContainerID: "cid-1"}).Error; err != nil {
		t.Fatalf("failed to insert server: %v", err)
	}
	disabled := false
	if err := store.DB().Create(&storage.ServerConfig{ServerID: "srv-1", EnableRCON: &disabled}).Error; err != nil {
		t.Fatalf("failed to insert config: %v", err)
	}

	exec := &fakeExecutor{output: "exec-output"}
	s := NewSender(store, &config.Config{}, exec, nil)

	out, err := s.SendCommand(ctx, "srv-1", "list")
	if err != nil {
		t.Fatalf("fallback exec failed: %v", err)
	}
	if out != "exec-output" || exec.calls != 1 || exec.lastID != "cid-1" {
		t.Errorf("exec = %q calls=%d id=%q", out, exec.calls, exec.lastID)
	}
}

func TestResolveExecutor_DefaultWithoutPool(t *testing.T) {
	exec := &fakeExecutor{}
	s := NewSender(newSenderTestStore(t), &config.Config{}, exec, nil)

	if got := s.resolveExecutor("any-node"); got != DockerExecutor(exec) {
		t.Error("nil pool must resolve to the default executor")
	}
}

func TestResolveExecutor_PoolFallsBackToDefault(t *testing.T) {
	store := newSenderTestStore(t)
	ctx := context.Background()

	if err := store.DB().Create(&storage.Server{ID: "srv-pool", Name: "p", DataPath: t.TempDir(), ContainerID: "cid-9"}).Error; err != nil {
		t.Fatalf("failed to insert server: %v", err)
	}
	disabled := false
	if err := store.DB().Create(&storage.ServerConfig{ServerID: "srv-pool", EnableRCON: &disabled}).Error; err != nil {
		t.Fatalf("failed to insert config: %v", err)
	}

	exec := &fakeExecutor{output: "pool-fallback"}
	pool := docker.NewClientPool(store, nil, logger.New())
	s := NewSender(store, &config.Config{}, exec, pool)

	out, err := s.SendCommand(ctx, "srv-pool", "list")
	if err != nil {
		t.Fatalf("fallback send failed: %v", err)
	}
	if out != "pool-fallback" || exec.calls != 1 || exec.lastID != "cid-9" {
		t.Errorf("exec = %q calls=%d id=%q", out, exec.calls, exec.lastID)
	}
}
