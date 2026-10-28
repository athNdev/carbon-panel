// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package scheduler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/google/uuid"
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

func TestExecuteScriptTask_UnavailableNodeDoesNotUseLocalDocker(t *testing.T) {
	for _, withPool := range []bool{true, false} {
		name := "without_pool"
		if withPool {
			name = "with_pool"
		}
		t.Run(name, func(t *testing.T) {
			store := setupTestStore(t)
			defer func() { _ = store.Close() }()

			var localCalls atomic.Int64
			localDaemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				localCalls.Add(1)
				w.Header().Set("Api-Version", "1.41")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("{}"))
			}))
			defer localDaemon.Close()
			localDocker, err := docker.NewClient(localDaemon.URL, logger.New())
			if err != nil {
				t.Fatalf("create local sentinel Docker client: %v", err)
			}
			defer func() { _ = localDocker.Close() }()
			localCalls.Store(0) // Ignore the constructor ping; count only caller operations.

			s := NewScheduler(store, localDocker, nil, nil, nil, logger.New())
			if withPool {
				s.SetClientPool(docker.NewClientPool(store, localDocker, logger.New()))
			}
			server := &storage.Server{ID: "script-remote", NodeID: "unavailable-remote", ContainerID: "remote-cid"}
			task := &storage.ScheduledTask{Config: `{"script_path":"/bin/true"}`}

			_, err = s.executeScriptTask(context.Background(), server, task)
			if err == nil {
				t.Fatal("expected unavailable-node error")
			}
			if got := localCalls.Load(); got != 0 {
				t.Fatalf("local Docker requests = %d, want 0", got)
			}
		})
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

func TestScheduler_StartRecoversOrphanedExecutions(t *testing.T) {
	store := setupTestStore(t)
	defer func() {
		_ = store.Close()
	}()

	ctx := context.Background()
	log := logger.New()

	server := &storage.Server{
		ID:     uuid.New().String(),
		Name:   "test-server-orphan",
		Status: storage.StatusRunning,
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	task := &storage.ScheduledTask{
		ID:       uuid.New().String(),
		ServerID: server.ID,
		Name:     "orphan-task",
		TaskType: storage.TaskTypeCommand,
		Schedule: storage.ScheduleTypeInterval,
		Status:   storage.TaskStatusDisabled,
	}
	if err := store.CreateScheduledTask(ctx, task); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	execID := uuid.New().String()
	exec := &storage.TaskExecution{
		ID:        execID,
		TaskID:    task.ID,
		ServerID:  server.ID,
		Status:    storage.ExecutionStatusRunning,
		StartedAt: time.Now().Add(-5 * time.Minute),
	}
	if err := store.CreateTaskExecution(ctx, exec); err != nil {
		t.Fatalf("failed to create running execution: %v", err)
	}

	appCfg := &config.Config{}
	s := NewScheduler(store, nil, nil, appCfg, nil, log, Config{
		CheckInterval: time.Hour,
	})

	if err := s.Start(); err != nil {
		t.Fatalf("failed to start scheduler: %v", err)
	}
	defer func() {
		_ = s.Stop()
	}()

	recovered, err := store.GetTaskExecution(ctx, execID)
	if err != nil {
		t.Fatalf("failed to get recovered execution: %v", err)
	}
	if recovered.Status != storage.ExecutionStatusFailed {
		t.Errorf("expected status %s, got %s", storage.ExecutionStatusFailed, recovered.Status)
	}
	if recovered.Error != "interrupted by panel restart; run the task again after the server is idle" {
		t.Errorf("expected the restart reason to name the next action, got '%s'", recovered.Error)
	}
	if recovered.EndedAt == nil {
		t.Errorf("expected EndedAt to be set")
	}
}

func TestScheduler_CancelExecution_OrphanedInStore(t *testing.T) {
	store := setupTestStore(t)
	defer func() {
		_ = store.Close()
	}()

	ctx := context.Background()
	log := logger.New()

	server := &storage.Server{
		ID:     uuid.New().String(),
		Name:   "test-server-cancel",
		Status: storage.StatusRunning,
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	task := &storage.ScheduledTask{
		ID:       uuid.New().String(),
		ServerID: server.ID,
		Name:     "cancel-task",
		TaskType: storage.TaskTypeCommand,
		Schedule: storage.ScheduleTypeInterval,
		Status:   storage.TaskStatusDisabled,
	}
	if err := store.CreateScheduledTask(ctx, task); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	execID := uuid.New().String()
	exec := &storage.TaskExecution{
		ID:        execID,
		TaskID:    task.ID,
		ServerID:  server.ID,
		Status:    storage.ExecutionStatusRunning,
		StartedAt: time.Now().Add(-2 * time.Minute),
	}
	if err := store.CreateTaskExecution(ctx, exec); err != nil {
		t.Fatalf("failed to create running execution: %v", err)
	}

	appCfg := &config.Config{}
	s := NewScheduler(store, nil, nil, appCfg, nil, log, Config{
		CheckInterval: time.Hour,
	})

	// Cancel execution that is orphaned in store (not in memory runningExecutions)
	if err := s.CancelExecution(execID); err != nil {
		t.Fatalf("failed to cancel orphaned execution: %v", err)
	}

	cancelled, err := store.GetTaskExecution(ctx, execID)
	if err != nil {
		t.Fatalf("failed to get cancelled execution: %v", err)
	}
	if cancelled.Status != storage.ExecutionStatusCancelled {
		t.Errorf("expected status %s, got %s", storage.ExecutionStatusCancelled, cancelled.Status)
	}
	if cancelled.Error != "execution cancelled" {
		t.Errorf("expected error 'execution cancelled', got '%s'", cancelled.Error)
	}
	if cancelled.EndedAt == nil {
		t.Errorf("expected EndedAt to be set")
	}

	// Calling cancel again on now-cancelled execution should return an error
	if err := s.CancelExecution(execID); err == nil {
		t.Errorf("expected error cancelling already finished execution, got nil")
	}

	// Calling cancel on non-existent execution should return an error
	if err := s.CancelExecution(uuid.New().String()); err == nil {
		t.Errorf("expected error cancelling non-existent execution, got nil")
	}
}
