<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import {
		CarbonButton,
		CarbonTile,
		CarbonTag,
		CarbonSearch,
		CarbonSelect,
		CarbonModal,
		CarbonTextInput
	} from '$lib/components/carbon';
	import {
		Package,
		Plus,
		ArrowLeft,
		Boxes,
		Trash2,
		Copy,
		UploadCloud,
		Loader2,
		Calendar
	} from '@lucide/svelte';
	import { toast } from 'svelte-sonner';
	import { apiFetch } from '$lib/api/fetch';

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

<svelte:head>
	<title>Modpack Studio - Carbon Panel</title>
</svelte:head>

<div class="h-full flex-1 space-y-6 font-sans text-[#f4f4f4] rounded-none">
	<!-- Top Bar -->
	<div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4 border-b border-[#393939] pb-6 rounded-none">
		<div class="flex items-center gap-4">
			<CarbonButton
				kind="ghost"
				size="md"
				iconOnly
				onclick={() => goto('/modpacks')}
				class="rounded-none text-[#c6c6c6] hover:text-white"
				title="Back to Modpacks"
			>
				<ArrowLeft class="h-5 w-5" />
			</CarbonButton>

			<div class="space-y-0.5">
				<div class="flex items-center gap-3">
					<h1 class="text-3xl font-semibold tracking-tight text-white">Modpack Studio</h1>
					<CarbonTag type="cyan" size="sm">PACKWIZ ENGINE</CarbonTag>
				</div>
				<p class="text-xs text-[#a8a8a8]">
					Visual workspace for custom modpacks: create, import (.mrpack, CurseForge, Packwiz), edit overrides, and deploy.
				</p>
			</div>
		</div>

		<div class="flex items-center gap-3">
			<CarbonButton
				kind="secondary"
				class="rounded-none"
				onclick={() => (importDialogOpen = true)}
			>
				<UploadCloud class="mr-2 h-4 w-4 text-[#0f62fe]" />
				Import Modpack
			</CarbonButton>

			<CarbonButton
				kind="primary"
				class="rounded-none"
				onclick={() => (createDialogOpen = true)}
			>
				<Plus class="mr-2 h-4 w-4" />
				Create Modpack
			</CarbonButton>
		</div>
	</div>

	<!-- Search & Summary Bar -->
	<div class="flex items-center justify-between gap-4">
		<div class="w-72">
			<CarbonSearch
				placeholder="Search modpack projects..."
				bind:value={filterQuery}
				size="sm"
			/>
		</div>
		<p class="text-xs text-[#8d8d8d] font-mono">
			SHOWING {filteredPacks.length} OF {packs.length} PROJECTS
		</p>
	</div>

	<!-- Projects Grid -->
	{#if loading}
		<div class="flex flex-col items-center justify-center py-24 text-[#8d8d8d]">
			<Loader2 class="h-8 w-8 animate-spin text-[#0f62fe]" />
			<p class="mt-3 text-xs">Loading modpack projects...</p>
		</div>
	{:else if filteredPacks.length === 0}
		<div class="border border-dashed border-[#393939] bg-[#262626] p-16 text-center space-y-4 rounded-none">
			<div class="mx-auto h-12 w-12 bg-[#161616] border border-[#393939] flex items-center justify-center text-[#0f62fe] rounded-none">
				<Boxes class="h-6 w-6" />
			</div>
			<div class="space-y-1">
				<h3 class="text-base font-semibold text-white">No Modpack Projects Found</h3>
				<p class="text-xs text-[#a8a8a8] max-w-sm mx-auto">
					Initialize a new custom modpack or import an existing Modrinth (.mrpack), CurseForge (.zip), or Packwiz archive.
				</p>
			</div>
			<div class="flex items-center justify-center gap-3 pt-2">
				<CarbonButton
					kind="secondary"
					size="sm"
					class="rounded-none"
					onclick={() => (importDialogOpen = true)}
				>
					<UploadCloud class="mr-2 h-4 w-4" />
					Import Modpack
				</CarbonButton>
				<CarbonButton
					kind="primary"
					size="sm"
					class="rounded-none"
					onclick={() => (createDialogOpen = true)}
				>
					<Plus class="mr-2 h-4 w-4" />
					Create Project
				</CarbonButton>
			</div>
		</div>
	{:else}
		<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
			{#each filteredPacks as pack (pack.id)}
				<CarbonTile class="flex flex-col justify-between p-5 rounded-none border-[#393939] hover:border-[#525252] transition-colors group">
					<div class="space-y-3">
						<!-- Tile Header -->
						<div class="flex items-start justify-between gap-3">
							<div class="space-y-1 min-w-0 flex-1">
								<h3 class="text-lg font-semibold truncate">
									<a href={`/modpacks/studio/${pack.id}`} class="text-white hover:text-[#0f62fe] transition-colors">
										{pack.name}
									</a>
								</h3>
								<p class="text-xs text-[#a8a8a8] truncate">
									v{pack.version} · by {pack.author || 'Admin'}
								</p>
							</div>

							<div class="flex items-center gap-1 shrink-0">
								<CarbonTag type="blue" size="sm">
									{pack.mod_loader}
								</CarbonTag>
								<CarbonTag type="gray" size="sm">
									MC {pack.mc_version}
								</CarbonTag>
							</div>
						</div>

						<!-- Metadata summary -->
						<div class="flex items-center justify-between py-2 border-y border-[#393939] text-xs text-[#a8a8a8]">
							<span class="flex items-center gap-1.5 font-mono">
								<Package class="h-3.5 w-3.5 text-[#0f62fe]" />
								<strong class="text-white">{pack.mod_count}</strong> {pack.mod_count === 1 ? 'mod' : 'mods'}
							</span>
							<span class="flex items-center gap-1 text-[11px] font-mono text-[#8d8d8d]">
								<Calendar class="h-3 w-3" />
								{new Date(pack.updated_at).toLocaleDateString()}
							</span>
						</div>
					</div>

					<!-- Tile Footer Actions -->
					<div class="mt-4 pt-3 border-t border-[#393939] flex items-center justify-between gap-2">
						<div class="flex items-center gap-1">
							<CarbonButton
								kind="ghost"
								size="sm"
								iconOnly
								class="rounded-none text-[#a8a8a8] hover:text-white"
								onclick={() => clonePack(pack)}
								title="Duplicate / Clone modpack"
							>
								<Copy class="h-3.5 w-3.5" />
							</CarbonButton>
							<CarbonButton
								kind="ghost"
								size="sm"
								iconOnly
								class="rounded-none text-[#da1e28] hover:bg-[#da1e28]/20"
								onclick={() => deletePack(pack)}
								title="Delete modpack"
							>
								<Trash2 class="h-3.5 w-3.5" />
							</CarbonButton>
						</div>

						<div class="flex items-center gap-1.5">
							<CarbonButton
								kind="tertiary"
								size="sm"
								class="rounded-none h-8 px-2 text-[11px]"
								onclick={() => exportPack(pack.id, 'packwiz', pack.name)}
								disabled={exportingPack === `${pack.id}-packwiz`}
								title="Export Native Packwiz .zip"
							>
								Packwiz
							</CarbonButton>
							<CarbonButton
								kind="tertiary"
								size="sm"
								class="rounded-none h-8 px-2 text-[11px]"
								onclick={() => exportPack(pack.id, 'mrpack', pack.name)}
								disabled={exportingPack === `${pack.id}-mrpack`}
								title="Export Modrinth .mrpack"
							>
								.mrpack
							</CarbonButton>

							<CarbonButton
								kind="primary"
								size="sm"
								class="rounded-none h-8 text-xs"
								onclick={() => goto(`/modpacks/studio/${pack.id}`)}
							>
								Open Studio
							</CarbonButton>
						</div>
					</div>
				</CarbonTile>
			{/each}
		</div>
	{/if}
</div>

<!-- Create Dialog with Carbon Design System Fidelity -->
<CarbonModal
	bind:open={createDialogOpen}
	title="Create New Modpack Project"
	description="Initialize a new visual Packwiz workspace"
	hasFooter={false}
	size="lg"
>
	<div class="space-y-4">
		<CarbonTextInput
			label="Modpack Name *"
			placeholder="e.g. Odyssey SMP"
			bind:value={newName}
			disabled={creating}
		/>

		<div class="grid grid-cols-2 gap-4">
			<CarbonTextInput
				label="Author"
				placeholder="Admin"
				bind:value={newAuthor}
				disabled={creating}
			/>
			<CarbonTextInput
				label="Initial Version"
				placeholder="1.0.0"
				bind:value={newVersion}
				disabled={creating}
			/>
		</div>

		<div class="grid grid-cols-2 gap-4">
			<CarbonSelect
				label="Minecraft Version"
				bind:value={newMcVersion}
				disabled={creating}
			>
				{#each MC_VERSIONS as v}
					<option value={v}>{v}</option>
				{/each}
			</CarbonSelect>

			<CarbonSelect
				label="Mod Loader"
				bind:value={newLoader}
				disabled={creating}
			>
				<option value="fabric">Fabric</option>
				<option value="neoforge">NeoForge</option>
				<option value="forge">Forge</option>
				<option value="quilt">Quilt</option>
			</CarbonSelect>
		</div>

		<CarbonSelect
			label="Loader Version"
			bind:value={newLoaderVersion}
			disabled={creating}
			helperText={loadingLoaderVersions ? 'Fetching compatible loader versions...' : undefined}
		>
			{#each availableLoaderVersions as v}
				<option value={v}>{v}</option>
			{/each}
		</CarbonSelect>

		<div class="flex items-center justify-end gap-3 pt-4 border-t border-[#393939]">
			<CarbonButton
				kind="secondary"
				onclick={() => (createDialogOpen = false)}
				disabled={creating}
				class="rounded-none"
			>
				Cancel
			</CarbonButton>
			<CarbonButton
				kind="primary"
				onclick={handleCreatePack}
				disabled={creating || !newName.trim()}
				class="rounded-none"
			>
				{#if creating}
					<Loader2 class="mr-2 h-4 w-4 animate-spin" />
					Creating...
				{:else}
					Create Project
				{/if}
			</CarbonButton>
		</div>
	</div>
</CarbonModal>

<!-- Import Dialog with Carbon Design System Fidelity -->
<CarbonModal
	bind:open={importDialogOpen}
	title="Import Existing Modpack"
	description="Import a Modrinth (.mrpack), CurseForge (.zip), or Packwiz archive"
	hasFooter={false}
	size="lg"
>
	<div class="space-y-4">
		<div class="space-y-1.5">
			<label class="text-xs font-normal text-[#c6c6c6] tracking-[0.32px]">
				Modpack Archive File (.mrpack or .zip) *
			</label>
			<input
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
				class="w-full p-2.5 bg-[#262626] border border-[#393939] text-xs text-[#f4f4f4] rounded-none focus:outline-none focus:border-[#0f62fe]"
			/>
		</div>

		<CarbonSelect
			label="Format Detection"
			bind:value={importFormat}
			disabled={importing}
		>
			<option value="auto">Auto-detect from file</option>
			<option value="mrpack">Modrinth (.mrpack)</option>
			<option value="curseforge">CurseForge (.zip manifest)</option>
			<option value="packwiz">Native Packwiz (.zip)</option>
		</CarbonSelect>

		<CarbonTextInput
			label="Project Name Override (Optional)"
			placeholder="Leave blank to use metadata name"
			bind:value={importName}
			disabled={importing}
		/>

		<div class="flex items-center justify-end gap-3 pt-4 border-t border-[#393939]">
			<CarbonButton
				kind="secondary"
				onclick={() => (importDialogOpen = false)}
				disabled={importing}
				class="rounded-none"
			>
				Cancel
			</CarbonButton>
			<CarbonButton
				kind="primary"
				onclick={handleImportPack}
				disabled={importing || !importFile}
				class="rounded-none"
			>
				{#if importing}
					<Loader2 class="mr-2 h-4 w-4 animate-spin" />
					Importing...
				{:else}
					Import Modpack
				{/if}
			</CarbonButton>
		</div>
	</div>
</CarbonModal>
