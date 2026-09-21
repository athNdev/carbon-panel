<script lang="ts">
	import type { Snippet } from 'svelte';
	import { onMount } from 'svelte';
	import { fade } from 'svelte/transition';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { serversStore, runningServers, activitySortedServers } from '$lib/stores/servers';
	import { authStore, currentUser, canAccessSettings } from '$lib/stores/auth';
	import { ServerStatus, type User } from '$lib/proto/carbonpanel/v1/common_pb';
	import { rpcClient, silentCallOptions } from '$lib/api/rpc-client';
	import { type Node } from '$lib/proto/carbonpanel/v1/node_pb';
	import { describeNodeStatus, pickPrimaryNode } from '$lib/utils/node-status';
	import CarbonTag from './CarbonTag.svelte';
	import CarbonButton from './CarbonButton.svelte';

	interface Props {
		children?: Snippet;
	}

	let { children }: Props = $props();

	let sideNavExpanded = $state(true);
	let servers = $derived($activitySortedServers);
	let runningCount = $derived($runningServers.length);
	let user = $derived($currentUser);
	let showSettingsNav = $derived($canAccessSettings);

	// Narrow-screen breakpoint mirrors IsMobile (768px) and Tailwind's md: prefix.
	const MOBILE_QUERY = '(max-width: 767px)';

	let isMobile = $state(false);
	let nodes = $state<Node[]>([]);
	let nodesLoaded = $state(false);
	let appPort = $state('');

	let primaryNode = $derived(pickPrimaryNode(nodes));
	let primaryNodeStatus = $derived(describeNodeStatus(primaryNode?.status));

	onMount(() => {
		const media = window.matchMedia(MOBILE_QUERY);
		const applyBreakpoint = () => {
			isMobile = media.matches;
			// Carbon behaviour: the sidenav starts hidden behind the hamburger on narrow screens.
			if (media.matches) sideNavExpanded = false;
		};
		applyBreakpoint();
		media.addEventListener('change', applyBreakpoint);

		appPort = window.location.port || (window.location.protocol === 'https:' ? '443' : '80');

		// Best-effort: the shell must render even when the node list fails (e.g. anonymous access).
		rpcClient.node
			.listNodes({}, silentCallOptions)
			.then((res) => {
				nodes = res.nodes ?? [];
			})
			.catch((err) => {
				console.error('Failed to fetch nodes for shell footer:', err);
			})
			.finally(() => {
				nodesLoaded = true;
			});

		return () => {
			media.removeEventListener('change', applyBreakpoint);
		};
	});

	function toggleNav() {
		sideNavExpanded = !sideNavExpanded;
	}

	function closeMobileNav() {
		if (isMobile) sideNavExpanded = false;
	}

	function handleWindowKeydown(event: KeyboardEvent) {
		if (event.key === 'Escape') closeMobileNav();
	}

	const navItems = [
		{
			href: '/',
			label: 'Overview',
			icon: 'M3 13h8V3H3v10zm0 8h8v-6H3v6zm10 0h8V11h-8v10zm0-18v6h8V3h-8z'
		},
		{
			href: '/servers',
			label: 'Servers',
			badge: true,
			icon: 'M4 6h16v3H4V6zm0 5h16v3H4v-3zm0 5h16v3H4v-3z'
		},
		{ href: '/servers/new', label: 'Create Server', icon: 'M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z' },
		{
			href: '/modpacks/studio',
			label: 'Modpack Studio',
			icon: 'M21 16.5l-9 5.2-9-5.2V7.5l9-5.2 9 5.2v9zM12 4.1L5 8.1l7 4 7-4-7-4zm-7 5.7v6.6l6 3.5v-6.6l-6-3.5zm8 10.1l6-3.5V9.8l-6 3.5v6.6z'
		},
		{
			href: '/modpacks',
			label: 'Modpacks & Manifests',
			icon: 'M19 3H5c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2zm-5 14H7v-2h7v2zm3-4H7v-2h10v2zm0-4H7V7h10v2z'
		},
		{
			href: '/modules',
			label: 'Modules & Sidecars',
			icon: 'M20.5 11H19V7c0-1.1-.9-2-2-2h-4V3.5a2.5 2.5 0 00-5 0V5H4c-1.1 0-1.99.9-1.99 2v3.8H3.5c1.49 0 2.7 1.21 2.7 2.7s-1.21 2.7-2.7 2.7H2V20c0 1.1.9 2 2 2h3.8v-1.5c0-1.49 1.21-2.7 2.7-2.7 1.49 0 2.7 1.21 2.7 2.7V22H17c1.1 0 2-.9 2-2v-4h1.5a2.5 2.5 0 000-5z'
		},
		{
			href: '/settings',
			label: 'Settings',
			icon: 'M19.14 12.94c.04-.3.06-.61.06-.94 0-.32-.02-.64-.07-.94l2.03-1.58c.18-.14.23-.41.12-.61l-1.92-3.32c-.12-.22-.37-.29-.59-.22l-2.39.96c-.5-.38-1.03-.7-1.62-.94l-.36-2.54c-.04-.24-.24-.41-.48-.41h-3.84c-.24 0-.43.17-.47.41l-.36 2.54c-.59.24-1.13.57-1.62.94l-2.39-.96c-.22-.08-.47 0-.59.22L2.74 8.87c-.12.21-.08.47.12.61l2.03 1.58c-.05.3-.09.63-.09.94s.02.64.07.94l-2.03 1.58c-.18.14-.23.41-.12.61l1.92 3.32c.12.22.37.29.59.22l2.39-.96c.5.38 1.03.7 1.62.94l.36 2.54c.05.24.24.41.48.41h3.84c.24 0 .44-.17.47-.41l.36-2.54c.59-.24 1.13-.56 1.62-.94l2.39.96c.22.08.47 0 .59-.22l1.92-3.32c.12-.22.07-.47-.12-.61l-2.01-1.58zM12 15.6c-1.98 0-3.6-1.62-3.6-3.6s1.62-3.6 3.6-3.6 3.6 1.62 3.6 3.6-1.62 3.6-3.6 3.6z'
		},
		{
			href: '/docs/api',
			label: 'API Reference',
			icon: 'M14 2H6c-1.1 0-1.99.9-1.99 2L4 20c0 1.1.89 2 1.99 2H18c1.1 0 2-.9 2-2V8l-6-6zm2 16H8v-2h8v2zm0-4H8v-2h8v2zm-3-5V3.5L18.5 9H13z'
		}
	];

	let activeNavHref = $derived.by(() => {
		const current = page.url.pathname;
		const matching = navItems.filter((item) => {
			if (item.href === '/') return current === '/';
			return current === item.href || current.startsWith(item.href + '/');
		});
		if (matching.length === 0) return null;
		return matching.reduce((prev, curr) => (curr.href.length > prev.href.length ? curr : prev))
			.href;
	});

	function isCurrentPath(path: string): boolean {
		return activeNavHref === path;
	}
</script>

<svelte:window onkeydown={handleWindowKeydown} />

<div
	class="flex min-h-screen flex-col bg-[#161616] font-sans text-[#f4f4f4] antialiased selection:bg-[#0f62fe] selection:text-white"
>
	<!-- IBM Carbon Global Header (48px fixed) -->
	<header
		class="fixed top-0 right-0 left-0 z-50 flex h-12 items-center justify-between border-b border-[#393939] bg-[#161616] px-3"
	>
		<div class="flex items-center gap-2">
			<!-- Hamburger Button -->
			<button
				type="button"
				onclick={toggleNav}
				aria-expanded={sideNavExpanded}
				aria-controls="carbon-sidenav"
				class="flex h-8 w-8 cursor-pointer items-center justify-center text-[#f4f4f4] transition-colors hover:bg-[#353535]"
				aria-label="Toggle Navigation"
			>
				<svg class="h-4 w-4" fill="currentColor" viewBox="0 0 20 20">
					<path
						fill-rule="evenodd"
						d="M3 5h14a1 1 0 010 2H3a1 1 0 010-2zm0 4h14a1 1 0 010 2H3a1 1 0 010-2zm0 4h14a1 1 0 010 2H3a1 1 0 010-2z"
						clip-rule="evenodd"
					/>
				</svg>
			</button>

			<!-- Brand Logo & Name -->
			<a
				href="/"
				class="flex items-center gap-2 font-sans text-sm tracking-[0.16px] text-white transition-colors hover:text-[#0f62fe]"
			>
				<img src="/carbon_panel_logo.png" alt="Carbon Panel" class="h-6 w-6 rounded-sm" />
				<span class="font-semibold tracking-wide">Carbon Panel</span>
				<CarbonTag type="cyan" size="sm" class="hidden md:inline-flex">Carbon UI</CarbonTag>
			</a>
		</div>

		<!-- Header Global Actions -->
		<div class="flex items-center gap-3">
			<!-- Quick Server Switcher -->
			<div class="hidden items-center gap-2 font-mono text-xs md:flex">
				<span class="text-[#8d8d8d]">RUNNING:</span>
				<CarbonTag type={runningCount > 0 ? 'green' : 'gray'} size="sm">
					{runningCount} / {servers.length}
				</CarbonTag>
			</div>

			<!-- User Profile / Status -->
			{#if user}
				<div class="flex items-center gap-2 border-l border-[#393939] pl-2">
					<div
						class="flex h-6 w-6 items-center justify-center bg-[#393939] font-mono text-[11px] font-bold text-[#f4f4f4]"
					>
						{user.username.slice(0, 2).toUpperCase()}
					</div>
					<span class="hidden text-xs text-[#c6c6c6] lg:inline">{user.username}</span>
				</div>
			{/if}
		</div>
	</header>

	<!-- Layout Body (SideNav + Content) -->
	<div class="flex flex-1 pt-12">
		<!-- IBM Carbon SideNav: off-canvas overlay on narrow screens, rail/panel on md+ -->
		<aside
			id="carbon-sidenav"
			class="fixed top-12 bottom-0 left-0 z-40 w-64 overflow-x-hidden overflow-y-auto border-r border-[#393939] bg-[#161616] transition-[width,transform] duration-[var(--motion-slow)] ease-[var(--ease-in-out-standard)] {sideNavExpanded
				? 'translate-x-0'
				: '-translate-x-full'} {sideNavExpanded ? 'md:w-64' : 'md:w-12'} md:translate-x-0"
		>
			<nav class="motion-stagger flex flex-col py-2" aria-label="Main Navigation">
				{#each navItems as item}
					{#if item.href !== '/settings' || showSettingsNav}
						<a
							href={item.href}
							onclick={closeMobileNav}
							class="relative flex items-center gap-3 px-3.5 py-2.5 font-sans text-sm transition-colors {isCurrentPath(
								item.href
							)
								? 'bg-[#353535] font-medium text-white before:absolute before:top-0 before:bottom-0 before:left-0 before:w-1 before:origin-center before:bg-[#0f62fe] before:transition-transform before:duration-[var(--motion-fast)] before:ease-[var(--ease-out-quart)]'
								: 'text-[#c6c6c6] hover:bg-[#262626] hover:text-white'}"
							title={item.label}
						>
							<svg class="h-4 w-4 flex-shrink-0" fill="currentColor" viewBox="0 0 24 24">
								<path d={item.icon} />
							</svg>
							{#if sideNavExpanded}
								<span class="flex-1 truncate">{item.label}</span>
								{#if item.badge && runningCount > 0}
									<span
										class="flex h-4 items-center justify-center bg-[#0f62fe] px-1.5 font-mono text-[10px] font-semibold text-white"
									>
										{runningCount}
									</span>
								{/if}
							{/if}
						</a>
					{/if}
				{/each}
			</nav>

			<!-- SideNav Footer: live node status, links to Docker Nodes settings -->
			{#if sideNavExpanded}
				<a
					href="/settings?tab=nodes"
					onclick={closeMobileNav}
					title="View Docker nodes"
					class="absolute right-0 bottom-0 left-0 border-t border-[#393939] bg-[#161616] p-3 font-mono text-[11px] text-[#8d8d8d] transition-colors hover:bg-[#262626]"
				>
					<div class="flex items-center justify-between gap-2">
						<span class="truncate"
							>{nodesLoaded
								? primaryNode
									? primaryNode.name.toUpperCase()
									: 'NO NODES'
								: '···'}</span
						>
						<span class="{primaryNodeStatus.dotClass} shrink-0 font-bold"
							>{primaryNodeStatus.label}</span
						>
					</div>
					<div class="mt-1 text-[10px] text-[#6f6f6f]">PORT {appPort || '···'} · CARBON V11</div>
				</a>
			{/if}
		</aside>

		<!-- Backdrop for the off-canvas sidenav on narrow screens -->
		{#if isMobile && sideNavExpanded}
			<button
				type="button"
				aria-label="Close navigation"
				onclick={closeMobileNav}
				transition:fade={{ duration: 150 }}
				class="fixed inset-0 top-12 z-30 cursor-default bg-black/60 md:hidden"
			></button>
		{/if}

		<!-- Carbon Main Content Area -->
		<main
			class="ml-0 flex-1 overflow-y-auto p-6 transition-[margin] duration-[var(--motion-slow)] ease-[var(--ease-in-out-standard)] {sideNavExpanded
				? 'md:ml-64'
				: 'md:ml-12'}"
		>
			<div class="mx-auto max-w-7xl space-y-6">
				{@render children?.()}
			</div>
		</main>
	</div>
</div>
