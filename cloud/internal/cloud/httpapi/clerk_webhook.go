package httpapi

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/auth"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/google/uuid"
)

func (s *Server) handleClerkWebhook(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(s.opts.ClerkWebhook.Secret) == "" || s.opts.Store == nil {
		http.Error(w, "webhook not configured", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "invalid webhook", http.StatusBadRequest)
		return
	}
	ev, err := auth.VerifyWebhook(s.opts.ClerkWebhook, r.Header, body)
	if err != nil {
		http.Error(w, "invalid webhook", http.StatusUnauthorized)
		return
	}
	if err := applyClerkEvent(r.Context(), s.opts.Store, ev); err != nil {
		http.Error(w, "apply failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// applyClerkEvent writes orgs and memberships from a verified Clerk event.
// An unknown Clerk org is inserted as its own row. It is never attached to
// the bootstrap slug "default".
func applyClerkEvent(ctx context.Context, store *db.Store, ev *auth.WebhookEvent) error {
	if ev == nil || store == nil {
		return nil
	}
	switch ev.Type {
	case auth.EventOrganizationCreated, auth.EventOrganizationUpdated:
		org, err := ev.Organization()
		if err != nil {
			return err
		}
		return upsertClerkOrg(ctx, store, org)
	case auth.EventMembershipCreated, auth.EventMembershipUpdated:
		m, err := ev.Membership()
		if err != nil {
			return err
		}
		return upsertClerkMember(ctx, store, m)
	case auth.EventMembershipDeleted:
		m, err := ev.Membership()
		if err != nil {
			return err
		}
		return deleteClerkMember(ctx, store, m)
	default:
		return nil
	}
}

func upsertClerkOrg(ctx context.Context, store *db.Store, org auth.OrganizationData) error {
	if strings.TrimSpace(org.ID) == "" {
		return nil
	}
	g := store.Unscoped().WithContext(ctx)
	var existing db.Org
	err := g.Where("clerk_org_id = ?", org.ID).First(&existing).Error
	if err == nil {
		updates := map[string]any{}
		if org.Name != "" {
			updates["name"] = org.Name
		}
		if len(updates) == 0 {
			return nil
		}
		return g.Model(&existing).Updates(updates).Error
	}
	slug := strings.TrimSpace(org.Slug)
	if slug == "" || slug == "default" {
		slug = "clerk-" + org.ID
	}
	var slugTaken db.Org
	if g.Where("slug = ?", slug).First(&slugTaken).Error == nil {
		slug = slug + "-" + org.ID
	}
	row := db.Org{ID: uuid.NewString(), Name: org.Name, Slug: slug, ClerkOrgID: &org.ID}
	if row.Name == "" {
		row.Name = slug
	}
	return g.Create(&row).Error
}

func upsertClerkMember(ctx context.Context, store *db.Store, m auth.MembershipData) error {
	if m.OrganizationID == "" || m.UserID == "" {
		return nil
	}
	g := store.Unscoped().WithContext(ctx)
	var org db.Org
	if err := g.Where("clerk_org_id = ?", m.OrganizationID).First(&org).Error; err != nil {
		return nil
	}
	role := auth.SessionPrincipal(auth.Claims{OrgRole: m.Role}).Role
	var member db.Member
	err := g.Where("org_id = ? AND user_id = ?", org.ID, m.UserID).First(&member).Error
	if err == nil {
		return g.Model(&member).Updates(map[string]any{"role": role, "status": "active"}).Error
	}
	return g.Create(&db.Member{
		TenantBase: db.TenantBase{ID: uuid.NewString(), OrgID: org.ID},
		UserID:     m.UserID,
		Role:       role,
		Status:     "active",
	}).Error
}

func deleteClerkMember(ctx context.Context, store *db.Store, m auth.MembershipData) error {
	if m.OrganizationID == "" || m.UserID == "" {
		return nil
	}
	g := store.Unscoped().WithContext(ctx)
	var org db.Org
	if err := g.Where("clerk_org_id = ?", m.OrganizationID).First(&org).Error; err != nil {
		return nil
	}
	return g.Where("org_id = ? AND user_id = ?", org.ID, m.UserID).Delete(&db.Member{}).Error
}
