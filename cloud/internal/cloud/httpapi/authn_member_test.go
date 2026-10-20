package httpapi

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/audit"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/auth"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
)

type staticVerifier struct {
	claims auth.Claims
}

func (s staticVerifier) Verify(context.Context, string) (auth.Claims, error) {
	return s.claims, nil
}

func TestBySessionDoesNotEnrollOrBindDefault(t *testing.T) {
	ctx := context.Background()
	store, err := db.Open(db.Options{Driver: "sqlite", DSN: ":memory:", AutoMigrate: true})
	if err != nil {
		t.Fatal(err)
	}
	def := db.Org{ID: "org-default", Name: "Default", Slug: "default"}
	if err := store.Unscoped().Create(&def).Error; err != nil {
		t.Fatal(err)
	}
	orgA := db.Org{ID: "org-a", Name: "A", Slug: "a"}
	clerkA := "org_clerk_a"
	orgA.ClerkOrgID = &clerkA
	if err := store.Unscoped().Create(&orgA).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.Unscoped().Create(&db.Member{
		TenantBase: db.TenantBase{ID: "mem-a", OrgID: orgA.ID},
		UserID:     "user-a", Role: "operator", Status: "active",
	}).Error; err != nil {
		t.Fatal(err)
	}
	orgB := db.Org{ID: "org-b", Name: "B", Slug: "b"}
	clerkB := "org_clerk_b"
	orgB.ClerkOrgID = &clerkB
	if err := store.Unscoped().Create(&orgB).Error; err != nil {
		t.Fatal(err)
	}

	audits := audit.NewGormStore(store)
	i := &authInterceptor{opts: AuthOptions{Verifier: staticVerifier{claims: auth.Claims{
		Subject: "user-a", OrgID: "org_clerk_b", OrgRole: "org:admin",
	}}, Store: store, Audits: audits}}

	_, err = i.bySession(ctx, "tok", "default")
	if err != errNotMember {
		t.Fatalf("cross-org session err = %v, want membership required", err)
	}
	var bound db.Org
	if err := store.Unscoped().First(&bound, "id = ?", def.ID).Error; err != nil {
		t.Fatal(err)
	}
	if bound.ClerkOrgID != nil && *bound.ClerkOrgID != "" {
		t.Fatalf("default org bound to %q", *bound.ClerkOrgID)
	}
	var extra int64
	if err := store.Unscoped().Model(&db.Member{}).Where("org_id = ?", orgB.ID).Count(&extra).Error; err != nil {
		t.Fatal(err)
	}
	if extra != 0 {
		t.Fatalf("org B gained %d members", extra)
	}
	actx := principal.WithPrincipal(ctx, principal.Principal{Kind: principal.KindSession, UserID: "user-a", OrgID: orgB.ID})
	events, _, err := audits.List(actx, audit.Filter{Action: "auth.membership_denied"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Result != audit.ResultDenied {
		t.Fatalf("audit = %+v", events)
	}

	i.opts.Verifier = staticVerifier{claims: auth.Claims{Subject: "user-a", OrgID: clerkA, OrgRole: "org:admin"}}
	p, err := i.bySession(ctx, "tok", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.OrgID != orgA.ID || p.Role != "operator" {
		t.Fatalf("principal = %+v, want org A operator from the membership row", p)
	}

	ctx2, err := i.authenticate(ctx, "Bearer tok", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if principal.OrgID(ctx2) != orgA.ID {
		t.Fatalf("authenticated org = %s", principal.OrgID(ctx2))
	}

	i.opts.Verifier = staticVerifier{claims: auth.Claims{Subject: "user-a", OrgID: clerkB}}
	_, err = i.authenticate(ctx, "Bearer tok", "", "")
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("code = %v, want PermissionDenied", err)
	}
}

func TestApplyClerkEventLeavesDefaultOrg(t *testing.T) {
	ctx := context.Background()
	store, err := db.Open(db.Options{Driver: "sqlite", DSN: ":memory:", AutoMigrate: true})
	if err != nil {
		t.Fatal(err)
	}
	def := db.Org{ID: "org-default", Name: "Default", Slug: "default"}
	if err := store.Unscoped().Create(&def).Error; err != nil {
		t.Fatal(err)
	}
	ev := &auth.WebhookEvent{Type: auth.EventOrganizationCreated, Payload: []byte(`{"data":{"id":"org_new","name":"New","slug":"default"}}`)}
	if err := applyClerkEvent(ctx, store, ev); err != nil {
		t.Fatal(err)
	}
	var still db.Org
	if err := store.Unscoped().First(&still, "slug = ?", "default").Error; err != nil {
		t.Fatal(err)
	}
	if still.ClerkOrgID != nil && *still.ClerkOrgID != "" {
		t.Fatalf("default clerk id = %q", *still.ClerkOrgID)
	}
	var created db.Org
	if err := store.Unscoped().Where("clerk_org_id = ?", "org_new").First(&created).Error; err != nil {
		t.Fatal(err)
	}
	mem := &auth.WebhookEvent{Type: auth.EventMembershipCreated, Payload: []byte(`{"data":{"organization":{"id":"org_new"},"public_user_data":{"user_id":"user_9"},"role":"org:admin"}}`)}
	if err := applyClerkEvent(ctx, store, mem); err != nil {
		t.Fatal(err)
	}
	var member db.Member
	if err := store.Unscoped().Where("org_id = ? AND user_id = ?", created.ID, "user_9").First(&member).Error; err != nil {
		t.Fatal(err)
	}
	if member.Role != "admin" || member.Status != "active" {
		t.Fatalf("member = %+v", member)
	}
}
