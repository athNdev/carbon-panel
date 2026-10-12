// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package db

import (
	"context"
	"testing"
)

func TestBackupRecord_CRUD(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	rec := &BackupRecord{
		ID:        "bk-1",
		ServerID:  "srv-1",
		Name:      "nightly",
		Path:      "/backups/nightly.zip",
		SizeBytes: 12345,
		SHA256:    "abc",
		Status:    "complete",
	}
	if err := store.CreateBackupRecord(ctx, rec); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	got, err := store.GetBackupRecord(ctx, "bk-1")
	if err != nil || got.Name != "nightly" || got.SizeBytes != 12345 {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := store.GetBackupRecord(ctx, "missing"); err == nil {
		t.Error("missing backup fetched without error")
	}

	if err := store.SetBackupLocked(ctx, "bk-1", true); err != nil {
		t.Fatalf("lock failed: %v", err)
	}
	if err := store.SetBackupStatus(ctx, "bk-1", "restoring"); err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if err := store.SetBackupRemoteKey(ctx, "bk-1", "s3://bucket/bk-1.zip"); err != nil {
		t.Fatalf("remote key failed: %v", err)
	}
	got, err = store.GetBackupRecord(ctx, "bk-1")
	if err != nil || !got.Locked || got.Status != "restoring" || got.RemoteKey != "s3://bucket/bk-1.zip" {
		t.Fatalf("after updates = %+v, %v", got, err)
	}

	listed, err := store.ListBackupRecords(ctx, "srv-1", 10)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list = %v, %v", listed, err)
	}
	other, err := store.ListBackupRecords(ctx, "srv-other", 10)
	if err != nil || len(other) != 0 {
		t.Fatalf("other server list = %v, %v", other, err)
	}

	if err := store.DeleteBackupRecord(ctx, "bk-1"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := store.GetBackupRecord(ctx, "bk-1"); err == nil {
		t.Error("deleted backup still fetchable")
	}
}

func TestBlueprint_SaveListGet(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	custom := &ServerBlueprint{
		ID: "bp-custom", Name: "Custom", ModLoader: ModLoaderVanilla,
		MCVersion: "1.21", DockerImage: "custom/img:latest",
	}
	builtin := &ServerBlueprint{
		ID: "bp-builtin", Name: "Builtin", ModLoader: ModLoaderPaper,
		MCVersion: "1.21", Builtin: true,
	}
	for _, bp := range []*ServerBlueprint{custom, builtin} {
		if err := store.SaveBlueprint(ctx, bp); err != nil {
			t.Fatalf("save %s failed: %v", bp.ID, err)
		}
	}

	got, err := store.GetBlueprint(ctx, "bp-custom")
	if err != nil || got.DockerImage != "custom/img:latest" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := store.GetBlueprint(ctx, "missing"); err == nil {
		t.Error("missing blueprint fetched without error")
	}

	listed, err := store.ListBlueprints(ctx)
	if err != nil || len(listed) != 2 {
		t.Fatalf("list = %v, %v", listed, err)
	}
	// Builtins sort first.
	if !listed[0].Builtin {
		t.Errorf("first blueprint builtin = %v, want builtins first", listed[0].Builtin)
	}

	// Save is an upsert: re-saving the same ID updates.
	custom.Description = "updated"
	if err := store.SaveBlueprint(ctx, custom); err != nil {
		t.Fatalf("resave failed: %v", err)
	}
	got, err = store.GetBlueprint(ctx, "bp-custom")
	if err != nil || got.Description != "updated" {
		t.Fatalf("after resave = %+v, %v", got, err)
	}
	listed, err = store.ListBlueprints(ctx)
	if err != nil || len(listed) != 2 {
		t.Fatalf("resave duplicated rows: %v, %v", listed, err)
	}
}
