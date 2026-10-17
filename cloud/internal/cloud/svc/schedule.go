package svc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/athNdev/carbon-panel/pkg/proto/cloud/v1/cloudv1connect"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

// ScheduleService implements cloudv1connect.ScheduleServiceHandler (MINE-164).
type ScheduleService struct {
	cloudv1connect.UnimplementedScheduleServiceHandler
	deps       Deps
	dispatcher *AgentDispatcher
	cronParser cron.Parser
	logger     *slog.Logger
}

// NewScheduleService initializes the schedule service.
func NewScheduleService(deps Deps, dispatcher *AgentDispatcher, logger *slog.Logger) *ScheduleService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ScheduleService{
		deps:       deps,
		dispatcher: dispatcher,
		cronParser: cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor),
		logger:     logger.With("service", "schedule"),
	}
}

// CreateSchedule defines a new recurring cron task for a workload.
func (s *ScheduleService) CreateSchedule(ctx context.Context, req *connect.Request[v1.CreateScheduleRequest]) (*connect.Response[v1.CreateScheduleResponse], error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	workloadID := strings.TrimSpace(req.Msg.WorkloadId)
	if workloadID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload_id is required"))
	}
	name := strings.TrimSpace(req.Msg.Name)
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name is required"))
	}
	cronExpr := strings.TrimSpace(req.Msg.CronExpression)
	if cronExpr == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("cron_expression is required"))
	}

	sched, err := s.cronParser.Parse(cronExpr)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid cron expression %q: %w", cronExpr, err))
	}

	var w db.Workload
	if err := q.First(&w, "id = ?", workloadID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("workload not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	actionStr := scheduleActionProtoToString(req.Msg.ActionType)
	if actionStr == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid action_type"))
	}

	nextRun := sched.Next(time.Now().UTC())
	record := &db.WorkloadSchedule{
		TenantBase: db.TenantBase{
			ID:    uuid.NewString(),
			OrgID: w.OrgID,
		},
		WorkloadID:     w.ID,
		Name:           name,
		CronExpression: cronExpr,
		ActionType:     actionStr,
		Payload:        strings.TrimSpace(req.Msg.Payload),
		Enabled:        req.Msg.Enabled,
		NextRunAt:      &nextRun,
	}

	createQ, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if err := createQ.Create(record).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create schedule record: %w", err))
	}

	return connect.NewResponse(&v1.CreateScheduleResponse{
		Schedule: scheduleToProto(record),
	}), nil
}

// ListSchedules returns all recurring schedules for a workload.
func (s *ScheduleService) ListSchedules(ctx context.Context, req *connect.Request[v1.ListSchedulesRequest]) (*connect.Response[v1.ListSchedulesResponse], error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	workloadID := strings.TrimSpace(req.Msg.WorkloadId)
	if workloadID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload_id is required"))
	}

	var schedules []db.WorkloadSchedule
	if err := q.Where("workload_id = ?", workloadID).Order("created_at ASC").Find(&schedules).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	out := make([]*v1.WorkloadSchedule, len(schedules))
	for i := range schedules {
		out[i] = scheduleToProto(&schedules[i])
	}

	return connect.NewResponse(&v1.ListSchedulesResponse{Schedules: out}), nil
}

// GetSchedule retrieves a single schedule by ID.
func (s *ScheduleService) GetSchedule(ctx context.Context, req *connect.Request[v1.GetScheduleRequest]) (*connect.Response[v1.GetScheduleResponse], error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	scheduleID := strings.TrimSpace(req.Msg.ScheduleId)
	if scheduleID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("schedule_id is required"))
	}

	var sched db.WorkloadSchedule
	if err := q.First(&sched, "id = ?", scheduleID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("schedule not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&v1.GetScheduleResponse{Schedule: scheduleToProto(&sched)}), nil
}

// UpdateSchedule updates properties of an existing schedule.
func (s *ScheduleService) UpdateSchedule(ctx context.Context, req *connect.Request[v1.UpdateScheduleRequest]) (*connect.Response[v1.UpdateScheduleResponse], error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	scheduleID := strings.TrimSpace(req.Msg.ScheduleId)
	if scheduleID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("schedule_id is required"))
	}

	var sched db.WorkloadSchedule
	if err := q.First(&sched, "id = ?", scheduleID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("schedule not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	updates := map[string]any{
		"enabled": req.Msg.Enabled,
		"payload": strings.TrimSpace(req.Msg.Payload),
	}
	sched.Payload = strings.TrimSpace(req.Msg.Payload)
	sched.Enabled = req.Msg.Enabled

	if name := strings.TrimSpace(req.Msg.Name); name != "" {
		updates["name"] = name
		sched.Name = name
	}
	if cronExpr := strings.TrimSpace(req.Msg.CronExpression); cronExpr != "" {
		parsed, err := s.cronParser.Parse(cronExpr)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid cron expression %q: %w", cronExpr, err))
		}
		updates["cron_expression"] = cronExpr
		sched.CronExpression = cronExpr
		nextRun := parsed.Next(time.Now().UTC())
		updates["next_run_at"] = &nextRun
		sched.NextRunAt = &nextRun
	}
	if req.Msg.ActionType != v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_UNSPECIFIED {
		actionStr := scheduleActionProtoToString(req.Msg.ActionType)
		if actionStr != "" {
			updates["action_type"] = actionStr
			sched.ActionType = actionStr
		}
	}

	uq, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if err := uq.Model(&db.WorkloadSchedule{}).Where("id = ?", sched.ID).Updates(updates).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("update schedule record: %w", err))
	}

	return connect.NewResponse(&v1.UpdateScheduleResponse{Schedule: scheduleToProto(&sched)}), nil
}

// DeleteSchedule removes a schedule.
func (s *ScheduleService) DeleteSchedule(ctx context.Context, req *connect.Request[v1.DeleteScheduleRequest]) (*connect.Response[v1.DeleteScheduleResponse], error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	scheduleID := strings.TrimSpace(req.Msg.ScheduleId)
	if scheduleID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("schedule_id is required"))
	}

	res := q.Where("id = ?", scheduleID).Delete(&db.WorkloadSchedule{})
	if res.Error != nil {
		return nil, connect.NewError(connect.CodeInternal, res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("schedule not found"))
	}

	return connect.NewResponse(&v1.DeleteScheduleResponse{Success: true}), nil
}

// RunSchedule triggers immediate execution of a schedule.
func (s *ScheduleService) RunSchedule(ctx context.Context, req *connect.Request[v1.RunScheduleRequest]) (*connect.Response[v1.RunScheduleResponse], error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	scheduleID := strings.TrimSpace(req.Msg.ScheduleId)
	if scheduleID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("schedule_id is required"))
	}

	var sched db.WorkloadSchedule
	if err := q.First(&sched, "id = ?", scheduleID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("schedule not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	exec, _ := s.executeSchedule(ctx, &sched, "manual")
	if exec == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to execute schedule"))
	}

	return connect.NewResponse(&v1.RunScheduleResponse{Execution: executionToProto(exec)}), nil
}

// ListScheduleExecutions returns past execution logs for a schedule.
func (s *ScheduleService) ListScheduleExecutions(ctx context.Context, req *connect.Request[v1.ListScheduleExecutionsRequest]) (*connect.Response[v1.ListScheduleExecutionsResponse], error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	scheduleID := strings.TrimSpace(req.Msg.ScheduleId)
	if scheduleID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("schedule_id is required"))
	}

	limit := int(req.Msg.Limit)
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var execs []db.ScheduleExecution
	if err := q.Where("schedule_id = ?", scheduleID).Order("started_at DESC").Limit(limit).Find(&execs).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	out := make([]*v1.ScheduleExecution, len(execs))
	for i := range execs {
		out[i] = executionToProto(&execs[i])
	}

	return connect.NewResponse(&v1.ListScheduleExecutionsResponse{Executions: out}), nil
}

// RunBackgroundLoop periodically inspects due schedules and executes them.
func (s *ScheduleService) RunBackgroundLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runDueSchedules(ctx)
		}
	}
}

func (s *ScheduleService) runDueSchedules(ctx context.Context) {
	now := time.Now().UTC()
	var due []db.WorkloadSchedule
	// Query all enabled schedules due for execution across orgs
	err := s.deps.Store.DB().
		Where("enabled = ? AND next_run_at <= ?", true, now).
		Limit(50).
		Find(&due).Error
	if err != nil {
		s.logger.Error("failed to query due schedules", "err", err)
		return
	}

	for i := range due {
		sched := due[i]
		s.logger.Info("executing scheduled task", "schedule_id", sched.ID, "workload_id", sched.WorkloadID, "name", sched.Name)

		// Context with tenant identity
		execCtx := principal.WithPrincipal(ctx, principal.Principal{Kind: principal.KindSystem, OrgID: sched.OrgID})
		_, execErr := s.executeSchedule(execCtx, &sched, "cron")
		if execErr != nil {
			s.logger.Warn("scheduled task execution failed", "schedule_id", sched.ID, "err", execErr)
		}
	}
}

func (s *ScheduleService) executeSchedule(ctx context.Context, sched *db.WorkloadSchedule, triggeredBy string) (*db.ScheduleExecution, error) {
	startTime := time.Now().UTC()

	execRecord := &db.ScheduleExecution{
		ID:          uuid.NewString(),
		OrgID:       sched.OrgID,
		ScheduleID:  sched.ID,
		WorkloadID:  sched.WorkloadID,
		TriggeredBy: triggeredBy,
		Status:      "running",
		StartedAt:   startTime,
	}

	// Persist initial execution record
	_ = s.deps.Store.DB().Create(execRecord).Error

	var w db.Workload
	if err := s.deps.Store.DB().Where("id = ? AND org_id = ?", sched.WorkloadID, sched.OrgID).First(&w).Error; err != nil {
		return s.finishExecution(execRecord, "", fmt.Errorf("load workload: %w", err), startTime)
	}

	var output string
	var execErr error

	switch sched.ActionType {
	case "command":
		output, execErr = s.executeCommand(ctx, &w, sched.Payload)
	case "start":
		output, execErr = s.executeStart(ctx, &w)
	case "stop":
		output, execErr = s.executeStop(ctx, &w)
	case "restart":
		output, execErr = s.executeRestart(ctx, &w)
	case "backup":
		output, execErr = s.executeBackup(ctx, &w, sched.Payload)
	default:
		execErr = fmt.Errorf("unknown action type: %s", sched.ActionType)
	}

	// Update schedule next run time and last run time
	now := time.Now().UTC()
	sched.LastRunAt = &now
	if parsed, err := s.cronParser.Parse(sched.CronExpression); err == nil {
		nextTime := parsed.Next(now)
		sched.NextRunAt = &nextTime
	}
	_ = s.deps.Store.DB().Model(sched).Updates(map[string]any{
		"last_run_at": sched.LastRunAt,
		"next_run_at": sched.NextRunAt,
	}).Error

	return s.finishExecution(execRecord, output, execErr, startTime)
}

func (s *ScheduleService) finishExecution(rec *db.ScheduleExecution, output string, err error, startedAt time.Time) (*db.ScheduleExecution, error) {
	now := time.Now().UTC()
	rec.FinishedAt = &now
	rec.DurationMs = time.Since(startedAt).Milliseconds()
	rec.Output = output

	if err != nil {
		rec.Status = "failed"
		rec.Error = err.Error()
	} else {
		rec.Status = "success"
	}

	_ = s.deps.Store.DB().Save(rec).Error
	return rec, err
}

func (s *ScheduleService) executeCommand(ctx context.Context, w *db.Workload, cmd string) (string, error) {
	if s.dispatcher == nil || !s.dispatcher.IsConnected(w.NodeID) {
		return "", errors.New("node agent is offline")
	}

	commandID := uuid.NewString()
	waitCh := s.dispatcher.ExpectCommand(commandID)
	defer s.dispatcher.CancelCommand(commandID)

	ok := s.dispatcher.DispatchContext(ctx, w.NodeID, &v1.ControlMessage{
		Payload: &v1.ControlMessage_RunCommand{
			RunCommand: &v1.ControlRunCommand{
				CommandId:  commandID,
				WorkloadId: w.ID,
				Command:    cmd,
			},
		},
	})
	if !ok {
		return "", errors.New("failed to dispatch command to node agent")
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(15 * time.Second):
		return "", errors.New("command execution timed out")
	case res := <-waitCh:
		if !res.Success {
			return res.Output, fmt.Errorf("command returned error: %s", res.Error)
		}
		return res.Output, nil
	}
}

func (s *ScheduleService) executeStart(ctx context.Context, w *db.Workload) (string, error) {
	if s.dispatcher == nil || !s.dispatcher.IsConnected(w.NodeID) {
		return "", errors.New("node agent is offline")
	}

	ok := s.dispatcher.DispatchContext(ctx, w.NodeID, &v1.ControlMessage{
		Payload: &v1.ControlMessage_AssignWorkload{
			AssignWorkload: &v1.ControlWorkloadAssignment{
				Workload: workloadToProto(w),
				Start:    true,
			},
		},
	})
	if !ok {
		return "", errors.New("failed to dispatch start assignment to node agent")
	}

	w.Status = "running"
	_ = s.deps.Store.DB().Model(w).Update("status", "running").Error
	return "workload start signaled", nil
}

func (s *ScheduleService) executeStop(ctx context.Context, w *db.Workload) (string, error) {
	if s.dispatcher == nil || !s.dispatcher.IsConnected(w.NodeID) {
		return "", errors.New("node agent is offline")
	}

	ok := s.dispatcher.DispatchContext(ctx, w.NodeID, &v1.ControlMessage{
		Payload: &v1.ControlMessage_StopWorkload{
			StopWorkload: &v1.ControlWorkloadStop{
				WorkloadId: w.ID,
			},
		},
	})
	if !ok {
		return "", errors.New("failed to dispatch stop to node agent")
	}

	w.Status = "stopped"
	_ = s.deps.Store.DB().Model(w).Update("status", "stopped").Error
	return "workload stop signaled", nil
}

func (s *ScheduleService) executeRestart(ctx context.Context, w *db.Workload) (string, error) {
	if _, err := s.executeStop(ctx, w); err != nil {
		return "", fmt.Errorf("stop before restart: %w", err)
	}
	time.Sleep(2 * time.Second)
	return s.executeStart(ctx, w)
}

func (s *ScheduleService) executeBackup(ctx context.Context, w *db.Workload, comment string) (string, error) {
	if s.dispatcher == nil || !s.dispatcher.IsConnected(w.NodeID) {
		return "", errors.New("node agent is offline")
	}

	backupID := uuid.NewString()
	name := fmt.Sprintf("cron-backup-%s", time.Now().UTC().Format("2006-01-02-150405"))
	if comment != "" {
		name = fmt.Sprintf("cron-%s-%s", sanitizeName(comment), time.Now().UTC().Format("2006-01-02-150405"))
	}

	backup := &db.WorkloadBackup{
		TenantBase: db.TenantBase{
			ID:    backupID,
			OrgID: w.OrgID,
		},
		WorkloadID: w.ID,
		NodeID:     w.NodeID,
		Name:       name,
		Status:     "in_progress",
	}

	if err := s.deps.Store.DB().Create(backup).Error; err != nil {
		return "", fmt.Errorf("create backup record: %w", err)
	}

	cmdID := uuid.NewString()
	waiter := s.dispatcher.ExpectCreateBackup(cmdID)
	defer s.dispatcher.CancelCreateBackup(cmdID)

	ok := s.dispatcher.DispatchContext(ctx, w.NodeID, &v1.ControlMessage{
		Payload: &v1.ControlMessage_CreateBackup{
			CreateBackup: &v1.ControlCreateBackup{
				CommandId:  cmdID,
				WorkloadId: w.ID,
				BackupId:   backupID,
			},
		},
	})
	if !ok {
		return "", errors.New("failed to dispatch create backup to node agent")
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(90 * time.Second):
		return "", errors.New("backup operation timed out on node agent")
	case res := <-waiter:
		if !res.Success {
			return "", fmt.Errorf("backup failed: %s", res.Error)
		}
		backup.Status = "completed"
		backup.SizeBytes = res.SizeBytes
		backup.SHA256 = res.Sha256
		_ = s.deps.Store.DB().Save(backup).Error
		return fmt.Sprintf("backup %s completed (%d bytes)", name, res.SizeBytes), nil
	}
}

func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 32 {
		out = out[:32]
	}
	return out
}

func scheduleActionProtoToString(a v1.ScheduleActionType) string {
	switch a {
	case v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_COMMAND:
		return "command"
	case v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_RESTART:
		return "restart"
	case v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_START:
		return "start"
	case v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_STOP:
		return "stop"
	case v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_BACKUP:
		return "backup"
	default:
		return ""
	}
}

func scheduleActionStringToProto(s string) v1.ScheduleActionType {
	switch s {
	case "command":
		return v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_COMMAND
	case "restart":
		return v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_RESTART
	case "start":
		return v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_START
	case "stop":
		return v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_STOP
	case "backup":
		return v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_BACKUP
	default:
		return v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_UNSPECIFIED
	}
}

func scheduleExecutionStatusStringToProto(s string) v1.ScheduleExecutionStatus {
	switch s {
	case "running":
		return v1.ScheduleExecutionStatus_SCHEDULE_EXECUTION_STATUS_RUNNING
	case "success":
		return v1.ScheduleExecutionStatus_SCHEDULE_EXECUTION_STATUS_SUCCESS
	case "failed":
		return v1.ScheduleExecutionStatus_SCHEDULE_EXECUTION_STATUS_FAILED
	default:
		return v1.ScheduleExecutionStatus_SCHEDULE_EXECUTION_STATUS_UNSPECIFIED
	}
}

func scheduleToProto(s *db.WorkloadSchedule) *v1.WorkloadSchedule {
	out := &v1.WorkloadSchedule{
		Id:             s.ID,
		OrgId:          s.OrgID,
		WorkloadId:     s.WorkloadID,
		Name:           s.Name,
		CronExpression: s.CronExpression,
		ActionType:     scheduleActionStringToProto(s.ActionType),
		Payload:        s.Payload,
		Enabled:        s.Enabled,
		CreatedAtUnix:  s.CreatedAt.Unix(),
		UpdatedAtUnix:  s.UpdatedAt.Unix(),
	}
	if s.LastRunAt != nil {
		out.LastRunAtUnix = s.LastRunAt.Unix()
	}
	if s.NextRunAt != nil {
		out.NextRunAtUnix = s.NextRunAt.Unix()
	}
	return out
}

func executionToProto(e *db.ScheduleExecution) *v1.ScheduleExecution {
	out := &v1.ScheduleExecution{
		Id:            e.ID,
		ScheduleId:    e.ScheduleID,
		WorkloadId:    e.WorkloadID,
		TriggeredBy:   e.TriggeredBy,
		Status:        scheduleExecutionStatusStringToProto(e.Status),
		Output:        e.Output,
		Error:         e.Error,
		DurationMs:    e.DurationMs,
		StartedAtUnix: e.StartedAt.Unix(),
	}
	if e.FinishedAt != nil {
		out.FinishedAtUnix = e.FinishedAt.Unix()
	}
	return out
}
