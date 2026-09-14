<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { rpcClient } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import {
		ArrowLeft,
		Loader2,
		Package,
		Settings,
		HardDrive,
		Server as ServerIcon,
		Network,
		Cpu,
		Activity,
		CheckCircle2,
		AlertCircle,
		ChevronRight,
		ChevronLeft,
		Plus,
		Check
	} from '@lucide/svelte';
	import { create } from '@bufbuild/protobuf';
	import type { CreateServerRequest } from '$lib/proto/carbonpanel/v1/server_pb';
	import { CreateServerRequestSchema } from '$lib/proto/carbonpanel/v1/server_pb';
	import { ModLoader, type ProxyListener } from '$lib/proto/carbonpanel/v1/common_pb';
	import type { ModLoaderInfo, DockerImage } from '$lib/proto/carbonpanel/v1/minecraft_pb';
	import type { IndexedModpack, Version } from '$lib/proto/carbonpanel/v1/modpack_pb';
	import type { Node } from '$lib/proto/carbonpanel/v1/node_pb';
	import { NodeStatus } from '$lib/proto/carbonpanel/v1/node_pb';
	import { CarbonTag, CarbonButton, CarbonTile } from '$lib/components/carbon';
	import {
		Dialog,
		DialogContent,
		DialogDescription,
		DialogHeader,
		DialogTitle
	} from '$lib/components/ui/dialog';
	import AdditionalPortsEditor from '$lib/components/additional-ports-editor.svelte';
	import DockerOverridesEditor from '$lib/components/docker-overrides-editor.svelte';
	import { getUniqueDockerImages, getDockerImageDisplayName } from '$lib/utils';
	import * as _ from 'lodash-es';

	let loading = $state(false);
	let loadingVersions = $state(true);
	let minecraftVersions = $state<string[]>([]);
	let modLoaders = $state<ModLoaderInfo[]>([]);
	let dockerImages = $state<DockerImage[]>([]);
	let latestVersion = $state('');
	let proxyEnabled = $state(false);
	let proxyBaseURL = $state('');
	let proxyListeners = $state<ProxyListener[]>([]);
	let usedPorts = $state<Record<number, boolean>>({});
	let portError = $state('');
	let useProxyMode = $state(false);

	// Wizard step tracking
	let currentStep = $state(0);
	const steps = [
		{ id: 0, title: 'Basic Information', subtitle: 'Name & preset' },
		{ id: 1, title: 'Game Engine', subtitle: 'Version & loader' },
		{ id: 2, title: 'Resources & Network', subtitle: 'Port & RAM' },
		{ id: 3, title: 'Cluster & Runtime', subtitle: 'Placement & flags' }
	];

	// Cluster node placement state
	let nodes = $state<Node[]>([]);
	let loadingNodes = $state(true);
	let nodePlacementMode = $state<'auto' | 'manual'>('auto');
	let selectedNodeId = $state<string>('default');
	let placementStrategy = $state<string>('least_memory');

	// Modpack selection
	let showModpackDialog = $state(false);
	let selectedModpack = $state<IndexedModpack | null>(null);
	let favoriteModpacks = $state<IndexedModpack[]>([]);
	let modpackVersions = $state<Version[]>([]);
	let selectedVersionId = $state<string>('');
	let loadingModpackVersions = $state(false);

	let formData = $state<CreateServerRequest>(
		create(CreateServerRequestSchema, {
			name: '',
			description: '',
			modLoader: ModLoader.UNSPECIFIED,
			mcVersion: '',
			port: 25565,
			maxPlayers: 20,
			memory: 2048,
			dockerImage: '',
			autoStart: false,
			detached: false,
			startImmediately: false,
			proxyHostname: '',
			proxyListenerId: '',
			useBaseUrl: false,
			additionalPorts: [],
			dockerOverrides: undefined,
			modpackId: '',
			modpackVersionId: ''
		})
	);

	onMount(async () => {
		try {
			const [versionsData, loadersData, imagesData, proxyStatus, portData, listeners, nodesData] =
				await Promise.allSettled([
					rpcClient.minecraft.getMinecraftVersions({}),
					rpcClient.minecraft.getModLoaders({}),
					rpcClient.minecraft.getDockerImages({}),
					rpcClient.proxy.getProxyStatus({}),
					rpcClient.server.getNextAvailablePort({}),
					rpcClient.proxy.getProxyListeners({}),
					rpcClient.node.listNodes({})
				]);

			if (versionsData.status === 'fulfilled') {
				minecraftVersions = versionsData.value.versions.map((v) => v.id);
				latestVersion = versionsData.value.latest;
				if (!formData.mcVersion && latestVersion) {
					formData.mcVersion = latestVersion;
				}
			}

			if (loadersData.status === 'fulfilled') {
				modLoaders = loadersData.value.modloaders;
			}

			if (imagesData.status === 'fulfilled') {
				dockerImages = imagesData.value.images;
			}

			if (proxyStatus.status === 'fulfilled') {
				proxyEnabled = proxyStatus.value.enabled;
				proxyBaseURL = proxyStatus.value.baseUrl || '';
			}

			if (listeners.status === 'fulfilled') {
				proxyListeners = listeners.value.listeners
					.map((l) => l.listener)
					.filter((l): l is ProxyListener => l !== undefined && l.enabled);

				const defaultListener = proxyListeners.find((l) => l?.isDefault);
				if (defaultListener) {
					formData.proxyListenerId = defaultListener.id;
				} else if (proxyListeners.length > 0) {
					formData.proxyListenerId = proxyListeners[0]?.id || '';
				}
			}

			if (portData.status === 'fulfilled') {
				formData.port = portData.value.port;
				usedPorts = Object.fromEntries(portData.value.usedPorts?.map((p) => [p.port, p.inUse]) || []);
			}

			if (nodesData.status === 'fulfilled') {
				nodes = nodesData.value.nodes || [];
				if (nodes.length > 0) {
					selectedNodeId = nodes[0].id;
				}
			}
		} catch (error) {
			console.error('Initialization error:', error);
		} finally {
			loadingVersions = false;
			loadingNodes = false;
		}

		loadFavoriteModpacks();
	});

	async function loadFavoriteModpacks() {
		try {
			const result = await rpcClient.modpack.listFavorites({});
			favoriteModpacks = result.modpacks;
		} catch (error) {
			console.error('Failed to load favorite modpacks:', error);
		}
	}

	async function loadModpackVersions(modpackId: string) {
		loadingModpackVersions = true;
		modpackVersions = [];
		selectedVersionId = '';

		try {
			const data = await rpcClient.modpack.getModpackVersions({
				id: modpackId,
				gameVersion: '',
				modLoader: ''
			});
			modpackVersions = data.versions || [];
		} catch (error) {
			console.error('Failed to load modpack versions:', error);
			modpackVersions = [];
		} finally {
			loadingModpackVersions = false;
		}
	}

	async function selectModpack(modpack: IndexedModpack) {
		selectedModpack = modpack;
		showModpackDialog = false;

		try {
			const cfg = await rpcClient.modpack.getModpackConfig({ id: modpack.id });
			formData.name = modpack.name || '';
			formData.description = modpack.summary || '';
			formData.modLoader = _.get(cfg, 'mod_loader', 0);
			formData.mcVersion = modpack.mcVersion || '';
			formData.memory = _.get(cfg, 'memory', modpack.recommendedRam || 2048);
			formData.dockerImage = modpack.dockerImage || '';
			await loadModpackVersions(modpack.id);
		} catch (error) {
			toast.error('Failed to load modpack configuration');
			console.error(error);
			selectedModpack = null;
		}
	}

	function removeModpack() {
		selectedModpack = null;
		modpackVersions = [];
		selectedVersionId = '';
		formData.modLoader = 0;
		formData.mcVersion = latestVersion || '';
		formData.dockerImage = '';
		formData.memory = 2048;
	}

	function parseJsonArray(jsonStr: string): string[] {
		try {
			return JSON.parse(jsonStr);
		} catch {
			return [];
		}
	}

	function validatePort(port: number) {
		portError = '';

		if (port < 1 || port > 65535) {
			portError = 'Port must be between 1 and 65535';
			return false;
		}

		if (usedPorts[port]) {
			portError = 'This port is already in use';
			return false;
		}

		return true;
	}

	async function refreshAvailablePort() {
		try {
			const portData = await rpcClient.server.getNextAvailablePort({});
			formData.port = portData.port;
			usedPorts = Object.fromEntries(portData.usedPorts?.map((p) => [p.port, p.inUse]) || []);
			portError = '';
		} catch (error) {
			console.error('Failed to get available port:', error);
		}
	}

	function canProceed(step: number): boolean {
		if (step === 0) {
			if (!formData.name.trim()) {
				toast.error('Server name is required');
				return false;
			}
		}
		if (step === 2) {
			if (!useProxyMode && !validatePort(formData.port)) {
				toast.error('Please enter a valid, available port');
				return false;
			}
		}
		return true;
	}

	function goToStep(stepIndex: number) {
		if (stepIndex > currentStep) {
			for (let i = currentStep; i < stepIndex; i++) {
				if (!canProceed(i)) return;
			}
		}
		currentStep = stepIndex;
	}

	function nextStep() {
		if (canProceed(currentStep)) {
			if (currentStep < steps.length - 1) {
				currentStep++;
			}
		}
	}

	function prevStep() {
		if (currentStep > 0) {
			currentStep--;
		}
	}

	async function handleSubmit(e?: Event) {
		if (e) e.preventDefault();

		if (!formData.name.trim()) {
			toast.error('Server name is required');
			currentStep = 0;
			return;
		}

		if (!useProxyMode && !validatePort(formData.port)) {
			toast.error('Please select a valid port');
			currentStep = 2;
			return;
		}

		loading = true;
		try {
			const selectedVersion = modpackVersions.find((v) => v.id === selectedVersionId);
			const versionToSend =
				selectedModpack?.indexer === 'modrinth' && selectedVersion?.versionNumber
					? selectedVersion.versionNumber
					: selectedVersionId;

			const createRequest = {
				...formData,
				nodeId: nodePlacementMode === 'manual' ? selectedNodeId : '',
				placementStrategy: nodePlacementMode === 'auto' ? placementStrategy : '',
				modpackId: selectedModpack?.id || '',
				modpackVersionId: versionToSend || '',
				port: useProxyMode ? 0 : formData.port
			};

			const response = await rpcClient.server.createServer(createRequest);
			toast.success(`Server "${response.server?.name}" created successfully!`);
			goto(resolve(`/servers/${response.server?.id}`));
		} catch (error) {
			toast.error(
				`Failed to create server: ${error instanceof Error ? error.message : 'Unknown error'}`
			);
		} finally {
			loading = false;
		}
	}
</script>

<div class="h-full overflow-y-auto bg-[#161616] text-[#f4f4f4] font-sans">
	<div class="space-y-6 p-6">
		<!-- Carbon Page Header -->
		<div class="flex items-center gap-4 border-b border-[#393939] pb-5">
			<a
				href="/servers"
				class="flex h-10 w-10 shrink-0 items-center justify-center bg-[#262626] hover:bg-[#353535] border border-[#393939] text-[#c6c6c6] hover:text-white transition-colors rounded-none"
				title="Back to Servers"
			>
				<ArrowLeft class="h-4 w-4" />
			</a>
			<div>
				<div class="flex items-center gap-2.5">
					<h1 class="text-2xl font-light tracking-tight text-[#f4f4f4]">Create New Server</h1>
					<span class="px-2 py-0.5 bg-[#0f62fe]/20 text-[#78a9ff] border border-[#0f62fe]/40 text-xs font-mono">
						WIZARD
					</span>
				</div>
				<p class="text-xs text-[#a8a8a8] mt-1 font-sans">
					Configure instance runtime, container engine, resource quotas, and network proxy
				</p>
			</div>
		</div>

		<!-- Carbon Progress Indicator / Steps (Requirement 3) -->
		<div class="w-full bg-[#262626] border border-[#393939] p-4 rounded-none">
			<div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
				{#each steps as step (step.id)}
					<button
						type="button"
						onclick={() => goToStep(step.id)}
						class="flex items-center gap-3 p-3 text-left transition-colors border cursor-pointer rounded-none relative select-none {currentStep === step.id ? 'bg-[#353535] border-[#0f62fe] border-l-4 border-l-[#0f62fe]' : currentStep > step.id ? 'bg-[#161616] border-[#393939] hover:bg-[#2e2e2e]' : 'bg-[#161616] border-[#2e2e2e] opacity-70 hover:opacity-100'}"
					>
						<!-- Indicator Badge -->
						<div
							class="flex h-7 w-7 shrink-0 items-center justify-center font-mono text-xs font-bold rounded-none {currentStep === step.id ? 'bg-[#0f62fe] text-white' : currentStep > step.id ? 'bg-[#24a148] text-white' : 'bg-[#393939] text-[#8d8d8d]'}"
						>
							{#if currentStep > step.id}
								<Check class="h-4 w-4" />
							{:else}
								{step.id + 1}
							{/if}
						</div>

						<div class="min-w-0 flex-1">
							<p class="text-xs font-semibold tracking-wide uppercase truncate {currentStep === step.id ? 'text-[#f4f4f4]' : currentStep > step.id ? 'text-[#c6c6c6]' : 'text-[#8d8d8d]'}">
								{step.title}
							</p>
							<p class="text-[11px] text-[#8d8d8d] truncate mt-0.5 font-sans">
								{step.subtitle}
							</p>
						</div>

						{#if currentStep === step.id}
							<div class="absolute right-0 top-0 bottom-0 w-1 bg-[#0f62fe]"></div>
						{/if}
					</button>
				{/each}
			</div>
		</div>

		<!-- Wizard Form Container -->
		<form onsubmit={handleSubmit} class="space-y-6">
			<!-- Step 0: Basic Information & Preset -->
			{#if currentStep === 0}
				<div class="bg-[#262626] border border-[#393939] p-6 rounded-none space-y-6">
					<div>
						<h2 class="text-lg font-light text-[#f4f4f4]">Step 1: Basic Information & Preset</h2>
						<p class="text-xs text-[#a8a8a8] mt-1 font-sans">
							Provide server identification and select whether to build manual config or load from a curated modpack.
						</p>
					</div>

					<!-- Preset Selection (Carbon Tiles) -->
					<div class="space-y-2">
						<label class="block text-xs text-[#c6c6c6] uppercase font-mono tracking-wider font-semibold">
							Configuration Method
						</label>
						<div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
							<!-- Manual Tile -->
							<button
								type="button"
								onclick={() => (selectedModpack = null)}
								class="p-4 text-left border transition-colors cursor-pointer rounded-none relative {selectedModpack === null ? 'bg-[#353535] border-[#0f62fe] border-l-4 border-l-[#0f62fe]' : 'bg-[#161616] border-[#393939] hover:bg-[#2a2a2a]'}"
							>
								<div class="flex items-center gap-2">
									<Settings class="h-4 w-4 text-[#0f62fe]" />
									<h4 class="font-semibold text-sm text-[#f4f4f4]">Manual Configuration</h4>
								</div>
								<p class="text-xs text-[#a8a8a8] mt-1">
									Start with clean Minecraft server settings and custom modloaders.
								</p>
							</button>

							<!-- Modpack Tile -->
							<button
								type="button"
								onclick={() => (showModpackDialog = true)}
								class="p-4 text-left border transition-colors cursor-pointer rounded-none relative {selectedModpack !== null ? 'bg-[#353535] border-[#0f62fe] border-l-4 border-l-[#0f62fe]' : 'bg-[#161616] border-[#393939] hover:bg-[#2a2a2a]'}"
							>
								<div class="flex items-center gap-2">
									<Package class="h-4 w-4 text-[#78a9ff]" />
									<h4 class="font-semibold text-sm text-[#f4f4f4]">
										{selectedModpack ? selectedModpack.name : 'From Modpack Preset'}
									</h4>
								</div>
								<p class="text-xs text-[#a8a8a8] mt-1">
									{selectedModpack
										? `${selectedModpack.indexer} • MC ${selectedModpack.mcVersion}`
										: favoriteModpacks.length === 0
											? 'Browse favorites or modpack repo'
											: 'Select from favorite saved modpacks'}
								</p>
							</button>
						</div>

						{#if selectedModpack}
							<div class="mt-3 p-4 bg-[#161616] border border-[#0f62fe] rounded-none flex items-start justify-between gap-4">
								<div class="flex items-start gap-3 min-w-0">
									{#if selectedModpack.logoUrl}
										<img
											src={selectedModpack.logoUrl}
											alt={selectedModpack.name}
											class="h-12 w-12 object-cover border border-[#393939] rounded-none shrink-0"
										/>
									{/if}
									<div class="min-w-0">
										<div class="flex items-center gap-2">
											<h4 class="font-semibold text-sm text-[#f4f4f4]">{selectedModpack.name}</h4>
											<CarbonTag type="blue" size="sm">{selectedModpack.indexer}</CarbonTag>
										</div>
										<p class="text-xs text-[#a8a8a8] line-clamp-2 mt-1 font-sans">
											{selectedModpack.summary}
										</p>
										{#if modpackVersions.length > 0}
											<div class="mt-2 flex items-center gap-2">
												<span class="text-xs text-[#c6c6c6] font-mono">Version:</span>
												<select
													bind:value={selectedVersionId}
													class="h-8 px-2 bg-[#262626] border border-[#525252] text-xs font-mono text-[#f4f4f4] rounded-none focus:outline-none focus:border-[#0f62fe]"
												>
													<option value="">Latest Release</option>
													{#each modpackVersions as version (version.id)}
														<option value={version.id}>
															{version.displayName} {version.releaseType ? `(${version.releaseType})` : ''}
														</option>
													{/each}
												</select>
											</div>
										{/if}
									</div>
								</div>
								<button
									type="button"
									onclick={removeModpack}
									class="px-3 py-1.5 bg-[#393939] hover:bg-[#da1e28] text-white text-xs font-mono uppercase transition-colors cursor-pointer rounded-none shrink-0"
								>
									Remove
								</button>
							</div>
						{/if}
					</div>

					<!-- Server Name (Carbon TextInput) -->
					<div class="space-y-1.5">
						<label for="name" class="block text-xs text-[#c6c6c6] uppercase font-mono tracking-wider font-semibold">
							Server Name <span class="text-[#ff8389]">*</span>
						</label>
						<input
							id="name"
							type="text"
							placeholder="survival-smp-01"
							bind:value={formData.name}
							required
							class="w-full h-10 px-4 bg-[#161616] border-b border-[#8d8d8d] focus:border-b-2 focus:border-[#0f62fe] focus:bg-[#262626] focus:outline-none text-sm text-[#f4f4f4] placeholder-[#6f6f6f] rounded-none transition-all"
						/>
					</div>

					<!-- Server Description -->
					<div class="space-y-1.5">
						<label for="description" class="block text-xs text-[#c6c6c6] uppercase font-mono tracking-wider font-semibold">
							Description <span class="text-xs text-[#8d8d8d] lowercase font-normal">(optional)</span>
						</label>
						<textarea
							id="description"
							placeholder="Primary survival server instance for cluster..."
							bind:value={formData.description}
							rows={3}
							class="w-full p-3 bg-[#161616] border-b border-[#8d8d8d] focus:border-b-2 focus:border-[#0f62fe] focus:bg-[#262626] focus:outline-none text-sm text-[#f4f4f4] placeholder-[#6f6f6f] rounded-none resize-none transition-all"
						></textarea>
					</div>
				</div>

			<!-- Step 1: Game Engine & Version -->
			{:else if currentStep === 1}
				<div class="bg-[#262626] border border-[#393939] p-6 rounded-none space-y-6">
					<div>
						<h2 class="text-lg font-light text-[#f4f4f4]">Step 2: Engine & Version</h2>
						<p class="text-xs text-[#a8a8a8] mt-1 font-sans">
							Select the Minecraft server release and runtime loader.
						</p>
					</div>

					<!-- Minecraft Version (Carbon Select) -->
					<div class="space-y-1.5">
						<label for="mcVersion" class="block text-xs text-[#c6c6c6] uppercase font-mono tracking-wider font-semibold">
							Minecraft Version
						</label>
						{#if loadingVersions}
							<div class="flex items-center gap-2 p-3 text-xs text-[#8d8d8d] bg-[#161616] border border-[#393939]">
								<Loader2 class="h-4 w-4 animate-spin text-[#0f62fe]" />
								<span>Loading supported versions catalog...</span>
							</div>
						{:else}
							<div class="relative">
								<select
									id="mcVersion"
									bind:value={formData.mcVersion}
									disabled={loading}
									class="w-full h-10 px-4 pr-10 bg-[#161616] border-b border-[#8d8d8d] focus:border-b-2 focus:border-[#0f62fe] focus:bg-[#262626] focus:outline-none text-sm text-[#f4f4f4] rounded-none appearance-none cursor-pointer"
								>
									{#each minecraftVersions as version (version)}
										<option value={version}>
											{version} {version === latestVersion ? '(Latest Official)' : ''}
										</option>
									{/each}
								</select>
								<div class="pointer-events-none absolute inset-y-0 right-0 flex items-center px-3 text-[#c6c6c6]">
									<svg class="h-4 w-4" fill="currentColor" viewBox="0 0 20 20">
										<path fill-rule="evenodd" d="M5.293 7.293a1 1 0 011.414 0L10 10.586l3.293-3.293a1 1 0 111.414 1.414l-4 4a1 1 0 01-1.414 0l-4-4a1 1 0 010-1.414z" clip-rule="evenodd" />
									</svg>
								</div>
							</div>
						{/if}
					</div>

					<!-- Mod Loader (Carbon Select) -->
					<div class="space-y-1.5">
						<label for="modLoader" class="block text-xs text-[#c6c6c6] uppercase font-mono tracking-wider font-semibold">
							Mod Loader / Server Software
						</label>
						<div class="relative">
							<select
								id="modLoader"
								value={formData.modLoader.toString()}
								onchange={(e) => {
									const val = Number(e.currentTarget.value);
									formData.modLoader = val as ModLoader;
								}}
								disabled={loading || !!selectedModpack}
								class="w-full h-10 px-4 pr-10 bg-[#161616] border-b border-[#8d8d8d] focus:border-b-2 focus:border-[#0f62fe] focus:bg-[#262626] focus:outline-none text-sm text-[#f4f4f4] rounded-none appearance-none cursor-pointer disabled:opacity-50"
							>
								{#each modLoaders as loader (loader.name)}
									{@const enumVal = ModLoader[loader.name.toUpperCase() as keyof typeof ModLoader] ?? 0}
									<option value={enumVal.toString()}>
										{loader.displayName}
									</option>
								{/each}
							</select>
							<div class="pointer-events-none absolute inset-y-0 right-0 flex items-center px-3 text-[#c6c6c6]">
								<svg class="h-4 w-4" fill="currentColor" viewBox="0 0 20 20">
									<path fill-rule="evenodd" d="M5.293 7.293a1 1 0 011.414 0L10 10.586l3.293-3.293a1 1 0 111.414 1.414l-4 4a1 1 0 01-1.414 0l-4-4a1 1 0 010-1.414z" clip-rule="evenodd" />
								</svg>
							</div>
						</div>

						{#if selectedModpack}
							<p class="text-xs text-[#78a9ff] font-mono mt-1">
								Mod loader auto-determined by preset configuration.
							</p>
						{:else if formData.modLoader === ModLoader.VANILLA}
							<p class="text-xs text-[#8d8d8d] font-sans mt-1">
								Vanilla Minecraft server binary with no mod dependencies.
							</p>
						{:else}
							<p class="text-xs text-[#6fdc8c] font-sans mt-1">
								Supports runtime mod jar injection and server extensions.
							</p>
						{/if}
					</div>
				</div>

			<!-- Step 2: Resources & Network -->
			{:else if currentStep === 2}
				<div class="bg-[#262626] border border-[#393939] p-6 rounded-none space-y-6">
					<div>
						<h2 class="text-lg font-light text-[#f4f4f4]">Step 3: Resources & Network</h2>
						<p class="text-xs text-[#a8a8a8] mt-1 font-sans">
							Configure port routing, reverse proxy domain, and system memory limits.
						</p>
					</div>

					<!-- Connection Method (Carbon Tiles) -->
					{#if proxyEnabled}
						<div class="space-y-2">
							<label class="block text-xs text-[#c6c6c6] uppercase font-mono tracking-wider font-semibold">
								Connection Ingress Method
							</label>
							<div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
								<button
									type="button"
									onclick={() => {
										useProxyMode = false;
										formData.proxyHostname = '';
										portError = '';
									}}
									class="p-4 text-left border transition-colors cursor-pointer rounded-none relative {!useProxyMode ? 'bg-[#353535] border-[#0f62fe] border-l-4 border-l-[#0f62fe]' : 'bg-[#161616] border-[#393939] hover:bg-[#2a2a2a]'}"
								>
									<h4 class="font-semibold text-sm text-[#f4f4f4]">Direct Port</h4>
									<p class="text-xs text-[#a8a8a8] mt-1">
										Bind directly to container host port (e.g. :25565)
									</p>
								</button>
								<button
									type="button"
									onclick={() => {
										useProxyMode = true;
										if (!formData.proxyHostname) {
											formData.proxyHostname =
												formData.name.toLowerCase().replace(/\s+/g, '-') || 'minecraft-server';
										}
										const currentListener = proxyListeners.find((l) => l.id === formData.proxyListenerId) || proxyListeners[0];
										if (currentListener) {
											formData.port = currentListener.port;
										}
										portError = '';
									}}
									class="p-4 text-left border transition-colors cursor-pointer rounded-none relative {useProxyMode ? 'bg-[#353535] border-[#0f62fe] border-l-4 border-l-[#0f62fe]' : 'bg-[#161616] border-[#393939] hover:bg-[#2a2a2a]'}"
								>
									<h4 class="font-semibold text-sm text-[#f4f4f4]">SNI / Custom Hostname</h4>
									<p class="text-xs text-[#a8a8a8] mt-1">
										Route traffic through proxy using virtual domain hostname
									</p>
								</button>
							</div>
						</div>
					{/if}

					<!-- Port or Proxy Hostname inputs -->
					{#if useProxyMode && proxyEnabled}
						<div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
							{#if proxyListeners.length > 0}
								<div class="space-y-1.5">
									<label for="proxy_listener" class="block text-xs text-[#c6c6c6] uppercase font-mono tracking-wider font-semibold">
										Proxy Listener
									</label>
									<div class="relative">
										<select
											id="proxy_listener"
											bind:value={formData.proxyListenerId}
											class="w-full h-10 px-4 pr-10 bg-[#161616] border-b border-[#8d8d8d] focus:border-b-2 focus:border-[#0f62fe] focus:bg-[#262626] focus:outline-none text-sm text-[#f4f4f4] rounded-none appearance-none cursor-pointer"
										>
											{#each proxyListeners as listener (listener.id)}
												<option value={listener.id}>
													{listener.name} (Port {listener.port}) {listener.isDefault ? '[Default]' : ''}
												</option>
											{/each}
										</select>
										<div class="pointer-events-none absolute inset-y-0 right-0 flex items-center px-3 text-[#c6c6c6]">
											<svg class="h-4 w-4" fill="currentColor" viewBox="0 0 20 20">
												<path fill-rule="evenodd" d="M5.293 7.293a1 1 0 011.414 0L10 10.586l3.293-3.293a1 1 0 111.414 1.414l-4 4a1 1 0 01-1.414 0l-4-4a1 1 0 010-1.414z" clip-rule="evenodd" />
											</svg>
										</div>
									</div>
								</div>
							{/if}

							<div class="space-y-1.5">
								<label for="proxy_hostname" class="block text-xs text-[#c6c6c6] uppercase font-mono tracking-wider font-semibold">
									Server Hostname
								</label>
								<input
									id="proxy_hostname"
									type="text"
									placeholder={proxyBaseURL ? 'survival' : 'survival.example.com'}
									bind:value={formData.proxyHostname}
									class="w-full h-10 px-4 bg-[#161616] border-b border-[#8d8d8d] focus:border-b-2 focus:border-[#0f62fe] focus:bg-[#262626] focus:outline-none text-sm font-mono text-[#f4f4f4] placeholder-[#6f6f6f] rounded-none"
								/>
							</div>
						</div>
					{:else}
						<div class="space-y-1.5">
							<div class="flex items-center justify-between">
								<label for="port" class="text-xs text-[#c6c6c6] uppercase font-mono tracking-wider font-semibold">
									Server Port
								</label>
								<button
									type="button"
									onclick={refreshAvailablePort}
									class="text-xs font-mono text-[#78a9ff] hover:underline cursor-pointer"
								>
									Auto-Assign Next Port
								</button>
							</div>
							<input
								id="port"
								type="number"
								min="1"
								max="65535"
								bind:value={formData.port}
								oninput={(e) => validatePort(Number(e.currentTarget.value))}
								class="w-full h-10 px-4 bg-[#161616] border-b border-[#8d8d8d] focus:border-b-2 focus:border-[#0f62fe] focus:bg-[#262626] focus:outline-none text-sm font-mono text-[#f4f4f4] rounded-none {portError ? '!border-b-2 !border-[#da1e28]' : ''}"
							/>
							{#if portError}
								<p class="text-xs text-[#ff8389] font-mono mt-1">{portError}</p>
							{/if}
						</div>
					{/if}

					<!-- Quotas: Memory & Max Players -->
					<div class="grid grid-cols-1 sm:grid-cols-2 gap-4 pt-2">
						<div class="space-y-1.5">
							<label for="memory" class="block text-xs text-[#c6c6c6] uppercase font-mono tracking-wider font-semibold">
								Memory Allocation (MB)
							</label>
							<input
								id="memory"
								type="number"
								min="512"
								step="256"
								bind:value={formData.memory}
								class="w-full h-10 px-4 bg-[#161616] border-b border-[#8d8d8d] focus:border-b-2 focus:border-[#0f62fe] focus:bg-[#262626] focus:outline-none text-sm font-mono text-[#f4f4f4] rounded-none"
							/>
							<p class="text-xs text-[#8d8d8d] font-mono">
								Equals {(formData.memory / 1024).toFixed(1)} GB
							</p>
						</div>

						<div class="space-y-1.5">
							<label for="max_players" class="block text-xs text-[#c6c6c6] uppercase font-mono tracking-wider font-semibold">
								Max Players
							</label>
							<input
								id="max_players"
								type="number"
								min="1"
								max="1000"
								bind:value={formData.maxPlayers}
								class="w-full h-10 px-4 bg-[#161616] border-b border-[#8d8d8d] focus:border-b-2 focus:border-[#0f62fe] focus:bg-[#262626] focus:outline-none text-sm font-mono text-[#f4f4f4] rounded-none"
							/>
						</div>
					</div>

					<!-- Additional Ports Editor -->
					<div class="pt-2 border-t border-[#393939]">
						<AdditionalPortsEditor
							bind:ports={formData.additionalPorts}
							disabled={loading}
							{usedPorts}
							onchange={(ports) => (formData.additionalPorts = ports)}
						/>
					</div>
				</div>

			<!-- Step 3: Cluster Placement & Runtime -->
			{:else if currentStep === 3}
				<div class="bg-[#262626] border border-[#393939] p-6 rounded-none space-y-6">
					<div>
						<h2 class="text-lg font-light text-[#f4f4f4]">Step 4: Cluster Placement & Runtime</h2>
						<p class="text-xs text-[#a8a8a8] mt-1 font-sans">
							Designate physical cluster host and configure container startup options.
						</p>
					</div>

					<!-- Cluster Placement Mode (Carbon Tiles) -->
					<div class="space-y-3">
						<label class="block text-xs text-[#c6c6c6] uppercase font-mono tracking-wider font-semibold">
							Node Placement Mode
						</label>
						<div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
							<button
								type="button"
								onclick={() => (nodePlacementMode = 'auto')}
								class="p-4 text-left border transition-colors cursor-pointer rounded-none relative {nodePlacementMode === 'auto' ? 'bg-[#353535] border-[#0f62fe] border-l-4 border-l-[#0f62fe]' : 'bg-[#161616] border-[#393939] hover:bg-[#2a2a2a]'}"
							>
								<div class="flex items-center gap-2">
									<Activity class="h-4 w-4 text-[#0f62fe]" />
									<h4 class="font-semibold text-sm text-[#f4f4f4]">Auto-Placement (Load Balanced)</h4>
								</div>
								<p class="text-xs text-[#a8a8a8] mt-1">
									Cluster scheduler selects optimal worker node based on available memory and active workload.
								</p>
							</button>

							<button
								type="button"
								onclick={() => (nodePlacementMode = 'manual')}
								class="p-4 text-left border transition-colors cursor-pointer rounded-none relative {nodePlacementMode === 'manual' ? 'bg-[#353535] border-[#0f62fe] border-l-4 border-l-[#0f62fe]' : 'bg-[#161616] border-[#393939] hover:bg-[#2a2a2a]'}"
							>
								<div class="flex items-center gap-2">
									<ServerIcon class="h-4 w-4 text-[#78a9ff]" />
									<h4 class="font-semibold text-sm text-[#f4f4f4]">Target Specific Node</h4>
								</div>
								<p class="text-xs text-[#a8a8a8] mt-1">
									Pin this Minecraft container instance to a designated Docker host machine.
								</p>
							</button>
						</div>

						{#if nodePlacementMode === 'auto'}
							<div class="p-4 bg-[#161616] border border-[#393939] rounded-none space-y-2">
								<label for="placement_strategy" class="block text-xs text-[#c6c6c6] font-mono uppercase">
									Placement Strategy
								</label>
								<select
									id="placement_strategy"
									bind:value={placementStrategy}
									class="w-full h-10 px-3 bg-[#262626] border border-[#525252] text-xs font-mono text-[#f4f4f4] rounded-none focus:outline-none focus:border-[#0f62fe]"
								>
									<option value="least_memory">Least Memory Allocated (Recommended)</option>
									<option value="least_servers">Least Active Server Count</option>
									<option value="round_robin">Round Robin Distribution</option>
								</select>
							</div>
						{:else}
							<div class="space-y-2">
								{#if loadingNodes}
									<div class="flex items-center justify-center p-6 text-xs text-[#8d8d8d] bg-[#161616] border border-[#393939]">
										<Loader2 class="h-4 w-4 animate-spin text-[#0f62fe] mr-2" />
										Scanning cluster topology...
									</div>
								{:else if nodes.length === 0}
									<div class="p-4 bg-[#161616] border border-[#393939] text-xs text-[#a8a8a8]">
										No worker nodes detected. Defaulting to local controller daemon.
									</div>
								{:else}
									<div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
										{#each nodes as node (node.id)}
											<button
												type="button"
												onclick={() => { if (node.enabled) selectedNodeId = node.id; }}
												class="p-3.5 text-left border transition-colors cursor-pointer rounded-none relative {selectedNodeId === node.id ? 'bg-[#353535] border-[#0f62fe] border-l-4 border-l-[#0f62fe]' : 'bg-[#161616] border-[#393939] hover:bg-[#2a2a2a]'} {!node.enabled ? 'opacity-40 cursor-not-allowed' : ''}"
											>
												<div class="flex items-center justify-between">
													<div class="flex items-center gap-2">
														<span class="font-medium text-sm text-[#f4f4f4]">{node.name}</span>
														{#if node.isLocal}
															<span class="px-1.5 py-0.2 bg-[#393939] text-[10px] font-mono text-[#c6c6c6]">Local</span>
														{/if}
													</div>
													<CarbonTag type={node.status === NodeStatus.ONLINE ? 'green' : 'red'} size="sm">
														{node.status === NodeStatus.ONLINE ? 'ONLINE' : 'OFFLINE'}
													</CarbonTag>
												</div>
												<p class="text-xs font-mono text-[#8d8d8d] mt-1 truncate">
													{node.host || node.advertisedIp || 'local socket'}
												</p>
											</button>
										{/each}
									</div>
								{/if}
							</div>
						{/if}
					</div>

					<!-- Advanced Docker Image -->
					<div class="space-y-1.5 pt-2 border-t border-[#393939]">
						<label for="docker_image" class="block text-xs text-[#c6c6c6] uppercase font-mono tracking-wider font-semibold">
							Docker Image <span class="text-xs text-[#8d8d8d] lowercase font-normal">(Advanced)</span>
						</label>
						<div class="relative">
							<select
								id="docker_image"
								bind:value={formData.dockerImage}
								class="w-full h-10 px-4 pr-10 bg-[#161616] border-b border-[#8d8d8d] focus:border-b-2 focus:border-[#0f62fe] focus:bg-[#262626] focus:outline-none text-sm font-mono text-[#f4f4f4] rounded-none appearance-none cursor-pointer"
							>
								<option value="">Auto-select base image (Recommended)</option>
								{#each getUniqueDockerImages(dockerImages) as image (image.tag)}
									<option value={image.tag}>
										{getDockerImageDisplayName(image)}
									</option>
								{/each}
							</select>
							<div class="pointer-events-none absolute inset-y-0 right-0 flex items-center px-3 text-[#c6c6c6]">
								<svg class="h-4 w-4" fill="currentColor" viewBox="0 0 20 20">
									<path fill-rule="evenodd" d="M5.293 7.293a1 1 0 011.414 0L10 10.586l3.293-3.293a1 1 0 111.414 1.414l-4 4a1 1 0 01-1.414 0l-4-4a1 1 0 010-1.414z" clip-rule="evenodd" />
								</svg>
							</div>
						</div>
					</div>

					<!-- Lifecycle Toggles -->
					<div class="space-y-3 pt-2 border-t border-[#393939]">
						<h3 class="text-xs text-[#c6c6c6] uppercase font-mono tracking-wider font-semibold">Lifecycle & Autostart</h3>
						<div class="grid grid-cols-1 sm:grid-cols-3 gap-3">
							<!-- Start Immediately -->
							<label class="p-3 bg-[#161616] border border-[#393939] flex items-center gap-3 cursor-pointer rounded-none hover:bg-[#202020] select-none">
								<input
									type="checkbox"
									bind:checked={formData.startImmediately}
									class="h-4 w-4 rounded-none accent-[#0f62fe]"
								/>
								<div>
									<span class="text-xs font-medium text-[#f4f4f4] block">Start Immediately</span>
									<span class="text-[11px] text-[#8d8d8d] block font-sans">Boot container upon creation</span>
								</div>
							</label>

							<!-- Detached Mode -->
							<label class="p-3 bg-[#161616] border border-[#393939] flex items-center gap-3 cursor-pointer rounded-none hover:bg-[#202020] select-none {useProxyMode ? 'opacity-50 cursor-not-allowed' : ''}">
								<input
									type="checkbox"
									bind:checked={formData.detached}
									disabled={useProxyMode}
									class="h-4 w-4 rounded-none accent-[#0f62fe]"
								/>
								<div>
									<span class="text-xs font-medium text-[#f4f4f4] block">Detached Mode</span>
									<span class="text-[11px] text-[#8d8d8d] block font-sans">Persists past daemon stop</span>
								</div>
							</label>

							<!-- Auto Start -->
							<label class="p-3 bg-[#161616] border border-[#393939] flex items-center gap-3 cursor-pointer rounded-none hover:bg-[#202020] select-none {formData.detached ? 'opacity-50 cursor-not-allowed' : ''}">
								<input
									type="checkbox"
									bind:checked={formData.autoStart}
									disabled={formData.detached}
									class="h-4 w-4 rounded-none accent-[#0f62fe]"
								/>
								<div>
									<span class="text-xs font-medium text-[#f4f4f4] block">Auto Start</span>
									<span class="text-[11px] text-[#8d8d8d] block font-sans">Start when host daemon starts</span>
								</div>
							</label>
						</div>
					</div>

					<!-- Docker Overrides -->
					<div class="pt-2 border-t border-[#393939]">
						<DockerOverridesEditor
							bind:overrides={formData.dockerOverrides}
							disabled={loading}
							onchange={(overrides) => (formData.dockerOverrides = overrides)}
						/>
					</div>
				</div>
			{/if}

			<!-- Carbon Wizard Navigation Bar -->
			<div class="flex items-center justify-between border-t border-[#393939] pt-4">
				<a
					href="/servers"
					class="h-10 px-4 bg-[#262626] hover:bg-[#353535] text-[#c6c6c6] hover:text-white text-sm font-sans flex items-center gap-2 rounded-none transition-colors border border-[#393939]"
				>
					Cancel
				</a>

				<div class="flex items-center gap-2">
					{#if currentStep > 0}
						<button
							type="button"
							onclick={prevStep}
							class="h-10 px-4 bg-[#393939] hover:bg-[#4c4c4c] text-white text-sm font-sans flex items-center gap-2 rounded-none transition-colors cursor-pointer"
						>
							<ChevronLeft class="h-4 w-4" />
							<span>Previous</span>
						</button>
					{/if}

					{#if currentStep < steps.length - 1}
						<button
							type="button"
							onclick={nextStep}
							class="h-10 px-5 bg-[#0f62fe] hover:bg-[#0353e9] active:bg-[#002d9c] text-white text-sm font-sans font-medium flex items-center gap-2 rounded-none transition-colors cursor-pointer"
						>
							<span>Next Step</span>
							<ChevronRight class="h-4 w-4" />
						</button>
					{:else}
						<button
							type="submit"
							disabled={loading || loadingVersions}
							class="h-10 px-6 bg-[#0f62fe] hover:bg-[#0353e9] active:bg-[#002d9c] text-white text-sm font-sans font-medium flex items-center gap-2 rounded-none transition-colors cursor-pointer disabled:opacity-50"
						>
							{#if loading}
								<Loader2 class="h-4 w-4 animate-spin" />
								<span>Creating Server...</span>
							{:else}
								<Check class="h-4 w-4" />
								<span>Create Server</span>
							{/if}
						</button>
					{/if}
				</div>
			</div>
		</form>
	</div>
</div>

<!-- Modpack Selection Modal (Sharp Carbon Modal) -->
<Dialog bind:open={showModpackDialog}>
	<DialogContent class="max-w-2xl bg-[#161616] border border-[#393939] text-[#f4f4f4] rounded-none p-6">
		<DialogHeader class="border-b border-[#393939] pb-3">
			<DialogTitle class="text-lg font-light text-[#f4f4f4]">Select Favorite Modpack</DialogTitle>
			<DialogDescription class="text-xs text-[#a8a8a8]">
				Choose a pre-configured modpack from your saved favorites list.
			</DialogDescription>
		</DialogHeader>

		<div class="max-h-[60vh] overflow-y-auto space-y-3 py-4">
			{#if favoriteModpacks.length === 0}
				<div class="p-8 text-center bg-[#262626] border border-[#393939] rounded-none">
					<Package class="h-8 w-8 text-[#8d8d8d] mx-auto mb-2" />
					<p class="text-sm font-semibold text-[#f4f4f4]">No favorite modpacks yet</p>
					<p class="text-xs text-[#a8a8a8] mt-1">
						Visit the Modpacks directory to search and favorite packs to use as presets.
					</p>
				</div>
			{:else}
				{#each favoriteModpacks as modpack (modpack.id)}
					<button
						type="button"
						onclick={() => selectModpack(modpack)}
						class="w-full p-3.5 bg-[#262626] hover:bg-[#353535] border border-[#393939] text-left transition-colors flex items-start gap-3 rounded-none cursor-pointer"
					>
						{#if modpack.logoUrl}
							<img
								src={modpack.logoUrl}
								alt={modpack.name}
								class="h-12 w-12 object-cover border border-[#393939] rounded-none shrink-0"
							/>
						{/if}
						<div class="min-w-0 flex-1">
							<div class="flex items-center gap-2">
								<h4 class="font-semibold text-sm text-[#f4f4f4]">{modpack.name}</h4>
								<CarbonTag type="blue" size="sm">{modpack.indexer}</CarbonTag>
							</div>
							<p class="text-xs text-[#a8a8a8] line-clamp-2 mt-1">
								{modpack.summary}
							</p>
							<div class="mt-2 flex items-center gap-2 text-[11px] font-mono text-[#78a9ff]">
								{#if parseJsonArray(modpack.gameVersions).length > 0}
									<span>MC {parseJsonArray(modpack.gameVersions)[0]}</span>
								{/if}
								{#if parseJsonArray(modpack.modLoaders).length > 0}
									<span>• {parseJsonArray(modpack.modLoaders)[0]}</span>
								{/if}
							</div>
						</div>
					</button>
				{/each}
			{/if}
		</div>
	</DialogContent>
</Dialog>
