<script lang="ts">
	import { create } from '@bufbuild/protobuf';
	import Tile from '$lib/components/Tile.svelte';
	import Tag from '$lib/components/Tag.svelte';
	import DataTable from '$lib/components/DataTable.svelte';
	import Button from '$lib/components/Button.svelte';
	import Field from '$lib/components/Field.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import { pushToast } from '$lib/components/toast.svelte';
	import { call, orgClient } from '$lib/api/client';
	import { can, roleLabel, ROLES, type Role } from '$lib/auth/permissions';
	import { auth } from '$lib/auth/clerk.svelte';
	import { createLoadable } from '$lib/state.svelte';
	import {
		ListMembersRequestSchema,
		ListInvitationsRequestSchema,
		InviteMemberRequestSchema,
		UpdateMemberRoleRequestSchema,
		RemoveMemberRequestSchema,
		RevokeInvitationRequestSchema
	} from '$lib/proto/cloud/v1/org_pb';
	import { Role as RoleEnum } from '$lib/proto/cloud/v1/common_pb';
	import { fmtDateTime, tsToDate, roleFromEnum, roleToEnum } from '$lib/utils';

	type Row = {
		userId: string;
		email: string;
		displayName: string;
		role: string;
		status: string;
		createdAt: Date | null;
	};

	type Invitation = {
		id: string;
		email: string;
		role: string;
		createdAt: string;
	};

	async function load(): Promise<{ members: Row[]; invitations: Invitation[] }> {
		const [m, inv] = await Promise.all([
			call(() => orgClient.listMembers(create(ListMembersRequestSchema, {}))),
			call(() => orgClient.listInvitations(create(ListInvitationsRequestSchema, {})))
		]);
		return {
			members: (m.members ?? []).map((x) => ({
				userId: x.userId,
				email: x.email,
				displayName: x.displayName,
				role: roleFromEnum(x.role),
				status: x.status || 'active',
				createdAt: tsToDate(x.createdAt)
			})),
			invitations: (inv.invitations ?? []).map((i) => ({
				id: i.id,
				email: i.email,
				role: roleFromEnum(i.role),
				createdAt: i.createdAt || ''
			}))
		};
	}

	const pageState = createLoadable(load);

	const canManage = $derived(can(auth.orgRole, 'member.manage'));
	const myId = $derived(auth.user?.id ?? '');

	let inviteEmail = $state('');
	let inviteRole = $state<Role>('operator');
	let inviteError = $state('');
	let inviteBusy = $state(false);

	async function invite() {
		inviteError = '';
		if (!inviteEmail.trim()) {
			inviteError = 'An email address is required.';
			return;
		}
		inviteBusy = true;
		try {
			await call(() =>
				orgClient.inviteMember(
					create(InviteMemberRequestSchema, {
						email: inviteEmail.trim(),
						role: roleToEnum(inviteRole) as RoleEnum
					})
				)
			);
			pushToast('success', `Invitation sent to ${inviteEmail.trim()}.`);
			inviteEmail = '';
			await pageState.reload();
		} catch (e) {
			inviteError = e instanceof Error ? e.message : String(e);
		} finally {
			inviteBusy = false;
		}
	}

	async function changeRole(userId: string, role: string) {
		try {
			await call(() =>
				orgClient.updateMemberRole(
					create(UpdateMemberRoleRequestSchema, { userId, role: roleToEnum(role) as RoleEnum })
				)
			);
			pushToast('success', 'Member role updated.');
			await pageState.reload();
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		}
	}

	let removeUser = $state<string | null>(null);
	let removeBusy = $state(false);

	async function removeMember() {
		const userId = removeUser;
		if (!userId) return;
		removeBusy = true;
		try {
			await call(() =>
				orgClient.removeMember(create(RemoveMemberRequestSchema, { userId }))
			);
			pushToast('success', 'Member removed.');
			removeUser = null;
			await pageState.reload();
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		} finally {
			removeBusy = false;
		}
	}

	async function revokeInvitation(id: string) {
		try {
			await call(() =>
				orgClient.revokeInvitation(create(RevokeInvitationRequestSchema, { id }))
			);
			pushToast('success', 'Invitation revoked.');
			await pageState.reload();
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		}
	}

	const removed = $derived(removeUser);
	const removedMember = $derived(
		removeUser ? pageState.data?.members.find((m) => m.userId === removeUser) ?? null : null
	);
</script>

<svelte:head><title>Members · Carbon Cloud</title></svelte:head>

<div class="flex flex-col gap-4">
	{#if !canManage}
		<EmptyState
			title="No permission"
			body="Your role does not allow managing members. Ask an organization owner or admin."
		/>
	{:else if pageState.loading}
		<div class="h-40 animate-pulse border border-border bg-card"></div>
	{:else if pageState.error}
		<div class="border border-destructive/50 bg-card p-4 text-sm">
			<p class="font-medium text-destructive">Could not load members</p>
			<p class="mt-1">{pageState.error}</p>
			<div class="mt-3">
				<Button variant="secondary" onclick={() => void pageState.reload()}>Retry</Button>
			</div>
		</div>
	{:else}
		<Tile title="Invite a member">
			<form
				class="flex max-w-md flex-col gap-4"
				onsubmit={(e) => {
					e.preventDefault();
					void invite();
				}}
			>
				<Field id="invite-email" label="Email" required error={inviteError || ''}>
					{#snippet control(f)}
						<input
							id={f.id}
							class="h-9 border border-input bg-card px-3 text-sm text-foreground focus-ring"
							type="email"
							placeholder="player@example.com"
							bind:value={inviteEmail}
							aria-describedby={f.describedBy}
						/>
					{/snippet}
				</Field>
				<Field id="invite-role" label="Role">
					{#snippet control(f)}
						<select
							id={f.id}
							class="h-9 border border-input bg-card px-3 text-sm text-foreground focus-ring"
							bind:value={inviteRole}
							aria-describedby={f.describedBy}
						>
							{#each ROLES as r (r)}
								<option value={r}>{roleLabel(r)}</option>
							{/each}
						</select>
					{/snippet}
				</Field>
				<div>
					<Button type="submit" loading={inviteBusy}>Send invitation</Button>
				</div>
			</form>
		</Tile>

		<Tile title="Members">
			{#if (pageState.data?.members ?? []).length === 0}
				<EmptyState title="No members" body="Invite someone to join this organization." />
			{:else}
				<DataTable
					label="Members"
					columns={[
						{ id: 'email', label: 'Email', sortable: true },
						{ id: 'name', label: 'Name' },
						{ id: 'role', label: 'Role', sortable: true },
						{ id: 'status', label: 'Status' },
						{ id: 'joined', label: 'Joined', sortable: true }
					]}
					rows={(pageState.data?.members ?? []).map((m) => ({
						userId: m.userId,
						email: m.email || m.userId,
						name: m.displayName || '—',
						role: m.role,
						status: m.status,
						joined: m.createdAt ? fmtDateTime(m.createdAt) : '—'
					}))}
					keyFor={(row) => row.userId}
				>
					{#snippet cell(row, col)}
						{#if col.id === 'role'}
							<div class="flex items-center gap-2">
								<Tag tone={row.role === 'owner' ? 'purple' : row.role === 'admin' ? 'blue' : 'neutral'}>
									{roleLabel(row.role as string)}
								</Tag>
								{#if row.userId !== myId}
									<select
										class="h-7 border border-input bg-card px-1 text-xs text-foreground focus-ring"
										aria-label={`Change role for ${row.email}`}
										value={row.role as string}
										onchange={(e) => void changeRole(row.userId as string, (e.target as HTMLSelectElement).value)}
									>
										{#each ROLES as r (r)}
											<option value={r}>{roleLabel(r)}</option>
										{/each}
									</select>
								{/if}
							</div>
						{:else if col.id === 'status'}
							<span class="text-xs text-muted-foreground">{row.status}</span>
						{:else if col.id === 'email'}
							<span class="text-foreground">{row.email}</span>
							{#if row.userId === myId}
								<span class="ml-1 text-xs text-muted-foreground">(you)</span>
							{/if}
						{:else}
							<span class="text-xs text-muted-foreground">{row[col.id]}</span>
						{/if}
					{/snippet}
				</DataTable>
			{/if}
		</Tile>

		{#if (pageState.data?.invitations ?? []).length > 0}
			<Tile title="Pending invitations">
				<DataTable
					label="Pending invitations"
					columns={[
						{ id: 'email', label: 'Email', sortable: true },
						{ id: 'role', label: 'Role' },
						{ id: 'sent', label: 'Sent' },
						{ id: 'actions', label: '' }
					]}
					rows={(pageState.data?.invitations ?? []).map((i) => ({
						id: i.id,
						email: i.email,
						role: roleLabel(i.role),
						sent: i.createdAt || '—'
					}))}
					keyFor={(row) => row.id}
				>
					{#snippet cell(row, col)}
						{#if col.id === 'actions'}
							<Button
								variant="ghost"
								size="sm"
								onclick={() => void revokeInvitation(row.id as string)}
							>
								Revoke
							</Button>
						{:else}
							{String(row[col.id] ?? '—')}
						{/if}
					{/snippet}
				</DataTable>
			</Tile>
		{/if}
	{/if}
</div>

<Modal open={!!removed} title="Remove member" onclose={() => (removeUser = null)}>
	<p class="text-sm text-foreground">
		Remove <span class="font-medium">{removedMember?.email ?? removed}</span> from this
		organization? They will lose access immediately.
	</p>
	{#snippet footer()}
		<Button variant="ghost" onclick={() => (removeUser = null)}>Cancel</Button>
		<Button variant="danger" onclick={removeMember} loading={removeBusy}>Remove member</Button>
	{/snippet}
</Modal>