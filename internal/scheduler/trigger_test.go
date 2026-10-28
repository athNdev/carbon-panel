// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/events"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func newTriggerTestScheduler(t *testing.T) (*Scheduler, *storage.Store) {
	t.Helper()

	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	store, err := storage.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{
			Path:           fmt.Sprintf("file:%s?mode=memory&cache=shared", dbName),
			AutoMigrate:    true,
			MaxConnections: 5,
		},
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return NewScheduler(store, nil, nil, nil, nil, logger.New()), store
}

func TestTriggerTask_MissingTask(t *testing.T) {
	s, _ := newTriggerTestScheduler(t)

	_, err := s.TriggerTask(context.Background(), "no-such-task")
	if !errors.Is(err, storage.ErrScheduledTaskNotFound) {
		t.Fatalf("TriggerTask missing = %v, want ErrScheduledTaskNotFound", err)
	}
}

func TestTriggerTask_AlreadyRunning(t *testing.T) {
	s, store := newTriggerTestScheduler(t)
	ctx := context.Background()

	task := &storage.ScheduledTask{
		ID:       "task-1",
		ServerID: "srv-1",
		Name:     "t",
		TaskType: storage.TaskTypeCommand,
		Status:   storage.TaskStatusEnabled,
		Schedule: storage.ScheduleTypeOnce,
	}
	if err := store.CreateScheduledTask(ctx, task); err != nil {
		t.Fatalf("failed to insert task: %v", err)
	}

	// Simulate an in-flight execution.
	s.runningTasksMu.Lock()
	s.runningTasks[task.ID] = true
	s.runningTasksMu.Unlock()

	_, err := s.TriggerTask(ctx, task.ID)
	if !errors.Is(err, ErrTaskAlreadyRunning) {
		t.Fatalf("TriggerTask in-flight = %v, want ErrTaskAlreadyRunning", err)
	}
}

func TestTriggerTask_RejectsWhenDatabaseRecordsRunningExecution(t *testing.T) {
	s, store := newTriggerTestScheduler(t)
	ctx := context.Background()

	task := &storage.ScheduledTask{
		ID:       "task-db-run",
		ServerID: "srv-1",
		Name:     "t",
		TaskType: storage.TaskTypeCommand,
		Status:   storage.TaskStatusEnabled,
		Schedule: storage.ScheduleTypeOnce,
	}
	if err := store.CreateScheduledTask(ctx, task); err != nil {
		t.Fatalf("failed to insert task: %v", err)
	}

	// Insert an active running execution directly in the DB
	exec := &storage.TaskExecution{
		ID:        "exec-active",
		TaskID:    task.ID,
		ServerID:  task.ServerID,
		Status:    storage.ExecutionStatusRunning,
		StartedAt: time.Now(),
		Trigger:   "scheduled",
	}
	if err := store.CreateTaskExecution(ctx, exec); err != nil {
		t.Fatalf("failed to insert running execution: %v", err)
	}

	// In-memory map is completely empty, but IsTaskRunning must report true from DB
	if !s.IsTaskRunning(task.ID) {
		t.Fatalf("expected IsTaskRunning to return true because DB has active execution")
	}

	_, err := s.TriggerTask(ctx, task.ID)
	if !errors.Is(err, ErrTaskAlreadyRunning) {
		t.Fatalf("TriggerTask with DB running execution = %v, want ErrTaskAlreadyRunning", err)
	}
}

func TestHandleServerEvent_DeduplicatesConcurrently(t *testing.T) {
	s, store := newTriggerTestScheduler(t)
	ctx := context.Background()

	task := &storage.ScheduledTask{
		ID:            "task-event-dup",
		ServerID:      "srv-1",
		Name:          "event-task",
		TaskType:      storage.TaskTypeCommand,
		Status:        storage.TaskStatusEnabled,
		Schedule:      storage.ScheduleTypeEvent,
		EventTriggers: []v1.TriggeredEventType{v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START},
	}
	if err := store.DB().Create(task).Error; err != nil {
		t.Fatalf("failed to seed task: %v", err)
	}

	// Claim the task in memory to simulate concurrent execution
	s.runningTasksMu.Lock()
	s.runningTasks[task.ID] = true
	s.runningTasksMu.Unlock()

	// Calling HandleServerEvent should claim-check and skip duplicate dispatch
	s.HandleServerEvent(ctx, events.Event{
		ServerID: "srv-1",
		Type:     v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START,
	})

	// Wait for any scheduler workgroups to complete
	s.wg.Wait()

	// Verify no running executions were spawned
	hasRunning, err := store.HasRunningExecution(ctx, task.ID)
	if err != nil {
		t.Fatalf("failed to query running execution: %v", err)
	}
	if hasRunning {
		t.Errorf("expected no execution to be dispatched for already running task")
	}
}

func TestTryClaimTaskRefusesWhenTheRunningCheckFails(t *testing.T) {
	s, store := newTriggerTestScheduler(t)
	ctx := context.Background()

	task := &storage.ScheduledTask{
		ID:       "task-check-fail",
		ServerID: "srv-1",
		Name:     "t",
		TaskType: storage.TaskTypeCommand,
		Status:   storage.TaskStatusEnabled,
		Schedule: storage.ScheduleTypeOnce,
	}
	if err := store.CreateScheduledTask(ctx, task); err != nil {
		t.Fatalf("failed to insert task: %v", err)
	}
	sqlDB, err := store.DB().DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	if !s.IsTaskRunning(task.ID) {
		t.Fatal("an unknown running check must be treated as running")
	}
	claimed, err := s.tryClaimTask(ctx, task.ID)
	if err == nil || claimed {
		t.Fatalf("claim = %v, %v; want an error and no claim", claimed, err)
	}
	s.runningTasksMu.Lock()
	registered := s.runningTasks[task.ID]
	s.runningTasksMu.Unlock()
	if registered {
		t.Fatal("a failed check registered the task as running")
	}
}

type failCommandSender struct {
	n int
}

func (f *failCommandSender) SendCommand(context.Context, string, string) (string, error) {
	f.n++
	return "", errors.New("rcon closed")
}

func TestCommandTaskDoesNotRepeatAFailedSend(t *testing.T) {
	s, store := newTriggerTestScheduler(t)
	ctx := context.Background()
	sender := &failCommandSender{}
	s.sender = sender

	server := &storage.Server{
		ID:          "srv-cmd",
		Name:        "cmd",
		Status:      storage.StatusRunning,
		ContainerID: "container-1",
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("create server: %v", err)
	}
	task := &storage.ScheduledTask{
		ID:         "task-cmd-once",
		ServerID:   server.ID,
		Name:       "say",
		TaskType:   storage.TaskTypeCommand,
		Status:     storage.TaskStatusEnabled,
		Schedule:   storage.ScheduleTypeOnce,
		Config:     `{"command":"say hi"}`,
		RetryCount: 3,
	}
	if err := store.CreateScheduledTask(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	execution, err := s.executeTask(ctx, task, "manual", v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_UNSPECIFIED, nil)
	if err == nil {
		t.Fatal("expected the command send to fail")
	}
	if sender.n != 1 {
		t.Fatalf("command sends = %d, want 1", sender.n)
	}
	if execution == nil || !strings.Contains(execution.Output, "command was not retried") {
		t.Fatalf("output = %q", executionOutput(execution))
	}
}

func TestDueCronRunsOnceWhenSeveralSlotsWereMissed(t *testing.T) {
	s, store := newTriggerTestScheduler(t)
	ctx := context.Background()

	server := &storage.Server{ID: "srv-missed", Name: "missed", Status: storage.StatusStopped}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("create server: %v", err)
	}
	overdue := time.Now().Add(-3 * time.Hour)
	task := &storage.ScheduledTask{
		ID:       "task-missed-cron",
		ServerID: server.ID,
		Name:     "nightly",
		TaskType: storage.TaskTypeCommand,
		Status:   storage.TaskStatusEnabled,
		Schedule: storage.ScheduleTypeCron,
		CronExpr: "0 0 * * *",
		NextRun:  &overdue,
	}
	if err := store.CreateScheduledTask(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	s.checkAndRunDueTasks()
	s.wg.Wait()

	updated, err := store.GetScheduledTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if updated.NextRun == nil || !updated.NextRun.After(time.Now()) {
		t.Fatalf("next run = %v, want a future slot", updated.NextRun)
	}
	executions, err := store.ListTaskExecutions(ctx, task.ID, 10)
	if err != nil {
		t.Fatalf("list executions: %v", err)
	}
	if len(executions) != 1 {
		t.Fatalf("executions after the overdue tick = %d, want 1", len(executions))
	}

	s.checkAndRunDueTasks()
	s.wg.Wait()
	executions, err = store.ListTaskExecutions(ctx, task.ID, 10)
	if err != nil {
		t.Fatalf("list executions again: %v", err)
	}
	if len(executions) != 1 {
		t.Fatalf("executions after the following tick = %d, want 1", len(executions))
	}
}
