<!--
SPDX-License-Identifier: MPL-2.0
This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->

<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { HTMLSelectAttributes } from 'svelte/elements';

	interface Props extends HTMLSelectAttributes {
		label?: string;
		helperText?: string;
		error?: string;
		value?: string | number;
		class?: string;
		children?: Snippet;
	}

	const autoId = $props.id();
	let {
		label = '',
		helperText = '',
		error = '',
		value = $bindable(''),
		class: className = '',
		id = autoId,
		children,
		...restProps
	}: Props = $props();

	// Associate the visible label and the helper/error text with the select.
	const describedById = `${id}-desc`;
	const hasDescription = $derived(Boolean(error || helperText));
</script>

<div class="flex flex-col space-y-1 rounded-none font-sans {className}">
	{#if label}
		<label for={id} class="text-xs font-normal tracking-[0.32px] text-foreground">
			{label}
		</label>
	{/if}
	<div class="relative rounded-none">
		<select
			{id}
			aria-describedby={hasDescription ? describedById : undefined}
			aria-invalid={error ? 'true' : undefined}
			bind:value
			class="focus-ring h-[var(--carbon-control-md)] w-full cursor-pointer appearance-none rounded-none border-b border-input bg-card pr-10 pl-4 text-sm text-foreground transition-[color,background-color,border-color] focus:border-b-2 focus:border-ring focus:bg-accent disabled:border-border disabled:bg-background disabled:text-muted-foreground {error
				? '!border-b-2 !border-destructive'
				: ''}"
			{...restProps}
		>
			{@render children?.()}
		</select>
		<div
			class="pointer-events-none absolute inset-y-0 right-0 flex items-center px-3 text-foreground"
		>
			<svg class="h-4 w-4" fill="currentColor" viewBox="0 0 20 20">
				<path
					fill-rule="evenodd"
					d="M5.293 7.293a1 1 0 011.414 0L10 10.586l3.293-3.293a1 1 0 111.414 1.414l-4 4a1 1 0 01-1.414 0l-4-4a1 1 0 010-1.414z"
					clip-rule="evenodd"
				/>
			</svg>
		</div>
	</div>
	{#if error}
		<span
			id={describedById}
			role="alert"
			class="motion-rise-in mt-0.5 font-sans text-xs text-destructive-text">{error}</span
		>
	{:else if helperText}
		<span id={describedById} class="motion-rise-in mt-0.5 font-sans text-xs text-muted-foreground"
			>{helperText}</span
		>
	{/if}
</div>
