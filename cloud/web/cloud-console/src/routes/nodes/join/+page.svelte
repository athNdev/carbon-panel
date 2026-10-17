<script lang="ts">
	import { create } from '@bufbuild/protobuf';
	import Tile from '$lib/components/Tile.svelte';
	import Field from '$lib/components/Field.svelte';
	import Button from '$lib/components/Button.svelte';
	import CopyField from '$lib/components/CopyField.svelte';
	import Tag from '$lib/components/Tag.svelte';
	import { call, nodeClient, nodeTypeClient } from '$lib/api/client';
	import { createLoadable, sleep } from '$lib/state.svelte';
	import { CreateJoinTokenRequestSchema, ListNodesRequestSchema } from '$lib/proto/cloud/v1/node_pb';
	import { ListNodeTypesRequestSchema } from '$lib/proto/cloud/v1/nodetype_pb';
	import { tsToDate, fmtDateTime } from '$lib/utils';

	type JoinResult = {
		joinCommand: string;
		secret: string;
		expiresAt: Date | null;
		tokenName: string;
		tokenId: string;
	};

	let name = $state('');
	let nodeTypeId = $state('');
	let ttlSeconds = $state(3600);
	let error = $state('');
	let creating = $state(false);

	let result = $state<JoinResult | null>(null);
	let foundNodeId = $state<string | null>(null);
	let pollError = $state<string | null>(null);
	let lastPoll = $state<Date | null>(null);
	let remaining = $state<number>(0);

	const nodeTypes = createLoadable(async () => {
		const res = await call(() => nodeTypeClient.listNodeTypes(create(ListNodeTypesRequestSchema, {})));
		return (res.nodeTypes ?? []).filter((t) => t.enabled);
	});

	function tick() {
		if (!result?.expiresAt) return;
		remaining = Math.max(0, Math.round((result.expiresAt.getTime() - Date.now()) / 1000));
	}

	async function createToken() {
		error = '';
		if (!name.trim()) {
			error = 'A name is required.';
			return;
		}
		if (!nodeTypeId) {
			error = 'Choose a node type.';
			return;
		}
		creating = true;
		try {
			const res = await call(() =>
				nodeClient.createJoinToken(
					create(CreateJoinTokenRequestSchema, {
						name: name.trim(),
						nodeTypeId,
						ttlSeconds: BigInt(ttlSeconds)
					})
				)
			);
			result = {
				joinCommand: res.joinCommand,
				secret: res.secret,
				expiresAt: tsToDate(res.token?.expiresAt),
				tokenName: name.trim(),
				tokenId: res.token?.id ?? ''
			};
			tick();
			void pollForNode();
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		} finally {
			creating = false;
		}
	}

	async function pollForNode() {
		if (!result) return;
		const wantedName = result.tokenName;
		while (true) {
			if (result.expiresAt && result.expiresAt.getTime() < Date.now()) break;
			await sleep(5000);
			if (!result) return;
			try {
				const res = await call(() => nodeClient.listNodes(create(ListNodesRequestSchema, {})));
				lastPoll = new Date();
				const match = (res.nodes ?? []).find((n) => n.name === wantedName);
				if (match) {
					foundNodeId = match.id;
					return;
				}
				pollError = null;
			} catch (e) {
				pollError = e instanceof Error ? e.message : String(e);
			}
		}
	}

	$effect(() => {
		if (!result?.expiresAt) return;
		const timer = setInterval(tick, 1000);
		return () => clearInterval(timer);
	});
</script>

<svelte:head><title>Join a node · Carbon Cloud</title></svelte:head>

<div class="flex flex-col gap-4">
	{#if nodeTypes.loading}
		<div class="h-24 animate-pulse border border-border bg-card"></div>
	{:else if nodeTypes.error}
		<div class="border border-destructive/50 bg-card p-4 text-sm">
			<p class="font-medium text-destructive">Could not load node types</p>
			<p class="mt-1">{nodeTypes.error}</p>
		</div>
	{:else}
		{#if !result}
			<Tile title="Bring your own node">
				<p class="mb-4 text-sm text-muted-foreground">
					Create a single-use join token, then run the join command on the machine you want to
					add. The token expires automatically.
				</p>
				<form
					class="flex max-w-md flex-col gap-4"
					onsubmit={(e) => {
						e.preventDefault();
						void createToken();
					}}
				>
					<Field id="join-name" label="Node name" required error={error || ''} hint="A short label, e.g. homeserver-1">
						{#snippet control(f)}
							<input
								id={f.id}
								class="h-9 border border-input bg-card px-3 text-sm text-foreground focus-ring"
								type="text"
								placeholder="homeserver-1"
								bind:value={name}
								aria-describedby={f.describedBy}
							/>
						{/snippet}
					</Field>
					<Field id="join-type" label="Node type" required>
						{#snippet control(f)}
							<select
								id={f.id}
								class="h-9 border border-input bg-card px-3 text-sm text-foreground focus-ring"
								bind:value={nodeTypeId}
								aria-describedby={f.describedBy}
							>
								<option value="" disabled>Choose a node type…</option>
								{#each nodeTypes.data ?? [] as t (t.id)}
									<option value={t.id}>{t.name} — {t.vcpu} vCPU / {Math.round(Number(t.ramMb) / 1024)} GiB</option>
								{/each}
							</select>
						{/snippet}
					</Field>
					<Field id="join-ttl" label="Token lifetime (seconds)" hint="The server clamps this to its configured maximum.">
						{#snippet control(f)}
							<input
								id={f.id}
								class="h-9 w-40 border border-input bg-card px-3 font-mono text-sm text-foreground focus-ring"
								type="number"
								min="60"
								max="86400"
								step="60"
								bind:value={ttlSeconds}
								aria-describedby={f.describedBy}
							/>
						{/snippet}
					</Field>
					<div>
						<Button type="submit" loading={creating} disabled={creating}>Create join token</Button>
					</div>
				</form>
			</Tile>
		{:else}
			<Tile title="Join command">
				<p class="mb-3 text-sm text-foreground">
					Run this on the target machine to register it. It is valid for{' '}
					<span class="font-mono text-xs">{remaining}s</span> ({fmtDateTime(result.expiresAt)}).
				</p>
				<CopyField label="Join command" value={result.joinCommand} />
				<div class="mt-3">
					<CopyField label="Join token secret (shown once)" value={result.secret} secret />
					<p class="mt-1 text-xs text-warning">
						This secret is shown only once. Keep it safe.
					</p>
				</div>

				<div class="mt-5 border-t border-border pt-4">
					{#if foundNodeId}
						<div class="flex items-center gap-2">
							<Tag tone="green">Connected</Tag>
							<a class="text-sm text-primary hover:underline focus-ring" href={`/nodes/${foundNodeId}`}>
								View the node →
							</a>
						</div>
					{:else}
						<div class="flex items-center gap-2" role="status">
							<span class="inline-block h-2 w-2 animate-pulse rounded-full bg-primary" aria-hidden="true"></span>
							<p class="text-sm text-muted-foreground">
								Waiting for the node to connect{lastPoll ? ` (last checked ${lastPoll.toLocaleTimeString()})` : ''}…
							</p>
							{#if pollError}
								<p class="text-xs text-destructive">Poll failed: {pollError}</p>
							{/if}
						</div>
						{#if remaining === 0}
							<p class="mt-2 text-xs text-destructive">
								This token has expired. Create a new one to continue.
							</p>
						{/if}
					{/if}
				</div>
			</Tile>
		{/if}
	{/if}
</div>