<!--
SPDX-License-Identifier: MPL-2.0
This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->

<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		type?:
			| 'blue'
			| 'green'
			| 'red'
			| 'purple'
			| 'cyan'
			| 'teal'
			| 'magenta'
			| 'yellow'
			| 'gray'
			| 'warm-gray'
			| 'cool-gray';
		size?: 'sm' | 'md';
		filter?: boolean;
		pulse?: boolean;
		onclose?: () => void;
		class?: string;
		children?: Snippet;
	}

	let {
		type = 'gray',
		size = 'md',
		filter = false,
		pulse = false,
		onclose,
		class: className = '',
		children
	}: Props = $props();

	const typeClasses = {
		blue: 'bg-[var(--tag-blue-bg)] text-[var(--tag-blue-text)] border border-current/30',
		green: 'bg-[var(--tag-green-bg)] text-[var(--tag-green-text)] border border-current/30',
		red: 'bg-[var(--tag-red-bg)] text-[var(--tag-red-text)] border border-current/30',
		purple: 'bg-[var(--tag-purple-bg)] text-[var(--tag-purple-text)] border border-current/30',
		cyan: 'bg-[var(--tag-cyan-bg)] text-[var(--tag-cyan-text)] border border-current/30',
		teal: 'bg-[var(--tag-teal-bg)] text-[var(--tag-teal-text)] border border-current/30',
		magenta: 'bg-[var(--tag-magenta-bg)] text-[var(--tag-magenta-text)] border border-current/30',
		yellow: 'bg-[var(--tag-yellow-bg)] text-[var(--tag-yellow-text)] border border-current/30',
		gray: 'bg-[var(--tag-gray-bg)] text-[var(--tag-gray-text)] border border-current/30',
		'warm-gray':
			'bg-[var(--tag-warm-gray-bg)] text-[var(--tag-warm-gray-text)] border border-current/30',
		'cool-gray':
			'bg-[var(--tag-cool-gray-bg)] text-[var(--tag-cool-gray-text)] border border-current/30'
	};

	const sizeClasses = {
		sm: 'h-5 px-1.5 text-xs font-mono',
		md: 'h-6 px-2 text-xs font-mono'
	};
</script>

<span
	class="inline-flex items-center gap-1 rounded-none font-mono font-semibold tracking-wider uppercase transition-[color,background-color,border-color] duration-[var(--motion-fast)] ease-[var(--ease-out-quart)] select-none {pulse
		? 'motion-pulse'
		: ''} {sizeClasses[size]} {typeClasses[type]} {className}"
>
	{@render children?.()}
	{#if filter}
		<button
			type="button"
			onclick={onclose}
			class="ml-1 cursor-pointer hover:opacity-75 focus:outline-none"
			aria-label="Remove filter"
		>
			×
		</button>
	{/if}
</span>
