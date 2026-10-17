import { describe, expect, test } from 'bun:test';
import { can, ROLE_PERMISSIONS, type Role } from '../src/lib/auth/permissions';

describe('can()', () => {
	test('owner can do everything, including billing and org deletion', () => {
		expect(can('owner', 'org.delete')).toBe(true);
		expect(can('owner', 'billing.view')).toBe(true);
		expect(can('owner', 'cost.view')).toBe(true);
		expect(can('owner', 'member.manage')).toBe(true);
		expect(can('owner', 'node.provision')).toBe(true);
	});

	test('admin has operational control but not org deletion or billing', () => {
		expect(can('admin', 'member.manage')).toBe(true);
		expect(can('admin', 'node.manage')).toBe(true);
		expect(can('admin', 'workload.manage')).toBe(true);
		expect(can('admin', 'org.delete')).toBe(false);
		expect(can('admin', 'billing.view')).toBe(false);
	});

	test('operator can operate nodes and workloads but not manage members or RBAC', () => {
		expect(can('operator', 'node.manage')).toBe(true);
		expect(can('operator', 'node.join')).toBe(true);
		expect(can('operator', 'node.provision')).toBe(true);
		expect(can('operator', 'workload.manage')).toBe(true);
		expect(can('operator', 'member.manage')).toBe(false);
		expect(can('operator', 'role.manage')).toBe(false);
		expect(can('operator', 'api_key.manage')).toBe(false);
		expect(can('operator', 'org.manage')).toBe(false);
	});

	test('viewer is read-only', () => {
		expect(can('viewer', 'node.view')).toBe(true);
		expect(can('viewer', 'workload.view')).toBe(true);
		expect(can('viewer', 'audit.view')).toBe(true);
		expect(can('viewer', 'node.manage')).toBe(false);
		expect(can('viewer', 'workload.manage')).toBe(false);
		expect(can('viewer', 'member.manage')).toBe(false);
	});

	test('billing only sees billing and cost', () => {
		expect(can('billing', 'billing.view')).toBe(true);
		expect(can('billing', 'cost.view')).toBe(true);
		expect(can('billing', 'node.view')).toBe(false);
		expect(can('billing', 'audit.view')).toBe(false);
	});

	test('unknown roles and unknown permissions are denied', () => {
		expect(can('superadmin', 'node.view')).toBe(false);
		expect(can('owner', 'node.superpowers')).toBe(false);
		expect(can(null, 'node.view')).toBe(false);
		expect(can(undefined, 'node.view')).toBe(false);
		expect(can('', 'node.view')).toBe(false);
	});

	test('role hierarchy: every permission a lower role holds is held by higher roles', () => {
		const order: Role[] = ['viewer', 'operator', 'admin', 'owner'];
		for (let i = 0; i < order.length; i++) {
			for (let j = i + 1; j < order.length; j++) {
				for (const perm of ROLE_PERMISSIONS[order[i]]) {
					expect(can(order[j], perm), `${order[j]} should hold ${perm}`).toBe(true);
				}
			}
		}
	});
});