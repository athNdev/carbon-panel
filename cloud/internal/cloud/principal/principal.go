// Package principal carries the authenticated caller through a request.
//
// It is deliberately dependency-free so that storage, authorization, audit and
// transport layers can all agree on who is calling without importing each other.
package principal

import "context"

// Kind distinguishes how the caller authenticated. It matters for audit and for
// rules that should only apply to interactive sessions.
type Kind string

const (
	// KindSession is a Clerk-backed interactive session.
	KindSession Kind = "session"
	// KindAPIKey is a long-lived org-scoped API key.
	KindAPIKey Kind = "api_key"
	// KindNode is a node agent authenticated by its client certificate.
	KindNode Kind = "node"
	// KindSystem is the control plane acting on its own behalf.
	KindSystem Kind = "system"
)

// Principal is the authenticated caller for a request.
//
// OrgID is empty only for requests that have not selected a tenant yet: session
// bootstrap, node join, and system work. Every tenant-scoped RPC requires a
// non-empty OrgID, and the storage layer refuses to build an org-scoped query
// without one.
type Principal struct {
	// Kind records the authentication mechanism.
	Kind Kind
	// UserID is the Clerk user id, or the owning user for an API key.
	UserID string
	// OrgID is the active tenant.
	OrgID string
	// Role is the org role string, e.g. "owner", "admin", "operator", "viewer".
	Role string
	// Email is known for session and API-key callers.
	Email string
	// APIKeyID is set when Kind is KindAPIKey.
	APIKeyID string
	// NodeID is set when Kind is KindNode.
	NodeID string
	// Permissions is the resolved permission set for this request.
	Permissions []string
}

// Anonymous reports whether the principal represents no authenticated caller.
func (p Principal) Anonymous() bool {
	return p.Kind == "" && p.UserID == "" && p.NodeID == ""
}

// Has reports whether the principal holds a permission.
func (p Principal) Has(permission string) bool {
	for _, held := range p.Permissions {
		if held == permission {
			return true
		}
	}
	return false
}

type ctxKey struct{}

// WithPrincipal returns a child context carrying p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// From returns the principal stored in ctx, if any.
func From(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}

// OrgID returns the active org id from ctx, or the empty string.
func OrgID(ctx context.Context) string {
	p, _ := From(ctx)
	return p.OrgID
}
