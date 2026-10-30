<!--
SPDX-License-Identifier: MPL-2.0
This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->

<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { HTMLButtonAttributes, HTMLAnchorAttributes } from 'svelte/elements';

	type BaseProps = {
		kind?: 'primary' | 'secondary' | 'tertiary' | 'ghost' | 'danger';
		size?: 'sm' | 'md' | 'lg' | 'xl';
		iconOnly?: boolean;
		disabled?: boolean;
		loading?: boolean;
		children?: Snippet;
		class?: string;
	};

	type ButtonProps = BaseProps & HTMLButtonAttributes & { href?: undefined };
	type AnchorProps = BaseProps & HTMLAnchorAttributes & { href: string };
	type Props = ButtonProps | AnchorProps;

	let {
		kind = 'primary',
		size = 'md',
		iconOnly = false,
		disabled = false,
		loading = false,
		onclick = undefined,
		href = undefined,
		class: className = '',
		children,
		...restProps
	}: Props = $props();

	const baseClasses =
		'inline-flex items-center justify-between font-sans text-sm font-normal tracking-[0.16px] transition-[color,background-color,border-color,transform] motion-press focus-ring cursor-pointer disabled:cursor-not-allowed disabled:opacity-50 select-none';

	const sizeClasses = {
		sm: 'h-[var(--carbon-control-sm)] px-3 text-xs',
		md: 'h-[var(--carbon-control-md)] px-4 text-sm',
		lg: 'h-[var(--carbon-control-lg)] px-4 text-sm',
		xl: 'h-[var(--carbon-control-xl)] px-4 text-base'
	};

	const kindClasses = {
		primary:
			'bg-primary text-primary-foreground hover:bg-[var(--button-primary-hover)] active:bg-[var(--button-primary-active)] border border-transparent',
		secondary:
			'bg-secondary text-secondary-foreground hover:bg-[var(--button-secondary-hover)] active:bg-[var(--button-secondary-active)] border border-transparent',
		tertiary:
			'bg-transparent text-foreground hover:bg-accent active:bg-[var(--button-secondary-active)] border border-foreground',
		ghost:
			'bg-transparent text-accent-text hover:bg-accent hover:text-foreground active:bg-[var(--button-secondary-active)] active:text-foreground border border-transparent disabled:hover:bg-transparent',
		danger:
			'bg-destructive text-white hover:bg-[var(--button-danger-hover)] active:bg-[var(--button-danger-active)] border border-transparent'
	};
</script>

{#if href}
	<!-- eslint-disable svelte/no-navigation-without-resolve -- href is caller-provided; callers pass resolve()d paths -->
	<a
		{...restProps as HTMLAnchorAttributes}
		href={disabled || loading ? undefined : href}
		role={disabled || loading ? 'link' : undefined}
		aria-disabled={disabled || loading ? 'true' : undefined}
		tabindex={disabled || loading ? -1 : undefined}
		aria-busy={loading ? 'true' : undefined}
		class="{baseClasses} {sizeClasses[size]} {kindClasses[kind]} {iconOnly
			? '!h-[var(--carbon-control-md)] !w-[var(--carbon-control-md)] !justify-center !p-2'
			: ''} {disabled || loading
			? 'pointer-events-none cursor-not-allowed opacity-50'
			: ''} {className}"
		onclick={(event) => {
			if (disabled || loading) {
				event.preventDefault();
				return;
			}
			(onclick as HTMLAnchorAttributes['onclick'])?.(event);
		}}
	>
		{#if loading}
			<span
				class="h-4 w-4 shrink-0 animate-spin border-2 border-current border-t-transparent"
				aria-hidden="true"
			></span>
		{/if}
		{@render children?.()}
	</a>
	<!-- eslint-enable svelte/no-navigation-without-resolve -->
{:else}
	<button
		type="button"
		disabled={disabled || loading}
		aria-busy={loading ? 'true' : undefined}
		class="{baseClasses} {sizeClasses[size]} {kindClasses[kind]} {iconOnly
			? '!h-[var(--carbon-control-md)] !w-[var(--carbon-control-md)] !justify-center !p-2'
			: ''} {className}"
		{...restProps as HTMLButtonAttributes}
		onclick={onclick as HTMLButtonAttributes['onclick']}
	>
		{#if loading}
			<span
				class="h-4 w-4 shrink-0 animate-spin border-2 border-current border-t-transparent"
				aria-hidden="true"
			></span>
		{/if}
		{@render children?.()}
	</button>
{/if}
