<!--
SPDX-License-Identifier: MPL-2.0
This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->


<script lang="ts">
	interface BreadcrumbItem {
		href?: string;
		label: string;
	}

	interface Props {
		items: BreadcrumbItem[];
		class?: string;
	}

	let { items = [], class: className = '' }: Props = $props();
</script>

<nav
	class="flex items-center gap-1 font-sans text-xs text-[#a8a8a8] {className}"
	aria-label="Breadcrumbs"
>
	{#each items as item, i (item.label + i)}
		{#if i > 0}
			<span class="text-[#a8a8a8]">/</span>
		{/if}
		{#if item.href && i < items.length - 1}
			<!-- eslint-disable svelte/no-navigation-without-resolve -- generic component; callers must resolve -->
			<a href={item.href} class="transition-colors hover:text-white hover:underline">
				<!-- eslint-enable svelte/no-navigation-without-resolve -->
				{item.label}
			</a>
		{:else}
			<span class="font-medium text-[#f4f4f4]">{item.label}</span>
		{/if}
	{/each}
</nav>
