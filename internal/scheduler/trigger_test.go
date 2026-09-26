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

	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
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
