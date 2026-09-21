package svc

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/node"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
)

// NodeService implements NodeServiceHandler over node.Service (registry) and
// node.JoinTokenService (join tokens). Registration itself happens through
// AgentService.JoinNode.
type NodeService struct {
	deps Deps
}

func (s *NodeService) nodeToProto(ctx context.Context, n *db.Node) *v1.Node {
	cap := n.CapacityDecoded()
	out := &v1.Node{
		Id:         n.ID,
		OrgId:      n.OrgID,
		Name:       n.Name,
		Origin:     originToProto(n.Origin),
		Provider:   providerToProto(n.Provider),
		NodeTypeId: n.NodeTypeID,
		Region:     n.Region,
		Status:     nodeStatusToProto(n.Status),
		Hostname:   n.Hostname,
		PublicIp:   n.PublicIP,
		PrivateIp:  n.PrivateIP,
		Capacity: &v1.NodeCapacity{
			Vcpu:   int32(cap.VCPU),
			RamMb:  int64(cap.RAMMB),
			DiskGb: int32(cap.DiskGB),
		},
	}
	// Best-effort allocation counts; a counting failure leaves the field unset
	// rather than failing the read.
	if q, err := s.deps.Store.Org(ctx); err == nil {
		var total, running int64
		q.Model(&db.Workload{}).Where("node_id = ?", n.ID).Count(&total)
		q.Model(&db.Workload{}).Where("node_id = ? AND status = ?", n.ID, "running").Count(&running)
		if total > 0 || running > 0 {
			out.Allocation = &v1.NodeAllocation{WorkloadCount: int32(total), RunningCount: int32(running)}
		}
	}
	return out
}

func joinTokenToProto(t *db.JoinToken) *v1.JoinToken {
	return &v1.JoinToken{
		Id:           t.ID,
		OrgId:        t.OrgID,
		Name:         t.Name,
		NodeTypeId:   t.NodeTypeID,
		Origin:       originToProto(t.Origin),
		ProvisionId:  t.ProvisionID,
		ExpiresAt:    tsPtr(t.ExpiresAt),
		UsedAt:       tsPtr(t.UsedAt),
		UsedByNodeId: t.UsedByNodeID,
		RevokedAt:    tsPtr(t.RevokedAt),
		CreatedAt:    ts(t.CreatedAt),
	}
}

// ListNodes lists nodes with optional status/origin filters.
func (s *NodeService) ListNodes(ctx context.Context, req *connect.Request[v1.ListNodesRequest]) (*connect.Response[v1.ListNodesResponse], error) {
	rows, err := s.deps.Nodes.List(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errNodeList)
	}
	// Status filtering compares the effective (liveness-aware) status.
	status, origin := nodeStatusToString(req.Msg.Status), originToString(req.Msg.Origin)
	filtered := make([]db.Node, 0, len(rows))
	for _, n := range rows {
		if status != "" && !strings.EqualFold(s.deps.Nodes.EffectiveStatus(n), status) {
			continue
		}
		if origin != "" && !strings.EqualFold(n.Origin, origin) {
			continue
		}
		filtered = append(filtered, n)
	}
	limit, offset := page(req.Msg.Page, 50)
	total := len(filtered)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	out := make([]*v1.Node, 0, end-offset)
	for i := offset; i < end; i++ {
		n := filtered[i]
		out = append(out, s.nodeToProto(ctx, &n))
	}
	return connect.NewResponse(&v1.ListNodesResponse{Nodes: out, Page: pageResp(total, limit, offset)}), nil
}

// GetNode returns one node by id.
func (s *NodeService) GetNode(ctx context.Context, req *connect.Request[v1.GetNodeRequest]) (*connect.Response[v1.GetNodeResponse], error) {
	n, err := s.deps.Nodes.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errNodeNotFound)
	}
	return connect.NewResponse(&v1.GetNodeResponse{Node: s.nodeToProto(ctx, &n)}), nil
}

// UpdateNode applies a name/labels/capacity patch. The request carries no
// id: it targets the calling node (node principal from the agent stream).
func (s *NodeService) UpdateNode(ctx context.Context, req *connect.Request[v1.UpdateNodeRequest]) (*connect.Response[v1.UpdateNodeResponse], error) {
	p, ok := principal.From(ctx)
	if !ok || p.Kind != principal.KindNode || p.NodeID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errNodeSelfOnly)
	}
	patch := node.NodePatch{}
	if req.Msg.Name != nil {
		patch.Name = req.Msg.Name
	}
	if req.Msg.Labels != nil {
		m := map[string]string(req.Msg.Labels)
		patch.Labels = &m
	}
	if req.Msg.Capacity != nil {
		patch.Capacity = &db.NodeCapacity{
			VCPU:   int(req.Msg.Capacity.Vcpu),
			RAMMB:  int(req.Msg.Capacity.RamMb),
			DiskGB: int(req.Msg.Capacity.DiskGb),
		}
	}
	n, err := s.deps.Nodes.Update(ctx, p.NodeID, patch)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errNodeNotFound)
	}
	return connect.NewResponse(&v1.UpdateNodeResponse{Node: s.nodeToProto(ctx, &n)}), nil
}

// DrainNode marks a node draining (no new workloads).
func (s *NodeService) DrainNode(ctx context.Context, req *connect.Request[v1.DrainNodeRequest]) (*connect.Response[v1.DrainNodeResponse], error) {
	n, err := s.deps.Nodes.Drain(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errNodeNotFound)
	}
	return connect.NewResponse(&v1.DrainNodeResponse{Node: s.nodeToProto(ctx, &n)}), nil
}

// ResumeNode clears the draining flag.
func (s *NodeService) ResumeNode(ctx context.Context, req *connect.Request[v1.ResumeNodeRequest]) (*connect.Response[v1.ResumeNodeResponse], error) {
	n, err := s.deps.Nodes.Resume(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errNodeNotFound)
	}
	return connect.NewResponse(&v1.ResumeNodeResponse{Node: s.nodeToProto(ctx, &n)}), nil
}

// DeleteNode removes a node row from the registry.
func (s *NodeService) DeleteNode(ctx context.Context, req *connect.Request[v1.DeleteNodeRequest]) (*connect.Response[v1.DeleteNodeResponse], error) {
	if err := s.deps.Nodes.Delete(ctx, req.Msg.Id); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errNodeNotFound)
	}
	return connect.NewResponse(&v1.DeleteNodeResponse{}), nil
}

// CreateJoinToken issues a single-use join token. The secret is returned
// once; only its hash is stored.
func (s *NodeService) CreateJoinToken(ctx context.Context, req *connect.Request[v1.CreateJoinTokenRequest]) (*connect.Response[v1.CreateJoinTokenResponse], error) {
	ttl := time.Duration(req.Msg.TtlSeconds) * time.Second
	secret, tok, err := s.deps.JoinTokens.Issue(ctx, node.IssueRequest{
		Name:       strings.TrimSpace(req.Msg.Name),
		NodeTypeID: strings.TrimSpace(req.Msg.NodeTypeId),
		TTL:        ttl,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errJoinIssue)
	}
	return connect.NewResponse(&v1.CreateJoinTokenResponse{
		Token:       joinTokenToProto(&tok),
		Secret:      secret,
		JoinCommand: joinCommand(s.deps.ControlPlaneURL, secret),
	}), nil
}

// ListJoinTokens lists non-sensitive join-token metadata (never secrets).
func (s *NodeService) ListJoinTokens(ctx context.Context, req *connect.Request[v1.ListJoinTokensRequest]) (*connect.Response[v1.ListJoinTokensResponse], error) {
	rows, err := s.deps.JoinTokens.List(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errJoinList)
	}
	limit, offset := page(req.Msg.Page, 50)
	total := len(rows)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	out := make([]*v1.JoinToken, 0, end-offset)
	for i := offset; i < end; i++ {
		out = append(out, joinTokenToProto(&rows[i]))
	}
	return connect.NewResponse(&v1.ListJoinTokensResponse{Tokens: out, Page: pageResp(total, limit, offset)}), nil
}

// RevokeJoinToken revokes a join token by id.
func (s *NodeService) RevokeJoinToken(ctx context.Context, req *connect.Request[v1.RevokeJoinTokenRequest]) (*connect.Response[v1.RevokeJoinTokenResponse], error) {
	if _, err := s.deps.JoinTokens.Revoke(ctx, req.Msg.Id); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errJoinNotFound)
	}
	return connect.NewResponse(&v1.RevokeJoinTokenResponse{}), nil
}

func joinCommand(baseURL, secret string) string {
	if strings.TrimSpace(baseURL) == "" {
		return "cloudnoded join --token " + secret
	}
	return "cloudnoded join --control-plane " + strings.TrimRight(baseURL, "/") + " --token " + secret
}
