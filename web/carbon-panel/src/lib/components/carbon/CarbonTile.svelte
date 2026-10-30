<!--
SPDX-License-Identifier: MPL-2.0
This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->

<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		clickable?: boolean;
		href?: string;
		class?: string;
		onclick?: () => void;
		children?: Snippet;
	}

	let {
		clickable = false,
		href = undefined,
		class: className = '',
		onclick,
		children
	}: Props = $props();

	const baseClasses =
		'bg-card border border-border p-[var(--carbon-space-4)] text-card-foreground transition-[background-color,border-color,transform] duration-[var(--motion-base)] ease-[var(--ease-out-quart)] relative select-none';
	const hoverClasses =
		clickable || href
			? 'hover:bg-accent hover:border-input hover:-translate-y-px cursor-pointer focus-ring'
			: '';
</script>

{#if href}
	<!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- href is caller-provided; callers pass resolve()d paths -->
	<a {href} class="{baseClasses} {hoverClasses} block {className}">
		{@render children?.()}
	</a>
	<!-- eslint-enable svelte/no-navigation-without-resolve -->
{:else if clickable}
	<div
		role="button"
		tabindex="0"
		{onclick}
		onkeydown={(e) => (e.key === 'Enter' || e.key === ' ') && onclick?.()}
		class="{baseClasses} {hoverClasses} {className}"
	>
		{@render children?.()}
	</div>
{:else}
	<div class="{baseClasses} {className}">
		{@render children?.()}
	</div>
{/if}
