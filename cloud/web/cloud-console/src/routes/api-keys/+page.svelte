<script lang="ts">
	import { create } from '@bufbuild/protobuf';
	import Tile from '$lib/components/Tile.svelte';
	import Tag from '$lib/components/Tag.svelte';
	import DataTable from '$lib/components/DataTable.svelte';
	import Button from '$lib/components/Button.svelte';
	import Field from '$lib/components/Field.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import CopyField from '$lib/components/CopyField.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import { pushToast } from '$lib/components/toast.svelte';
	import { call, apiKeyClient, roleClient } from '$lib/api/client';
	import { can } from '$lib/auth/permissions';
	import { auth } from '$lib/auth/clerk.svelte';
	import { createLoadable } from '$lib/state.svelte';
	import {
		ListApiKeysRequestSchema,
		CreateApiKeyRequestSchema,
		RevokeApiKeyRequestSchema
	} from '$lib/proto/cloud/v1/auth_pb';
	import { ListPermissionsRequestSchema } from '$lib/proto/cloud/v1/rbac_pb';
	import { fmtDateTime, tsToDate } from '$lib/utils';

	type KeyRow = {
		id: string;
		name: string;
		prefix: string;
		permissions: string[];
		lastUsed: Date | null;
		expires: Date | null;
		created: Date | null;
		revoked: boolean;
	};

	async function load(): Promise<KeyRow[]> {
		const res = await call(() => apiKeyClient.listApiKeys(create(ListApiKeysRequestSchema, {})));
		return (res.apiKeys ?? []).map((k) => ({
			id: k.id,
			name: k.name,
			prefix: k.prefix,
			permissions: [...k.permissions],
			lastUsed: tsToDate(k.lastUsedAt),
			expires: tsToDate(k.expiresAt),
			created: tsToDate(k.createdAt),
			revoked: k.revokedAt != null
		}));
	}

	const pageState = createLoadable(load);

	const canManage = $derived(can(auth.orgRole, 'api_key.manage'));

	const permissions = createLoadable(async () => {
		const res = await call(() => roleClient.listPermissions(create(ListPermissionsRequestSchema, {})));
		return (res.permissions ?? []).map((p) => p.id);
	});

	let createOpen = $state(false);
	let keyName = $state('');
	let selectedPerms = $state<string[]>([]);
	let createError = $state('');
	let createBusy = $state(false);

	let newSecret = $state<string | null>(null);
	let newKeyName = $state('');

	async function createKey() {
		createError = '';
		if (!keyName.trim()) {
			createError = 'A name is required.';
			return;
		}
		createBusy = true;
		try {
			const res = await call(() =>
				apiKeyClient.createApiKey(
					create(CreateApiKeyRequestSchema, {
						name: keyName.trim(),
						permissions: selectedPerms
					})
				)
			);
			newSecret = res.secret;
			newKeyName = keyName.trim();
			createOpen = false;
			keyName = '';
			selectedPerms = [];
			await pageState.reload();
		} catch (e) {
			createError = e instanceof Error ? e.message : String(e);
		} finally {
			createBusy = false;
		}
	}

	function togglePerm(id: string) {
		if (selectedPerms.includes(id)) {
			selectedPerms = selectedPerms.filter((p) => p !== id);
		} else {
			selectedPerms = [...selectedPerms, id];
		}
	}

	let revokeId = $state<string | null>(null);
	let revokeBusy = $state(false);

	async function revoke() {
		const id = revokeId;
		if (!id) return;
		revokeBusy = true;
		try {
			await call(() => apiKeyClient.revokeApiKey(create(RevokeApiKeyRequestSchema, { id })));
			pushToast('success', 'API key revoked.');
			revokeId = null;
			await pageState.reload();
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		} finally {
			revokeBusy = false;
		}
	}

	function closeSecret() {
		newSecret = null;
	}
</script>

<svelte:head><title>API keys · Carbon Cloud</title></svelte:head>

<div class="flex flex-col gap-4">
	{#if pageState.loading}
		<div class="h-40 animate-pulse border border-border bg-card"></div>
	{:else if pageState.error}
		<div class="border border-destructive/50 bg-card p-4 text-sm">
			<p class="font-medium text-destructive">Could not load API keys</p>
			<p class="mt-1">{pageState.error}</p>
			<div class="mt-3">
				<Button variant="secondary" onclick={() => void pageState.reload()}>Retry</Button>
			</div>
		</div>
	{:else}
		{#if canManage}
			<div class="flex items-center justify-between gap-3">
				<p class="text-sm text-muted-foreground">
					Long-lived programmatic credentials scoped to this organization.
				</p>
				<Button onclick={() => (createOpen = true)}>Create API key</Button>
			</div>
		{/if}

		{#if (pageState.data ?? []).length === 0}
			<EmptyState
				title="No API keys"
				body="Create a key to use the Carbon Cloud API programmatically."
			/>
		{:else}
			<DataTable
				label="API keys"
				columns={[
					{ id: 'name', label: 'Name', sortable: true },
					{ id: 'prefix', label: 'Prefix' },
					{ id: 'lastUsed', label: 'Last used', sortable: true },
					{ id: 'expires', label: 'Expires' },
					{ id: 'actions', label: '' }
				]}
				rows={(pageState.data ?? []).map((k) => ({
					id: k.id,
					name: k.name,
					prefix: k.prefix,
					lastUsed: k.lastUsed ? fmtDateTime(k.lastUsed) : 'never',
					expires: k.expires ? fmtDateTime(k.expires) : 'never',
					revoked: k.revoked
				}))}
				keyFor={(row) => row.id}
			>
				{#snippet cell(row, col)}
					{#if col.id === 'prefix'}
						<span class="font-mono text-xs text-muted-foreground">{row.prefix}…</span>
					{:else if col.id === 'actions'}
						{#if row.revoked}
							<Tag tone="red">revoked</Tag>
						{:else if canManage}
							<Button
								variant="ghost"
								size="sm"
								onclick={() => (revokeId = row.id as string)}
							>
								Revoke
							</Button>
						{/if}
					{:else}
						{String(row[col.id] ?? '—')}
					{/if}
				{/snippet}
			</DataTable>
		{/if}
	{/if}
</div>

{#if newSecret !== null}
	<div class="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/60 p-4 pt-24" role="presentation">
		<div class="w-full max-w-lg border border-border bg-card p-5" role="dialog" aria-modal="true" aria-labelledby="secret-title">
			<h2 id="secret-title" class="mb-2 text-base font-semibold text-foreground">API key created</h2>
			<p class="mb-3 text-sm text-warning">
				This secret is shown exactly once. Copy it now — it cannot be retrieved again.
			</p>
			<CopyField label={`Secret for ${newKeyName}`} value={newSecret} secret />
			<div class="mt-5 flex justify-end">
				<Button onclick={closeSecret}>Done</Button>
			</div>
		</div>
	</div>
{/if}

<Modal open={createOpen} title="Create API key" onclose={() => (createOpen = false)}>
	<form
		class="flex flex-col gap-4"
		onsubmit={(e) => {
			e.preventDefault();
			void createKey();
		}}
	>
		<Field id="key-name" label="Name" required error={createError || ''}>
			{#snippet control(f)}
				<input
					id={f.id}
					class="h-9 border border-input bg-card px-3 text-sm text-foreground focus-ring"
					type="text"
					placeholder="ci-deploys"
					bind:value={keyName}
					aria-describedby={f.describedBy}
				/>
			{/snippet}
		</Field>
		<fieldset class="flex flex-col gap-1.5">
			<legend class="mb-1 text-xs font-medium text-foreground">Permissions</legend>
			{#if permissions.loading}
				<p class="text-xs text-muted-foreground">Loading permissions…</p>
			{:else}
				<div class="max-h-48 overflow-y-auto border border-border bg-background p-2">
					{#each permissions.data ?? [] as p (p)}
						<label class="flex items-center gap-2 py-1 text-xs text-foreground">
							<input
								type="checkbox"
								class="h-3.5 w-3.5 accent-primary focus-ring"
								checked={selectedPerms.includes(p)}
								onchange={() => togglePerm(p)}
							/>
							<span class="font-mono">{p}</span>
						</label>
					{/each}
				</div>
			{/if}
		</fieldset>
		<div class="flex justify-end gap-2">
			<Button variant="ghost" onclick={() => (createOpen = false)}>Cancel</Button>
			<Button type="submit" loading={createBusy}>Create</Button>
		</div>
	</form>
</Modal>

<Modal open={!!revokeId} title="Revoke API key" onclose={() => (revokeId = null)}>
	<p class="text-sm text-foreground">
		Revoke this API key? Any programmatic access using it will stop working immediately.
	</p>
	{#snippet footer()}
		<Button variant="ghost" onclick={() => (revokeId = null)}>Cancel</Button>
		<Button variant="danger" onclick={revoke} loading={revokeBusy}>Revoke key</Button>
	{/snippet}
</Modal>