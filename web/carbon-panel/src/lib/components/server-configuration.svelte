<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { SvelteMap, SvelteSet, SvelteURL } from 'svelte/reactivity';
	import { rpcClient } from '$lib/api/rpc-client';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import { toast } from 'svelte-sonner';
	import {
		Save,
		RefreshCw,
		Loader2,
		Link,
		CircleDot,
		Circle,
		Send,
		KeyRound,
		ChevronDown,
		ChevronRight,
		Wand2
	} from '@lucide/svelte';
	import { copyToClipboard } from '$lib/utils/clipboard';
	import { CarbonInlineLoading } from '$lib/components/carbon';
	import type { Server } from '$lib/proto/carbonpanel/v1/common_pb';
	import { ServerStatus } from '$lib/proto/carbonpanel/v1/common_pb';
	import type { ConfigCategory, ConfigProperty } from '$lib/proto/carbonpanel/v1/config_pb';
	import ScrollToTop from './scroll-to-top.svelte';

	interface Props {
		server?: Server;
		config?: ConfigCategory[];
		onSave?: (updates: Record<string, string>) => Promise<void>;
		saving?: boolean;
	}

	let { server, config, onSave, saving: externalSaving = false }: Props = $props();

	// State
	let loading = $state(false);
	let saving = $state(false);
	let syncing = $state(false);
	let categories = $state<ConfigCategory[]>([]);
	let activeCategory = $state<string>('');
	let highlightedField = $state<string | null>(null);

	// Track original and current values
	let originalValues = $state<Map<string, string | null>>(new Map());
	let currentValues = $state<Map<string, string | null>>(new Map());
	let originalEnabled = $state<Set<string>>(new Set());
	let currentEnabled = $state<Set<string>>(new Set());

	// Derived state
	let isSaving = $derived(externalSaving || saving);
	let isServerRunning = $derived(!!server && server.status === ServerStatus.RUNNING);

	// Get filtered categories (hide empty ones and filter system fields for global)
	let filteredCategories = $derived.by(() => {
		return categories
			.map((cat) => ({
				...cat,
				properties: !server ? cat.properties.filter((p) => !p.system) : cat.properties
			}))
			.filter((cat) => cat.properties.length > 0);
	});

	// Get current category's properties
	let currentCategoryProps = $derived.by(() => {
		const cat = filteredCategories.find((c) => getCategoryId(c.name) === activeCategory);
		return cat?.properties ?? [];
	});

	// CurseForge primary vs advanced fields separation
	const cfPrimaryKeys = new Set([
		'cfPageUrl',
		'cfSlug',
		'cfFileId',
		'cfForceSynchronize',
		'cfApiKey'
	]);
	let showAdvancedCurseForge = $state(false);

	let primaryCategoryProps = $derived.by(() => {
		if (activeCategory === 'curseforge') {
			return currentCategoryProps.filter((p) => cfPrimaryKeys.has(p.key));
		}
		return currentCategoryProps;
	});

	let advancedCategoryProps = $derived.by(() => {
		if (activeCategory === 'curseforge') {
			return currentCategoryProps.filter((p) => !cfPrimaryKeys.has(p.key));
		}
		return [];
	});

	function parseCurseForgeUrl(url: string): { slug: string; fileId: string } {
		let slug = '';
		let fileId = '';
		if (!url) return { slug, fileId };
		const trimmed = url.trim();

		const fileMatch = trimmed.match(/\/files\/(\d+)/);
		if (fileMatch) {
			fileId = fileMatch[1];
		}

		const slugMatch = trimmed.match(
			/\/(?:minecraft\/(?:modpacks|mc-mods|customization|worlds|texture-packs)|projects)\/([a-zA-Z0-9_\-]+)/
		);
		if (slugMatch) {
			slug = slugMatch[1];
		} else if (
			!trimmed.startsWith('http://') &&
			!trimmed.startsWith('https://') &&
			!trimmed.includes('/')
		) {
			slug = trimmed;
		}

		return { slug, fileId };
	}

	function handleExtractFromPageUrl(url: string) {
		const { slug, fileId } = parseCurseForgeUrl(url);
		const newValues = new SvelteMap(currentValues);
		const newEnabled = new SvelteSet(currentEnabled);
		let updated = false;

		if (slug) {
			newValues.set('cfSlug', slug);
			newEnabled.add('cfSlug');
			updated = true;
		}
		if (fileId) {
			newValues.set('cfFileId', fileId);
			newEnabled.add('cfFileId');
			updated = true;
		}

		if (updated) {
			currentValues = newValues;
			currentEnabled = newEnabled;
			toast.success(
				`Extracted from URL: ${slug ? `Slug: ${slug}` : ''}${slug && fileId ? ', ' : ''}${fileId ? `File ID: ${fileId}` : ''}`
			);
		} else {
			toast.info('Could not find slug or file ID in the entered URL');
		}
	}

	function assumeCfSlugFromServerName() {
		if (!server?.name) return;
		const slug = server.name
			.toLowerCase()
			.trim()
			.replace(/[^a-z0-9]+/g, '-')
			.replace(/^-|-$/g, '');
		if (slug) {
			const newValues = new SvelteMap(currentValues);
			newValues.set('cfSlug', slug);
			currentValues = newValues;
			const newEnabled = new SvelteSet(currentEnabled);
			newEnabled.add('cfSlug');
			currentEnabled = newEnabled;
			toast.success(`Assumed CurseForge slug from server name: "${slug}"`);
		}
	}

	let currentCategoryName = $derived.by(() => {
		const cat = filteredCategories.find((c) => getCategoryId(c.name) === activeCategory);
		return cat?.name ?? '';
	});

	// Calculate modified fields
	let modifiedFields = $derived.by(() => {
		const modified = new SvelteSet<string>();
		for (const [key, value] of currentValues) {
			const origValue = originalValues.get(key);
			const wasEnabled = originalEnabled.has(key);
			const isEnabled = currentEnabled.has(key);

			if (wasEnabled !== isEnabled) {
				modified.add(key);
				continue;
			}

			if (isEnabled && value !== origValue) {
				modified.add(key);
			}
		}
		return modified;
	});

	// Count modified fields per category
	let modifiedCountByCategory = $derived.by(() => {
		const counts = new SvelteMap<string, number>();
		for (const cat of filteredCategories) {
			const catId = getCategoryId(cat.name);
			let count = 0;
			for (const prop of cat.properties) {
				if (modifiedFields.has(prop.key)) count++;
			}
			counts.set(catId, count);
		}
		return counts;
	});

	let hasChanges = $derived(modifiedFields.size > 0);

	// Initialize URL hash handling
	onMount(() => {
		checkUrlHash();
		window.addEventListener('hashchange', checkUrlHash);
		return () => window.removeEventListener('hashchange', checkUrlHash);
	});

	// React to config prop changes (for global settings)
	let previousConfig = $state<ConfigCategory[] | undefined>(undefined);
	$effect(() => {
		if (config && config !== previousConfig) {
			untrack(() => {
				previousConfig = config;
				processConfig(config);
			});
		}
	});

	// Reload when server changes
	let previousServerId = $state<string | undefined>(undefined);
	$effect(() => {
		if (server && server.id !== previousServerId) {
			untrack(() => {
				previousServerId = server!.id;
				loadServerConfig();
			});
		}
	});

	// Set initial active category when categories load
	$effect(() => {
		if (filteredCategories.length > 0 && !activeCategory) {
			untrack(() => {
				activeCategory = getCategoryId(filteredCategories[0].name);
			});
		}
	});

	async function loadServerConfig() {
		if (!server) return;
		loading = true;
		try {
			const response = await rpcClient.config.getServerConfig({ serverId: server.id });
			processConfig(response.categories);
		} catch (error) {
			toast.error('Failed to load server configuration');
			console.error(error);
		} finally {
			loading = false;
		}
	}

	function processConfig(configData: ConfigCategory[]) {
		categories = configData;

		const newOriginalValues = new SvelteMap<string, string | null>();
		const newCurrentValues = new SvelteMap<string, string | null>();
		const newOriginalEnabled = new SvelteSet<string>();
		const newCurrentEnabled = new SvelteSet<string>();

		const isGlobal = !server;

		for (const category of configData) {
			for (const prop of category.properties) {
				if (['id', 'serverId', 'updatedAt'].includes(prop.key)) continue;

				const value = prop.value || null;
				newOriginalValues.set(prop.key, value);
				newCurrentValues.set(prop.key, value);

				const hasValue = value !== null && value !== '';
				const shouldEnable = isGlobal
					? hasValue || prop.required
					: hasValue || prop.required || prop.system;

				if (shouldEnable) {
					newOriginalEnabled.add(prop.key);
					newCurrentEnabled.add(prop.key);
				}
			}
		}

		originalValues = newOriginalValues;
		currentValues = newCurrentValues;
		originalEnabled = newOriginalEnabled;
		currentEnabled = newCurrentEnabled;
	}

	async function handleSave() {
		if (!hasChanges) {
			toast.info('No changes to save');
			return;
		}

		saving = true;
		try {
			const updates: Record<string, string> = {};

			for (const key of modifiedFields) {
				if (currentEnabled.has(key)) {
					const value = currentValues.get(key);
					updates[key] = value ?? '';
				} else {
					updates[key] = '';
				}
			}

			if (onSave) {
				await onSave(updates);
			} else if (server) {
				const response = await rpcClient.config.updateServerConfig({
					serverId: server.id,
					updates
				});
				processConfig(response.categories);
			}

			toast.success('Configuration saved');

			if (server && isServerRunning) {
				toast.info('Restart the server for changes to take effect');
			}
		} catch (error) {
			toast.error('Failed to save configuration');
			console.error(error);
		} finally {
			saving = false;
		}
	}

	async function handleSyncToAllServers(opsAndWhitelistOnly: boolean) {
		if (hasChanges) {
			toast.error('Please save your pending changes first before syncing to servers');
			return;
		}
		syncing = true;
		try {
			const res = await rpcClient.config.syncGlobalSettingsToServers({
				opsAndWhitelistOnly
			});
			toast.success(res.message || 'Settings synced to all servers');
		} catch (error) {
			toast.error('Failed to sync settings to servers');
			console.error(error);
		} finally {
			syncing = false;
		}
	}

	function handleReset() {
		currentValues = new SvelteMap(originalValues);
		currentEnabled = new SvelteSet(originalEnabled);
	}

	function toggleFieldEnabled(key: string, enabled: boolean, prop: ConfigProperty) {
		const newEnabled = new SvelteSet(currentEnabled);
		const newValues = new SvelteMap(currentValues);

		if (enabled) {
			newEnabled.add(key);
			if (!newValues.get(key)) {
				newValues.set(key, prop.defaultValue ?? getDefaultForType(prop.type));
			}
		} else {
			newEnabled.delete(key);
		}

		currentEnabled = newEnabled;
		currentValues = newValues;
	}

	function updateValue(key: string, value: string | boolean) {
		const newValues = new SvelteMap(currentValues);
		const strValue = typeof value === 'boolean' ? String(value) : value;
		newValues.set(key, strValue || null);
		currentValues = newValues;

		// Auto-extract slug and file ID when user enters/pastes a CurseForge Page URL
		if (key === 'cfPageUrl' && typeof strValue === 'string' && strValue.trim().length > 10) {
			const { slug, fileId } = parseCurseForgeUrl(strValue);
			let changed = false;
			if (slug && !currentValues.get('cfSlug')) {
				newValues.set('cfSlug', slug);
				const newEnabled = new SvelteSet(currentEnabled);
				newEnabled.add('cfSlug');
				currentEnabled = newEnabled;
				changed = true;
			}
			if (fileId && !currentValues.get('cfFileId')) {
				newValues.set('cfFileId', fileId);
				const newEnabled = new SvelteSet(currentEnabled);
				newEnabled.add('cfFileId');
				currentEnabled = newEnabled;
				changed = true;
			}
			if (changed) {
				currentValues = newValues;
			}
		}
	}

	function getDefaultForType(type: string): string {
		switch (type) {
			case 'number':
				return '0';
			case 'checkbox':
				return 'false';
			default:
				return '';
		}
	}

	function getDisplayValue(prop: ConfigProperty): string {
		const value = currentValues.get(prop.key);
		const isEnabled = currentEnabled.has(prop.key);

		if (isEnabled && value !== null && value !== undefined) {
			return value;
		}
		return prop.defaultValue ?? '';
	}

	function getBooleanValue(prop: ConfigProperty): boolean {
		const value = getDisplayValue(prop);
		return value.toLowerCase() === 'true';
	}

	function getCategoryId(name: string): string {
		return name.toLowerCase().replace(/\s+/g, '-');
	}

	function selectCategory(categoryId: string) {
		activeCategory = categoryId;
		const url = new SvelteURL(window.location.href);
		url.hash = categoryId;
		window.history.replaceState({}, '', `${url.pathname}${url.search}${url.hash}`);
	}

	async function copyLinkToClipboard(anchor: string) {
		const url = new SvelteURL(window.location.href);
		url.hash = anchor;
		const success = await copyToClipboard(url.toString());
		if (success) {
			toast.success('Link copied to clipboard');
		}
	}

	function checkUrlHash() {
		const hash = window.location.hash.slice(1);
		if (!hash) return;

		setTimeout(() => {
			const matchingCategory = filteredCategories.find((c) => getCategoryId(c.name) === hash);
			if (matchingCategory) {
				activeCategory = hash;
				return;
			}

			for (const cat of filteredCategories) {
				const matchingProp = cat.properties.find((p) => p.key === hash);
				if (matchingProp) {
					activeCategory = getCategoryId(cat.name);
					setTimeout(() => {
						const element = document.getElementById(hash);
						if (element) {
							element.scrollIntoView({ behavior: 'smooth', block: 'center' });
							highlightedField = hash;
							setTimeout(() => {
								highlightedField = null;
							}, 3000);
						}
					}, 100);
					return;
				}
			}
		}, 50);
	}

	function canToggleField(prop: ConfigProperty): boolean {
		if (isServerRunning) return false;
		if (prop.required) return false;
		if (prop.system) return false;
		return true;
	}
</script>

<div
	class="flex h-full flex-col gap-0 rounded-none border border-[#393939] bg-[#262626] shadow-none"
>
	<div class="shrink-0 border-b border-[#393939] bg-[#262626] p-4 sm:p-5">
		<div class="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
			<div>
				<h2 class="font-sans text-lg font-light tracking-tight text-[#f4f4f4]">
					{!server ? 'Default Server Configuration' : 'Server Configuration'}
				</h2>
				<p class="mt-1 text-xs text-[#a8a8a8]">
					{!server
						? 'Configure default values for new servers'
						: 'Configure Minecraft server environment variables'}
				</p>
			</div>
			<div class="flex items-center gap-3">
				{#if hasChanges}
					<span class="font-mono text-xs whitespace-nowrap text-[#ff832b]">
						{modifiedFields.size} unsaved {modifiedFields.size === 1 ? 'change' : 'changes'}
					</span>
				{/if}
				{#if !server}
					<button
						type="button"
						onclick={() => handleSyncToAllServers(true)}
						disabled={loading || syncing || hasChanges}
						title="Propagate configured default Ops and Whitelist to all existing servers"
						class="inline-flex h-8 cursor-pointer items-center gap-2 rounded-none border border-[#393939] bg-[#353535] px-3 font-sans text-xs text-[#f4f4f4] transition-colors hover:bg-[#393939] hover:text-white disabled:cursor-not-allowed disabled:opacity-40"
					>
						{#if syncing}
							<Loader2 class="mr-2 h-3.5 w-3.5 animate-spin" />
						{:else}
							<Send class="mr-2 h-3.5 w-3.5" />
						{/if}
						<span>Sync Ops & Whitelist</span>
					</button>
				{/if}
				<button
					type="button"
					onclick={handleReset}
					disabled={loading || isServerRunning || !hasChanges}
					class="inline-flex h-8 cursor-pointer items-center gap-2 rounded-none border border-[#393939] bg-[#353535] px-3 font-sans text-xs text-[#f4f4f4] transition-colors hover:bg-[#393939] hover:text-white disabled:cursor-not-allowed disabled:opacity-40"
				>
					<RefreshCw class="mr-2 h-3.5 w-3.5" />
					<span>Reset</span>
				</button>
				<button
					type="button"
					onclick={handleSave}
					disabled={loading || isSaving || isServerRunning || !hasChanges}
					class="inline-flex h-8 cursor-pointer items-center gap-2 rounded-none bg-[#0f62fe] px-4 font-sans text-xs font-medium text-white transition-colors hover:bg-[#0353e9] disabled:cursor-not-allowed disabled:opacity-40"
				>
					{#if isSaving}
						<Loader2 class="mr-2 h-3.5 w-3.5 animate-spin" />
					{:else}
						<Save class="mr-2 h-3.5 w-3.5" />
					{/if}
					<span>Save</span>
				</button>
			</div>
		</div>
	</div>

	<div class="my-0 flex-1 overflow-hidden p-0">
		{#if loading}
			<div class="flex items-center justify-center py-16">
				<CarbonInlineLoading description="Loading server configuration..." />
			</div>
		{:else if filteredCategories.length === 0}
			<div class="flex flex-col items-center justify-center py-12 text-[#8d8d8d]">
				<p class="mb-2 text-sm">No configuration found</p>
				<p class="text-xs text-[#6f6f6f]">Unable to load server configuration</p>
			</div>
		{:else}
			<div class="flex h-full">
				<!-- Category Sidebar -->
				<div class="w-48 shrink-0 overflow-y-auto border-r border-[#393939] bg-[#161616]">
					<nav class="space-y-0 p-0">
						{#each filteredCategories as category (category.name)}
							{@const categoryId = getCategoryId(category.name)}
							{@const isActive = activeCategory === categoryId}
							{@const modCount = modifiedCountByCategory.get(categoryId) ?? 0}
							<button
								class="flex w-full cursor-pointer items-center justify-between rounded-none border-b border-[#262626] px-3 py-2.5 text-left text-xs transition-colors
									{isActive
									? 'border-l-4 border-l-[#0f62fe] bg-[#353535] pl-2.5 font-medium text-white'
									: 'border-l-4 border-l-transparent text-[#8d8d8d] hover:bg-[#262626] hover:text-white'}"
								onclick={() => selectCategory(categoryId)}
							>
								<span class="truncate">{category.name}</span>
								{#if modCount > 0}
									<span
										class="ml-2 inline-flex h-4 min-w-4 items-center justify-center rounded-none px-1 font-mono text-[10px] font-medium
										{isActive ? 'bg-[#ff832b] font-semibold text-black' : 'bg-[#ff832b] text-black'}"
									>
										{modCount}
									</span>
								{/if}
							</button>
						{/each}
					</nav>
				</div>

				<!-- Fields Panel -->
				<div class="flex min-w-0 flex-1 flex-col bg-[#161616]">
					<!-- Category Header -->
					<div class="shrink-0 border-b border-[#393939] bg-[#262626] px-4 py-3">
						<h3 class="font-sans text-sm font-normal text-[#f4f4f4]">{currentCategoryName}</h3>
						<p class="mt-0.5 text-xs text-[#a8a8a8]">{currentCategoryProps.length} fields</p>
					</div>

					{#snippet fieldCard(prop: ConfigProperty)}
						{@const isEnabled = currentEnabled.has(prop.key)}
						{@const isModified = modifiedFields.has(prop.key)}
						{@const isHighlighted = highlightedField === prop.key}
						{@const canToggle = canToggleField(prop)}

						<div
							id={prop.key}
							data-field="true"
							class="group rounded-none border border-[#393939] p-4 transition-all duration-150
								{isHighlighted ? 'outline-2 outline-[#0f62fe]' : ''}
								{isModified
								? 'border-l-4 border-l-[#ff832b] bg-[#2d251e]'
								: !isEnabled
									? 'bg-[#1e1e1e] opacity-80'
									: 'bg-[#262626]'}"
						>
							<!-- Field Header -->
							<div class="mb-3 flex items-start justify-between gap-2">
								<div class="min-w-0 flex-1">
									<div class="mb-1 flex flex-wrap items-center gap-2">
										<Label
											for={prop.key}
											class="text-xs font-medium text-[#f4f4f4] {!isEnabled
												? 'text-[#8d8d8d]'
												: ''}"
										>
											{prop.label}
										</Label>
										{#if prop.required}
											<span
												class="rounded-none border border-[#da1e28]/40 bg-[#da1e28]/20 px-1 py-0.5 font-mono text-[10px] font-medium text-[#ff8389]"
												>required</span
											>
										{/if}
										{#if prop.system}
											<span
												class="rounded-none border border-[#0f62fe]/40 bg-[#0f62fe]/20 px-1 py-0.5 font-mono text-[10px] font-medium text-[#78a9ff]"
												>system</span
											>
										{/if}
										{#if isModified}
											<span
												class="rounded-none border border-[#ff832b]/40 bg-[#ff832b]/20 px-1 py-0.5 font-mono text-[10px] font-medium text-[#ff832b]"
												>modified</span
											>
										{/if}
										{#if !isEnabled}
											<span
												class="rounded-none border border-[#525252] bg-[#353535] px-1 py-0.5 font-mono text-[10px] text-[#8d8d8d]"
												>(unset)</span
											>
										{/if}
										{#if prop.key === 'cfApiKey'}
											<span
												class="rounded-none border border-[#0f62fe]/40 bg-[#0f62fe]/20 px-1.5 py-0.5 font-mono text-[10px] font-medium text-[#78a9ff]"
											>
												Optional (Keyless Fallback)
											</span>
										{/if}
									</div>
									{#if prop.envVar}
										<code class="font-mono text-xs text-[#8d8d8d]">{prop.envVar}</code>
									{/if}
									{#if prop.description}
										<p class="mt-1 text-xs text-[#a8a8a8]">{prop.description}</p>
									{/if}

									<!-- Quick Actions for CurseForge Fields -->
									{#if prop.key === 'cfSlug' && server?.name}
										<div class="mt-2">
											<button
												type="button"
												class="inline-flex h-6 cursor-pointer items-center rounded-none border border-[#393939] bg-[#353535] px-2 font-sans text-xs text-[#78a9ff] hover:bg-[#393939] hover:text-white"
												onclick={assumeCfSlugFromServerName}
											>
												<Wand2 class="mr-1 h-3 w-3" />
												Assume from Server Name ("{server.name}")
											</button>
										</div>
									{/if}
									{#if prop.key === 'cfPageUrl' && getDisplayValue(prop)}
										<div class="mt-2">
											<button
												type="button"
												class="inline-flex h-6 cursor-pointer items-center rounded-none border border-[#393939] bg-[#353535] px-2 font-sans text-xs text-[#78a9ff] hover:bg-[#393939] hover:text-white"
												onclick={() => handleExtractFromPageUrl(getDisplayValue(prop))}
											>
												<Wand2 class="mr-1 h-3 w-3" />
												Extract Slug & File ID
											</button>
										</div>
									{/if}
								</div>
								<div class="flex items-center gap-1">
									<!-- Set/Unset Toggle -->
									<button
										class="rounded-none p-1 transition-colors
											{canToggle ? 'cursor-pointer hover:bg-[#353535]' : 'cursor-not-allowed opacity-50'}"
										onclick={() => canToggle && toggleFieldEnabled(prop.key, !isEnabled, prop)}
										disabled={!canToggle}
										title={isEnabled
											? 'Click to unset (use default)'
											: 'Click to set a custom value'}
									>
										{#if isEnabled}
											<CircleDot class="h-4 w-4 text-[#42be65]" />
										{:else}
											<Circle class="h-4 w-4 text-[#8d8d8d]" />
										{/if}
									</button>
									<button
										type="button"
										class="flex h-6 w-6 cursor-pointer items-center justify-center rounded-none text-[#8d8d8d] transition-colors hover:bg-[#353535] hover:text-white"
										onclick={() => copyLinkToClipboard(prop.key)}
										title="Copy link to field"
									>
										<Link class="h-3 w-3" />
									</button>
								</div>
							</div>

							<!-- Field Input -->
							{#if prop.type === 'checkbox'}
								<div class="flex items-center gap-3 py-1">
									<Switch
										id={prop.key}
										checked={getBooleanValue(prop)}
										onCheckedChange={(checked) => updateValue(prop.key, checked)}
										disabled={prop.system || !isEnabled || isServerRunning}
									/>
									<span class="text-xs text-[#c6c6c6] {!isEnabled ? 'text-[#8d8d8d]' : ''}">
										{getBooleanValue(prop) ? 'Enabled' : 'Disabled'}
									</span>
								</div>
							{:else if prop.type === 'select' && prop.options?.length}
								<Select
									type="single"
									value={getDisplayValue(prop)}
									onValueChange={(value) => updateValue(prop.key, value ?? '')}
									disabled={prop.system || !isEnabled || isServerRunning}
								>
									<SelectTrigger
										class="h-9 rounded-none border border-[#525252] bg-[#161616] text-[#f4f4f4] {!isEnabled
											? 'opacity-60'
											: ''}"
									>
										<span class="truncate">
											{getDisplayValue(prop) || 'Select...'}
										</span>
									</SelectTrigger>
									<SelectContent class="rounded-none border border-[#393939] bg-[#262626]">
										{#each prop.options as option (option)}
											<SelectItem value={option} class="rounded-none"
												>{option || '(empty)'}</SelectItem
											>
										{/each}
									</SelectContent>
								</Select>
							{:else if prop.type === 'number'}
								<Input
									id={prop.key}
									type="number"
									value={getDisplayValue(prop)}
									placeholder={prop.defaultValue ?? ''}
									oninput={(e) => updateValue(prop.key, e.currentTarget.value)}
									disabled={prop.system || !isEnabled || isServerRunning}
									class="h-9 rounded-none border border-[#525252] bg-[#161616] text-[#f4f4f4] placeholder:text-[#6f6f6f] {!isEnabled
										? 'opacity-60'
										: ''}"
								/>
							{:else if prop.type === 'password'}
								<Input
									id={prop.key}
									type="password"
									value={getDisplayValue(prop)}
									placeholder={prop.defaultValue ?? ''}
									oninput={(e) => updateValue(prop.key, e.currentTarget.value)}
									disabled={prop.system || !isEnabled || isServerRunning}
									class="h-9 rounded-none border border-[#525252] bg-[#161616] text-[#f4f4f4] placeholder:text-[#6f6f6f] {!isEnabled
										? 'opacity-60'
										: ''}"
								/>
							{:else}
								<Input
									id={prop.key}
									type="text"
									value={getDisplayValue(prop)}
									placeholder={prop.defaultValue ?? ''}
									oninput={(e) => updateValue(prop.key, e.currentTarget.value)}
									disabled={prop.system || !isEnabled || isServerRunning}
									class="h-9 rounded-none border border-[#525252] bg-[#161616] text-[#f4f4f4] placeholder:text-[#6f6f6f] {!isEnabled
										? 'opacity-60'
										: ''}"
								/>
							{/if}

							{#if prop.defaultValue !== undefined && prop.defaultValue !== ''}
								<p class="mt-2 text-xs text-[#8d8d8d]">
									Default: <code
										class="rounded-none bg-[#353535] px-1 py-0.5 font-mono text-xs text-[#c6c6c6]"
										>{prop.defaultValue}</code
									>
								</p>
							{/if}
						</div>
					{/snippet}

					<!-- Fields Grid -->
					<div class="flex-1 overflow-y-auto p-4">
						{#if activeCategory === 'curseforge'}
							<div class="mb-4 rounded-none border border-[#0f62fe]/40 bg-[#0043ce]/10 p-3 text-sm">
								<div class="flex items-center gap-2 font-medium text-[#78a9ff]">
									<KeyRound class="h-4 w-4" />
									CurseForge Keyless Mode & Defaults Active
								</div>
								<p class="mt-1 text-xs text-[#a8a8a8]">
									CurseForge modpacks and mods can be downloaded and searched without an API key via
									community proxies. If provided, slugs and file IDs can be auto-extracted directly
									from modpack URLs or assumed from the server name.
								</p>
							</div>
						{/if}

						<div class="grid gap-4 lg:grid-cols-2">
							{#each primaryCategoryProps as prop (prop.key)}
								{@render fieldCard(prop)}
							{/each}
						</div>

						{#if activeCategory === 'curseforge' && advancedCategoryProps.length > 0}
							<div class="mt-6 rounded-none border border-[#393939] bg-[#1e1e1e] p-4">
								<button
									type="button"
									onclick={() => (showAdvancedCurseForge = !showAdvancedCurseForge)}
									class="flex w-full cursor-pointer items-center justify-between rounded-none text-left text-sm font-medium text-[#f4f4f4] transition-colors hover:text-[#78a9ff]"
								>
									<span class="flex items-center gap-2">
										{#if showAdvancedCurseForge}
											<ChevronDown class="h-4 w-4 text-[#8d8d8d]" />
										{:else}
											<ChevronRight class="h-4 w-4 text-[#8d8d8d]" />
										{/if}
										Advanced CurseForge Options
									</span>
									<span class="text-xs text-[#8d8d8d]">
										{advancedCategoryProps.length} additional options
									</span>
								</button>
								{#if showAdvancedCurseForge}
									<div class="mt-4 grid gap-4 lg:grid-cols-2">
										{#each advancedCategoryProps as prop (prop.key)}
											{@render fieldCard(prop)}
										{/each}
									</div>
								{/if}
							</div>
						{/if}
					</div>
				</div>
			</div>

			{#if server && isServerRunning}
				<div class="rounded-none border-t border-[#f1c21b]/30 bg-[#f1c21b]/10 p-4">
					<p class="font-mono text-xs text-[#f1c21b]">
						Server must be stopped to modify configuration. Changes will take effect after restart.
					</p>
				</div>
			{/if}
		{/if}
	</div>
</div>

<ScrollToTop />
