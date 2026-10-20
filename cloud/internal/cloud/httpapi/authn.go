package httpapi

import (
	"context"
	"errors"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/audit"
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
	Audits   audit.Store
}

// errNotMember is a verified session with no recorded membership. It is
// PermissionDenied, not a bad credential.
var errNotMember = errors.New("membership required")

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
	if errors.Is(err, errNotMember) {
		return nil, connect.NewError(connect.CodePermissionDenied, errAuthFailed)
	}
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

// bySession verifies a Clerk session JWT. A token with no org claim stays a
// session principal so CreateOrg can bootstrap. A token that names an org is
// accepted only when that org and an invited or active membership already
// exist. It does not create members and it does not attach an unknown Clerk
// org to the default slug.
func (i *authInterceptor) bySession(ctx context.Context, raw, _ string) (principal.Principal, error) {
	claims, err := i.opts.Verifier.Verify(ctx, raw)
	if err != nil {
		return principal.Principal{}, errAuthFailed
	}
	p := auth.SessionPrincipal(claims)
	if i.opts.Store == nil || strings.TrimSpace(claims.OrgID) == "" {
		return p, nil
	}

	target := strings.TrimSpace(claims.OrgID)
	var org db.Org
	if err := i.opts.Store.Unscoped().WithContext(ctx).
		Where("clerk_org_id = ? OR id = ?", target, target).First(&org).Error; err != nil {
		i.auditDeny(ctx, "", claims.Subject)
		return principal.Principal{}, errNotMember
	}
	var member db.Member
	err = i.opts.Store.Unscoped().WithContext(ctx).
		Where("org_id = ? AND user_id = ? AND status IN ?", org.ID, claims.Subject, []string{"active", "invited"}).
		First(&member).Error
	if err != nil {
		i.auditDeny(ctx, org.ID, claims.Subject)
		return principal.Principal{}, errNotMember
	}
	if member.Status == "invited" {
		_ = i.opts.Store.Unscoped().WithContext(ctx).Model(&member).Update("status", "active").Error
		member.Status = "active"
	}
	p.OrgID = org.ID
	if member.Role != "" {
		p.Role = member.Role
	}
	if member.Email != "" {
		p.Email = member.Email
	}
	return p, nil
}

func (i *authInterceptor) auditDeny(ctx context.Context, orgID, userID string) {
	if i.opts.Audits == nil || orgID == "" {
		return
	}
	actx := principal.WithPrincipal(ctx, principal.Principal{
		Kind:   principal.KindSession,
		UserID: userID,
		OrgID:  orgID,
	})
	_ = i.opts.Audits.Append(actx, audit.Event{
		OrgID:       orgID,
		ActorKind:   string(principal.KindSession),
		ActorUserID: userID,
		Action:      "auth.membership_denied",
		Result:      audit.ResultDenied,
	})
}
