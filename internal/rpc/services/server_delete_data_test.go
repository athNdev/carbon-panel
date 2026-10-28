// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/internal/activity"
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
	if err := svc.store.DB().Create(&db.BackendPortLease{NodeID: server.NodeID, Port: server.Port, ServerID: server.ID}).Error; err != nil {
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
	var leases []db.BackendPortLease
	if err := svc.store.DB().Where("server_id = ?", server.ID).Find(&leases).Error; err != nil {
		t.Fatal(err)
	}
	if len(leases) != 0 {
		t.Fatalf("port leases still held: %+v", leases)
	}
	requireDisposition(t, svc, server.ID, db.DispositionDelete, db.DispositionSettled, dataPath)
	if _, err := svc.DeleteServer(ctx, connect.NewRequest(&v1.DeleteServerRequest{Id: server.ID})); err != nil {
		t.Fatalf("settled delete retry: %v", err)
	}
}

func TestDeleteServerKeepDataLeavesTheWorldAndDropsTheRow(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	ctx := context.Background()
	dataPath := filepath.Join(t.TempDir(), "world")
	marker := writeServerMarker(t, dataPath)

	server := &db.Server{
		ID: "keep-local", Name: "Keep Local", NodeID: "test-node",
		MCVersion: "1.20.1", ModLoader: db.ModLoaderVanilla,
		Port: 25593, Memory: 2048, Status: db.StatusStopped, DataPath: dataPath,
	}
	if err := svc.store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.DeleteServer(ctx, connect.NewRequest(&v1.DeleteServerRequest{
		Id: server.ID, KeepData: true,
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.GetServer(ctx, server.ID); err == nil {
		t.Fatal("server row still exists")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("kept world stat = %v", err)
	}
	requireDisposition(t, svc, server.ID, db.DispositionKeepData, db.DispositionSettled, dataPath)

	var logs []db.ActivityLog
	if err := svc.store.DB().Where("subject_id = ? AND event = ?", server.ID, activity.EventServerDelete).Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("delete audit count = %d", len(logs))
	}
	var props map[string]any
	if err := json.Unmarshal([]byte(logs[0].Properties), &props); err != nil {
		t.Fatal(err)
	}
	if props["keep_data"] != true || props["data_path"] != dataPath {
		t.Fatalf("delete audit properties = %s", logs[0].Properties)
	}
}

func TestDeleteServerDetachLeavesTheContainerWorldAndPort(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	if svc.docker != nil {
		_ = svc.docker.Close()
	}
	svc.pool = nil
	svc.docker = nil
	ctx := context.Background()
	dataPath := filepath.Join(t.TempDir(), "world")
	marker := writeServerMarker(t, dataPath)

	server := &db.Server{
		ID: "detach-local", Name: "Detach Local", NodeID: "test-node",
		MCVersion: "1.20.1", ModLoader: db.ModLoaderVanilla,
		Port: 25595, Memory: 2048, Status: db.StatusRunning,
		DataPath: dataPath, ContainerID: "still-running",
	}
	if err := svc.store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.DB().Create(&db.BackendPortLease{NodeID: server.NodeID, Port: server.Port, ServerID: server.ID}).Error; err != nil {
		t.Fatal(err)
	}
	svc.removeData = func(string) error { t.Fatal("detach removed server data"); return nil }

	if _, err := svc.DeleteServer(ctx, connect.NewRequest(&v1.DeleteServerRequest{
		Id: server.ID, Detach: true,
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.GetServer(ctx, server.ID); err == nil {
		t.Fatal("server row still exists")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("detached world stat = %v", err)
	}
	requireDisposition(t, svc, server.ID, db.DispositionDetach, db.DispositionSettled, dataPath)
	var leases []db.BackendPortLease
	if err := svc.store.DB().Where("server_id = ?", server.ID).Find(&leases).Error; err != nil {
		t.Fatal(err)
	}
	if len(leases) != 1 || leases[0].Port != server.Port {
		t.Fatalf("port leases = %+v", leases)
	}

	var logs []db.ActivityLog
	if err := svc.store.DB().Where("subject_id = ? AND event = ?", server.ID, activity.EventServerDelete).Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("delete audit count = %d", len(logs))
	}
	var props map[string]any
	if err := json.Unmarshal([]byte(logs[0].Properties), &props); err != nil {
		t.Fatal(err)
	}
	if props["detach"] != true || props["keep_data"] != true || props["container_id"] != server.ContainerID || props["data_path"] != dataPath {
		t.Fatalf("delete audit properties = %s", logs[0].Properties)
	}
}

func TestDeleteServerKeepDataOnRemoteNodeLeavesControllerFiles(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	ctx := context.Background()
	lookalike := filepath.Join(t.TempDir(), "world")
	marker := writeServerMarker(t, lookalike)
	if err := svc.store.CreateNode(ctx, &db.Node{
		ID: "remote-keep", Name: "Remote Keep", Host: "tcp://10.0.0.10:2375",
		Enabled: true, Status: db.NodeStatusOnline,
	}); err != nil {
		t.Fatal(err)
	}

	server := &db.Server{
		ID: "keep-remote", Name: "Keep Remote", NodeID: "remote-keep",
		MCVersion: "1.20.1", ModLoader: db.ModLoaderVanilla,
		Port: 25594, Memory: 2048, Status: db.StatusStopped, DataPath: lookalike,
	}
	if err := svc.store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.DeleteServer(ctx, connect.NewRequest(&v1.DeleteServerRequest{
		Id: server.ID, KeepData: true,
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.GetServer(ctx, server.ID); err == nil {
		t.Fatal("server row still exists")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("controller path stat = %v", err)
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
	requireDisposition(t, svc, server.ID, db.DispositionDelete, db.DispositionPending, dataPath)

	svc.removeData = nil
	if _, err := svc.DeleteServer(ctx, connect.NewRequest(&v1.DeleteServerRequest{Id: server.ID})); err != nil {
		t.Fatalf("retry after a failed data delete: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("retried delete left the world: %v", err)
	}
	requireDisposition(t, svc, server.ID, db.DispositionDelete, db.DispositionSettled, dataPath)
}

func TestStartupResumesAPendingDeletion(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	ctx := context.Background()
	dataPath := filepath.Join(t.TempDir(), "world")
	marker := writeServerMarker(t, dataPath)
	server := &db.Server{
		ID: "resume-pending", Name: "Resume Pending", NodeID: "test-node",
		MCVersion: "1.20.1", ModLoader: db.ModLoaderVanilla,
		Port: 25596, Memory: 2048, Status: db.StatusStopped, DataPath: dataPath,
	}
	if err := svc.store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.BeginServerDisposition(ctx, &db.ServerDisposition{
		ServerID: server.ID, Name: server.Name, NodeID: server.NodeID,
		DataPath: dataPath, Port: server.Port, Mode: db.DispositionDelete,
	}); err != nil {
		t.Fatal(err)
	}

	finished, err := svc.ResumeUnsettledDispositions(ctx)
	if err != nil || finished != 1 {
		t.Fatalf("resume = %d %v, want 1", finished, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("resumed delete left the world: %v", err)
	}
	if _, err := svc.store.GetServer(ctx, server.ID); err == nil {
		t.Fatal("server row still exists")
	}
	requireDisposition(t, svc, server.ID, db.DispositionDelete, db.DispositionSettled, dataPath)
}

func TestStartupFinishesARecordedDispositionWithoutDeletingTheWorldAgain(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	ctx := context.Background()
	dataPath := filepath.Join(t.TempDir(), "world")
	marker := writeServerMarker(t, dataPath)
	svc.removeData = func(string) error {
		t.Fatal("startup deleted a world whose disposition was already recorded")
		return nil
	}
	server := &db.Server{
		ID: "resume-settled-data", Name: "Resume Settled Data", NodeID: "test-node",
		MCVersion: "1.20.1", ModLoader: db.ModLoaderVanilla,
		Port: 25597, Memory: 2048, Status: db.StatusStopped, DataPath: dataPath,
	}
	if err := svc.store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	disp, err := svc.store.BeginServerDisposition(ctx, &db.ServerDisposition{
		ServerID: server.ID, Name: server.Name, NodeID: server.NodeID,
		DataPath: dataPath, Port: server.Port, Mode: db.DispositionDelete,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetServerDispositionPhase(ctx, disp.ID, db.DispositionDataSettled); err != nil {
		t.Fatal(err)
	}

	finished, err := svc.ResumeUnsettledDispositions(ctx)
	if err != nil || finished != 1 {
		t.Fatalf("resume = %d %v, want 1", finished, err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("recorded world was removed: %v", err)
	}
	if _, err := svc.store.GetServer(ctx, server.ID); err == nil {
		t.Fatal("server row still exists")
	}
	requireDisposition(t, svc, server.ID, db.DispositionDelete, db.DispositionSettled, dataPath)
}

func requireDisposition(t *testing.T, svc *ServerService, serverID, mode, phase, dataPath string) {
	t.Helper()
	disp, err := svc.store.LatestServerDisposition(context.Background(), serverID)
	if err != nil || disp == nil {
		t.Fatalf("disposition = %v %v", disp, err)
	}
	if disp.Mode != mode || disp.Phase != phase || disp.DataPath != dataPath {
		t.Fatalf("disposition = mode %s phase %s path %s, want %s %s %s", disp.Mode, disp.Phase, disp.DataPath, mode, phase, dataPath)
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
