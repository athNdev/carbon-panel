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
