package svc

import (
	"context"
	"errors"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/billing"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/node"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/notify"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/provision"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/google/uuid"
)

// ProvisionService implements ProvisionServiceHandler over
// provision.Service. Listing is a direct org-scoped db query; the domain
// service owns every state transition.
type ProvisionService struct {
	deps Deps
}

func provisionToProto(p *db.Provision) *v1.Provision {
	return &v1.Provision{
		Id:          p.ID,
		OrgId:       p.OrgID,
		Name:        p.Name,
		Provider:    providerToProto(p.Provider),
		Region:      p.Region,
		NodeTypeId:  p.NodeTypeID,
		Status:      provisionStatusToProto(p.Status),
		Workspace:   p.Workspace,
		PlanSummary: p.PlanSummary,
		PlanDiff:    p.PlanDiff,
		Outputs:     p.OutputMap(),
		Error:       p.Error,
		NodeId:      p.NodeID,
		CreatedBy:   p.CreatedBy,
		CreatedAt:   ts(p.CreatedAt),
		UpdatedAt:   ts(p.UpdatedAt),
	}
}

func (s *ProvisionService) requireProvisioner() error {
	if s.deps.Provision == nil {
		return connect.NewError(connect.CodeUnavailable, errNoProvisioner)
	}
	return nil
}

// createRequestFor builds a domain CreateRequest, resolving sizing from the
// node-type catalog. Unknown node types fail fast.
func (s *ProvisionService) createRequestFor(ctx context.Context, providerName, region, nodeTypeID string, vars map[string]string) (provision.CreateRequest, error) {
	t, err := s.deps.Catalog.Get(ctx, nodeTypeID)
	if err != nil {
		return provision.CreateRequest{}, errProvisionNodeType
	}
	p, _ := principal.From(ctx)
	instance := ""
	if it, ok := s.deps.Catalog.InstanceType(ctx, nodeTypeID, providerName); ok {
		instance = it
	}
	var sshKey, joinToken string
	if vars != nil {
		sshKey = vars["ssh_public_key"]
		if sshKey == "" {
			sshKey = vars["ssh_key"]
		}
		joinToken = vars["join_token"]
	}
	if joinToken == "" && s.deps.JoinTokens != nil {
		secret, _, err := s.deps.JoinTokens.Issue(ctx, node.IssueRequest{
			Name:       "provision-" + providerName + "-" + region,
			NodeTypeID: nodeTypeID,
			Origin:     "managed",
		})
		if err == nil {
			joinToken = secret
		}
	}
	return provision.CreateRequest{
		Provider:      providerName,
		Region:        region,
		NodeTypeID:    nodeTypeID,
		VCPU:          t.VCPU,
		RAMMB:         t.RAMMB,
		DiskGB:        t.DiskGB,
		InstanceType:  instance,
		SSHPublicKey:  sshKey,
		JoinToken:     joinToken,
		ProviderExtra: vars,
		CreatedBy:     p.UserID,
	}, nil
}

// CreateProvision creates a provision row; with plan_only it stops before
// planning.
func (s *ProvisionService) CreateProvision(ctx context.Context, req *connect.Request[v1.CreateProvisionRequest]) (*connect.Response[v1.CreateProvisionResponse], error) {
	if err := s.requireProvisioner(); err != nil {
		return nil, err
	}
	m := req.Msg
	providerName := providerToString(m.Provider)
	if providerName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errProvisionProvider)
	}
	if strings.TrimSpace(m.Name) == "" || strings.TrimSpace(m.Region) == "" || strings.TrimSpace(m.NodeTypeId) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errProvisionFields)
	}

	// Enforce managed node quota if billing is active
	if s.deps.Billing != nil {
		orgID := principal.OrgID(ctx)
		var org db.Org
		if err := s.deps.Store.Unscoped().WithContext(ctx).Where("id = ?", orgID).First(&org).Error; err == nil {
			plan := s.deps.Billing.Catalog().GetOrDefault(org.Plan)
			var count int64
			_ = s.deps.Store.Unscoped().WithContext(ctx).Model(&db.Provision{}).
				Where("org_id = ? AND status NOT IN ('failed', 'destroyed')", orgID).Count(&count).Error
			if err := s.deps.Billing.Check(plan, billing.Usage{ManagedNodeCount: int(count)}, billing.Usage{ManagedNodeCount: 1}); err != nil {
				return nil, connect.NewError(connect.CodeResourceExhausted, err)
			}
		}
	}

	creq, err := s.createRequestFor(ctx, providerName, m.Region, m.NodeTypeId, m.ProviderVars)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	creq.Name = strings.TrimSpace(m.Name)
	p, err := s.deps.Provision.CreateProvision(ctx, creq)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errProvisionCreate)
	}
	if !m.PlanOnly {
		if p, err = s.deps.Provision.Plan(ctx, p.ID, creq); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errProvisionPlan)
		}
	}

	if s.deps.Notifier != nil {
		s.deps.Notifier.Dispatch(ctx, notify.WebhookPayload{
			EventID:   uuid.NewString(),
			EventType: "provision.created",
			OrgID:     principal.OrgID(ctx),
			Timestamp: time.Now().Unix(),
			Data: map[string]any{
				"provision_id": p.ID,
				"name":         p.Name,
				"provider":     providerName,
				"region":       m.Region,
			},
		})
	}

	return connect.NewResponse(&v1.CreateProvisionResponse{Provision: provisionToProto(p)}), nil
}

// PlanProvision runs terraform plan for an existing provision.
func (s *ProvisionService) PlanProvision(ctx context.Context, req *connect.Request[v1.PlanProvisionRequest]) (*connect.Response[v1.PlanProvisionResponse], error) {
	if err := s.requireProvisioner(); err != nil {
		return nil, err
	}
	stored, err := s.deps.Provision.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errProvisionNotFound)
	}
	creq, err := s.createRequestFor(ctx, stored.Provider, stored.Region, stored.NodeTypeID, nil)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	creq.Name = stored.Name
	p, err := s.deps.Provision.Plan(ctx, stored.ID, creq)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errProvisionPlan)
	}
	return connect.NewResponse(&v1.PlanProvisionResponse{Provision: provisionToProto(p)}), nil
}

// ApplyProvision applies a planned provision. When the caller passes a
// confirm_plan_hash it must match the stored plan hash.
func (s *ProvisionService) ApplyProvision(ctx context.Context, req *connect.Request[v1.ApplyProvisionRequest]) (*connect.Response[v1.ApplyProvisionResponse], error) {
	if err := s.requireProvisioner(); err != nil {
		return nil, err
	}
	stored, err := s.deps.Provision.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errProvisionNotFound)
	}
	if h := strings.TrimSpace(req.Msg.ConfirmPlanHash); h != "" && h != stored.PlanHash {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errProvisionStalePlan)
	}
	p, err := s.deps.Provision.Apply(ctx, stored.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errProvisionApply)
	}

	if s.deps.Notifier != nil {
		s.deps.Notifier.Dispatch(ctx, notify.WebhookPayload{
			EventID:   uuid.NewString(),
			EventType: "provision.applied",
			OrgID:     principal.OrgID(ctx),
			Timestamp: time.Now().Unix(),
			Data: map[string]any{
				"provision_id": p.ID,
			},
		})
	}

	return connect.NewResponse(&v1.ApplyProvisionResponse{Provision: provisionToProto(p)}), nil
}

// DestroyProvision destroys provisioned infrastructure. The caller confirms
// by echoing the provision id.
func (s *ProvisionService) DestroyProvision(ctx context.Context, req *connect.Request[v1.DestroyProvisionRequest]) (*connect.Response[v1.DestroyProvisionResponse], error) {
	if err := s.requireProvisioner(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Msg.ConfirmId) != strings.TrimSpace(req.Msg.Id) || strings.TrimSpace(req.Msg.Id) == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errProvisionConfirm)
	}
	p, err := s.deps.Provision.Destroy(ctx, req.Msg.Id)
	if err != nil {
		if errors.Is(err, provision.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, errProvisionNotFound)
		}
		return nil, connect.NewError(connect.CodeInternal, errProvisionDestroy)
	}

	if s.deps.Notifier != nil {
		s.deps.Notifier.Dispatch(ctx, notify.WebhookPayload{
			EventID:   uuid.NewString(),
			EventType: "provision.destroyed",
			OrgID:     principal.OrgID(ctx),
			Timestamp: time.Now().Unix(),
			Data: map[string]any{
				"provision_id": p.ID,
			},
		})
	}

	return connect.NewResponse(&v1.DestroyProvisionResponse{Provision: provisionToProto(p)}), nil
}

// GetProvision returns one provision by id, with a direct-read fallback so
// status stays available when the provisioner is not wired.
func (s *ProvisionService) GetProvision(ctx context.Context, req *connect.Request[v1.GetProvisionRequest]) (*connect.Response[v1.GetProvisionResponse], error) {
	if s.deps.Provision != nil {
		if p, err := s.deps.Provision.Get(ctx, req.Msg.Id); err == nil {
			return connect.NewResponse(&v1.GetProvisionResponse{Provision: provisionToProto(p)}), nil
		}
	}
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}
	var row db.Provision
	if err := q.Where("id = ?", req.Msg.Id).First(&row).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errProvisionNotFound)
	}
	return connect.NewResponse(&v1.GetProvisionResponse{Provision: provisionToProto(&row)}), nil
}

// ListProvisions lists provision history, most recent first.
func (s *ProvisionService) ListProvisions(ctx context.Context, req *connect.Request[v1.ListProvisionsRequest]) (*connect.Response[v1.ListProvisionsResponse], error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}
	fq := q.Model(&db.Provision{})
	if v := provisionStatusToString(req.Msg.Status); v != "" {
		fq = fq.Where("status = ?", v)
	}
	limit, offset := page(req.Msg.Page, 50)
	var total int64
	if err := fq.Count(&total).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errProvisionList)
	}
	var rows []db.Provision
	if err := fq.Order("created_at DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errProvisionList)
	}
	out := make([]*v1.Provision, 0, len(rows))
	for i := range rows {
		out = append(out, provisionToProto(&rows[i]))
	}
	return connect.NewResponse(&v1.ListProvisionsResponse{Provisions: out, Page: pageResp(int(total), limit, offset)}), nil
}

// StreamProvisionLogs streams terraform output as it is produced.
func (s *ProvisionService) StreamProvisionLogs(ctx context.Context, req *connect.Request[v1.StreamProvisionLogsRequest], stream *connect.ServerStream[v1.ProvisionLogLine]) error {
	return connect.NewError(connect.CodeUnimplemented, errors.New("svc: provision log streaming not implemented"))
}
