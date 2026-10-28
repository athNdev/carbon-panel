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
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/operation"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func TestDeleteRefusesWhileAJournalLockIsHeld(t *testing.T) {
	ctx := context.Background()
	svc, _ := newDockerPrivilegeTestService(t)

	dataPath := filepath.Join(t.TempDir(), "world")
	if err := os.MkdirAll(dataPath, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	marker := filepath.Join(dataPath, "level.dat")
	if err := os.WriteFile(marker, []byte("world"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	server := &storage.Server{
		ID: "delete-while-backup", Name: "Delete While Backup", NodeID: "test-node",
		Port: 25680, Memory: 1024, DataPath: dataPath, Status: storage.StatusStopped,
	}
	if err := svc.store.CreateServer(ctx, server); err != nil {
		t.Fatalf("CreateServer fixture: %v", err)
	}
	if _, _, err := svc.store.BeginServerOperation(ctx, server.ID, server.NodeID, "", "backup-key", "backup", operation.Digest("backup", server)); err != nil {
		t.Fatalf("BeginServerOperation: %v", err)
	}

	_, err := svc.DeleteServer(ctx, connect.NewRequest(&v1.DeleteServerRequest{Id: server.ID}))
	connErr, ok := err.(*connect.Error)
	if !ok || connErr.Code() != connect.CodeAborted {
		t.Fatalf("DeleteServer error = %T %v, want CodeAborted", err, err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("world was removed while a backup lock was held: %v", err)
	}
	if _, err := svc.store.GetServer(ctx, server.ID); err != nil {
		t.Fatalf("server row was removed while a backup lock was held: %v", err)
	}
	active, err := svc.store.ListActiveServerOperations(ctx)
	if err != nil {
		t.Fatalf("ListActiveServerOperations: %v", err)
	}
	if len(active) != 1 || active[0].Kind != "backup" {
		t.Fatalf("active operations = %+v, want the backup lock to remain", active)
	}
}

func TestStartRefusesWhileAJournalLockIsHeld(t *testing.T) {
	ctx := context.Background()
	svc, _ := newDockerPrivilegeTestService(t)
	server := &storage.Server{
		ID: "start-while-delete", Name: "Start While Delete", NodeID: "test-node",
		Port: 25682, Memory: 1024, ContainerID: "still-there", Status: storage.StatusStopped,
	}
	if err := svc.store.CreateServer(ctx, server); err != nil {
		t.Fatalf("CreateServer fixture: %v", err)
	}
	if _, _, err := svc.store.BeginServerOperation(ctx, server.ID, server.NodeID, server.ContainerID, "delete-key", "delete", operation.Digest("delete", server)); err != nil {
		t.Fatalf("BeginServerOperation: %v", err)
	}

	_, err := svc.StartServer(ctx, connect.NewRequest(&v1.StartServerRequest{Id: server.ID}))
	connErr, ok := err.(*connect.Error)
	if !ok || connErr.Code() != connect.CodeAborted {
		t.Fatalf("StartServer error = %T %v, want CodeAborted", err, err)
	}
	stored, err := svc.store.GetServer(ctx, server.ID)
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if stored.Status != storage.StatusStopped || stored.ContainerID != "still-there" {
		t.Fatalf("server after refused start = status %s container %s", stored.Status, stored.ContainerID)
	}
}

func TestDeleteWithoutAContainerRemovesTheWorld(t *testing.T) {
	ctx := context.Background()
	svc, _ := newDockerPrivilegeTestService(t)

	dataPath := filepath.Join(t.TempDir(), "world")
	if err := os.MkdirAll(dataPath, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	marker := filepath.Join(dataPath, "level.dat")
	if err := os.WriteFile(marker, []byte("world"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	server := &storage.Server{
		ID: "delete-no-container", Name: "Delete No Container", NodeID: "test-node",
		Port: 25681, Memory: 1024, DataPath: dataPath, Status: storage.StatusStopped,
	}
	if err := svc.store.CreateServer(ctx, server); err != nil {
		t.Fatalf("CreateServer fixture: %v", err)
	}

	if _, err := svc.DeleteServer(ctx, connect.NewRequest(&v1.DeleteServerRequest{Id: server.ID})); err != nil {
		t.Fatalf("DeleteServer: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("world marker still present after delete: %v", err)
	}
	if _, err := svc.store.GetServer(ctx, server.ID); err == nil {
		t.Fatal("server row remained after delete")
	}
	active, err := svc.store.ListActiveServerOperations(ctx)
	if err != nil {
		t.Fatalf("ListActiveServerOperations: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("active operations = %+v, want the delete lock released", active)
	}
	ops, err := svc.store.ListServerOperations(ctx)
	if err != nil {
		t.Fatalf("ListServerOperations: %v", err)
	}
	if len(ops) != 1 || ops[0].Kind != "delete" || ops[0].Phase != storage.ServerOperationComplete {
		t.Fatalf("journal = %+v, want one completed delete", ops)
	}
}
