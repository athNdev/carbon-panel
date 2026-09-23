package httpapi

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/auth"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
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
		ctx, err := i.authenticate(ctx, req.Header().Get("Authorization"))
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
		ctx, err = i.authenticate(ctx, conn.RequestHeader().Get("Authorization"))
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

func (i *authInterceptor) authenticate(ctx context.Context, header string) (context.Context, error) {
	raw := bearerToken(header)
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
	p, err := i.bySession(ctx, raw)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errAuthFailed)
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

// bySession verifies a Clerk session JWT and maps the caller's Clerk org
// onto the control-plane org. Unknown Clerk orgs leave OrgID empty so only
// org-optional bootstrap RPCs can proceed.
func (i *authInterceptor) bySession(ctx context.Context, raw string) (principal.Principal, error) {
	claims, err := i.opts.Verifier.Verify(ctx, raw)
	if err != nil {
		return principal.Principal{}, errAuthFailed
	}
	p := auth.SessionPrincipal(claims)
	if i.opts.Store != nil && strings.TrimSpace(claims.OrgID) != "" {
		var org db.Org
		if err := i.opts.Store.Unscoped().WithContext(ctx).
			Where("clerk_org_id = ? OR id = ?", claims.OrgID, claims.OrgID).First(&org).Error; err == nil {
			p.OrgID = org.ID
			var member db.Member
			if err := i.opts.Store.Unscoped().WithContext(ctx).
				Where("org_id = ? AND user_id = ?", org.ID, claims.Subject).
				First(&member).Error; err == nil {
				if member.Role != "" {
					p.Role = member.Role
				}
				if member.Email != "" {
					p.Email = member.Email
				}
			}
		}
	}
	return p, nil
}
