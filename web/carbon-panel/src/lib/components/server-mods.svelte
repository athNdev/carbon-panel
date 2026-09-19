<script lang="ts">
	import { ResizablePaneGroup, ResizablePane } from '$lib/components/ui/resizable';
	import {
		Loader2,
		Upload,
		Download,
		Trash2,
		ToggleLeft,
		ToggleRight,
		Package,
		FileText,
		X,
		Boxes,
		Search,
		Check,
		Ban
	} from '@lucide/svelte';
	import { rpcClient } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import { ModLoader, type Server } from '$lib/proto/carbonpanel/v1/common_pb';
	import type { Mod } from '$lib/proto/carbonpanel/v1/mod_pb';
	import { formatBytes } from '$lib/utils';
	import { uploadFile, cancelUpload, type UploadProgress } from '$lib/utils/chunked-upload';
	import ModBrowserDialog from '$lib/components/mod-browser-dialog.svelte';
	import { CarbonTag, CarbonInlineLoading, CarbonButton } from '$lib/components/carbon';

	interface Props {
		server: Server;
		active?: boolean;
	}

	let { server, active = false }: Props = $props();

	let mods = $state<Mod[]>([]);
	let loading = $state(true);
	let uploading = $state(false);
	let uploadProgress = $state<UploadProgress | null>(null);
	let currentUploadFilename = $state('');
	let uploadAbortController = $state<AbortController | null>(null);
	let fileInput = $state<HTMLInputElement | null>(null);
	let browserDialogOpen = $state(false);

	let searchQuery = $state('');
	let modFilter = $state<'all' | 'enabled' | 'disabled'>('all');

	let hasLoaded = false;
	let previousServerId = $state(server.id);

	$effect(() => {
		if (server.id !== previousServerId) {
			previousServerId = server.id;
			mods = [];
			loading = true;
			uploading = false;
			hasLoaded = false;
		}
	});

	$effect(() => {
		if (active && !hasLoaded) {
			hasLoaded = true;
			loadMods();
		}
	});

	let filteredMods = $derived.by(() => {
		let result = [...mods];
		if (modFilter === 'enabled') {
			result = result.filter((m) => m.enabled);
		} else if (modFilter === 'disabled') {
			result = result.filter((m) => !m.enabled);
		}

		if (searchQuery.trim()) {
			const q = searchQuery.toLowerCase().trim();
			result = result.filter(
				(m) =>
					m.displayName.toLowerCase().includes(q) ||
					m.fileName.toLowerCase().includes(q) ||
					(m.description && m.description.toLowerCase().includes(q))
			);
		}
		return result;
	});

	async function loadMods() {
		try {
			loading = true;
			const response = await rpcClient.mod.listMods({ serverId: server.id });
			mods = response.mods;
		} catch (_e) {
			if (server.modLoader !== ModLoader.VANILLA) {
				toast.error('Failed to load mods');
			}
		} finally {
			loading = false;
		}
	}

	async function handleFileSelect(event: Event) {
		const input = event.target as HTMLInputElement;
		const fileList = input.files;
		if (!fileList || fileList.length === 0) return;

		uploading = true;
		uploadAbortController = new AbortController();

		try {
			for (const file of Array.from(fileList)) {
				currentUploadFilename = file.name;
				uploadProgress = null;

				const result = await uploadFile(file, {
					onProgress: (progress) => {
						uploadProgress = progress;
					},
					signal: uploadAbortController.signal
				});

				await rpcClient.mod.importUploadedMod({
					serverId: server.id,
					uploadSessionId: result.sessionId,
					displayName: file.name,
					description: ''
				});
			}
			toast.success(`Uploaded ${fileList.length} mod(s)`);
			await loadMods();
		} catch (error: unknown) {
			if (error instanceof Error && error.message === 'Upload cancelled') {
				toast.info('Upload cancelled');
			} else {
				toast.error('Failed to upload mod');
			}
		} finally {
			uploading = false;
			uploadProgress = null;
			currentUploadFilename = '';
			uploadAbortController = null;
			input.value = '';
		}
	}

	function cancelCurrentUpload() {
		if (uploadAbortController) {
			uploadAbortController.abort();
		}
		if (uploadProgress?.sessionId) {
			cancelUpload(uploadProgress.sessionId).catch(() => {});
		}
	}

	async function toggleMod(mod: Mod) {
		try {
			await rpcClient.mod.updateMod({
				serverId: server.id,
				modId: mod.id,
				enabled: !mod.enabled,
				displayName: mod.displayName,
				description: mod.description
			});
			toast.success(`Mod ${!mod.enabled ? 'enabled' : 'disabled'}`);
			await loadMods();
		} catch (_e) {
			toast.error('Failed to toggle mod');
		}
	}

	async function deleteMod(mod: Mod) {
		const confirmed = confirm(`Are you sure you want to delete "${mod.displayName}"?`);
		if (!confirmed) return;

		try {
			await rpcClient.mod.deleteMod({
				serverId: server.id,
				modId: mod.id
			});
			toast.success('Mod deleted');
			await loadMods();
		} catch (_e) {
			toast.error('Failed to delete mod');
		}
	}

	async function downloadMod(mod: Mod) {
		try {
			const response = await rpcClient.file.getFile({
				serverId: server.id,
				path: `${getModsDirectory()}/${mod.fileName}`
			});
			const blob = new Blob([new Uint8Array(response.content)]);
			const url = URL.createObjectURL(blob);
			const a = document.createElement('a');
			a.href = url;
			a.download = mod.fileName;
			a.click();
			URL.revokeObjectURL(url);
		} catch (_e) {
			toast.error('Failed to download mod');
		}
	}

	function getModsDirectory(): string {
		const modLoaderInfo: Record<ModLoader, string> = {
			[ModLoader.UNSPECIFIED]: 'mods',
			[ModLoader.VANILLA]: 'mods',
			[ModLoader.FORGE]: 'mods',
			[ModLoader.NEOFORGE]: 'mods',
			[ModLoader.FABRIC]: 'mods',
			[ModLoader.QUILT]: 'mods',
			[ModLoader.BUKKIT]: 'plugins',
			[ModLoader.SPIGOT]: 'plugins',
			[ModLoader.PAPER]: 'plugins',
			[ModLoader.PURPUR]: 'plugins',
			[ModLoader.SPONGE_VANILLA]: 'mods',
			[ModLoader.SPONGE_FORGE]: 'mods',
			[ModLoader.MOHIST]: 'mods',
			[ModLoader.CATSERVER]: 'mods',
			[ModLoader.ARCLIGHT]: 'mods',
			[ModLoader.AUTO_CURSEFORGE]: 'mods',
			[ModLoader.MODRINTH]: 'mods',
			[ModLoader.FOLIA]: 'plugins'
		};

		return modLoaderInfo[server.modLoader] || 'mods';
	}

	function canHaveMods(): boolean {
		const noModLoaders = [ModLoader.VANILLA, ModLoader.UNSPECIFIED];
		return !noModLoaders.includes(server.modLoader);
	}
</script>

<!-- Carbon Container (Requirement 6) -->
<ResizablePaneGroup
	direction="vertical"
	class="h-full max-h-[800px] min-h-[450px] rounded-none border border-[#393939] bg-[#161616] font-sans text-[#f4f4f4]"
>
	<ResizablePane defaultSize={100}>
		<div class="flex h-full flex-col bg-[#161616]">
			<!-- Header -->
			<div class="p-4 bg-[#262626] border-b border-[#393939] flex flex-col sm:flex-row sm:items-center justify-between gap-3">
				<div>
					<div class="flex items-center gap-2">
						<h3 class="text-base font-semibold text-[#f4f4f4]">Mod & Plugin Management</h3>
						<CarbonTag type="teal" size="sm">{getModsDirectory()}/</CarbonTag>
					</div>
					<p class="text-xs text-[#a8a8a8] mt-0.5">
						{#if canHaveMods()}
							Manage extensions in the container {getModsDirectory()} directory
						{:else}
							Vanilla Minecraft does not support external mod injection
						{/if}
					</p>
				</div>

				{#if canHaveMods()}
					<div class="flex items-center gap-2">
						<CarbonButton
							kind="primary"
							size="sm"
							class="justify-center gap-1.5"
							onclick={() => (browserDialogOpen = true)}
						>
							<Boxes class="h-4 w-4" />
							<span>Browse Mods</span>
						</CarbonButton>
						<CarbonButton
							kind="secondary"
							size="sm"
							class="justify-center gap-1.5"
							onclick={() => fileInput?.click()}
							disabled={uploading}
						>
							{#if uploading}
								<Loader2 class="h-4 w-4 animate-spin" />
							{:else}
								<Upload class="h-4 w-4" />
							{/if}
							<span>Upload .jar</span>
						</CarbonButton>
						<input
							bind:this={fileInput}
							type="file"
							multiple
							accept=".jar,.zip"
							onchange={handleFileSelect}
							class="hidden"
						/>
					</div>
				{/if}
			</div>

			<!-- Carbon Action Bar: Search & Filter Tags -->
			{#if canHaveMods()}
				<div class="p-3 bg-[#161616] border-b border-[#393939] flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-3">
					<!-- Search -->
					<div class="relative flex-1 max-w-md">
						<Search class="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-[#8d8d8d]" />
						<input
							type="search"
							placeholder="Search installed mods..."
							bind:value={searchQuery}
							class="w-full h-8 pl-8 pr-3 bg-[#262626] border-b border-[#8d8d8d] focus:border-b-2 focus:border-[#0f62fe] focus:outline-none text-xs text-[#f4f4f4] placeholder-[#6f6f6f] rounded-none"
						/>
					</div>

					<!-- Filter Tags -->
					<div class="flex items-center gap-1">
						<button
							type="button"
							onclick={() => (modFilter = 'all')}
							class="px-2.5 py-1 text-xs font-mono uppercase transition-colors cursor-pointer rounded-none border {modFilter === 'all' ? 'bg-[#0f62fe] text-white border-[#0f62fe]' : 'bg-[#262626] text-[#c6c6c6] border-[#393939] hover:bg-[#353535]'}"
						>
							All ({mods.length})
						</button>
						<button
							type="button"
							onclick={() => (modFilter = 'enabled')}
							class="px-2.5 py-1 text-xs font-mono uppercase transition-colors cursor-pointer rounded-none border {modFilter === 'enabled' ? 'bg-[#198038] text-white border-[#198038]' : 'bg-[#262626] text-[#6fdc8c] border-[#198038]/50 hover:bg-[#353535]'}"
						>
							Enabled ({mods.filter((m) => m.enabled).length})
						</button>
						<button
							type="button"
							onclick={() => (modFilter = 'disabled')}
							class="px-2.5 py-1 text-xs font-mono uppercase transition-colors cursor-pointer rounded-none border {modFilter === 'disabled' ? 'bg-[#525252] text-white border-[#525252]' : 'bg-[#262626] text-[#c6c6c6] border-[#525252]/60 hover:bg-[#353535]'}"
						>
							Disabled ({mods.filter((m) => !m.enabled).length})
						</button>
					</div>
				</div>
			{/if}

			<!-- Upload Progress Bar (Sharp Carbon) -->
			{#if uploading && uploadProgress}
				<div class="p-3 bg-[#262626] border-b border-[#393939] text-xs">
					<div class="flex items-center justify-between mb-1">
						<span class="text-[#f4f4f4] font-mono truncate">Uploading: {currentUploadFilename}</span>
						<div class="flex items-center gap-2">
							<span class="font-mono text-[#78a9ff]">{uploadProgress.percentComplete.toFixed(0)}%</span>
							<button
								type="button"
								onclick={cancelCurrentUpload}
								class="text-[#ff8389] hover:text-white p-0.5"
								title="Cancel upload"
								aria-label="Cancel upload"
							>
								<X class="h-3.5 w-3.5" />
							</button>
						</div>
					</div>
					<div class="w-full h-1.5 bg-[#161616] border border-[#393939] rounded-none">
						<div
							class="h-full bg-[#0f62fe] rounded-none transition-all"
							style="width: {uploadProgress.percentComplete}%"
						></div>
					</div>
					<p class="text-[10px] font-mono text-[#8d8d8d] mt-1">
						{formatBytes(uploadProgress.bytesUploaded)} / {formatBytes(uploadProgress.totalBytes)}
					</p>
				</div>
			{/if}

			<!-- Content Area / Carbon DataTable Layout -->
			<div class="flex-1 overflow-auto bg-[#161616]">
				{#if !canHaveMods()}
					<div class="flex flex-col items-center justify-center p-12 text-center text-[#8d8d8d]">
						<Package class="h-10 w-10 text-[#525252] mb-3" />
						<p class="text-sm font-semibold text-[#f4f4f4]">Mod loader incompatible</p>
						<p class="text-xs text-[#8d8d8d] mt-1 max-w-sm">
							To install mods or plugins, configure this server to use Fabric, Forge, NeoForge, Paper, or Purpur.
						</p>
					</div>
				{:else if loading}
					<div class="flex items-center justify-center p-12">
						<CarbonInlineLoading description="Reading {getModsDirectory()}/ directory..." />
					</div>
				{:else if mods.length === 0}
					<div class="flex flex-col items-center justify-center p-12 text-center text-[#8d8d8d]">
						<Package class="h-10 w-10 text-[#525252] mb-3" />
						<p class="text-sm font-semibold text-[#f4f4f4]">No mods installed</p>
						<p class="text-xs text-[#8d8d8d] mt-1">
							Browse the repository or upload .jar files directly to get started.
						</p>
					</div>
				{:else if filteredMods.length === 0}
					<div class="p-8 text-center text-xs text-[#8d8d8d]">
						No mods match your search query or filter.
					</div>
				{:else}
					<!-- Carbon DataTable rows -->
					<div class="divide-y divide-[#393939] border-b border-[#393939]">
						{#each filteredMods as mod (mod.id)}
							<div class="p-3.5 bg-[#262626] hover:bg-[#353535] transition-colors flex items-center justify-between gap-4 rounded-none group">
								<div class="flex items-center gap-3.5 min-w-0">
									<!-- Enable/Disable Carbon Toggle -->
									<button
										type="button"
										onclick={() => toggleMod(mod)}
										class="h-7 px-2 text-xs font-mono flex items-center gap-1 rounded-none border transition-colors cursor-pointer select-none {mod.enabled ? 'bg-[#198038]/20 border-[#198038] text-[#6fdc8c]' : 'bg-[#161616] border-[#525252] text-[#8d8d8d]'}"
										title={mod.enabled ? 'Click to disable' : 'Click to enable'}
									>
										{#if mod.enabled}
											<Check class="h-3 w-3" />
											<span class="text-[10px] uppercase">Active</span>
										{:else}
											<Ban class="h-3 w-3" />
											<span class="text-[10px] uppercase">Disabled</span>
										{/if}
									</button>

									<!-- Info -->
									<div class="min-w-0">
										<div class="flex items-center gap-2">
											<h4 class="font-medium text-sm text-[#f4f4f4] truncate {mod.enabled ? '' : 'line-through opacity-70'}">
												{mod.displayName}
											</h4>
											{#if mod.version}
												<span class="px-1.5 py-0 bg-[#161616] border border-[#393939] text-[10px] font-mono text-[#c6c6c6]">
													{mod.version}
												</span>
											{/if}
										</div>
										<div class="flex flex-wrap items-center gap-3 text-xs text-[#8d8d8d] font-mono mt-0.5">
											<span class="flex items-center gap-1 text-[#c6c6c6]">
												<FileText class="h-3 w-3" />
												{mod.fileName}
											</span>
											<span>{formatBytes(Number(mod.fileSize))}</span>
											{#if mod.uploadedAt}
												<span>{new Date(Number(mod.uploadedAt.seconds) * 1000).toLocaleDateString()}</span>
											{/if}
										</div>
										{#if mod.description}
											<p class="text-xs text-[#a8a8a8] line-clamp-1 mt-1 font-sans">{mod.description}</p>
										{/if}
									</div>
								</div>

								<!-- Actions -->
								<div class="flex items-center border border-[#393939] bg-[#161616] shrink-0">
									<button
										type="button"
										onclick={() => downloadMod(mod)}
										class="h-8 w-8 flex items-center justify-center border-r border-[#393939] text-[#c6c6c6] hover:text-white hover:bg-[#353535] transition-colors rounded-none cursor-pointer"
										title="Download .jar"
									>
										<Download class="h-3.5 w-3.5" />
									</button>
									<button
										type="button"
										onclick={() => deleteMod(mod)}
										class="h-8 w-8 flex items-center justify-center text-[#ff8389] hover:bg-[#da1e28]/20 transition-colors rounded-none cursor-pointer"
										title="Delete mod"
									>
										<Trash2 class="h-3.5 w-3.5" />
									</button>
								</div>
							</div>
						{/each}
					</div>
				{/if}
			</div>
		</div>
	</ResizablePane>
</ResizablePaneGroup>

<ModBrowserDialog bind:open={browserDialogOpen} {server} onInstalled={loadMods} />
