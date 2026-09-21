package rbac

import "testing"

// TestVelocitySecretIsNotGrantedToLowPrivilegeRoles guards the fix for
// GetVelocitySecret being reachable with only proxy:read. The default "user"
// role and the "anonymous" role both hold proxy read, so the procedure must be
// gated on a permission they do not have (settings read).
func TestVelocitySecretIsNotGrantedToLowPrivilegeRoles(t *testing.T) {
	const proc = "/carbonpanel.v1.ProxyService/GetVelocitySecret"

	perm, ok := ProcedurePermissions[proc]
	if !ok {
		t.Fatalf("%s is missing from ProcedurePermissions", proc)
	}
	if perm.Resource == ResourceProxy && perm.Action == ActionRead {
		t.Fatalf("%s is mapped to %s/%s, the same permission the default low-privilege roles hold",
			proc, ResourceProxy, ActionRead)
	}

	policies := DefaultRolePolicies()
	for _, role := range []string{"user", "anonymous"} {
		for _, p := range policies[role] {
			res, action := p[1], p[2]
			if res == "*" && action == "*" {
				continue // admin-like wildcard, not these roles
			}
			if res == perm.Resource && (action == perm.Action || action == "*") {
				t.Fatalf("default role %q policy %v grants %s, exposing the Velocity secret",
					role, p, proc)
			}
		}
	}
}

// TestProxyReadStillWorksForLowPrivilegeRoles is a guard against over-correcting:
// ordinary proxy read endpoints must remain available to the default "user"
// role so the UI keeps working.
func TestProxyReadStillWorksForLowPrivilegeRoles(t *testing.T) {
	for _, proc := range []string{
		"/carbonpanel.v1.ProxyService/GetProxyRoutes",
		"/carbonpanel.v1.ProxyService/GetProxyStatus",
	} {
		perm, ok := ProcedurePermissions[proc]
		if !ok {
			t.Fatalf("%s missing from ProcedurePermissions", proc)
		}
		if perm.Resource != ResourceProxy || perm.Action != ActionRead {
			t.Errorf("%s should stay proxy/read, got %s/%s", proc, perm.Resource, perm.Action)
		}
	}
}
