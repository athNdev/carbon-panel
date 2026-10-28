// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

// TestFileServiceListFilesUsesMappedNodeStorage pins the ADR 0005 mapping at
// the file surface: a remote node on shared storage lists through the
// controller's mount of that storage, never through the node-side path.
func TestFileServiceListFilesUsesMappedNodeStorage(t *testing.T) {
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()
	ctx := context.Background()

	mount := filepath.Join(t.TempDir(), "shared")
	mapped := filepath.Join(mount, "srv-data")
	if err := os.MkdirAll(filepath.Join(mapped, "world"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mapped, "server.properties"), []byte("motd=hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mapped, "world", "level.dat"), []byte("level"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := store.CreateNode(ctx, &db.Node{ID: "remote-1", Name: "remote-1", Host: "tcp://10.0.0.5:2375", Enabled: true, StorageRoot: mount}); err != nil {
		t.Fatal(err)
	}
	server := &db.Server{ID: "srv-1", Name: "srv-1", DataPath: filepath.Join("srv", "data", "servers", "srv-data"), NodeID: "remote-1"}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}

	svc := NewFileService(store, nil, nil, nil, logger.New())
	t.Cleanup(svc.Stop)

	resp, err := svc.ListFiles(ctx, connect.NewRequest(&v1.ListFilesRequest{ServerId: server.ID, Path: "."}))
	if err != nil {
		t.Fatalf("ListFiles through mapped root failed: %v", err)
	}
	names := map[string]bool{}
	for _, f := range resp.Msg.Files {
		names[f.Name] = true
	}
	if !names["server.properties"] || !names["world"] {
		t.Fatalf("listing must come from the mapped root, got %v", names)
	}
}

// TestFileServiceListFilesRefusesUnreachableRemoteNode pins the fail-closed
// rule at the same surface: a remote node without reachable storage is a
// typed refusal, not a listing of some controller-local path.
func TestFileServiceListFilesRefusesUnreachableRemoteNode(t *testing.T) {
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()
	ctx := context.Background()

	if err := store.CreateNode(ctx, &db.Node{ID: "remote-bare", Name: "remote-bare", Host: "tcp://10.0.0.6:2375", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	server := &db.Server{ID: "srv-2", Name: "srv-2", DataPath: filepath.Join("srv", "data", "servers", "bare"), NodeID: "remote-bare"}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}

	svc := NewFileService(store, nil, nil, nil, logger.New())
	t.Cleanup(svc.Stop)

	_, err := svc.ListFiles(ctx, connect.NewRequest(&v1.ListFilesRequest{ServerId: server.ID, Path: "."}))
	if err == nil {
		t.Fatal("expected refusal for remote node without reachable storage")
	}
	connectErr, ok := err.(*connect.Error)
	if !ok || connectErr.Code() != connect.CodeUnavailable {
		t.Fatalf("expected CodeUnavailable, got %v", err)
	}
}

func TestGetFileReadsTheMappedRootWhenTheControllerCopyIsASymlink(t *testing.T) {
	store := setupTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	dir := t.TempDir()
	mount := filepath.Join(dir, "mount")
	mapped := filepath.Join(mount, "srv-data")
	if err := os.MkdirAll(mapped, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mapped, "server.properties"), []byte("from-mount"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(dir, "outside", "secret.txt")
	if err := os.MkdirAll(filepath.Dir(outside), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	controller := filepath.Join(dir, "controller", "srv-data")
	if err := os.MkdirAll(controller, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(controller, "server.properties")); err != nil {
		t.Fatal(err)
	}

	if err := store.CreateNode(ctx, &db.Node{ID: "remote-1", Name: "remote-1", Host: "tcp://10.0.0.5:2375", Enabled: true, StorageRoot: mount}); err != nil {
		t.Fatal(err)
	}
	server := &db.Server{ID: "srv-get", Name: "srv-get", DataPath: controller, NodeID: "remote-1"}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	svc := NewFileService(store, nil, nil, nil, logger.New())
	t.Cleanup(svc.Stop)

	resp, err := svc.GetFile(ctx, connect.NewRequest(&v1.GetFileRequest{ServerId: server.ID, Path: "server.properties"}))
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if string(resp.Msg.Content) != "from-mount" {
		t.Fatalf("content = %q, want the mapped world", resp.Msg.Content)
	}

	if _, err := svc.UpdateFile(ctx, connect.NewRequest(&v1.UpdateFileRequest{
		ServerId: server.ID, Path: "server.properties", Content: []byte("updated-mount"),
	})); err != nil {
		t.Fatalf("UpdateFile: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(mapped, "server.properties"))
	if err != nil || string(body) != "updated-mount" {
		t.Fatalf("mapped file = %q, %v", body, err)
	}
	secret, err := os.ReadFile(outside)
	if err != nil || string(secret) != "secret" {
		t.Fatalf("controller symlink target = %q, %v", secret, err)
	}
}

func TestGetFileRefusesAnUnreachableRemoteBeforeTheControllerCopy(t *testing.T) {
	store := setupTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	dir := t.TempDir()
	outside := filepath.Join(dir, "outside", "secret.txt")
	if err := os.MkdirAll(filepath.Dir(outside), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	controller := filepath.Join(dir, "controller", "srv-data")
	if err := os.MkdirAll(controller, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(controller, "server.properties")); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateNode(ctx, &db.Node{ID: "remote-bare", Name: "remote-bare", Host: "tcp://10.0.0.6:2375", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	server := &db.Server{ID: "srv-bare", Name: "srv-bare", DataPath: controller, NodeID: "remote-bare"}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	svc := NewFileService(store, nil, nil, nil, logger.New())
	t.Cleanup(svc.Stop)

	_, err := svc.GetFile(ctx, connect.NewRequest(&v1.GetFileRequest{ServerId: server.ID, Path: "server.properties"}))
	if connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("GetFile error = %v, want unavailable", err)
	}
}
