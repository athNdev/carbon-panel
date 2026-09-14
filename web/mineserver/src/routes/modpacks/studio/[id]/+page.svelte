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
		DialogFooter,
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
		Blocks,
		Layers,
		CheckCircle2,
		AlertTriangle,
		RefreshCw,
		FileCode,
		FolderGit2,
		ArrowRightLeft,
		UploadCloud,
		Copy,
		Sparkles
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

	interface PackFileInfo {
		path: string;
		size: number;
		mod_time: string;
		category: string;
	}

	interface ModUpdateInfo {
		slug: string;
		name: string;
		platform: string;
		current_version_id: string;
		latest_version_id: string;
		latest_file_name: string;
		latest_download_url: string;
		update_available: boolean;
		pinned: boolean;
	}

	interface MigrationReport {
		pack_id: string;
		current_mc: string;
		current_loader: string;
		target_mc: string;
		target_loader: string;
		total_mods: number;
		compatible_count: number;
		incompatible_count: number;
		mods: {
			slug: string;
			name: string;
			platform: string;
			current_version_id: string;
			target_version_id?: string;
			target_file_name?: string;
			compatible: boolean;
		}[];
	}

	let packId = $derived(page.params.id);

	let pack = $state<Pack | null>(null);
	let loading = $state(true);
	let saving = $state(false);
	let exportingPack = $state<'mrpack' | 'curseforge' | 'packwiz' | null>(null);
	let availableLoaderVersions = $state<string[]>(['latest']);
	let loadingLoaderVersions = $state(false);

	// Primary View Tab: "mods", "overrides", "migrate", "maintenance"
	let activeTab = $state<'mods' | 'overrides' | 'migrate' | 'maintenance'>('mods');

	// Mod Filter & Selection
	let modFilter = $state('');
	let sideFilter = $state<'all' | 'both' | 'client' | 'server' | 'pinned' | 'updates'>('all');
	let selectedSlugs = $state<string[]>([]);

	// Update Engine State
	let checkingUpdates = $state(false);
	let updatesMap = $state<Record<string, ModUpdateInfo>>({});

	// Overrides / Files State
	let packFiles = $state<PackFileInfo[]>([]);
	let loadingFiles = $state(false);
	let uploadFileDialogOpen = $state(false);
	let uploadFilePath = $state('config/settings.json');
	let uploadFileContent = $state('');
	let savingFile = $state(false);

	// Migration State
	let migrateMC = $state('1.21.1');
	let migrateLoader = $state('fabric');
	let migrating = $state(false);
	let migrationReport = $state<MigrationReport | null>(null);

	// Search / Add Mods Modal State
	let searchDrawerOpen = $state(false);
	let addTab = $state<'modrinth' | 'curseforge' | 'url'>('modrinth');
	let searchQuery = $state('');
	let searching = $state(false);
	let searchResults = $state<SearchModResult[]>([]);
	let searchError = $state('');

	// Direct URL Mod Input State
	let urlModName = $state('');
	let urlModFileName = $state('');
	let urlModDownloadUrl = $state('');
	let urlModSide = $state<'both' | 'client' | 'server'>('both');
	let urlModPinned = $state(false);
	let addingUrlMod = $state(false);

	// Maintenance State
	let refreshing = $state(false);
	let rawPackToml = $state('');
	let rawIndexToml = $state('');
	let loadingRaw = $state(false);

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

	async function fetchLoaderVersions(loader: string, mcVer: string) {
		if (!loader) return;
		loadingLoaderVersions = true;
		try {
			const res = await apiFetch(`/api/v1/packwiz/loaders/${loader.toLowerCase()}/versions?game_version=${mcVer || ''}`);
			if (res.ok) {
				const data = await res.json();
				if (data.versions && data.versions.length > 0) {
					availableLoaderVersions = data.versions;
					if (pack?.loader_version && !availableLoaderVersions.includes(pack.loader_version)) {
						availableLoaderVersions = [pack.loader_version, ...availableLoaderVersions];
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
	}

	$effect(() => {
		if (pack?.mod_loader) {
			fetchLoaderVersions(pack.mod_loader, pack.mc_version);
		}
	});

	async function exportPack(format: 'mrpack' | 'curseforge' | 'packwiz') {
		if (!pack) return;
		exportingPack = format;
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
			a.download = `${pack.name}${ext}`;
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

	async function loadPack() {
		if (!packId) return;
		loading = true;
		try {
			const res = await apiFetch(`/api/v1/packwiz/packs/${packId}`);
			if (!res.ok) throw new Error(`HTTP ${res.status}`);
			const data = await res.json();
			if (data) {
				data.mods = data.mods || [];
			}
			pack = data;
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
			pack.mods = (pack.mods || []).filter((m) => m.slug !== mod.slug);
			selectedSlugs = selectedSlugs.filter((s) => s !== mod.slug);
			toast.success(`Removed ${mod.name} from modpack`);
		} catch (err) {
			console.error('Failed to delete mod:', err);
			toast.error('Failed to remove mod');
		}
	}

	// Batch Operations
	async function runBatchAction(action: 'set_side' | 'pin' | 'unpin' | 'remove', side?: string) {
		if (!pack || selectedSlugs.length === 0) return;
		try {
			const res = await apiFetch(`/api/v1/packwiz/packs/${packId}/mods/batch`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ action, slugs: selectedSlugs, side })
			});
			if (!res.ok) throw new Error(`HTTP ${res.status}`);
			toast.success(`Batch action applied to ${selectedSlugs.length} mods`);
			selectedSlugs = [];
			await loadPack();
		} catch (err: any) {
			console.error('Batch action failed:', err);
			toast.error(err.message || 'Batch action failed');
		}
	}

	// Updates Checker
	async function checkUpdates() {
		if (!pack) return;
		checkingUpdates = true;
		try {
			const res = await apiFetch(`/api/v1/packwiz/packs/${packId}/updates`);
			if (!res.ok) throw new Error(`HTTP ${res.status}`);
			const data = await res.json();
			const updates = data.updates || [];
			const map: Record<string, ModUpdateInfo> = {};
			let availableCount = 0;
			for (const u of updates) {
				map[u.slug] = u;
				if (u.update_available) availableCount++;
			}
			updatesMap = map;
			toast.success(`Checked updates: ${availableCount} newer versions available`);
		} catch (err: any) {
			console.error('Check updates failed:', err);
			toast.error('Failed to check mod updates');
		} finally {
			checkingUpdates = false;
		}
	}

	// Overrides / Files
	async function loadFiles() {
		if (!packId) return;
		loadingFiles = true;
		try {
			const res = await apiFetch(`/api/v1/packwiz/packs/${packId}/files`);
			if (!res.ok) throw new Error(`HTTP ${res.status}`);
			const data = await res.json();
			packFiles = data.files || [];
		} catch (err) {
			console.error('Failed to load files:', err);
		} finally {
			loadingFiles = false;
		}
	}

	async function handleSaveFile() {
		if (!uploadFilePath.trim()) {
			toast.error('Please specify a valid file path');
			return;
		}
		savingFile = true;
		try {
			const res = await apiFetch(`/api/v1/packwiz/packs/${packId}/files`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ path: uploadFilePath.trim(), content: uploadFileContent })
			});
			if (!res.ok) throw new Error(`HTTP ${res.status}`);
			toast.success(`Saved override file: ${uploadFilePath}`);
			uploadFileDialogOpen = false;
			await loadFiles();
		} catch (err: any) {
			console.error('Failed to save file:', err);
			toast.error('Failed to save override file');
		} finally {
			savingFile = false;
		}
	}

	async function handleDeleteFile(filePath: string) {
		if (!confirm(`Delete "${filePath}" from packwiz overrides?`)) return;
		try {
			const res = await apiFetch(`/api/v1/packwiz/packs/${packId}/files?path=${encodeURIComponent(filePath)}`, {
				method: 'DELETE'
			});
			if (!res.ok) throw new Error(`HTTP ${res.status}`);
			toast.success(`Deleted file "${filePath}"`);
			await loadFiles();
		} catch (err: any) {
			console.error('Failed to delete file:', err);
			toast.error('Failed to delete override file');
		}
	}

	// Version Migration
	async function simulateMigration(apply: boolean = false) {
		if (!pack) return;
		migrating = true;
		try {
			const res = await apiFetch(`/api/v1/packwiz/packs/${packId}/migrate`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ target_mc: migrateMC, target_loader: migrateLoader, apply })
			});
			if (!res.ok) throw new Error(`HTTP ${res.status}`);
			const data = await res.json();
			migrationReport = data;
			if (apply) {
				toast.success(`Migrated modpack to MC ${migrateMC} (${migrateLoader})`);
				await loadPack();
			} else {
				toast.success(`Simulation complete: ${data.compatible_count} compatible mods found`);
			}
		} catch (err: any) {
			console.error('Migration failed:', err);
			toast.error(err.message || 'Version migration simulation failed');
		} finally {
			migrating = false;
		}
	}

	// Maintenance & Refresh
	async function handleRefreshPack() {
		if (!packId) return;
		refreshing = true;
		try {
			const res = await apiFetch(`/api/v1/packwiz/packs/${packId}/refresh`, { method: 'POST' });
			if (!res.ok) throw new Error(`HTTP ${res.status}`);
			toast.success('Packwiz re-indexed & refreshed successfully');
			await loadPack();
			await loadRawTomls();
		} catch (err: any) {
			console.error('Refresh failed:', err);
			toast.error('Failed to refresh pack index');
		} finally {
			refreshing = false;
		}
	}

	async function loadRawTomls() {
		if (!packId) return;
		loadingRaw = true;
		try {
			const pRes = await apiFetch(`/api/v1/packwiz/${packId}/pack.toml`);
			if (pRes.ok) rawPackToml = await pRes.text();
			const iRes = await apiFetch(`/api/v1/packwiz/${packId}/index.toml`);
			if (iRes.ok) rawIndexToml = await iRes.text();
		} catch (err) {
			console.error('Failed to load raw tomls:', err);
		} finally {
			loadingRaw = false;
		}
	}

	// Search & Add Mods
	async function searchMods() {
		if (!pack) return;
		searching = true;
		searchError = '';
		searchResults = [];

		try {
			const params = new URLSearchParams({
				query: searchQuery.trim(),
				platform: addTab === 'curseforge' ? 'curseforge' : 'modrinth',
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
			const vParams = new URLSearchParams({
				platform: item.platform,
				loader: pack.mod_loader.toLowerCase(),
				mc_version: pack.mc_version
			});
			const modTarget = (item.platform === 'curseforge' && item.id) ? item.id : (item.slug || item.id);
			const vRes = await apiFetch(`/api/v1/servers/none/mods/${modTarget}/versions?${vParams.toString()}`);
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
				file_name: selectedVer.file_name || selectedVer.fileName || 'mod.jar',
				side,
				platform: item.platform,
				project_id: item.id,
				version_id: selectedVer.id,
				download_url: selectedVer.download_url || selectedVer.downloadUrl || '',
				file_size: selectedVer.file_size || selectedVer.fileSize,
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

	async function handleAddUrlMod() {
		if (!urlModDownloadUrl.trim()) {
			toast.error('Download URL is required');
			return;
		}
		addingUrlMod = true;
		try {
			const res = await apiFetch(`/api/v1/packwiz/packs/${packId}/mods/url`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({
					name: urlModName.trim(),
					file_name: urlModFileName.trim(),
					download_url: urlModDownloadUrl.trim(),
					side: urlModSide,
					pinned: urlModPinned
				})
			});
			if (!res.ok) {
				const txt = await res.text();
				throw new Error(txt || `HTTP ${res.status}`);
			}
			const created = await res.json();
			toast.success(`Added custom mod "${created.name}" from URL`);
			searchDrawerOpen = false;
			urlModName = '';
			urlModFileName = '';
			urlModDownloadUrl = '';
			await loadPack();
		} catch (err: any) {
			console.error('Failed to add mod from URL:', err);
			toast.error(err.message || 'Failed to add direct URL mod');
		} finally {
			addingUrlMod = false;
		}
	}

	let filteredMods = $derived(
		(pack?.mods || []).filter((m) => {
			const matchesText =
				(m.name || '').toLowerCase().includes(modFilter.toLowerCase()) ||
				(m.file_name || '').toLowerCase().includes(modFilter.toLowerCase());
			if (!matchesText) return false;

			if (sideFilter === 'both') return m.side === 'both';
			if (sideFilter === 'client') return m.side === 'client';
			if (sideFilter === 'server') return m.side === 'server';
			if (sideFilter === 'pinned') return !!m.pinned;
			if (sideFilter === 'updates') return !!updatesMap[m.slug]?.update_available;
			return true;
		})
	);

	function formatDownloads(count: number): string {
		if (!count) return '0';
		if (count >= 1_000_000) return `${(count / 1_000_000).toFixed(1)}M`;
		if (count >= 1_000) return `${(count / 1_000).toFixed(1)}k`;
		return count.toString();
	}

	let prevLoadedId = $state<string | undefined>(undefined);
	$effect(() => {
		if (packId && packId !== prevLoadedId) {
			prevLoadedId = packId;
			loadPack();
			loadFiles();
			loadRawTomls();
		}
	});
</script>

<div class="h-full flex-1 space-y-6 bg-linear-to-br from-background to-muted/10 p-8 pt-6">
	<!-- Top Header Bar -->
	<div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4 border-b-2 border-border/50 pb-5">
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
						Packwiz Studio Project  v{pack.version} by {pack.author || 'Admin'}
					</p>
				</div>
			{/if}
		</div>

		<div class="flex items-center gap-2 flex-wrap">
			{#if pack}
				<Button variant="outline" size="sm" onclick={() => (deployDialogOpen = true)}>
					<Rocket class="mr-1.5 h-4 w-4 text-primary" />
					Deploy to Server
				</Button>

				<!-- Export Formats -->
				<Button
					variant="outline"
					size="sm"
					onclick={() => exportPack('packwiz')}
					disabled={exportingPack === 'packwiz'}
					class="text-xs h-8"
					title="Export native Packwiz .zip archive"
				>
					{#if exportingPack === 'packwiz'}
						<Loader2 class="mr-1.5 h-3.5 w-3.5 animate-spin" />
					{:else}
						<Download class="mr-1.5 h-3.5 w-3.5 text-primary" />
					{/if}
					Packwiz .zip
				</Button>

				<Button
					variant="outline"
					size="sm"
					onclick={() => exportPack('mrpack')}
					disabled={exportingPack === 'mrpack'}
					class="text-xs h-8"
					title="Export Modrinth .mrpack"
				>
					{#if exportingPack === 'mrpack'}
						<Loader2 class="mr-1.5 h-3.5 w-3.5 animate-spin" />
					{:else}
						<Download class="mr-1.5 h-3.5 w-3.5" />
					{/if}
					.mrpack
				</Button>

				<Button
					variant="outline"
					size="sm"
					onclick={() => exportPack('curseforge')}
					disabled={exportingPack === 'curseforge'}
					class="text-xs h-8"
					title="Export CurseForge .zip"
				>
					{#if exportingPack === 'curseforge'}
						<Loader2 class="mr-1.5 h-3.5 w-3.5 animate-spin" />
					{:else}
						<Download class="mr-1.5 h-3.5 w-3.5" />
					{/if}
					CurseForge .zip
				</Button>

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
			<!-- Settings Sidebar -->
			<Card class="lg:col-span-1 h-fit shadow-xs">
				<CardHeader class="pb-3">
					<CardTitle class="text-base font-semibold">Modpack Settings</CardTitle>
					<CardDescription class="text-xs">Configure target runtime & loader</CardDescription>
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
						<div class="flex items-center justify-between">
							<Label for="metaLoaderVer" class="text-xs">Loader Version</Label>
							{#if loadingLoaderVersions}
								<span class="inline-flex items-center text-[10px] text-muted-foreground">
									<Loader2 class="h-2.5 w-2.5 mr-1 animate-spin" />
									fetching...
								</span>
							{/if}
						</div>
						<Select type="single" bind:value={pack.loader_version}>
							<SelectTrigger id="metaLoaderVer" class="h-8 text-xs">
								<span>{pack.loader_version || 'latest'}</span>
							</SelectTrigger>
							<SelectContent class="max-h-56 overflow-y-auto">
								{#each availableLoaderVersions as v}
									<SelectItem value={v}>{v}</SelectItem>
								{/each}
							</SelectContent>
						</Select>
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

			<!-- Studio Main Workspace Area -->
			<div class="lg:col-span-3 space-y-4">
				<!-- Workspace View Tabs -->
				<div class="flex items-center justify-between border-b pb-2">
					<div class="flex items-center gap-2">
						<Button
							variant={activeTab === 'mods' ? 'default' : 'ghost'}
							size="sm"
							class="h-8 text-xs font-semibold"
							onclick={() => (activeTab = 'mods')}
						>
							<Package class="mr-1.5 h-3.5 w-3.5" />
							Mods ({pack.mods.length})
						</Button>

						<Button
							variant={activeTab === 'overrides' ? 'default' : 'ghost'}
							size="sm"
							class="h-8 text-xs font-semibold"
							onclick={() => (activeTab = 'overrides')}
						>
							<FolderGit2 class="mr-1.5 h-3.5 w-3.5" />
							Overrides & Configs ({packFiles.length})
						</Button>

						<Button
							variant={activeTab === 'migrate' ? 'default' : 'ghost'}
							size="sm"
							class="h-8 text-xs font-semibold"
							onclick={() => (activeTab = 'migrate')}
						>
							<ArrowRightLeft class="mr-1.5 h-3.5 w-3.5" />
							Version Migration
						</Button>

						<Button
							variant={activeTab === 'maintenance' ? 'default' : 'ghost'}
							size="sm"
							class="h-8 text-xs font-semibold"
							onclick={() => (activeTab = 'maintenance')}
						>
							<RefreshCw class="mr-1.5 h-3.5 w-3.5" />
							Maintenance & TOML
						</Button>
					</div>
				</div>

				<!-- TAB 1: MODS MANAGEMENT -->
				{#if activeTab === 'mods'}
					<Card class="shadow-xs">
						<CardHeader class="pb-3">
							<div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
								<div class="space-y-1">
									<div class="flex items-center gap-2">
										<CardTitle class="text-lg font-bold">Mods in Modpack</CardTitle>
										<Badge variant="secondary" class="text-xs font-mono">
											{pack.mods.length}
										</Badge>
									</div>
									<CardDescription class="text-xs">
										Manage server/client sides, pinning, dependency versions, and update checks.
									</CardDescription>
								</div>

								<div class="flex items-center gap-2 flex-wrap">
									<div class="relative w-44 sm:w-56">
										<Input
											placeholder="Filter mods..."
											bind:value={modFilter}
											class="h-8 pl-8 text-xs"
										/>
										<Search class="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
									</div>

									<Button
										variant="outline"
										size="sm"
										class="h-8 text-xs"
										onclick={checkUpdates}
										disabled={checkingUpdates}
										title="Check Modrinth for newer mod releases"
									>
										{#if checkingUpdates}
											<Loader2 class="mr-1.5 h-3.5 w-3.5 animate-spin" />
										{:else}
											<Sparkles class="mr-1.5 h-3.5 w-3.5 text-amber-500" />
										{/if}
										Check Updates
									</Button>

									<Button size="sm" class="h-8 text-xs" onclick={() => (searchDrawerOpen = true)}>
										<Plus class="mr-1.5 h-4 w-4" />
										Add Mods
									</Button>
								</div>
							</div>

							<!-- Filter Pills Bar -->
							<div class="flex items-center gap-1.5 pt-2 flex-wrap text-xs">
								<button
									type="button"
									onclick={() => (sideFilter = 'all')}
									class="px-2.5 py-1 rounded-full text-[11px] font-medium transition-colors cursor-pointer
										{sideFilter === 'all' ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground hover:text-foreground'}"
								>
									All ({pack.mods.length})
								</button>
								<button
									type="button"
									onclick={() => (sideFilter = 'both')}
									class="px-2.5 py-1 rounded-full text-[11px] font-medium transition-colors cursor-pointer
										{sideFilter === 'both' ? 'bg-primary/20 text-primary border border-primary/30' : 'bg-muted text-muted-foreground hover:text-foreground'}"
								>
									Both ({pack.mods.filter((m) => m.side === 'both').length})
								</button>
								<button
									type="button"
									onclick={() => (sideFilter = 'client')}
									class="px-2.5 py-1 rounded-full text-[11px] font-medium transition-colors cursor-pointer
										{sideFilter === 'client' ? 'bg-amber-500/20 text-amber-600 dark:text-amber-400 border border-amber-500/30' : 'bg-muted text-muted-foreground hover:text-foreground'}"
								>
									Client Only ({pack.mods.filter((m) => m.side === 'client').length})
								</button>
								<button
									type="button"
									onclick={() => (sideFilter = 'server')}
									class="px-2.5 py-1 rounded-full text-[11px] font-medium transition-colors cursor-pointer
										{sideFilter === 'server' ? 'bg-emerald-500/20 text-emerald-600 dark:text-emerald-400 border border-emerald-500/30' : 'bg-muted text-muted-foreground hover:text-foreground'}"
								>
									Server Only ({pack.mods.filter((m) => m.side === 'server').length})
								</button>
								<button
									type="button"
									onclick={() => (sideFilter = 'pinned')}
									class="px-2.5 py-1 rounded-full text-[11px] font-medium transition-colors cursor-pointer
										{sideFilter === 'pinned' ? 'bg-purple-500/20 text-purple-600 dark:text-purple-400 border border-purple-500/30' : 'bg-muted text-muted-foreground hover:text-foreground'}"
								>
									Pinned ({pack.mods.filter((m) => m.pinned).length})
								</button>
							</div>

							<!-- Batch Action Toolbar (Visible when >= 1 mod selected) -->
							{#if selectedSlugs.length > 0}
								<div class="flex items-center justify-between bg-primary/10 border border-primary/30 rounded-md p-2 mt-2">
									<span class="text-xs font-semibold text-primary">
										{selectedSlugs.length} mod(s) selected
									</span>
									<div class="flex items-center gap-1.5">
										<Button size="sm" variant="outline" class="h-7 text-xs" onclick={() => runBatchAction('set_side', 'both')}>
											Set Both
										</Button>
										<Button size="sm" variant="outline" class="h-7 text-xs" onclick={() => runBatchAction('set_side', 'server')}>
											Set Server
										</Button>
										<Button size="sm" variant="outline" class="h-7 text-xs" onclick={() => runBatchAction('set_side', 'client')}>
											Set Client
										</Button>
										<Button size="sm" variant="outline" class="h-7 text-xs" onclick={() => runBatchAction('pin')}>
											Pin
										</Button>
										<Button size="sm" variant="outline" class="h-7 text-xs" onclick={() => runBatchAction('unpin')}>
											Unpin
										</Button>
										<Button size="sm" variant="destructive" class="h-7 text-xs" onclick={() => runBatchAction('remove')}>
											Remove
										</Button>
										<Button size="sm" variant="ghost" class="h-7 text-xs" onclick={() => (selectedSlugs = [])}>
											Clear
										</Button>
									</div>
								</div>
							{/if}
						</CardHeader>

						<CardContent class="p-0">
							{#if filteredMods.length === 0}
								<div class="flex flex-col items-center justify-center py-20 text-center text-muted-foreground">
									<Package class="h-10 w-10 stroke-[1.5]" />
									<p class="mt-3 text-base font-semibold">No mods match current filter</p>
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
												<th class="py-2.5 px-3 w-8">
													<input
														type="checkbox"
														class="rounded border-border text-primary cursor-pointer"
														checked={selectedSlugs.length > 0 && selectedSlugs.length === filteredMods.length}
														onchange={(e) => {
															const checked = (e.target as HTMLInputElement).checked;
															selectedSlugs = checked ? filteredMods.map((m) => m.slug) : [];
														}}
													/>
												</th>
												<th class="py-2.5 px-3">Mod Name</th>
												<th class="py-2.5 px-3">File Name</th>
												<th class="py-2.5 px-3">Platform</th>
												<th class="py-2.5 px-3">Side</th>
												<th class="py-2.5 px-3">Pin</th>
												<th class="py-2.5 px-3">Status</th>
												<th class="py-2.5 px-3 text-right">Actions</th>
											</tr>
										</thead>
										<tbody class="divide-y divide-border/60">
											{#each filteredMods as mod (mod.slug)}
												<tr class="hover:bg-muted/20 transition-colors {selectedSlugs.includes(mod.slug) ? 'bg-primary/5' : ''}">
													<td class="py-3 px-3">
														<input
															type="checkbox"
															class="rounded border-border text-primary cursor-pointer"
															checked={selectedSlugs.includes(mod.slug)}
															onchange={(e) => {
																const checked = (e.target as HTMLInputElement).checked;
																if (checked) {
																	selectedSlugs = [...selectedSlugs, mod.slug];
																} else {
																	selectedSlugs = selectedSlugs.filter((s) => s !== mod.slug);
																}
															}}
														/>
													</td>
													<td class="py-3 px-3 font-semibold text-foreground">
														<div class="flex items-center gap-2">
															<Package class="h-4 w-4 text-primary/70 flex-shrink-0" />
															<span>{mod.name}</span>
														</div>
													</td>
													<td class="py-3 px-3 font-mono text-[11px] text-muted-foreground max-w-[180px] truncate">
														{mod.file_name}
													</td>
													<td class="py-3 px-3">
														<Badge variant="outline" class="text-[10px] uppercase font-mono">
															{mod.platform}
														</Badge>
													</td>
													<td class="py-3 px-3">
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
													<td class="py-3 px-3">
														<button
															type="button"
															onclick={() => togglePin(mod)}
															class="p-1 rounded text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
															title={mod.pinned ? 'Version pinned' : 'Click to pin version'}
														>
															{#if mod.pinned}
																<Pin class="h-3.5 w-3.5 text-primary" />
															{:else}
																<PinOff class="h-3.5 w-3.5 opacity-40 hover:opacity-100" />
															{/if}
														</button>
													</td>
													<td class="py-3 px-3">
														{#if updatesMap[mod.slug]?.update_available}
															<Badge variant="default" class="bg-amber-500/15 text-amber-600 dark:text-amber-400 border border-amber-500/30 text-[10px]">
																Update Ready
															</Badge>
														{:else}
															<span class="text-[11px] text-muted-foreground">Up to date</span>
														{/if}
													</td>
													<td class="py-3 px-3 text-right">
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
				{/if}

				<!-- TAB 2: OVERRIDES & CONFIGS -->
				{#if activeTab === 'overrides'}
					<Card class="shadow-xs">
						<CardHeader class="pb-3">
							<div class="flex items-center justify-between">
								<div>
									<CardTitle class="text-lg font-bold">Overrides & Config Files</CardTitle>
									<CardDescription class="text-xs">
										Packwiz overrides bundled with the modpack: server configurations, resource packs, shaders, datapacks.
									</CardDescription>
								</div>
								<Button size="sm" onclick={() => (uploadFileDialogOpen = true)}>
									<Plus class="mr-1.5 h-4 w-4" />
									Add Override File
								</Button>
							</div>
						</CardHeader>
						<CardContent>
							{#if loadingFiles}
								<div class="flex items-center justify-center py-12 text-muted-foreground">
									<Loader2 class="h-6 w-6 animate-spin text-primary" />
									<span class="ml-2 text-xs">Scanning override files...</span>
								</div>
							{:else if packFiles.length === 0}
								<div class="flex flex-col items-center justify-center py-16 text-center text-muted-foreground">
									<FolderGit2 class="h-10 w-10 stroke-[1.5]" />
									<p class="mt-3 text-sm font-semibold">No override files found</p>
									<p class="text-xs">Add custom server configs, datapacks, or options to bundle them in the modpack.</p>
									<Button size="sm" variant="outline" class="mt-4" onclick={() => (uploadFileDialogOpen = true)}>
										<Plus class="mr-1.5 h-4 w-4" />
										Add Config File
									</Button>
								</div>
							{:else}
								<div class="overflow-x-auto border rounded-md">
									<table class="w-full text-left text-xs">
										<thead class="border-b bg-muted/40 font-semibold text-muted-foreground">
											<tr>
												<th class="py-2.5 px-3">File Path</th>
												<th class="py-2.5 px-3">Category</th>
												<th class="py-2.5 px-3">Size</th>
												<th class="py-2.5 px-3 text-right">Actions</th>
											</tr>
										</thead>
										<tbody class="divide-y">
											{#each packFiles as file (file.path)}
												<tr class="hover:bg-muted/20">
													<td class="py-2.5 px-3 font-mono text-[11px] text-foreground font-semibold">
														{file.path}
													</td>
													<td class="py-2.5 px-3">
														<Badge variant="outline" class="text-[10px] capitalize">
															{file.category}
														</Badge>
													</td>
													<td class="py-2.5 px-3 text-muted-foreground">
														{formatBytes(file.size)}
													</td>
													<td class="py-2.5 px-3 text-right">
														<Button
															variant="ghost"
															size="icon"
															class="h-7 w-7 text-destructive hover:bg-destructive/10"
															onclick={() => handleDeleteFile(file.path)}
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
				{/if}

				<!-- TAB 3: VERSION MIGRATION -->
				{#if activeTab === 'migrate'}
					<Card class="shadow-xs">
						<CardHeader class="pb-3">
							<CardTitle class="text-lg font-bold">Minecraft Version Migration Assistant</CardTitle>
							<CardDescription class="text-xs">
								Simulate upgrading all pack mods to a newer Minecraft release or loader before applying changes.
							</CardDescription>
						</CardHeader>
						<CardContent class="space-y-4">
							<div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
								<div class="space-y-1.5">
									<Label class="text-xs">Target Minecraft Version</Label>
									<Select type="single" bind:value={migrateMC}>
										<SelectTrigger class="h-8 text-xs">
											<span>{migrateMC}</span>
										</SelectTrigger>
										<SelectContent>
											{#each MC_VERSIONS as v}
												<SelectItem value={v}>{v}</SelectItem>
											{/each}
										</SelectContent>
									</Select>
								</div>

								<div class="space-y-1.5">
									<Label class="text-xs">Target Mod Loader</Label>
									<Select type="single" bind:value={migrateLoader}>
										<SelectTrigger class="h-8 text-xs">
											<span class="capitalize">{migrateLoader}</span>
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

							<div class="flex items-center gap-3 pt-2">
								<Button size="sm" variant="outline" onclick={() => simulateMigration(false)} disabled={migrating}>
									{#if migrating}
										<Loader2 class="mr-1.5 h-3.5 w-3.5 animate-spin" />
									{:else}
										<Search class="mr-1.5 h-3.5 w-3.5 text-primary" />
									{/if}
									Simulate Compatibility
								</Button>

								<Button size="sm" onclick={() => simulateMigration(true)} disabled={migrating}>
									<ArrowRightLeft class="mr-1.5 h-3.5 w-3.5" />
									Apply Migration
								</Button>
							</div>

							{#if migrationReport}
								<div class="border rounded-md p-4 bg-muted/20 space-y-3 mt-4">
									<div class="flex items-center justify-between">
										<div class="flex items-center gap-2">
											<Badge variant="default" class="bg-emerald-600 text-white">
												{migrationReport.compatible_count} Compatible
											</Badge>
											<Badge variant="destructive">
												{migrationReport.incompatible_count} Missing Version
											</Badge>
										</div>
										<span class="text-xs text-muted-foreground">
											Target: MC {migrationReport.target_mc} ({migrationReport.target_loader})
										</span>
									</div>

									<div class="max-h-60 overflow-y-auto space-y-1.5 divide-y divide-border/50 text-xs">
										{#each migrationReport.mods as m}
											<div class="flex items-center justify-between py-1.5">
												<span class="font-medium">{m.name}</span>
												{#if m.compatible}
													<span class="text-emerald-600 font-mono text-[11px] flex items-center gap-1">
														<CheckCircle2 class="h-3.5 w-3.5" />
														Compatible
													</span>
												{:else}
													<span class="text-amber-600 font-mono text-[11px] flex items-center gap-1">
														<AlertTriangle class="h-3.5 w-3.5" />
														No version for {migrationReport.target_mc}
													</span>
												{/if}
											</div>
										{/each}
									</div>
								</div>
							{/if}
						</CardContent>
					</Card>
				{/if}

				<!-- TAB 4: MAINTENANCE & RAW TOML -->
				{#if activeTab === 'maintenance'}
					<Card class="shadow-xs">
						<CardHeader class="pb-3">
							<CardTitle class="text-lg font-bold">Packwiz Maintenance & Live Serving</CardTitle>
							<CardDescription class="text-xs">
								Inspect raw generated TOML files, rebuild indices, and view live container bootstrap URLs.
							</CardDescription>
						</CardHeader>
						<CardContent class="space-y-6">
							<!-- Live Serving URL -->
							<div class="space-y-2 p-3.5 rounded-lg border bg-card">
								<div class="flex items-center justify-between">
									<Label class="text-xs font-semibold">Live Container Bootstrap URL (PACKWIZ_URL)</Label>
									<Button
										variant="ghost"
										size="sm"
										class="h-7 text-xs"
										onclick={() => {
											const u = `${window.location.origin}/api/v1/packwiz/${pack?.id}/pack.toml`;
											navigator.clipboard.writeText(u);
											toast.success('Copied PACKWIZ_URL to clipboard!');
										}}
									>
										<Copy class="mr-1.5 h-3.5 w-3.5" />
										Copy URL
									</Button>
								</div>
								<div class="font-mono text-xs p-2 rounded bg-muted/60 select-all break-all">
									{typeof window !== 'undefined' ? `${window.location.origin}/api/v1/packwiz/${pack?.id}/pack.toml` : `/api/v1/packwiz/${pack?.id}/pack.toml`}
								</div>
								<p class="text-[11px] text-muted-foreground">
									Set this URL in Docker container environments or `packwiz-installer` to dynamically provision this modpack on startup.
								</p>
							</div>

							<!-- Refresh Index -->
							<div class="flex items-center justify-between p-3.5 rounded-lg border bg-card">
								<div class="space-y-0.5">
									<h4 class="text-sm font-semibold">Refresh Packwiz Index</h4>
									<p class="text-xs text-muted-foreground">
										Recalculate SHA256 hashes for all `.pw.toml` and override files according to `packwiz refresh`.
									</p>
								</div>
								<Button size="sm" variant="outline" onclick={handleRefreshPack} disabled={refreshing}>
									{#if refreshing}
										<Loader2 class="mr-1.5 h-3.5 w-3.5 animate-spin" />
									{:else}
										<RefreshCw class="mr-1.5 h-3.5 w-3.5 text-primary" />
									{/if}
									Run Packwiz Refresh
								</Button>
							</div>

							<!-- Raw TOML Inspector -->
							<div class="space-y-2">
								<div class="flex items-center justify-between">
									<Label class="text-xs font-semibold">Raw pack.toml Preview</Label>
									<Button variant="ghost" size="sm" class="h-6 text-[11px]" onclick={loadRawTomls} disabled={loadingRaw}>
										Reload TOML
									</Button>
								</div>
								<pre class="font-mono text-[11px] p-3 rounded-md bg-muted/60 max-h-48 overflow-y-auto">{rawPackToml || 'Loading pack.toml...'}</pre>
							</div>
						</CardContent>
					</Card>
				{/if}
			</div>
		</div>
	{/if}
</div>

<!-- Add Mods Drawer / Modal with Modrinth, CurseForge & Direct URL -->
<DialogPrimitive.Root bind:open={searchDrawerOpen}>
	<DialogContent class="max-h-[90vh] !max-w-3xl flex flex-col p-6">
		<DialogHeader>
			<div class="flex items-center justify-between">
				<DialogTitle class="flex items-center gap-2 text-xl font-bold">
					<Blocks class="h-5 w-5 text-primary" />
					Add Mods to Packwiz
				</DialogTitle>
				{#if pack}
					<div class="flex items-center gap-2">
						<Badge variant="outline" class="text-xs uppercase">{pack.mod_loader}</Badge>
						<Badge variant="secondary" class="text-xs">MC {pack.mc_version}</Badge>
					</div>
				{/if}
			</div>
			<DialogDescription>
				Search Modrinth and CurseForge, or add mods via direct download link.
			</DialogDescription>
		</DialogHeader>

		<!-- Mode Tabs -->
		<div class="mt-4 flex items-center justify-between gap-3">
			<Tabs
				value={addTab}
				onValueChange={(val) => {
					addTab = val as 'modrinth' | 'curseforge' | 'url';
					if (addTab !== 'url') searchMods();
				}}
			>
				<TabsList>
					<TabsTrigger value="modrinth" class="px-3 text-xs">Modrinth</TabsTrigger>
					<TabsTrigger value="curseforge" class="px-3 text-xs">CurseForge</TabsTrigger>
					<TabsTrigger value="url" class="px-3 text-xs">Direct Download URL</TabsTrigger>
				</TabsList>
			</Tabs>

			{#if addTab !== 'url'}
				<div class="relative flex-1">
					<Input
						placeholder="Search online mods..."
						bind:value={searchQuery}
						onkeydown={(e) => e.key === 'Enter' && searchMods()}
						class="pl-8 text-xs h-9"
					/>
					<Search class="absolute left-2.5 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
				</div>

				<Button onclick={searchMods} disabled={searching} class="h-9">
					{#if searching}
						<Loader2 class="h-4 w-4 animate-spin" />
					{:else}
						Search
					{/if}
				</Button>
			{/if}
		</div>

		{#if addTab === 'url'}
			<!-- Direct URL Tab -->
			<div class="space-y-4 py-4">
				<div class="space-y-1.5">
					<Label for="urlModLink" class="text-xs">Direct Download URL (*.jar)</Label>
					<Input
						id="urlModLink"
						bind:value={urlModDownloadUrl}
						placeholder="https://example.com/mods/my-custom-mod.jar"
						class="text-xs"
					/>
					<p class="text-[11px] text-muted-foreground">
						Direct link to JAR file (GitHub Release, custom server, etc.). Packwiz will compute SHA256 hashes automatically.
					</p>
				</div>

				<div class="grid grid-cols-2 gap-3">
					<div class="space-y-1.5">
						<Label for="urlName" class="text-xs">Mod Name (Optional)</Label>
						<Input id="urlName" bind:value={urlModName} placeholder="Custom Mod" class="text-xs" />
					</div>
					<div class="space-y-1.5">
						<Label for="urlFileName" class="text-xs">File Name (Optional)</Label>
						<Input id="urlFileName" bind:value={urlModFileName} placeholder="mod.jar" class="text-xs" />
					</div>
				</div>

				<div class="grid grid-cols-2 gap-3">
					<div class="space-y-1.5">
						<Label class="text-xs">Side</Label>
						<Select type="single" bind:value={urlModSide}>
							<SelectTrigger class="h-8 text-xs">
								<span class="capitalize">{urlModSide}</span>
							</SelectTrigger>
							<SelectContent>
								<SelectItem value="both">Both (Client & Server)</SelectItem>
								<SelectItem value="server">Server Only</SelectItem>
								<SelectItem value="client">Client Only</SelectItem>
							</SelectContent>
						</Select>
					</div>

					<div class="flex items-center gap-2 pt-6">
						<input type="checkbox" id="urlPin" bind:checked={urlModPinned} class="rounded border-border text-primary cursor-pointer" />
						<Label for="urlPin" class="text-xs cursor-pointer">Pin version (prevent automatic updates)</Label>
					</div>
				</div>

				<Button class="w-full mt-2" onclick={handleAddUrlMod} disabled={addingUrlMod}>
					{#if addingUrlMod}
						<Loader2 class="mr-2 h-4 w-4 animate-spin" />
						Fetching & Hashing...
					{:else}
						<Plus class="mr-1.5 h-4 w-4" />
						Add Mod from URL
					{/if}
				</Button>
			</div>
		{:else}
			<!-- Modrinth / CurseForge Online Results List -->
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
									<Button size="sm" variant="outline" class="h-7 text-xs px-2" onclick={() => addModToPack(item, 'client')} title="Add as client-only">
										+ Client
									</Button>
									<Button size="sm" variant="outline" class="h-7 text-xs px-2" onclick={() => addModToPack(item, 'server')} title="Add as server-only">
										+ Server
									</Button>
									<Button size="sm" class="h-7 text-xs px-2.5" onclick={() => addModToPack(item, 'both')} title="Add for both client & server">
										<Plus class="mr-1 h-3.5 w-3.5" />
										Add (Both)
									</Button>
								</div>
							</div>
						{/each}
					</div>
				{/if}
			</div>
		{/if}
	</DialogContent>
</DialogPrimitive.Root>

<!-- Upload / Add Override File Modal -->
<DialogPrimitive.Root bind:open={uploadFileDialogOpen}>
	<DialogContent class="sm:max-w-[480px]">
		<DialogHeader>
			<DialogTitle class="flex items-center gap-2">
				<UploadCloud class="h-5 w-5 text-primary" />
				Add Override File
			</DialogTitle>
			<DialogDescription>
				Bundle a custom config file, script, or options into your Packwiz modpack.
			</DialogDescription>
		</DialogHeader>

		<div class="space-y-4 py-3">
			<div class="space-y-1.5">
				<Label for="overridePath" class="text-xs">Relative File Path</Label>
				<Input id="overridePath" bind:value={uploadFilePath} placeholder="e.g. config/options.txt" class="text-xs" />
			</div>

			<div class="space-y-1.5">
				<Label for="overrideContent" class="text-xs">File Content (Text / JSON / YAML)</Label>
				<textarea
					id="overrideContent"
					bind:value={uploadFileContent}
					rows={8}
					class="w-full rounded-md border border-input bg-transparent px-3 py-2 text-xs font-mono shadow-xs focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
					placeholder="Enter configuration content here..."
				></textarea>
			</div>
		</div>

		<DialogFooter>
			<Button variant="outline" onclick={() => (uploadFileDialogOpen = false)} disabled={savingFile}>
				Cancel
			</Button>
			<Button onclick={handleSaveFile} disabled={savingFile}>
				{#if savingFile}
					<Loader2 class="mr-2 h-4 w-4 animate-spin" />
					Saving...
				{:else}
					Save File
				{/if}
			</Button>
		</DialogFooter>
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
