package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

func setupChainTest(t *testing.T) (*Scheduler, *storage.Store, context.Context, *storage.Server) {
	t.Helper()
	store := setupTestStore(t)
	ctx := context.Background()
	log := logger.New()
	server := &storage.Server{ID: uuid.New().String(), Name: "chain-server", Status: storage.StatusStopped}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("create server: %v", err)
	}
	appCfg := &config.Config{}
	s := NewScheduler(store, nil, nil, appCfg, nil, log, Config{CheckInterval: time.Hour})
	return s, store, ctx, server
}

func mkTask(serverID, name string, taskType storage.TaskType) *storage.ScheduledTask {
	return &storage.ScheduledTask{
		ID: storageUUID(), ServerID: serverID, Name: name,
		TaskType: taskType, Schedule: storage.ScheduleTypeOnce,
		Status: storage.TaskStatusEnabled, RequireOnline: true,
	}
}

func storageUUID() string { return uuid.New().String() }

func countExecutions(t *testing.T, store *storage.Store, ctx context.Context, taskID string) int {
	t.Helper()
	execs, err := store.ListTaskExecutions(ctx, taskID, 50)
	if err != nil {
		t.Fatalf("list executions: %v", err)
	}
	return len(execs)
}

func TestChainRunsChildrenInOrder(t *testing.T) {
	s, store, ctx, server := setupChainTest(t)
	parent := mkTask(server.ID, "parent", storage.TaskTypeCommand)
	if err := store.CreateScheduledTask(ctx, parent); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"step-a", "step-b"} {
		child := mkTask(server.ID, name, storage.TaskTypeCommand)
		pid := parent.ID
		child.ParentTaskID = &pid
		child.StepOrder = i
		if err := store.CreateScheduledTask(ctx, child); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.executeTask(parent, "manual", 0, nil); err != nil {
		t.Fatalf("parent: %v", err)
	}
	if n := countExecutions(t, store, ctx, parent.ID); n != 1 {
		t.Fatalf("expected 1 parent execution, got %d", n)
	}
	children, err := store.ListChildTasks(ctx, parent.ID)
	if err != nil || len(children) != 2 {
		t.Fatalf("children: %v %+v", err, children)
	}
	for _, c := range children {
		if n := countExecutions(t, store, ctx, c.ID); n != 1 {
			t.Fatalf("expected 1 execution for %s, got %d", c.Name, n)
		}
	}
}

func TestChainAbortsOnFailure(t *testing.T) {
	s, store, ctx, server := setupChainTest(t)
	// Failing tasks must actually execute: mark the server running (offline
	// would skip before reaching the executor). RequireOnline=false does not
	// persist (gorm default:true), so flip server status instead.
	if err := store.UpdateServerStatus(ctx, server.ID, storage.StatusRunning); err != nil {
		t.Fatal(err)
	}
	parent := mkTask(server.ID, "parent", storage.TaskType("bogus"))
	parent.RequireOnline = false
	if err := store.CreateScheduledTask(ctx, parent); err != nil {
		t.Fatal(err)
	}
	child := mkTask(server.ID, "child", storage.TaskTypeCommand)
	pid := parent.ID
	child.ParentTaskID = &pid
	child.RequireOnline = false
	if err := store.CreateScheduledTask(ctx, child); err != nil {
		t.Fatal(err)
	}
	if _, err := s.executeTask(parent, "manual", 0, nil); err == nil {
		t.Fatalf("expected parent failure")
	}
	if n := countExecutions(t, store, ctx, child.ID); n != 0 {
		t.Fatalf("chain should abort: child has %d executions", n)
	}
}

func TestChainContinuesWhenAllowed(t *testing.T) {
	s, store, ctx, server := setupChainTest(t)
	if err := store.UpdateServerStatus(ctx, server.ID, storage.StatusRunning); err != nil {
		t.Fatal(err)
	}
	parent := mkTask(server.ID, "parent", storage.TaskType("bogus"))
	parent.RequireOnline = false
	parent.ContinueOnFailure = true
	if err := store.CreateScheduledTask(ctx, parent); err != nil {
		t.Fatal(err)
	}
	child := mkTask(server.ID, "child", storage.TaskType("bogus"))
	pid := parent.ID
	child.ParentTaskID = &pid
	child.RequireOnline = false
	if err := store.CreateScheduledTask(ctx, child); err != nil {
		t.Fatal(err)
	}
	if _, err := s.executeTask(parent, "manual", 0, nil); err == nil {
		t.Fatalf("expected parent failure")
	}
	// Chain attempted the child despite parent failure (child itself fails
	// on unknown type, which is fine — the point is it ran).
	if n := countExecutions(t, store, ctx, child.ID); n != 1 {
		t.Fatalf("expected 1 child execution, got %d", n)
	}
}
