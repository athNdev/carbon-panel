package db

import (
	"context"
	"testing"
	"time"
)

func TestModuleTemplate_CRUD(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	custom := &ModuleTemplate{ID: "tpl-1", Name: "Custom", Type: ModuleTemplateTypeCustom, DockerImage: "img:latest"}
	builtin := &ModuleTemplate{ID: "tpl-b", Name: "Builtin", Type: ModuleTemplateTypeBuiltin, DockerImage: "img:b"}
	for _, tpl := range []*ModuleTemplate{custom, builtin} {
		if err := store.CreateModuleTemplate(ctx, tpl); err != nil {
			t.Fatalf("create %s failed: %v", tpl.ID, err)
		}
	}

	got, err := store.GetModuleTemplate(ctx, "tpl-1")
	if err != nil || got.DockerImage != "img:latest" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := store.GetModuleTemplate(ctx, "missing"); err == nil {
		t.Error("missing template fetched")
	}
	got, err = store.GetModuleTemplateByName(ctx, "Builtin")
	if err != nil || got.ID != "tpl-b" {
		t.Fatalf("by name = %+v, %v", got, err)
	}
	if _, err := store.GetModuleTemplateByName(ctx, "missing"); err == nil {
		t.Error("missing name matched")
	}

	listed, err := store.ListModuleTemplates(ctx)
	if err != nil || len(listed) != 2 {
		t.Fatalf("list = %d, %v", len(listed), err)
	}
	builtins, err := store.ListBuiltinModuleTemplates(ctx)
	if err != nil || len(builtins) != 1 || builtins[0].ID != "tpl-b" {
		t.Fatalf("builtins = %v, %v", builtins, err)
	}

	custom.Description = "updated"
	if err := store.UpdateModuleTemplate(ctx, custom); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	after, err := store.GetModuleTemplate(ctx, "tpl-1")
	if err != nil || after.Description != "updated" {
		t.Fatalf("after update = %+v, %v", after, err)
	}
}

func TestCleanOldTaskExecutions(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	if err := store.CreateScheduledTask(ctx, &ScheduledTask{
		ID: "task-1", ServerID: "srv-1", Name: "t",
		TaskType: TaskTypeBackup, Status: TaskStatusEnabled, Schedule: ScheduleTypeInterval,
	}); err != nil {
		t.Fatalf("failed to insert task: %v", err)
	}
	mk := func(id string, ended time.Time) {
		if err := store.DB().Create(&TaskExecution{
			ID: id, TaskID: "task-1", ServerID: "srv-1",
			Status: ExecutionStatusCompleted, StartedAt: ended.Add(-time.Minute),
			EndedAt: &ended,
		}).Error; err != nil {
			t.Fatalf("seed %s failed: %v", id, err)
		}
	}
	old := time.Now().Add(-48 * time.Hour)
	mk("e-old-1", old)
	mk("e-old-2", old)
	mk("e-new", time.Now())

	// Keep minimum 1: only one old execution may go.
	if err := store.CleanOldTaskExecutions(ctx, time.Now().Add(-time.Hour), 1); err != nil {
		t.Fatalf("clean failed: %v", err)
	}
	listed, err := store.ListTaskExecutions(ctx, "task-1", 10)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "e-new" {
		t.Errorf("after clean = %d executions, want only e-new", len(listed))
	}
}

func TestListByNode_ServersAndModules(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	if err := store.DB().Create(&Server{ID: "srv-1", Name: "s1", DataPath: t.TempDir(), NodeID: "node-a"}).Error; err != nil {
		t.Fatalf("seed server failed: %v", err)
	}
	if err := store.DB().Create(&Module{ID: "mod-1", Name: "m1", ServerID: "srv-1", TemplateID: "tpl", NodeID: "node-a"}).Error; err != nil {
		t.Fatalf("seed module failed: %v", err)
	}

	servers, err := store.ListServersByNode(ctx, "node-a")
	if err != nil || len(servers) != 1 {
		t.Fatalf("servers = %d, %v", len(servers), err)
	}
	servers, err = store.ListServersByNode(ctx, "node-b")
	if err != nil || len(servers) != 0 {
		t.Fatalf("other node servers = %d, %v", len(servers), err)
	}
	modules, err := store.ListModulesByNode(ctx, "node-a")
	if err != nil || len(modules) != 1 {
		t.Fatalf("modules = %d, %v", len(modules), err)
	}
}
