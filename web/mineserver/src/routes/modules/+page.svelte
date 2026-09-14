<script lang="ts">
	import { CarbonTabs } from '$lib/components/carbon';
	import { Puzzle } from '@lucide/svelte';
	import TemplateManagement from './TemplateManagement.svelte';
	import ActiveModules from './ActiveModules.svelte';

	let activeTab = $state('templates');

	const moduleTabs = [
		{ id: 'templates', label: 'Templates' },
		{ id: 'active', label: 'Active Instances' }
	];
</script>

<svelte:head>
	<title>Modules & Sidecars - MineServer</title>
</svelte:head>

<div class="h-full flex-1 space-y-6 font-sans text-[#f4f4f4] rounded-none">
	<!-- Top Bar -->
	<div class="flex items-center justify-between border-b border-[#393939] pb-6 rounded-none">
		<div class="flex items-center gap-4">
			<div class="flex h-12 w-12 items-center justify-center bg-[#262626] border border-[#393939] text-[#0f62fe] rounded-none shadow-sm">
				<Puzzle class="h-6 w-6" />
			</div>
			<div class="space-y-0.5">
				<h1 class="text-3xl font-semibold tracking-tight text-white">Modules & Sidecars</h1>
				<p class="text-xs text-[#a8a8a8]">
					Manage global module templates and inspect active sidecar container instances
				</p>
			</div>
		</div>
	</div>

	<!-- Carbon Navigation Tabs -->
	<CarbonTabs
		tabs={moduleTabs}
		bind:selectedTab={activeTab}
	/>

	<div>
		{#if activeTab === 'templates'}
			<TemplateManagement />
		{:else if activeTab === 'active'}
			<ActiveModules />
		{/if}
	</div>
</div>
