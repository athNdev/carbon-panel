<script lang="ts">
	import { create } from '@bufbuild/protobuf';
	import Tile from '$lib/components/Tile.svelte';
	import Field from '$lib/components/Field.svelte';
	import Button from '$lib/components/Button.svelte';
	import Tag from '$lib/components/Tag.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import { pushToast } from '$lib/components/toast.svelte';
	import { call, nodeTypeClient, provisionClient } from '$lib/api/client';
	import { can } from '$lib/auth/permissions';
	import { auth } from '$lib/auth/clerk.svelte';
	import { ProviderId } from '$lib/proto/cloud/v1/common_pb';
	import { createLoadable } from '$lib/state.svelte';
	import { ListProvidersRequestSchema } from '$lib/proto/cloud/v1/nodetype_pb';
	import { ListNodeTypesRequestSchema } from '$lib/proto/cloud/v1/nodetype_pb';
	import {
		CreateProvisionRequestSchema,
		PlanProvisionRequestSchema,
		ApplyProvisionRequestSchema
	} from '$lib/proto/cloud/v1/provision_pb';
	import { fmtRam, fmtGb, fmtUsd, sha256Hex } from '$lib/utils';

	type Provider = {
		provider: number;
		name: string;
		configured: boolean;
		regions: string[];
		requiredKeys: string[];
		missingKeys: string[];
	};

	type NodeType = {
		id: string;
		name: string;
		vcpu: number;
		ramMb: number;
		diskGb: number;
		monthlyPriceUsd: number;
		description: string;
		enabled: boolean;
	};

	const catalog = createLoadable(async () => {
		const [provRes, typesRes] = await Promise.all([
			call(() => nodeTypeClient.listProviders(create(ListProvidersRequestSchema, {}))),
			call(() => nodeTypeClient.listNodeTypes(create(ListNodeTypesRequestSchema, {})))
		]);
		return {
			providers: (provRes.providers ?? []).map((p) => ({
				provider: p.provider,
				name: p.name,
				configured: p.configured,
				regions: [...p.regions],
				requiredKeys: [...p.requiredKeys],
				missingKeys: [...p.missingKeys]
			})),
			nodeTypes: (typesRes.nodeTypes ?? []).map((t) => ({
				id: t.id,
				name: t.name,
				vcpu: t.vcpu,
				ramMb: Number(t.ramMb ?? 0),
				diskGb: t.diskGb ?? 0,
				monthlyPriceUsd: t.monthlyPriceUsd,
				description: t.description,
				enabled: t.enabled
			}))
		};
	});

	const canProvision = $derived(can(auth.orgRole, 'node.provision'));

	let step = $state<'provider' | 'region' | 'type' | 'review' | 'plan' | 'done'>('provider');
	let provider = $state<Provider | null>(null);
	let region = $state('');
	let nodeType = $state<NodeType | null>(null);
	let nodeName = $state('');
	let error = $state('');
	let busy = $state(false);

	let provisionId = $state('');
	let planDiff = $state('');
	let planSummary = $state('');
	let outputs = $state<Record<string, string>>({});
	let applyError = $state('');

	const enabledTypes = $derived((catalog.data?.nodeTypes ?? []).filter((t) => t.enabled));

	function selectProvider(p: Provider) {
		if (!p.configured) return;
		provider = p;
		region = p.regions[0] ?? '';
		step = 'region';
	}

	function nextRegion() {
		if (!region.trim()) {
			error = 'Choose a region.';
			return;
		}
		error = '';
		step = 'type';
	}

	function selectType(t: NodeType) {
		nodeType = t;
		error = '';
		step = 'review';
	}

	async function createAndPlan() {
		error = '';
		const prov = provider;
		const nt = nodeType;
		if (!prov || !region || !nt) return;
		if (!nodeName.trim()) {
			error = 'A display name for the node is required.';
			return;
		}
		busy = true;
		try {
			const created = await call(() =>
				provisionClient.createProvision(
					create(CreateProvisionRequestSchema, {
						name: nodeName.trim(),
						provider: prov.provider as ProviderId,
						region,
						nodeTypeId: nt.id,
						planOnly: false
					})
				)
			);
			provisionId = created.provision?.id ?? '';
			const planned = await call(() =>
				provisionClient.planProvision(
					create(PlanProvisionRequestSchema, { id: provisionId })
				)
			);
			planDiff = planned.provision?.planDiff ?? '';
			planSummary = planned.provision?.planSummary ?? '';
			step = 'plan';
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		} finally {
			busy = false;
		}
	}

	let confirmText = $state('');

	async function apply() {
		applyError = '';
		if (confirmText.trim() !== provisionId) {
			applyError = 'Type the provision id to confirm the apply.';
			return;
		}
		busy = true;
		try {
			const hash = await sha256Hex(planDiff || planSummary || provisionId);
			const res = await call(() =>
				provisionClient.applyProvision(
					create(ApplyProvisionRequestSchema, { id: provisionId, confirmPlanHash: hash })
				)
			);
			outputs = { ...(res.provision?.outputs ?? {}) };
			pushToast('success', 'Provision applied.');
			step = 'done';
		} catch (e) {
			applyError = e instanceof Error ? e.message : String(e);
		} finally {
			busy = false;
		}
	}

	function restart() {
		step = 'provider';
		provider = null;
		region = '';
		nodeType = null;
		nodeName = '';
		provisionId = '';
		planDiff = '';
		planSummary = '';
		outputs = {};
		error = '';
		applyError = '';
		confirmText = '';
	}
</script>

<svelte:head><title>Provision a node · Carbon Cloud</title></svelte:head>

<div class="flex flex-col gap-4">
	{#if !canProvision}
		<EmptyState
			title="No permission"
			body="Your role does not allow provisioning nodes. Ask an organization owner or admin."
		/>
	{:else if catalog.loading}
		<div class="h-40 animate-pulse border border-border bg-card"></div>
	{:else if catalog.error}
		<div class="border border-destructive/50 bg-card p-4 text-sm">
			<p class="font-medium text-destructive">Could not load the provider catalog</p>
			<p class="mt-1">{catalog.error}</p>
			<div class="mt-3">
				<Button variant="secondary" onclick={() => void catalog.reload()}>Retry</Button>
			</div>
		</div>
	{:else}
		<div class="flex items-center gap-2 text-xs text-muted-foreground" aria-label="Provisioning steps">
			<span class={step === 'provider' ? 'font-medium text-primary' : ''}>1. Provider</span>
			<span aria-hidden="true">→</span>
			<span class={step === 'region' ? 'font-medium text-primary' : ''}>2. Region</span>
			<span aria-hidden="true">→</span>
			<span class={step === 'type' ? 'font-medium text-primary' : ''}>3. Node type</span>
			<span aria-hidden="true">→</span>
			<span class={step === 'review' || step === 'plan' || step === 'done' ? 'font-medium text-primary' : ''}>4. Review &amp; apply</span>
		</div>

		{#if step === 'provider'}
			<Tile title="Choose a provider">
				<ul class="grid grid-cols-1 gap-3 sm:grid-cols-2">
					{#each catalog.data?.providers ?? [] as p (p.provider)}
						<li>
							<button
								class="flex w-full flex-col gap-2 border p-3 text-left text-sm focus-ring disabled:cursor-not-allowed disabled:opacity-50"
								class:border-primary={p.configured}
								class:border-border={!p.configured}
								disabled={!p.configured}
								onclick={() => selectProvider(p)}
							>
								<span class="font-medium text-foreground">{p.name}</span>
								{#if p.configured}
									<span class="text-xs text-muted-foreground">
										{p.regions.length > 0 ? `${p.regions.length} regions` : 'Configured'}
									</span>
								{:else}
									<span class="text-xs text-warning">
										Missing env keys:
										{#each p.missingKeys as k, i (k)}
											<code class="font-mono text-xs">{k}</code>{i < p.missingKeys.length - 1 ? ', ' : ''}
										{/each}
									</span>
								{/if}
							</button>
						</li>
					{/each}
				</ul>
			</Tile>
		{:else if step === 'region'}
			<Tile title="Choose a region">
				<p class="mb-3 text-sm text-muted-foreground">Provider: {provider?.name}</p>
				<form
					class="flex max-w-md flex-col gap-4"
					onsubmit={(e) => {
						e.preventDefault();
						nextRegion();
					}}
				>
					<Field id="region" label="Region" required error={error || ''}>
						{#snippet control(f)}
							<select
								id={f.id}
								class="h-9 border border-input bg-card px-3 text-sm text-foreground focus-ring"
								bind:value={region}
								aria-describedby={f.describedBy}
							>
								{#each provider?.regions ?? [] as r (r)}
									<option value={r}>{r}</option>
								{/each}
							</select>
						{/snippet}
					</Field>
					<div class="flex gap-2">
						<Button variant="ghost" onclick={() => (step = 'provider')}>Back</Button>
						<Button type="submit">Next</Button>
					</div>
				</form>
			</Tile>
		{:else if step === 'type'}
			<Tile title="Choose a node type">
				<ul class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
					{#each enabledTypes as t (t.id)}
						<li>
							<button
								class="flex w-full flex-col gap-1 border border-border p-3 text-left text-sm hover:bg-accent/40 focus-ring"
								onclick={() => selectType(t)}
							>
								<span class="font-medium text-foreground">{t.name}</span>
								<span class="font-mono text-xs text-muted-foreground">
									{t.vcpu} vCPU · {fmtRam(t.ramMb)} · {fmtGb(t.diskGb)} · {fmtUsd(t.monthlyPriceUsd)}/mo
								</span>
								{#if t.description}
									<span class="text-xs text-muted-foreground">{t.description}</span>
								{/if}
							</button>
						</li>
					{/each}
				</ul>
				<div class="mt-3">
					<Button variant="ghost" onclick={() => (step = 'region')}>Back</Button>
				</div>
			</Tile>
		{:else if step === 'review'}
			<Tile title="Review">
				<dl class="mb-4 grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
					<dt class="text-muted-foreground">Provider</dt>
					<dd>{provider?.name}</dd>
					<dt class="text-muted-foreground">Region</dt>
					<dd>{region}</dd>
					<dt class="text-muted-foreground">Node type</dt>
					<dd>{nodeType?.name} ({nodeType?.vcpu} vCPU / {fmtRam(nodeType?.ramMb)} / {fmtGb(nodeType?.diskGb)})</dd>
					<dt class="text-muted-foreground">Monthly price</dt>
					<dd class="font-mono text-xs">{fmtUsd(nodeType?.monthlyPriceUsd)}</dd>
				</dl>
				<form
					class="flex max-w-md flex-col gap-4"
					onsubmit={(e) => {
						e.preventDefault();
						void createAndPlan();
					}}
				>
					<Field id="node-name" label="Node display name" required error={error || ''}>
						{#snippet control(f)}
							<input
								id={f.id}
								class="h-9 border border-input bg-card px-3 text-sm text-foreground focus-ring"
								type="text"
								placeholder="mc-eu-1"
								bind:value={nodeName}
								aria-describedby={f.describedBy}
							/>
						{/snippet}
					</Field>
					<div class="flex gap-2">
						<Button variant="ghost" onclick={() => (step = 'type')}>Back</Button>
						<Button type="submit" loading={busy}>Create &amp; plan</Button>
					</div>
				</form>
			</Tile>
		{:else if step === 'plan'}
			<Tile title="Terraform plan">
				<p class="mb-2 text-sm text-foreground">Provision <span class="font-mono text-xs">{provisionId}</span></p>
				{#if planSummary}
					<div class="mb-2">
						<Tag tone="blue">{planSummary}</Tag>
					</div>
				{/if}
				<pre class="max-h-80 overflow-auto border border-border bg-background p-3 font-mono text-xs text-foreground">{planDiff || 'Plan produced no diff output.'}</pre>

				<form
					class="mt-4 flex max-w-md flex-col gap-4"
					onsubmit={(e) => {
						e.preventDefault();
						void apply();
					}}
				>
					<Field
						id="apply-confirm"
						label="Confirm apply"
						required
						error={applyError || ''}
						hint="Type the provision id above to apply this exact plan."
					>
						{#snippet control(f)}
							<input
								id={f.id}
								class="h-9 border border-input bg-card px-3 font-mono text-sm text-foreground focus-ring"
								type="text"
								bind:value={confirmText}
								autocomplete="off"
								aria-describedby={f.describedBy}
							/>
						{/snippet}
					</Field>
					<div class="flex gap-2">
						<Button variant="ghost" onclick={restart}>Start over</Button>
						<Button type="submit" variant="danger" loading={busy}>Apply provision</Button>
					</div>
				</form>
			</Tile>
		{:else}
			<Tile title="Provision applied">
				<p class="mb-3 text-sm text-foreground">
					Provision <span class="font-mono text-xs">{provisionId}</span> applied successfully.
				</p>
				{#if Object.keys(outputs).length > 0}
					<dl class="mb-4 grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
						{#each Object.entries(outputs) as [k, v] (k)}
							<dt class="text-muted-foreground">{k}</dt>
							<dd class="font-mono text-xs break-all">{v}</dd>
						{/each}
					</dl>
				{/if}
				<Button onclick={restart}>Provision another node</Button>
			</Tile>
		{/if}
	{/if}
</div>