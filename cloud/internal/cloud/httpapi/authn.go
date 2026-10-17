package httpapi

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/auth"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	"github.com/google/uuid"
)

// AuthOptions wires the authentication interceptor. A nil Verifier disables
// Clerk session JWTs (API keys still work); a nil Store disables API keys.
type AuthOptions struct {
	Verifier auth.Verifier
	Store    *db.Store
	Pepper   []byte
}

// AuthInterceptor resolves bearer credentials into a principal.Principal
// before rbac runs. It is the first link in the chain: requests without
// credentials pass through untouched (rbac denies non-public procedures),
// while presented-but-invalid credentials fail closed with Unauthenticated.
func AuthInterceptor(o AuthOptions) connect.Interceptor {
	return &authInterceptor{opts: o}
}

type authInterceptor struct {
	opts AuthOptions
}

func (i *authInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		ctx, err := i.authenticate(ctx, req.Header().Get("Authorization"), req.Header().Get("X-Carbon-Node-ID"), req.Header().Get("X-Carbon-Org"))
		if err != nil {
			return nil, err
		}
		return next(ctx, req)
	}
}

func (i *authInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *authInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		var err error
		ctx, err = i.authenticate(ctx, conn.RequestHeader().Get("Authorization"), conn.RequestHeader().Get("X-Carbon-Node-ID"), conn.RequestHeader().Get("X-Carbon-Org"))
		if err != nil {
			return err
		}
		return next(ctx, conn)
	}
}

func bearerToken(header string) string {
	if header == "" {
		return ""
	}
	if rest, ok := strings.CutPrefix(header, "Bearer "); ok {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(header)
}

func (i *authInterceptor) authenticate(ctx context.Context, header, nodeIDHeader, orgHeader string) (context.Context, error) {
	raw := bearerToken(header)
	if strings.HasPrefix(raw, "ccn_") {
		return i.byNode(ctx, strings.TrimPrefix(raw, "ccn_"))
	}
	if strings.HasPrefix(raw, "node:") {
		return i.byNode(ctx, strings.TrimPrefix(raw, "node:"))
	}
	if nodeIDHeader != "" && raw == "" {
		return i.byNode(ctx, nodeIDHeader)
	}
	if raw == "" {
		return ctx, nil
	}
	// API keys carry a cc_ prefix; without a session verifier everything
	// bearer-shaped is tried as an API key.
	if strings.HasPrefix(raw, "cc_") || i.opts.Verifier == nil {
		p, err := i.byAPIKey(ctx, raw)
		if err != nil {
			return nil, connect.NewError(connect.CodeUnauthenticated, errAuthFailed)
		}
		return principal.WithPrincipal(ctx, p), nil
	}
	p, err := i.bySession(ctx, raw, orgHeader)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errAuthFailed)
	}
	return principal.WithPrincipal(ctx, p), nil
}

func (i *authInterceptor) byNode(ctx context.Context, nodeID string) (context.Context, error) {
	if i.opts.Store == nil || strings.TrimSpace(nodeID) == "" {
		return ctx, nil
	}
	var n db.Node
	if err := i.opts.Store.Unscoped().WithContext(ctx).Where("id = ?", strings.TrimSpace(nodeID)).First(&n).Error; err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errAuthFailed)
	}
	p := principal.Principal{
		Kind:   principal.KindNode,
		NodeID: n.ID,
		OrgID:  n.OrgID,
	}
	return principal.WithPrincipal(ctx, p), nil
}

// byAPIKey verifies an org API key against its stored hash. Revoked and
// expired keys never verify. Role/email resolve from the owning member row
// when present; the key's own permission list always travels on the
// principal for the policy engine.
func (i *authInterceptor) byAPIKey(ctx context.Context, secret string) (principal.Principal, error) {
	if i.opts.Store == nil {
		return principal.Principal{}, errAuthFailed
	}
	hash := auth.HashAPIKey(i.opts.Pepper, secret)
	var key db.ApiKey
	if err := i.opts.Store.Unscoped().WithContext(ctx).Where("hash = ?", hash).First(&key).Error; err != nil {
		return principal.Principal{}, errAuthFailed
	}
	now := time.Now().UTC()
	if key.RevokedAt != nil {
		return principal.Principal{}, errAuthFailed
	}
	if key.ExpiresAt != nil && !key.ExpiresAt.After(now) {
		return principal.Principal{}, errAuthFailed
	}
	p := principal.Principal{
		Kind:        principal.KindAPIKey,
		UserID:      key.CreatedBy,
		OrgID:       key.OrgID,
		APIKeyID:    key.ID,
		Permissions: key.PermissionList(),
	}
	var member db.Member
	if key.CreatedBy != "" {
		if err := i.opts.Store.Unscoped().WithContext(ctx).
			Where("org_id = ? AND user_id = ?", key.OrgID, key.CreatedBy).
			First(&member).Error; err == nil {
			p.Role, p.Email = member.Role, member.Email
		}
	}
	// Best-effort last-use tracking; never fails authentication.
	_ = i.opts.Store.Unscoped().WithContext(ctx).Model(&db.ApiKey{}).
		Where("id = ?", key.ID).Update("last_used_at", now).Error
	return p, nil
}

// bySession verifies a Clerk session JWT and maps the caller onto the control-plane
// org and permissions. Unknown Clerk orgs fall back to X-Carbon-Org, existing
// user memberships, or the bootstrap default org so single-tenant and dev sessions
// succeed smoothly.
func (i *authInterceptor) bySession(ctx context.Context, raw, orgHeader string) (principal.Principal, error) {
	claims, err := i.opts.Verifier.Verify(ctx, raw)
	if err != nil {
		return principal.Principal{}, errAuthFailed
	}
	p := auth.SessionPrincipal(claims)
	if i.opts.Store == nil {
		return p, nil
	}

	var org db.Org
	var foundOrg bool

	targetOrg := strings.TrimSpace(claims.OrgID)
	if targetOrg != "" {
		if err := i.opts.Store.Unscoped().WithContext(ctx).
			Where("clerk_org_id = ? OR id = ?", targetOrg, targetOrg).First(&org).Error; err == nil {
			foundOrg = true
		} else {
			// Auto-link unlinked default organization if present
			var defOrg db.Org
			if err := i.opts.Store.Unscoped().WithContext(ctx).
				Where("slug = ? AND (clerk_org_id IS NULL OR clerk_org_id = '')", "default").First(&defOrg).Error; err == nil {
				clerkID := targetOrg
				defOrg.ClerkOrgID = &clerkID
				_ = i.opts.Store.Unscoped().WithContext(ctx).Save(&defOrg)
				org = defOrg
				foundOrg = true
			}
		}
	}

	if !foundOrg && strings.TrimSpace(orgHeader) != "" {
		h := strings.TrimSpace(orgHeader)
		if err := i.opts.Store.Unscoped().WithContext(ctx).
			Where("id = ? OR slug = ? OR clerk_org_id = ?", h, h, h).First(&org).Error; err == nil {
			foundOrg = true
		}
	}

	if !foundOrg {
		// Check if user has an existing membership in any org
		var mem db.Member
		if err := i.opts.Store.Unscoped().WithContext(ctx).
			Where("user_id = ?", claims.Subject).First(&mem).Error; err == nil {
			if err := i.opts.Store.Unscoped().WithContext(ctx).
				Where("id = ?", mem.OrgID).First(&org).Error; err == nil {
				foundOrg = true
			}
		}
	}

	if foundOrg {
		p.OrgID = org.ID
	}

	if p.OrgID != "" {
		var member db.Member
		if err := i.opts.Store.Unscoped().WithContext(ctx).
			Where("org_id = ? AND user_id = ?", p.OrgID, claims.Subject).
			First(&member).Error; err == nil {
			if member.Role != "" {
				p.Role = member.Role
			}
			if member.Email != "" {
				p.Email = member.Email
			}
		} else {
			// Count existing non-bootstrap members in this organization:
			var humanCount int64
			_ = i.opts.Store.Unscoped().WithContext(ctx).Model(&db.Member{}).
				Where("org_id = ? AND user_id != 'usr_bootstrap_owner'", p.OrgID).Count(&humanCount).Error

			role := "operator"
			if humanCount == 0 {
				role = "owner"
			} else if p.Role != "" {
				role = p.Role
			}

			displayName := claims.Email
			if displayName == "" {
				displayName = claims.Subject
			}
			m := db.Member{
				TenantBase:  db.TenantBase{ID: uuid.NewString(), OrgID: p.OrgID},
				UserID:      claims.Subject,
				Email:       claims.Email,
				DisplayName: displayName,
				Role:        role,
				Status:      "active",
			}
			if err := i.opts.Store.Unscoped().WithContext(ctx).Create(&m).Error; err == nil {
				p.Role = m.Role
				if m.Email != "" {
					p.Email = m.Email
				}
			}
		}
	}

	return p, nil
}
