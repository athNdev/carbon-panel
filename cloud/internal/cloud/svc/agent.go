package svc

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/billing"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/node"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/notify"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// AgentService implements AgentServiceHandler: the control-plane side of
// the node agent protocol (join, server stream, command envelopes).
//
// Join mints node identity from a single-use join token. The stream carries
// agent→control messages (hello, heartbeat, workload status, logs, command
// results) and control→agent envelopes (welcome, assignments, stops,
// deletes, run-command, probe, disconnect). Command fan-out per workload is
// recorded as workload events; live dispatch state belongs to the w3-noded
// lane.
type AgentService struct {
	deps       Deps
	dispatcher *AgentDispatcher
}

// JoinNode redeems a single-use token and writes the node row.
func (s *AgentService) JoinNode(ctx context.Context, req *connect.Request[v1.JoinNodeRequest]) (*connect.Response[v1.JoinNodeResponse], error) {
	m := req.Msg
	if strings.TrimSpace(m.Token) == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errAgentJoin)
	}
	tok, err := s.deps.JoinTokens.Redeem(ctx, strings.TrimSpace(m.Token))
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errAgentJoin)
	}

	// Enforce node quota if billing is active
	if s.deps.Billing != nil {
		var org db.Org
		if err := s.deps.Store.Unscoped().WithContext(ctx).Where("id = ?", tok.OrgID).First(&org).Error; err == nil {
			plan := s.deps.Billing.Catalog().GetOrDefault(org.Plan)
			var count int64
			_ = s.deps.Store.Unscoped().WithContext(ctx).Model(&db.Node{}).Where("org_id = ?", tok.OrgID).Count(&count).Error
			if err := s.deps.Billing.Check(plan, billing.Usage{NodeCount: int(count)}, billing.Usage{NodeCount: 1}); err != nil {
				return nil, connect.NewError(connect.CodeResourceExhausted, err)
			}
		}
	}

	// The caller has no org yet: the token carries it. Scope all writes to
	// the token's org so a token can never create rows elsewhere.
	orgCtx := principal.WithPrincipal(ctx, principal.Principal{Kind: principal.KindNode, OrgID: tok.OrgID})
	cap := db.NodeCapacity{}
	if m.Capacity != nil {
		cap = db.NodeCapacity{VCPU: int(m.Capacity.Vcpu), RAMMB: int(m.Capacity.RamMb), DiskGB: int(m.Capacity.DiskGb)}
	}
	name := strings.TrimSpace(m.Hostname)
	if name == "" {
		name = "node-" + tok.ID[:8]
	}
	origin := tok.Origin
	if origin == "" {
		origin = "byo"
	}
	n, err := s.deps.Nodes.Register(orgCtx, node.RegisterRequest{
		Name:       name,
		Origin:     origin,
		NodeTypeID: tok.NodeTypeID,
		Capacity:   cap,
		Labels:     map[string]string{"agent_version": m.AgentVersion},
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errAgentJoin)
	}

	if s.deps.Notifier != nil {
		s.deps.Notifier.Dispatch(orgCtx, notify.WebhookPayload{
			EventID:   uuid.NewString(),
			EventType: "node.joined",
			OrgID:     tok.OrgID,
			Timestamp: time.Now().Unix(),
			Data: map[string]any{
				"node_id":  n.ID,
				"hostname": n.Hostname,
				"origin":   n.Origin,
			},
		})
	}

	nodeSvc := &NodeService{deps: s.deps}
	return connect.NewResponse(&v1.JoinNodeResponse{
		Identity: &v1.NodeIdentity{
			NodeId:                   n.ID,
			OrgId:                    tok.OrgID,
			ControlPlaneEndpoint:     s.deps.ControlPlaneURL,
			HeartbeatIntervalSeconds: 30,
		},
		Node: nodeSvc.nodeToProto(orgCtx, &n),
	}), nil
}

// RenewCredentials rotates a node client certificate. There is no CA lane
// yet, so this is a typed not-implemented rather than a fake certificate.
func (s *AgentService) RenewCredentials(ctx context.Context, req *connect.Request[v1.RenewCredentialsRequest]) (*connect.Response[v1.RenewCredentialsResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errAgentCreds)
}

// Connect serves the agent bidi stream: hello→welcome, heartbeats applied
// to the registry, workload transitions recorded, command results logged.
func (s *AgentService) Connect(ctx context.Context, stream *connect.BidiStream[v1.AgentMessage, v1.ControlMessage]) error {
	slog.Info("AgentService.Connect: stream opened")
	defer slog.Info("AgentService.Connect: stream returned")

	var currentSession *NodeSession
	defer func() {
		if currentSession != nil && s.dispatcher != nil {
			s.dispatcher.Unregister(currentSession.NodeID)
		}
	}()

	for {
		msg, err := stream.Receive()
		if err != nil {
			slog.Warn("AgentService.Connect: receive error", "err", err, "ctx_err", ctx.Err())
			return err
		}
		switch payload := msg.Payload.(type) {
		case *v1.AgentMessage_Hello:
			nodeID := payload.Hello.GetNodeId()
			slog.Info("AgentService.Connect: received Hello", "node_id", nodeID)
			if err := stream.Send(welcomeMsg(ctx, s, payload.Hello)); err != nil {
				slog.Warn("AgentService.Connect: send welcome error", "err", err)
				return err
			}
			if s.dispatcher != nil && nodeID != "" {
				currentSession = s.dispatcher.Register(nodeID)
				go func(sess *NodeSession) {
					for {
						select {
						case <-ctx.Done():
							return
						case outMsg, ok := <-sess.NextMessage():
							if !ok {
								return
							}
							if err := stream.Send(outMsg); err != nil {
								slog.Warn("AgentService.Connect: error sending control msg to node", "node_id", sess.NodeID, "err", err)
								return
							}
						}
					}
				}(currentSession)
				go s.reconcileNodeWorkloads(ctx, nodeID)
			}
		case *v1.AgentMessage_Heartbeat:
			slog.Info("AgentService.Connect: received Heartbeat", "node_id", payload.Heartbeat.GetNodeId())
			s.onHeartbeat(ctx, payload.Heartbeat)
		case *v1.AgentMessage_WorkloadStatus:
			s.onWorkloadStatus(ctx, payload.WorkloadStatus)
		case *v1.AgentMessage_Logs:
			// Log shipping is buffered node-side and tailed via the
			// workload RPCs; chunks here are acknowledged by receipt.
		case *v1.AgentMessage_CommandResult:
			s.onCommandResult(ctx, payload.CommandResult)
		default:
			// Unknown envelopes are ignored: old agents keep working.
		}
	}
}

func (s *AgentService) reconcileNodeWorkloads(ctx context.Context, nodeID string) {
	if s.deps.Store == nil || s.dispatcher == nil || nodeID == "" {
		return
	}
	var workloads []db.Workload
	err := s.deps.Store.Unscoped().WithContext(ctx).
		Where("node_id = ? AND status IN ('pending', 'starting', 'running')", nodeID).
		Find(&workloads).Error
	if err != nil {
		slog.Error("failed querying workloads to reconcile for node", "node_id", nodeID, "err", err)
		return
	}
	for i := range workloads {
		w := &workloads[i]
		slog.Info("reconciling workload to connected node", "node_id", nodeID, "workload_id", w.ID, "status", w.Status)
		s.dispatcher.Dispatch(nodeID, &v1.ControlMessage{
			Payload: &v1.ControlMessage_AssignWorkload{
				AssignWorkload: &v1.ControlWorkloadAssignment{
					CommandId: uuid.NewString(),
					Workload:  workloadToProto(w),
					Start:     w.Status == "starting" || w.Status == "running",
				},
			},
		})
	}
}

func welcomeMsg(ctx context.Context, s *AgentService, hello *v1.AgentHello) *v1.ControlMessage {
	draining := false
	if hello != nil && hello.NodeId != "" {
		if n, err := s.deps.Nodes.Get(ctx, hello.NodeId); err == nil {
			draining = n.Draining
		}
		if _, err := s.deps.Nodes.Heartbeat(ctx, hello.NodeId); err == nil {
			if n, err := s.deps.Nodes.Get(ctx, hello.NodeId); err == nil {
				labels := n.LabelMap()
				if hello.DockerVersion != "" {
					labels["docker_version"] = hello.DockerVersion
				}
				patch := node.NodePatch{Labels: &labels}
				if hello.Hostname != "" {
					patch.Hostname = &hello.Hostname
				}
				_, _ = s.deps.Nodes.Update(ctx, hello.NodeId, patch)
			}
		}
	}
	return &v1.ControlMessage{
		Payload: &v1.ControlMessage_Welcome{
			Welcome: &v1.ControlWelcome{
				ServerTime:               timestamppb.New(time.Now().UTC()),
				HeartbeatIntervalSeconds: 30,
				Draining:                 draining,
			},
		},
	}
}

func (s *AgentService) onHeartbeat(ctx context.Context, hb *v1.AgentHeartbeat) {
	if hb == nil || hb.NodeId == "" {
		return
	}
	p, ok := principal.From(ctx)
	if !ok || p.Kind != principal.KindNode {
		return
	}
	// Check previous node status to detect recovery from offline
	prevNode, _ := s.deps.Nodes.Get(ctx, hb.NodeId)

	// Heartbeats refresh last_seen and reconcile capacity if reported.
	n, err := s.deps.Nodes.Heartbeat(ctx, hb.NodeId)
	if err != nil {
		return
	}

	// If node was offline and recovered back to active
	if prevNode.Status == "offline" && n.Status == "active" {
		if s.deps.Notifier != nil {
			s.deps.Notifier.Dispatch(ctx, notify.WebhookPayload{
				EventID:   uuid.NewString(),
				EventType: "node.online",
				OrgID:     p.OrgID,
				Timestamp: time.Now().Unix(),
				Data: map[string]any{
					"node_id":  n.ID,
					"hostname": n.Hostname,
				},
			})
		}

		// Reconcile degraded workloads on this node back to running
		if q, err := s.deps.Store.Org(ctx); err == nil {
			var degraded []db.Workload
			if err := q.Where("node_id = ? AND status = ?", n.ID, "degraded").Find(&degraded).Error; err == nil {
				for _, wl := range degraded {
					if uq, err := s.deps.Store.Org(ctx); err == nil {
						_ = uq.Model(&db.Workload{}).Where("id = ?", wl.ID).Updates(map[string]any{
							"status":        "running",
							"status_detail": "node reconnected; heartbeat recovered",
						}).Error
					}
					if eq, err := s.deps.Store.Org(ctx); err == nil {
						_ = eq.Create(&db.WorkloadEvent{
							TenantBase: db.TenantBase{ID: uuid.NewString(), OrgID: p.OrgID},
							WorkloadID: wl.ID,
							Kind:       "recovery",
							Message:    "node heartbeats resumed; workload restored to running",
						}).Error
					}
					if s.deps.Notifier != nil {
						s.deps.Notifier.Dispatch(ctx, notify.WebhookPayload{
							EventID:   uuid.NewString(),
							EventType: "workload.restored",
							OrgID:     p.OrgID,
							Timestamp: time.Now().Unix(),
							Data: map[string]any{
								"workload_id": wl.ID,
								"node_id":     n.ID,
								"status":      "running",
							},
						})
					}
				}
			}
		}
	}
}

func (s *AgentService) onWorkloadStatus(ctx context.Context, st *v1.AgentWorkloadStatus) {
	if st == nil || st.WorkloadId == "" {
		return
	}
	p, ok := principal.From(ctx)
	if !ok || p.Kind != principal.KindNode {
		return
	}
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return
	}
	updates := map[string]any{
		"status":        workloadStatusToString(st.Status),
		"status_detail": st.Detail,
	}
	if st.ContainerId != "" {
		updates["container_id"] = st.ContainerId
	}
	_ = q.Model(&db.Workload{}).Where("id = ?", st.WorkloadId).Updates(updates).Error
	if eq, err := s.deps.Store.Org(ctx); err == nil {
		_ = eq.Create(&db.WorkloadEvent{
			TenantBase: db.TenantBase{ID: uuid.NewString(), OrgID: p.OrgID},
			WorkloadID: st.WorkloadId,
			Kind:       "status_transition",
			Message:    st.Detail,
		}).Error
	}
}

func (s *AgentService) onCommandResult(ctx context.Context, res *v1.AgentCommandResult) {
	if res == nil || res.CommandId == "" {
		return
	}
	p, ok := principal.From(ctx)
	if !ok || p.Kind != principal.KindNode {
		return
	}
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return
	}
	_ = q.Create(&db.WorkloadEvent{
		TenantBase: db.TenantBase{ID: uuid.NewString(), OrgID: p.OrgID},
		Kind:       "command_result",
		Message:    res.CommandId + ": " + res.Output + res.Error,
	}).Error
}
