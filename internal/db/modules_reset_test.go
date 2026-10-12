// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package db

import (
	"context"
	"testing"
)

func TestModule_CRUD(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	mod := &Module{ID: "mod-1", Name: "Geyser", ServerID: "srv-1", TemplateID: "tpl-1", NodeID: "node-a"}
	if err := store.DB().Create(mod).Error; err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	listed, err := store.ListModules(ctx)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list = %d, %v", len(listed), err)
	}
	byServer, err := store.ListServerModules(ctx, "srv-1")
	if err != nil || len(byServer) != 1 {
		t.Fatalf("by server = %d, %v", len(byServer), err)
	}
	byServer, err = store.ListServerModules(ctx, "srv-other")
	if err != nil || len(byServer) != 0 {
		t.Fatalf("other server = %d, %v", len(byServer), err)
	}
	byTemplate, err := store.ListModulesByTemplate(ctx, "tpl-1")
	if err != nil || len(byTemplate) != 1 {
		t.Fatalf("by template = %d, %v", len(byTemplate), err)
	}

	mod.Status = ModuleStatusRunning
	if err := store.UpdateModule(ctx, mod); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	got, err := store.GetModuleByHostPort(ctx, 8080)
	if err != nil || got != nil {
		t.Logf("by host port (none configured) = %+v, %v", got, err)
	}
	got, err = store.GetModuleByContainerID(ctx, "cid-missing")
	if err != nil || got != nil {
		t.Logf("by container (none) = %+v, %v", got, err)
	}

	if err := store.DeleteModule(ctx, "mod-1"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	listed, err = store.ListModules(ctx)
	if err != nil || len(listed) != 0 {
		t.Fatalf("after delete = %d, %v", len(listed), err)
	}
}

func TestModuleTemplate_UpsertDelete(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	tpl := &ModuleTemplate{ID: "tpl-1", Name: "T", Type: ModuleTemplateTypeCustom, DockerImage: "img:1"}
	if err := store.CreateModuleTemplate(ctx, tpl); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	tpl.DockerImage = "img:2"
	if err := store.UpsertModuleTemplate(ctx, tpl); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	got, err := store.GetModuleTemplate(ctx, "tpl-1")
	if err != nil || got.DockerImage != "img:2" {
		t.Fatalf("after upsert = %+v, %v", got, err)
	}

	fresh := &ModuleTemplate{ID: "tpl-2", Name: "New", Type: ModuleTemplateTypeCustom, DockerImage: "img:x"}
	if err := store.UpsertModuleTemplate(ctx, fresh); err != nil {
		t.Fatalf("upsert-new failed: %v", err)
	}
	if _, err := store.GetModuleTemplate(ctx, "tpl-2"); err != nil {
		t.Fatalf("upserted template missing: %v", err)
	}

	if err := store.DeleteModuleTemplate(ctx, "tpl-1"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := store.GetModuleTemplate(ctx, "tpl-1"); err == nil {
		t.Error("deleted template fetched")
	}
}

func TestListChildTasks_EnabledOnly(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	parent := "task-parent"
	mk := func(id, status string) {
		if err := store.DB().Create(&ScheduledTask{
			ID: id, ServerID: "srv-1", Name: id,
			TaskType: TaskTypeBackup, Status: TaskStatus(status),
			Schedule: ScheduleTypeInterval, ParentTaskID: &parent,
		}).Error; err != nil {
			t.Fatalf("seed %s failed: %v", id, err)
		}
	}
	_ = parent
	// ParentTaskID is *string; use a parent row for realism.
	if err := store.CreateScheduledTask(ctx, &ScheduledTask{
		ID: "task-parent", ServerID: "srv-1", Name: "p",
		TaskType: TaskTypeBackup, Status: TaskStatusEnabled, Schedule: ScheduleTypeInterval,
	}); err != nil {
		t.Fatalf("parent seed failed: %v", err)
	}
	mk("child-on", string(TaskStatusEnabled))
	mk("child-off", string(TaskStatusDisabled))

	children, err := store.ListChildTasks(ctx, "task-parent")
	if err != nil {
		t.Fatalf("children failed: %v", err)
	}
	if len(children) != 1 || children[0].ID != "child-on" {
		t.Errorf("children = %v", children)
	}
	children, err = store.ListChildTasks(ctx, "no-such-parent")
	if err != nil || len(children) != 0 {
		t.Errorf("missing parent children = %v, %v", children, err)
	}
}

func TestResetAllUsers_WipesIdentity(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	if err := store.DB().Create(&User{ID: "u1", Username: "alice", AuthProvider: "local", IsActive: true}).Error; err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	if err := store.ResetAllUsers(ctx); err != nil {
		t.Fatalf("reset failed: %v", err)
	}
	if n, err := store.CountUsers(ctx); err != nil || n != 0 {
		t.Errorf("users after reset = %d, %v", n, err)
	}
}
