// Package rbac is the Carbon Cloud authorization catalogue, policy engine
// and fail-closed Connect-RPC interceptor.
//
// Authorization is deny by default: an unknown procedure, an empty permission
// mapping, an engine error, a missing principal or a missing org each deny
// with connect.CodePermissionDenied and no detail leakage.
//
// Role policy storage arrives through BindingSource so this package never
// imports db; the apiserver lane backs it with db.RoleBinding.
package rbac

import "sort"

// Permission is a lowercase dotted capability string, e.g. "nodes.read".
type Permission string

// Permission catalogue. Read permissions observe state; every other
// permission mutates, executes or administers and must never be held by the
// viewer role.
const (
	PermNodesRead        Permission = "nodes.read"
	PermNodesWrite       Permission = "nodes.write"
	PermNodesJoin        Permission = "nodes.join"
	PermNodeTypesRead    Permission = "nodetypes.read"
	PermWorkloadsRead    Permission = "workloads.read"
	PermWorkloadsWrite   Permission = "workloads.write"
	PermWorkloadsExec    Permission = "workloads.exec"
	PermProvisionRead    Permission = "provision.read"
	PermProvisionWrite   Permission = "provision.write"
	PermProvisionApply   Permission = "provision.apply"
	PermOrgRead          Permission = "org.read"
	PermOrgWrite         Permission = "org.write"
	PermOrgMembersWrite  Permission = "org.members.write"
	PermOrgDelete        Permission = "org.delete"
	PermKeysManage       Permission = "keys.manage"
	PermAuditRead        Permission = "audit.read"
	PermRBACRead         Permission = "rbac.read"
	PermRBACWrite        Permission = "rbac.write"
	PermProfileRead      Permission = "profile.read"
	PermSystemRead       Permission = "system.read"
	PermAgentConnect     Permission = "agent.connect"
	PermAgentCredentials Permission = "agent.credentials"
)

// Roles exactly matching the Clerk role normalisation in auth
// (owner > admin > operator > billing > viewer). Unknown roles fail closed:
// they hold no permissions.
const (
	RoleOwner    = "owner"
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleBilling  = "billing"
	RoleViewer   = "viewer"
)

// Roles returns every known role ordered from most to least privileged.
func Roles() []string {
	return []string{RoleOwner, RoleAdmin, RoleOperator, RoleBilling, RoleViewer}
}

// rolePermissions is the single source of truth for role → permission sets.
// Every set is a subset of the one above it: owner ⊇ admin ⊇ operator and
// billing/viewer hold read-only permissions.
var rolePermissions = map[string][]Permission{
	RoleOwner: {
		PermNodesRead, PermNodesWrite, PermNodesJoin,
		PermNodeTypesRead,
		PermWorkloadsRead, PermWorkloadsWrite, PermWorkloadsExec,
		PermProvisionRead, PermProvisionWrite, PermProvisionApply,
		PermOrgRead, PermOrgWrite, PermOrgMembersWrite, PermOrgDelete,
		PermKeysManage,
		PermAuditRead,
		PermRBACRead, PermRBACWrite,
		PermProfileRead, PermSystemRead,
		PermAgentConnect, PermAgentCredentials,
	},
	RoleAdmin: {
		PermNodesRead, PermNodesWrite, PermNodesJoin,
		PermNodeTypesRead,
		PermWorkloadsRead, PermWorkloadsWrite, PermWorkloadsExec,
		PermProvisionRead, PermProvisionWrite, PermProvisionApply,
		PermOrgRead, PermOrgWrite, PermOrgMembersWrite,
		PermKeysManage,
		PermAuditRead,
		PermRBACRead, PermRBACWrite,
		PermProfileRead, PermSystemRead,
		PermAgentConnect, PermAgentCredentials,
	},
	RoleOperator: {
		PermNodesRead, PermNodesWrite, PermNodesJoin,
		PermNodeTypesRead,
		PermWorkloadsRead, PermWorkloadsWrite, PermWorkloadsExec,
		PermProvisionRead, PermProvisionWrite,
		PermOrgRead,
		PermAuditRead,
		PermRBACRead,
		PermProfileRead, PermSystemRead,
	},
	RoleBilling: {
		PermOrgRead,
		PermAuditRead,
		PermProfileRead, PermSystemRead,
		PermProvisionRead,
	},
	RoleViewer: {
		PermNodesRead,
		PermNodeTypesRead,
		PermWorkloadsRead,
		PermProvisionRead,
		PermOrgRead,
		PermAuditRead,
		PermRBACRead,
		PermProfileRead, PermSystemRead,
	},
}

// Permissions returns the whole catalogue, sorted.
func Permissions() []Permission {
	seen := map[Permission]struct{}{}
	for _, perms := range rolePermissions {
		for _, p := range perms {
			seen[p] = struct{}{}
		}
	}
	out := make([]Permission, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// PermissionsForRole returns a sorted copy of the permissions held by role.
// Unknown roles hold nothing (fail closed), never a default set.
func PermissionsForRole(role string) []Permission {
	perms, ok := rolePermissions[normalizeRoleName(role)]
	if !ok {
		return nil
	}
	out := append([]Permission(nil), perms...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// IsMutating reports whether perm authorizes a state change, execution or
// administration. Read-only permissions observe state.
func IsMutating(perm Permission) bool {
	switch perm {
	case PermNodesRead, PermNodeTypesRead, PermWorkloadsRead,
		PermProvisionRead, PermOrgRead, PermAuditRead, PermRBACRead,
		PermProfileRead, PermSystemRead:
		return false
	default:
		return true
	}
}

// normalizeRoleName trims and lowercases a role for lookup.
func normalizeRoleName(role string) string {
	lower := make([]byte, 0, len(role))
	for i := 0; i < len(role); i++ {
		c := role[i]
		if c == ' ' || c == '\t' || c == '\n' {
			continue
		}
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		lower = append(lower, c)
	}
	return string(lower)
}
