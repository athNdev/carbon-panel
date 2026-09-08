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
		Sparkles,
		Download,
		Rocket,
		Trash2,
		ExternalLink,
		Loader2,
		Calendar
	} from '@lucide/svelte';
	import { toast } from 'svelte-sonner';
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

	// Create Dialog State
	let createDialogOpen = $state(false);
	let creating = $state(false);
	let newName = $state('My Custom Pack');
	let newAuthor = $state('Admin');
	let newVersion = $state('1.0.0');
	let newMcVersion = $state('1.20.1');
	let newLoader = $state('fabric');
	let newLoaderVersion = $state('latest');

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

	async function loadPacks() {
		loading = true;
		try {
			const res = await fetch('/api/v1/packwiz/packs');
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
			const res = await fetch('/api/v1/packwiz/packs', {
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

	async function deletePack(pack: PackSummary) {
		if (!confirm(`Are you sure you want to delete "${pack.name}"? This cannot be undone.`)) {
			return;
		}

		try {
			const res = await fetch(`/api/v1/packwiz/packs/${pack.id}`, { method: 'DELETE' });
			if (!res.ok) throw new Error(`HTTP ${res.status}`);
			toast.success(`Deleted modpack "${pack.name}"`);
			packs = packs.filter((p) => p.id !== pack.id);
		} catch (err) {
			console.error('Failed to delete pack:', err);
			toast.error('Failed to delete pack');
		}
	}

	function openDeploy(pack: PackSummary) {
		deployPack = pack;
		deployDialogOpen = true;
	}

	onMount(() => {
		loadPacks();
	});
</script>

<div class="h-full flex-1 space-y-8 bg-linear-to-br from-background to-muted/10 p-8 pt-6">
	<!-- Top Bar -->
	<div class="flex items-center justify-between border-b-2 border-border/50 pb-6">
		<div class="flex items-center gap-4">
			<Button variant="ghost" size="icon" onclick={() => goto('/modpacks')} class="h-10 w-10">
				<ArrowLeft class="h-5 w-5" />
			</Button>
			<div class="flex h-16 w-16 items-center justify-center rounded-2xl bg-gradient-to-br from-primary/20 to-primary/10 shadow-lg">
				<Sparkles class="h-8 w-8 text-primary" />
			</div>
			<div class="space-y-1">
				<h2 class="bg-gradient-to-r from-foreground to-foreground/70 bg-clip-text text-4xl font-bold tracking-tight text-transparent">
					Modpack Studio
				</h2>
				<p class="text-base text-muted-foreground">
					Create, edit, and export custom Packwiz modpacks with 1-click server deployment
				</p>
			</div>
		</div>

		<div class="flex items-center gap-3">
			<Button onclick={() => (createDialogOpen = true)} class="shadow-md hover:shadow-lg">
				<Plus class="mr-2 h-5 w-5" />
				New Modpack
			</Button>
		</div>
	</div>

	<!-- Project Grid -->
	{#if loading}
		<div class="flex flex-col items-center justify-center py-24 text-muted-foreground">
			<Loader2 class="h-10 w-10 animate-spin text-primary" />
			<p class="mt-4 text-base font-medium">Loading Modpack Studio projects...</p>
		</div>
	{:else if packs.length === 0}
		<div class="flex flex-col items-center justify-center py-24 text-center">
			<div class="flex h-20 w-20 items-center justify-center rounded-3xl bg-muted/60 text-muted-foreground">
				<Package class="h-10 w-10 stroke-[1.5]" />
			</div>
			<h3 class="mt-4 text-xl font-bold">No Modpacks Yet</h3>
			<p class="mt-1 max-w-sm text-sm text-muted-foreground">
				Create your first custom modpack with Packwiz, browse online mods, and deploy directly to your Minecraft servers.
			</p>
			<Button onclick={() => (createDialogOpen = true)} class="mt-6">
				<Plus class="mr-2 h-4 w-4" />
				Create New Modpack
			</Button>
		</div>
	{:else}
		<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
			{#each packs as pack (pack.id)}
				<Card class="flex flex-col justify-between hover:border-primary/50 transition-all hover:shadow-md">
					<CardHeader class="pb-3">
						<div class="flex items-start justify-between gap-2">
							<div class="space-y-1 min-w-0">
								<CardTitle class="text-xl font-bold truncate">{pack.name}</CardTitle>
								<CardDescription class="text-xs">
									by <span class="text-foreground font-medium">{pack.author || 'Unknown'}</span> · v{pack.version}
								</CardDescription>
							</div>
							<Badge variant="outline" class="font-mono text-xs uppercase flex-shrink-0">
								{pack.mod_loader}
							</Badge>
						</div>

						<div class="flex flex-wrap items-center gap-2 pt-2">
							<Badge variant="secondary" class="font-mono text-xs">
								MC {pack.mc_version}
							</Badge>
							<Badge variant="outline" class="text-xs">
								<Package class="mr-1 h-3 w-3" />
								{pack.mod_count} mods
							</Badge>
						</div>
					</CardHeader>

					<CardContent class="pt-0 space-y-4">
						<div class="flex items-center justify-between text-xs text-muted-foreground border-t pt-3">
							<div class="flex items-center gap-1.5">
								<Calendar class="h-3.5 w-3.5" />
								<span>{new Date(pack.updated_at).toLocaleDateString()}</span>
							</div>

							<div class="flex items-center gap-2">
								<a
									href={`/api/v1/packwiz/packs/${pack.id}/export/mrpack`}
									download
									class="inline-flex items-center text-xs text-muted-foreground hover:text-primary transition-colors"
									title="Export .mrpack"
								>
									<Download class="h-3.5 w-3.5 mr-1" />
									.mrpack
								</a>
								<span>·</span>
								<a
									href={`/api/v1/packwiz/packs/${pack.id}/export/curseforge`}
									download
									class="inline-flex items-center text-xs text-muted-foreground hover:text-primary transition-colors"
									title="Export CurseForge .zip"
								>
									<Download class="h-3.5 w-3.5 mr-1" />
									CurseForge
								</a>
							</div>
						</div>

						<div class="flex items-center justify-between gap-2 pt-1">
							<Button
								variant="default"
								size="sm"
								onclick={() => goto(`/modpacks/studio/${pack.id}`)}
								class="flex-1"
							>
								Open Studio
							</Button>

							<Button
								variant="outline"
								size="sm"
								onclick={() => openDeploy(pack)}
								title="Deploy to server"
							>
								<Rocket class="h-4 w-4" />
							</Button>

							<Button
								variant="ghost"
								size="sm"
								onclick={() => deletePack(pack)}
								title="Delete project"
								class="text-destructive hover:bg-destructive/10"
							>
								<Trash2 class="h-4 w-4" />
							</Button>
						</div>
					</CardContent>
				</Card>
			{/each}
		</div>
	{/if}
</div>

<!-- Create New Modpack Dialog -->
<DialogPrimitive.Root bind:open={createDialogOpen}>
	<DialogContent class="max-w-md p-6">
		<DialogHeader>
			<DialogTitle class="flex items-center gap-2 text-xl font-bold">
				<Sparkles class="h-5 w-5 text-primary" />
				Create New Modpack
			</DialogTitle>
			<DialogDescription>
				Set up a new Packwiz modpack project with your chosen Minecraft version and mod loader.
			</DialogDescription>
		</DialogHeader>

		<div class="space-y-4 py-3">
			<div class="space-y-1.5">
				<Label for="packName">Modpack Name</Label>
				<Input id="packName" placeholder="e.g. Fabric Horizons" bind:value={newName} />
			</div>

			<div class="grid grid-cols-2 gap-3">
				<div class="space-y-1.5">
					<Label for="packAuthor">Author</Label>
					<Input id="packAuthor" placeholder="e.g. Admin" bind:value={newAuthor} />
				</div>
				<div class="space-y-1.5">
					<Label for="packVersion">Version</Label>
					<Input id="packVersion" placeholder="1.0.0" bind:value={newVersion} />
				</div>
			</div>

			<div class="grid grid-cols-2 gap-3">
				<div class="space-y-1.5">
					<Label for="mcVersion">Minecraft Version</Label>
					<Select value={newMcVersion} onValueChange={(val) => (newMcVersion = val)}>
						<SelectTrigger id="mcVersion">
							<span>{newMcVersion}</span>
						</SelectTrigger>
						<SelectContent>
							{#each MC_VERSIONS as v}
								<SelectItem value={v}>{v}</SelectItem>
							{/each}
						</SelectContent>
					</Select>
				</div>

				<div class="space-y-1.5">
					<Label for="loader">Mod Loader</Label>
					<Select value={newLoader} onValueChange={(val) => (newLoader = val)}>
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

			<div class="space-y-1.5">
				<Label for="loaderVersion">Loader Version</Label>
				<Input id="loaderVersion" placeholder="latest" bind:value={newLoaderVersion} />
			</div>
		</div>

		<DialogFooter>
			<Button variant="outline" onclick={() => (createDialogOpen = false)}>Cancel</Button>
			<Button onclick={handleCreatePack} disabled={creating}>
				{#if creating}
					<Loader2 class="mr-2 h-4 w-4 animate-spin" />
					Creating...
				{:else}
					<Sparkles class="mr-2 h-4 w-4" />
					Create Project
				{/if}
			</Button>
		</DialogFooter>
	</DialogContent>
</DialogPrimitive.Root>

<!-- Deploy Dialog -->
{#if deployPack}
	<ModpackDeployDialog
		bind:open={deployDialogOpen}
		packId={deployPack.id}
		packName={deployPack.name}
		mcVersion={deployPack.mc_version}
		modLoader={deployPack.mod_loader}
	/>
{/if}
