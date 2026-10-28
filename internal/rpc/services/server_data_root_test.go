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
	"github.com/athNdev/carbon-panel/internal/docker"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func registerRemoteCreateNode(t *testing.T, svc *ServerService, id, storageRoot string) {
	t.Helper()
	if err := svc.store.CreateNode(context.Background(), &db.Node{
		ID: id, Name: id, Host: "tcp://10.2.0.8:2375", Enabled: true,
		Status: db.NodeStatusOnline, StorageRoot: storageRoot,
	}); err != nil {
		t.Fatal(err)
	}
	pool := docker.NewClientPool(svc.store, svc.docker, svc.log)
	pool.RegisterClient(id, svc.docker)
	svc.pool = pool
}

func TestCreateServerWritesTheWorldOnSharedStorage(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	mount := t.TempDir()
	registerRemoteCreateNode(t, svc, "remote-create", mount)

	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "pack.zip")
	if err := os.WriteFile(src, []byte("modpack-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.DB().Create(&db.IndexedModpack{
		ID: "manual-remote", Indexer: "manual", Name: "Manual", GameVersions: "[]",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.store.DB().Create(&db.IndexedModpackFile{
		ID: "manual-remote-file", ModpackID: "manual-remote", DownloadURL: src,
	}).Error; err != nil {
		t.Fatal(err)
	}

	resp, err := svc.CreateServer(ctxWithRole("operator"), connect.NewRequest(&v1.CreateServerRequest{
		Name: "remote-world", McVersion: "1.20.1", DockerImage: "java21",
		NodeId: "remote-create", Port: 25581, Memory: 2048, ModpackId: "manual-remote",
	}))
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	server, err := svc.store.GetServer(context.Background(), resp.Msg.Server.Id)
	if err != nil {
		t.Fatal(err)
	}
	mapped := filepath.Join(mount, filepath.Base(server.DataPath))
	info, statErr := os.Stat(mapped)
	if statErr != nil || !info.IsDir() {
		t.Fatalf("mapped world %s: %v", mapped, statErr)
	}
	if _, err := os.Stat(server.DataPath); !os.IsNotExist(err) {
		t.Fatalf("controller data path %s exists: %v", server.DataPath, err)
	}
	got, err := os.ReadFile(filepath.Join(mapped, "modpack.zip"))
	if err != nil || string(got) != "modpack-bytes" {
		t.Fatalf("modpack.zip = %q, %v", got, err)
	}
}

func TestDeleteServerRemovesTheMappedWorldOnly(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	ctx := context.Background()
	mount := t.TempDir()
	registerRemoteCreateNode(t, svc, "remote-delete", mount)

	logical := filepath.Join(t.TempDir(), "servers", "world-a")
	if err := os.MkdirAll(logical, 0o755); err != nil {
		t.Fatal(err)
	}
	lookalike := filepath.Join(logical, "level.dat")
	if err := os.WriteFile(lookalike, []byte("controller"), 0o600); err != nil {
		t.Fatal(err)
	}
	mapped := filepath.Join(mount, "world-a")
	if err := os.MkdirAll(mapped, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mapped, "level.dat"), []byte("remote"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := &db.Server{
		ID: "del-mapped", Name: "Del Mapped", NodeID: "remote-delete",
		MCVersion: "1.20.1", ModLoader: db.ModLoaderVanilla,
		Port: 25582, Memory: 1024, Status: db.StatusStopped, DataPath: logical,
	}
	if err := svc.store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.DeleteServer(ctx, connect.NewRequest(&v1.DeleteServerRequest{Id: server.ID})); err != nil {
		t.Fatalf("DeleteServer: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mapped, "level.dat")); !os.IsNotExist(err) {
		t.Fatalf("mapped world stat = %v, want removed", err)
	}
	if _, err := os.Stat(lookalike); err != nil {
		t.Fatalf("controller lookalike stat = %v", err)
	}
	if _, err := svc.store.GetServer(ctx, server.ID); err == nil {
		t.Fatal("server row still exists")
	}
}

func TestDeleteServerRefusesARemoteNodeWithNoStorage(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	ctx := context.Background()
	if err := svc.store.CreateNode(ctx, &db.Node{
		ID: "remote-nostorage", Name: "Remote No Storage", Host: "tcp://10.2.0.9:2375",
		Enabled: true, Status: db.NodeStatusOnline,
	}); err != nil {
		t.Fatal(err)
	}
	logical := filepath.Join(t.TempDir(), "servers", "world-b")
	if err := os.MkdirAll(logical, 0o755); err != nil {
		t.Fatal(err)
	}
	lookalike := filepath.Join(logical, "level.dat")
	if err := os.WriteFile(lookalike, []byte("controller"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := &db.Server{
		ID: "del-nostorage", Name: "Del No Storage", NodeID: "remote-nostorage",
		MCVersion: "1.20.1", ModLoader: db.ModLoaderVanilla,
		Port: 25583, Memory: 1024, Status: db.StatusStopped, DataPath: logical,
	}
	if err := svc.store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}

	_, err := svc.DeleteServer(ctx, connect.NewRequest(&v1.DeleteServerRequest{Id: server.ID}))
	connErr, ok := err.(*connect.Error)
	if !ok || connErr.Code() != connect.CodeUnavailable {
		t.Fatalf("DeleteServer error = %T %v, want CodeUnavailable", err, err)
	}
	if _, err := os.Stat(lookalike); err != nil {
		t.Fatalf("controller lookalike stat = %v", err)
	}
	if _, err := svc.store.GetServer(ctx, server.ID); err != nil {
		t.Fatalf("server row was removed: %v", err)
	}
}

func TestCreateServerRefusesARemoteNodeWithNoStorage(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	registerRemoteCreateNode(t, svc, "remote-bare", "")

	_, err := svc.CreateServer(ctxWithRole("operator"), connect.NewRequest(&v1.CreateServerRequest{
		Name: "bare-world", McVersion: "1.20.1", DockerImage: "java21",
		NodeId: "remote-bare", Port: 25582, Memory: 1024,
	}))
	if connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("CreateServer error = %v, want unavailable", err)
	}
	servers, listErr := svc.store.ListServers(context.Background())
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(servers) != 0 {
		t.Fatalf("created %d servers after a storage refusal", len(servers))
	}
	controllerServers := filepath.Join(svc.config.Storage.DataDir, "servers")
	if _, statErr := os.Stat(controllerServers); !os.IsNotExist(statErr) {
		t.Fatalf("controller servers directory %s: %v", controllerServers, statErr)
	}
}
