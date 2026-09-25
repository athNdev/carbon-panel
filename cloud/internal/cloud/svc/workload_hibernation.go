package svc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/internal/proxy"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/google/uuid"
)

// HibernateWorkload pauses or deep-sleeps an idle workload.
func (s *WorkloadService) HibernateWorkload(ctx context.Context, req *connect.Request[v1.HibernateWorkloadRequest]) (*connect.Response[v1.HibernateWorkloadResponse], error) {
	if req.Msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload id is required"))
	}

	w, err := s.load(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}

	if w.Status == "hibernated" {
		return connect.NewResponse(&v1.HibernateWorkloadResponse{
			Success: true,
			Status:  v1.WorkloadStatus_WORKLOAD_STATUS_HIBERNATED,
			Message: "workload is already hibernated",
		}), nil
	}

	if w.Status != "running" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("cannot hibernate workload in %s state", w.Status))
	}

	if w.NodeID == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("workload is not placed on any node"))
	}

	if s.dispatcher == nil || !s.dispatcher.IsConnected(w.NodeID) {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("node agent is not connected"))
	}

	cmdID := uuid.NewString()
	resCh := s.dispatcher.ExpectHibernate(cmdID)
	defer s.dispatcher.CancelHibernate(cmdID)

	reason := req.Msg.Reason
	if reason == "" {
		reason = "manual_hibernation"
	}

	mode := "pause"
	// check spec for configured mode
	if w.Spec != "" {
		protoW := workloadToProto(w)
		if protoW.Spec != nil && protoW.Spec.HibernationMode != "" {
			mode = protoW.Spec.HibernationMode
		}
	}

	ok := s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
		Payload: &v1.ControlMessage_HibernateWorkload{
			HibernateWorkload: &v1.ControlHibernateWorkload{
				CommandId:  cmdID,
				WorkloadId: w.ID,
				Mode:       mode,
			},
		},
	})
	if !ok {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("failed to dispatch hibernate to node agent"))
	}

	select {
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
	case <-time.After(15 * time.Second):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for node agent to hibernate workload"))
	case res := <-resCh:
		if !res.Success {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("hibernate failed: %s", res.Error))
		}
	}

	q, err := s.deps.Store.Org(ctx)
	if err == nil {
		_ = q.Model(&db.Workload{}).Where("id = ?", w.ID).Updates(map[string]any{
			"status":        "hibernated",
			"status_detail": fmt.Sprintf("hibernated: %s", reason),
		}).Error
	}

	s.recordEvent(ctx, w.ID, "hibernated", fmt.Sprintf("workload hibernated (%s)", reason))

	// Best effort: update ingress routes if router configured
	s.syncWorkloadRoute(ctx, w.ID, true)

	return connect.NewResponse(&v1.HibernateWorkloadResponse{
		Success: true,
		Status:  v1.WorkloadStatus_WORKLOAD_STATUS_HIBERNATED,
		Message: "workload successfully hibernated",
	}), nil
}

// WakeWorkload unpauses or starts a hibernated workload.
func (s *WorkloadService) WakeWorkload(ctx context.Context, req *connect.Request[v1.WakeWorkloadRequest]) (*connect.Response[v1.WakeWorkloadResponse], error) {
	if req.Msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload id is required"))
	}

	w, err := s.load(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}

	if w.Status == "running" {
		return connect.NewResponse(&v1.WakeWorkloadResponse{
			Success: true,
			Status:  v1.WorkloadStatus_WORKLOAD_STATUS_RUNNING,
			Message: "workload is already running",
		}), nil
	}

	if w.NodeID == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("workload is not placed on any node"))
	}

	if s.dispatcher == nil || !s.dispatcher.IsConnected(w.NodeID) {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("node agent is not connected"))
	}

	cmdID := uuid.NewString()
	resCh := s.dispatcher.ExpectWake(cmdID)
	defer s.dispatcher.CancelWake(cmdID)

	ok := s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
		Payload: &v1.ControlMessage_WakeWorkload{
			WakeWorkload: &v1.ControlWakeWorkload{
				CommandId:  cmdID,
				WorkloadId: w.ID,
			},
		},
	})
	if !ok {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("failed to dispatch wake to node agent"))
	}

	select {
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
	case <-time.After(20 * time.Second):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for node agent to wake workload"))
	case res := <-resCh:
		if !res.Success {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("wake failed: %s", res.Error))
		}
	}

	q, err := s.deps.Store.Org(ctx)
	if err == nil {
		_ = q.Model(&db.Workload{}).Where("id = ?", w.ID).Updates(map[string]any{
			"status":        "running",
			"status_detail": "container resumed from hibernation",
		}).Error
	}

	s.recordEvent(ctx, w.ID, "woken", "workload resumed from hibernation")

	// Best effort: update ingress routes if router configured
	s.syncWorkloadRoute(ctx, w.ID, false)

	return connect.NewResponse(&v1.WakeWorkloadResponse{
		Success: true,
		Status:  v1.WorkloadStatus_WORKLOAD_STATUS_RUNNING,
		Message: "workload successfully resumed",
	}), nil
}

// SyncIngressRoutes synchronizes hostname routing table for ingress / Gate proxy.
func (s *WorkloadService) SyncIngressRoutes(ctx context.Context, req *connect.Request[v1.SyncIngressRoutesRequest]) (*connect.Response[v1.SyncIngressRoutesResponse], error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}

	var workloads []db.Workload
	query := q.Model(&db.Workload{})
	if req.Msg.WorkloadId != "" {
		query = query.Where("id = ?", req.Msg.WorkloadId)
	} else {
		query = query.Where("status IN ('running', 'hibernated')")
	}

	if err := query.Find(&workloads).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	var routes []*v1.IngressRoute

	// Collect unique node IDs to resolve backend hosts
	nodeIDs := make(map[string]struct{})
	for _, w := range workloads {
		if w.NodeID != "" {
			nodeIDs[w.NodeID] = struct{}{}
		}
	}

	nodeMap := make(map[string]db.Node)
	if len(nodeIDs) > 0 {
		var ids []string
		for id := range nodeIDs {
			ids = append(ids, id)
		}
		var nodes []db.Node
		if err := q.Where("id IN ?", ids).Find(&nodes).Error; err == nil {
			for _, n := range nodes {
				nodeMap[n.ID] = n
			}
		}
	}

	for _, w := range workloads {
		backendHost := "127.0.0.1"
		if n, ok := nodeMap[w.NodeID]; ok {
			if n.PublicIP != "" {
				backendHost = n.PublicIP
			} else if n.Hostname != "" {
				backendHost = n.Hostname
			} else if n.PrivateIP != "" {
				backendHost = n.PrivateIP
			}
		}

		backendPort := int32(w.HostPort)
		if backendPort <= 0 {
			backendPort = 25565
		}

		hostname := w.Hostname
		if hostname == "" {
			prefixLen := 8
			if len(w.ID) < prefixLen {
				prefixLen = len(w.ID)
			}
			hostname = fmt.Sprintf("wl-%s.carbon.local", w.ID[:prefixLen])
		}

		isHibernated := w.Status == "hibernated"

		route := &v1.IngressRoute{
			WorkloadId:  w.ID,
			Hostname:    hostname,
			BackendHost: backendHost,
			BackendPort: backendPort,
			ProxyPort:   25565,
			Hibernated:  isHibernated,
			NodeId:      w.NodeID,
		}
		routes = append(routes, route)
	}

	// If Valkey/Redis endpoint is configured, broadcast/sync routes to proxy
	valkeyAddr := os.Getenv("VALKEY_ADDR")
	if valkeyAddr == "" {
		valkeyAddr = os.Getenv("REDIS_ADDR")
	}
	if valkeyAddr != "" {
		client := proxy.NewRespClient(valkeyAddr, os.Getenv("VALKEY_PASSWORD"))
		defer client.Close()

		syncCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()

		for _, r := range routes {
			_ = client.SetRoute(syncCtx, r.Hostname, &proxy.RouteInfo{
				ServerID:    r.WorkloadId,
				Hostname:    r.Hostname,
				BackendHost: r.BackendHost,
				BackendPort: int(r.BackendPort),
				ProxyPort:   int(r.ProxyPort),
				UpdatedAt:   time.Now().Unix(),
			})
		}
	}

	return connect.NewResponse(&v1.SyncIngressRoutesResponse{
		RoutesSynced: int32(len(routes)),
		Routes:       routes,
	}), nil
}

func (s *WorkloadService) syncWorkloadRoute(ctx context.Context, workloadID string, hibernated bool) {
	valkeyAddr := os.Getenv("VALKEY_ADDR")
	if valkeyAddr == "" {
		valkeyAddr = os.Getenv("REDIS_ADDR")
	}
	if valkeyAddr == "" {
		return
	}

	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = s.SyncIngressRoutes(bgCtx, connect.NewRequest(&v1.SyncIngressRoutesRequest{
			WorkloadId: workloadID,
		}))
	}()
}
