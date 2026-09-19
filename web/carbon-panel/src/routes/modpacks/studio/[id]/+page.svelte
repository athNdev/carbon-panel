<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import {
		CarbonButton,
		CarbonTile,
		CarbonTag,
		CarbonTabs,
		CarbonModal,
		CarbonTextInput,
		CarbonSelect,
		CarbonSearch,
		CarbonStructuredList,
		CarbonAccordion,
		CarbonAccordionItem,
		CarbonInlineLoading
	} from '$lib/components/carbon';
	import {
		ArrowLeft,
		Save,
		Download,
		Rocket,
		Plus,
		Trash2,
		Pin,
		PinOff,
		Package,
		Loader2,
		Blocks,
		CheckCircle2,
		AlertTriangle,
		RefreshCw,
		FolderGit2,
		ArrowRightLeft,
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
	// MC version the pack had when last loaded/saved — used to detect an MC
	// version change that marks installed mods stale (ticket: stale + resolve).
	let loadedMCVersion = $state('');
	let exportingPack = $state<'mrpack' | 'curseforge' | 'packwiz' | null>(null);
	let availableLoaderVersions = $state<string[]>(['latest']);
	let loadingLoaderVersions = $state(false);

	// Primary View Tab: "mods", "overrides", "migrate", "maintenance"
	let activeTab = $state<'mods' | 'overrides' | 'migrate' | 'maintenance'>('mods');

	// Overrides / Files State (declared before studioTabs which references packFiles)
	let packFiles = $state<PackFileInfo[]>([]);

	const studioTabs = $derived([
		{ id: 'mods', label: 'Mods', badge: pack?.mods?.length ?? 0 },
		{ id: 'overrides', label: 'Overrides & Configs', badge: packFiles.length },
		{ id: 'migrate', label: 'Version Migration' },
		{ id: 'maintenance', label: 'Maintenance & TOML' }
	]);

	// Mod Filter & Selection
	let modFilter = $state('');
	let sideFilter = $state<'all' | 'both' | 'client' | 'server' | 'pinned' | 'updates'>('all');
	let selectedSlugs = $state<string[]>([]);

	// Update Engine State
	let checkingUpdates = $state(false);
	let updatesMap = $state<Record<string, ModUpdateInfo>>({});

	// Overrides / Files State
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

	const addModSourceTabs = [
		{ id: 'modrinth', label: 'Modrinth' },
		{ id: 'curseforge', label: 'CurseForge' },
		{ id: 'url', label: 'Direct Download URL' }
	];

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
			loadedMCVersion = data.mc_version || '';
		} catch (err) {
			console.error('Failed to load pack:', err);
			toast.error('Failed to load modpack project');
		} finally {
			loading = false;
		}
	}

	async function saveMetadata() {
		if (!pack) return;
		const mcChanged = loadedMCVersion !== '' && pack.mc_version !== loadedMCVersion;
		saving = true;
		try {
			const res = await apiFetch(`/api/v1/packwiz/packs/${packId}`, {
				method: 'PUT',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify(pack)
			});
			if (!res.ok) throw new Error(`HTTP ${res.status}`);
			if (mcChanged) {
				// MC version change marks installed mods stale: jump to the
				// migration tab and simulate so the user can update & resolve
				// conflicts in one flow.
				loadedMCVersion = pack.mc_version;
				migrateMC = pack.mc_version;
				activeTab = 'migrate';
				toast.warning(
					`MC version changed to ${pack.mc_version} — installed mods are now stale. Review compatibility below.`
				);
				await simulateMigration(false);
			} else {
				toast.success('Modpack settings saved');
			}
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

			// Resolve transitive required dependencies (best-effort, depth-bounded).
			const visited = new Set<string>();
			for (const m of pack.mods || []) {
				if (m.project_id) visited.add(`${(m.platform || item.platform).toLowerCase()}:${m.project_id.toLowerCase()}`);
				if (m.slug) visited.add(`slug:${m.slug.toLowerCase()}`);
			}
			visited.add(`${item.platform.toLowerCase()}:${item.id.toLowerCase()}`);
			try {
				const depCount = await resolveAndAddDependencies(
					selectedVer.dependencies || [],
					item.platform,
					side,
					0,
					visited
				);
				if (depCount > 0) {
					toast.success(`Auto-installed ${depCount} required dependenc${depCount === 1 ? 'y' : 'ies'}`);
				}
			} catch (depErr) {
				console.warn('Dependency auto-resolve failed (non-fatal):', depErr);
			}
			await loadPack();
		} catch (err: any) {
			console.error('Failed to add mod:', err);
			toast.error(err.message || 'Failed to add mod to pack');
		}
	}

	interface VersionDependency {
		project_id?: string;
		version_id?: string;
		dependency_type?: string;
	}

	// Recursively installs required (transitive) dependencies of an added mod
	// for the pack's loader/MC version. Skips optional deps, versions already
	// in the pack, and cycles via the visited set. Returns the count added.
	async function resolveAndAddDependencies(
		deps: VersionDependency[],
		platform: string,
		side: 'both' | 'client' | 'server',
		depth: number,
		visited: Set<string>
	): Promise<number> {
		if (!pack || depth > 3) return 0;
		let added = 0;
		const required = (deps || []).filter(
			(d) => (d.dependency_type || 'required').toLowerCase() === 'required' && d.project_id
		);
		for (const dep of required) {
			const depId = dep.project_id as string;
			const key = `${platform.toLowerCase()}:${depId.toLowerCase()}`;
			if (visited.has(key)) continue;
			visited.add(key);
			if (
				(pack.mods || []).some(
					(m) => m.project_id === depId || m.slug === depId.toLowerCase()
				)
			) {
				continue;
			}
			try {
				const vParams = new URLSearchParams({
					platform,
					loader: pack.mod_loader.toLowerCase(),
					mc_version: pack.mc_version
				});
				const vRes = await apiFetch(`/api/v1/servers/none/mods/${depId}/versions?${vParams.toString()}`);
				if (!vRes.ok) continue;
				const vData = await vRes.json();
				const versions = vData.versions || [];
				if (versions.length === 0) continue;
				const ver = versions[0];
				const depSlug = depId.toLowerCase();
				if (visited.has(`slug:${depSlug}`)) continue;
				visited.add(`slug:${depSlug}`);
				const depMod: ModItem = {
					slug: depSlug,
					name: ver.name || ver.version_number || depSlug,
					file_name: ver.file_name || ver.fileName || 'mod.jar',
					side,
					platform,
					project_id: depId,
					version_id: ver.id,
					download_url: ver.download_url || ver.downloadUrl || '',
					file_size: ver.file_size || ver.fileSize,
					pinned: false
				};
				const addRes = await apiFetch(`/api/v1/packwiz/packs/${packId}/mods`, {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify(depMod)
				});
				if (!addRes.ok) continue;
				added++;
				added += await resolveAndAddDependencies(ver.dependencies || [], platform, side, depth + 1, visited);
			} catch (err) {
				console.warn(`Skipping unresolvable dependency ${depId}:`, err);
			}
		}
		return added;
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

<svelte:head>
	<title>{pack ? pack.name : 'Studio'} - Modpack Studio</title>
</svelte:head>

<div class="h-full flex-1 space-y-6 font-sans text-[#f4f4f4] rounded-none">
	<!-- Top Header Bar -->
	<div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4 border-b border-[#393939] pb-5 rounded-none">
		<div class="flex items-center gap-4">
			<CarbonButton
				kind="ghost"
				size="md"
				iconOnly
				onclick={() => goto('/modpacks/studio')}
				class="rounded-none text-[#c6c6c6] hover:text-white"
				title="Back to Studio List"
			>
				<ArrowLeft class="h-5 w-5" />
			</CarbonButton>

			{#if pack}
				<div class="space-y-0.5">
					<div class="flex items-center gap-3">
						<h1 class="text-3xl font-semibold tracking-tight text-white">{pack.name}</h1>
						<CarbonTag type="blue" size="sm" class="uppercase">
							{pack.mod_loader}
						</CarbonTag>
						<CarbonTag type="gray" size="sm">
							MC {pack.mc_version}
						</CarbonTag>
					</div>
					<p class="text-xs text-[#a8a8a8]">
						Packwiz Studio Project · v{pack.version} by {pack.author || 'Admin'}
					</p>
				</div>
			{/if}
		</div>

		<div class="flex items-center gap-2 flex-wrap">
			{#if pack}
				<CarbonButton
					kind="tertiary"
					size="sm"
					class="rounded-none"
					onclick={() => (deployDialogOpen = true)}
				>
					<Rocket class="mr-1.5 h-3.5 w-3.5 text-[#0f62fe]" />
					Deploy to Server
				</CarbonButton>

				<CarbonButton
					kind="tertiary"
					size="sm"
					class="rounded-none text-xs h-8"
					onclick={() => exportPack('packwiz')}
					disabled={exportingPack === 'packwiz'}
					title="Export native Packwiz .zip archive"
				>
					{#if exportingPack === 'packwiz'}
						<Loader2 class="mr-1.5 h-3.5 w-3.5 animate-spin" />
					{:else}
						<Download class="mr-1.5 h-3.5 w-3.5 text-[#0f62fe]" />
					{/if}
					Packwiz .zip
				</CarbonButton>

				<CarbonButton
					kind="tertiary"
					size="sm"
					class="rounded-none text-xs h-8"
					onclick={() => exportPack('mrpack')}
					disabled={exportingPack === 'mrpack'}
					title="Export Modrinth .mrpack"
				>
					{#if exportingPack === 'mrpack'}
						<Loader2 class="mr-1.5 h-3.5 w-3.5 animate-spin" />
					{:else}
						<Download class="mr-1.5 h-3.5 w-3.5" />
					{/if}
					.mrpack
				</CarbonButton>

				<CarbonButton
					kind="tertiary"
					size="sm"
					class="rounded-none text-xs h-8"
					onclick={() => exportPack('curseforge')}
					disabled={exportingPack === 'curseforge'}
					title="Export CurseForge .zip"
				>
					{#if exportingPack === 'curseforge'}
						<Loader2 class="mr-1.5 h-3.5 w-3.5 animate-spin" />
					{:else}
						<Download class="mr-1.5 h-3.5 w-3.5" />
					{/if}
					CurseForge .zip
				</CarbonButton>

				<CarbonButton
					kind="primary"
					size="sm"
					class="rounded-none"
					onclick={saveMetadata}
					disabled={saving}
				>
					{#if saving}
						<Loader2 class="mr-2 h-4 w-4 animate-spin" />
						Saving...
					{:else}
						<Save class="mr-2 h-4 w-4" />
						Save
					{/if}
				</CarbonButton>
			{/if}
		</div>
	</div>

	{#if loading}
		<div class="flex items-center justify-center py-24">
			<CarbonInlineLoading description="Loading studio workspace..." />
		</div>
	{:else if pack}
		<!-- Studio Grid Layout -->
		<div class="grid grid-cols-1 lg:grid-cols-4 gap-6">
			<!-- Modpack Settings Sidebar Tile -->
			<CarbonTile class="lg:col-span-1 h-fit rounded-none border-[#393939] space-y-4">
				<div class="pb-3 border-b border-[#393939]">
					<h2 class="text-base font-semibold text-white">Modpack Settings</h2>
					<p class="text-xs text-[#a8a8a8]">Configure runtime parameters</p>
				</div>

				<div class="space-y-3">
					<CarbonTextInput
						label="Pack Name"
						bind:value={pack.name}
					/>

					<CarbonTextInput
						label="Author"
						bind:value={pack.author}
					/>

					<CarbonTextInput
						label="Pack Version"
						bind:value={pack.version}
					/>

					<CarbonSelect
						label="Minecraft Version"
						bind:value={pack.mc_version}
					>
						{#each MC_VERSIONS as v}
							<option value={v}>{v}</option>
						{/each}
					</CarbonSelect>

					<CarbonSelect
						label="Mod Loader"
						bind:value={pack.mod_loader}
					>
						<option value="fabric">Fabric</option>
						<option value="neoforge">NeoForge</option>
						<option value="forge">Forge</option>
						<option value="quilt">Quilt</option>
					</CarbonSelect>

					<CarbonSelect
						label="Loader Version"
						bind:value={pack.loader_version}
						helperText={loadingLoaderVersions ? 'Fetching versions...' : undefined}
					>
						{#each availableLoaderVersions as v}
							<option value={v}>{v}</option>
						{/each}
					</CarbonSelect>

					<CarbonButton
						kind="primary"
						size="md"
						class="w-full justify-center rounded-none mt-2"
						onclick={saveMetadata}
						disabled={saving}
					>
						{#if saving}
							<Loader2 class="mr-2 h-4 w-4 animate-spin" />
							Saving...
						{:else}
							<Save class="mr-2 h-4 w-4" />
							Save Settings
						{/if}
					</CarbonButton>
				</div>

				<!-- Carbon Accordion for Pack Manifest Quick Insights -->
				<div class="pt-4 border-t border-[#393939]">
					<CarbonAccordion>
						<CarbonAccordionItem title="Manifest Breakdown">
							<div class="space-y-2 text-xs font-mono">
								<div class="flex justify-between text-[#c6c6c6]">
									<span>Total Mods:</span>
									<span class="text-white font-bold">{pack.mods.length}</span>
								</div>
								<div class="flex justify-between text-[#c6c6c6]">
									<span>Client Only:</span>
									<span>{pack.mods.filter((m) => m.side === 'client').length}</span>
								</div>
								<div class="flex justify-between text-[#c6c6c6]">
									<span>Server Only:</span>
									<span>{pack.mods.filter((m) => m.side === 'server').length}</span>
								</div>
								<div class="flex justify-between text-[#c6c6c6]">
									<span>Both Sides:</span>
									<span>{pack.mods.filter((m) => m.side === 'both').length}</span>
								</div>
								<div class="flex justify-between text-[#c6c6c6]">
									<span>Pinned Versions:</span>
									<span>{pack.mods.filter((m) => m.pinned).length}</span>
								</div>
							</div>
						</CarbonAccordionItem>
					</CarbonAccordion>
				</div>
			</CarbonTile>

			<!-- Studio Main Workspace Area -->
			<div class="lg:col-span-3 space-y-4">
				<!-- Workspace CarbonTabs -->
				<CarbonTabs
					tabs={studioTabs}
					selectedTab={activeTab}
					onselect={(id) => (activeTab = id as any)}
				/>

				<!-- TAB 1: MODS MANAGEMENT -->
				{#if activeTab === 'mods'}
					<CarbonTile class="p-0 rounded-none border-[#393939]">
						<div class="p-4 border-b border-[#393939] bg-[#262626] flex flex-col sm:flex-row sm:items-center justify-between gap-3">
							<div class="space-y-0.5">
								<div class="flex items-center gap-2">
									<h2 class="text-base font-semibold text-white">Mods in Modpack</h2>
									<CarbonTag type="cyan" size="sm">{pack.mods.length}</CarbonTag>
								</div>
								<p class="text-xs text-[#a8a8a8]">
									Manage server/client sides, pinning, dependency versions, and update checks.
								</p>
							</div>

							<div class="flex items-center gap-2 flex-wrap">
								<div class="w-44 sm:w-56">
									<CarbonSearch
										placeholder="Filter mods..."
										bind:value={modFilter}
										size="sm"
									/>
								</div>

								<CarbonButton
									kind="tertiary"
									size="sm"
									class="rounded-none"
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
								</CarbonButton>

								<CarbonButton
									kind="primary"
									size="sm"
									class="rounded-none"
									onclick={() => (searchDrawerOpen = true)}
								>
									<Plus class="mr-1.5 h-4 w-4" />
									Add Mods
								</CarbonButton>
							</div>
						</div>

						<!-- Filter pills bar (0px border radius) -->
						<div class="flex items-center gap-1.5 p-3 bg-[#161616] border-b border-[#393939] flex-wrap text-xs">
							<button
								type="button"
								onclick={() => (sideFilter = 'all')}
								class="px-2.5 py-1 text-xs font-mono transition-colors cursor-pointer rounded-none border {sideFilter === 'all' ? 'bg-[#0f62fe] border-[#0f62fe] text-white' : 'bg-[#262626] border-[#393939] text-[#c6c6c6] hover:bg-[#353535]'}"
							>
								All ({pack.mods.length})
							</button>
							<button
								type="button"
								onclick={() => (sideFilter = 'both')}
								class="px-2.5 py-1 text-xs font-mono transition-colors cursor-pointer rounded-none border {sideFilter === 'both' ? 'bg-[#0f62fe] border-[#0f62fe] text-white' : 'bg-[#262626] border-[#393939] text-[#c6c6c6] hover:bg-[#353535]'}"
							>
								Both ({pack.mods.filter((m) => m.side === 'both').length})
							</button>
							<button
								type="button"
								onclick={() => (sideFilter = 'client')}
								class="px-2.5 py-1 text-xs font-mono transition-colors cursor-pointer rounded-none border {sideFilter === 'client' ? 'bg-[#0f62fe] border-[#0f62fe] text-white' : 'bg-[#262626] border-[#393939] text-[#c6c6c6] hover:bg-[#353535]'}"
							>
								Client Only ({pack.mods.filter((m) => m.side === 'client').length})
							</button>
							<button
								type="button"
								onclick={() => (sideFilter = 'server')}
								class="px-2.5 py-1 text-xs font-mono transition-colors cursor-pointer rounded-none border {sideFilter === 'server' ? 'bg-[#0f62fe] border-[#0f62fe] text-white' : 'bg-[#262626] border-[#393939] text-[#c6c6c6] hover:bg-[#353535]'}"
							>
								Server Only ({pack.mods.filter((m) => m.side === 'server').length})
							</button>
							<button
								type="button"
								onclick={() => (sideFilter = 'pinned')}
								class="px-2.5 py-1 text-xs font-mono transition-colors cursor-pointer rounded-none border {sideFilter === 'pinned' ? 'bg-[#0f62fe] border-[#0f62fe] text-white' : 'bg-[#262626] border-[#393939] text-[#c6c6c6] hover:bg-[#353535]'}"
							>
								Pinned ({pack.mods.filter((m) => m.pinned).length})
							</button>
						</div>

						<!-- Batch Action Bar -->
						{#if selectedSlugs.length > 0}
							<div class="flex items-center justify-between bg-[#0043ce]/20 border-b border-[#0f62fe] p-2.5 rounded-none">
								<span class="text-xs font-semibold text-[#78a9ff] font-mono">
									{selectedSlugs.length} mod(s) selected
								</span>
								<div class="flex items-center gap-1.5 flex-wrap">
									<CarbonButton size="sm" kind="secondary" class="h-7 text-xs rounded-none" onclick={() => runBatchAction('set_side', 'both')}>
										Set Both
									</CarbonButton>
									<CarbonButton size="sm" kind="secondary" class="h-7 text-xs rounded-none" onclick={() => runBatchAction('set_side', 'server')}>
										Set Server
									</CarbonButton>
									<CarbonButton size="sm" kind="secondary" class="h-7 text-xs rounded-none" onclick={() => runBatchAction('set_side', 'client')}>
										Set Client
									</CarbonButton>
									<CarbonButton size="sm" kind="secondary" class="h-7 text-xs rounded-none" onclick={() => runBatchAction('pin')}>
										Pin
									</CarbonButton>
									<CarbonButton size="sm" kind="secondary" class="h-7 text-xs rounded-none" onclick={() => runBatchAction('unpin')}>
										Unpin
									</CarbonButton>
									<CarbonButton size="sm" kind="danger" class="h-7 text-xs rounded-none" onclick={() => runBatchAction('remove')}>
										Remove
									</CarbonButton>
									<CarbonButton size="sm" kind="ghost" class="h-7 text-xs rounded-none" onclick={() => (selectedSlugs = [])}>
										Clear
									</CarbonButton>
								</div>
							</div>
						{/if}

						{#if filteredMods.length === 0}
							<div class="py-20 text-center text-[#8d8d8d] space-y-3">
								<Package class="mx-auto h-10 w-10 text-[#525252]" />
								<p class="text-sm font-semibold text-white">No mods match current filter</p>
								<CarbonButton size="sm" class="rounded-none" onclick={() => (searchDrawerOpen = true)}>
									<Plus class="mr-1.5 h-4 w-4" />
									Browse & Add Mods
								</CarbonButton>
							</div>
						{:else}
							<div class="overflow-x-auto">
								<table class="w-full text-left text-xs border-collapse">
									<thead class="bg-[#393939] text-[#f4f4f4] border-b border-[#525252] uppercase font-semibold tracking-wider">
										<tr>
											<th class="py-2.5 px-3 w-8">
												<input
													type="checkbox"
													class="rounded-none border-[#8d8d8d] bg-[#262626] cursor-pointer"
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
									<tbody class="divide-y divide-[#393939] bg-[#262626] text-[#f4f4f4]">
										{#each filteredMods as mod (mod.slug)}
											<tr class="hover:bg-[#353535] transition-colors {selectedSlugs.includes(mod.slug) ? 'bg-[#0f62fe]/10' : ''}">
												<td class="py-3 px-3">
													<input
														type="checkbox"
														class="rounded-none border-[#8d8d8d] bg-[#262626] cursor-pointer"
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
												<td class="py-3 px-3 font-semibold text-white">
													<div class="flex items-center gap-2">
														<Package class="h-4 w-4 text-[#0f62fe] shrink-0" />
														<span>{mod.name}</span>
													</div>
												</td>
												<td class="py-3 px-3 font-mono text-[11px] text-[#a8a8a8] max-w-[180px] truncate">
													{mod.file_name}
												</td>
												<td class="py-3 px-3">
													<CarbonTag type="cyan" size="sm" class="uppercase">
														{mod.platform}
													</CarbonTag>
												</td>
												<td class="py-3 px-3">
													<button
														type="button"
														onclick={() => toggleSide(mod)}
														class="inline-flex items-center font-mono text-[10px] font-semibold px-2 py-0.5 border cursor-pointer rounded-none
															{mod.side === 'both' ? 'bg-[#0f62fe]/20 border-[#0f62fe] text-[#78a9ff]' : ''}
															{mod.side === 'server' ? 'bg-[#198038]/20 border-[#198038] text-[#6fdc8c]' : ''}
															{mod.side === 'client' ? 'bg-[#8a3ffc]/20 border-[#8a3ffc] text-[#d4bbff]' : ''}"
														title="Click to toggle side (Both -> Server -> Client)"
													>
														{mod.side.toUpperCase()}
													</button>
												</td>
												<td class="py-3 px-3">
													<button
														type="button"
														onclick={() => togglePin(mod)}
														class="p-1 text-[#8d8d8d] hover:text-white transition-colors cursor-pointer rounded-none"
														title={mod.pinned ? 'Version pinned' : 'Click to pin version'}
														aria-label={mod.pinned ? `Unpin version for ${mod.name}` : `Pin version for ${mod.name}`}
													>
														{#if mod.pinned}
															<Pin class="h-3.5 w-3.5 text-[#0f62fe]" />
														{:else}
															<PinOff class="h-3.5 w-3.5 opacity-40 hover:opacity-100" />
														{/if}
													</button>
												</td>
												<td class="py-3 px-3">
													{#if updatesMap[mod.slug]?.update_available}
														<CarbonTag type="magenta" size="sm">Update Ready</CarbonTag>
													{:else}
														<span class="text-[11px] text-[#8d8d8d] font-mono">Up to date</span>
													{/if}
												</td>
												<td class="py-3 px-3 text-right">
													<CarbonButton
														kind="ghost"
														size="sm"
														iconOnly
														class="rounded-none text-[#da1e28] hover:bg-[#da1e28]/20"
														onclick={() => removeMod(mod)}
														title="Remove mod"
													>
														<Trash2 class="h-3.5 w-3.5" />
													</CarbonButton>
												</td>
											</tr>
										{/each}
									</tbody>
								</table>
							</div>
						{/if}
					</CarbonTile>
				{/if}

				<!-- TAB 2: OVERRIDES & CONFIGS -->
				{#if activeTab === 'overrides'}
					<CarbonTile class="p-0 rounded-none border-[#393939]">
						<div class="p-4 border-b border-[#393939] bg-[#262626] flex items-center justify-between">
							<div>
								<h2 class="text-base font-semibold text-white">Overrides & Config Files</h2>
								<p class="text-xs text-[#a8a8a8]">
									Packwiz overrides bundled with the modpack: server configurations, datapacks, and scripts.
								</p>
							</div>
							<CarbonButton size="sm" class="rounded-none" onclick={() => (uploadFileDialogOpen = true)}>
								<Plus class="mr-1.5 h-4 w-4" />
								Add Override File
							</CarbonButton>
						</div>

						{#if loadingFiles}
							<div class="flex items-center justify-center py-16">
								<CarbonInlineLoading description="Scanning override files..." />
							</div>
						{:else if packFiles.length === 0}
							<div class="py-16 text-center text-[#8d8d8d] space-y-3">
								<FolderGit2 class="mx-auto h-10 w-10 text-[#525252]" />
								<p class="text-sm font-semibold text-white">No override files found</p>
								<p class="text-xs text-[#a8a8a8] max-w-sm mx-auto">Add custom server configs, datapacks, or options to bundle them directly into the modpack.</p>
								<CarbonButton size="sm" kind="secondary" class="rounded-none" onclick={() => (uploadFileDialogOpen = true)}>
									<Plus class="mr-1.5 h-4 w-4" />
									Add Config File
								</CarbonButton>
							</div>
						{:else}
							<div class="overflow-x-auto">
								<table class="w-full text-left text-xs border-collapse">
									<thead class="bg-[#393939] text-[#f4f4f4] border-b border-[#525252] uppercase font-semibold">
										<tr>
											<th class="py-2.5 px-4">File Path</th>
											<th class="py-2.5 px-4">Category</th>
											<th class="py-2.5 px-4">Size</th>
											<th class="py-2.5 px-4 text-right">Actions</th>
										</tr>
									</thead>
									<tbody class="divide-y divide-[#393939] bg-[#262626] text-[#f4f4f4]">
										{#each packFiles as file (file.path)}
											<tr class="hover:bg-[#353535] transition-colors">
												<td class="py-2.5 px-4 font-mono text-[11px] text-white font-semibold">
													{file.path}
												</td>
												<td class="py-2.5 px-4">
													<CarbonTag type="gray" size="sm" class="capitalize">
														{file.category}
													</CarbonTag>
												</td>
												<td class="py-2.5 px-4 text-[#a8a8a8] font-mono">
													{formatBytes(file.size)}
												</td>
												<td class="py-2.5 px-4 text-right">
													<CarbonButton
														kind="ghost"
														size="sm"
														iconOnly
														class="rounded-none text-[#da1e28] hover:bg-[#da1e28]/20"
														onclick={() => handleDeleteFile(file.path)}
														title="Delete file"
													>
														<Trash2 class="h-3.5 w-3.5" />
													</CarbonButton>
												</td>
											</tr>
										{/each}
									</tbody>
								</table>
							</div>
						{/if}
					</CarbonTile>
				{/if}

				<!-- TAB 3: VERSION MIGRATION -->
				{#if activeTab === 'migrate'}
					<CarbonTile class="rounded-none border-[#393939] space-y-4">
						<div class="pb-3 border-b border-[#393939]">
							<h2 class="text-base font-semibold text-white">Minecraft Version Migration Assistant</h2>
							<p class="text-xs text-[#a8a8a8]">
								Simulate upgrading all pack mods to a newer Minecraft release or loader before applying changes.
							</p>
						</div>

						<div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
							<CarbonSelect
								label="Target Minecraft Version"
								bind:value={migrateMC}
							>
								{#each MC_VERSIONS as v}
									<option value={v}>{v}</option>
								{/each}
							</CarbonSelect>

							<CarbonSelect
								label="Target Mod Loader"
								bind:value={migrateLoader}
							>
								<option value="fabric">Fabric</option>
								<option value="neoforge">NeoForge</option>
								<option value="forge">Forge</option>
								<option value="quilt">Quilt</option>
							</CarbonSelect>
						</div>

						<div class="flex items-center gap-3 pt-2">
							<CarbonButton
								kind="secondary"
								size="sm"
								class="rounded-none"
								onclick={() => simulateMigration(false)}
								disabled={migrating}
							>
								{#if migrating}
									<Loader2 class="mr-1.5 h-3.5 w-3.5 animate-spin" />
								{:else}
									<Sparkles class="mr-1.5 h-3.5 w-3.5 text-[#0f62fe]" />
								{/if}
								Simulate Compatibility
							</CarbonButton>

							<CarbonButton
								kind="primary"
								size="sm"
								class="rounded-none"
								onclick={() => simulateMigration(true)}
								disabled={migrating}
							>
								<ArrowRightLeft class="mr-1.5 h-3.5 w-3.5" />
								Apply Migration
							</CarbonButton>
						</div>

						{#if migrationReport}
							<div class="border border-[#393939] bg-[#161616] p-4 space-y-3 mt-4 rounded-none">
								<div class="flex items-center justify-between">
									<div class="flex items-center gap-2">
										<CarbonTag type="green" size="md">
											{migrationReport.compatible_count} Compatible
										</CarbonTag>
										<CarbonTag type="red" size="md">
											{migrationReport.incompatible_count} Missing Version
										</CarbonTag>
									</div>
									<span class="text-xs text-[#8d8d8d] font-mono">
										Target: MC {migrationReport.target_mc} ({migrationReport.target_loader})
									</span>
								</div>

								<!-- Carbon Structured List for Migration Details -->
								<CarbonStructuredList class="mt-3">
									{#snippet header()}
										<div class="flex justify-between w-full">
											<span>Mod Slug / Name</span>
											<span>Target Compatibility Status</span>
										</div>
									{/snippet}

									{#each migrationReport.mods as m}
										<div class="flex items-center justify-between py-2.5 px-4 text-xs font-mono">
											<span class="font-medium text-white">{m.name}</span>
											{#if m.compatible}
												<span class="text-[#6fdc8c] flex items-center gap-1">
													<CheckCircle2 class="h-3.5 w-3.5" />
													Compatible
												</span>
											{:else}
												<span class="text-[#ff8389] flex items-center gap-1">
													<AlertTriangle class="h-3.5 w-3.5" />
													No version for {migrationReport.target_mc}
												</span>
											{/if}
										</div>
									{/each}
								</CarbonStructuredList>
							</div>
						{/if}
					</CarbonTile>
				{/if}

				<!-- TAB 4: MAINTENANCE & RAW TOML -->
				{#if activeTab === 'maintenance'}
					<CarbonTile class="rounded-none border-[#393939] space-y-6">
						<div class="pb-3 border-b border-[#393939]">
							<h2 class="text-base font-semibold text-white">Packwiz Maintenance & Live Serving</h2>
							<p class="text-xs text-[#a8a8a8]">
								Inspect raw generated TOML files, rebuild indices, and view live container bootstrap URLs.
							</p>
						</div>

						<!-- Live Serving URL Tile -->
						<div class="p-4 bg-[#161616] border border-[#393939] space-y-2 rounded-none">
							<div class="flex items-center justify-between">
								<span class="text-xs font-semibold text-white">Live Container Bootstrap URL (PACKWIZ_URL)</span>
								<CarbonButton
									kind="ghost"
									size="sm"
									class="rounded-none text-xs"
									onclick={() => {
										const u = `${typeof window !== 'undefined' ? window.location.origin : ''}/api/v1/packwiz/${pack?.id}/pack.toml`;
										navigator.clipboard.writeText(u);
										toast.success('Copied PACKWIZ_URL to clipboard!');
									}}
								>
									<Copy class="mr-1.5 h-3.5 w-3.5" />
									Copy URL
								</CarbonButton>
							</div>
							<div class="font-mono text-xs p-2.5 bg-[#262626] border border-[#393939] text-[#6fdc8c] select-all break-all rounded-none">
								{typeof window !== 'undefined' ? `${window.location.origin}/api/v1/packwiz/${pack?.id}/pack.toml` : `/api/v1/packwiz/${pack?.id}/pack.toml`}
							</div>
							<p class="text-[11px] text-[#8d8d8d]">
								Set this URL in Docker container environments or packwiz-installer to dynamically provision this modpack on startup.
							</p>
						</div>

						<!-- Refresh Index -->
						<div class="flex items-center justify-between p-4 bg-[#161616] border border-[#393939] rounded-none">
							<div class="space-y-0.5">
								<h3 class="text-sm font-semibold text-white">Refresh Packwiz Index</h3>
								<p class="text-xs text-[#a8a8a8]">
									Recalculate SHA256 hashes for all .pw.toml and override files according to packwiz refresh.
								</p>
							</div>
							<CarbonButton kind="secondary" size="sm" class="rounded-none" onclick={handleRefreshPack} disabled={refreshing}>
								{#if refreshing}
									<Loader2 class="mr-1.5 h-3.5 w-3.5 animate-spin" />
								{:else}
									<RefreshCw class="mr-1.5 h-3.5 w-3.5 text-[#0f62fe]" />
								{/if}
								Run Refresh
							</CarbonButton>
						</div>

						<!-- Raw TOML Inspector -->
						<div class="space-y-2">
							<div class="flex items-center justify-between">
								<span class="text-xs font-semibold text-white">Raw pack.toml Preview</span>
								<CarbonButton kind="ghost" size="sm" class="rounded-none text-[11px]" onclick={loadRawTomls} disabled={loadingRaw}>
									Reload TOML
								</CarbonButton>
							</div>
							<pre class="font-mono text-[11px] p-3 bg-[#161616] border border-[#393939] text-[#c6c6c6] max-h-48 overflow-y-auto rounded-none">{rawPackToml || 'Loading pack.toml...'}</pre>
						</div>
					</CarbonTile>
				{/if}
			</div>
		</div>
	{/if}
</div>

<!-- Add Mods Modal (Modrinth, CurseForge, URL) with Carbon Design System Fidelity -->
<CarbonModal
	bind:open={searchDrawerOpen}
	title="Add Mods to Packwiz"
	description={pack ? `Search online catalogs for ${pack.mod_loader.toUpperCase()} MC ${pack.mc_version}` : 'Search online mods'}
	hasFooter={false}
	size="4xl"
>
	<div class="space-y-4">
		<!-- Add Mod Source Tabs -->
		<CarbonTabs
			tabs={addModSourceTabs}
			selectedTab={addTab}
			onselect={(id) => {
				addTab = id as any;
				if (addTab !== 'url') searchMods();
			}}
		/>

		{#if addTab !== 'url'}
			<div class="flex items-center gap-2">
				<div class="flex-1">
					<CarbonSearch
						placeholder="Search online mods..."
						bind:value={searchQuery}
						onkeydown={(e) => e.key === 'Enter' && searchMods()}
						size="md"
					/>
				</div>
				<CarbonButton kind="primary" onclick={searchMods} disabled={searching} class="rounded-none">
					{#if searching}
						<Loader2 class="h-4 w-4 animate-spin" />
					{:else}
						Search
					{/if}
				</CarbonButton>
			</div>

			<!-- Search Results List -->
			<div class="mt-4 overflow-y-auto space-y-2.5 min-h-[300px] max-h-[460px]">
				{#if searchError}
					<div class="p-4 bg-[#da1e28]/10 border-l-4 border-[#da1e28] text-xs text-[#ff8389] rounded-none">
						{searchError}
					</div>
				{:else if searching}
					<div class="flex items-center justify-center py-16">
						<CarbonInlineLoading description="Searching mods..." />
					</div>
				{:else if searchResults.length === 0}
					<div class="py-16 text-center text-[#8d8d8d] space-y-2">
						<Package class="mx-auto h-10 w-10 text-[#525252]" />
						<p class="text-sm font-semibold text-white">Enter a search query</p>
					</div>
				{:else}
					{#each searchResults as item (item.id)}
						<CarbonTile class="p-3 rounded-none border-[#393939] hover:border-[#525252] flex items-start justify-between gap-3">
							<div class="flex items-start gap-3 min-w-0 flex-1">
								{#if item.icon_url}
									<img src={item.icon_url} alt={item.title} class="h-10 w-10 object-contain bg-[#161616] p-1 shrink-0 rounded-none border border-[#393939]" />
								{:else}
									<div class="h-10 w-10 bg-[#161616] border border-[#393939] flex items-center justify-center shrink-0 rounded-none text-[#525252]">
										<Package class="h-5 w-5" />
									</div>
								{/if}

								<div class="space-y-0.5 min-w-0 flex-1">
									<div class="flex items-center gap-2">
										<span class="font-semibold text-sm text-white truncate">{item.title}</span>
										<span class="text-[11px] text-[#8d8d8d]">by {item.author}</span>
									</div>
									<p class="text-xs text-[#a8a8a8] line-clamp-1">{item.description}</p>
									<div class="flex items-center gap-1.5 pt-1">
										<CarbonTag type="cyan" size="sm">
											<Download class="mr-0.5 h-2.5 w-2.5" />
											{formatDownloads(item.downloads)}
										</CarbonTag>
										{#each (item.categories || []).slice(0, 2) as cat}
											<CarbonTag type="gray" size="sm">{cat}</CarbonTag>
										{/each}
									</div>
								</div>
							</div>

							<div class="flex items-center gap-1.5 shrink-0">
								<CarbonButton size="sm" kind="tertiary" class="rounded-none text-xs px-2" onclick={() => addModToPack(item, 'client')} title="Add as client-only">
									+ Client
								</CarbonButton>
								<CarbonButton size="sm" kind="tertiary" class="rounded-none text-xs px-2" onclick={() => addModToPack(item, 'server')} title="Add as server-only">
									+ Server
								</CarbonButton>
								<CarbonButton size="sm" kind="primary" class="rounded-none text-xs px-2.5" onclick={() => addModToPack(item, 'both')} title="Add for both">
									<Plus class="mr-1 h-3.5 w-3.5" />
									Add (Both)
								</CarbonButton>
							</div>
						</CarbonTile>
					{/each}
				{/if}
			</div>
		{:else}
			<!-- Direct URL Tab -->
			<div class="space-y-4 py-2">
				<CarbonTextInput
					label="Direct Download URL (*.jar) *"
					placeholder="https://example.com/mods/my-custom-mod.jar"
					bind:value={urlModDownloadUrl}
					helperText="Direct download link. Packwiz will download and compute SHA256 hashes automatically."
				/>

				<div class="grid grid-cols-2 gap-4">
					<CarbonTextInput
						label="Mod Name (Optional)"
						placeholder="Custom Mod"
						bind:value={urlModName}
					/>
					<CarbonTextInput
						label="File Name (Optional)"
						placeholder="mod.jar"
						bind:value={urlModFileName}
					/>
				</div>

				<div class="grid grid-cols-2 gap-4">
					<CarbonSelect
						label="Side"
						bind:value={urlModSide}
					>
						<option value="both">Both (Client & Server)</option>
						<option value="server">Server Only</option>
						<option value="client">Client Only</option>
					</CarbonSelect>

					<div class="flex items-center gap-2 pt-6">
						<input type="checkbox" id="urlPin" bind:checked={urlModPinned} class="rounded-none border-[#8d8d8d] bg-[#262626] cursor-pointer" />
						<label for="urlPin" class="text-xs text-[#c6c6c6] cursor-pointer select-none">Pin version (prevent automatic updates)</label>
					</div>
				</div>

				<CarbonButton class="w-full justify-center rounded-none mt-2" onclick={handleAddUrlMod} disabled={addingUrlMod}>
					{#if addingUrlMod}
						<Loader2 class="mr-2 h-4 w-4 animate-spin" />
						Fetching & Hashing...
					{:else}
						<Plus class="mr-1.5 h-4 w-4" />
						Add Mod from URL
					{/if}
				</CarbonButton>
			</div>
		{/if}
	</div>
</CarbonModal>

<!-- Upload / Add Override File Modal -->
<CarbonModal
	bind:open={uploadFileDialogOpen}
	title="Add Override File"
	description="Bundle a custom configuration or options file into your Packwiz modpack"
	hasFooter={false}
	size="lg"
>
	<div class="space-y-4">
		<CarbonTextInput
			label="Relative File Path *"
			placeholder="e.g. config/options.txt"
			bind:value={uploadFilePath}
		/>

		<div class="space-y-1.5">
			<label class="text-xs font-normal text-[#c6c6c6] tracking-[0.32px]">File Content (Text / JSON / YAML)</label>
			<textarea
				bind:value={uploadFileContent}
				rows={8}
				class="w-full p-3 bg-[#262626] border-b border-[#8d8d8d] text-xs font-mono text-[#f4f4f4] rounded-none focus:outline-none focus:border-[#0f62fe]"
				placeholder="Enter configuration content here..."
			></textarea>
		</div>

		<div class="flex items-center justify-end gap-3 pt-4 border-t border-[#393939]">
			<CarbonButton
				kind="secondary"
				onclick={() => (uploadFileDialogOpen = false)}
				disabled={savingFile}
				class="rounded-none"
			>
				Cancel
			</CarbonButton>
			<CarbonButton
				kind="primary"
				onclick={handleSaveFile}
				disabled={savingFile}
				class="rounded-none"
			>
				{#if savingFile}
					<Loader2 class="mr-2 h-4 w-4 animate-spin" />
					Saving...
				{:else}
					Save File
				{/if}
			</CarbonButton>
		</div>
	</div>
</CarbonModal>

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
