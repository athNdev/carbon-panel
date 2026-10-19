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
