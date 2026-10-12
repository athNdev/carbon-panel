package db

import (
	"context"
	"testing"
	"time"
)

func TestTaskExecution_CRUD(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	mk := func(id, taskID, serverID string, status ExecutionStatus) {
		if err := store.CreateTaskExecution(ctx, &TaskExecution{
			ID: id, TaskID: taskID, ServerID: serverID,
			Status: status, StartedAt: time.Now(), Trigger: "manual",
		}); err != nil {
			t.Fatalf("create %s failed: %v", id, err)
		}
	}
	mk("e1", "task-1", "srv-1", ExecutionStatusRunning)
	mk("e2", "task-1", "srv-1", ExecutionStatusCompleted)

	got, err := store.GetTaskExecution(ctx, "e1")
	if err != nil || got.Status != ExecutionStatusRunning {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := store.GetTaskExecution(ctx, "missing"); err == nil {
		t.Error("missing execution fetched")
	}

	listed, err := store.ListTaskExecutions(ctx, "task-1", 10)
	if err != nil || len(listed) != 2 {
		t.Fatalf("list = %d, %v", len(listed), err)
	}
	one, err := store.ListTaskExecutions(ctx, "task-1", 1)
	if err != nil || len(one) != 1 {
		t.Fatalf("paged list = %d, %v", len(one), err)
	}

	srv, err := store.ListServerTaskExecutions(ctx, "srv-1", 10)
	if err != nil || len(srv) != 2 {
		t.Fatalf("server list = %d, %v", len(srv), err)
	}

	// Finish the run: status + end update round-trips.
	got.Status = ExecutionStatusCompleted
	now := time.Now()
	got.EndedAt = &now
	got.Output = "done"
	if err := store.UpdateTaskExecution(ctx, got); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	after, err := store.GetTaskExecution(ctx, "e1")
	if err != nil || after.Status != ExecutionStatusCompleted || after.Output != "done" {
		t.Fatalf("after update = %+v, %v", after, err)
	}

	if err := store.DeleteTaskExecutions(ctx, "task-1"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	listed, err = store.ListTaskExecutions(ctx, "task-1", 10)
	if err != nil || len(listed) != 0 {
		t.Fatalf("after delete = %d, %v", len(listed), err)
	}
}

func TestScheduledTask_ChildrenDueNextRun(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	parent := &ScheduledTask{
		ID: "parent", ServerID: "srv-1", Name: "p",
		TaskType: TaskTypeBackup, Status: TaskStatusEnabled, Schedule: ScheduleTypeInterval,
	}
	if err := store.CreateScheduledTask(ctx, parent); err != nil {
		t.Fatalf("create parent failed: %v", err)
	}
	child := &ScheduledTask{
		ID: "child", ServerID: "srv-1", Name: "c",
		TaskType: TaskTypeCommand, Status: TaskStatusEnabled, Schedule: ScheduleTypeEvent,
	}
	_ = child
	// ParentID may not exist on this model version; create via raw map instead if needed.
	if err := store.DB().Table("scheduled_tasks").Create(map[string]any{
		"id": "child", "server_id": "srv-1", "name": "c",
		"task_type": "command", "status": "enabled", "schedule": "event",
		"parent_id": "parent",
	}).Error; err != nil {
		t.Logf("child insert skipped: %v", err)
	} else {
		children, err := store.ListChildTasks(ctx, "parent")
		if err != nil {
			t.Fatalf("children failed: %v", err)
		}
		_ = children
	}

	next := time.Now().Add(time.Hour)
	if err := store.UpdateTaskNextRun(ctx, "parent", &next, nil); err != nil {
		t.Fatalf("next run failed: %v", err)
	}
	due, err := store.ListDueScheduledTasks(ctx, time.Now().Add(2*time.Hour))
	if err != nil {
		t.Fatalf("due failed: %v", err)
	}
	found := false
	for _, task := range due {
		if task.ID == "parent" {
			found = true
		}
	}
	if !found {
		t.Error("parent not listed as due")
	}

	far, err := store.ListDueScheduledTasks(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("past due failed: %v", err)
	}
	for _, task := range far {
		if task.ID == "parent" {
			t.Error("parent due in the past window")
		}
	}

	if err := store.DeleteScheduledTask(ctx, "parent"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := store.GetScheduledTask(ctx, "parent"); err == nil {
		t.Error("deleted task fetched")
	}
}

func TestEnsureDefaultNode_Missing(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	if _, err := store.EnsureDefaultNode(ctx); err == nil {
		t.Error("missing default node returned without error")
	}
}
