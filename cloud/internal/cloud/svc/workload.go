package svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/billing"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/notify"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/portalloc"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/google/uuid"
)

// WorkloadService implements WorkloadServiceHandler directly against
// db.Store (no domain package exists yet).
//
// Control-plane side only: create/update/delete/list/status transitions and
// event history are recorded here. Anything that must run inside a
// container (start/stop/restart execution, console commands, live logs) is
// dispatched to the node agent; where no agent round-trip exists yet the
// RPC records intent and returns errNodeExecutes so callers can tell
// "accepted by control plane" from "executed on node".
type WorkloadService struct {
	deps       Deps
	dispatcher *AgentDispatcher
}

func workloadToProto(w *db.Workload) *v1.Workload {
	out := &v1.Workload{
		Id:           w.ID,
		OrgId:        w.OrgID,
		NodeId:       w.NodeID,
		Name:         w.Name,
		Status:       workloadStatusToProto(w.Status),
		ContainerId:  w.ContainerID,
		HostPort:     int32(w.HostPort),
		StatusDetail: w.StatusDetail,
		Hostname:     w.Hostname,
		CreatedBy:    w.CreatedBy,
		CreatedAt:    ts(w.CreatedAt),
		UpdatedAt:    ts(w.UpdatedAt),
	}
	if w.Spec != "" {
		var s struct {
			Loader           string `json:"loader"`
			MinecraftVersion string `json:"minecraft_version"`
			MemoryMB         int    `json:"memory_mb"`
			Hostname         string `json:"hostname"`
		}
		if json.Unmarshal([]byte(w.Spec), &s) == nil {
			out.Spec = &v1.WorkloadSpec{
				Loader:           s.Loader,
				MinecraftVersion: s.MinecraftVersion,
				MemoryMb:         int64(s.MemoryMB),
				Hostname:         s.Hostname,
				HostPort:         int32(w.HostPort),
			}
		}
	}
	return out
}

func eventToProto(e *db.WorkloadEvent) *v1.WorkloadEvent {
	return &v1.WorkloadEvent{
		Id:         e.ID,
		WorkloadId: e.WorkloadID,
		Kind:       e.Kind,
		Message:    e.Message,
		CreatedAt:  ts(e.CreatedAt),
	}
}

// recordEvent appends a status breadcrumb. Event-write failure never fails
// the RPC it annotates.
func (s *WorkloadService) recordEvent(ctx context.Context, workloadID, kind, msg string) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return
	}
	_ = q.Create(&db.WorkloadEvent{
		TenantBase: db.TenantBase{ID: uuid.NewString(), OrgID: principal.OrgID(ctx)},
		WorkloadID: workloadID,
		Kind:       kind,
		Message:    msg,
	}).Error
}

func (s *WorkloadService) load(ctx context.Context, id string) (*db.Workload, error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, err
	}
	var w db.Workload
	if err := q.Where("id = ?", id).First(&w).Error; err != nil {
		return nil, errWorkloadNotFound
	}
	return &w, nil
}

// ListWorkloads lists workloads with optional node/status/name filters.
func (s *WorkloadService) ListWorkloads(ctx context.Context, req *connect.Request[v1.ListWorkloadsRequest]) (*connect.Response[v1.ListWorkloadsResponse], error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}
	m := req.Msg
	fq := q.Model(&db.Workload{})
	if v := strings.TrimSpace(m.NodeId); v != "" {
		fq = fq.Where("node_id = ?", v)
	}
	if v := workloadStatusToString(m.Status); v != "" {
		fq = fq.Where("status = ?", v)
	}
	if v := strings.TrimSpace(m.NameContains); v != "" {
		fq = fq.Where("name LIKE ?", "%"+v+"%")
	}
	var total int64
	if err := fq.Count(&total).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errWorkloadList)
	}
	limit, offset := page(m.Page, 50)
	var rows []db.Workload
	if err := fq.Order("created_at DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errWorkloadList)
	}
	out := make([]*v1.Workload, 0, len(rows))
	for i := range rows {
		out = append(out, workloadToProto(&rows[i]))
	}
	return connect.NewResponse(&v1.ListWorkloadsResponse{Workloads: out, Page: pageResp(int(total), limit, offset)}), nil
}

// GetWorkload returns one workload by id.
func (s *WorkloadService) GetWorkload(ctx context.Context, req *connect.Request[v1.GetWorkloadRequest]) (*connect.Response[v1.GetWorkloadResponse], error) {
	w, err := s.load(ctx, req.Msg.Id)
	if err != nil {
		if err == errNoOrgCtx {
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewResponse(&v1.GetWorkloadResponse{Workload: workloadToProto(w)}), nil
}

// CreateWorkload records a pending workload. Placement validation
// (capacity admission) is the caller's job via the capacity package.
func (s *WorkloadService) CreateWorkload(ctx context.Context, req *connect.Request[v1.CreateWorkloadRequest]) (*connect.Response[v1.CreateWorkloadResponse], error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}
	m := req.Msg
	if strings.TrimSpace(m.Name) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errWorkloadName)
	}
	if strings.TrimSpace(m.NodeId) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errWorkloadNode)
	}

	orgID := principal.OrgID(ctx)

	// Enforce quota if billing is active
	if s.deps.Billing != nil {
		var org db.Org
		if err := s.deps.Store.Unscoped().WithContext(ctx).Where("id = ?", orgID).First(&org).Error; err == nil {
			plan := s.deps.Billing.Catalog().GetOrDefault(org.Plan)
			var count int64
			_ = q.Model(&db.Workload{}).Where("status NOT IN ('terminated', 'deleted')").Count(&count).Error

			reqRAM := int64(0)
			if m.Spec != nil && m.Spec.MemoryMb > 0 {
				reqRAM = m.Spec.MemoryMb
			}
			if err := s.deps.Billing.Check(plan, billing.Usage{WorkloadCount: int(count)}, billing.Usage{WorkloadCount: 1, AllocatedRAMMB: reqRAM}); err != nil {
				return nil, connect.NewError(connect.CodeResourceExhausted, err)
			}
		}
	}

	p, _ := principal.From(ctx)
	w := &db.Workload{
		TenantBase: db.TenantBase{ID: uuid.NewString(), OrgID: orgID},
		NodeID:     strings.TrimSpace(m.NodeId),
		Name:       strings.TrimSpace(m.Name),
		Status:     "pending",
		CreatedBy:  p.UserID,
	}

	requestedPort := 0
	if m.Spec != nil && m.Spec.HostPort > 0 {
		requestedPort = int(m.Spec.HostPort)
	}
	alloc := portalloc.NewAllocator(0, 0)
	allocatedPort, err := alloc.Allocate(ctx, q, w.NodeID, requestedPort, "")
	if err != nil {
		if errors.Is(err, portalloc.ErrPortConflict) {
			return nil, connect.NewError(connect.CodeAlreadyExists, err)
		}
		if errors.Is(err, portalloc.ErrNoPortsAvailable) {
			return nil, connect.NewError(connect.CodeResourceExhausted, err)
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	w.HostPort = allocatedPort

	if m.Spec != nil {
		raw, _ := json.Marshal(map[string]any{
			"loader":            m.Spec.Loader,
			"minecraft_version": m.Spec.MinecraftVersion,
			"memory_mb":         m.Spec.MemoryMb,
			"hostname":          m.Spec.Hostname,
			"host_port":         allocatedPort,
		})
		w.Spec = string(raw)
		w.Hostname = m.Spec.Hostname
	}
	if err := q.Create(w).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errWorkloadCreate)
	}
	s.recordEvent(ctx, w.ID, "created", "workload created, pending placement")

	if s.deps.Notifier != nil {
		s.deps.Notifier.Dispatch(ctx, notify.WebhookPayload{
			EventID:   uuid.NewString(),
			EventType: "workload.created",
			OrgID:     orgID,
			Timestamp: time.Now().Unix(),
			Data: map[string]any{
				"workload_id": w.ID,
				"name":        w.Name,
				"node_id":     w.NodeID,
			},
		})
	}

	if s.dispatcher != nil && w.NodeID != "" {
		s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
			Payload: &v1.ControlMessage_AssignWorkload{
				AssignWorkload: &v1.ControlWorkloadAssignment{
					CommandId: uuid.NewString(),
					Workload:  workloadToProto(w),
					Start:     false,
				},
			},
		})
	}

	return connect.NewResponse(&v1.CreateWorkloadResponse{Workload: workloadToProto(w)}), nil
}

// UpdateWorkload patches name/spec on a pending workload.
func (s *WorkloadService) UpdateWorkload(ctx context.Context, req *connect.Request[v1.UpdateWorkloadRequest]) (*connect.Response[v1.UpdateWorkloadResponse], error) {
	w, err := s.load(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}
	updates := map[string]any{}
	if req.Msg.Name != nil {
		if v := strings.TrimSpace(*req.Msg.Name); v != "" {
			updates["name"] = v
		}
	}
	if req.Msg.Spec != nil {
		newPort := w.HostPort
		if req.Msg.Spec.HostPort > 0 && int(req.Msg.Spec.HostPort) != w.HostPort {
			alloc := portalloc.NewAllocator(0, 0)
			allocatedPort, err := alloc.Allocate(ctx, q, w.NodeID, int(req.Msg.Spec.HostPort), w.ID)
			if err != nil {
				if errors.Is(err, portalloc.ErrPortConflict) {
					return nil, connect.NewError(connect.CodeAlreadyExists, err)
				}
				return nil, connect.NewError(connect.CodeInvalidArgument, err)
			}
			newPort = allocatedPort
			updates["host_port"] = newPort
		}
		raw, _ := json.Marshal(map[string]any{
			"loader":            req.Msg.Spec.Loader,
			"minecraft_version": req.Msg.Spec.MinecraftVersion,
			"memory_mb":         req.Msg.Spec.MemoryMb,
			"hostname":          req.Msg.Spec.Hostname,
			"host_port":         newPort,
		})
		updates["spec"] = string(raw)
		updates["hostname"] = req.Msg.Spec.Hostname
	}
	if len(updates) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errOrgEmptyUpdate)
	}
	if err := q.Model(&db.Workload{}).Where("id = ?", w.ID).Updates(updates).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errWorkloadUpdate)
	}
	w, _ = s.load(ctx, w.ID)
	return connect.NewResponse(&v1.UpdateWorkloadResponse{Workload: workloadToProto(w)}), nil
}

// DeleteWorkload removes a workload row. Live container teardown is the
// node agent's job; delete_data is recorded for it.
func (s *WorkloadService) DeleteWorkload(ctx context.Context, req *connect.Request[v1.DeleteWorkloadRequest]) (*connect.Response[v1.DeleteWorkloadResponse], error) {
	w, err := s.load(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}
	if err := q.Where("id = ?", req.Msg.Id).Delete(&db.Workload{}).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errWorkloadDelete)
	}

	if s.dispatcher != nil && w.NodeID != "" {
		s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
			Payload: &v1.ControlMessage_DeleteWorkload{
				DeleteWorkload: &v1.ControlWorkloadDelete{
					CommandId:  uuid.NewString(),
					WorkloadId: w.ID,
					DeleteData: req.Msg.DeleteData,
				},
			},
		})
	}

	if s.deps.Notifier != nil {
		s.deps.Notifier.Dispatch(ctx, notify.WebhookPayload{
			EventID:   uuid.NewString(),
			EventType: "workload.deleted",
			OrgID:     principal.OrgID(ctx),
			Timestamp: time.Now().Unix(),
			Data: map[string]any{
				"workload_id": req.Msg.Id,
			},
		})
	}

	return connect.NewResponse(&v1.DeleteWorkloadResponse{}), nil
}

// intend records a lifecycle intent and returns the workload. Container
// execution belongs to the node agent.
func (s *WorkloadService) intend(ctx context.Context, id, status, kind, msg string) (*db.Workload, error) {
	w, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, errNoOrgCtx
	}
	if err := q.Model(&db.Workload{}).Where("id = ?", id).Updates(map[string]any{"status": status, "status_detail": msg}).Error; err != nil {
		return nil, errWorkloadUpdate
	}
	w.Status, w.StatusDetail = status, msg
	s.recordEvent(ctx, id, kind, msg)
	return w, nil
}

// StartWorkload records start intent; the agent starts the container.
func (s *WorkloadService) StartWorkload(ctx context.Context, req *connect.Request[v1.StartWorkloadRequest]) (*connect.Response[v1.StartWorkloadResponse], error) {
	w, err := s.intend(ctx, req.Msg.Id, "starting", "start_requested", "start requested, awaiting node agent")
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	if s.dispatcher != nil && w.NodeID != "" {
		s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
			Payload: &v1.ControlMessage_AssignWorkload{
				AssignWorkload: &v1.ControlWorkloadAssignment{
					CommandId: uuid.NewString(),
					Workload:  workloadToProto(w),
					Start:     true,
				},
			},
		})
	}

	if s.deps.Notifier != nil {
		s.deps.Notifier.Dispatch(ctx, notify.WebhookPayload{
			EventID:   uuid.NewString(),
			EventType: "workload.started",
			OrgID:     principal.OrgID(ctx),
			Timestamp: time.Now().Unix(),
			Data: map[string]any{
				"workload_id": w.ID,
			},
		})
	}

	return connect.NewResponse(&v1.StartWorkloadResponse{Workload: workloadToProto(w)}), nil
}

// StopWorkload records stop intent; the agent stops the container.
func (s *WorkloadService) StopWorkload(ctx context.Context, req *connect.Request[v1.StopWorkloadRequest]) (*connect.Response[v1.StopWorkloadResponse], error) {
	w, err := s.intend(ctx, req.Msg.Id, "stopping", "stop_requested", "stop requested, awaiting node agent")
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	if s.dispatcher != nil && w.NodeID != "" {
		s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
			Payload: &v1.ControlMessage_StopWorkload{
				StopWorkload: &v1.ControlWorkloadStop{
					CommandId:      uuid.NewString(),
					WorkloadId:     w.ID,
					TimeoutSeconds: 30,
				},
			},
		})
	}

	if s.deps.Notifier != nil {
		s.deps.Notifier.Dispatch(ctx, notify.WebhookPayload{
			EventID:   uuid.NewString(),
			EventType: "workload.stopped",
			OrgID:     principal.OrgID(ctx),
			Timestamp: time.Now().Unix(),
			Data: map[string]any{
				"workload_id": w.ID,
			},
		})
	}

	return connect.NewResponse(&v1.StopWorkloadResponse{Workload: workloadToProto(w)}), nil
}

// RestartWorkload records restart intent; the agent restarts the container.
func (s *WorkloadService) RestartWorkload(ctx context.Context, req *connect.Request[v1.RestartWorkloadRequest]) (*connect.Response[v1.RestartWorkloadResponse], error) {
	w, err := s.intend(ctx, req.Msg.Id, "starting", "restart_requested", "restart requested, awaiting node agent")
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	if s.dispatcher != nil && w.NodeID != "" {
		s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
			Payload: &v1.ControlMessage_AssignWorkload{
				AssignWorkload: &v1.ControlWorkloadAssignment{
					CommandId: uuid.NewString(),
					Workload:  workloadToProto(w),
					Start:     true,
				},
			},
		})
	}

	return connect.NewResponse(&v1.RestartWorkloadResponse{Workload: workloadToProto(w)}), nil
}

// StreamWorkloadLogs tails live console output streamed from the node agent.
func (s *WorkloadService) StreamWorkloadLogs(ctx context.Context, req *connect.Request[v1.StreamWorkloadLogsRequest], stream *connect.ServerStream[v1.WorkloadLogLine]) error {
	if req.Msg.Id == "" {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("workload id is required"))
	}
	w, err := s.load(ctx, req.Msg.Id)
	if err != nil {
		return connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}
	if s.dispatcher == nil {
		return connect.NewError(connect.CodeUnavailable, errors.New("dispatcher unavailable"))
	}

	logCh, cancel := s.dispatcher.SubscribeLogs(w.ID)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return nil
		case line, ok := <-logCh:
			if !ok {
				return nil
			}
			if err := stream.Send(line); err != nil {
				return err
			}
		}
	}
}

// SendWorkloadCommand runs a console command over RCON via the node agent.
func (s *WorkloadService) SendWorkloadCommand(ctx context.Context, req *connect.Request[v1.SendWorkloadCommandRequest]) (*connect.Response[v1.SendWorkloadCommandResponse], error) {
	if req.Msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload id is required"))
	}
	if strings.TrimSpace(req.Msg.Command) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("command cannot be empty"))
	}
	w, err := s.load(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}
	if w.NodeID == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("workload is not placed on any node"))
	}
	if s.dispatcher == nil || !s.dispatcher.IsConnected(w.NodeID) {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("node agent is offline"))
	}

	commandID := uuid.NewString()
	waitCh := s.dispatcher.ExpectCommand(commandID)
	defer s.dispatcher.CancelCommand(commandID)

	ok := s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
		Payload: &v1.ControlMessage_RunCommand{
			RunCommand: &v1.ControlRunCommand{
				CommandId:  commandID,
				WorkloadId: w.ID,
				Command:    req.Msg.Command,
			},
		},
	})
	if !ok {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("failed to dispatch command to node agent"))
	}

	select {
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
	case <-time.After(15 * time.Second):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for command output from node agent"))
	case res := <-waitCh:
		if !res.Success {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("command execution failed: %s", res.Error))
		}
		return connect.NewResponse(&v1.SendWorkloadCommandResponse{
			Output: res.Output,
		}), nil
	}
}

// ListWorkloadEvents returns the breadcrumb history of a workload.
func (s *WorkloadService) ListWorkloadEvents(ctx context.Context, req *connect.Request[v1.ListWorkloadEventsRequest]) (*connect.Response[v1.ListWorkloadEventsResponse], error) {
	if _, err := s.load(ctx, req.Msg.Id); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}
	limit, offset := page(req.Msg.Page, 50)
	var total int64
	fq := q.Model(&db.WorkloadEvent{}).Where("workload_id = ?", req.Msg.Id)
	if err := fq.Count(&total).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errWorkloadEvents)
	}
	var rows []db.WorkloadEvent
	if err := fq.Order("created_at DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errWorkloadEvents)
	}
	out := make([]*v1.WorkloadEvent, 0, len(rows))
	for i := range rows {
		out = append(out, eventToProto(&rows[i]))
	}
	return connect.NewResponse(&v1.ListWorkloadEventsResponse{Events: out, Page: pageResp(int(total), limit, offset)}), nil
}

// GetWorkloadConfig reads and parses workload configuration properties.
func (s *WorkloadService) GetWorkloadConfig(ctx context.Context, req *connect.Request[v1.GetWorkloadConfigRequest]) (*connect.Response[v1.GetWorkloadConfigResponse], error) {
	if req.Msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload id is required"))
	}
	w, err := s.load(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}
	if w.NodeID == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("workload is not placed on any node"))
	}
	if s.dispatcher == nil || !s.dispatcher.IsConnected(w.NodeID) {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("node agent is offline"))
	}

	targetFile := strings.TrimSpace(req.Msg.File)
	if targetFile == "" {
		targetFile = "server.properties"
	}

	commandID := uuid.NewString()
	chunkCh := s.dispatcher.ExpectFileChunk(commandID)
	defer s.dispatcher.CancelFileChunk(commandID)

	ok := s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
		Payload: &v1.ControlMessage_FileRead{
			FileRead: &v1.ControlReadFile{
				CommandId:  commandID,
				WorkloadId: w.ID,
				Path:       targetFile,
			},
		},
	})
	if !ok {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("failed to dispatch read request to node agent"))
	}

	var contentBuilder strings.Builder
	for {
		select {
		case <-ctx.Done():
			return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
		case <-time.After(15 * time.Second):
			return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for config file content from node agent"))
		case chunk, ok := <-chunkCh:
			if !ok {
				goto DoneRead
			}
			if !chunk.Success {
				return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("read config file failed: %s", chunk.Error))
			}
			contentBuilder.Write(chunk.Chunk)
			if chunk.IsLast {
				goto DoneRead
			}
		}
	}

DoneRead:
	raw := contentBuilder.String()
	props := ParseProperties(raw)

	return connect.NewResponse(&v1.GetWorkloadConfigResponse{
		Properties: props.ToMap(),
		Raw:        raw,
	}), nil
}

// UpdateWorkloadConfig mutates configuration properties and optionally triggers a reload/restart.
func (s *WorkloadService) UpdateWorkloadConfig(ctx context.Context, req *connect.Request[v1.UpdateWorkloadConfigRequest]) (*connect.Response[v1.UpdateWorkloadConfigResponse], error) {
	if req.Msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload id is required"))
	}
	w, err := s.load(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}
	if w.NodeID == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("workload is not placed on any node"))
	}
	if s.dispatcher == nil || !s.dispatcher.IsConnected(w.NodeID) {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("node agent is offline"))
	}

	targetFile := strings.TrimSpace(req.Msg.File)
	if targetFile == "" {
		targetFile = "server.properties"
	}

	// Read current file first to preserve comments and structure
	readCmdID := uuid.NewString()
	chunkCh := s.dispatcher.ExpectFileChunk(readCmdID)
	defer s.dispatcher.CancelFileChunk(readCmdID)

	ok := s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
		Payload: &v1.ControlMessage_FileRead{
			FileRead: &v1.ControlReadFile{
				CommandId:  readCmdID,
				WorkloadId: w.ID,
				Path:       targetFile,
			},
		},
	})
	if !ok {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("failed to dispatch read request to node agent"))
	}

	var currentBuilder strings.Builder
	for {
		select {
		case <-ctx.Done():
			return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
		case <-time.After(15 * time.Second):
			return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out reading existing config from node agent"))
		case chunk, ok := <-chunkCh:
			if !ok {
				goto DoneCurrentRead
			}
			if chunk.Success {
				currentBuilder.Write(chunk.Chunk)
			}
			if chunk.IsLast {
				goto DoneCurrentRead
			}
		}
	}

DoneCurrentRead:
	props := ParseProperties(currentBuilder.String())
	for k, v := range req.Msg.Properties {
		props.Set(k, v)
	}
	for _, k := range req.Msg.RemoveKeys {
		props.Delete(k)
	}
	newRawContent := props.Serialize()
	resultingProps := props.ToMap()

	// Write updated file back to the workload
	writeCmdID := uuid.NewString()
	writeCh := s.dispatcher.ExpectFileWrite(writeCmdID)
	defer s.dispatcher.CancelFileWrite(writeCmdID)

	ok = s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
		Payload: &v1.ControlMessage_FileWrite{
			FileWrite: &v1.ControlWriteFileChunk{
				CommandId:  writeCmdID,
				WorkloadId: w.ID,
				Path:       targetFile,
				Chunk:      []byte(newRawContent),
				IsLast:     true,
				Mode:       0644,
			},
		},
	})
	if !ok {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("failed to dispatch write request to node agent"))
	}

	select {
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
	case <-time.After(15 * time.Second):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out writing config to node agent"))
	case res := <-writeCh:
		if !res.Success {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("write config failed: %s", res.Error))
		}
	}

	actionTaken := "saved"
	if req.Msg.RestartOrReload {
		// If workload is currently running, try a reload or restart
		if w.Status == "running" {
			// Trigger reload command via RCON
			cmdID := uuid.NewString()
			cmdCh := s.dispatcher.ExpectCommand(cmdID)
			defer s.dispatcher.CancelCommand(cmdID)

			if ok := s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
				Payload: &v1.ControlMessage_RunCommand{
					RunCommand: &v1.ControlRunCommand{
						CommandId:  cmdID,
						WorkloadId: w.ID,
						Command:    "reload",
					},
				},
			}); ok {
				select {
				case <-time.After(5 * time.Second):
					actionTaken = "saved (reload timed out)"
				case <-cmdCh:
					actionTaken = "saved and reloaded"
				}
			}
		}
	}

	s.recordEvent(ctx, w.ID, "config_updated", fmt.Sprintf("updated configuration file %s (%s)", targetFile, actionTaken))

	return connect.NewResponse(&v1.UpdateWorkloadConfigResponse{
		Properties:  resultingProps,
		ActionTaken: actionTaken,
	}), nil
}
