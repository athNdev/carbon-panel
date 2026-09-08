<script lang="ts">
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import {
		Card,
		CardContent,
		CardDescription,
		CardHeader,
		CardTitle
	} from '$lib/components/ui/card';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import { Badge } from '$lib/components/ui/badge';
	import { Tabs, TabsList, TabsTrigger } from '$lib/components/ui/tabs';
	import { Dialog as DialogPrimitive } from 'bits-ui';
	import {
		DialogContent,
		DialogDescription,
		DialogHeader,
		DialogTitle
	} from '$lib/components/ui/dialog';
	import { Alert, AlertDescription, AlertTitle } from '$lib/components/ui/alert';
	import {
		ArrowLeft,
		Save,
		Download,
		Rocket,
		Plus,
		Search,
		Trash2,
		Pin,
		PinOff,
		Package,
		ExternalLink,
		Loader2,
		Sparkles,
		Layers,
		CheckCircle2,
		AlertTriangle
	} from '@lucide/svelte';
	import { toast } from 'svelte-sonner';
	import { apiFetch } from '$lib/api/fetch';
	import ModpackDeployDialog from '$lib/components/modpack-deploy-dialog.svelte';
	import { formatBytes } from '$lib/utils';

	interface ModItem {
		slug: string;
		name: string;
		file_name: string;
		side: 'both' | 'client' | 'server';
		platform: string;
		project_id: string;
		version_id: string;
		download_url: string;
		file_size?: number;
		pinned?: boolean;
	}

	interface Pack {
		id: string;
		name: string;
		author: string;
		version: string;
		mc_version: string;
		mod_loader: string;
		loader_version: string;
		updated_at: string;
		mods: ModItem[];
	}

	interface SearchModResult {
		id: string;
		slug: string;
		title: string;
		description: string;
		icon_url: string;
		author: string;
		downloads: number;
		categories: string[];
		platform: string;
	}

	const packId = page.params.id;

	let pack = $state<Pack | null>(null);
	let loading = $state(true);
	let saving = $state(false);

	// Mod filter in table
	let modFilter = $state('');

	// Search Drawer State
	let searchDrawerOpen = $state(false);
	let searchQuery = $state('');
	let searchPlatform = $state<'modrinth' | 'curseforge'>('modrinth');
	let searching = $state(false);
	let searchResults = $state<SearchModResult[]>([]);
	let searchError = $state('');

	// Deploy Dialog State
	let deployDialogOpen = $state(false);

	const MC_VERSIONS = [
		'1.21.4',
		'1.21.1',
		'1.20.6',
		'1.20.4',
		'1.20.1',
		'1.19.4',
		'1.19.2',
		'1.18.2',
		'1.16.5',
		'1.12.2'
	];

	async function loadPack() {
		loading = true;
		try {
			const res = await apiFetch(`/api/v1/packwiz/packs/${packId}`);
			if (!res.ok) throw new Error(`HTTP ${res.status}`);
			pack = await res.json();
		} catch (err) {
			console.error('Failed to load pack:', err);
			toast.error('Failed to load modpack project');
		} finally {
			loading = false;
		}
	}

	async function saveMetadata() {
		if (!pack) return;
		saving = true;
		try {
			const res = await apiFetch(`/api/v1/packwiz/packs/${packId}`, {
				method: 'PUT',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify(pack)
			});
			if (!res.ok) throw new Error(`HTTP ${res.status}`);
			toast.success('Modpack settings saved');
		} catch (err) {
			console.error('Failed to save pack:', err);
			toast.error('Failed to save modpack settings');
		} finally {
			saving = false;
		}
	}

	async function toggleSide(mod: ModItem) {
		if (!pack) return;
		const nextSide: Record<string, 'both' | 'client' | 'server'> = {
			both: 'server',
			server: 'client',
			client: 'both'
		};
		const newSide = nextSide[mod.side] || 'both';

		try {
			const res = await apiFetch(`/api/v1/packwiz/packs/${packId}/mods/${mod.slug}`, {
				method: 'PATCH',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ side: newSide, pinned: mod.pinned ?? false })
			});
			if (!res.ok) throw new Error(`HTTP ${res.status}`);
			mod.side = newSide;
			toast.success(`Updated ${mod.name} side to ${newSide.toUpperCase()}`);
		} catch (err) {
			console.error('Failed to update mod side:', err);
			toast.error('Failed to update mod side');
		}
	}

	async function togglePin(mod: ModItem) {
		if (!pack) return;
		const newPinned = !mod.pinned;
		try {
			const res = await apiFetch(`/api/v1/packwiz/packs/${packId}/mods/${mod.slug}`, {
				method: 'PATCH',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ side: mod.side, pinned: newPinned })
			});
			if (!res.ok) throw new Error(`HTTP ${res.status}`);
			mod.pinned = newPinned;
			toast.success(newPinned ? `Pinned ${mod.name}` : `Unpinned ${mod.name}`);
		} catch (err) {
			console.error('Failed to update mod pin:', err);
			toast.error('Failed to update mod');
		}
	}

	async function removeMod(mod: ModItem) {
		if (!pack) return;
		try {
			const res = await apiFetch(`/api/v1/packwiz/packs/${packId}/mods/${mod.slug}`, {
				method: 'DELETE'
			});
			if (!res.ok) throw new Error(`HTTP ${res.status}`);
			pack.mods = pack.mods.filter((m) => m.slug !== mod.slug);
			toast.success(`Removed ${mod.name} from modpack`);
		} catch (err) {
			console.error('Failed to delete mod:', err);
			toast.error('Failed to remove mod');
		}
	}

	async function searchMods() {
		if (!pack) return;
		searching = true;
		searchError = '';
		searchResults = [];

		try {
			const params = new URLSearchParams({
				query: searchQuery.trim(),
				platform: searchPlatform,
				loader: pack.mod_loader.toLowerCase(),
				mc_version: pack.mc_version
			});

			const res = await apiFetch(`/api/v1/servers/none/mods/search?${params.toString()}`);
			const data = await res.json();
			if (!res.ok) {
				searchError = data.error || 'Search request failed';
			} else {
				searchResults = data.results || [];
			}
		} catch (err: any) {
			console.error('Search failed:', err);
			searchError = 'Failed to fetch online mods';
		} finally {
			searching = false;
		}
	}

	async function addModToPack(item: SearchModResult, side: 'both' | 'client' | 'server' = 'both') {
		if (!pack) return;

		try {
			// Fetch compatible version first
			const vParams = new URLSearchParams({
				platform: item.platform,
				loader: pack.mod_loader.toLowerCase(),
				mc_version: pack.mc_version
			});
			const vRes = await apiFetch(`/api/v1/servers/none/mods/${item.slug || item.id}/versions?${vParams.toString()}`);
			if (!vRes.ok) throw new Error('No compatible versions found');
			const vData = await vRes.json();
			const versions = vData.versions || [];
			if (versions.length === 0) {
				toast.error(`No compatible versions of ${item.title} for MC ${pack.mc_version} on ${pack.mod_loader}`);
				return;
			}

			const selectedVer = versions[0];
			const newMod: ModItem = {
				slug: item.slug || item.id,
				name: item.title,
				file_name: selectedVer.file_name,
				side,
				platform: item.platform,
				project_id: item.id,
				version_id: selectedVer.id,
				download_url: selectedVer.download_url,
				file_size: selectedVer.file_size,
				pinned: false
			};

			const addRes = await apiFetch(`/api/v1/packwiz/packs/${packId}/mods`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify(newMod)
			});
			if (!addRes.ok) throw new Error(`HTTP ${addRes.status}`);

			toast.success(`Added ${item.title} (${side.toUpperCase()}) to pack!`);
			await loadPack();
		} catch (err: any) {
			console.error('Failed to add mod:', err);
			toast.error(err.message || 'Failed to add mod to pack');
		}
	}

	let filteredMods = $derived(
		(pack?.mods || []).filter((m) =>
			m.name.toLowerCase().includes(modFilter.toLowerCase()) ||
			m.file_name.toLowerCase().includes(modFilter.toLowerCase())
		)
	);

	function formatDownloads(count: number): string {
		if (!count) return '0';
		if (count >= 1_000_000) return `${(count / 1_000_000).toFixed(1)}M`;
		if (count >= 1_000) return `${(count / 1_000).toFixed(1)}k`;
		return count.toString();
	}

	onMount(() => {
		loadPack();
	});
</script>

<div class="h-full flex-1 space-y-6 bg-linear-to-br from-background to-muted/10 p-8 pt-6">
	<!-- Top Bar -->
	<div class="flex items-center justify-between border-b-2 border-border/50 pb-5">
		<div class="flex items-center gap-4">
			<Button variant="ghost" size="icon" onclick={() => goto('/modpacks/studio')} class="h-10 w-10">
				<ArrowLeft class="h-5 w-5" />
			</Button>

			{#if pack}
				<div class="space-y-0.5">
					<div class="flex items-center gap-3">
						<h2 class="text-3xl font-bold tracking-tight text-foreground">{pack.name}</h2>
						<Badge variant="outline" class="font-mono text-xs uppercase">
							{pack.mod_loader}
						</Badge>
						<Badge variant="secondary" class="font-mono text-xs">
							MC {pack.mc_version}
						</Badge>
					</div>
					<p class="text-xs text-muted-foreground">
						Packwiz Studio Project · v{pack.version} by {pack.author || 'Admin'}
					</p>
				</div>
			{/if}
		</div>

		<div class="flex items-center gap-2.5">
			{#if pack}
				<Button variant="outline" size="sm" onclick={() => (deployDialogOpen = true)}>
					<Rocket class="mr-2 h-4 w-4 text-primary" />
					Deploy to Server
				</Button>

				<a
					href={`/api/v1/packwiz/packs/${packId}/export/mrpack`}
					download
					class="inline-flex items-center justify-center rounded-md border border-input bg-background px-3 py-1.5 text-xs font-medium hover:bg-accent hover:text-accent-foreground shadow-xs transition-colors"
				>
					<Download class="mr-1.5 h-3.5 w-3.5" />
					.mrpack
				</a>

				<a
					href={`/api/v1/packwiz/packs/${packId}/export/curseforge`}
					download
					class="inline-flex items-center justify-center rounded-md border border-input bg-background px-3 py-1.5 text-xs font-medium hover:bg-accent hover:text-accent-foreground shadow-xs transition-colors"
				>
					<Download class="mr-1.5 h-3.5 w-3.5" />
					CurseForge .zip
				</a>

				<Button size="sm" onclick={saveMetadata} disabled={saving}>
					{#if saving}
						<Loader2 class="mr-2 h-4 w-4 animate-spin" />
						Saving...
					{:else}
						<Save class="mr-2 h-4 w-4" />
						Save
					{/if}
				</Button>
			{/if}
		</div>
	</div>

	{#if loading}
		<div class="flex flex-col items-center justify-center py-24 text-muted-foreground">
			<Loader2 class="h-10 w-10 animate-spin text-primary" />
			<p class="mt-4 text-sm">Loading studio workspace...</p>
		</div>
	{:else if pack}
		<!-- Main Studio Grid -->
		<div class="grid grid-cols-1 lg:grid-cols-4 gap-6">
			<!-- Metadata Sidebar Card -->
			<Card class="lg:col-span-1 h-fit shadow-xs">
				<CardHeader class="pb-3">
					<CardTitle class="text-base font-semibold">Modpack Settings</CardTitle>
					<CardDescription class="text-xs">Configure core target versions</CardDescription>
				</CardHeader>
				<CardContent class="space-y-4">
					<div class="space-y-1.5">
						<Label for="metaName" class="text-xs">Pack Name</Label>
						<Input id="metaName" bind:value={pack.name} class="h-8 text-sm" />
					</div>

					<div class="space-y-1.5">
						<Label for="metaAuthor" class="text-xs">Author</Label>
						<Input id="metaAuthor" bind:value={pack.author} class="h-8 text-sm" />
					</div>

					<div class="space-y-1.5">
						<Label for="metaVer" class="text-xs">Pack Version</Label>
						<Input id="metaVer" bind:value={pack.version} class="h-8 text-sm" />
					</div>

					<div class="space-y-1.5">
						<Label for="metaMcVer" class="text-xs">Minecraft Version</Label>
						<Select type="single" bind:value={pack.mc_version}>
							<SelectTrigger id="metaMcVer" class="h-8 text-xs">
								<span>{pack.mc_version}</span>
							</SelectTrigger>
							<SelectContent>
								{#each MC_VERSIONS as v}
									<SelectItem value={v}>{v}</SelectItem>
								{/each}
							</SelectContent>
						</Select>
					</div>

					<div class="space-y-1.5">
						<Label for="metaLoader" class="text-xs">Mod Loader</Label>
						<Select type="single" bind:value={pack.mod_loader}>
							<SelectTrigger id="metaLoader" class="h-8 text-xs">
								<span class="capitalize">{pack.mod_loader}</span>
							</SelectTrigger>
							<SelectContent>
								<SelectItem value="fabric">Fabric</SelectItem>
								<SelectItem value="neoforge">NeoForge</SelectItem>
								<SelectItem value="forge">Forge</SelectItem>
								<SelectItem value="quilt">Quilt</SelectItem>
							</SelectContent>
						</Select>
					</div>

					<div class="space-y-1.5">
						<Label for="metaLoaderVer" class="text-xs">Loader Version</Label>
						<Input id="metaLoaderVer" bind:value={pack.loader_version} class="h-8 text-sm" />
					</div>

					<Button size="sm" class="w-full mt-2" onclick={saveMetadata} disabled={saving}>
						{#if saving}
							<Loader2 class="mr-2 h-4 w-4 animate-spin" />
							Saving...
						{:else}
							<Save class="mr-2 h-4 w-4" />
							Save Settings
						{/if}
					</Button>
				</CardContent>
			</Card>

			<!-- Mod Management Table -->
			<Card class="lg:col-span-3 shadow-xs">
				<CardHeader class="pb-3">
					<div class="flex items-center justify-between gap-4">
						<div>
							<div class="flex items-center gap-2">
								<CardTitle class="text-lg font-bold">Mods in Pack</CardTitle>
								<Badge variant="secondary" class="text-xs">
									{pack.mods.length}
								</Badge>
							</div>
							<CardDescription class="text-xs">Manage sides, versions, and dependencies</CardDescription>
						</div>

						<div class="flex items-center gap-2">
							<div class="relative w-48 sm:w-64">
								<Input
									placeholder="Filter mods..."
									bind:value={modFilter}
									class="h-8 pl-8 text-xs"
								/>
								<Search class="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
							</div>

							<Button size="sm" onclick={() => (searchDrawerOpen = true)}>
								<Plus class="mr-1.5 h-4 w-4" />
								Add Mods
							</Button>
						</div>
					</div>
				</CardHeader>

				<CardContent class="p-0">
					{#if pack.mods.length === 0}
						<div class="flex flex-col items-center justify-center py-20 text-center text-muted-foreground">
							<Package class="h-10 w-10 stroke-[1.5]" />
							<p class="mt-3 text-base font-semibold">No mods added to pack yet</p>
							<p class="text-xs">Search Modrinth and CurseForge to populate this modpack.</p>
							<Button size="sm" onclick={() => (searchDrawerOpen = true)} class="mt-4">
								<Plus class="mr-1.5 h-4 w-4" />
								Browse & Add Mods
							</Button>
						</div>
					{:else}
						<div class="overflow-x-auto">
							<table class="w-full text-left text-xs">
								<thead class="border-y bg-muted/30 text-muted-foreground font-semibold">
									<tr>
										<th class="py-2.5 px-4">Mod</th>
										<th class="py-2.5 px-4">File Name</th>
										<th class="py-2.5 px-4">Platform</th>
										<th class="py-2.5 px-4">Side</th>
										<th class="py-2.5 px-4">Pin</th>
										<th class="py-2.5 px-4 text-right">Actions</th>
									</tr>
								</thead>
								<tbody class="divide-y divide-border/60">
									{#each filteredMods as mod (mod.slug)}
										<tr class="hover:bg-muted/20 transition-colors">
											<td class="py-3 px-4 font-semibold text-foreground">
												<div class="flex items-center gap-2">
													<Package class="h-4 w-4 text-primary/70 flex-shrink-0" />
													<span>{mod.name}</span>
												</div>
											</td>
											<td class="py-3 px-4 font-mono text-[11px] text-muted-foreground max-w-[200px] truncate">
												{mod.file_name}
											</td>
											<td class="py-3 px-4">
												<Badge variant="outline" class="text-[10px] uppercase font-mono">
													{mod.platform}
												</Badge>
											</td>
											<td class="py-3 px-4">
												<button
													type="button"
													onclick={() => toggleSide(mod)}
													class="inline-flex items-center gap-1 rounded px-2 py-0.5 text-[11px] font-semibold transition-colors cursor-pointer
														{mod.side === 'both' ? 'bg-primary/15 text-primary border border-primary/30' : ''}
														{mod.side === 'server' ? 'bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border border-emerald-500/30' : ''}
														{mod.side === 'client' ? 'bg-amber-500/15 text-amber-600 dark:text-amber-400 border border-amber-500/30' : ''}"
													title="Click to toggle side (Both -> Server -> Client)"
												>
													{mod.side.toUpperCase()}
												</button>
											</td>
											<td class="py-3 px-4">
												<button
													type="button"
													onclick={() => togglePin(mod)}
													class="p-1 rounded text-muted-foreground hover:text-foreground transition-colors"
													title={mod.pinned ? 'Version pinned' : 'Click to pin version'}
												>
													{#if mod.pinned}
														<Pin class="h-3.5 w-3.5 text-primary" />
													{:else}
														<PinOff class="h-3.5 w-3.5 opacity-40 hover:opacity-100" />
													{/if}
												</button>
											</td>
											<td class="py-3 px-4 text-right">
												<Button
													variant="ghost"
													size="icon"
													class="h-7 w-7 text-destructive hover:bg-destructive/10"
													onclick={() => removeMod(mod)}
													title="Remove mod"
												>
													<Trash2 class="h-3.5 w-3.5" />
												</Button>
											</td>
										</tr>
									{/each}
								</tbody>
							</table>
						</div>
					{/if}
				</CardContent>
			</Card>
		</div>
	{/if}
</div>

<!-- Mod Search & Add Drawer / Dialog -->
<DialogPrimitive.Root bind:open={searchDrawerOpen}>
	<DialogContent class="max-h-[90vh] !max-w-3xl flex flex-col p-6">
		<DialogHeader>
			<div class="flex items-center justify-between">
				<DialogTitle class="flex items-center gap-2 text-xl font-bold">
					<Sparkles class="h-5 w-5 text-primary" />
					Search & Add Mods
				</DialogTitle>
				{#if pack}
					<div class="flex items-center gap-2">
						<Badge variant="outline" class="text-xs uppercase">{pack.mod_loader}</Badge>
						<Badge variant="secondary" class="text-xs">MC {pack.mc_version}</Badge>
					</div>
				{/if}
			</div>
			<DialogDescription>
				Search mod catalogs and add mods directly to this Packwiz modpack project.
			</DialogDescription>
		</DialogHeader>

		<!-- Search Bar -->
		<div class="mt-4 flex items-center gap-2">
			<Tabs
				value={searchPlatform}
				onValueChange={(val) => {
					searchPlatform = val as 'modrinth' | 'curseforge';
					searchMods();
				}}
			>
				<TabsList>
					<TabsTrigger value="modrinth" class="px-3 text-xs">Modrinth</TabsTrigger>
					<TabsTrigger value="curseforge" class="px-3 text-xs">CurseForge</TabsTrigger>
				</TabsList>
			</Tabs>

			<div class="relative flex-1">
				<Input
					placeholder="Search online mods..."
					bind:value={searchQuery}
					onkeydown={(e) => e.key === 'Enter' && searchMods()}
					class="pl-8"
				/>
				<Search class="absolute left-2.5 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
			</div>

			<Button onclick={searchMods} disabled={searching}>
				{#if searching}
					<Loader2 class="h-4 w-4 animate-spin" />
				{:else}
					Search
				{/if}
			</Button>
		</div>

		<!-- Results List -->
		<div class="mt-4 flex-1 overflow-y-auto pr-1 space-y-3 min-h-[300px] max-h-[460px]">
			{#if searchError}
				<Alert variant="destructive">
					<AlertTriangle class="h-4 w-4" />
					<AlertTitle>Notice</AlertTitle>
					<AlertDescription>{searchError}</AlertDescription>
				</Alert>
			{:else if searching}
				<div class="flex flex-col items-center justify-center py-16 text-muted-foreground">
					<Loader2 class="h-8 w-8 animate-spin text-primary" />
					<p class="mt-3 text-xs">Searching mods...</p>
				</div>
			{:else if searchResults.length === 0}
				<div class="flex flex-col items-center justify-center py-16 text-muted-foreground">
					<Package class="h-10 w-10 stroke-[1.5]" />
					<p class="mt-3 text-sm font-medium">Type a query to search mods</p>
				</div>
			{:else}
				<div class="space-y-2.5">
					{#each searchResults as item (item.id)}
						<div class="flex items-start justify-between gap-3 p-3 rounded-lg border hover:border-primary/40 bg-card transition-colors">
							<div class="flex items-start gap-3 min-w-0 flex-1">
								{#if item.icon_url}
									<img src={item.icon_url} alt={item.title} class="h-10 w-10 rounded object-contain bg-muted p-1 flex-shrink-0" />
								{:else}
									<div class="h-10 w-10 rounded bg-muted flex items-center justify-center flex-shrink-0">
										<Package class="h-5 w-5 text-muted-foreground" />
									</div>
								{/if}

								<div class="space-y-0.5 min-w-0 flex-1">
									<div class="flex items-center gap-2">
										<span class="font-semibold text-sm truncate">{item.title}</span>
										<span class="text-[11px] text-muted-foreground">by {item.author}</span>
									</div>
									<p class="text-xs text-muted-foreground line-clamp-1">{item.description}</p>
									<div class="flex items-center gap-1.5 pt-0.5">
										<Badge variant="secondary" class="text-[10px] h-4 px-1">
											<Download class="mr-0.5 h-2.5 w-2.5" />
											{formatDownloads(item.downloads)}
										</Badge>
										{#each (item.categories || []).slice(0, 2) as cat}
											<Badge variant="outline" class="text-[10px] h-4 px-1 capitalize">{cat}</Badge>
										{/each}
									</div>
								</div>
							</div>

							<!-- Add with Side Select -->
							<div class="flex items-center gap-1.5 flex-shrink-0">
								<Button size="sm" variant="outline" onclick={() => addModToPack(item, 'client')} title="Add as client-only">
									+ Client
								</Button>
								<Button size="sm" variant="outline" onclick={() => addModToPack(item, 'server')} title="Add as server-only">
									+ Server
								</Button>
								<Button size="sm" onclick={() => addModToPack(item, 'both')} title="Add for both client & server">
									<Plus class="mr-1 h-3.5 w-3.5" />
									Add (Both)
								</Button>
							</div>
						</div>
					{/each}
				</div>
			{/if}
		</div>
	</DialogContent>
</DialogPrimitive.Root>

<!-- Deploy Dialog -->
{#if pack}
	<ModpackDeployDialog
		bind:open={deployDialogOpen}
		packId={pack.id}
		packName={pack.name}
		mcVersion={pack.mc_version}
		modLoader={pack.mod_loader}
	/>
{/if}
