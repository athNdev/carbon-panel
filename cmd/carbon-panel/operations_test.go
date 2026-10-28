// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
)

func TestOperationsListAndResolveDoNotRequireDocker(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{
			Path:        filepath.Join(t.TempDir(), "ops.db"),
			AutoMigrate: true,
		},
		Docker: config.DockerConfig{Host: "unix:///var/run/docker.sock"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	op, _, err := store.BeginServerOperation(ctx, "srv-1", "node-1", "cont-1", "key-1", "start", "digest")
	if err != nil {
		t.Fatal(err)
	}

	var listed bytes.Buffer
	if err := runOperationsCommand(ctx, store, "list", "", "", "", &listed); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listed.String(), op.ID) || !strings.Contains(listed.String(), "\tyes\t") {
		t.Fatalf("list = %q", listed.String())
	}

	var resolved bytes.Buffer
	if err := runOperationsCommand(ctx, store, "resolve", op.ID, "checked the container by hand", "", &resolved); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resolved.String(), "without changing containers") {
		t.Fatalf("resolve = %q", resolved.String())
	}
	if err := store.AssertNoServerOperation(ctx, "srv-1"); err != nil {
		t.Fatal(err)
	}

	var again bytes.Buffer
	if err := runOperationsCommand(ctx, store, "resolve", op.ID, "", "", &again); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(again.String(), "already released") {
		t.Fatalf("second resolve = %q", again.String())
	}
	if err := runOperationsCommand(ctx, store, "resolve", "", "", "", &bytes.Buffer{}); err == nil {
		t.Fatal("expected resolve without an id to fail")
	}
}

func TestReleasePortKeepsALiveServerAndItsWorld(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{
			Path:        filepath.Join(t.TempDir(), "ops.db"),
			AutoMigrate: true,
		},
		Docker: config.DockerConfig{Host: "unix:///var/run/docker.sock"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	world := t.TempDir()
	marker := filepath.Join(world, "world.dat")
	if err := os.WriteFile(marker, []byte("world"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateServer(ctx, &storage.Server{
		ID: "live", Name: "Live", ModLoader: storage.ModLoaderVanilla, MCVersion: "1.21",
		NodeID: "node-a", Status: storage.StatusRunning, Port: 25566, DataPath: world,
		ContainerID: "still-running",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().Create(&storage.BackendPortLease{NodeID: "node-a", Port: 25566, ServerID: "live"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB().Create(&storage.BackendPortLease{NodeID: "node-a", Port: 25580, ServerID: "gone"}).Error; err != nil {
		t.Fatal(err)
	}

	var listed bytes.Buffer
	if err := runOperationsCommand(ctx, store, "ports", "", "", "", &listed); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listed.String(), "node-a\t25566\tlive\tyes") || !strings.Contains(listed.String(), "node-a\t25580\tgone\tno") {
		t.Fatalf("ports = %q", listed.String())
	}

	if err := runOperationsCommand(ctx, store, "release-port", "", "", "live", &bytes.Buffer{}); err == nil {
		t.Fatal("release-port removed ports for a server that still exists")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("world stat = %v", err)
	}
	if _, err := store.GetServer(ctx, "live"); err != nil {
		t.Fatal(err)
	}

	var released bytes.Buffer
	if err := runOperationsCommand(ctx, store, "release-port", "", "", "gone", &released); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(released.String(), "node-a:25580") || !strings.Contains(released.String(), "without stopping a container or deleting files") {
		t.Fatalf("release = %q", released.String())
	}
	var left []storage.BackendPortLease
	if err := store.DB().Order("port").Find(&left).Error; err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].ServerID != "live" || left[0].Port != 25566 {
		t.Fatalf("leases = %+v", left)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("world stat after release = %v", err)
	}
	if err := runOperationsCommand(ctx, store, "release-port", "", "", "", &bytes.Buffer{}); err == nil {
		t.Fatal("expected release-port without a server id to fail")
	}
}
