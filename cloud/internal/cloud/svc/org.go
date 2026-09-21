package svc

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/google/uuid"
)

// OrgService implements OrgServiceHandler directly against db.Store.
//
// There is no domain package for orgs yet: orgs, members and invitations are
// stored in db.Org and db.Member. Invitations are modelled as Member rows
// with Status "invited" until an invitation table lands; the mapping is
// documented on each method.
type OrgService struct {
	deps Deps
}

const memberStatusInvited = "invited"

func orgToProto(o *db.Org) *v1.Org {
	out := &v1.Org{
		Id:        o.ID,
		Name:      o.Name,
		Slug:      o.Slug,
		Plan:      o.Plan,
		CreatedAt: ts(o.CreatedAt),
		UpdatedAt: ts(o.UpdatedAt),
	}
	if o.ClerkOrgID != nil {
		out.ClerkOrgId = *o.ClerkOrgID
	}
	return out
}

func memberToProto(m *db.Member) *v1.Member {
	return &v1.Member{
		UserId:      m.UserID,
		OrgId:       m.OrgID,
		Email:       m.Email,
		DisplayName: m.DisplayName,
		Role:        roleToProto(m.Role),
		Status:      m.Status,
		CreatedAt:   ts(m.CreatedAt),
	}
}

func invitedToProto(m *db.Member) *v1.Invitation {
	created := ""
	if !m.CreatedAt.IsZero() {
		created = m.CreatedAt.UTC().Format(time.RFC3339)
	}
	return &v1.Invitation{
		Id:        m.ID,
		OrgId:     m.OrgID,
		Email:     m.Email,
		Role:      roleToProto(m.Role),
		CreatedAt: created,
	}
}

// CreateOrg is the org-optional bootstrap RPC: it runs without an org in
// context, creates the org globally, and adds the caller as owner.
func (s *OrgService) CreateOrg(ctx context.Context, req *connect.Request[v1.CreateOrgRequest]) (*connect.Response[v1.CreateOrgResponse], error) {
	p, ok := principal.From(ctx)
	if !ok || p.Anonymous() {
		return nil, connect.NewError(connect.CodeUnauthenticated, errNoPrincipal)
	}
	name := strings.TrimSpace(req.Msg.Name)
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errOrgName)
	}
	slug := strings.TrimSpace(req.Msg.Slug)
	if slug == "" {
		slug = slugify(name)
	}
	g := s.deps.Store.Unscoped().WithContext(ctx)
	org := &db.Org{
		ID:   uuid.NewString(),
		Name: name,
		Slug: slug,
	}
	if err := g.Create(org).Error; err != nil {
		return nil, connect.NewError(connect.CodeAlreadyExists, errOrgCreate)
	}
	member := &db.Member{
		TenantBase: db.TenantBase{ID: uuid.NewString(), OrgID: org.ID},
		UserID:     p.UserID,
		Email:      p.Email,
		Role:       "owner",
		Status:     "active",
	}
	if err := g.Create(member).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errOrgCreate)
	}
	return connect.NewResponse(&v1.CreateOrgResponse{Org: orgToProto(org)}), nil
}

// GetOrg reads the caller's org. Requires an org in context.
func (s *OrgService) GetOrg(ctx context.Context, req *connect.Request[v1.GetOrgRequest]) (*connect.Response[v1.GetOrgResponse], error) {
	orgID := principal.OrgID(ctx)
	if orgID == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}
	var org db.Org
	if err := s.deps.Store.Unscoped().WithContext(ctx).Where("id = ?", orgID).First(&org).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errOrgNotFound)
	}
	return connect.NewResponse(&v1.GetOrgResponse{Org: orgToProto(&org)}), nil
}

// UpdateOrg patches name/slug/plan on the caller's org.
func (s *OrgService) UpdateOrg(ctx context.Context, req *connect.Request[v1.UpdateOrgRequest]) (*connect.Response[v1.UpdateOrgResponse], error) {
	orgID := principal.OrgID(ctx)
	if orgID == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}
	updates := map[string]any{}
	if req.Msg.Name != nil {
		if v := strings.TrimSpace(*req.Msg.Name); v != "" {
			updates["name"] = v
		}
	}
	if req.Msg.Slug != nil {
		if v := strings.TrimSpace(*req.Msg.Slug); v != "" {
			updates["slug"] = v
		}
	}
	if req.Msg.Plan != nil {
		if v := strings.TrimSpace(*req.Msg.Plan); v != "" {
			updates["plan"] = v
		}
	}
	if len(updates) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errOrgEmptyUpdate)
	}
	g := s.deps.Store.Unscoped().WithContext(ctx)
	if err := g.Model(&db.Org{}).Where("id = ?", orgID).Updates(updates).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errOrgUpdate)
	}
	var org db.Org
	if err := g.Where("id = ?", orgID).First(&org).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errOrgNotFound)
	}
	return connect.NewResponse(&v1.UpdateOrgResponse{Org: orgToProto(&org)}), nil
}

// DeleteOrg removes the org and its tenant rows. The caller must confirm by
// echoing the org id; audit history (audit_events) is intentionally kept.
func (s *OrgService) DeleteOrg(ctx context.Context, req *connect.Request[v1.DeleteOrgRequest]) (*connect.Response[v1.DeleteOrgResponse], error) {
	orgID := principal.OrgID(ctx)
	if orgID == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}
	if req.Msg.ConfirmOrgId != orgID {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errOrgConfirm)
	}
	g := s.deps.Store.Unscoped().WithContext(ctx)
	for _, model := range []any{&db.Member{}, &db.ApiKey{}, &db.RoleBinding{}, &db.JoinToken{}, &db.Node{}, &db.Workload{}, &db.WorkloadEvent{}, &db.Provision{}} {
		if err := g.Where("org_id = ?", orgID).Delete(model).Error; err != nil {
			return nil, connect.NewError(connect.CodeInternal, errOrgDelete)
		}
	}
	if err := g.Where("id = ?", orgID).Delete(&db.Org{}).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errOrgDelete)
	}
	return connect.NewResponse(&v1.DeleteOrgResponse{}), nil
}

// ListMembers lists active members of the caller's org.
func (s *OrgService) ListMembers(ctx context.Context, req *connect.Request[v1.ListMembersRequest]) (*connect.Response[v1.ListMembersResponse], error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}
	limit, offset := page(req.Msg.Page, 50)
	var total int64
	if err := q.Model(&db.Member{}).Where("status <> ?", memberStatusInvited).Count(&total).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errOrgMembers)
	}
	var rows []db.Member
	if err := q.Where("status <> ?", memberStatusInvited).Order("created_at ASC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errOrgMembers)
	}
	out := make([]*v1.Member, 0, len(rows))
	for i := range rows {
		out = append(out, memberToProto(&rows[i]))
	}
	return connect.NewResponse(&v1.ListMembersResponse{Members: out, Page: pageResp(int(total), limit, offset)}), nil
}

// UpdateMemberRole changes a member's role by user id.
func (s *OrgService) UpdateMemberRole(ctx context.Context, req *connect.Request[v1.UpdateMemberRoleRequest]) (*connect.Response[v1.UpdateMemberRoleResponse], error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}
	role := roleToString(req.Msg.Role)
	if role == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errOrgBadRole)
	}
	if strings.TrimSpace(req.Msg.UserId) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errOrgNoUser)
	}
	var m db.Member
	if err := q.Where("user_id = ? AND status <> ?", req.Msg.UserId, memberStatusInvited).First(&m).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errOrgMemberNotFound)
	}
	if err := q.Model(&db.Member{}).Where("id = ?", m.ID).Update("role", role).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errOrgUpdate)
	}
	m.Role = role
	return connect.NewResponse(&v1.UpdateMemberRoleResponse{Member: memberToProto(&m)}), nil
}

// RemoveMember drops a member from the caller's org.
func (s *OrgService) RemoveMember(ctx context.Context, req *connect.Request[v1.RemoveMemberRequest]) (*connect.Response[v1.RemoveMemberResponse], error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}
	if strings.TrimSpace(req.Msg.UserId) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errOrgNoUser)
	}
	if err := q.Where("user_id = ?", req.Msg.UserId).Delete(&db.Member{}).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errOrgUpdate)
	}
	return connect.NewResponse(&v1.RemoveMemberResponse{}), nil
}

// InviteMember records an invitation as a Member row with status "invited".
// A future invitation table replaces this mapping without changing the RPC.
func (s *OrgService) InviteMember(ctx context.Context, req *connect.Request[v1.InviteMemberRequest]) (*connect.Response[v1.InviteMemberResponse], error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}
	email := strings.TrimSpace(req.Msg.Email)
	if email == "" || !strings.Contains(email, "@") {
		return nil, connect.NewError(connect.CodeInvalidArgument, errOrgBadEmail)
	}
	role := roleToString(req.Msg.Role)
	if role == "" {
		role = "viewer"
	}
	m := &db.Member{
		TenantBase: db.TenantBase{ID: uuid.NewString(), OrgID: principal.OrgID(ctx)},
		Email:      email,
		Role:       role,
		Status:     memberStatusInvited,
	}
	if err := q.Create(m).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errOrgInvite)
	}
	return connect.NewResponse(&v1.InviteMemberResponse{Invitation: invitedToProto(m)}), nil
}

// ListInvitations lists pending invitations (invited-status members).
func (s *OrgService) ListInvitations(ctx context.Context, req *connect.Request[v1.ListInvitationsRequest]) (*connect.Response[v1.ListInvitationsResponse], error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}
	limit, offset := page(req.Msg.Page, 50)
	var total int64
	if err := q.Model(&db.Member{}).Where("status = ?", memberStatusInvited).Count(&total).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errOrgMembers)
	}
	var rows []db.Member
	if err := q.Where("status = ?", memberStatusInvited).Order("created_at ASC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errOrgMembers)
	}
	out := make([]*v1.Invitation, 0, len(rows))
	for i := range rows {
		out = append(out, invitedToProto(&rows[i]))
	}
	return connect.NewResponse(&v1.ListInvitationsResponse{Invitations: out, Page: pageResp(int(total), limit, offset)}), nil
}

// RevokeInvitation deletes a pending invitation by member-row id.
func (s *OrgService) RevokeInvitation(ctx context.Context, req *connect.Request[v1.RevokeInvitationRequest]) (*connect.Response[v1.RevokeInvitationResponse], error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}
	if strings.TrimSpace(req.Msg.Id) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errOrgNoInvite)
	}
	if err := q.Where("id = ? AND status = ?", req.Msg.Id, memberStatusInvited).Delete(&db.Member{}).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errOrgUpdate)
	}
	return connect.NewResponse(&v1.RevokeInvitationResponse{}), nil
}
