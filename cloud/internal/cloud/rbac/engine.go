package rbac

import (
	"context"

	"github.com/casbin/casbin/v3"
	"github.com/casbin/casbin/v3/model"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
)

// casbinModelText is a minimal RBAC model: a subject holds a permission when
// a policy line pairs them. Role → permission pairs are loaded as policies;
// per-request identity (extra grants, bindings) is evaluated in Go so every
// failure mode stays explicit and fail-closed.
const casbinModelText = `
[request_definition]
r = sub, act

[policy_definition]
p = sub, act

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = r.sub == p.sub && r.act == p.act
`

// Binding narrows or extends authorization inside one org. An empty
// ResourceType/ResourceID grants org-wide; otherwise the grant applies only
// to the named resource, so a binding on node A never authorizes node B.
// The apiserver lane backs BindingSource with db.RoleBinding.
type Binding struct {
	SubjectType  string
	SubjectID    string
	ResourceType string
	ResourceID   string
	Permissions  []Permission
}

// BindingSource lists the policy bindings for one org. Implementations must
// scope to orgID and return an error (never partial data) when storage fails;
// errors deny.
type BindingSource interface {
	Bindings(ctx context.Context, orgID string) ([]Binding, error)
}

// Options configures the engine and the interceptor. Source may be nil, in
// which case only role policies apply.
type Options struct {
	Source BindingSource
}

// Engine evaluates permissions against role policies plus org-scoped
// bindings. It is safe for concurrent use.
type Engine struct {
	enforcer *casbin.Enforcer
	source   BindingSource
}

// New builds an Engine with the Casbin model plus one policy per
// role → permission pair. It fails on model or policy errors; a nil Engine
// must never authorize, so callers treat the error as fatal.
func New(opts Options) (*Engine, error) {
	m, err := model.NewModelFromString(casbinModelText)
	if err != nil {
		return nil, err
	}
	e, err := casbin.NewEnforcer(m)
	if err != nil {
		return nil, err
	}
	for role, perms := range rolePermissions {
		for _, perm := range perms {
			if _, err := e.AddPolicy(role, string(perm)); err != nil {
				return nil, err
			}
		}
	}
	return &Engine{enforcer: e, source: opts.Source}, nil
}

// Allowed reports whether p holds perm org-wide. Resource-scoped bindings do
// not grant here; use AllowedForResource for resource checks. Any error
// denies: unknown/empty permission, anonymous principal, missing org, engine
// failure or binding-source failure all return (false, …).
func (e *Engine) Allowed(ctx context.Context, p principal.Principal, perm Permission) (bool, error) {
	return e.allowed(ctx, p, perm, "", "")
}

// AllowedForResource reports whether p holds perm on the named resource.
// Unscoped bindings grant; scoped bindings grant only when both the resource
// type and (when set) the resource id match.
func (e *Engine) AllowedForResource(ctx context.Context, p principal.Principal, perm Permission, resourceType, resourceID string) (bool, error) {
	if resourceType == "" {
		return false, nil
	}
	return e.allowed(ctx, p, perm, resourceType, resourceID)
}

func (e *Engine) allowed(ctx context.Context, p principal.Principal, perm Permission, resourceType, resourceID string) (bool, error) {
	if e == nil || e.enforcer == nil {
		return false, errEngineUnavailable
	}
	if perm == "" || !isKnownPermission(perm) {
		return false, nil
	}
	if p.Anonymous() {
		return false, nil
	}
	role := normalizeRoleName(p.Role)
	if role == "" {
		return false, nil
	}
	if p.OrgID == "" {
		return false, nil
	}
	ok, err := e.enforcer.Enforce(role, string(perm))
	if err != nil {
		return false, err
	}
	if ok {
		return true, nil
	}
	if p.Has(string(perm)) {
		return true, nil
	}
	if e.source == nil {
		return false, nil
	}
	bindings, err := e.source.Bindings(ctx, p.OrgID)
	if err != nil {
		return false, err
	}
	for _, b := range bindings {
		if !bindingMatchesSubject(b, p, role) {
			continue
		}
		if !bindingHasPermission(b, perm) {
			continue
		}
		if b.ResourceType == "" && b.ResourceID == "" {
			return true, nil
		}
		if resourceType != "" && b.ResourceType == resourceType &&
			(b.ResourceID == "" || b.ResourceID == resourceID) {
			return true, nil
		}
	}
	return false, nil
}

// bindingMatchesSubject reports whether binding b was issued to p, addressed
// by user id, API key id or role name.
func bindingMatchesSubject(b Binding, p principal.Principal, role string) bool {
	switch b.SubjectType {
	case "user":
		return b.SubjectID != "" && b.SubjectID == p.UserID
	case "api_key":
		return b.SubjectID != "" && b.SubjectID == p.APIKeyID
	case "role":
		return b.SubjectID != "" && normalizeRoleName(b.SubjectID) == role
	default:
		return false
	}
}

func bindingHasPermission(b Binding, perm Permission) bool {
	for _, held := range b.Permissions {
		if held == perm {
			return true
		}
	}
	return false
}

var knownPermissions map[Permission]struct{}

func init() {
	knownPermissions = map[Permission]struct{}{}
	for _, p := range Permissions() {
		knownPermissions[p] = struct{}{}
	}
}

func isKnownPermission(perm Permission) bool {
	_, ok := knownPermissions[perm]
	return ok
}
