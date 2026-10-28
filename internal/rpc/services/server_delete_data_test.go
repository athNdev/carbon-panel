// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/internal/db"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func TestDeleteServerRemovesLocalDataBeforeTheRow(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	ctx := context.Background()
	dataPath := filepath.Join(t.TempDir(), "world")
	marker := writeServerMarker(t, dataPath)

	server := &db.Server{
		ID: "delete-local", Name: "Delete Local", NodeID: "test-node",
		MCVersion: "1.20.1", ModLoader: db.ModLoaderVanilla,
		Port: 25590, Memory: 2048, Status: db.StatusStopped, DataPath: dataPath,
	}
	if err := svc.store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.DeleteServer(ctx, connect.NewRequest(&v1.DeleteServerRequest{Id: server.ID})); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.GetServer(ctx, server.ID); err == nil {
		t.Fatal("server row still exists")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("local data stat = %v, want removed", err)
	}
}

func TestDeleteServerKeepsTheRowWhenDataRemovalFails(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	ctx := context.Background()
	dataPath := filepath.Join(t.TempDir(), "world")
	marker := writeServerMarker(t, dataPath)
	svc.removeData = func(string) error { return errors.New("disk full") }

	server := &db.Server{
		ID: "delete-retry", Name: "Delete Retry", NodeID: "test-node",
		MCVersion: "1.20.1", ModLoader: db.ModLoaderVanilla,
		Port: 25591, Memory: 2048, Status: db.StatusStopped, DataPath: dataPath,
	}
	if err := svc.store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}

	_, err := svc.DeleteServer(ctx, connect.NewRequest(&v1.DeleteServerRequest{Id: server.ID}))
	if err == nil {
		t.Fatal("DeleteServer succeeded while data removal failed")
	}
	if _, err := svc.store.GetServer(ctx, server.ID); err != nil {
		t.Fatalf("server row was removed: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("server data stat = %v", err)
	}
}

func TestDeleteServerDoesNotRemoveControllerFilesForRemoteNode(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	ctx := context.Background()
	lookalike := filepath.Join(t.TempDir(), "world")
	marker := writeServerMarker(t, lookalike)
	if err := svc.store.CreateNode(ctx, &db.Node{
		ID: "remote-no-storage", Name: "Remote", Host: "tcp://10.0.0.8:2375",
		Enabled: true, Status: db.NodeStatusOnline,
	}); err != nil {
		t.Fatal(err)
	}

	server := &db.Server{
		ID: "delete-remote", Name: "Delete Remote", NodeID: "remote-no-storage",
		MCVersion: "1.20.1", ModLoader: db.ModLoaderVanilla,
		Port: 25592, Memory: 2048, Status: db.StatusStopped, DataPath: lookalike,
	}
	if err := svc.store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}

	_, err := svc.DeleteServer(ctx, connect.NewRequest(&v1.DeleteServerRequest{Id: server.ID}))
	if err == nil {
		t.Fatal("DeleteServer removed a remote server without shared storage")
	}
	connErr, ok := err.(*connect.Error)
	if !ok || connErr.Code() != connect.CodeUnavailable {
		t.Fatalf("DeleteServer error = %T %v, want Unavailable", err, err)
	}
	if _, err := svc.store.GetServer(ctx, server.ID); err != nil {
		t.Fatalf("server row was removed: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("controller path was removed: %v", err)
	}
}

func TestDeleteServerRemovesOnlyTheSharedStorageCopy(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	ctx := context.Background()
	mount := t.TempDir()
	lookalike := filepath.Join(t.TempDir(), "world")
	kept := writeServerMarker(t, lookalike)
	mapped := filepath.Join(mount, "world")
	removed := writeServerMarker(t, mapped)
	if err := svc.store.CreateNode(ctx, &db.Node{
		ID: "remote-shared", Name: "Shared", Host: "tcp://10.0.0.9:2375",
		Enabled: true, Status: db.NodeStatusOnline, IsLocal: false,
		StorageRoot: "/node/data", ControllerStorageRoot: mount,
	}); err != nil {
		t.Fatal(err)
	}

	server := &db.Server{
		ID: "delete-shared", Name: "Delete Shared", NodeID: "remote-shared",
		MCVersion: "1.20.1", ModLoader: db.ModLoaderVanilla,
		Port: 25593, Memory: 2048, Status: db.StatusStopped,
		DataPath: filepath.Join("remote-servers", "world"),
	}
	if err := svc.store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.DeleteServer(ctx, connect.NewRequest(&v1.DeleteServerRequest{Id: server.ID})); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(removed); !os.IsNotExist(err) {
		t.Fatalf("shared copy stat = %v, want removed", err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("controller lookalike stat = %v", err)
	}
}

func writeServerMarker(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "world.dat")
	if err := os.WriteFile(marker, []byte("world"), 0o600); err != nil {
		t.Fatal(err)
	}
	return marker
}
