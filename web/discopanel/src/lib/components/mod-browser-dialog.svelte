<script lang="ts">
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
		Sparkles,
		AlertTriangle,
		Layers
	} from '@lucide/svelte';
	import { toast } from 'svelte-sonner';
	import type { Server } from '$lib/proto/discopanel/v1/common_pb';
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
				case 1: return 'forge';
				case 2: return 'fabric';
				case 3: return 'quilt';
				case 4: return 'neoforge';
				default: return 'fabric';
			}
		}
		const s = String(val || '').toLowerCase().replace('mod_loader_', '');
		if (s.includes('neoforge')) return 'neoforge';
		if (s.includes('forge') || s.includes('curseforge')) return 'forge';
		if (s.includes('quilt')) return 'quilt';
		if (s.includes('fabric')) return 'fabric';
		return s || 'fabric';
	}

	async function searchMods() {
		searching = true;
		searchError = '';
		mods = [];
		try {
			const loader = getLoaderString(server.modLoader);
			const params = new URLSearchParams({
				query: searchQuery.trim(),
				platform,
				loader,
				mc_version: filterByVersion ? (server.mcVersion || '') : ''
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

	$effect(() => {
		if (open && mods.length === 0 && !searching) {
			searchMods();
		}
	});

	async function viewModVersions(mod: SearchMod) {
		selectedMod = mod;
		loadingVersions = true;
		versions = [];
		selectedVersion = null;
		selectedDependencies = {};

		try {
			const loader = getLoaderString(server.modLoader);
			const params = new URLSearchParams({
				platform: mod.platform,
				loader,
				mc_version: filterByVersion ? (server.mcVersion || '') : ''
			});

			const res = await apiFetch(`/api/v1/servers/${server.id}/mods/${mod.slug || mod.id}/versions?${params.toString()}`);
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

	$effect(() => {
		if (open) {
			searchMods();
		} else {
			selectedMod = null;
		}
	});
</script>

<DialogPrimitive.Root bind:open>
	<DialogContent class="max-h-[90vh] !max-w-4xl overflow-hidden flex flex-col p-6">
		<DialogHeader>
			<div class="flex items-center justify-between">
				<div class="flex items-center gap-3">
					{#if selectedMod}
						<Button variant="ghost" size="sm" onclick={() => (selectedMod = null)} class="h-8 w-8 p-0">
							<ArrowLeft class="h-4 w-4" />
						</Button>
					{/if}
					<div>
						<DialogTitle class="flex items-center gap-2 text-xl font-bold">
							<Sparkles class="h-5 w-5 text-primary" />
							{selectedMod ? selectedMod.title : 'Browse & Install Online Mods'}
						</DialogTitle>
						<DialogDescription>
							Loader: <span class="font-semibold text-foreground">{getLoaderString(server.modLoader).toUpperCase()}</span>
							{#if filterByVersion && server.mcVersion}
								· Filtering for MC <span class="font-semibold text-foreground">{server.mcVersion}</span>
							{:else}
								· <span class="text-muted-foreground">All MC versions</span>
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
								searchMods();
							}}
							class="inline-flex items-center rounded-md px-2.5 py-0.5 text-xs font-mono font-semibold transition-colors cursor-pointer {filterByVersion ? 'bg-secondary text-secondary-foreground hover:bg-secondary/80' : 'bg-muted text-muted-foreground border border-dashed hover:text-foreground'}"
							title="Click to toggle version filter"
						>
							MC {server.mcVersion} {filterByVersion ? '✓' : '(any)'}
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
						<Search class="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
					</div>

					<Button onclick={searchMods} disabled={searching}>
						{#if searching}
							<Loader2 class="h-4 w-4 animate-spin" />
						{:else}
							Search
						{/if}
					</Button>
				</div>
			</div>

			<!-- Search Results / Mod List -->
			<div class="mt-4 flex-1 overflow-y-auto pr-1 space-y-3 min-h-[360px] max-h-[500px]">
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
				{:else if mods.length === 0}
					<div class="flex flex-col items-center justify-center py-20 text-muted-foreground">
						<Package class="h-10 w-10 stroke-[1.5]" />
						<p class="mt-3 text-base font-medium">No matching mods found</p>
						<p class="text-xs text-muted-foreground">Try a different query or switch between Modrinth and CurseForge.</p>
					</div>
				{:else}
					<div class="grid grid-cols-1 gap-3">
						{#each mods as mod (mod.id)}
							<Card class="hover:border-primary/50 transition-colors">
								<CardContent class="p-4 flex items-start justify-between gap-4">
									<div class="flex items-start gap-3.5 flex-1 min-w-0">
										{#if mod.icon_url}
											<img
												src={mod.icon_url}
												alt={mod.title}
												class="h-12 w-12 rounded-lg object-contain bg-muted/40 p-1 flex-shrink-0"
											/>
										{:else}
											<div class="h-12 w-12 rounded-lg bg-muted flex items-center justify-center flex-shrink-0">
												<Package class="h-6 w-6 text-muted-foreground" />
											</div>
										{/if}

										<div class="space-y-1 flex-1 min-w-0">
											<div class="flex items-center gap-2">
												<span class="font-semibold text-base text-foreground truncate">{mod.title}</span>
												{#if mod.author}
													<span class="text-xs text-muted-foreground truncate">by {mod.author}</span>
												{/if}
												{#if mod.installed}
													<Badge variant="default" class="bg-emerald-600 text-white text-[10px] px-1.5 py-0 h-4">
														Installed
													</Badge>
												{/if}
											</div>
											<p class="text-xs text-muted-foreground line-clamp-2">{mod.description}</p>

											<div class="flex flex-wrap items-center gap-1.5 pt-1">
												<Badge variant="secondary" class="text-[11px] h-5 px-1.5">
													<Download class="mr-1 h-3 w-3" />
													{formatDownloads(mod.downloads)}
												</Badge>
												{#each (mod.categories || []).slice(0, 3) as cat}
													<Badge variant="outline" class="text-[11px] h-5 px-1.5 capitalize">
														{cat}
													</Badge>
												{/each}
											</div>
										</div>
									</div>

									<div class="flex flex-col items-end gap-2 flex-shrink-0">
										<Button size="sm" onclick={() => viewModVersions(mod)}>
											Select Version
										</Button>
									</div>
								</CardContent>
							</Card>
						{/each}
					</div>
				{/if}
			</div>

		{:else}
			<!-- Mod Versions & 1-Click Install View -->
			<div class="mt-4 flex-1 overflow-y-auto pr-1 space-y-4 min-h-[360px] max-h-[500px]">
				<div class="rounded-lg border bg-muted/20 p-4 space-y-2">
					<div class="flex items-center justify-between">
						<span class="font-semibold text-base">{selectedMod.title}</span>
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
							No versions were found matching Minecraft {server.mcVersion} and {getLoaderString(server.modLoader)}.
						</AlertDescription>
					</Alert>
				{:else}
					<div class="space-y-3">
						<label class="text-sm font-medium">Compatible Versions ({versions.length} available)</label>
						<div class="space-y-2 max-h-48 overflow-y-auto border rounded-lg p-2">
							{#each versions as ver (ver.id)}
								<div
									role="button"
									tabindex="0"
									onclick={() => (selectedVersion = ver)}
									onkeydown={(e) => e.key === 'Enter' && (selectedVersion = ver)}
									class="flex items-center justify-between p-2 rounded-md cursor-pointer text-sm transition-colors
										{selectedVersion?.id === ver.id ? 'bg-primary/15 border border-primary/40 font-medium' : 'hover:bg-muted/50'}"
								>
									<div class="flex items-center gap-2">
										<span>{ver.version_number || ver.name}</span>
										<Badge
											variant={ver.version_type === 'release' ? 'default' : 'secondary'}
											class="text-[10px] h-4 px-1 capitalize"
										>
											{ver.version_type}
										</Badge>
									</div>
									<div class="flex items-center gap-2 text-xs text-muted-foreground font-mono">
										<span>{ver.file_name}</span>
										<span>({formatBytes(ver.file_size)})</span>
									</div>
								</div>
							{/each}
						</div>
					</div>

					<!-- Dependencies Section -->
					{#if selectedVersion && (selectedVersion.dependencies || []).length > 0}
						<div class="rounded-lg border border-primary/30 bg-primary/5 p-4 space-y-2">
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

					<div class="pt-3 flex justify-end gap-3">
						<Button variant="outline" onclick={() => (selectedMod = null)}>
							Cancel
						</Button>
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
