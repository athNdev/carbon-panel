<script lang="ts">
	import { untrack } from 'svelte';
	import { Dialog as DialogPrimitive } from 'bits-ui';
	import {
		DialogContent,
		DialogDescription,
		DialogHeader,
		DialogTitle
	} from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Badge } from '$lib/components/ui/badge';
	import { Tabs, TabsList, TabsTrigger } from '$lib/components/ui/tabs';
	import { Card, CardContent } from '$lib/components/ui/card';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import { Alert, AlertDescription, AlertTitle } from '$lib/components/ui/alert';
	import {
		Search,
		Download,
		Package,
		Loader2,
		CheckCircle2,
		ExternalLink,
		ArrowLeft,
		Boxes,
		AlertTriangle,
		Layers,
		Filter,
		X,
		Plus,
		Check
	} from '@lucide/svelte';
	import { toast } from 'svelte-sonner';
	import type { Server } from '$lib/proto/carbonpanel/v1/common_pb';
	import { formatBytes } from '$lib/utils';
	import { apiFetch } from '$lib/api/fetch';

	interface Props {
		open: boolean;
		server: Server;
		onInstalled?: () => void;
	}

	interface SearchMod {
		id: string;
		slug: string;
		title: string;
		description: string;
		icon_url: string;
		author: string;
		downloads: number;
		categories: string[];
		platform: string;
		client_side?: string;
		server_side?: string;
		installed: boolean;
	}

	interface ModDependency {
		project_id: string;
		version_id?: string;
		dependency_type: string;
		title?: string;
	}

	interface ModVersion {
		id: string;
		version_number: string;
		name: string;
		version_type: string;
		file_name: string;
		download_url: string;
		file_size: number;
		dependencies: ModDependency[];
	}

	interface ActiveFilter {
		key: string;
		value: string;
		removable: boolean;
	}

	let { open = $bindable(false), server, onInstalled }: Props = $props();

	let searchQuery = $state('');
	let platform = $state<'modrinth' | 'curseforge'>('modrinth');
	let searching = $state(false);
	let mods = $state<SearchMod[]>([]);
	let searchError = $state('');

	// Version selection view
	let selectedMod = $state<SearchMod | null>(null);
	let loadingVersions = $state(false);
	let versions = $state<ModVersion[]>([]);
	let selectedVersion = $state<ModVersion | null>(null);
	let selectedDependencies = $state<Record<string, boolean>>({});
	let installing = $state(false);

	let filterByVersion = $state(true);

	function getLoaderString(val: number | string): string {
		if (typeof val === 'number') {
			switch (val) {
				case 1:
					return 'forge';
				case 2:
					return 'fabric';
				case 3:
					return 'quilt';
				case 4:
					return 'neoforge';
				default:
					return 'fabric';
			}
		}
		const s = String(val || '')
			.toLowerCase()
			.replace('mod_loader_', '');
		if (s.includes('neoforge')) return 'neoforge';
		if (s.includes('forge') || s.includes('curseforge')) return 'forge';
		if (s.includes('quilt')) return 'quilt';
		if (s.includes('fabric')) return 'fabric';
		return s || 'fabric';
	}

	// Filter Bar State
	let activeFilters = $state<ActiveFilter[]>([
		{ key: 'side', value: 'server', removable: true },
		{ key: 'loader', value: getLoaderString(server.modLoader), removable: false },
		...(server.mcVersion ? [{ key: 'version', value: server.mcVersion, removable: true }] : [])
	]);

	let filterInput = $state('');
	let filterInputFocused = $state(false);

	const FILTER_VOCABULARY = [
		{ label: 'side:server', desc: 'Server-compatible only (omits client-only)' },
		{ label: 'side:both', desc: 'Runs on client and server' },
		{ label: 'side:client', desc: 'Client-side mods' },
		{ label: 'loader:fabric', desc: 'Fabric mod loader' },
		{ label: 'loader:forge', desc: 'Forge mod loader' },
		{ label: 'loader:neoforge', desc: 'NeoForge mod loader' },
		{ label: 'loader:quilt', desc: 'Quilt mod loader' },
		{ label: 'version:1.21.4', desc: 'Minecraft 1.21.4' },
		{ label: 'version:1.21.1', desc: 'Minecraft 1.21.1' },
		{ label: 'version:1.20.4', desc: 'Minecraft 1.20.4' },
		{ label: 'version:1.20.1', desc: 'Minecraft 1.20.1' },
		{ label: 'version:1.19.4', desc: 'Minecraft 1.19.4' },
		{ label: 'version:1.19.2', desc: 'Minecraft 1.19.2' },
		{ label: 'version:1.18.2', desc: 'Minecraft 1.18.2' },
		{ label: 'version:1.16.5', desc: 'Minecraft 1.16.5' },
		{ label: 'category:optimization', desc: 'Performance and FPS' },
		{ label: 'category:technology', desc: 'Tech & machinery' },
		{ label: 'category:magic', desc: 'Spells, rituals, sorcery' },
		{ label: 'category:storage', desc: 'Chests & backpacks' },
		{ label: 'category:adventure', desc: 'Dungeons & exploration' },
		{ label: 'category:utility', desc: 'QoL tools & utilities' },
		{ label: 'category:worldgen', desc: 'Biomes & structures' }
	];

	let suggestions = $derived.by(() => {
		const q = filterInput.trim().toLowerCase();
		if (!q) {
			return FILTER_VOCABULARY.slice(0, 6);
		}
		return FILTER_VOCABULARY.filter(
			(item) => item.label.toLowerCase().includes(q) || item.desc.toLowerCase().includes(q)
		).slice(0, 6);
	});

	function addFilter(filterStr: string) {
		const parts = filterStr.split(':');
		if (parts.length >= 2) {
			const key = parts[0].trim().toLowerCase();
			const value = parts.slice(1).join(':').trim().toLowerCase();
			activeFilters = activeFilters.filter((f) => f.key !== key);
			activeFilters.push({ key, value, removable: true });
			filterInput = '';
			filterInputFocused = false;
			searchMods();
		}
	}

	function removeFilter(key: string) {
		activeFilters = activeFilters.filter((f) => f.key !== key);
		searchMods();
	}

	// Filtered mods list - omits client-only mods by default in server context
	let displayMods = $derived.by(() => {
		const sideFilter = activeFilters.find((f) => f.key === 'side')?.value;
		if (sideFilter === 'server') {
			return mods.filter((m) => m.server_side !== 'unsupported');
		} else if (sideFilter === 'client') {
			return mods.filter((m) => m.client_side !== 'unsupported');
		}
		return mods;
	});

	async function searchMods() {
		searching = true;
		searchError = '';
		mods = [];
		try {
			const loaderVal =
				activeFilters.find((f) => f.key === 'loader')?.value || getLoaderString(server.modLoader);
			const versionVal =
				activeFilters.find((f) => f.key === 'version')?.value ||
				(filterByVersion ? server.mcVersion || '' : '');
			const sideVal = activeFilters.find((f) => f.key === 'side')?.value || '';
			const categoryVal = activeFilters.find((f) => f.key === 'category')?.value || '';

			const params = new URLSearchParams({
				query: searchQuery.trim(),
				platform,
				loader: loaderVal,
				mc_version: versionVal,
				side: sideVal,
				...(categoryVal ? { category: categoryVal } : {})
			});

			const res = await apiFetch(`/api/v1/servers/${server.id}/mods/search?${params.toString()}`);
			if (!res.ok) {
				const errorText = await res.text();
				throw new Error(errorText || `HTTP ${res.status}`);
			}
			const data = await res.json();
			if (data.error) {
				searchError = data.error;
			} else {
				mods = data.results || [];
			}
		} catch (err: any) {
			console.error('Failed to search mods:', err);
			searchError = `Failed to search mods: ${err.message || 'Please verify server connection.'}`;
		} finally {
			searching = false;
		}
	}

	async function viewModVersions(mod: SearchMod) {
		selectedMod = mod;
		loadingVersions = true;
		versions = [];
		selectedVersion = null;
		selectedDependencies = {};

		try {
			const loaderVal =
				activeFilters.find((f) => f.key === 'loader')?.value || getLoaderString(server.modLoader);
			const versionVal =
				activeFilters.find((f) => f.key === 'version')?.value ||
				(filterByVersion ? server.mcVersion || '' : '');
			const params = new URLSearchParams({
				platform: mod.platform,
				loader: loaderVal,
				mc_version: versionVal
			});

			const modTarget = mod.platform === 'curseforge' && mod.id ? mod.id : mod.slug || mod.id;
			const res = await apiFetch(
				`/api/v1/servers/${server.id}/mods/${modTarget}/versions?${params.toString()}`
			);
			if (!res.ok) {
				const errorText = await res.text();
				throw new Error(errorText || `HTTP ${res.status}`);
			}
			const data = await res.json();
			versions = data.versions || [];
			if (versions.length > 0) {
				selectedVersion = versions[0];
				// Pre-select required dependencies
				for (const dep of selectedVersion.dependencies || []) {
					if (dep.dependency_type === 'required') {
						selectedDependencies[dep.project_id] = true;
					}
				}
			}
		} catch (err) {
			console.error('Failed to load versions:', err);
			toast.error('Failed to load compatible versions for this mod');
		} finally {
			loadingVersions = false;
		}
	}

	async function installMod() {
		if (!selectedVersion || !selectedMod) return;

		installing = true;
		try {
			const items = [
				{
					name: selectedMod.title,
					filename: selectedVersion.file_name,
					download_url: selectedVersion.download_url
				}
			];

			const res = await apiFetch(`/api/v1/servers/${server.id}/mods/install`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ items })
			});

			const data = await res.json();
			if (data.success) {
				toast.success(data.message || `Installed ${selectedMod.title}`);
				selectedMod.installed = true;
				if (onInstalled) {
					onInstalled();
				}
				selectedMod = null;
			} else {
				toast.error(data.message || 'Failed to install mod');
			}
		} catch (err) {
			console.error('Failed to install mod:', err);
			toast.error('Installation request failed');
		} finally {
			installing = false;
		}
	}

	function formatDownloads(count: number): string {
		if (!count) return '0';
		if (count >= 1_000_000) {
			return `${(count / 1_000_000).toFixed(1)}M`;
		}
		if (count >= 1_000) {
			return `${(count / 1_000).toFixed(1)}k`;
		}
		return count.toString();
	}

	let wasOpen = false;
	$effect(() => {
		if (open && !wasOpen) {
			wasOpen = true;
			untrack(() => {
				searchMods();
			});
		} else if (!open && wasOpen) {
			wasOpen = false;
			selectedMod = null;
		}
	});
</script>

<DialogPrimitive.Root bind:open>
	<DialogContent class="flex max-h-[90vh] !max-w-4xl flex-col overflow-hidden p-6">
		<DialogHeader>
			<div class="flex items-center justify-between">
				<div class="flex items-center gap-3">
					{#if selectedMod}
						<Button
							variant="ghost"
							size="sm"
							onclick={() => (selectedMod = null)}
							class="h-8 w-8 p-0"
						>
							<ArrowLeft class="h-4 w-4" />
						</Button>
					{/if}
					<div>
						<DialogTitle class="flex items-center gap-2 text-xl font-bold">
							<Boxes class="h-5 w-5 text-primary" />
							{selectedMod ? selectedMod.title : 'Browse & Install Online Mods'}
						</DialogTitle>
						<DialogDescription>
							Loader: <span class="font-semibold text-foreground"
								>{getLoaderString(server.modLoader).toUpperCase()}</span
							>
							{#if filterByVersion && server.mcVersion}
								Â· Filtering for MC <span class="font-semibold text-foreground"
									>{server.mcVersion}</span
								>
							{:else}
								Â· <span class="text-muted-foreground">All MC versions</span>
							{/if}
						</DialogDescription>
					</div>
				</div>
				<div class="flex items-center gap-2">
					<Badge variant="outline" class="font-mono text-xs uppercase">
						{getLoaderString(server.modLoader)}
					</Badge>
					{#if server.mcVersion}
						<button
							type="button"
							onclick={() => {
								filterByVersion = !filterByVersion;
								if (filterByVersion) {
									if (!activeFilters.some((f) => f.key === 'version')) {
										activeFilters.push({
											key: 'version',
											value: server.mcVersion,
											removable: true
										});
									}
								} else {
									activeFilters = activeFilters.filter((f) => f.key !== 'version');
								}
								searchMods();
							}}
							class="inline-flex cursor-pointer items-center rounded-md px-2.5 py-0.5 font-mono text-xs font-semibold transition-colors {filterByVersion
								? 'bg-secondary text-secondary-foreground hover:bg-secondary/80'
								: 'border border-dashed bg-muted text-muted-foreground hover:text-foreground'}"
							title="Click to toggle version filter"
						>
							MC {server.mcVersion}
							{filterByVersion ? 'âœ“' : '(any)'}
						</button>
					{/if}
				</div>
			</div>
		</DialogHeader>

		{#if !selectedMod}
			<!-- Search & Platform Bar -->
			<div class="mt-4 flex flex-col gap-3">
				<div class="flex items-center gap-2">
					<Tabs
						value={platform}
						onValueChange={(val) => {
							platform = val as 'modrinth' | 'curseforge';
							searchMods();
						}}
						class="w-auto"
					>
						<TabsList>
							<TabsTrigger value="modrinth" class="px-3">Modrinth</TabsTrigger>
							<TabsTrigger value="curseforge" class="px-3">CurseForge</TabsTrigger>
						</TabsList>
					</Tabs>

					<div class="relative flex-1">
						<Input
							placeholder={`Search ${platform === 'modrinth' ? 'Modrinth' : 'CurseForge'} mods...`}
							bind:value={searchQuery}
							onkeydown={(e) => e.key === 'Enter' && searchMods()}
							class="pl-9"
						/>
						<Search
							class="absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-muted-foreground"
						/>
					</div>

					<Button onclick={searchMods} disabled={searching}>
						{#if searching}
							<Loader2 class="h-4 w-4 animate-spin" />
						{:else}
							Search
						{/if}
					</Button>
				</div>

				<!-- Interactive Filter Bar -->
				<div class="flex flex-wrap items-center gap-2 border-t border-border/40 pt-1">
					<div class="mr-1 flex items-center text-xs font-semibold text-muted-foreground">
						<Filter class="mr-1 h-3.5 w-3.5 text-primary" />
						Filters:
					</div>

					{#each activeFilters as f (f.key)}
						<span
							class="inline-flex items-center gap-1 rounded-md border border-border/50 bg-secondary px-2 py-0.5 text-xs font-medium text-secondary-foreground"
						>
							<span class="font-mono text-[11px] text-muted-foreground">{f.key}:</span>
							<span class="font-semibold">{f.value}</span>
							{#if f.removable}
								<button
									type="button"
									onclick={() => removeFilter(f.key)}
									class="ml-0.5 cursor-pointer transition-colors hover:text-destructive"
									title={`Remove ${f.key} filter`}
								>
									<X class="h-3 w-3" />
								</button>
							{/if}
						</span>
					{/each}

					<!-- Filter input with auto-suggestions -->
					<div class="relative inline-block">
						<div class="flex items-center">
							<input
								type="text"
								placeholder="+ Add filter (e.g. side:server, category:optimization)..."
								bind:value={filterInput}
								onfocus={() => (filterInputFocused = true)}
								onblur={() => setTimeout(() => (filterInputFocused = false), 200)}
								onkeydown={(e) => {
									if (e.key === 'Enter' && filterInput.trim()) {
										addFilter(filterInput.trim());
									} else if (e.key === 'Escape') {
										filterInputFocused = false;
									}
								}}
								class="h-7 w-72 rounded-md border border-dashed border-border bg-muted/50 px-2 text-xs placeholder:text-muted-foreground/70 focus:border-primary focus:bg-background focus:outline-none"
							/>
						</div>

						{#if filterInputFocused && suggestions.length > 0}
							<div
								class="absolute top-full left-0 z-50 mt-1 w-80 overflow-hidden rounded-lg border bg-popover py-1 text-popover-foreground shadow-lg"
							>
								<div
									class="mb-1 border-b px-2 py-1 text-[10px] font-semibold tracking-wider text-muted-foreground uppercase"
								>
									Filter Suggestions (type key:value)
								</div>
								{#each suggestions as s}
									<button
										type="button"
										onmousedown={() => addFilter(s.label)}
										class="flex w-full cursor-pointer flex-col px-2.5 py-1.5 text-left text-xs transition-colors hover:bg-accent hover:text-accent-foreground"
									>
										<span class="font-mono font-medium text-primary">{s.label}</span>
										<span class="text-[10px] text-muted-foreground">{s.desc}</span>
									</button>
								{/each}
							</div>
						{/if}
					</div>
				</div>
			</div>

			<!-- Search Results / Mod List -->
			<div class="mt-4 max-h-[500px] min-h-[360px] flex-1 space-y-3 overflow-y-auto pr-1">
				{#if searchError}
					<Alert variant="destructive" class="my-4">
						<AlertTriangle class="h-4 w-4" />
						<AlertTitle>Search Notice</AlertTitle>
						<AlertDescription>{searchError}</AlertDescription>
					</Alert>
				{:else if searching}
					<div class="flex flex-col items-center justify-center py-20 text-muted-foreground">
						<Loader2 class="h-8 w-8 animate-spin text-primary" />
						<p class="mt-3 text-sm">Searching compatible mods for MC {server.mcVersion}...</p>
					</div>
				{:else if displayMods.length === 0}
					<div class="flex flex-col items-center justify-center py-20 text-muted-foreground">
						<Package class="h-10 w-10 stroke-[1.5]" />
						<p class="mt-3 text-base font-medium">No matching mods found</p>
						<p class="text-xs text-muted-foreground">
							Try a different query or remove some active filters.
						</p>
					</div>
				{:else}
					<div class="grid grid-cols-1 gap-3">
						{#each displayMods as mod (mod.id)}
							<Card class="transition-colors hover:border-primary/50">
								<CardContent class="flex items-start justify-between gap-4 p-4">
									<div class="flex min-w-0 flex-1 items-start gap-3.5">
										{#if mod.icon_url}
											<img
												src={mod.icon_url}
												alt={mod.title}
												class="h-12 w-12 flex-shrink-0 rounded-lg bg-muted/40 object-contain p-1"
											/>
										{:else}
											<div
												class="flex h-12 w-12 flex-shrink-0 items-center justify-center rounded-lg bg-muted"
											>
												<Package class="h-6 w-6 text-muted-foreground" />
											</div>
										{/if}

										<div class="min-w-0 flex-1 space-y-1">
											<div class="flex items-center gap-2">
												<span class="truncate text-base font-semibold text-foreground"
													>{mod.title}</span
												>
												{#if mod.author}
													<span class="truncate text-xs text-muted-foreground">by {mod.author}</span
													>
												{/if}
												{#if mod.installed}
													<Badge
														variant="default"
														class="h-4 bg-emerald-600 px-1.5 py-0 text-[10px] text-white"
													>
														Installed
													</Badge>
												{/if}
											</div>
											<p class="line-clamp-2 text-xs text-muted-foreground">{mod.description}</p>

											<div class="flex flex-wrap items-center gap-1.5 pt-1">
												<Badge variant="secondary" class="h-5 px-1.5 text-[11px]">
													<Download class="mr-1 h-3 w-3" />
													{formatDownloads(mod.downloads)}
												</Badge>
												{#if mod.server_side}
													<Badge
														variant="outline"
														class="h-5 px-1.5 text-[10px] {mod.server_side === 'required'
															? 'border-primary/60 text-primary'
															: 'text-muted-foreground'}"
													>
														Server: {mod.server_side}
													</Badge>
												{/if}
												{#if mod.client_side}
													<Badge
														variant="outline"
														class="h-5 px-1.5 text-[10px] text-muted-foreground"
													>
														Client: {mod.client_side}
													</Badge>
												{/if}
												{#each (mod.categories || []).slice(0, 3) as cat}
													<Badge variant="outline" class="h-5 px-1.5 text-[11px] capitalize">
														{cat}
													</Badge>
												{/each}
											</div>
										</div>
									</div>

									<div class="flex flex-shrink-0 flex-col items-end gap-2">
										<Button size="sm" onclick={() => viewModVersions(mod)}>Select Version</Button>
									</div>
								</CardContent>
							</Card>
						{/each}
					</div>
				{/if}
			</div>
		{:else}
			<!-- Mod Versions & 1-Click Install View -->
			<div class="mt-4 max-h-[500px] min-h-[360px] flex-1 space-y-4 overflow-y-auto pr-1">
				<div class="space-y-2 rounded-lg border bg-muted/20 p-4">
					<div class="flex items-center justify-between">
						<span class="text-base font-semibold">{selectedMod.title}</span>
						<Badge variant="outline">{selectedMod.platform.toUpperCase()}</Badge>
					</div>
					<p class="text-sm text-muted-foreground">{selectedMod.description}</p>
				</div>

				{#if loadingVersions}
					<div class="flex flex-col items-center justify-center py-16 text-muted-foreground">
						<Loader2 class="h-8 w-8 animate-spin text-primary" />
						<p class="mt-3 text-sm">Fetching compatible versions and dependencies...</p>
					</div>
				{:else if versions.length === 0}
					<Alert variant="destructive">
						<AlertTriangle class="h-4 w-4" />
						<AlertTitle>No Compatible Versions</AlertTitle>
						<AlertDescription>
							No versions were found matching Minecraft {server.mcVersion} and {getLoaderString(
								server.modLoader
							)}.
						</AlertDescription>
					</Alert>
				{:else}
					<div class="space-y-3">
						<label class="text-sm font-medium"
							>Compatible Versions ({versions.length} available)</label
						>
						<div class="max-h-48 space-y-2 overflow-y-auto rounded-lg border p-2">
							{#each versions as ver (ver.id)}
								<div
									role="button"
									tabindex="0"
									onclick={() => (selectedVersion = ver)}
									onkeydown={(e) => e.key === 'Enter' && (selectedVersion = ver)}
									class="flex cursor-pointer items-center justify-between rounded-md p-2 text-sm transition-colors
										{selectedVersion?.id === ver.id
										? 'border border-primary/40 bg-primary/15 font-medium'
										: 'hover:bg-muted/50'}"
								>
									<div class="flex items-center gap-2">
										<span>{ver.version_number || ver.name}</span>
										<Badge
											variant={ver.version_type === 'release' ? 'default' : 'secondary'}
											class="h-4 px-1 text-[10px] capitalize"
										>
											{ver.version_type}
										</Badge>
									</div>
									<div class="flex items-center gap-2 font-mono text-xs text-muted-foreground">
										<span>{ver.file_name}</span>
										<span>({formatBytes(ver.file_size)})</span>
									</div>
								</div>
							{/each}
						</div>
					</div>

					<!-- Dependencies Section -->
					{#if selectedVersion && (selectedVersion.dependencies || []).length > 0}
						<div class="space-y-2 rounded-lg border border-primary/30 bg-primary/5 p-4">
							<div class="flex items-center gap-2 text-sm font-semibold text-foreground">
								<Layers class="h-4 w-4 text-primary" />
								Required Dependencies Detected
							</div>
							<p class="text-xs text-muted-foreground">
								This mod specifies {selectedVersion.dependencies.length} upstream dependency/dependencies:
							</p>
							<div class="space-y-1.5 pt-1">
								{#each selectedVersion.dependencies as dep}
									<div class="flex items-center gap-2 text-xs">
										<Badge variant="outline" class="font-mono text-[10px]">
											{dep.dependency_type}
										</Badge>
										<span class="font-medium text-foreground">{dep.title || dep.project_id}</span>
									</div>
								{/each}
							</div>
						</div>
					{/if}

					<div class="flex justify-end gap-3 pt-3">
						<Button variant="outline" onclick={() => (selectedMod = null)}>Cancel</Button>
						<Button onclick={installMod} disabled={installing || !selectedVersion}>
							{#if installing}
								<Loader2 class="mr-2 h-4 w-4 animate-spin" />
								Installing to /mods...
							{:else}
								<Download class="mr-2 h-4 w-4" />
								1-Click Install to Server
							{/if}
						</Button>
					</div>
				{/if}
			</div>
		{/if}
	</DialogContent>
</DialogPrimitive.Root>
