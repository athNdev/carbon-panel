<script lang="ts">
	import {
		Dialog,
		DialogContent,
		DialogHeader,
		DialogTitle,
		DialogDescription,
		DialogFooter
	} from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Badge } from '$lib/components/ui/badge';
	import { Card, CardContent } from '$lib/components/ui/card';
	import { toast } from 'svelte-sonner';
	import { inspectManifest, exportServerManifest } from '$lib/utils/manifest-inspector';
	import type { ManifestInspectionResult } from '$lib/utils/manifest-inspector';
	import {
		FileSearch,
		Upload,
		Copy,
		Download,
		Laptop,
		Server,
		Layers,
		ExternalLink
	} from '@lucide/svelte';

	interface Props {
		open: boolean;
		onOpenChange: (open: boolean) => void;
	}

	let { open = $bindable(false), onOpenChange }: Props = $props();

	let rawInput = $state('');
	let inspection = $state<ManifestInspectionResult | null>(null);
	let error = $state<string | null>(null);
	let filter = $state<'all' | 'both' | 'client' | 'server'>('all');
	let searchQuery = $state('');

	function handleInspect() {
		error = null;
		if (!rawInput.trim()) {
			error = 'Please paste manifest JSON or upload a manifest file.';
			return;
		}
		try {
			inspection = inspectManifest(rawInput);
			toast.success(`Parsed ${inspection.name} (${inspection.totalMods} mods)`);
		} catch (e: any) {
			error = e.message || 'Failed to parse manifest.';
			inspection = null;
		}
	}

	function handleFileUpload(e: Event) {
		const target = e.target as HTMLInputElement;
		const file = target.files?.[0];
		if (!file) return;

		const reader = new FileReader();
		reader.onload = (event) => {
			rawInput = (event.target?.result as string) || '';
			handleInspect();
		};
		reader.readAsText(file);
	}

	function copyServerManifest() {
		if (!inspection) return;
		const exportText = exportServerManifest(inspection);
		navigator.clipboard.writeText(exportText);
		toast.success('Server-ready manifest copied to clipboard!');
	}

	function downloadServerManifest() {
		if (!inspection) return;
		const exportText = exportServerManifest(inspection);
		const filename =
			inspection.format === 'modrinth' ? 'modrinth.server.index.json' : 'manifest.server.json';
		const blob = new Blob([exportText], { type: 'application/json' });
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		a.download = filename;
		a.click();
		URL.revokeObjectURL(url);
		toast.success(`Downloaded ${filename}`);
	}

	let filteredMods = $derived.by(() => {
		if (!inspection) return [];
		return inspection.mods.filter((m) => {
			const matchesFilter =
				filter === 'all' ||
				(filter === 'both' && m.env === 'both') ||
				(filter === 'client' && m.env === 'client') ||
				(filter === 'server' && m.env === 'server');

			const q = searchQuery.toLowerCase().trim();
			const matchesQuery =
				!q ||
				m.name.toLowerCase().includes(q) ||
				(m.filename && m.filename.toLowerCase().includes(q));

			return matchesFilter && matchesQuery;
		});
	});
</script>

<Dialog bind:open {onOpenChange}>
	<DialogContent class="flex max-h-[85vh] max-w-4xl flex-col overflow-hidden p-6">
		<DialogHeader>
			<DialogTitle class="flex items-center gap-2 text-xl font-bold">
				<FileSearch class="h-5 w-5 text-primary" />
				CurseForge & Modrinth Manifest Inspector
			</DialogTitle>
			<DialogDescription>
				Inspect modpack manifests, detect client-only vs server-side mods, and export server-clean
				manifests.
			</DialogDescription>
		</DialogHeader>

		{#if !inspection}
			<div class="flex flex-col gap-4 overflow-y-auto py-4">
				<div class="flex items-center gap-4">
					<label class="cursor-pointer">
						<input type="file" accept=".json,.mrpack" class="hidden" onchange={handleFileUpload} />
						<span
							class="inline-flex items-center gap-2 rounded-md border px-4 py-2 text-sm font-medium transition-colors hover:bg-muted"
						>
							<Upload class="h-4 w-4" /> Upload manifest.json / modrinth.index.json
						</span>
					</label>
					<span class="text-xs text-muted-foreground">or paste JSON content below:</span>
				</div>

				<textarea
					bind:value={rawInput}
					rows={10}
					placeholder="Paste manifest.json or modrinth.index.json contents here..."
					class="w-full rounded-md border bg-muted/30 p-3 font-mono text-xs focus:ring-1 focus:ring-primary focus:outline-none"
				></textarea>

				{#if error}
					<div
						class="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-xs text-destructive"
					>
						{error}
					</div>
				{/if}

				<Button onclick={handleInspect} class="w-full">Inspect Manifest</Button>
			</div>
		{:else}
			<div class="flex flex-col gap-4 overflow-y-auto py-2 pr-1">
				<!-- Header info -->
				<div class="grid grid-cols-2 gap-3 md:grid-cols-4">
					<Card>
						<CardContent class="p-3">
							<div class="text-xs text-muted-foreground">Format</div>
							<div class="text-sm font-bold tracking-wider uppercase">{inspection.format}</div>
							<div class="mt-1 truncate text-xs text-muted-foreground">{inspection.name}</div>
						</CardContent>
					</Card>
					<Card>
						<CardContent class="p-3">
							<div class="text-xs text-muted-foreground">Game & Loader</div>
							<div class="truncate text-sm font-bold">{inspection.gameVersion}</div>
							<div class="mt-1 truncate text-xs text-muted-foreground">{inspection.modLoader}</div>
						</CardContent>
					</Card>
					<Card>
						<CardContent class="p-3">
							<div class="text-xs text-muted-foreground">Server-Compatible</div>
							<div class="text-sm font-bold text-emerald-500">
								{inspection.universalCount + inspection.serverOnlyCount} mods
							</div>
							<div class="mt-1 text-xs text-muted-foreground">
								{Math.round(
									((inspection.universalCount + inspection.serverOnlyCount) /
										Math.max(1, inspection.totalMods)) *
										100
								)}% of total
							</div>
						</CardContent>
					</Card>
					<Card>
						<CardContent class="p-3">
							<div class="text-xs text-muted-foreground">Client-Only (Omit)</div>
							<div class="text-sm font-bold text-amber-500">{inspection.clientOnlyCount} mods</div>
							<div class="mt-1 text-xs text-muted-foreground">Safe to exclude on server</div>
						</CardContent>
					</Card>
				</div>

				<!-- Controls -->
				<div class="flex flex-wrap items-center justify-between gap-2 pt-2">
					<div class="flex items-center gap-1 rounded-md bg-muted p-1 text-xs">
						<button
							class="rounded px-2 py-1 {filter === 'all'
								? 'bg-background font-semibold shadow-xs'
								: 'text-muted-foreground'}"
							onclick={() => (filter = 'all')}
						>
							All ({inspection.totalMods})
						</button>
						<button
							class="rounded px-2 py-1 {filter === 'both'
								? 'bg-background font-semibold text-emerald-500 shadow-xs'
								: 'text-muted-foreground'}"
							onclick={() => (filter = 'both')}
						>
							Universal ({inspection.universalCount})
						</button>
						<button
							class="rounded px-2 py-1 {filter === 'client'
								? 'bg-background font-semibold text-amber-500 shadow-xs'
								: 'text-muted-foreground'}"
							onclick={() => (filter = 'client')}
						>
							Client-Only ({inspection.clientOnlyCount})
						</button>
						<button
							class="rounded px-2 py-1 {filter === 'server'
								? 'bg-background font-semibold text-blue-500 shadow-xs'
								: 'text-muted-foreground'}"
							onclick={() => (filter = 'server')}
						>
							Server-Only ({inspection.serverOnlyCount})
						</button>
					</div>

					<Input
						type="search"
						bind:value={searchQuery}
						placeholder="Search mods..."
						class="h-8 w-48 text-xs"
					/>
				</div>

				<!-- Mod list table -->
				<div class="max-h-64 overflow-y-auto rounded-md border">
					<table class="w-full text-left text-xs">
						<thead class="sticky top-0 border-b bg-muted/50">
							<tr>
								<th scope="col" class="p-2">Mod Name</th>
								<th scope="col" class="p-2">Environment</th>
								<th scope="col" class="p-2">Role</th>
								<th scope="col" class="p-2 text-right">Action</th>
							</tr>
						</thead>
						<tbody class="divide-y">
							{#each filteredMods as mod}
								<tr class="hover:bg-muted/20">
									<td class="max-w-[250px] truncate p-2 font-medium">
										{mod.name}
										{#if mod.filename && mod.filename !== mod.name}
											<div class="truncate font-mono text-[10px] text-muted-foreground">
												{mod.filename}
											</div>
										{/if}
									</td>
									<td class="p-2">
										{#if mod.env === 'client'}
											<Badge variant="outline" class="gap-1 border-amber-500/50 text-amber-500">
												<Laptop class="h-3 w-3" /> Client Only
											</Badge>
										{:else if mod.env === 'server'}
											<Badge variant="outline" class="gap-1 border-blue-500/50 text-blue-500">
												<Server class="h-3 w-3" /> Server Only
											</Badge>
										{:else}
											<Badge variant="outline" class="gap-1 border-emerald-500/50 text-emerald-500">
												<Layers class="h-3 w-3" /> Universal
											</Badge>
										{/if}
									</td>
									<td class="p-2">
										<span class="text-muted-foreground">
											{mod.required ? 'Required' : 'Optional'}
										</span>
									</td>
									<td class="p-2 text-right">
										{#if mod.downloadUrl}
											<a
												href={mod.downloadUrl}
												target="_blank"
												rel="noreferrer"
												class="inline-flex items-center gap-1 text-[11px] text-primary hover:underline"
											>
												<ExternalLink class="h-3 w-3" /> Link
											</a>
										{:else if mod.projectId}
											<span class="font-mono text-[10px] text-muted-foreground">
												#{mod.projectId}
											</span>
										{/if}
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</div>

			<DialogFooter class="flex flex-row items-center justify-between gap-2 border-t pt-3">
				<Button variant="outline" size="sm" onclick={() => (inspection = null)}>
					Inspect Another
				</Button>
				<div class="flex items-center gap-2">
					<Button variant="secondary" size="sm" onclick={copyServerManifest} class="gap-1 text-xs">
						<Copy class="h-3.5 w-3.5" /> Copy Server Manifest
					</Button>
					<Button size="sm" onclick={downloadServerManifest} class="gap-1 text-xs">
						<Download class="h-3.5 w-3.5" /> Export Clean Manifest
					</Button>
				</div>
			</DialogFooter>
		{/if}
	</DialogContent>
</Dialog>
