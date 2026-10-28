// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/internal/activity"
	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/download"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func TestExportWorldLeavesTheServerAndTheWorld(t *testing.T) {
	store := setupTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	dataPath := filepath.Join(t.TempDir(), "world")
	marker := writeServerMarker(t, dataPath)
	downloads := download.NewManager(t.TempDir(), time.Hour, logger.New())
	t.Cleanup(downloads.Stop)

	server := &db.Server{
		ID: "export-local", Name: "Export Local", NodeID: "local",
		DataPath: dataPath, Status: db.StatusRunning, ContainerID: "still-running",
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	svc := NewFileService(store, nil, nil, downloads, logger.New())
	t.Cleanup(svc.Stop)

	resp, err := svc.ExportWorld(ctx, connect.NewRequest(&v1.ExportWorldRequest{ServerId: server.ID}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Msg.GetFilename() != "Export Local-world.zip" || resp.Msg.GetDataPath() != dataPath || resp.Msg.GetSessionId() == "" {
		t.Fatalf("export = %+v", resp.Msg)
	}
	session, err := downloads.GetSession(resp.Msg.GetSessionId())
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(session.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = archive.Close() })
	found := false
	for _, entry := range archive.File {
		if entry.Name == "world.dat" || entry.Name == filepath.Join("world.dat") {
			found = true
		}
	}
	if !found {
		names := make([]string, 0, len(archive.File))
		for _, entry := range archive.File {
			names = append(names, entry.Name)
		}
		t.Fatalf("archive entries = %v", names)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("world stat = %v", err)
	}
	kept, err := store.GetServer(ctx, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if kept.ContainerID != "still-running" {
		t.Fatalf("container id = %q", kept.ContainerID)
	}

	var logs []db.ActivityLog
	if err := store.DB().Where("subject_id = ? AND event = ?", server.ID, activity.EventServerExport).Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("export audit count = %d", len(logs))
	}
	var props map[string]any
	if err := json.Unmarshal([]byte(logs[0].Properties), &props); err != nil {
		t.Fatal(err)
	}
	if props["data_path"] != dataPath || props["filename"] != "Export Local-world.zip" {
		t.Fatalf("export audit properties = %s", logs[0].Properties)
	}
}

func TestExportWorldRefusesAnUnreachableRemoteNode(t *testing.T) {
	store := setupTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	lookalike := filepath.Join(t.TempDir(), "world")
	marker := writeServerMarker(t, lookalike)
	tempDir := t.TempDir()
	downloads := download.NewManager(tempDir, time.Hour, logger.New())
	t.Cleanup(downloads.Stop)

	if err := store.CreateNode(ctx, &db.Node{
		ID: "remote-export", Name: "Remote Export", Host: "tcp://10.0.0.11:2375",
		Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	server := &db.Server{
		ID: "export-remote", Name: "Export Remote", NodeID: "remote-export",
		DataPath: lookalike, Status: db.StatusStopped, ContainerID: "remote-container",
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	svc := NewFileService(store, nil, nil, downloads, logger.New())
	t.Cleanup(svc.Stop)

	_, err := svc.ExportWorld(ctx, connect.NewRequest(&v1.ExportWorldRequest{ServerId: server.ID}))
	connErr, ok := err.(*connect.Error)
	if !ok || connErr.Code() != connect.CodeUnavailable {
		t.Fatalf("ExportWorld error = %T %v, want Unavailable", err, err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("controller path stat = %v", err)
	}
	if _, err := store.GetServer(ctx, server.ID); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("export wrote %d files for an unreachable node", len(entries))
	}
}
