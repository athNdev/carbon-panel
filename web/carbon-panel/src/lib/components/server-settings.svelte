<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import { Switch } from '$lib/components/ui/switch';
	import { Separator } from '$lib/components/ui/separator';
	import { rpcClient } from '$lib/api/rpc-client';
	import { create } from '@bufbuild/protobuf';
	import { toast } from 'svelte-sonner';
	import { Loader2, Save, AlertCircle, Network, Server as ServerIcon, ArrowRightLeft, ShieldCheck, CheckCircle2, RefreshCw } from '@lucide/svelte';
	import type { Server } from '$lib/proto/carbonpanel/v1/common_pb';
	import * as _ from 'lodash-es';
	import { ServerStatus, ModLoader } from '$lib/proto/carbonpanel/v1/common_pb';
	import type { UpdateServerRequest } from '$lib/proto/carbonpanel/v1/server_pb';
	import { UpdateServerRequestSchema, MigrateServerRequestSchema } from '$lib/proto/carbonpanel/v1/server_pb';
	import type { Node } from '$lib/proto/carbonpanel/v1/node_pb';
	import { NodeStatus } from '$lib/proto/carbonpanel/v1/node_pb';
	import { Badge } from '$lib/components/ui/badge';
	import type {
		GetMinecraftVersionsResponse,
		GetModLoadersResponse,
		GetDockerImagesResponse
	} from '$lib/proto/carbonpanel/v1/minecraft_pb';
	import {
		GetMinecraftVersionsRequestSchema,
		GetModLoadersRequestSchema,
		GetDockerImagesRequestSchema
	} from '$lib/proto/carbonpanel/v1/minecraft_pb';
	import { Alert, AlertDescription } from '$lib/components/ui/alert';
	import AdditionalPortsEditor from '$lib/components/additional-ports-editor.svelte';
	import { getUniqueDockerImages } from '$lib/utils';
	import DockerOverridesEditor from '$lib/components/docker-overrides-editor.svelte';
	import { enumToString } from '$lib/utils';

	interface Props {
		server: Server;
		onUpdate?: () => void;
	}

	let { server, onUpdate }: Props = $props();

	let saving = $state(false);

	function safeToString(data?: unknown): string | undefined {
		if (!data) return undefined;
		try {
			return JSON.stringify(data, (_, value) =>
				typeof value === 'bigint' ? value.toString() : value
			);
		} catch (e) {
			console.error('Failed to parse dockerOverrides:', e);
			return undefined;
		}
	}

	let formData = $state<UpdateServerRequest>(
		create(UpdateServerRequestSchema, {
			id: server.id,
			name: server.name,
			description: server.description || '',
			port: server.port,
			maxPlayers: server.maxPlayers,
			memory: server.memory,
			modLoader: enumToString(ModLoader, server.modLoader),
			mcVersion: server.mcVersion,
			dockerImage: server.dockerImage,
			detached: server.detached,
			autoStart: server.autoStart,
			tpsCommand: server.tpsCommand || '',
			modpackId: '', // Not used in this context
			modpackVersionId: '', // Not used in this context
			additionalPorts: server.additionalPorts || [],
			dockerOverrides: server.dockerOverrides
		})
	);

	let isDirty = $derived(
		formData.name !== server.name ||
			formData.description !== (server.description || '') ||
			formData.port !== server.port ||
			formData.maxPlayers !== server.maxPlayers ||
			formData.memory !== server.memory ||
			formData.modLoader !== enumToString(ModLoader, server.modLoader) ||
			formData.mcVersion !== server.mcVersion ||
			formData.dockerImage !== server.dockerImage ||
			formData.detached !== server.detached ||
			formData.autoStart !== server.autoStart ||
			formData.tpsCommand !== (server.tpsCommand || '') ||
			safeToString(formData.additionalPorts) !== safeToString(server.additionalPorts || []) ||
			safeToString($state.snapshot(formData.dockerOverrides)) !==
				safeToString(server.dockerOverrides)
	);

	// Available options
	let minecraftVersions = $state<GetMinecraftVersionsResponse | null>(null);
	let modLoaders = $state<GetModLoadersResponse | null>(null);
	let dockerImages = $state<GetDockerImagesResponse | null>(null);
	let loadingOptions = $state(true);

	// Reset state when server changes
	let previousServerId = $state(server.id);
	$effect(() => {
		if (server.id !== previousServerId) {
			previousServerId = server.id;

			// Reset form data to match new server
			formData = create(UpdateServerRequestSchema, {
				id: server.id,
				name: server.name,
				description: server.description || '',
				port: server.port,
				maxPlayers: server.maxPlayers,
				memory: server.memory,
				modLoader: enumToString(ModLoader, server.modLoader),
				mcVersion: server.mcVersion,
				dockerImage: server.dockerImage,
				detached: server.detached,
				autoStart: server.autoStart,
				tpsCommand: server.tpsCommand || '',
				modpackId: '', // Not used in this context
				modpackVersionId: '', // Not used in this context
				additionalPorts: server.additionalPorts || [],
				dockerOverrides: server.dockerOverrides
			});
			saving = false;
			// Reload options for new server
			loadOptions();
		}
	});

	// Load available options
	$effect(() => {
		loadOptions();
	});

	// Cluster Node & Migration state
	let nodes = $state<Node[]>([]);
	let loadingNodes = $state(true);
	let targetNodeId = $state<string>('');
	let forceMigrate = $state(false);
	let migrating = $state(false);
	let migrationStep = $state<string>('');

	$effect(() => {
		loadNodes();
	});

	async function loadNodes() {
		try {
			loadingNodes = true;
			const res = await rpcClient.node.listNodes({});
			nodes = res.nodes || [];
			const other = nodes.find((n) => n.id !== server.nodeId && n.enabled);
			if (other) {
				targetNodeId = other.id;
			} else if (nodes.length > 0) {
				targetNodeId = nodes[0].id;
			}
		} catch (err) {
			console.error('Failed to load nodes for migration:', err);
		} finally {
			loadingNodes = false;
		}
	}

	async function handleMigrate() {
		if (!targetNodeId || targetNodeId === server.nodeId) {
			toast.error('Please select a different target node to migrate to');
			return;
		}
		const target = nodes.find((n) => n.id === targetNodeId);
		const isLive = server.status === ServerStatus.RUNNING;
		const confirmMsg = isLive
			? `Perform live migration of "${server.name}" to node "${target?.name || targetNodeId}"?\n\n- World state will be flushed to disk\n- Server container will transition cleanly\n- Proxy routing will be updated with zero player disconnection`
			: `Migrate "${server.name}" to node "${target?.name || targetNodeId}"?`;
		if (!confirm(confirmMsg)) return;

		migrating = true;
		migrationStep = isLive ? 'Flushing world chunks and migrating container...' : 'Recreating container on target node...';
		try {
			const req = create(MigrateServerRequestSchema, {
				id: server.id,
				targetNodeId: targetNodeId,
				force: forceMigrate
			});
			const res = await rpcClient.server.migrateServer(req);
			if (res.success) {
				toast.success(res.message || `Server migrated to ${target?.name || targetNodeId} successfully!`);
				if (onUpdate) onUpdate();
				await loadNodes();
			} else {
				toast.error(res.message || 'Migration did not succeed');
			}
		} catch (err: any) {
			console.error('Live migration failed:', err);
			toast.error(`Migration failed: ${err.message || 'Unknown error'}`);
		} finally {
			migrating = false;
			migrationStep = '';
		}
	}

	async function loadOptions() {
		try {
			loadingOptions = true;
			const [versions, loaders, images] = await Promise.all([
				rpcClient.minecraft.getMinecraftVersions(create(GetMinecraftVersionsRequestSchema, {})),
				rpcClient.minecraft.getModLoaders(create(GetModLoadersRequestSchema, {})),
				rpcClient.minecraft.getDockerImages(create(GetDockerImagesRequestSchema, {}))
			]);

			minecraftVersions = versions;
			modLoaders = loaders;
			dockerImages = images;
		} catch (_e) {
			toast.error('Failed to load options');
		} finally {
			loadingOptions = false;
		}
	}

	function handleMemoryInput(e: Event) {
		const input = e.currentTarget as HTMLInputElement;
		const value = Number(input.value);

		// Prevent negative values
		if (value < 0) {
			input.value = '512';
			formData.memory = 512;
		}
	}

	function handleMaxPlayersInput(e: Event) {
		const input = e.currentTarget as HTMLInputElement;
		const value = Number(input.value);

		// Prevent negative values and zero
		if (value <= 0) {
			input.value = '1';
			formData.maxPlayers = 1;
		}
	}

	async function handleSave() {
		if (!isDirty) return;

		saving = true;
		try {
			const request = create(UpdateServerRequestSchema, formData);
			await rpcClient.server.updateServer(request);
			toast.success('Server settings updated. Restart the server to apply changes.');
			onUpdate?.();
		} catch (_e) {
			toast.error('Failed to update server settings');
		} finally {
			saving = false;
		}
	}

	function getCompatibleModLoaders(_mcVersion: string) {
		// The proto doesn't include version compatibility info, so all loaders are shown
		// Backend has SupportedVersions field but it's not populated or sent via proto
		return modLoaders?.modloaders || [];
	}
</script>

<div class="h-full space-y-6 overflow-y-auto p-4">
	{#if server.status !== ServerStatus.STOPPED}
		<Alert class="border-warning/50 bg-warning/10">
			<AlertCircle class="text-warning h-4 w-4" />
			<AlertDescription class="text-sm">
				Server must be stopped to modify these settings. Changes will take effect after restart.
			</AlertDescription>
		</Alert>
	{/if}

	<div class="grid gap-6 md:grid-cols-2">
		<div class="space-y-2">
			<Label for="name" class="text-sm font-medium">Server Name</Label>
			<Input id="name" bind:value={formData.name} placeholder="My Server" class="h-10" />
		</div>

		<div class="space-y-2">
			<Label for="description" class="text-sm font-medium">Description</Label>
			<Input
				id="description"
				bind:value={formData.description}
				placeholder="A Minecraft server"
				class="h-10"
			/>
		</div>

		<div class="space-y-2">
			<Label for="port" class="text-sm font-medium">Port</Label>
			<Input
				id="port"
				type="number"
				bind:value={formData.port}
				min="1"
				max="65535"
				disabled={server.proxyHostname !== ''}
				class="h-10"
			/>
			{#if server.proxyHostname}
				<p class="text-xs text-muted-foreground">
					Port cannot be changed for proxy-enabled servers
				</p>
			{/if}
		</div>

		<div class="space-y-2">
			<Label for="memory" class="text-sm font-medium">Memory (MB)</Label>
			<Input
				id="memory"
				type="number"
				bind:value={formData.memory}
				oninput={handleMemoryInput}
				min="512"
				class="h-10"
			/>
			<p class="text-xs text-muted-foreground">
				Recommended: {formData.modLoader === 'vanilla' ? '2048' : '4096'} MB
			</p>
		</div>

		<div class="space-y-2">
			<Label for="max_players" class="text-sm font-medium">Max Players</Label>
			<Input
				id="max_players"
				type="number"
				bind:value={formData.maxPlayers}
				oninput={handleMaxPlayersInput}
				min="1"
				max="1000"
				class="h-10"
			/>
		</div>

		<div class="space-y-2">
			<Label for="mc_version" class="text-sm font-medium">Minecraft Version</Label>
			<Select
				type="single"
				disabled={loadingOptions || server.status !== ServerStatus.STOPPED}
				value={formData.mcVersion}
				onValueChange={(value: string | undefined) => (formData.mcVersion = value || '')}
			>
				<SelectTrigger id="mc_version" class="h-10">
					<span>{formData.mcVersion || 'Select a version'}</span>
				</SelectTrigger>
				<SelectContent>
					{#if minecraftVersions}
						{#each minecraftVersions.versions as version (version.id)}
							<SelectItem value={version.id}>{version.id}</SelectItem>
						{/each}
					{/if}
				</SelectContent>
			</Select>
		</div>

		<div class="space-y-2">
			<Label for="mod_loader" class="text-sm font-medium">Mod Loader</Label>
			<Select
				type="single"
				disabled={loadingOptions || server.status !== ServerStatus.STOPPED}
				value={formData.modLoader}
				onValueChange={(value: string) => (formData.modLoader = value)}
			>
				<SelectTrigger id="mod_loader" class="h-10">
					<span
						>{modLoaders?.modloaders?.find((l) => l.name === formData.modLoader)?.displayName ||
							_.startCase(formData.modLoader) ||
							'Select a mod loader'}</span
					>
				</SelectTrigger>
				<SelectContent>
					{#if formData.mcVersion}
						{#each getCompatibleModLoaders(formData.mcVersion || '') as loader (loader.name)}
							<SelectItem value={loader.name}>
								{loader.displayName}
							</SelectItem>
						{/each}
					{/if}
				</SelectContent>
			</Select>
		</div>

		<div class="space-y-2">
			<Label for="docker_image" class="text-sm font-medium"
				>Docker Image <span class="text-xs text-muted-foreground">(Advanced)</span></Label
			>
			<Select
				type="single"
				disabled={loadingOptions || server.status !== ServerStatus.STOPPED}
				value={formData.dockerImage}
				onValueChange={(value: string | undefined) => (formData.dockerImage = value || '')}
			>
				<SelectTrigger id="docker_image" class="h-10">
					<span>{formData.dockerImage || 'Select Docker image'}</span>
				</SelectTrigger>
				<SelectContent>
					{#each getUniqueDockerImages(dockerImages?.images || []) as image (image.tag)}
						<SelectItem value={image.tag}>
							{image.displayName || image.tag}
						</SelectItem>
					{/each}
				</SelectContent>
			</Select>
		</div>

		<div class="space-y-2">
			<Label for="tps_command" class="text-sm font-medium"
				>TPS Command <span class="text-xs text-muted-foreground">(Optional)</span></Label
			>
			<Input
				id="tps_command"
				placeholder="Polling TPS command"
				bind:value={formData.tpsCommand}
				class="h-10"
			/>
			<p class="text-xs text-muted-foreground">
				Override the TPS monitoring command (empty to disable). Use " ?? " to specify fallback
				commands (e.g., "forge tps ?? neoforge tps ?? tps")
			</p>
		</div>

		<div class="space-y-4">
			<h4 class="text-sm font-semibold">Lifecycle Management</h4>

			<div class="flex items-center justify-between rounded-lg bg-muted/50 p-4">
				<div class="space-y-0.5">
					<Label for="detached" class="cursor-pointer text-sm font-medium">Detached Mode</Label>
					<p class="text-xs text-muted-foreground">
						Server continues running when Carbon Panel stops (not available for proxied servers)
					</p>
				</div>
				<Switch
					id="detached"
					checked={formData.detached}
					disabled={server.proxyHostname !== ''}
					onCheckedChange={(checked) => {
						if (checked && server.proxyHostname !== '') {
							toast.error('Cannot detach proxied servers');
							formData.detached = false;
							return;
						}
						formData.detached = checked;
						// If detaching, disable auto-start
						if (checked) {
							formData.autoStart = false;
						}
					}}
				/>
			</div>

			<div class="flex items-center justify-between rounded-lg bg-muted/50 p-4">
				<div class="space-y-0.5">
					<Label for="auto_start" class="cursor-pointer text-sm font-medium">Auto Start</Label>
					<p class="text-xs text-muted-foreground">
						Automatically start when Carbon Panel starts{formData.detached
							? ' (disabled for detached servers)'
							: ''}
					</p>
				</div>
				<Switch
					id="auto_start"
					checked={formData.autoStart}
					disabled={formData.detached}
					onCheckedChange={(checked) => {
						if (formData.detached) {
							toast.error('Cannot enable auto-start for detached servers');
							formData.autoStart = false;
							return;
						}
						formData.autoStart = checked;
					}}
				/>
			</div>
		</div>
	</div>

	<Separator class="my-4" />

	<div class="space-y-4">
		<AdditionalPortsEditor
			bind:ports={formData.additionalPorts}
			disabled={saving}
			onchange={(ports) => (formData.additionalPorts = ports)}
		/>

		<DockerOverridesEditor
			bind:overrides={formData.dockerOverrides}
			disabled={saving}
			onchange={(overrides) => (formData.dockerOverrides = overrides)}
		/>
	</div>

	<Separator class="my-6" />

	<!-- Cluster Node Placement & Live Migration Card -->
	<div class="rounded-xl border border-border/80 bg-linear-to-br from-card to-card/90 p-5 shadow-sm space-y-5">
		<div class="flex items-start justify-between gap-4">
			<div class="flex items-center gap-3">
				<div class="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
					<Network class="h-5 w-5" />
				</div>
				<div>
					<h4 class="text-base font-semibold text-foreground">Cluster Node Placement & Live Migration</h4>
					<p class="text-xs text-muted-foreground">
						Seamlessly migrate this instance across Docker hosts without player disconnection
					</p>
				</div>
			</div>
			<Badge variant="outline" class="border-primary/30 bg-primary/5 text-primary text-xs font-mono">
				Live Engine
			</Badge>
		</div>

		<!-- Current Placement Info -->
		<div class="grid grid-cols-1 sm:grid-cols-2 gap-3 p-3.5 rounded-lg bg-muted/40 border border-border/50 text-xs">
			<div>
				<span class="text-muted-foreground">Current Node:</span>
				<div class="flex items-center gap-2 mt-1">
					<span class="font-semibold text-foreground text-sm">{server.nodeId || 'default'}</span>
					{#if nodes.find((n) => n.id === server.nodeId)?.isLocal}
						<Badge variant="secondary" class="text-[10px] px-1.5 py-0 h-4">Local</Badge>
					{/if}
				</div>
			</div>
			<div>
				<span class="text-muted-foreground">Container Endpoint:</span>
				<div class="font-mono text-foreground mt-1 truncate">
					{nodes.find((n) => n.id === server.nodeId)?.host || 'local daemon'}
				</div>
			</div>
		</div>

		<!-- Target Node Migration Form -->
		<div class="space-y-3">
			<Label for="target_node" class="text-sm font-medium">Migrate to Target Node</Label>
			<div class="flex flex-col sm:flex-row items-stretch sm:items-center gap-3">
				<div class="flex-1">
					<Select type="single" bind:value={targetNodeId} disabled={migrating || loadingNodes}>
						<SelectTrigger id="target_node" class="w-full h-10">
							<span>
								{nodes.find((n) => n.id === targetNodeId)?.name || 'Select destination node...'}
							</span>
						</SelectTrigger>
						<SelectContent>
							{#each nodes as node (node.id)}
								<SelectItem value={node.id} disabled={node.id === server.nodeId || !node.enabled}>
									<div class="flex items-center justify-between w-full gap-3">
										<span>{node.name} {node.id === server.nodeId ? '(Current)' : ''}</span>
										<div class="flex items-center gap-1.5 text-xs text-muted-foreground font-mono">
											{#if node.status === NodeStatus.ONLINE}
												<span class="text-emerald-400">Online</span>
											{:else}
												<span class="text-rose-400">Offline</span>
											{/if}
											â€¢ {(Number(node.allocatedMemoryMb) / 1024).toFixed(1)} GB RAM
										</div>
									</div>
								</SelectItem>
							{/each}
						</SelectContent>
					</Select>
				</div>

				<Button
					onclick={handleMigrate}
					disabled={migrating || !targetNodeId || targetNodeId === server.nodeId}
					class="h-10 min-w-36 gap-2"
				>
					{#if migrating}
						<Loader2 class="h-4 w-4 animate-spin" />
						Migrating...
					{:else}
						<ArrowRightLeft class="h-4 w-4" />
						Migrate Node
					{/if}
				</Button>
			</div>

			<!-- Migration Helper & Force Bypass -->
			<div class="flex flex-col sm:flex-row sm:items-center justify-between gap-2 pt-1">
				<p class="text-[11px] text-muted-foreground">
					{#if server.status === ServerStatus.RUNNING}
						âš¡ <b>Live zero-downtime migration</b>: Memory & world state are synced before proxy rerouting.
					{:else}
						ðŸ’¤ <b>Offline migration</b>: Container will be initialized on the selected node.
					{/if}
				</p>
				<div class="flex items-center gap-2 text-xs text-muted-foreground">
					<Switch id="force_migrate" bind:checked={forceMigrate} disabled={migrating} />
					<Label for="force_migrate" class="cursor-pointer text-xs">Bypass capacity checks</Label>
				</div>
			</div>

			{#if migrating && migrationStep}
				<div class="flex items-center gap-2 p-3 rounded-md bg-primary/10 border border-primary/20 text-xs text-primary animate-pulse">
					<Loader2 class="h-3.5 w-3.5 animate-spin" />
					<span>{migrationStep}</span>
				</div>
			{/if}
		</div>
	</div>

	<Separator class="my-4" />

	<div class="flex justify-end pt-2">
		<Button onclick={handleSave} disabled={!isDirty || saving} size="sm" class="min-w-[120px]">
			{#if saving}
				<Loader2 class="mr-2 h-4 w-4 animate-spin" />
			{:else}
				<Save class="mr-2 h-4 w-4" />
			{/if}
			Save Changes
		</Button>
	</div>
</div>
