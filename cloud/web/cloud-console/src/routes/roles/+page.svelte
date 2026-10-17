<script lang="ts">
	import { create } from '@bufbuild/protobuf';
	import Tile from '$lib/components/Tile.svelte';
	import Tag from '$lib/components/Tag.svelte';
	import Button from '$lib/components/Button.svelte';
	import { call, roleClient, sessionClient } from '$lib/api/client';
	import { can } from '$lib/auth/permissions';
	import { auth } from '$lib/auth/clerk.svelte';
	import { createLoadable } from '$lib/state.svelte';
	import { ListRolesRequestSchema, ListPermissionsRequestSchema } from '$lib/proto/cloud/v1/rbac_pb';
	import { ListMyPermissionsRequestSchema } from '$lib/proto/cloud/v1/auth_pb';
	import { Role as RoleEnum } from '$lib/proto/cloud/v1/common_pb';
	import { roleFromEnum } from '$lib/utils';

	type RoleRow = {
		name: string;
		description: string;
		permissions: string[];
		key: string;
	};

	type PermissionRow = {
		id: string;
		description: string;
		mutating: boolean;
		defaultRoles: string[];
	};

	async function load(): Promise<{ roles: RoleRow[]; permissions: PermissionRow[]; mine: string[] }> {
		const [roles, perms, mine] = await Promise.all([
			call(() => roleClient.listRoles(create(ListRolesRequestSchema, {}))),
			call(() => roleClient.listPermissions(create(ListPermissionsRequestSchema, {}))),
			call(() => sessionClient.listMyPermissions(create(ListMyPermissionsRequestSchema, {})))
		]);
		return {
			roles: (roles.roles ?? []).map((r) => ({
				name: r.name,
				description: r.description,
				permissions: [...r.permissions],
				key: roleFromEnum(r.role as RoleEnum)
			})),
			permissions: (perms.permissions ?? []).map((p) => ({
				id: p.id,
				description: p.description,
				mutating: p.mutating,
				defaultRoles: (p.defaultRoles ?? []).map((r) => roleFromEnum(r as RoleEnum))
			})),
			mine: [...(mine.permissions ?? [])]
		};
	}

	const pageState = createLoadable(load);
	const myRole = $derived(auth.orgRole ?? 'viewer');
	const canView = $derived(can(myRole, 'role.view'));
</script>

<svelte:head><title>Roles · Carbon Cloud</title></svelte:head>

<div class="flex flex-col gap-4">
	{#if !canView}
		<div class="border border-destructive/50 bg-card p-4 text-sm">
			<p class="font-medium text-destructive">No permission</p>
			<p class="mt-1">Your role does not allow viewing the role catalog.</p>
		</div>
	{:else if pageState.loading}
		<div class="h-40 animate-pulse border border-border bg-card"></div>
	{:else if pageState.error}
		<div class="border border-destructive/50 bg-card p-4 text-sm">
			<p class="font-medium text-destructive">Could not load roles</p>
			<p class="mt-1">{pageState.error}</p>
			<div class="mt-3">
				<Button variant="secondary" onclick={() => void pageState.reload()}>Retry</Button>
			</div>
		</div>
	{:else if pageState.data}
		<div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
			{#each pageState.data.roles as r (r.key)}
				<Tile title={`${r.name} (${r.key})`}>
					<p class="mb-3 text-xs text-muted-foreground">{r.description}</p>
					<div class="flex flex-wrap gap-1.5">
						{#each r.permissions as p (p)}
							<Tag tone="neutral"><span class="font-mono">{p}</span></Tag>
						{/each}
					</div>
				</Tile>
			{/each}
		</div>

		<Tile title="Your effective permissions">
			<p class="mb-3 text-xs text-muted-foreground">
				Your role is <span class="font-medium text-foreground">{myRole}</span>.
			</p>
			{#if pageState.data.mine.length === 0}
				<p class="text-xs text-muted-foreground">No permissions granted.</p>
			{:else}
				<div class="flex flex-wrap gap-1.5">
					{#each pageState.data.mine as p (p)}
						<Tag tone="blue"><span class="font-mono">{p}</span></Tag>
					{/each}
				</div>
			{/if}
		</Tile>

		<Tile title="Permission catalog">
			<div class="flex flex-col gap-1.5">
				{#each pageState.data.permissions as p (p.id)}
					<div class="flex items-start justify-between gap-3 border-b border-border/50 py-2 last:border-b-0">
						<div class="min-w-0">
							<p class="font-mono text-xs text-foreground">{p.id}</p>
							<p class="text-xs text-muted-foreground">{p.description}</p>
						</div>
						<div class="flex shrink-0 items-center gap-1.5">
							{#if p.mutating}<Tag tone="yellow">mutating</Tag>{/if}
							{#each p.defaultRoles as dr (dr)}
								<Tag tone="neutral">{dr}</Tag>
							{/each}
						</div>
					</div>
				{/each}
			</div>
		</Tile>
	{/if}
</div>