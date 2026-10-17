<script lang="ts">
	import { create } from '@bufbuild/protobuf';
	import Tile from '$lib/components/Tile.svelte';
	import Field from '$lib/components/Field.svelte';
	import Button from '$lib/components/Button.svelte';
	import { pushToast } from '$lib/components/toast.svelte';
	import { call, orgClient, systemClient } from '$lib/api/client';
	import { can } from '$lib/auth/permissions';
	import { auth } from '$lib/auth/clerk.svelte';
	import { createLoadable } from '$lib/state.svelte';
	import { GetOrgRequestSchema, UpdateOrgRequestSchema } from '$lib/proto/cloud/v1/org_pb';
	import { GetBuildInfoRequestSchema } from '$lib/proto/cloud/v1/system_pb';
	import { fmtDateTime, tsToDate } from '$lib/utils';

	type OrgData = {
		id: string;
		name: string;
		slug: string;
		plan: string;
		clerkOrgId: string;
		createdAt: Date | null;
		updatedAt: Date | null;
	};

	type Build = {
		version: string;
		commit: string;
		buildTime: string;
		goVersion: string;
		terraformVersion: string;
		databaseDriver: string;
	};

	async function load(): Promise<{ org: OrgData | null; build: Build | null }> {
		const [orgRes, buildRes] = await Promise.all([
			call(() => orgClient.getOrg(create(GetOrgRequestSchema, {}))),
			call(() => systemClient.getBuildInfo(create(GetBuildInfoRequestSchema, {})))
		]);
		return {
			org: orgRes.org
				? {
						id: orgRes.org.id,
						name: orgRes.org.name,
						slug: orgRes.org.slug,
						plan: orgRes.org.plan,
						clerkOrgId: orgRes.org.clerkOrgId,
						createdAt: tsToDate(orgRes.org.createdAt),
						updatedAt: tsToDate(orgRes.org.updatedAt)
					}
				: null,
			build: buildRes.build
				? {
						version: buildRes.build.version,
						commit: buildRes.build.commit,
						buildTime: buildRes.build.buildTime,
						goVersion: buildRes.build.goVersion,
						terraformVersion: buildRes.build.terraformVersion,
						databaseDriver: buildRes.build.databaseDriver
					}
				: null
		};
	}

	const pageState = createLoadable(load);

	$effect(() => {
		const o = pageState.data?.org;
		if (o && name === '' && slug === '') {
			name = o.name;
			slug = o.slug;
		}
	});

	const canManage = $derived(can(auth.orgRole, 'org.manage'));
	let name = $state('');
	let slug = $state('');
	let saveBusy = $state(false);
	let saveError = $state('');

	async function save() {
		saveError = '';
		if (!name.trim()) {
			saveError = 'A display name is required.';
			return;
		}
		saveBusy = true;
		try {
			await call(() =>
				orgClient.updateOrg(
					create(UpdateOrgRequestSchema, {
						name: name.trim(),
						slug: slug.trim() || undefined
					})
				)
			);
			pushToast('success', 'Organization settings saved.');
			await pageState.reload();
			name = pageState.data?.org?.name ?? name;
			slug = pageState.data?.org?.slug ?? slug;
		} catch (e) {
			saveError = e instanceof Error ? e.message : String(e);
		} finally {
			saveBusy = false;
		}
	}
</script>

<svelte:head><title>Settings · Carbon Cloud</title></svelte:head>

<div class="flex flex-col gap-4">
	{#if pageState.loading}
		<div class="h-48 animate-pulse border border-border bg-card"></div>
	{:else if pageState.error}
		<div class="border border-destructive/50 bg-card p-4 text-sm">
			<p class="font-medium text-destructive">Could not load settings</p>
			<p class="mt-1">{pageState.error}</p>
			<div class="mt-3">
				<Button variant="secondary" onclick={() => void pageState.reload()}>Retry</Button>
			</div>
		</div>
	{:else if pageState.data}
		{@const org = pageState.data.org}
		{#if org}
			<Tile title="Organization">
				{#if canManage}
					<form
						class="flex max-w-md flex-col gap-4"
						onsubmit={(e) => {
							e.preventDefault();
							void save();
						}}
					>
						<Field id="org-name" label="Display name" required error={saveError || ''}>
							{#snippet control(f)}
								<input
									id={f.id}
									class="h-9 border border-input bg-card px-3 text-sm text-foreground focus-ring"
									type="text"
									bind:value={name}
									aria-describedby={f.describedBy}
								/>
							{/snippet}
						</Field>
						<Field id="org-slug" label="Slug" hint="URL-safe identifier; the server may reject changes.">
							{#snippet control(f)}
								<input
									id={f.id}
									class="h-9 border border-input bg-card px-3 font-mono text-sm text-foreground focus-ring"
									type="text"
									bind:value={slug}
									aria-describedby={f.describedBy}
								/>
							{/snippet}
						</Field>
						<div>
							<Button type="submit" loading={saveBusy}>Save</Button>
						</div>
					</form>
				{:else}
					<dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
						<dt class="text-muted-foreground">Name</dt>
						<dd>{org.name}</dd>
						<dt class="text-muted-foreground">Slug</dt>
						<dd class="font-mono text-xs">{org.slug || '—'}</dd>
					</dl>
				{/if}

				<div class="mt-4 border-t border-border pt-4">
					<dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
						<dt class="text-muted-foreground">ID</dt>
						<dd class="font-mono text-xs break-all">{org.id}</dd>
						<dt class="text-muted-foreground">Clerk org</dt>
						<dd class="font-mono text-xs break-all">{org.clerkOrgId || '—'}</dd>
						<dt class="text-muted-foreground">Plan</dt>
						<dd>{org.plan || '—'}</dd>
						<dt class="text-muted-foreground">Created</dt>
						<dd class="text-xs">{fmtDateTime(org.createdAt)}</dd>
						<dt class="text-muted-foreground">Updated</dt>
						<dd class="text-xs">{fmtDateTime(org.updatedAt)}</dd>
					</dl>
				</div>
			</Tile>
		{/if}

		<Tile title="Build information">
			{#if pageState.data.build}
				<dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
					<dt class="text-muted-foreground">Version</dt>
					<dd class="font-mono text-xs">{pageState.data.build.version || '—'}</dd>
					<dt class="text-muted-foreground">Commit</dt>
					<dd class="font-mono text-xs break-all">{pageState.data.build.commit || '—'}</dd>
					<dt class="text-muted-foreground">Build time</dt>
					<dd class="font-mono text-xs">{pageState.data.build.buildTime || '—'}</dd>
					<dt class="text-muted-foreground">Go version</dt>
					<dd class="font-mono text-xs">{pageState.data.build.goVersion || '—'}</dd>
					<dt class="text-muted-foreground">Terraform</dt>
					<dd class="font-mono text-xs">{pageState.data.build.terraformVersion || 'not installed'}</dd>
					<dt class="text-muted-foreground">Database</dt>
					<dd class="font-mono text-xs">{pageState.data.build.databaseDriver || '—'}</dd>
				</dl>
			{:else}
				<p class="text-xs text-muted-foreground">Build info unavailable.</p>
			{/if}
		</Tile>
	{/if}
</div>