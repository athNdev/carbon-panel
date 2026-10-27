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

	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/nodestorage"
)

func TestDataRootForLocalPassthrough(t *testing.T) {
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()
	ctx := context.Background()

	base := t.TempDir()
	dataPath := filepath.Join(base, "servers", "srv-data")
	if err := os.MkdirAll(dataPath, 0o755); err != nil {
		t.Fatal(err)
	}
	server := &db.Server{ID: "srv-1", Name: "srv-1", DataPath: dataPath, NodeID: "local"}

	root, err := dataRootFor(ctx, store, server, false)
	if err != nil || root != dataPath {
		t.Fatalf("local read root = %q, %v; want %q", root, err, dataPath)
	}
	root, err = dataRootFor(ctx, store, server, true)
	if err != nil || root != dataPath {
		t.Fatalf("local write root = %q, %v; want %q", root, err, dataPath)
	}
}

func TestDataRootForRemoteSharedStorage(t *testing.T) {
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()
	ctx := context.Background()

	mount := filepath.Join(t.TempDir(), "shared")
	if err := os.MkdirAll(mount, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateNode(ctx, &db.Node{ID: "remote-1", Name: "remote-1", Host: "tcp://10.0.0.5:2375", Enabled: true, StorageRoot: mount}); err != nil {
		t.Fatal(err)
	}
	server := &db.Server{ID: "srv-1", Name: "srv-1", DataPath: filepath.Join("srv", "data", "servers", "srv-data"), NodeID: "remote-1"}

	// Reads map the path without creating anything.
	root, err := dataRootFor(ctx, store, server, false)
	want := filepath.Join(mount, "srv-data")
	if err != nil || root != want {
		t.Fatalf("remote read root = %q, %v; want %q", root, err, want)
	}
	if _, statErr := os.Stat(want); !os.IsNotExist(statErr) {
		t.Fatalf("read mapping must not create the server directory, stat err = %v", statErr)
	}

	// Writes create the mapped server directory.
	root, err = dataRootFor(ctx, store, server, true)
	if err != nil || root != want {
		t.Fatalf("remote write root = %q, %v; want %q", root, err, want)
	}
	if fi, statErr := os.Stat(want); statErr != nil || !fi.IsDir() {
		t.Fatalf("write mapping must create the server directory: %v", statErr)
	}
}

func TestDataRootForRefusals(t *testing.T) {
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()
	ctx := context.Background()

	if err := store.CreateNode(ctx, &db.Node{ID: "remote-bare", Name: "remote-bare", Host: "tcp://10.0.0.6:2375", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateNode(ctx, &db.Node{ID: "remote-unmounted", Name: "remote-unmounted", Host: "tcp://10.0.0.7:2375", Enabled: true, StorageRoot: filepath.Join(t.TempDir(), "not-mounted")}); err != nil {
		t.Fatal(err)
	}

	bare := &db.Server{ID: "srv-bare", Name: "srv-bare", DataPath: filepath.Join("srv", "servers", "bare"), NodeID: "remote-bare"}
	if _, err := dataRootFor(ctx, store, bare, false); err == nil || !nodestorage.IsUnavailable(err) {
		t.Fatalf("remote node without storage must be refused with ErrUnavailable, got %v", err)
	}
	if _, err := dataRootFor(ctx, store, bare, true); err == nil || !nodestorage.IsUnavailable(err) {
		t.Fatalf("remote node without storage must be refused for writes, got %v", err)
	}

	unmounted := &db.Server{ID: "srv-unmounted", Name: "srv-unmounted", DataPath: filepath.Join("srv", "servers", "unmounted"), NodeID: "remote-unmounted"}
	if _, err := dataRootFor(ctx, store, unmounted, true); err == nil || !nodestorage.IsUnavailable(err) {
		t.Fatalf("missing mount must be refused with ErrUnavailable, got %v", err)
	}

	// An unresolvable node reference fails closed instead of guessing.
	missing := &db.Server{ID: "srv-missing", Name: "srv-missing", DataPath: filepath.Join("srv", "servers", "missing"), NodeID: "no-such-node"}
	if _, err := dataRootFor(ctx, store, missing, false); err == nil {
		t.Fatal("unknown storage node must fail closed")
	}
}
