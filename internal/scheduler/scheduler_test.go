package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/pkg/logger"
)

func setupTestStore(t *testing.T) *storage.Store {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Path:           ":memory:",
			AutoMigrate:    true,
			MaxConnections: 1,
		},
	}
	store, err := storage.NewSQLiteStore(cfg)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}
	return store
}

func TestScheduler_InFlightTaskDeduplication(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	log := logger.New()

	server := &storage.Server{
		ID:     uuid.New().String(),
		Name:   "test-server",
		Status: storage.StatusRunning,
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	past := time.Now().Add(-10 * time.Minute)
	task := &storage.ScheduledTask{
		ID:           uuid.New().String(),
		ServerID:     server.ID,
		Name:         "test-task",
		TaskType:     storage.TaskTypeCommand,
		Schedule:     storage.ScheduleTypeInterval,
		IntervalSecs: 60,
		Status:       storage.TaskStatusEnabled,
		NextRun:      &past,
	}
	if err := store.CreateScheduledTask(ctx, task); err != nil {
		t.Fatalf("failed to create scheduled task: %v", err)
	}

	appCfg := &config.Config{}
	s := NewScheduler(store, nil, nil, appCfg, nil, log, Config{
		CheckInterval: 5 * time.Second,
	})

	// Pre-mark task as running (simulating an in-flight execution)
	s.runningTasksMu.Lock()
	s.runningTasks[task.ID] = true
	s.runningTasksMu.Unlock()

	if !s.IsTaskRunning(task.ID) {
		t.Fatalf("expected task to be reported as running")
	}

	// Trigger due task check
	s.checkAndRunDueTasks()

	// Wait for any asynchronous tasks (none should be started)
	s.wg.Wait()

	// Verify no execution records were created because it was skipped
	executions, err := store.ListTaskExecutions(ctx, task.ID, 10)
	if err != nil {
		t.Fatalf("failed to list task executions: %v", err)
	}
	if len(executions) != 0 {
		t.Errorf("expected 0 executions for in-flight task, got %d", len(executions))
	}
}

func TestScheduler_DispatchUpdatesNextRunAndCleansUp(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	log := logger.New()

	server := &storage.Server{
		ID:     uuid.New().String(),
		Name:   "test-server-2",
		Status: storage.StatusRunning,
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	past := time.Now().Add(-10 * time.Minute)
	task := &storage.ScheduledTask{
		ID:           uuid.New().String(),
		ServerID:     server.ID,
		Name:         "test-task-2",
		TaskType:     storage.TaskTypeCommand,
		Schedule:     storage.ScheduleTypeInterval,
		IntervalSecs: 3600, // 1 hour
		Status:       storage.TaskStatusEnabled,
		NextRun:      &past,
	}
	if err := store.CreateScheduledTask(ctx, task); err != nil {
		t.Fatalf("failed to create scheduled task: %v", err)
	}

	appCfg := &config.Config{}
	s := NewScheduler(store, nil, nil, appCfg, nil, log, Config{
		CheckInterval: 5 * time.Second,
	})

	// Run due tasks check
	s.checkAndRunDueTasks()
	s.wg.Wait()

	// After completion, task should no longer be in runningTasks
	if s.IsTaskRunning(task.ID) {
		t.Errorf("expected task to no longer be marked as running")
	}

	// NextRun in DB must now be in the future (not due anymore)
	updatedTask, err := store.GetScheduledTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("failed to get updated task: %v", err)
	}
	if updatedTask.NextRun == nil || !updatedTask.NextRun.After(time.Now()) {
		t.Errorf("expected NextRun to be updated to future, got %v", updatedTask.NextRun)
	}
}
