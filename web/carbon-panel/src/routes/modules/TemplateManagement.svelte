<script lang="ts">
	import {
		CarbonButton,
		CarbonDataTable,
		CarbonTag,
		CarbonSelect,
		CarbonInlineLoading
	} from '$lib/components/carbon';
	import DynamicIcon from '$lib/components/ui/DynamicIcon.svelte';
	import { rpcClient } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import type { ModuleTemplate } from '$lib/proto/carbonpanel/v1/module_pb';
	import { ModuleTemplateType } from '$lib/proto/carbonpanel/v1/module_pb';
	import { Plus, Trash2, Settings, RefreshCw, Layers } from '@lucide/svelte';
	import ModuleTemplateCreateDialog from '$lib/components/server/ModuleTemplateCreateDialog.svelte';
	import { onMount } from 'svelte';

	let templates = $state<ModuleTemplate[]>([]);
	let loading = $state(true);

	// Dialog state
	let createDialogOpen = $state(false);
	let editDialogOpen = $state(false);
	let selectedTemplate = $state<ModuleTemplate | null>(null);

	onMount(() => {
		loadTemplates();
	});

	async function loadTemplates(silent = false) {
		try {
			if (!silent) loading = true;
			const response = await rpcClient.module.listModuleTemplates({});
			templates = response.templates;
		} catch {
			if (!silent) toast.error('Failed to load module templates');
		} finally {
			if (!silent) loading = false;
		}
	}

	async function handleDeleteTemplate(template: ModuleTemplate) {
		const confirmed = confirm(
			`Are you sure you want to delete template "${template.name}"?\n\nThis cannot be undone and will not affect existing instances.`
		);
		if (!confirmed) return;

		try {
			await rpcClient.module.deleteModuleTemplate({ id: template.id });
			toast.success(`Template "${template.name}" deleted`);
			await loadTemplates(true);
		} catch (error) {
			toast.error(
				`Failed to delete template: ${error instanceof Error ? error.message : 'Unknown error'}`
			);
		}
	}

	function openEditDialog(template: ModuleTemplate) {
		selectedTemplate = template;
		editDialogOpen = true;
	}

	let categories = $derived.by(() => {
		const cats = new Set<string>();
		templates.forEach((t) => {
			if (t.category) cats.add(t.category);
		});
		return Array.from(cats).sort();
	});

	let selectedCategory = $state<string>('');

	let filteredTemplates = $derived.by(() => {
		if (!selectedCategory) return templates;
		return templates.filter((t) => t.category === selectedCategory);
	});
</script>

<div class="space-y-4 rounded-none font-sans text-[#f4f4f4]">
	{#snippet templateToolbar()}
		<div class="flex flex-wrap items-center gap-2">
			<div class="w-36">
				<CarbonSelect bind:value={selectedCategory}>
					<option value="">All Categories</option>
					{#each categories as cat}
						<option value={cat}>{cat}</option>
					{/each}
				</CarbonSelect>
			</div>

			<CarbonButton
				kind="tertiary"
				size="sm"
				class="rounded-none"
				onclick={() => loadTemplates()}
				disabled={loading}
				title="Refresh templates"
			>
				<RefreshCw class={`mr-1.5 h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
				Refresh
			</CarbonButton>

			<CarbonButton
				kind="primary"
				size="sm"
				class="rounded-none"
				onclick={() => (createDialogOpen = true)}
			>
				<Plus class="mr-1.5 h-4 w-4" />
				Create Template
			</CarbonButton>
		</div>
	{/snippet}

	{#snippet templateHeader()}
		<tr>
			<th scope="col" class="px-4 py-3 text-xs font-semibold tracking-wider uppercase"
				>Template Name</th
			>
			<th scope="col" class="px-4 py-3 text-xs font-semibold tracking-wider uppercase">Type</th>
			<th scope="col" class="px-4 py-3 text-xs font-semibold tracking-wider uppercase">Category</th>
			<th scope="col" class="px-4 py-3 text-xs font-semibold tracking-wider uppercase"
				>Docker Image</th
			>
			<th scope="col" class="px-4 py-3 text-xs font-semibold tracking-wider uppercase"
				>Description</th
			>
			<th scope="col" class="px-4 py-3 text-right text-xs font-semibold tracking-wider uppercase"
				>Actions</th
			>
		</tr>
	{/snippet}

	<CarbonDataTable
		title="Module Templates"
		description="Blueprints for provisioning containerized services, monitoring sidecars, and proxies"
		toolbar={templateToolbar}
		header={templateHeader}
		class="rounded-none"
	>
		{#if loading && templates.length === 0}
			<tr>
				<td colspan="6" class="py-16 text-center">
					<div class="flex items-center justify-center">
						<CarbonInlineLoading description="Loading templates..." />
					</div>
				</td>
			</tr>
		{:else if filteredTemplates.length === 0}
			<tr>
				<td colspan="6" class="py-16 text-center text-[#8d8d8d]">
					<Layers class="mx-auto mb-3 h-10 w-10 text-[#525252]" />
					<p class="text-sm font-semibold text-white">No module templates found</p>
					<p class="mt-1 text-xs text-[#8d8d8d]">
						Create your first custom module blueprint to get started.
					</p>
					<div class="mt-4">
						<CarbonButton size="sm" class="rounded-none" onclick={() => (createDialogOpen = true)}>
							<Plus class="mr-1.5 h-4 w-4" />
							Create Template
						</CarbonButton>
					</div>
				</td>
			</tr>
		{:else}
			{#each filteredTemplates as template (template.name)}
				<tr class="transition-colors hover:bg-[#353535]">
					<td class="px-4 py-3 font-medium text-white">
						<div class="flex items-center gap-2.5">
							<div
								class="flex h-7 w-7 shrink-0 items-center justify-center rounded-none border border-[#393939] bg-[#161616] text-[#0f62fe]"
							>
								<DynamicIcon
									name={template.icon}
									class="h-4 w-4 text-[#0f62fe]"
									fallback="Package"
								/>
							</div>
							<span class="text-sm font-semibold">{template.name}</span>
						</div>
					</td>

					<td class="px-4 py-3">
						{#if template.type === ModuleTemplateType.BUILTIN}
							<CarbonTag type="blue" size="sm">Built-in</CarbonTag>
						{:else}
							<CarbonTag type="purple" size="sm">Custom</CarbonTag>
						{/if}
					</td>

					<td class="px-4 py-3">
						{#if template.category}
							<CarbonTag type="gray" size="sm">{template.category}</CarbonTag>
						{:else}
							<span class="font-mono text-xs text-[#8d8d8d]">-</span>
						{/if}
					</td>

					<td
						class="max-w-[180px] truncate px-4 py-3 font-mono text-xs text-[#a8a8a8]"
						title={template.dockerImage}
					>
						{template.dockerImage}
					</td>

					<td
						class="max-w-[240px] truncate px-4 py-3 text-xs text-[#a8a8a8]"
						title={template.description}
					>
						{template.description || 'No description provided'}
					</td>

					<td class="px-4 py-3 text-right">
						<div class="flex items-center justify-end gap-1">
							{#if template.type === ModuleTemplateType.CUSTOM}
								<CarbonButton
									kind="ghost"
									size="sm"
									iconOnly
									class="rounded-none text-[#c6c6c6] hover:text-white"
									onclick={() => openEditDialog(template)}
									title="Edit template"
								>
									<Settings class="h-3.5 w-3.5" />
								</CarbonButton>
								<CarbonButton
									kind="ghost"
									size="sm"
									iconOnly
									class="rounded-none text-[#da1e28] hover:bg-[#da1e28]/20"
									onclick={() => handleDeleteTemplate(template)}
									title="Delete template"
								>
									<Trash2 class="h-3.5 w-3.5" />
								</CarbonButton>
							{:else}
								<CarbonTag type="gray" size="sm">Read-only</CarbonTag>
							{/if}
						</div>
					</td>
				</tr>
			{/each}
		{/if}
	</CarbonDataTable>
</div>

<ModuleTemplateCreateDialog
	bind:open={createDialogOpen}
	mode="create"
	onSuccess={() => loadTemplates(true)}
/>

{#if selectedTemplate}
	<ModuleTemplateCreateDialog
		bind:open={editDialogOpen}
		mode="edit"
		template={selectedTemplate}
		onSuccess={() => loadTemplates(true)}
	/>
{/if}
