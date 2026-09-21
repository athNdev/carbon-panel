<script lang="ts">
	import { Dialog as DialogPrimitive } from 'bits-ui';
	import {
		DialogContent,
		DialogDescription,
		DialogFooter,
		DialogHeader,
		DialogTitle
	} from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import { RadioGroup, RadioGroupItem } from '$lib/components/ui/radio-group';
	import { Label } from '$lib/components/ui/label';
	import { Alert, AlertDescription, AlertTitle } from '$lib/components/ui/alert';
	import {
		Rocket,
		Loader2,
		Radio,
		HardDrive,
		CheckCircle2,
		Server as ServerIcon,
		Info
	} from '@lucide/svelte';
	import { toast } from 'svelte-sonner';
	import { rpcClient } from '$lib/api/rpc-client';
	import { apiFetch } from '$lib/api/fetch';
	import type { Server } from '$lib/proto/carbonpanel/v1/common_pb';

	interface Props {
		open: boolean;
		packId: string;
		packName: string;
		mcVersion: string;
		modLoader: string;
		onSuccess?: () => void;
	}

	let {
		open = $bindable(false),
		packId,
		packName,
		mcVersion,
		modLoader,
		onSuccess
	}: Props = $props();

	let servers = $state<Server[]>([]);
	let loadingServers = $state(false);
	let selectedServerId = $state('');
	let deployMode = $state<'sync' | 'bake'>('sync');
	let deploying = $state(false);
	let deployResult = $state<string | null>(null);

	async function loadServers() {
		loadingServers = true;
		try {
			const res = await rpcClient.server.listServers({});
			servers = res.servers || [];
			if (servers.length > 0 && !selectedServerId) {
				selectedServerId = servers[0].id;
			}
		} catch (err) {
			console.error('Failed to load servers:', err);
			toast.error('Failed to load servers list');
		} finally {
			loadingServers = false;
		}
	}

	async function executeDeploy() {
		if (!selectedServerId) {
			toast.error('Please select a target server');
			return;
		}

		deploying = true;
		deployResult = null;
		try {
			const res = await apiFetch(`/api/v1/packwiz/packs/${packId}/deploy`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({
					server_id: selectedServerId,
					mode: deployMode
				})
			});

			const data = await res.json();
			if (!res.ok || !data.success) {
				throw new Error(data.message || data.error || `HTTP ${res.status}`);
			}

			deployResult = data.message || 'Deployed successfully!';
			toast.success(deployResult || 'Deployed successfully!');
			if (onSuccess) {
				onSuccess();
			}
		} catch (err: any) {
			console.error('Failed to deploy modpack:', err);
			toast.error(err.message || 'Deployment failed');
		} finally {
			deploying = false;
		}
	}

	$effect(() => {
		if (open) {
			deployResult = null;
			loadServers();
		}
	});

	let selectedServer = $derived(servers.find((s) => s.id === selectedServerId));
</script>

<DialogPrimitive.Root bind:open>
	<DialogContent class="max-w-lg p-6">
		<DialogHeader>
			<DialogTitle class="flex items-center gap-2 text-xl font-bold">
				<Rocket class="h-5 w-5 text-primary" />
				Deploy "{packName}" to Server
			</DialogTitle>
			<DialogDescription>
				Deploy this Packwiz modpack ({modLoader.toUpperCase()} · MC {mcVersion}) to an existing
				server instance.
			</DialogDescription>
		</DialogHeader>

		<div class="space-y-5 py-3">
			<!-- Target Server Picker -->
			<div class="space-y-2">
				<Label class="text-sm font-semibold">Target Server Instance</Label>
				{#if loadingServers}
					<div class="flex items-center gap-2 text-xs text-muted-foreground">
						<Loader2 class="h-4 w-4 animate-spin text-primary" />
						Loading servers...
					</div>
				{:else if servers.length === 0}
					<Alert variant="destructive">
						<Info class="h-4 w-4" />
						<AlertTitle>No Servers Found</AlertTitle>
						<AlertDescription>Create a server first before deploying modpacks.</AlertDescription>
					</Alert>
				{:else}
					<Select type="single" bind:value={selectedServerId}>
						<SelectTrigger class="w-full">
							<div class="flex items-center gap-2 truncate">
								<ServerIcon class="h-4 w-4 flex-shrink-0 text-muted-foreground" />
								<span class="truncate">{selectedServer?.name || 'Select a server...'}</span>
							</div>
						</SelectTrigger>
						<SelectContent>
							{#each servers as s (s.id)}
								<SelectItem value={s.id}>
									<div class="flex w-full items-center justify-between gap-4">
										<span>{s.name}</span>
										<div class="flex items-center gap-1.5 font-mono text-xs text-muted-foreground">
											<span>MC {s.mcVersion}</span>
										</div>
									</div>
								</SelectItem>
							{/each}
						</SelectContent>
					</Select>
				{/if}
			</div>

			<!-- Deployment Mode Selection -->
			<div class="space-y-3">
				<Label class="text-sm font-semibold">Deployment Engine Mode</Label>

				<div class="grid grid-cols-1 gap-3">
					<!-- Mode A: Live Sync URL -->
					<div
						role="button"
						tabindex="0"
						onclick={() => (deployMode = 'sync')}
						onkeydown={(e) => e.key === 'Enter' && (deployMode = 'sync')}
						class="flex cursor-pointer items-start gap-3 rounded-lg border p-3.5 transition-colors
							{deployMode === 'sync' ? 'border-primary bg-primary/10' : 'border-border hover:bg-muted/40'}"
					>
						<Radio class="mt-0.5 h-5 w-5 flex-shrink-0 text-primary" />
						<div class="space-y-1 text-left">
							<div class="flex items-center gap-2">
								<span class="text-sm font-semibold">Mode A: Live Sync URL (itzg bootstrap)</span>
								<Badge variant="secondary" class="px-1 py-0 text-[10px]">Recommended</Badge>
							</div>
							<p class="text-xs text-muted-foreground">
								Configures container environment with <code
									class="rounded bg-muted px-1 text-primary">PACKWIZ_URL</code
								>. The server automatically syncs with Carbon Panel API on every startup.
							</p>
						</div>
					</div>

					<!-- Mode B: Bake to Server Data -->
					<div
						role="button"
						tabindex="0"
						onclick={() => (deployMode = 'bake')}
						onkeydown={(e) => e.key === 'Enter' && (deployMode = 'bake')}
						class="flex cursor-pointer items-start gap-3 rounded-lg border p-3.5 transition-colors
							{deployMode === 'bake' ? 'border-primary bg-primary/10' : 'border-border hover:bg-muted/40'}"
					>
						<HardDrive class="mt-0.5 h-5 w-5 flex-shrink-0 text-primary" />
						<div class="space-y-1 text-left">
							<div class="flex items-center gap-2">
								<span class="text-sm font-semibold">Mode B: Bake to Server Data (/data/mods)</span>
								<Badge variant="outline" class="px-1 py-0 text-[10px]">Standalone</Badge>
							</div>
							<p class="text-xs text-muted-foreground">
								Downloads and installs all server-side mod JARs directly into the server's mods
								directory. No external network calls needed during container startup.
							</p>
						</div>
					</div>
				</div>
			</div>

			{#if deployResult}
				<Alert
					variant="default"
					class="border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
				>
					<CheckCircle2 class="h-4 w-4" />
					<AlertTitle>Success</AlertTitle>
					<AlertDescription class="text-xs">{deployResult}</AlertDescription>
				</Alert>
			{/if}
		</div>

		<DialogFooter class="gap-2 sm:gap-0">
			<Button variant="outline" onclick={() => (open = false)}>Close</Button>
			<Button onclick={executeDeploy} disabled={deploying || !selectedServerId}>
				{#if deploying}
					<Loader2 class="mr-2 h-4 w-4 animate-spin" />
					Deploying...
				{:else}
					<Rocket class="mr-2 h-4 w-4" />
					Deploy to Server
				{/if}
			</Button>
		</DialogFooter>
	</DialogContent>
</DialogPrimitive.Root>
