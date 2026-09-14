<script lang="ts">
	import type { Snippet } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { serversStore, runningServers, activitySortedServers } from '$lib/stores/servers';
	import { authStore, currentUser, canAccessSettings } from '$lib/stores/auth';
	import { ServerStatus, type User } from '$lib/proto/discopanel/v1/common_pb';
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

	const navItems = [
		{ href: '/', label: 'Overview', icon: 'M3 13h8V3H3v10zm0 8h8v-6H3v6zm10 0h8V11h-8v10zm0-18v6h8V3h-8z' },
		{ href: '/servers', label: 'Servers', badge: true, icon: 'M4 6h16v3H4V6zm0 5h16v3H4v-3zm0 5h16v3H4v-3z' },
		{ href: '/servers/new', label: 'Create Server', icon: 'M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z' },
		{ href: '/modpacks/studio', label: 'Modpack Studio', icon: 'M21 16.5l-9 5.2-9-5.2V7.5l9-5.2 9 5.2v9zM12 4.1L5 8.1l7 4 7-4-7-4zm-7 5.7v6.6l6 3.5v-6.6l-6-3.5zm8 10.1l6-3.5V9.8l-6 3.5v6.6z' },
		{ href: '/modpacks', label: 'Modpacks & Manifests', icon: 'M19 3H5c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2zm-5 14H7v-2h7v2zm3-4H7v-2h10v2zm0-4H7V7h10v2z' },
		{ href: '/modules', label: 'Modules & Sidecars', icon: 'M20.5 11H19V7c0-1.1-.9-2-2-2h-4V3.5a2.5 2.5 0 00-5 0V5H4c-1.1 0-1.99.9-1.99 2v3.8H3.5c1.49 0 2.7 1.21 2.7 2.7s-1.21 2.7-2.7 2.7H2V20c0 1.1.9 2 2 2h3.8v-1.5c0-1.49 1.21-2.7 2.7-2.7 1.49 0 2.7 1.21 2.7 2.7V22H17c1.1 0 2-.9 2-2v-4h1.5a2.5 2.5 0 000-5z' },
		{ href: '/settings', label: 'Settings', icon: 'M19.14 12.94c.04-.3.06-.61.06-.94 0-.32-.02-.64-.07-.94l2.03-1.58c.18-.14.23-.41.12-.61l-1.92-3.32c-.12-.22-.37-.29-.59-.22l-2.39.96c-.5-.38-1.03-.7-1.62-.94l-.36-2.54c-.04-.24-.24-.41-.48-.41h-3.84c-.24 0-.43.17-.47.41l-.36 2.54c-.59.24-1.13.57-1.62.94l-2.39-.96c-.22-.08-.47 0-.59.22L2.74 8.87c-.12.21-.08.47.12.61l2.03 1.58c-.05.3-.09.63-.09.94s.02.64.07.94l-2.03 1.58c-.18.14-.23.41-.12.61l1.92 3.32c.12.22.37.29.59.22l2.39-.96c.5.38 1.03.7 1.62.94l.36 2.54c.05.24.24.41.48.41h3.84c.24 0 .44-.17.47-.41l.36-2.54c.59-.24 1.13-.56 1.62-.94l2.39.96c.22.08.47 0 .59-.22l1.92-3.32c.12-.22.07-.47-.12-.61l-2.01-1.58zM12 15.6c-1.98 0-3.6-1.62-3.6-3.6s1.62-3.6 3.6-3.6 3.6 1.62 3.6 3.6-1.62 3.6-3.6 3.6z' },
		{ href: '/docs/api', label: 'API Reference', icon: 'M14 2H6c-1.1 0-1.99.9-1.99 2L4 20c0 1.1.89 2 1.99 2H18c1.1 0 2-.9 2-2V8l-6-6zm2 16H8v-2h8v2zm0-4H8v-2h8v2zm-3-5V3.5L18.5 9H13z' }
	];

	function isCurrentPath(path: string): boolean {
		if (path === '/') return page.url.pathname === '/';
		return page.url.pathname.startsWith(path);
	}
</script>

<div class="min-h-screen bg-[#161616] text-[#f4f4f4] font-sans antialiased flex flex-col selection:bg-[#0f62fe] selection:text-white">
	<!-- IBM Carbon Global Header (48px fixed) -->
	<header class="fixed top-0 left-0 right-0 h-12 bg-[#161616] border-b border-[#393939] z-50 flex items-center justify-between px-3">
		<div class="flex items-center gap-2">
			<!-- Hamburger Button -->
			<button
				type="button"
				onclick={() => (sideNavExpanded = !sideNavExpanded)}
				class="h-8 w-8 flex items-center justify-center hover:bg-[#353535] text-[#f4f4f4] transition-colors cursor-pointer"
				aria-label="Toggle Navigation"
			>
				<svg class="h-4 w-4" fill="currentColor" viewBox="0 0 20 20">
					<path fill-rule="evenodd" d="M3 5h14a1 1 0 010 2H3a1 1 0 010-2zm0 4h14a1 1 0 010 2H3a1 1 0 010-2zm0 4h14a1 1 0 010 2H3a1 1 0 010-2z" clip-rule="evenodd" />
				</svg>
			</button>

			<!-- Brand Logo & Name -->
			<a href="/" class="flex items-center gap-2 text-sm font-sans tracking-[0.16px] text-white hover:text-[#0f62fe] transition-colors">
				<img src="/mineserver_logo.png" alt="MineServer" class="h-6 w-6 rounded-sm" />
				<span class="font-semibold tracking-wide">MineServer</span>
				<CarbonTag type="cyan" size="sm" class="hidden md:inline-flex">Carbon UI</CarbonTag>
			</a>
		</div>

		<!-- Header Global Actions -->
		<div class="flex items-center gap-3">
			<!-- Quick Server Switcher -->
			<div class="hidden md:flex items-center gap-2 font-mono text-xs">
				<span class="text-[#8d8d8d]">RUNNING:</span>
				<CarbonTag type={runningCount > 0 ? 'green' : 'gray'} size="sm">
					{runningCount} / {servers.length}
				</CarbonTag>
			</div>

			<!-- User Profile / Status -->
			{#if user}
				<div class="flex items-center gap-2 pl-2 border-l border-[#393939]">
					<div class="h-6 w-6 bg-[#393939] text-[#f4f4f4] font-mono text-[11px] font-bold flex items-center justify-center">
						{user.username.slice(0, 2).toUpperCase()}
					</div>
					<span class="text-xs text-[#c6c6c6] hidden lg:inline">{user.username}</span>
				</div>
			{/if}
		</div>
	</header>

	<!-- Layout Body (SideNav + Content) -->
	<div class="flex-1 flex pt-12">
		<!-- IBM Carbon SideNav -->
		<aside
			class="fixed top-12 bottom-0 left-0 bg-[#161616] border-r border-[#393939] z-40 transition-all duration-200 overflow-y-auto {sideNavExpanded ? 'w-64' : 'w-12'}"
		>
			<nav class="flex flex-col py-2" aria-label="Main Navigation">
				{#each navItems as item}
					{#if item.href !== '/settings' || showSettingsNav}
						<a
							href={item.href}
							class="flex items-center gap-3 px-3.5 py-2.5 text-sm font-sans transition-colors relative {isCurrentPath(item.href) ? 'bg-[#353535] text-white font-medium before:absolute before:left-0 before:top-0 before:bottom-0 before:w-1 before:bg-[#0f62fe]' : 'text-[#c6c6c6] hover:bg-[#262626] hover:text-white'}"
							title={item.label}
						>
							<svg class="h-4 w-4 flex-shrink-0" fill="currentColor" viewBox="0 0 24 24">
								<path d={item.icon} />
							</svg>
							{#if sideNavExpanded}
								<span class="truncate flex-1">{item.label}</span>
								{#if item.badge && runningCount > 0}
									<span class="h-4 px-1.5 bg-[#0f62fe] text-white text-[10px] font-mono font-semibold flex items-center justify-center">
										{runningCount}
									</span>
								{/if}
							{/if}
						</a>
					{/if}
				{/each}
			</nav>

			<!-- SideNav Footer -->
			{#if sideNavExpanded}
				<div class="absolute bottom-0 left-0 right-0 p-3 bg-[#161616] border-t border-[#393939] text-[11px] font-mono text-[#8d8d8d]">
					<div class="flex items-center justify-between">
						<span>PROXMOX CT 108</span>
						<span class="text-emerald-500 font-bold">ONLINE</span>
					</div>
					<div class="mt-1 text-[10px] text-[#6f6f6f]">PORT 5174 · CARBON V11</div>
				</div>
			{/if}
		</aside>

		<!-- Carbon Main Content Area -->
		<main class="flex-1 transition-all duration-200 p-6 overflow-y-auto {sideNavExpanded ? 'ml-64' : 'ml-12'}">
			<div class="max-w-7xl mx-auto space-y-6">
				{@render children?.()}
			</div>
		</main>
	</div>
</div>
