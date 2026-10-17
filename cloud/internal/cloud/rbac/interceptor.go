package rbac

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
)

// errPermissionDenied is the only denial message: it carries no procedure,
// role or reason detail.
var errPermissionDenied = errors.New("permission denied")

// errEngineUnavailable is returned when the engine was never built.
var errEngineUnavailable = errors.New("rbac engine unavailable")

// AuditInfo is attached to the request context by the interceptor so the
// audit lane can record who attempted what without re-resolving it.
type AuditInfo struct {
	// Procedure is the fully-qualified Connect procedure name.
	Procedure string
	// Permission is the mapped permission, empty for public procedures.
	Permission Permission
	// Actor is the authenticated caller, zero for public procedures.
	Actor principal.Principal
	// Authenticated reports whether a principal was present.
	Authenticated bool
}

type auditKey struct{}

// WithAuditInfo returns a child context carrying info.
func WithAuditInfo(ctx context.Context, info AuditInfo) context.Context {
	return context.WithValue(ctx, auditKey{}, info)
}

// AuditInfoFrom returns the AuditInfo stored in ctx, if any.
func AuditInfoFrom(ctx context.Context) (AuditInfo, bool) {
	info, ok := ctx.Value(auditKey{}).(AuditInfo)
	return info, ok
}

// Interceptor returns a fail-closed connect.Interceptor covering unary and
// streaming handlers. Order per request: authenticate → resolve procedure →
// check allowlist/mapping → evaluate policy → attach audit metadata → next.
// Every denial is connect.CodePermissionDenied with no detail leakage, and
// every engine error denies.
func Interceptor(engine *Engine, _ Options) connect.Interceptor {
	return &failClosedInterceptor{engine: engine}
}

type failClosedInterceptor struct {
	engine *Engine
}

// WrapUnary implements connect.Interceptor.
func (i *failClosedInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		ctx, err := i.authorize(ctx, req.Spec().Procedure)
		if err != nil {
			return nil, err
		}
		return next(ctx, req)
	}
}

// WrapStreamingClient implements connect.Interceptor. The interceptor is
// server-side; client streaming calls pass through untouched.
func (i *failClosedInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// WrapStreamingHandler implements connect.Interceptor.
func (i *failClosedInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		var err error
		ctx, err = i.authorize(ctx, conn.Spec().Procedure)
		if err != nil {
			return err
		}
		return next(ctx, conn)
	}
}

// authorize enforces the decision chain and returns the context carrying
// audit metadata on success.
func (i *failClosedInterceptor) authorize(ctx context.Context, procedure string) (context.Context, error) {
	deny := func() (context.Context, error) {
		return nil, connect.NewError(connect.CodePermissionDenied, errPermissionDenied)
	}
	if procedure == "" {
		return deny()
	}
	if IsPublic(procedure) {
		perm, _ := PermissionForProcedure(procedure)
		return WithAuditInfo(ctx, AuditInfo{Procedure: procedure, Permission: perm}), nil
	}
	p, ok := principal.From(ctx)
	if !ok || p.Anonymous() {
		return deny()
	}
	if p.Kind == principal.KindSystem {
		perm, _ := PermissionForProcedure(procedure)
		return WithAuditInfo(ctx, AuditInfo{Procedure: procedure, Permission: perm, Actor: p, Authenticated: true}), nil
	}
	if IsNodeIdentityProcedure(procedure) {
		if p.Kind == principal.KindNode && p.NodeID != "" {
			perm, _ := PermissionForProcedure(procedure)
			return WithAuditInfo(ctx, AuditInfo{Procedure: procedure, Permission: perm, Actor: p, Authenticated: true}), nil
		}
		return deny()
	}
	if RequiresOrg(procedure) && p.OrgID == "" {
		return deny()
	}
	perm, ok := PermissionForProcedure(procedure)
	if !ok {
		return deny()
	}
	if p.OrgID == "" {
		// Org-optional bootstrap RPCs (org creation, self-scoped session
		// reads) run before a tenant is selected: authentication is the
		// authorization. The principal can only touch its own identity and
		// org membership here, never another tenant's data.
		if _, ok := orgOptionalProcedures[procedure]; ok {
			return WithAuditInfo(ctx, AuditInfo{Procedure: procedure, Permission: perm, Actor: p, Authenticated: true}), nil
		}
		return deny()
	}
	if i.engine == nil {
		return deny()
	}
	allowed, err := i.engine.Allowed(ctx, p, perm)
	if err != nil || !allowed {
		return deny()
	}
	return WithAuditInfo(ctx, AuditInfo{Procedure: procedure, Permission: perm, Actor: p, Authenticated: true}), nil
}
