package svc

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestScheduleService_CRUDAndExecution(t *testing.T) {
	t.Parallel()
	bundle := testBundle(t)
	schedSvc := bundle.Schedule
	ctx := orgCtx()

	// 1. Seed a test workload in database
	q, err := bundle.Schedule.deps.Store.Org(ctx)
	require.NoError(t, err)

	workloadID := uuid.NewString()
	nodeID := uuid.NewString()
	w := &db.Workload{
		TenantBase: db.TenantBase{
			ID:    workloadID,
			OrgID: testOrg,
		},
		NodeID: nodeID,
		Name:   "survival-cron",
		Status: "running",
	}
	require.NoError(t, q.Create(w).Error)

	// 2. Validate invalid cron expression
	_, err = schedSvc.CreateSchedule(ctx, connect.NewRequest(&v1.CreateScheduleRequest{
		WorkloadId:     workloadID,
		Name:           "Invalid Cron",
		CronExpression: "invalid-cron-syntax",
		ActionType:     v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_COMMAND,
		Payload:        "say hello",
		Enabled:        true,
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// 3. Create a valid schedule
	createResp, err := schedSvc.CreateSchedule(ctx, connect.NewRequest(&v1.CreateScheduleRequest{
		WorkloadId:     workloadID,
		Name:           "Daily Restart",
		CronExpression: "0 4 * * *",
		ActionType:     v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_RESTART,
		Enabled:        true,
	}))
	require.NoError(t, err)
	schedID := createResp.Msg.Schedule.Id
	require.NotEmpty(t, schedID)
	require.Equal(t, "Daily Restart", createResp.Msg.Schedule.Name)
	require.Equal(t, "0 4 * * *", createResp.Msg.Schedule.CronExpression)
	require.Equal(t, v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_RESTART, createResp.Msg.Schedule.ActionType)
	require.True(t, createResp.Msg.Schedule.Enabled)
	require.Greater(t, createResp.Msg.Schedule.NextRunAtUnix, time.Now().Unix())

	// 4. List schedules
	listResp, err := schedSvc.ListSchedules(ctx, connect.NewRequest(&v1.ListSchedulesRequest{
		WorkloadId: workloadID,
	}))
	require.NoError(t, err)
	require.Len(t, listResp.Msg.Schedules, 1)
	require.Equal(t, schedID, listResp.Msg.Schedules[0].Id)

	// 5. Get schedule
	getResp, err := schedSvc.GetSchedule(ctx, connect.NewRequest(&v1.GetScheduleRequest{
		ScheduleId: schedID,
	}))
	require.NoError(t, err)
	require.Equal(t, "Daily Restart", getResp.Msg.Schedule.Name)

	// 6. Update schedule
	updateResp, err := schedSvc.UpdateSchedule(ctx, connect.NewRequest(&v1.UpdateScheduleRequest{
		ScheduleId:     schedID,
		Name:           "Hourly Broadcast",
		CronExpression: "0 * * * *",
		ActionType:     v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_COMMAND,
		Payload:        "say Hydration Check!",
		Enabled:        true,
	}))
	require.NoError(t, err)
	require.Equal(t, "Hourly Broadcast", updateResp.Msg.Schedule.Name)
	require.Equal(t, "0 * * * *", updateResp.Msg.Schedule.CronExpression)
	require.Equal(t, v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_COMMAND, updateResp.Msg.Schedule.ActionType)
	require.Equal(t, "say Hydration Check!", updateResp.Msg.Schedule.Payload)

	// 7. Trigger manual run (node agent offline in unit test so execution will fail gracefully and be recorded)
	runResp, err := schedSvc.RunSchedule(ctx, connect.NewRequest(&v1.RunScheduleRequest{
		ScheduleId: schedID,
	}))
	require.NoError(t, err)
	require.NotEmpty(t, runResp.Msg.Execution.Id)
	require.Equal(t, "manual", runResp.Msg.Execution.TriggeredBy)
	require.Equal(t, v1.ScheduleExecutionStatus_SCHEDULE_EXECUTION_STATUS_FAILED, runResp.Msg.Execution.Status)
	require.Contains(t, runResp.Msg.Execution.Error, "offline")

	// 8. List executions
	execsResp, err := schedSvc.ListScheduleExecutions(ctx, connect.NewRequest(&v1.ListScheduleExecutionsRequest{
		ScheduleId: schedID,
	}))
	require.NoError(t, err)
	require.Len(t, execsResp.Msg.Executions, 1)
	require.Equal(t, runResp.Msg.Execution.Id, execsResp.Msg.Executions[0].Id)

	// 9. Delete schedule
	delResp, err := schedSvc.DeleteSchedule(ctx, connect.NewRequest(&v1.DeleteScheduleRequest{
		ScheduleId: schedID,
	}))
	require.NoError(t, err)
	require.True(t, delResp.Msg.Success)

	// Verify not found after delete
	_, err = schedSvc.GetSchedule(ctx, connect.NewRequest(&v1.GetScheduleRequest{
		ScheduleId: schedID,
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}


func TestScheduleService_BackgroundDueExecution(t *testing.T) {
	t.Parallel()
	bundle := testBundle(t)
	schedSvc := bundle.Schedule
	ctx := orgCtx()

	q, err := bundle.Schedule.deps.Store.Org(ctx)
	require.NoError(t, err)

	workloadID := uuid.NewString()
	w := &db.Workload{
		TenantBase: db.TenantBase{
			ID:    workloadID,
			OrgID: testOrg,
		},
		NodeID: uuid.NewString(),
		Name:   "survival-ticker",
		Status: "running",
	}
	require.NoError(t, q.Create(w).Error)

	past := time.Now().UTC().Add(-1 * time.Minute)
	sched := &db.WorkloadSchedule{
		TenantBase: db.TenantBase{
			ID:    uuid.NewString(),
			OrgID: testOrg,
		},
		WorkloadID:     workloadID,
		Name:           "Due Broadcast",
		CronExpression: "*/5 * * * *",
		ActionType:     "command",
		Payload:        "say hello",
		Enabled:        true,
		NextRunAt:      &past,
	}
	sq, err := bundle.Schedule.deps.Store.Org(ctx)
	require.NoError(t, err)
	require.NoError(t, sq.Create(sched).Error)

	// Run due schedules check directly
	schedSvc.runDueSchedules(context.Background())

	// Verify an execution record was created
	execsResp, err := schedSvc.ListScheduleExecutions(ctx, connect.NewRequest(&v1.ListScheduleExecutionsRequest{
		ScheduleId: sched.ID,
	}))
	require.NoError(t, err)
	require.Len(t, execsResp.Msg.Executions, 1)
	require.Equal(t, "cron", execsResp.Msg.Executions[0].TriggeredBy)

	// Verify next_run_at was advanced into the future
	getResp, err := schedSvc.GetSchedule(ctx, connect.NewRequest(&v1.GetScheduleRequest{
		ScheduleId: sched.ID,
	}))
	require.NoError(t, err)
	require.Greater(t, getResp.Msg.Schedule.NextRunAtUnix, time.Now().Unix())
}
