// Pure role -> permission table mirroring the server's role model.
// Roles: owner > admin > operator > viewer. Billing has billing-only visibility.
// Permission strings follow "resource.action", e.g. "node.manage".

export type Role = 'owner' | 'admin' | 'operator' | 'viewer' | 'billing';

export const ROLE_PERMISSIONS: Record<Role, readonly string[]> = {
	viewer: [
		'org.view',
		'member.view',
		'role.view',
		'node.view',
		'workload.view',
		'api_key.view',
		'audit.view'
	],
	operator: [
		'org.view',
		'member.view',
		'role.view',
		'node.view',
		'node.manage',
		'node.join',
		'node.provision',
		'workload.view',
		'workload.manage',
		'api_key.view',
		'audit.view'
	],
	admin: [
		'org.view',
		'org.manage',
		'member.view',
		'member.manage',
		'role.view',
		'role.manage',
		'node.view',
		'node.manage',
		'node.join',
		'node.provision',
		'workload.view',
		'workload.manage',
		'api_key.view',
		'api_key.manage',
		'audit.view'
	],
	owner: [
		'org.view',
		'org.manage',
		'org.delete',
		'member.view',
		'member.manage',
		'role.view',
		'role.manage',
		'node.view',
		'node.manage',
		'node.join',
		'node.provision',
		'workload.view',
		'workload.manage',
		'api_key.view',
		'api_key.manage',
		'audit.view',
		'billing.view',
		'cost.view'
	],
	billing: ['billing.view', 'cost.view']
};

export const ROLES: Role[] = ['owner', 'admin', 'operator', 'viewer', 'billing'];

/**
 * Whether `role` holds `permission`. Unknown roles and unknown permissions are false.
 */
export function can(role: string | null | undefined, permission: string): boolean {
	if (!role) return false;
	const perms = ROLE_PERMISSIONS[role as Role];
	if (!perms) return false;
	return perms.includes(permission);
}

/** Human label for a role key, used in selects and tables. */
export function roleLabel(role: string | null | undefined): string {
	if (!role) return 'unknown';
	const labels: Record<string, string> = {
		owner: 'Owner',
		admin: 'Admin',
		operator: 'Operator',
		viewer: 'Viewer',
		billing: 'Billing'
	};
	return labels[role] ?? role;
}