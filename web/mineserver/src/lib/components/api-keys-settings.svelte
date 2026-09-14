<script lang="ts">
	import { onMount } from 'svelte';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Badge } from '$lib/components/ui/badge';
	import { Alert, AlertDescription, AlertTitle } from '$lib/components/ui/alert';
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
	<Card>
		<CardHeader>
			<div class="flex items-center justify-between">
				<div class="space-y-1">
					<CardTitle class="flex items-center gap-2 text-xl font-bold">
						<Key class="h-5 w-5 text-primary" />
						CurseForge API Key
					</CardTitle>
					<CardDescription>
						Required to search, browse, and synchronize CurseForge modpacks and mods.
					</CardDescription>
				</div>
				<div>
					{#if cfApiKey.trim()}
						<Badge variant="default" class="bg-emerald-600/90 text-white">
							<CheckCircle2 class="mr-1 h-3.5 w-3.5" />
							Configured
						</Badge>
					{:else}
						<Badge variant="secondary">
							<AlertCircle class="mr-1 h-3.5 w-3.5" />
							Not Configured
						</Badge>
					{/if}
				</div>
			</div>
		</CardHeader>
		<CardContent class="space-y-4">
			<div class="rounded-lg border border-border/60 bg-muted/30 p-4 text-sm text-muted-foreground">
				<p class="font-medium text-foreground">How to get a CurseForge API Key:</p>
				<ol class="mt-2 list-inside list-decimal space-y-1">
					<li>
						Log in to the
						<a
							href="https://console.curseforge.com/#/api-keys"
							target="_blank"
							rel="noopener noreferrer"
							class="inline-flex items-center gap-1 font-medium text-primary hover:underline"
						>
							CurseForge Developer Console <ExternalLink class="h-3 w-3" />
						</a>.
					</li>
					<li>Generate a new API Key for your organization or personal project.</li>
					<li>Paste the generated key below and click "Test Connection" to verify.</li>
				</ol>
			</div>

			<div class="space-y-2">
				<Label for="cf-api-key">API Key</Label>
				<div class="flex gap-2">
					<div class="relative flex-1">
						<Input
							id="cf-api-key"
							type={showCfKey ? 'text' : 'password'}
							placeholder="$2a$10$..."
							bind:value={cfApiKey}
							class="pr-10 font-mono text-sm"
						/>
						<button
							type="button"
							onclick={() => (showCfKey = !showCfKey)}
							class="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
						>
							{#if showCfKey}
								<EyeOff class="h-4 w-4" />
							{:else}
								<Eye class="h-4 w-4" />
							{/if}
						</button>
					</div>
					<Button
						variant="outline"
						onclick={testCurseForge}
						disabled={testingCf || !cfApiKey.trim()}
					>
						{#if testingCf}
							<Loader2 class="mr-2 h-4 w-4 animate-spin" />
							Testing...
						{:else}
							<ShieldCheck class="mr-2 h-4 w-4" />
							Test Connection
						{/if}
					</Button>
				</div>
			</div>

			{#if cfTestResult}
				{#if cfTestResult.valid}
					<Alert class="border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300">
						<CheckCircle2 class="h-4 w-4 text-emerald-500" />
						<AlertTitle>Connection Valid</AlertTitle>
						<AlertDescription>{cfTestResult.message}</AlertDescription>
					</Alert>
				{:else}
					<Alert variant="destructive">
						<XCircle class="h-4 w-4" />
						<AlertTitle>Validation Failed</AlertTitle>
						<AlertDescription>{cfTestResult.message}</AlertDescription>
					</Alert>
				{/if}
			{/if}
		</CardContent>
	</Card>

	<!-- Modrinth Configuration Card -->
	<Card>
		<CardHeader>
			<div class="flex items-center justify-between">
				<div class="space-y-1">
					<CardTitle class="flex items-center gap-2 text-xl font-bold">
						<Key class="h-5 w-5 text-primary" />
						Modrinth Credentials & User-Agent
					</CardTitle>
					<CardDescription>
						Configure Modrinth API authentication token and identification headers.
					</CardDescription>
				</div>
				<div>
					{#if modrinthToken.trim()}
						<Badge variant="default" class="bg-emerald-600/90 text-white">
							<CheckCircle2 class="mr-1 h-3.5 w-3.5" />
							Authenticated
						</Badge>
					{:else}
						<Badge variant="outline">Public Access</Badge>
					{/if}
				</div>
			</div>
		</CardHeader>
		<CardContent class="space-y-4">
			<div class="rounded-lg border border-border/60 bg-muted/30 p-4 text-sm text-muted-foreground">
				<p>
					Modrinth modpack and mod indexing works publicly without authentication. Supplying a Personal
					Access Token (PAT) unlocks elevated rate limits and enables accessing private or unlisted projects.
				</p>
				<p class="mt-1">
					Generate tokens in your
					<a
						href="https://modrinth.com/settings/pats"
						target="_blank"
						rel="noopener noreferrer"
						class="inline-flex items-center gap-1 font-medium text-primary hover:underline"
					>
						Modrinth PAT Settings <ExternalLink class="h-3 w-3" />
					</a>.
				</p>
			</div>

			<div class="space-y-2">
				<Label for="modrinth-token">Personal Access Token (PAT)</Label>
				<div class="relative">
					<Input
						id="modrinth-token"
						type={showModrinthToken ? 'text' : 'password'}
						placeholder="mrp_..."
						bind:value={modrinthToken}
						class="pr-10 font-mono text-sm"
					/>
					<button
						type="button"
						onclick={() => (showModrinthToken = !showModrinthToken)}
						class="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
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
				<Label for="modrinth-ua">User-Agent Header</Label>
				<Input
					id="modrinth-ua"
					type="text"
					placeholder="MineServer/1.0 (contact@example.com)"
					bind:value={modrinthUserAgent}
					class="font-mono text-sm"
				/>
				<p class="text-xs text-muted-foreground">
					Modrinth API policy encourages client identification including application name and contact info.
				</p>
			</div>

			<div class="pt-2">
				<Button
					variant="outline"
					onclick={testModrinth}
					disabled={testingModrinth}
				>
					{#if testingModrinth}
						<Loader2 class="mr-2 h-4 w-4 animate-spin" />
						Testing...
					{:else}
						<ShieldCheck class="mr-2 h-4 w-4" />
						Test Connection
					{/if}
				</Button>
			</div>

			{#if modrinthTestResult}
				{#if modrinthTestResult.valid}
					<Alert class="border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300">
						<CheckCircle2 class="h-4 w-4 text-emerald-500" />
						<AlertTitle>Connection Valid</AlertTitle>
						<AlertDescription>{modrinthTestResult.message}</AlertDescription>
					</Alert>
				{:else}
					<Alert variant="destructive">
						<XCircle class="h-4 w-4" />
						<AlertTitle>Validation Failed</AlertTitle>
						<AlertDescription>{modrinthTestResult.message}</AlertDescription>
					</Alert>
				{/if}
			{/if}
		</CardContent>
	</Card>

	<!-- Actions -->
	<div class="flex justify-end gap-3">
		<Button onclick={saveKeys} disabled={saving || loading}>
			{#if saving}
				<Loader2 class="mr-2 h-4 w-4 animate-spin" />
				Saving...
			{:else}
				<Save class="mr-2 h-4 w-4" />
				Save API Keys
			{/if}
		</Button>
	</div>
</div>
