package svc

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/google/uuid"
)

// GetWorkloadMetrics retrieves real-time CPU, RAM, disk, network, player count, and TPS for a workload.
func (s *WorkloadService) GetWorkloadMetrics(ctx context.Context, req *connect.Request[v1.GetWorkloadMetricsRequest]) (*connect.Response[v1.GetWorkloadMetricsResponse], error) {
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

	// 1. Check if cached metrics from recent heartbeats exist and are fresh (<30s)
	if s.dispatcher != nil {
		if cached := s.dispatcher.GetWorkloadMetrics(w.ID); cached != nil {
			if cached.UpdatedAt != nil && time.Since(cached.UpdatedAt.AsTime()) < 30*time.Second {
				return connect.NewResponse(&v1.GetWorkloadMetricsResponse{
					Metrics: cached,
				}), nil
			}
		}
	}

	// 2. Fall back to on-demand query if node is connected
	if s.dispatcher == nil || !s.dispatcher.IsConnected(w.NodeID) {
		// If node is offline, but we have ANY cached metric, return it rather than failing
		if s.dispatcher != nil {
			if cached := s.dispatcher.GetWorkloadMetrics(w.ID); cached != nil {
				return connect.NewResponse(&v1.GetWorkloadMetricsResponse{
					Metrics: cached,
				}), nil
			}
		}
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("node agent is offline"))
	}

	cmdID := uuid.NewString()
	waiter := s.dispatcher.ExpectMetrics(cmdID)
	defer s.dispatcher.CancelMetrics(cmdID)

	ok := s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
		Payload: &v1.ControlMessage_GetWorkloadMetrics{
			GetWorkloadMetrics: &v1.ControlGetWorkloadMetrics{
				CommandId:  cmdID,
				WorkloadId: w.ID,
			},
		},
	})
	if !ok {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("failed to dispatch metrics request to node"))
	}

	select {
	case res := <-waiter:
		if !res.Success {
			return nil, connect.NewError(connect.CodeInternal, errors.New(res.Error))
		}
		return connect.NewResponse(&v1.GetWorkloadMetricsResponse{
			Metrics: res.Metrics,
		}), nil
	case <-time.After(5 * time.Second):
		if cached := s.dispatcher.GetWorkloadMetrics(w.ID); cached != nil {
			return connect.NewResponse(&v1.GetWorkloadMetricsResponse{
				Metrics: cached,
			}), nil
		}
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for node metrics response"))
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
	}
}
