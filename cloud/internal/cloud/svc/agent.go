package svc

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/node"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
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
	deps Deps
}

// JoinNode redeems a join token and registers the node. Public procedure;
// the token is the credential.
func (s *AgentService) JoinNode(ctx context.Context, req *connect.Request[v1.JoinNodeRequest]) (*connect.Response[v1.JoinNodeResponse], error) {
	m := req.Msg
	if strings.TrimSpace(m.Token) == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errAgentJoin)
	}
	tok, err := s.deps.JoinTokens.Redeem(ctx, strings.TrimSpace(m.Token))
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errAgentJoin)
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
	for {
		msg, err := stream.Receive()
		if err != nil {
			return err
		}
		switch payload := msg.Payload.(type) {
		case *v1.AgentMessage_Hello:
			if err := stream.Send(welcomeMsg(ctx, s, payload.Hello)); err != nil {
				return err
			}
		case *v1.AgentMessage_Heartbeat:
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

func welcomeMsg(ctx context.Context, s *AgentService, hello *v1.AgentHello) *v1.ControlMessage {
	draining := false
	if hello != nil && hello.NodeId != "" {
		if n, err := s.deps.Nodes.Get(ctx, hello.NodeId); err == nil {
			draining = n.Draining
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
	_, _ = s.deps.Nodes.Heartbeat(ctx, hb.NodeId)
}

func (s *AgentService) onWorkloadStatus(ctx context.Context, st *v1.AgentWorkloadStatus) {
	if st == nil || st.WorkloadId == "" {
		return
	}
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return
	}
	status := "running"
	detail := st.Detail
	switch st.Status {
	case v1.WorkloadStatus_WORKLOAD_STATUS_RUNNING:
		status = "running"
	case v1.WorkloadStatus_WORKLOAD_STATUS_STOPPED:
		status = "stopped"
	case v1.WorkloadStatus_WORKLOAD_STATUS_ERROR:
		status = "error"
	case v1.WorkloadStatus_WORKLOAD_STATUS_PENDING:
		status = "pending"
	}
	_ = q.Model(&db.Workload{}).Where("id = ?", st.WorkloadId).Updates(map[string]any{
		"status": status, "status_detail": detail, "container_id": st.ContainerId,
	}).Error
}

func (s *AgentService) onCommandResult(ctx context.Context, res *v1.AgentCommandResult) {
	if res == nil || res.CommandId == "" {
		return
	}
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return
	}
	_ = q.Create(&db.WorkloadEvent{
		TenantBase: db.TenantBase{ID: uuid.NewString(), OrgID: principal.OrgID(ctx)},
		Kind:       "command_result",
		Message:    res.CommandId + ": " + res.Output + res.Error,
	}).Error
}
