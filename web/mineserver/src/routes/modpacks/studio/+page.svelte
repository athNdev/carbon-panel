<script lang="ts">
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
	import { Dialog as DialogPrimitive } from 'bits-ui';
	import {
		DialogContent,
		DialogDescription,
		DialogFooter,
		DialogHeader,
		DialogTitle
	} from '$lib/components/ui/dialog';
	import {
		Package,
		Plus,
		ArrowLeft,
		Blocks,
		Boxes,
		PackagePlus,
		Download,
		Rocket,
		Trash2,
		Copy,
		UploadCloud,
		Search,
		ExternalLink,
		Loader2,
		Calendar
	} from '@lucide/svelte';
	import { toast } from 'svelte-sonner';
	import { apiFetch } from '$lib/api/fetch';
	import ModpackDeployDialog from '$lib/components/modpack-deploy-dialog.svelte';

	interface PackSummary {
		id: string;
		name: string;
		author: string;
		version: string;
		mc_version: string;
		mod_loader: string;
		loader_version: string;
		mod_count: number;
		updated_at: string;
	}

	let packs = $state<PackSummary[]>([]);
	let loading = $state(true);
	let filterQuery = $state('');

	// Create Dialog State
	let createDialogOpen = $state(false);
	let creating = $state(false);
	let newName = $state('My Custom Pack');
	let newAuthor = $state('Admin');
	let newVersion = $state('1.0.0');
	let newMcVersion = $state('1.20.1');
	let newLoader = $state('fabric');
	let newLoaderVersion = $state('latest');
	let availableLoaderVersions = $state<string[]>(['latest']);
	let loadingLoaderVersions = $state(false);
	let exportingPack = $state<string | null>(null);

	// Import Dialog State
	let importDialogOpen = $state(false);
	let importing = $state(false);
	let importFile = $state<File | null>(null);
	let importFormat = $state<'auto' | 'mrpack' | 'curseforge' | 'packwiz'>('auto');
	let importName = $state('');

	// Deploy Dialog State
	let deployDialogOpen = $state(false);
	let deployPack = $state<PackSummary | null>(null);

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

	async function fetchLoaderVersions(loader: string, mcVer: string) {
		loadingLoaderVersions = true;
		try {
			const res = await apiFetch(`/api/v1/packwiz/loaders/${loader.toLowerCase()}/versions?game_version=${mcVer || ''}`);
			if (res.ok) {
				const data = await res.json();
				if (data.versions && data.versions.length > 0) {
					availableLoaderVersions = data.versions;
					if (!availableLoaderVersions.includes(newLoaderVersion)) {
						newLoaderVersion = 'latest';
					}
					return;
				}
			}
		} catch (err) {
			console.error('Failed to load loader versions:', err);
		} finally {
			loadingLoaderVersions = false;
		}
		availableLoaderVersions = ['latest'];
		newLoaderVersion = 'latest';
	}

	$effect(() => {
		if (newLoader && newMcVersion) {
			fetchLoaderVersions(newLoader, newMcVersion);
		}
	});

	async function exportPack(packId: string, format: 'mrpack' | 'curseforge' | 'packwiz', packName: string) {
		exportingPack = `${packId}-${format}`;
		try {
			const res = await apiFetch(`/api/v1/packwiz/packs/${packId}/export/${format}`);
			if (!res.ok) {
				const txt = await res.text();
				throw new Error(txt || `HTTP ${res.status}`);
			}
			const blob = await res.blob();
			const url = window.URL.createObjectURL(blob);
			const a = document.createElement('a');
			a.href = url;
			let ext = '.zip';
			if (format === 'mrpack') ext = '.mrpack';
			else if (format === 'packwiz') ext = '.packwiz.zip';
			a.download = `${packName}${ext}`;
			document.body.appendChild(a);
			a.click();
			window.URL.revokeObjectURL(url);
			document.body.removeChild(a);
			toast.success(`Exported ${format === 'mrpack' ? '.mrpack' : format === 'packwiz' ? 'Packwiz .zip' : 'CurseForge .zip'}`);
		} catch (err: any) {
			console.error('Failed to export modpack:', err);
			toast.error(`Export failed: ${err.message || 'Unauthorized or server error'}`);
		} finally {
			exportingPack = null;
		}
	}

	async function loadPacks() {
		loading = true;
		try {
			const res = await apiFetch('/api/v1/packwiz/packs');
			if (!res.ok) throw new Error(`HTTP ${res.status}`);
			const data = await res.json();
			packs = data.packs || [];
		} catch (err) {
			console.error('Failed to load packwiz projects:', err);
			toast.error('Failed to load custom modpack projects');
		} finally {
			loading = false;
		}
	}

	async function handleCreatePack() {
		if (!newName.trim()) {
			toast.error('Please enter a pack name');
			return;
		}

		creating = true;
		try {
			const res = await apiFetch('/api/v1/packwiz/packs', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({
					name: newName.trim(),
					author: newAuthor.trim(),
					version: newVersion.trim(),
					mc_version: newMcVersion,
					mod_loader: newLoader,
					loader_version: newLoaderVersion.trim() || 'latest'
				})
			});

			if (!res.ok) throw new Error(`HTTP ${res.status}`);
			const created = await res.json();
			toast.success(`Created modpack project "${created.name}"`);
			createDialogOpen = false;
			goto(`/modpacks/studio/${created.id}`);
		} catch (err: any) {
			console.error('Failed to create pack:', err);
			toast.error(err.message || 'Failed to create pack');
		} finally {
			creating = false;
		}
	}

	async function handleImportPack() {
		if (!importFile) {
			toast.error('Please choose a file to import');
			return;
		}

		importing = true;
		try {
			const fd = new FormData();
			fd.append('file', importFile);
			if (importFormat !== 'auto') {
				fd.append('format', importFormat);
			}
			if (importName.trim()) {
				fd.append('name', importName.trim());
			}

			const res = await apiFetch('/api/v1/packwiz/packs/import', {
				method: 'POST',
				body: fd
			});

			if (!res.ok) {
				const txt = await res.text();
				throw new Error(txt || `HTTP ${res.status}`);
			}

			const imported = await res.json();
			toast.success(`Successfully imported "${imported.name}"!`);
			importDialogOpen = false;
			importFile = null;
			importName = '';
			await loadPacks();
			goto(`/modpacks/studio/${imported.id}`);
		} catch (err: any) {
			console.error('Import failed:', err);
			toast.error(err.message || 'Failed to import modpack');
		} finally {
			importing = false;
		}
	}

	async function clonePack(pack: PackSummary) {
		try {
			const res = await apiFetch(`/api/v1/packwiz/packs/${pack.id}/clone`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ name: `Copy of ${pack.name}` })
			});
			if (!res.ok) throw new Error(`HTTP ${res.status}`);
			const cloned = await res.json();
			toast.success(`Duplicated "${pack.name}"`);
			await loadPacks();
			goto(`/modpacks/studio/${cloned.id}`);
		} catch (err: any) {
			console.error('Clone failed:', err);
			toast.error(err.message || 'Failed to duplicate pack');
		}
	}

	async function deletePack(pack: PackSummary) {
		if (!confirm(`Are you sure you want to delete "${pack.name}"? This cannot be undone.`)) {
			return;
		}

		try {
			const res = await apiFetch(`/api/v1/packwiz/packs/${pack.id}`, { method: 'DELETE' });
			if (!res.ok) throw new Error(`HTTP ${res.status}`);
			toast.success(`Deleted modpack "${pack.name}"`);
			packs = packs.filter((p) => p.id !== pack.id);
		} catch (err: any) {
			console.error('Failed to delete pack:', err);
			toast.error(err.message || 'Failed to delete modpack');
		}
	}

	let filteredPacks = $derived(
		packs.filter((p) => {
			const q = filterQuery.toLowerCase().trim();
			if (!q) return true;
			return (
				p.name.toLowerCase().includes(q) ||
				p.mc_version.toLowerCase().includes(q) ||
				p.mod_loader.toLowerCase().includes(q) ||
				p.author.toLowerCase().includes(q)
			);
		})
	);

	onMount(() => {
		loadPacks();
	});
</script>

<div class="h-full flex-1 space-y-6 bg-linear-to-br from-background to-muted/20 p-8 pt-6">
	<!-- Top Bar -->
	<div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4 border-b-2 border-border/50 pb-5">
		<div class="flex items-center gap-4">
			<Button variant="ghost" size="icon" onclick={() => goto('/modpacks')} class="h-10 w-10">
				<ArrowLeft class="h-5 w-5" />
			</Button>

			<div class="space-y-0.5">
				<div class="flex items-center gap-3">
					<h2 class="text-3xl font-bold tracking-tight text-foreground">Modpack Studio</h2>
					<Badge variant="outline" class="font-mono text-xs">PACKWIZ ENGINE</Badge>
				</div>
				<p class="text-xs text-muted-foreground">
					Visual Packwiz workspace: create, import (.mrpack, CurseForge, Packwiz), customize, and deploy modpacks.
				</p>
			</div>
		</div>

		<div class="flex items-center gap-3">
			<Button variant="outline" onclick={() => (importDialogOpen = true)}>
				<UploadCloud class="mr-2 h-4 w-4 text-primary" />
				Import Modpack
			</Button>

			<Button onclick={() => (createDialogOpen = true)}>
				<Plus class="mr-2 h-4 w-4" />
				Create Modpack
			</Button>
		</div>
	</div>

	<!-- Search & Summary Bar -->
	<div class="flex items-center justify-between gap-4">
		<div class="relative w-72">
			<Input
				placeholder="Search modpack projects..."
				bind:value={filterQuery}
				class="h-9 pl-9 text-xs"
			/>
			<Search class="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
		</div>
		<p class="text-xs text-muted-foreground">
			Showing {filteredPacks.length} of {packs.length} projects
		</p>
	</div>

	<!-- Projects Grid -->
	{#if loading}
		<div class="flex flex-col items-center justify-center py-20 text-muted-foreground">
			<Loader2 class="h-8 w-8 animate-spin text-primary" />
			<p class="mt-3 text-sm">Loading modpack projects...</p>
		</div>
	{:else if filteredPacks.length === 0}
		<Card class="border-dashed py-16 text-center">
			<CardContent class="flex flex-col items-center justify-center space-y-4">
				<div class="rounded-full bg-primary/10 p-4">
					<Boxes class="h-10 w-10 text-primary" />
				</div>
				<div class="space-y-1">
					<h3 class="text-lg font-semibold">No Modpack Projects Found</h3>
					<p class="text-xs text-muted-foreground max-w-sm">
						Get started by creating a new custom modpack from scratch or importing an existing Modrinth (.mrpack), CurseForge (.zip), or Packwiz archive.
					</p>
				</div>
				<div class="flex items-center gap-3 pt-2">
					<Button variant="outline" size="sm" onclick={() => (importDialogOpen = true)}>
						<UploadCloud class="mr-2 h-4 w-4" />
						Import Modpack
					</Button>
					<Button size="sm" onclick={() => (createDialogOpen = true)}>
						<Plus class="mr-2 h-4 w-4" />
						Create Project
					</Button>
				</div>
			</CardContent>
		</Card>
	{:else}
		<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
			{#each filteredPacks as pack (pack.id)}
				<Card class="group hover:border-primary/50 transition-all duration-200 flex flex-col justify-between shadow-xs">
					<CardHeader class="pb-3">
						<div class="flex items-start justify-between gap-3">
							<div class="space-y-1 min-w-0">
								<CardTitle class="text-lg font-bold truncate group-hover:text-primary transition-colors">
									<a href={`/modpacks/studio/${pack.id}`}>{pack.name}</a>
								</CardTitle>
								<CardDescription class="text-xs truncate">
									v{pack.version}  by {pack.author || 'Admin'}
								</CardDescription>
							</div>

							<div class="flex items-center gap-1.5 flex-shrink-0">
								<Badge variant="outline" class="font-mono text-[10px] uppercase">
									{pack.mod_loader}
								</Badge>
								<Badge variant="secondary" class="font-mono text-[10px]">
									MC {pack.mc_version}
								</Badge>
							</div>
						</div>
					</CardHeader>

					<CardContent class="py-2 text-xs text-muted-foreground">
						<div class="flex items-center justify-between border-t border-b py-2 my-1">
							<span class="flex items-center gap-1.5">
								<Package class="h-3.5 w-3.5 text-primary" />
								<strong>{pack.mod_count}</strong> {pack.mod_count === 1 ? 'mod' : 'mods'}
							</span>
							<span class="flex items-center gap-1 text-[11px]">
								<Calendar class="h-3 w-3" />
								{new Date(pack.updated_at).toLocaleDateString()}
							</span>
						</div>
					</CardContent>

					<div class="p-4 pt-2 border-t flex items-center justify-between gap-2">
						<div class="flex items-center gap-1">
							<Button
								variant="ghost"
								size="icon"
								class="h-8 w-8 text-muted-foreground hover:text-foreground"
								onclick={() => clonePack(pack)}
								title="Duplicate / Clone modpack"
							>
								<Copy class="h-3.5 w-3.5" />
							</Button>
							<Button
								variant="ghost"
								size="icon"
								class="h-8 w-8 text-destructive hover:bg-destructive/10"
								onclick={() => deletePack(pack)}
								title="Delete modpack"
							>
								<Trash2 class="h-3.5 w-3.5" />
							</Button>
						</div>

						<div class="flex items-center gap-1.5">
							<!-- Export Options -->
							<Button
								variant="outline"
								size="sm"
								class="h-8 px-2 text-[11px]"
								onclick={() => exportPack(pack.id, 'packwiz', pack.name)}
								disabled={exportingPack === `${pack.id}-packwiz`}
								title="Export Native Packwiz .zip"
							>
								Packwiz
							</Button>
							<Button
								variant="outline"
								size="sm"
								class="h-8 px-2 text-[11px]"
								onclick={() => exportPack(pack.id, 'mrpack', pack.name)}
								disabled={exportingPack === `${pack.id}-mrpack`}
								title="Export Modrinth .mrpack"
							>
								.mrpack
							</Button>

							<Button
								size="sm"
								class="h-8 text-xs"
								onclick={() => goto(`/modpacks/studio/${pack.id}`)}
							>
								Open Studio
							</Button>
						</div>
					</div>
				</Card>
			{/each}
		</div>
	{/if}
</div>

<!-- Create Dialog -->
<DialogPrimitive.Root bind:open={createDialogOpen}>
	<DialogContent class="sm:max-w-[480px]">
		<DialogHeader>
			<DialogTitle class="flex items-center gap-2">
				<PackagePlus class="h-5 w-5 text-primary" />
				Create New Modpack
			</DialogTitle>
			<DialogDescription>
				Initialize a new custom modpack managed with Packwiz.
			</DialogDescription>
		</DialogHeader>

		<div class="space-y-4 py-3">
			<div class="space-y-2">
				<Label for="packName">Modpack Name</Label>
				<Input id="packName" bind:value={newName} placeholder="e.g. Odyssey SMP" />
			</div>

			<div class="grid grid-cols-2 gap-4">
				<div class="space-y-2">
					<Label for="packAuthor">Author</Label>
					<Input id="packAuthor" bind:value={newAuthor} placeholder="Admin" />
				</div>
				<div class="space-y-2">
					<Label for="packVer">Initial Version</Label>
					<Input id="packVer" bind:value={newVersion} placeholder="1.0.0" />
				</div>
			</div>

			<div class="grid grid-cols-2 gap-4">
				<div class="space-y-2">
					<Label for="mcVer">Minecraft Version</Label>
					<Select type="single" bind:value={newMcVersion}>
						<SelectTrigger id="mcVer">
							<span>{newMcVersion}</span>
						</SelectTrigger>
						<SelectContent>
							{#each MC_VERSIONS as v}
								<SelectItem value={v}>{v}</SelectItem>
							{/each}
						</SelectContent>
					</Select>
				</div>

				<div class="space-y-2">
					<Label for="loader">Mod Loader</Label>
					<Select type="single" bind:value={newLoader}>
						<SelectTrigger id="loader">
							<span class="capitalize">{newLoader}</span>
						</SelectTrigger>
						<SelectContent>
							<SelectItem value="fabric">Fabric</SelectItem>
							<SelectItem value="neoforge">NeoForge</SelectItem>
							<SelectItem value="forge">Forge</SelectItem>
							<SelectItem value="quilt">Quilt</SelectItem>
						</SelectContent>
					</Select>
				</div>
			</div>

			<div class="space-y-2">
				<div class="flex items-center justify-between">
					<Label for="loaderVer">Loader Version</Label>
					{#if loadingLoaderVersions}
						<span class="inline-flex items-center text-[10px] text-muted-foreground">
							<Loader2 class="h-2.5 w-2.5 mr-1 animate-spin" />
							fetching versions...
						</span>
					{/if}
				</div>
				<Select type="single" bind:value={newLoaderVersion}>
					<SelectTrigger id="loaderVer">
						<span>{newLoaderVersion}</span>
					</SelectTrigger>
					<SelectContent class="max-h-56 overflow-y-auto">
						{#each availableLoaderVersions as v}
							<SelectItem value={v}>{v}</SelectItem>
						{/each}
					</SelectContent>
				</Select>
			</div>
		</div>

		<DialogFooter>
			<Button variant="outline" onclick={() => (createDialogOpen = false)} disabled={creating}>
				Cancel
			</Button>
			<Button onclick={handleCreatePack} disabled={creating}>
				{#if creating}
					<Loader2 class="mr-2 h-4 w-4 animate-spin" />
					Creating...
				{:else}
					Create Project
				{/if}
			</Button>
		</DialogFooter>
	</DialogContent>
</DialogPrimitive.Root>

<!-- Import Dialog -->
<DialogPrimitive.Root bind:open={importDialogOpen}>
	<DialogContent class="sm:max-w-[500px]">
		<DialogHeader>
			<DialogTitle class="flex items-center gap-2">
				<UploadCloud class="h-5 w-5 text-primary" />
				Import Existing Modpack
			</DialogTitle>
			<DialogDescription>
				Import a modpack archive from Modrinth (.mrpack), CurseForge (.zip), or a native Packwiz archive (.zip).
			</DialogDescription>
		</DialogHeader>

		<div class="space-y-4 py-3">
			<div class="space-y-2">
				<Label for="importFile">Modpack Archive File</Label>
				<Input
					id="importFile"
					type="file"
					accept=".mrpack,.zip"
					onchange={(e) => {
						const target = e.target as HTMLInputElement;
						if (target.files && target.files.length > 0) {
							importFile = target.files[0];
							if (!importName && importFile.name) {
								importName = importFile.name.replace(/\.(mrpack|zip)$/i, '');
							}
						}
					}}
				/>
				<p class="text-[11px] text-muted-foreground">
					Supported formats: Modrinth (.mrpack), CurseForge manifest (.zip), or Packwiz archive (.zip).
				</p>
			</div>

			<div class="space-y-2">
				<Label for="importFormat">Format Detection</Label>
				<Select type="single" bind:value={importFormat}>
					<SelectTrigger id="importFormat">
						<span class="capitalize">{importFormat === 'auto' ? 'Auto-detect format' : importFormat}</span>
					</SelectTrigger>
					<SelectContent>
						<SelectItem value="auto">Auto-detect from file</SelectItem>
						<SelectItem value="mrpack">Modrinth (.mrpack)</SelectItem>
						<SelectItem value="curseforge">CurseForge (.zip manifest)</SelectItem>
						<SelectItem value="packwiz">Native Packwiz (.zip)</SelectItem>
					</SelectContent>
				</Select>
			</div>

			<div class="space-y-2">
				<Label for="importName">Project Name Override (Optional)</Label>
				<Input id="importName" bind:value={importName} placeholder="Leave blank to use metadata name" />
			</div>
		</div>

		<DialogFooter>
			<Button variant="outline" onclick={() => (importDialogOpen = false)} disabled={importing}>
				Cancel
			</Button>
			<Button onclick={handleImportPack} disabled={importing || !importFile}>
				{#if importing}
					<Loader2 class="mr-2 h-4 w-4 animate-spin" />
					Importing...
				{:else}
					Import Modpack
				{/if}
			</Button>
		</DialogFooter>
	</DialogContent>
</DialogPrimitive.Root>
