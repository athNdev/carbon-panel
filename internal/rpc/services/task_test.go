package services

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/scheduler"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func setupTaskTestService(t *testing.T) (*TaskService, *storage.Server) {
	t.Helper()
	store := setupTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	log := logger.New()

	sched := scheduler.NewScheduler(store, nil, nil, nil, nil, log)
	taskSvc := NewTaskService(store, sched, log)

	server := &storage.Server{
		ID:       uuid.New().String(),
		Name:     "task-test-server",
		DataPath: t.TempDir(),
		Status:   storage.StatusStopped,
	}
	require.NoError(t, store.CreateServer(context.Background(), server))

	return taskSvc, server
}

func TestTaskService_CreateTask_CronValidation(t *testing.T) {
	svc, server := setupTaskTestService(t)
	ctx := context.Background()

	// 1. Cron task with empty CronExpr must fail with CodeInvalidArgument (SCH-7)
	_, err := svc.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		ServerId: server.ID,
		Name:     "empty-cron-task",
		Schedule: v1.ScheduleType_SCHEDULE_TYPE_CRON,
		CronExpr: "",
		TaskType: v1.TaskType_TASK_TYPE_BACKUP,
	}))
	require.Error(t, err)
	connectErr, ok := err.(*connect.Error)
	require.True(t, ok)
	assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
	assert.Contains(t, connectErr.Message(), "cron_expr is required")

	// 2. Cron task with invalid cron syntax must fail with CodeInvalidArgument (SCH-7)
	_, err = svc.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		ServerId: server.ID,
		Name:     "invalid-cron-task",
		Schedule: v1.ScheduleType_SCHEDULE_TYPE_CRON,
		CronExpr: "not-a-valid-cron-spec",
		TaskType: v1.TaskType_TASK_TYPE_BACKUP,
	}))
	require.Error(t, err)
	connectErr, ok = err.(*connect.Error)
	require.True(t, ok)
	assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
	assert.Contains(t, connectErr.Message(), "invalid cron expression")

	// 3. Cron task with valid cron syntax must succeed and calculate NextRun
	res, err := svc.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		ServerId: server.ID,
		Name:     "valid-cron-task",
		Schedule: v1.ScheduleType_SCHEDULE_TYPE_CRON,
		CronExpr: "0 0 * * *",
		TaskType: v1.TaskType_TASK_TYPE_BACKUP,
	}))
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "valid-cron-task", res.Msg.Task.Name)
	assert.NotNil(t, res.Msg.Task.NextRun)
}

func TestTaskService_CreateTask_IntervalValidation(t *testing.T) {
	svc, server := setupTaskTestService(t)
	ctx := context.Background()

	// 1. Interval <= 0 must fail with CodeInvalidArgument
	_, err := svc.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		ServerId:     server.ID,
		Name:         "bad-interval-task",
		Schedule:     v1.ScheduleType_SCHEDULE_TYPE_INTERVAL,
		IntervalSecs: 0,
		TaskType:     v1.TaskType_TASK_TYPE_BACKUP,
	}))
	require.Error(t, err)
	connectErr, ok := err.(*connect.Error)
	require.True(t, ok)
	assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
	assert.Contains(t, connectErr.Message(), "interval_secs must be greater than 0")

	// 2. Valid interval must succeed
	res, err := svc.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		ServerId:     server.ID,
		Name:         "valid-interval-task",
		Schedule:     v1.ScheduleType_SCHEDULE_TYPE_INTERVAL,
		IntervalSecs: 300,
		TaskType:     v1.TaskType_TASK_TYPE_BACKUP,
	}))
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, int32(300), res.Msg.Task.IntervalSecs)
	assert.NotNil(t, res.Msg.Task.NextRun)
}

func TestTaskService_CreateTask_OnceValidation(t *testing.T) {
	svc, server := setupTaskTestService(t)
	ctx := context.Background()

	// 1. Once schedule without RunAt must fail
	_, err := svc.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		ServerId: server.ID,
		Name:     "missing-run-at-task",
		Schedule: v1.ScheduleType_SCHEDULE_TYPE_ONCE,
		TaskType: v1.TaskType_TASK_TYPE_BACKUP,
	}))
	require.Error(t, err)
	connectErr, ok := err.(*connect.Error)
	require.True(t, ok)
	assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
	assert.Contains(t, connectErr.Message(), "run_at is required")

	// 2. Once schedule with future RunAt must succeed
	future := time.Now().Add(2 * time.Hour)
	res, err := svc.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		ServerId: server.ID,
		Name:     "valid-once-task",
		Schedule: v1.ScheduleType_SCHEDULE_TYPE_ONCE,
		RunAt:    timestamppb.New(future),
		TaskType: v1.TaskType_TASK_TYPE_BACKUP,
	}))
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.NotNil(t, res.Msg.Task.NextRun)
}

func TestTaskService_UpdateTask_CronValidation(t *testing.T) {
	svc, server := setupTaskTestService(t)
	ctx := context.Background()

	// Create valid initial task
	res, err := svc.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		ServerId: server.ID,
		Name:     "updatable-task",
		Schedule: v1.ScheduleType_SCHEDULE_TYPE_CRON,
		CronExpr: "0 12 * * *",
		TaskType: v1.TaskType_TASK_TYPE_BACKUP,
	}))
	require.NoError(t, err)
	taskID := res.Msg.Task.Id

	// 1. Update with invalid cron expression must fail with CodeInvalidArgument
	invalidCron := "bad-cron"
	_, err = svc.UpdateTask(ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		Id:       taskID,
		CronExpr: &invalidCron,
	}))
	require.Error(t, err)
	connectErr, ok := err.(*connect.Error)
	require.True(t, ok)
	assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())

	// 2. Update with valid cron expression must succeed
	validCron := "*/10 * * * *"
	updateRes, err := svc.UpdateTask(ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		Id:       taskID,
		CronExpr: &validCron,
	}))
	require.NoError(t, err)
	assert.Equal(t, validCron, updateRes.Msg.Task.CronExpr)
}
