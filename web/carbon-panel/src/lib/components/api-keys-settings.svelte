<script lang="ts">
	import { onMount } from 'svelte';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Alert, AlertDescription, AlertTitle } from '$lib/components/ui/alert';
	import { CarbonButton } from '$lib/components/carbon';
	import { toast } from 'svelte-sonner';
	import {
		Key,
		CheckCircle2,
		XCircle,
		AlertCircle,
		ExternalLink,
		Loader2,
		Eye,
		EyeOff,
		ShieldCheck,
		Save
	} from '@lucide/svelte';
	import { rpcClient } from '$lib/api/rpc-client';
	import { apiFetch } from '$lib/api/fetch';

	let cfApiKey = $state('');
	let modrinthToken = $state('');
	let modrinthUserAgent = $state('');

	let showCfKey = $state(false);
	let showModrinthToken = $state(false);

	let loading = $state(true);
	let saving = $state(false);

	let testingCf = $state(false);
	let cfTestResult = $state<{ valid: boolean; message: string } | null>(null);

	let testingModrinth = $state(false);
	let modrinthTestResult = $state<{ valid: boolean; message: string } | null>(null);

	async function loadKeys() {
		loading = true;
		try {
			const res = await rpcClient.config.getGlobalSettings({});
			for (const cat of res.categories) {
				for (const prop of cat.properties) {
					if (prop.key === 'cfApiKey') {
						cfApiKey = prop.value || '';
					} else if (prop.key === 'modrinthToken') {
						modrinthToken = prop.value || '';
					} else if (prop.key === 'modrinthUserAgent') {
						modrinthUserAgent = prop.value || '';
					}
				}
			}
		} catch (err) {
			console.error('Failed to load API keys:', err);
			toast.error('Failed to load API keys configuration');
		} finally {
			loading = false;
		}
	}

	async function testCurseForge() {
		if (!cfApiKey.trim()) {
			toast.error('Please enter a CurseForge API key before testing');
			return;
		}

		testingCf = true;
		cfTestResult = null;
		try {
			const res = await apiFetch('/api/v1/settings/validate-key', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({
					provider: 'curseforge',
					api_key: cfApiKey.trim()
				})
			});
			const data = await res.json();
			cfTestResult = data;
			if (data.valid) {
				toast.success(data.message);
			} else {
				toast.error(data.message);
			}
		} catch (err) {
			cfTestResult = { valid: false, message: 'Failed to contact validation endpoint' };
			toast.error('Connection test failed');
		} finally {
			testingCf = false;
		}
	}

	async function testModrinth() {
		testingModrinth = true;
		modrinthTestResult = null;
		try {
			const res = await apiFetch('/api/v1/settings/validate-key', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({
					provider: 'modrinth',
					api_key: modrinthToken.trim(),
					user_agent: modrinthUserAgent.trim()
				})
			});
			const data = await res.json();
			modrinthTestResult = data;
			if (data.valid) {
				toast.success(data.message);
			} else {
				toast.error(data.message);
			}
		} catch (err) {
			modrinthTestResult = { valid: false, message: 'Failed to contact validation endpoint' };
			toast.error('Connection test failed');
		} finally {
			testingModrinth = false;
		}
	}

	async function saveKeys() {
		saving = true;
		try {
			const updates: Record<string, string> = {
				cfApiKey: cfApiKey.trim(),
				modrinthToken: modrinthToken.trim(),
				modrinthUserAgent: modrinthUserAgent.trim()
			};

			await rpcClient.config.updateGlobalSettings({ updates });
			toast.success('API key settings saved successfully');
		} catch (err) {
			console.error('Failed to save API keys:', err);
			toast.error('Failed to save API key settings');
		} finally {
			saving = false;
		}
	}

	onMount(() => {
		loadKeys();
	});
</script>

<div class="space-y-6">
	<!-- CurseForge API Key Card -->
	<div class="space-y-4 rounded-none border border-[#393939] bg-[#262626] p-5 shadow-none">
		<div class="flex items-start justify-between border-b border-[#393939] pb-4">
			<div class="space-y-1">
				<h3 class="flex items-center gap-2 text-base font-normal text-[#f4f4f4]">
					<Key class="h-4 w-4 text-[#0f62fe]" />
					CurseForge API Key
				</h3>
				<p class="text-xs text-[#a8a8a8]">
					Required to search, browse, and synchronize CurseForge modpacks and mods.
				</p>
			</div>
			<div>
				{#if cfApiKey.trim()}
					<span
						class="inline-flex items-center gap-1 rounded-none border border-[#24a148] bg-[#24a148]/20 px-2 py-0.5 font-mono text-xs text-[#42be65]"
					>
						<CheckCircle2 class="h-3 w-3" />
						Configured
					</span>
				{:else}
					<span
						class="inline-flex items-center gap-1 rounded-none border border-[#da1e28] bg-[#da1e28]/20 px-2 py-0.5 font-mono text-xs text-[#ff8389]"
					>
						<AlertCircle class="h-3 w-3" />
						Not Configured
					</span>
				{/if}
			</div>
		</div>

		<div class="space-y-4">
			<div class="rounded-none border border-[#393939] bg-[#1e1e1e] p-4 text-xs text-[#a8a8a8]">
				<p class="font-medium text-[#f4f4f4]">How to get a CurseForge API Key:</p>
				<ol class="mt-2 list-inside list-decimal space-y-1">
					<li>
						Log in to the
						<a
							href="https://console.curseforge.com/#/api-keys"
							target="_blank"
							rel="noopener noreferrer"
							class="inline-flex items-center gap-1 font-medium text-[#78a9ff] hover:underline"
						>
							CurseForge Developer Console <ExternalLink class="h-3 w-3" />
						</a>.
					</li>
					<li>Generate a new API Key for your organization or personal project.</li>
					<li>Paste the generated key below and click "Test Connection" to verify.</li>
				</ol>
			</div>

			<div class="space-y-2">
				<Label for="cf-api-key" class="text-xs text-[#c6c6c6]">API Key</Label>
				<div class="flex gap-2">
					<div class="relative flex-1">
						<Input
							id="cf-api-key"
							type={showCfKey ? 'text' : 'password'}
							placeholder="$2a$10$..."
							bind:value={cfApiKey}
							class="h-10 rounded-none border border-[#525252] bg-[#161616] pr-10 font-mono text-sm text-[#f4f4f4] placeholder:text-[#6f6f6f]"
						/>
						<button
							type="button"
							onclick={() => (showCfKey = !showCfKey)}
							class="absolute top-1/2 right-3 -translate-y-1/2 cursor-pointer rounded-none text-[#8d8d8d] hover:text-[#f4f4f4]"
						>
							{#if showCfKey}
								<EyeOff class="h-4 w-4" />
							{:else}
								<Eye class="h-4 w-4" />
							{/if}
						</button>
					</div>
					<button
						type="button"
						onclick={testCurseForge}
						disabled={testingCf || !cfApiKey.trim()}
						class="inline-flex h-10 cursor-pointer items-center gap-2 rounded-none border border-[#0f62fe] px-4 font-sans text-xs font-medium text-[#78a9ff] transition-colors hover:bg-[#0f62fe] hover:text-white disabled:cursor-not-allowed disabled:opacity-40"
					>
						{#if testingCf}
							<Loader2 class="h-3.5 w-3.5 animate-spin" />
							<span>Testing...</span>
						{:else}
							<ShieldCheck class="h-3.5 w-3.5" />
							<span>Test Connection</span>
						{/if}
					</button>
				</div>
			</div>

			{#if cfTestResult}
				{#if cfTestResult.valid}
					<Alert class="rounded-none border border-[#24a148]/40 bg-[#24a148]/10 text-[#42be65]">
						<CheckCircle2 class="h-4 w-4 text-[#42be65]" />
						<AlertTitle class="text-xs font-medium">Connection Valid</AlertTitle>
						<AlertDescription class="text-xs">{cfTestResult.message}</AlertDescription>
					</Alert>
				{:else}
					<Alert
						variant="destructive"
						class="rounded-none border border-[#da1e28]/40 bg-[#da1e28]/10 text-[#ff8389]"
					>
						<XCircle class="h-4 w-4 text-[#da1e28]" />
						<AlertTitle class="text-xs font-medium">Validation Failed</AlertTitle>
						<AlertDescription class="text-xs">{cfTestResult.message}</AlertDescription>
					</Alert>
				{/if}
			{/if}
		</div>
	</div>

	<!-- Modrinth Configuration Card -->
	<div class="space-y-4 rounded-none border border-[#393939] bg-[#262626] p-5 shadow-none">
		<div class="flex items-start justify-between border-b border-[#393939] pb-4">
			<div class="space-y-1">
				<h3 class="flex items-center gap-2 text-base font-normal text-[#f4f4f4]">
					<Key class="h-4 w-4 text-[#0f62fe]" />
					Modrinth Credentials & User-Agent
				</h3>
				<p class="text-xs text-[#a8a8a8]">
					Configure Modrinth API authentication token and identification headers.
				</p>
			</div>
			<div>
				{#if modrinthToken.trim()}
					<span
						class="inline-flex items-center gap-1 rounded-none border border-[#24a148] bg-[#24a148]/20 px-2 py-0.5 font-mono text-xs text-[#42be65]"
					>
						<CheckCircle2 class="h-3 w-3" />
						Authenticated
					</span>
				{:else}
					<span
						class="inline-flex items-center gap-1 rounded-none border border-[#525252] bg-transparent px-2 py-0.5 font-mono text-xs text-[#c6c6c6]"
					>
						Public Access
					</span>
				{/if}
			</div>
		</div>

		<div class="space-y-4">
			<div class="rounded-none border border-[#393939] bg-[#1e1e1e] p-4 text-xs text-[#a8a8a8]">
				<p class="font-medium text-[#f4f4f4]">
					Modrinth modpack and mod indexing works publicly without authentication. Supplying a
					Personal Access Token (PAT) unlocks elevated rate limits and enables accessing private or
					unlisted projects.
				</p>
				<p class="mt-1">
					Generate tokens in your
					<a
						href="https://modrinth.com/settings/pats"
						target="_blank"
						rel="noopener noreferrer"
						class="inline-flex items-center gap-1 font-medium text-[#78a9ff] hover:underline"
					>
						Modrinth PAT Settings <ExternalLink class="h-3 w-3" />
					</a>.
				</p>
			</div>

			<div class="space-y-2">
				<Label for="modrinth-token" class="text-xs text-[#c6c6c6]"
					>Personal Access Token (PAT)</Label
				>
				<div class="relative">
					<Input
						id="modrinth-token"
						type={showModrinthToken ? 'text' : 'password'}
						placeholder="mrp_..."
						bind:value={modrinthToken}
						class="h-10 rounded-none border border-[#525252] bg-[#161616] pr-10 font-mono text-sm text-[#f4f4f4] placeholder:text-[#6f6f6f]"
					/>
					<button
						type="button"
						onclick={() => (showModrinthToken = !showModrinthToken)}
						class="absolute top-1/2 right-3 -translate-y-1/2 cursor-pointer rounded-none text-[#8d8d8d] hover:text-[#f4f4f4]"
					>
						{#if showModrinthToken}
							<EyeOff class="h-4 w-4" />
						{:else}
							<Eye class="h-4 w-4" />
						{/if}
					</button>
				</div>
			</div>

			<div class="space-y-2">
				<Label for="modrinth-ua" class="text-xs text-[#c6c6c6]">User-Agent Header</Label>
				<Input
					id="modrinth-ua"
					type="text"
					placeholder="CarbonPanel/1.0 (contact@example.com)"
					bind:value={modrinthUserAgent}
					class="h-10 rounded-none border border-[#525252] bg-[#161616] font-mono text-sm text-[#f4f4f4] placeholder:text-[#6f6f6f]"
				/>
				<p class="text-xs text-[#8d8d8d]">
					Modrinth API policy encourages client identification including application name and
					contact info.
				</p>
			</div>

			<div class="pt-2">
				<button
					type="button"
					onclick={testModrinth}
					disabled={testingModrinth}
					class="inline-flex h-10 cursor-pointer items-center gap-2 rounded-none border border-[#0f62fe] px-4 font-sans text-xs font-medium text-[#78a9ff] transition-colors hover:bg-[#0f62fe] hover:text-white disabled:cursor-not-allowed disabled:opacity-40"
				>
					{#if testingModrinth}
						<Loader2 class="h-3.5 w-3.5 animate-spin" />
						<span>Testing...</span>
					{:else}
						<ShieldCheck class="h-3.5 w-3.5" />
						<span>Test Connection</span>
					{/if}
				</button>
			</div>

			{#if modrinthTestResult}
				{#if modrinthTestResult.valid}
					<Alert class="rounded-none border border-[#24a148]/40 bg-[#24a148]/10 text-[#42be65]">
						<CheckCircle2 class="h-4 w-4 text-[#42be65]" />
						<AlertTitle class="text-xs font-medium">Connection Valid</AlertTitle>
						<AlertDescription class="text-xs">{modrinthTestResult.message}</AlertDescription>
					</Alert>
				{:else}
					<Alert
						variant="destructive"
						class="rounded-none border border-[#da1e28]/40 bg-[#da1e28]/10 text-[#ff8389]"
					>
						<XCircle class="h-4 w-4 text-[#da1e28]" />
						<AlertTitle class="text-xs font-medium">Validation Failed</AlertTitle>
						<AlertDescription class="text-xs">{modrinthTestResult.message}</AlertDescription>
					</Alert>
				{/if}
			{/if}
		</div>
	</div>

	<!-- Actions -->
	<div class="flex justify-end gap-3">
		<CarbonButton
			kind="primary"
			size="md"
			class="justify-center gap-2 px-6"
			onclick={saveKeys}
			disabled={saving || loading}
		>
			{#if saving}
				<Loader2 class="h-4 w-4 animate-spin" />
				<span>Saving...</span>
			{:else}
				<Save class="h-4 w-4" />
				<span>Save API Keys</span>
			{/if}
		</CarbonButton>
	</div>
</div>
