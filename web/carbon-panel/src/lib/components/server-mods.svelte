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
			<div
				class="flex flex-col justify-between gap-3 border-b border-[#393939] bg-[#262626] p-4 sm:flex-row sm:items-center"
			>
				<div>
					<div class="flex items-center gap-2">
						<h3 class="text-base font-semibold text-[#f4f4f4]">Mod & Plugin Management</h3>
						<CarbonTag type="teal" size="sm">{getModsDirectory()}/</CarbonTag>
					</div>
					<p class="mt-0.5 text-xs text-[#a8a8a8]">
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
				<div
					class="flex flex-col items-stretch justify-between gap-3 border-b border-[#393939] bg-[#161616] p-3 sm:flex-row sm:items-center"
				>
					<!-- Search -->
					<div class="relative max-w-md flex-1">
						<Search class="absolute top-1/2 left-3 h-3.5 w-3.5 -translate-y-1/2 text-[#8d8d8d]" />
						<input
							type="search"
							placeholder="Search installed mods..."
							bind:value={searchQuery}
							class="h-8 w-full rounded-none border-b border-[#8d8d8d] bg-[#262626] pr-3 pl-8 text-xs text-[#f4f4f4] placeholder-[#6f6f6f] focus:border-b-2 focus:border-[#0f62fe] focus:outline-none"
						/>
					</div>

					<!-- Filter Tags -->
					<div class="flex items-center gap-1">
						<button
							type="button"
							onclick={() => (modFilter = 'all')}
							class="cursor-pointer rounded-none border px-2.5 py-1 font-mono text-xs uppercase transition-colors {modFilter ===
							'all'
								? 'border-[#0f62fe] bg-[#0f62fe] text-white'
								: 'border-[#393939] bg-[#262626] text-[#c6c6c6] hover:bg-[#353535]'}"
						>
							All ({mods.length})
						</button>
						<button
							type="button"
							onclick={() => (modFilter = 'enabled')}
							class="cursor-pointer rounded-none border px-2.5 py-1 font-mono text-xs uppercase transition-colors {modFilter ===
							'enabled'
								? 'border-[#198038] bg-[#198038] text-white'
								: 'border-[#198038]/50 bg-[#262626] text-[#6fdc8c] hover:bg-[#353535]'}"
						>
							Enabled ({mods.filter((m) => m.enabled).length})
						</button>
						<button
							type="button"
							onclick={() => (modFilter = 'disabled')}
							class="cursor-pointer rounded-none border px-2.5 py-1 font-mono text-xs uppercase transition-colors {modFilter ===
							'disabled'
								? 'border-[#525252] bg-[#525252] text-white'
								: 'border-[#525252]/60 bg-[#262626] text-[#c6c6c6] hover:bg-[#353535]'}"
						>
							Disabled ({mods.filter((m) => !m.enabled).length})
						</button>
					</div>
				</div>
			{/if}

			<!-- Upload Progress Bar (Sharp Carbon) -->
			{#if uploading && uploadProgress}
				<div class="border-b border-[#393939] bg-[#262626] p-3 text-xs">
					<div class="mb-1 flex items-center justify-between">
						<span class="truncate font-mono text-[#f4f4f4]">Uploading: {currentUploadFilename}</span
						>
						<div class="flex items-center gap-2">
							<span class="font-mono text-[#78a9ff]"
								>{uploadProgress.percentComplete.toFixed(0)}%</span
							>
							<button
								type="button"
								onclick={cancelCurrentUpload}
								class="p-0.5 text-[#ff8389] hover:text-white"
								title="Cancel upload"
								aria-label="Cancel upload"
							>
								<X class="h-3.5 w-3.5" />
							</button>
						</div>
					</div>
					<div class="h-1.5 w-full rounded-none border border-[#393939] bg-[#161616]">
						<div
							class="h-full rounded-none bg-[#0f62fe] transition-all"
							style="width: {uploadProgress.percentComplete}%"
						></div>
					</div>
					<p class="mt-1 font-mono text-[10px] text-[#8d8d8d]">
						{formatBytes(uploadProgress.bytesUploaded)} / {formatBytes(uploadProgress.totalBytes)}
					</p>
				</div>
			{/if}

			<!-- Content Area / Carbon DataTable Layout -->
			<div class="flex-1 overflow-auto bg-[#161616]">
				{#if !canHaveMods()}
					<div class="flex flex-col items-center justify-center p-12 text-center text-[#8d8d8d]">
						<Package class="mb-3 h-10 w-10 text-[#525252]" />
						<p class="text-sm font-semibold text-[#f4f4f4]">Mod loader incompatible</p>
						<p class="mt-1 max-w-sm text-xs text-[#8d8d8d]">
							To install mods or plugins, configure this server to use Fabric, Forge, NeoForge,
							Paper, or Purpur.
						</p>
					</div>
				{:else if loading}
					<div class="flex items-center justify-center p-12">
						<CarbonInlineLoading description="Reading {getModsDirectory()}/ directory..." />
					</div>
				{:else if mods.length === 0}
					<div class="flex flex-col items-center justify-center p-12 text-center text-[#8d8d8d]">
						<Package class="mb-3 h-10 w-10 text-[#525252]" />
						<p class="text-sm font-semibold text-[#f4f4f4]">No mods installed</p>
						<p class="mt-1 text-xs text-[#8d8d8d]">
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
							<div
								class="group flex items-center justify-between gap-4 rounded-none bg-[#262626] p-3.5 transition-colors hover:bg-[#353535]"
							>
								<div class="flex min-w-0 items-center gap-3.5">
									<!-- Enable/Disable Carbon Toggle -->
									<button
										type="button"
										onclick={() => toggleMod(mod)}
										class="flex h-7 cursor-pointer items-center gap-1 rounded-none border px-2 font-mono text-xs transition-colors select-none {mod.enabled
											? 'border-[#198038] bg-[#198038]/20 text-[#6fdc8c]'
											: 'border-[#525252] bg-[#161616] text-[#8d8d8d]'}"
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
											<h4
												class="truncate text-sm font-medium text-[#f4f4f4] {mod.enabled
													? ''
													: 'line-through opacity-70'}"
											>
												{mod.displayName}
											</h4>
											{#if mod.version}
												<span
													class="border border-[#393939] bg-[#161616] px-1.5 py-0 font-mono text-[10px] text-[#c6c6c6]"
												>
													{mod.version}
												</span>
											{/if}
										</div>
										<div
											class="mt-0.5 flex flex-wrap items-center gap-3 font-mono text-xs text-[#8d8d8d]"
										>
											<span class="flex items-center gap-1 text-[#c6c6c6]">
												<FileText class="h-3 w-3" />
												{mod.fileName}
											</span>
											<span>{formatBytes(Number(mod.fileSize))}</span>
											{#if mod.uploadedAt}
												<span
													>{new Date(
														Number(mod.uploadedAt.seconds) * 1000
													).toLocaleDateString()}</span
												>
											{/if}
										</div>
										{#if mod.description}
											<p class="mt-1 line-clamp-1 font-sans text-xs text-[#a8a8a8]">
												{mod.description}
											</p>
										{/if}
									</div>
								</div>

								<!-- Actions -->
								<div class="flex shrink-0 items-center border border-[#393939] bg-[#161616]">
									<button
										type="button"
										onclick={() => downloadMod(mod)}
										class="flex h-8 w-8 cursor-pointer items-center justify-center rounded-none border-r border-[#393939] text-[#c6c6c6] transition-colors hover:bg-[#353535] hover:text-white"
										title="Download .jar"
									>
										<Download class="h-3.5 w-3.5" />
									</button>
									<button
										type="button"
										onclick={() => deleteMod(mod)}
										class="flex h-8 w-8 cursor-pointer items-center justify-center rounded-none text-[#ff8389] transition-colors hover:bg-[#da1e28]/20"
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
