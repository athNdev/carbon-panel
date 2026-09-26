package svc

import (
	"context"
	"strings"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/rbac"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/google/uuid"
)

// RoleService implements RoleServiceHandler: the role catalogue from rbac
// plus org-scoped role bindings stored in db.RoleBinding.
type RoleService struct {
	deps Deps
}

// ListRoles returns the role catalogue with each role's permissions.
func (s *RoleService) ListRoles(ctx context.Context, req *connect.Request[v1.ListRolesRequest]) (*connect.Response[v1.ListRolesResponse], error) {
	out := make([]*v1.RoleDefinition, 0, len(rbac.Roles()))
	for _, r := range rbac.Roles() {
		var perms []string
		for _, p := range rbac.PermissionsForRole(r) {
			perms = append(perms, string(p))
		}
		out = append(out, &v1.RoleDefinition{
			Role:        roleToProto(r),
			Name:        r,
			Permissions: perms,
		})
	}
	return connect.NewResponse(&v1.ListRolesResponse{Roles: out}), nil
}

// ListPermissions returns every permission with its default roles and
// mutating flag.
func (s *RoleService) ListPermissions(ctx context.Context, req *connect.Request[v1.ListPermissionsRequest]) (*connect.Response[v1.ListPermissionsResponse], error) {
	perms := rbac.Permissions()
	out := make([]*v1.Permission, 0, len(perms))
	for _, p := range perms {
		out = append(out, &v1.Permission{
			Id:           string(p),
			DefaultRoles: defaultRolesFor(p),
			Mutating:     rbac.IsMutating(p),
		})
	}
	return connect.NewResponse(&v1.ListPermissionsResponse{Permissions: out}), nil
}

func defaultRolesFor(p rbac.Permission) []v1.Role {
	var roles []v1.Role
	for _, r := range rbac.Roles() {
		for _, rp := range rbac.PermissionsForRole(r) {
			if rp == p {
				roles = append(roles, roleToProto(r))
				break
			}
		}
	}
	return roles
}

func bindingToProto(b *db.RoleBinding) *v1.RoleBinding {
	return &v1.RoleBinding{
		Id:           b.ID,
		OrgId:        b.OrgID,
		SubjectType:  b.SubjectType,
		SubjectId:    b.SubjectID,
		ResourceType: b.ResourceType,
		ResourceId:   b.ResourceID,
		Permissions:  b.PermissionList(),
		CreatedAt:    ts(b.CreatedAt),
	}
}

// ListRoleBindings lists scoped bindings in the caller's org, optionally
// filtered by subject.
func (s *RoleService) ListRoleBindings(ctx context.Context, req *connect.Request[v1.ListRoleBindingsRequest]) (*connect.Response[v1.ListRoleBindingsResponse], error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}
	limit, offset := page(req.Msg.Page, 50)
	fq := q.Model(&db.RoleBinding{})
	if v := strings.TrimSpace(req.Msg.SubjectType); v != "" {
		fq = fq.Where("subject_type = ?", v)
	}
	if v := strings.TrimSpace(req.Msg.SubjectId); v != "" {
		fq = fq.Where("subject_id = ?", v)
	}
	var total int64
	if err := fq.Count(&total).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errRoleBindings)
	}
	var rows []db.RoleBinding
	if err := fq.Order("created_at ASC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errRoleBindings)
	}
	out := make([]*v1.RoleBinding, 0, len(rows))
	for i := range rows {
		out = append(out, bindingToProto(&rows[i]))
	}
	return connect.NewResponse(&v1.ListRoleBindingsResponse{Bindings: out, Page: pageResp(int(total), limit, offset)}), nil
}

// CreateRoleBinding grants extra permissions to a subject inside the
// caller's org. Permissions must exist in the catalogue.
func (s *RoleService) CreateRoleBinding(ctx context.Context, req *connect.Request[v1.CreateRoleBindingRequest]) (*connect.Response[v1.CreateRoleBindingResponse], error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}
	if strings.TrimSpace(req.Msg.SubjectType) == "" || strings.TrimSpace(req.Msg.SubjectId) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errRoleSubject)
	}
	if len(req.Msg.Permissions) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errRolePerms)
	}
	known := map[string]bool{}
	for _, p := range rbac.Permissions() {
		known[string(p)] = true
	}
	for _, p := range req.Msg.Permissions {
		if !known[p] {
			return nil, connect.NewError(connect.CodeInvalidArgument, errRoleUnknownPerm)
		}
	}
	b := &db.RoleBinding{
		TenantBase:   db.TenantBase{ID: uuid.NewString(), OrgID: principal.OrgID(ctx)},
		SubjectType:  strings.TrimSpace(req.Msg.SubjectType),
		SubjectID:    strings.TrimSpace(req.Msg.SubjectId),
		ResourceType: strings.TrimSpace(req.Msg.ResourceType),
		ResourceID:   strings.TrimSpace(req.Msg.ResourceId),
	}
	b.SetPermissions(req.Msg.Permissions)
	if err := q.Create(b).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errRoleCreate)
	}
	return connect.NewResponse(&v1.CreateRoleBindingResponse{Binding: bindingToProto(b)}), nil
}

// DeleteRoleBinding removes one binding by id inside the caller's org.
func (s *RoleService) DeleteRoleBinding(ctx context.Context, req *connect.Request[v1.DeleteRoleBindingRequest]) (*connect.Response[v1.DeleteRoleBindingResponse], error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}
	if strings.TrimSpace(req.Msg.Id) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errRoleNoID)
	}
	if err := q.Where("id = ?", req.Msg.Id).Delete(&db.RoleBinding{}).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errRoleDelete)
	}
	return connect.NewResponse(&v1.DeleteRoleBindingResponse{}), nil
}
