<!--
SPDX-License-Identifier: MIT AND MPL-2.0
This file includes DiscoPanel material under the MIT License,
Copyright (c) 2025 Nicholas W. Heyer. The MIT license text is in LICENSE
and still applies to that inherited material.
Maintainer changes are licensed under the Mozilla Public License, v. 2.0.
If a copy of the MPL was not distributed with this file, You can obtain
one at https://mozilla.org/MPL/2.0/.
-->

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
	<title>Modules & Sidecars - Carbon Panel</title>
</svelte:head>

<div class="h-full flex-1 space-y-6 rounded-none font-sans text-[#f4f4f4]">
	<!-- Top Bar -->
	<div class="flex items-center justify-between rounded-none border-b border-[#393939] pb-6">
		<div class="flex items-center gap-4">
			<div
				class="flex h-12 w-12 items-center justify-center rounded-none border border-[#393939] bg-[#262626] text-accent-text shadow-sm"
			>
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
	<CarbonTabs tabs={moduleTabs} bind:selectedTab={activeTab} />

	{#key activeTab}
		<div class="motion-fade-in">
			{#if activeTab === 'templates'}
				<TemplateManagement />
			{:else if activeTab === 'active'}
				<ActiveModules />
			{/if}
		</div>
	{/key}
</div>
