package svc

import (
	"context"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/rbac"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
)

// SessionService implements SessionServiceHandler: identity introspection
// for the caller. All reads are self-scoped; no org is required.
type SessionService struct {
	deps Deps
}

func principalToProto(p principal.Principal) *v1.Principal {
	return &v1.Principal{
		UserId:   p.UserID,
		OrgId:    p.OrgID,
		Role:     roleToProto(p.Role),
		Email:    p.Email,
		ApiKeyId: p.APIKeyID,
	}
}

// orgsForUser returns the orgs the user belongs to via Member rows.
func (s *SessionService) orgsForUser(ctx context.Context, userID string) []*v1.Org {
	if userID == "" {
		return nil
	}
	var members []db.Member
	if err := s.deps.Store.Unscoped().WithContext(ctx).Where("user_id = ?", userID).Find(&members).Error; err != nil || len(members) == 0 {
		return nil
	}
	orgIDs := make([]string, 0, len(members))
	for _, m := range members {
		orgIDs = append(orgIDs, m.OrgID)
	}
	var orgs []db.Org
	if err := s.deps.Store.Unscoped().WithContext(ctx).Where("id IN ?", orgIDs).Find(&orgs).Error; err != nil {
		return nil
	}
	out := make([]*v1.Org, 0, len(orgs))
	for i := range orgs {
		out = append(out, orgToProto(&orgs[i]))
	}
	return out
}

// GetSession returns the caller's resolved principal, switchable orgs, and
// whether the request authenticated with an API key.
func (s *SessionService) GetSession(ctx context.Context, req *connect.Request[v1.GetSessionRequest]) (*connect.Response[v1.GetSessionResponse], error) {
	p, ok := principal.From(ctx)
	if !ok || p.Anonymous() {
		return nil, connect.NewError(connect.CodeUnauthenticated, errNoPrincipal)
	}
	return connect.NewResponse(&v1.GetSessionResponse{
		Principal: principalToProto(p),
		Orgs:      s.orgsForUser(ctx, p.UserID),
		ViaApiKey: p.Kind == principal.KindAPIKey,
	}), nil
}

// ListMyOrgs lists the orgs the caller is a member of.
func (s *SessionService) ListMyOrgs(ctx context.Context, req *connect.Request[v1.ListMyOrgsRequest]) (*connect.Response[v1.ListMyOrgsResponse], error) {
	p, ok := principal.From(ctx)
	if !ok || p.Anonymous() {
		return nil, connect.NewError(connect.CodeUnauthenticated, errNoPrincipal)
	}
	orgs := s.orgsForUser(ctx, p.UserID)
	limit, offset := page(req.Msg.Page, 50)
	total := len(orgs)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return connect.NewResponse(&v1.ListMyOrgsResponse{Orgs: orgs[offset:end], Page: pageResp(total, limit, offset)}), nil
}

// ListMyPermissions returns the caller's effective role and permission set.
func (s *SessionService) ListMyPermissions(ctx context.Context, req *connect.Request[v1.ListMyPermissionsRequest]) (*connect.Response[v1.ListMyPermissionsResponse], error) {
	p, ok := principal.From(ctx)
	if !ok || p.Anonymous() {
		return nil, connect.NewError(connect.CodeUnauthenticated, errNoPrincipal)
	}
	perms := p.Permissions
	if len(perms) == 0 && p.Role != "" {
		for _, rp := range rbac.PermissionsForRole(p.Role) {
			perms = append(perms, string(rp))
		}
	}
	return connect.NewResponse(&v1.ListMyPermissionsResponse{Permissions: perms, Role: roleToProto(p.Role)}), nil
}
