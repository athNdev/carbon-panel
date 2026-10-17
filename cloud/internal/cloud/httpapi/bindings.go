package httpapi

import (
	"context"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/rbac"
)

// BindingSource backs the rbac engine with db.RoleBinding rows. It lives in
// the apiserver lane so rbac never imports db (lane rule).
type BindingSource struct {
	Store *db.Store
}

// Bindings lists the policy bindings for one org. Storage errors deny:
// callers treat any error as no bindings.
func (b *BindingSource) Bindings(ctx context.Context, orgID string) ([]rbac.Binding, error) {
	if orgID == "" {
		return nil, nil
	}
	var rows []db.RoleBinding
	if err := b.Store.Unscoped().WithContext(ctx).Where("org_id = ?", orgID).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]rbac.Binding, 0, len(rows))
	for i := range rows {
		perms := make([]rbac.Permission, 0, len(rows[i].PermissionList()))
		for _, p := range rows[i].PermissionList() {
			perms = append(perms, rbac.Permission(p))
		}
		out = append(out, rbac.Binding{
			SubjectType:  rows[i].SubjectType,
			SubjectID:    rows[i].SubjectID,
			ResourceType: rows[i].ResourceType,
			ResourceID:   rows[i].ResourceID,
			Permissions:  perms,
		})
	}
	return out, nil
}
