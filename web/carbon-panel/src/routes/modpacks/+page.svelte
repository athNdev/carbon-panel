<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import {
		CarbonButton,
		CarbonTile,
		CarbonTag,
		CarbonSearch,
		CarbonSelect,
		CarbonModal,
		CarbonTextInput
	} from '$lib/components/carbon';
	import { toast } from 'svelte-sonner';
	import {
		Heart,
		Download,
		RefreshCw,
		ExternalLink,
		Settings,
		Upload,
		Package,
		ArrowLeft,
		Trash2,
		X,
		Globe,
		Loader2,
		Boxes,
		KeyRound,
		FileSearch
	} from '@lucide/svelte';
	import ManifestInspectorDialog from '$lib/components/manifest-inspector-dialog.svelte';
	import { create } from '@bufbuild/protobuf';
	import type {
		IndexedModpack,
		SearchModpacksRequest,
		SearchModpacksResponse,
		GetIndexerStatusResponse
	} from '$lib/proto/carbonpanel/v1/modpack_pb';
	import { SearchModpacksRequestSchema } from '$lib/proto/carbonpanel/v1/modpack_pb';
	import { NodeStatus } from '$lib/proto/carbonpanel/v1/node_pb';
	import { rpcClient } from '$lib/api/rpc-client';
	import { debounce } from 'lodash-es';
	import { uploadFile, cancelUpload, type UploadProgress } from '$lib/utils/chunked-upload';
	import { formatBytes } from '$lib/utils';

	let searchParams = $state<SearchModpacksRequest>(
		create(SearchModpacksRequestSchema, {
			query: '',
			gameVersion: '',
			modLoader: '',
			indexer: '',
			page: 1,
			pageSize: 20
		})
	);

	let searchResults = $state<SearchModpacksResponse | null>(null);
	let favorites = $state<IndexedModpack[]>([]);
	let uploadedPacks = $state<IndexedModpack[]>([]);
	let loading = $state(false);
	let syncing = $state(false);
	let showFavorites = $state(false);
	let showUploaded = $state(false);
	let showManifestInspector = $state(false);
	let indexerStatus = $state<GetIndexerStatusResponse | null>(null);
	let fileInput = $state<HTMLInputElement | null>(null);
	let uploading = $state(false);
	let uploadProgress = $state<UploadProgress | null>(null);
	let uploadAbortController = $state<AbortController | null>(null);
	let selectedIndexer = $state('modrinth'); // Default Modrinth
	let indexerName = $derived(selectedIndexer === 'fuego' ? 'CurseForge' : 'Modrinth');

	// Remote modpack import state
	let showRemoteModal = $state(false);
	let importingRemote = $state(false);
	let remoteUrl = $state('');
	let remoteName = $state('');
	let remoteDescription = $state('');
	let remoteMcVersion = $state('');
	let remoteModLoader = $state('');
	let remoteAuthToken = $state('');

	// Dynamic game versions and mod loaders from API
	let gameVersions = $state<string[]>([]);
	let modLoaders = $state<Array<{ value: string; label: string }>>([
		{ value: '', label: 'All Loaders' }
	]);

	onMount(async () => {
		await Promise.all([
			checkIndexerStatus(),
			loadFavorites(),
			loadUploadedPacks(),
			loadMinecraftVersions(),
			loadModLoaders(),
			searchModpacks()
		]);

		if (searchResults && searchResults.total === 0 && !searchParams.query) {
			syncModpacks();
		}
	});

	async function loadMinecraftVersions() {
		try {
			const response = await rpcClient.minecraft.getMinecraftVersions({});
			gameVersions = response.versions.map((v) => v.id);
		} catch (error) {
			console.error('Failed to load Minecraft versions:', error);
		}
	}

	async function loadModLoaders() {
		try {
			const response = await rpcClient.minecraft.getModLoaders({});
			const loaders = response.modloaders || [];
			modLoaders = [
				{ value: '', label: 'All Loaders' },
				...loaders.map((loader) => ({
					value: loader.name,
					label: loader.displayName || loader.name
				}))
			];
		} catch (error) {
			console.error('Failed to load mod loaders:', error);
		}
	}

	async function checkIndexerStatus() {
		try {
			const response = await rpcClient.modpack.getIndexerStatus({});
			indexerStatus = response;
		} catch (error) {
			console.error('Failed to check indexer status:', error);
		}
	}

	async function searchModpacks(resetPage = true) {
		loading = true;
		try {
			if (resetPage) {
				searchParams.page = 1;
			}
			const response = await rpcClient.modpack.searchModpacks({
				...searchParams,
				indexer: selectedIndexer
			});
			searchResults = response;
		} catch (error) {
			toast.error('Failed to search modpacks');
			console.error(error);
		} finally {
			loading = false;
		}
	}

	async function _syncModpacks() {
		syncing = true;
		try {
			const result = await rpcClient.modpack.syncModpacks({
				query: searchParams.query || '',
				gameVersion: searchParams.gameVersion || '',
				modLoader: searchParams.modLoader || '',
				indexer: selectedIndexer
			});
			toast.success(`Synced ${result.syncedCount} modpacks from ${indexerName}`);
			await searchModpacks();
		} catch (error) {
			toast.error(error instanceof Error ? error.message : 'Failed to sync modpacks');
			console.error(error);
		} finally {
			syncing = false;
		}
	}

	const syncModpacks = debounce(_syncModpacks, 1000, { leading: true, trailing: false });

	async function toggleFavorite(modpack: IndexedModpack) {
		try {
			const result = await rpcClient.modpack.toggleFavorite({ id: modpack.id });

			if (searchResults) {
				searchResults.modpacks = searchResults.modpacks.map((m) =>
					m.id === modpack.id ? { ...m, isFavorited: result.isFavorited } : m
				);
			}

			uploadedPacks = uploadedPacks.map((m) =>
				m.id === modpack.id ? { ...m, isFavorited: result.isFavorited } : m
			);

			if (result.isFavorited) {
				if (!favorites.find((f) => f.id === modpack.id)) {
					favorites = [...favorites, { ...modpack, isFavorited: true }];
				}
				toast.success('Added to favorites');
			} else {
				favorites = favorites.filter((f) => f.id !== modpack.id);
				toast.success('Removed from favorites');
			}
		} catch (error) {
			toast.error('Failed to toggle favorite');
			console.error(error);
		}
	}

	async function loadFavorites() {
		try {
			const result = await rpcClient.modpack.listFavorites({});
			favorites = result.modpacks;
		} catch (error) {
			toast.error('Failed to load favorites');
			console.error(error);
		}
	}

	async function loadUploadedPacks() {
		try {
			const result = await rpcClient.modpack.searchModpacks({
				query: '',
				gameVersion: '',
				modLoader: '',
				indexer: 'manual',
				page: 1,
				pageSize: 100
			});
			uploadedPacks = result.modpacks || [];
		} catch (error) {
			console.error('Failed to load uploaded packs:', error);
		}
	}

	function formatNumber(num: number): string {
		if (num >= 1000000) {
			return `${(num / 1000000).toFixed(1)}M`;
		} else if (num >= 1000) {
			return `${(num / 1000).toFixed(1)}K`;
		}
		return num.toString();
	}

	function parseJsonArray(jsonStr: string): string[] {
		try {
			return JSON.parse(jsonStr);
		} catch {
			return [];
		}
	}

	async function handleModpackUpload(event: Event) {
		const input = event.target as HTMLInputElement;
		const files = input.files;
		if (!files || files.length === 0) return;

		const file = files[0];
		if (!file.name.endsWith('.zip')) {
			toast.error('Please select a valid modpack ZIP file');
			return;
		}

		uploading = true;
		uploadAbortController = new AbortController();
		uploadProgress = null;

		try {
			const uploadResult = await uploadFile(file, {
				onProgress: (progress) => {
					uploadProgress = progress;
				},
				signal: uploadAbortController.signal
			});
			if (!uploadResult.sessionId) {
				throw new Error('Upload completed but no session ID returned');
			}

			const result = await rpcClient.modpack.importUploadedModpack({
				uploadSessionId: uploadResult.sessionId,
				name: file.name.replace('.zip', ''),
				description: ''
			});

			toast.success(`Modpack "${result.modpack?.name}" uploaded successfully`);
			await Promise.all([searchModpacks(), loadUploadedPacks()]);
		} catch (error: unknown) {
			if (error instanceof Error && error.message === 'Upload cancelled') {
				toast.info('Upload cancelled');
			}
		} finally {
			uploading = false;
			uploadProgress = null;
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

	async function handleRemoteImport() {
		const trimmedUrl = remoteUrl.trim();
		if (!trimmedUrl) {
			toast.error('Please enter a valid modpack URL');
			return;
		}

		importingRemote = true;
		try {
			const res = await rpcClient.modpack.importRemoteModpack({
				url: trimmedUrl,
				name: remoteName.trim(),
				description: remoteDescription.trim(),
				mcVersion: remoteMcVersion.trim(),
				modLoader: remoteModLoader.trim(),
				authToken: remoteAuthToken.trim()
			});

			toast.success(res.message || 'Modpack imported successfully');
			showRemoteModal = false;
			remoteUrl = '';
			remoteName = '';
			remoteDescription = '';
			remoteMcVersion = '';
			remoteModLoader = '';
			remoteAuthToken = '';

			await Promise.all([searchModpacks(), loadUploadedPacks()]);
		} catch (err: unknown) {
			toast.error(err instanceof Error ? err.message : 'Failed to import modpack');
			console.error(err);
		} finally {
			importingRemote = false;
		}
	}

	async function handleUseInServer(modpack: IndexedModpack) {
		try {
			const res = await rpcClient.node.listNodes({});
			const active = (res.nodes || []).filter((n) => n.enabled && n.status === NodeStatus.ONLINE);
			if (active.length === 0) {
				toast.error(
					'No active Docker nodes available — add and enable a node in Settings → Docker Nodes before creating a server.'
				);
				return;
			}
		} catch (error) {
			toast.error('Could not verify available nodes. Please try again.');
			console.error(error);
			return;
		}
		goto(resolve(`/servers/new?modpack=${modpack.id}`));
	}

	async function deleteModpack(modpack: IndexedModpack) {
		if (
			!confirm(`Are you sure you want to delete "${modpack.name}"? This action cannot be undone.`)
		) {
			return;
		}

		try {
			await rpcClient.modpack.deleteModpack({ id: modpack.id });
			toast.success(`Modpack "${modpack.name}" deleted successfully`);

			uploadedPacks = uploadedPacks.filter((m) => m.id !== modpack.id);
			favorites = favorites.filter((m) => m.id !== modpack.id);

			if (!showFavorites && !showUploaded) {
				await searchModpacks();
			}
		} catch (error) {
			toast.error(error instanceof Error ? error.message : 'Failed to delete modpack');
		}
	}

	let displayModpacks = $derived(
		showFavorites
			? favorites
			: showUploaded
				? uploadedPacks
				: (() => {
						const results = searchResults?.modpacks || [];
						const uploaded: IndexedModpack[] = [];
						const indexed: IndexedModpack[] = [];
						results.forEach((m) => (m.indexer === 'manual' ? uploaded : indexed).push(m));
						return [...uploaded, ...indexed];
					})()
	);
</script>

<svelte:head>
	<title>Modpacks - Carbon Panel</title>
</svelte:head>

<div class="h-full flex-1 space-y-6 rounded-none font-sans text-[#f4f4f4]">
	<!-- Top Bar -->
	<div
		class="flex flex-col justify-between gap-4 rounded-none border-b border-[#393939] pb-6 lg:flex-row lg:items-center"
	>
		<div class="flex items-center gap-4">
			<div
				class="flex h-12 w-12 items-center justify-center rounded-none border border-[#393939] bg-[#262626] text-[#0f62fe]"
			>
				<Package class="h-6 w-6" />
			</div>
			<div class="space-y-0.5">
				<h1 class="text-3xl font-semibold tracking-tight text-white">Modpacks</h1>
				<p class="text-xs text-[#a8a8a8]">
					Browse, upload, and deploy modpacks to Minecraft servers
				</p>
			</div>
		</div>

		<!-- Top Action Buttons -->
		<div class="flex flex-wrap items-center gap-2">
			<CarbonButton
				kind={showUploaded ? 'primary' : 'secondary'}
				size="sm"
				class="rounded-none"
				onclick={() => {
					showUploaded = !showUploaded;
					if (showUploaded) showFavorites = false;
				}}
			>
				{#if showUploaded}
					<ArrowLeft class="mr-1.5 h-4 w-4" />
					All Packs
				{:else}
					<Upload class="mr-1.5 h-4 w-4" />
					Uploaded ({uploadedPacks.length})
				{/if}
			</CarbonButton>

			<CarbonButton
				kind={showFavorites ? 'primary' : 'secondary'}
				size="sm"
				class="rounded-none"
				onclick={() => {
					showFavorites = !showFavorites;
					if (showFavorites) showUploaded = false;
				}}
			>
				{#if showFavorites}
					<ArrowLeft class="mr-1.5 h-4 w-4" />
					All Packs
				{:else}
					<Heart class="mr-1.5 h-4 w-4" />
					Favorites ({favorites.length})
				{/if}
			</CarbonButton>

			<CarbonButton
				kind="tertiary"
				size="sm"
				class="rounded-none"
				onclick={() => goto(resolve('/modpacks/studio'))}
			>
				<Boxes class="mr-1.5 h-4 w-4 text-[#0f62fe]" />
				Modpack Studio
			</CarbonButton>

			<CarbonButton
				kind="tertiary"
				size="sm"
				class="rounded-none"
				onclick={() => (showManifestInspector = true)}
			>
				<FileSearch class="mr-1.5 h-4 w-4" />
				Inspect Manifest
			</CarbonButton>
		</div>
	</div>

	{#if selectedIndexer === 'fuego'}
		<div
			class="flex items-center justify-between rounded-none border-y border-r border-l-4 border-[#0f62fe] border-[#393939] bg-[#262626] p-3.5 text-xs text-[#c6c6c6]"
		>
			<div class="flex items-center gap-2">
				<KeyRound class="h-4 w-4 shrink-0 text-[#78a9ff]" />
				<span
					>CurseForge Keyless Mode: syncing and browsing modpacks operates through community
					proxies.</span
				>
			</div>
			<CarbonButton
				kind="ghost"
				size="sm"
				class="h-7 rounded-none text-xs"
				href="/settings?tab=api-keys"
			>
				<Settings class="mr-1 h-3.5 w-3.5" />
				Custom API Key
			</CarbonButton>
		</div>
	{/if}

	{#if !showFavorites && !showUploaded}
		<!-- Search & Filter Controls -->
		<div class="space-y-4">
			<div class="grid grid-cols-1 gap-2 md:grid-cols-12">
				<div class="md:col-span-4">
					<CarbonSearch
						placeholder="Search modpacks by name or description..."
						bind:value={searchParams.query}
						onkeydown={(e) => e.key === 'Enter' && searchModpacks()}
						size="md"
					/>
				</div>

				<div class="md:col-span-2">
					<CarbonSelect bind:value={searchParams.gameVersion} disabled={loading}>
						<option value="">All MC Versions</option>
						{#each gameVersions as version}
							<option value={version}>{version}</option>
						{/each}
					</CarbonSelect>
				</div>

				<div class="md:col-span-2">
					<CarbonSelect bind:value={searchParams.modLoader} disabled={loading}>
						<option value="">All Loaders</option>
						<option value="forge">Forge</option>
						<option value="fabric">Fabric</option>
						<option value="neoforge">NeoForge</option>
						<option value="quilt">Quilt</option>
					</CarbonSelect>
				</div>

				<div class="md:col-span-2">
					<CarbonSelect
						bind:value={selectedIndexer}
						disabled={syncing}
						onchange={() => syncModpacks()}
					>
						<option value="modrinth">Modrinth</option>
						<option value="fuego">CurseForge</option>
					</CarbonSelect>
				</div>

				<div class="flex items-center gap-1 md:col-span-2">
					<CarbonButton
						kind="primary"
						class="w-full justify-center rounded-none"
						onclick={() => searchModpacks(true)}
						disabled={loading}
					>
						Search
					</CarbonButton>
				</div>
			</div>

			<!-- Secondary Action Row -->
			<div class="flex flex-wrap items-center justify-between gap-2 pt-1">
				<div class="flex items-center gap-2">
					<CarbonButton
						kind="tertiary"
						size="sm"
						class="rounded-none"
						onclick={syncModpacks}
						disabled={syncing}
					>
						<RefreshCw class={`mr-1.5 h-3.5 w-3.5 ${syncing ? 'animate-spin' : ''}`} />
						Sync {indexerName}
					</CarbonButton>

					<CarbonButton
						kind="tertiary"
						size="sm"
						class="rounded-none"
						onclick={() => fileInput?.click()}
						disabled={uploading}
					>
						<Upload class="mr-1.5 h-3.5 w-3.5" />
						Upload ZIP
					</CarbonButton>

					<CarbonButton
						kind="tertiary"
						size="sm"
						class="rounded-none"
						onclick={() => (showRemoteModal = true)}
						disabled={importingRemote}
					>
						<Globe class="mr-1.5 h-3.5 w-3.5" />
						Add from URL
					</CarbonButton>

					<input
						bind:this={fileInput}
						type="file"
						accept=".zip"
						onchange={handleModpackUpload}
						class="hidden"
					/>
				</div>

				<div class="font-mono text-xs text-[#8d8d8d]">
					{#if searchResults}
						TOTAL: {searchResults.total} PACKS
					{/if}
				</div>
			</div>

			{#if uploading && uploadProgress}
				<div class="space-y-2 rounded-none border border-[#393939] bg-[#262626] p-4">
					<div class="flex items-center justify-between text-xs">
						<span class="font-medium text-white">Uploading modpack archive...</span>
						<div class="flex items-center gap-3 font-mono text-[#a8a8a8]">
							<span>{uploadProgress.percentComplete.toFixed(0)}%</span>
							<span
								>{formatBytes(uploadProgress.bytesUploaded)} / {formatBytes(
									uploadProgress.totalBytes
								)}</span
							>
							<button
								type="button"
								onclick={cancelCurrentUpload}
								class="cursor-pointer text-[#da1e28] hover:text-white"
								title="Cancel upload"
								aria-label="Cancel upload"
							>
								<X class="h-4 w-4" />
							</button>
						</div>
					</div>
					<div class="h-2 w-full overflow-hidden rounded-none bg-[#161616]">
						<div
							class="h-full bg-[#0f62fe] transition-all"
							style="width: {uploadProgress.percentComplete}%"
						></div>
					</div>
				</div>
			{/if}
		</div>
	{/if}

	<!-- Modpack Packages Grid with CarbonTiles -->
	<div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
		{#each displayModpacks as modpack (modpack.id)}
			<CarbonTile
				class="group flex flex-col justify-between rounded-none border-[#393939] p-5 transition-colors hover:border-[#525252]"
			>
				<div class="space-y-3">
					<!-- Tile Header -->
					<div class="flex items-start gap-3">
						{#if modpack.logoUrl}
							<img
								src={modpack.logoUrl}
								alt={modpack.name}
								class="h-14 w-14 shrink-0 rounded-none border border-[#393939] bg-[#161616] object-cover"
							/>
						{:else}
							<div
								class="flex h-14 w-14 shrink-0 items-center justify-center rounded-none border border-[#393939] bg-[#161616] text-[#525252]"
							>
								<Package class="h-7 w-7" />
							</div>
						{/if}

						<div class="min-w-0 flex-1">
							<h3 class="truncate text-base font-semibold text-white" title={modpack.name}>
								{modpack.name}
							</h3>
							<div class="mt-1 flex flex-wrap items-center gap-2">
								<CarbonTag type={modpack.indexer === 'manual' ? 'purple' : 'blue'} size="sm">
									{modpack.indexer === 'manual' ? 'Manual Upload' : modpack.indexer}
								</CarbonTag>
								<span class="flex items-center gap-1 font-mono text-xs text-[#8d8d8d]">
									<Download class="inline h-3 w-3" />
									{formatNumber(modpack.downloadCount)}
								</span>
							</div>
						</div>

						<button
							type="button"
							onclick={() => toggleFavorite(modpack)}
							class="cursor-pointer rounded-none p-2 text-[#8d8d8d] transition-colors hover:text-[#da1e28]"
							title={modpack.isFavorited ? 'Remove favorite' : 'Add favorite'}
							aria-label={modpack.isFavorited
								? `Remove ${modpack.name} from favorites`
								: `Add ${modpack.name} to favorites`}
						>
							<Heart
								class={`h-4 w-4 ${modpack.isFavorited ? 'fill-[#da1e28] text-[#da1e28]' : ''}`}
							/>
						</button>
					</div>

					<!-- Description -->
					<p class="line-clamp-2 min-h-8 text-xs text-[#a8a8a8]">
						{modpack.summary || 'No summary available for this modpack.'}
					</p>

					<!-- Loaders & Versions -->
					<div class="space-y-1.5 pt-1">
						{#if parseJsonArray(modpack.modLoaders).length > 0}
							<div class="flex flex-wrap gap-1">
								{#each parseJsonArray(modpack.modLoaders) as loader (loader)}
									<CarbonTag type="cyan" size="sm">{loader}</CarbonTag>
								{/each}
							</div>
						{/if}

						{#if parseJsonArray(modpack.gameVersions).length > 0}
							<div class="truncate font-mono text-[11px] text-[#8d8d8d]">
								MC: {parseJsonArray(modpack.gameVersions).slice(0, 3).join(', ')}
								{#if parseJsonArray(modpack.gameVersions).length > 3}
									+{parseJsonArray(modpack.gameVersions).length - 3} more
								{/if}
							</div>
						{/if}
					</div>
				</div>

				<!-- Tile Actions -->
				<div class="mt-4 flex items-center justify-between gap-2 border-t border-[#393939] pt-3">
					<div class="flex items-center gap-1">
						{#if modpack.websiteUrl}
							<a href={modpack.websiteUrl} target="_blank" rel="noopener noreferrer">
								<CarbonButton kind="ghost" size="sm" class="rounded-none">
									<ExternalLink class="mr-1 h-3 w-3" />
									View
								</CarbonButton>
							</a>
						{/if}
						{#if modpack.indexer === 'manual'}
							<CarbonButton
								kind="danger"
								size="sm"
								class="rounded-none"
								onclick={() => deleteModpack(modpack)}
							>
								<Trash2 class="mr-1 h-3 w-3" />
								Delete
							</CarbonButton>
						{/if}
					</div>

					<CarbonButton
						kind="primary"
						size="sm"
						class="rounded-none"
						onclick={() => handleUseInServer(modpack)}
					>
						Use in Server
					</CarbonButton>
				</div>
			</CarbonTile>
		{/each}
	</div>

	<!-- Empty state -->
	{#if displayModpacks.length === 0}
		<div class="rounded-none border border-dashed border-[#393939] bg-[#262626] py-16 text-center">
			<Package class="mx-auto mb-3 h-10 w-10 text-[#525252]" />
			<p class="text-sm font-semibold text-[#f4f4f4]">
				{#if showFavorites}
					No favorite modpacks found
				{:else if loading}
					Loading modpack catalog...
				{:else if syncing}
					Syncing modpacks from indexers...
				{:else if searchParams.query}
					No modpacks matching "{searchParams.query}"
				{:else}
					No modpacks available locally
				{/if}
			</p>
			<p class="mx-auto mt-1 max-w-sm text-xs text-[#8d8d8d]">
				{#if showFavorites}
					Click the heart icon on any package to add it to your pinned favorites.
				{:else if !loading && !syncing}
					Click "Sync" to index popular modpacks or import a custom modpack from URL or ZIP.
				{/if}
			</p>
		</div>
	{/if}

	<!-- Pagination -->
	{#if !showFavorites && !showUploaded && searchResults && searchResults.total > searchResults.pageSize}
		<div class="flex items-center justify-center gap-3 border-t border-[#393939] pt-4">
			<CarbonButton
				kind="tertiary"
				size="sm"
				class="rounded-none"
				disabled={(searchParams.page || 1) === 1}
				onclick={() => {
					searchParams.page = Math.max(1, (searchParams.page || 1) - 1);
					searchModpacks(false);
				}}
			>
				Previous
			</CarbonButton>
			<span class="font-mono text-xs text-[#a8a8a8]">
				Page {searchParams.page} of {Math.ceil(searchResults.total / searchResults.pageSize)}
			</span>
			<CarbonButton
				kind="tertiary"
				size="sm"
				class="rounded-none"
				disabled={(searchParams.page || 1) >=
					Math.ceil(searchResults.total / searchResults.pageSize)}
				onclick={() => {
					searchParams.page = (searchParams.page || 1) + 1;
					searchModpacks(false);
				}}
			>
				Next
			</CarbonButton>
		</div>
	{/if}

	<!-- Remote Import Modal with Carbon Design System Fidelity -->
	<CarbonModal
		bind:open={showRemoteModal}
		title="Add Modpack from URL / GitHub"
		description="Import archive directly from GitHub Releases, Cloudflare R2, or static CDN"
		hasFooter={false}
		size="3xl"
	>
		<div class="space-y-4">
			<CarbonTextInput
				label="Modpack Archive URL *"
				placeholder="https://github.com/owner/repo/releases/download/v1.0/modpack.zip"
				bind:value={remoteUrl}
				disabled={importingRemote}
				helperText="Direct link to .zip or .mrpack archive file"
			/>

			<div class="grid grid-cols-2 gap-4">
				<CarbonTextInput
					label="Pack Name (Optional)"
					placeholder="Auto-detected if blank"
					bind:value={remoteName}
					disabled={importingRemote}
				/>
				<CarbonTextInput
					label="Minecraft Version"
					placeholder="e.g. 1.20.1 (or auto-detect)"
					bind:value={remoteMcVersion}
					disabled={importingRemote}
				/>
			</div>

			<div class="grid grid-cols-2 gap-4">
				<CarbonSelect label="Mod Loader" bind:value={remoteModLoader} disabled={importingRemote}>
					<option value="">Auto-detect from manifest</option>
					<option value="fabric">Fabric</option>
					<option value="forge">Forge</option>
					<option value="neoforge">NeoForge</option>
					<option value="quilt">Quilt</option>
					<option value="custom">Custom</option>
				</CarbonSelect>

				<CarbonTextInput
					type="password"
					label="Auth Token (Optional)"
					placeholder="Bearer token or GitHub PAT"
					bind:value={remoteAuthToken}
					disabled={importingRemote}
				/>
			</div>

			<CarbonTextInput
				label="Description (Optional)"
				placeholder="Brief description or release summary"
				bind:value={remoteDescription}
				disabled={importingRemote}
			/>

			<div class="flex items-center justify-end gap-3 border-t border-[#393939] pt-4">
				<CarbonButton
					kind="secondary"
					onclick={() => (showRemoteModal = false)}
					disabled={importingRemote}
					class="rounded-none"
				>
					Cancel
				</CarbonButton>
				<CarbonButton
					kind="primary"
					onclick={handleRemoteImport}
					disabled={importingRemote || !remoteUrl.trim()}
					class="rounded-none"
				>
					{#if importingRemote}
						<Loader2 class="mr-2 h-4 w-4 animate-spin" />
						Downloading & Importing...
					{:else}
						<Download class="mr-2 h-4 w-4" />
						Import Modpack
					{/if}
				</CarbonButton>
			</div>
		</div>
	</CarbonModal>

	<ManifestInspectorDialog
		bind:open={showManifestInspector}
		onOpenChange={(v) => (showManifestInspector = v)}
	/>
</div>
