<script lang="ts">
	import type { Snippet } from 'svelte';
	import { page } from '$app/state';
	import { auth } from '$lib/auth/clerk.svelte';
	import { can, roleLabel } from '$lib/auth/permissions';
	import { cls } from '$lib/utils';
	import Toast from './Toast.svelte';

	let {
		children,
		title = undefined
	}: {
		children: Snippet;
		title?: string;
	} = $props();

	let collapsed = $state(false);

	type NavItem = { href: string; label: string; base: string; show: boolean };
	const orgRole = () => auth.orgRole;

	const navItems = $derived.by(() => {
		const items: NavItem[] = [
			{ href: '/', label: 'Overview', base: '/', show: true },
			{ href: '/nodes', label: 'Nodes', base: '/nodes', show: true },
			{ href: '/workloads', label: 'Workloads', base: '/workloads', show: true },
			{ href: '/node-types', label: 'Node types', base: '/node-types', show: true },
			{
				href: '/members',
				label: 'Members',
				base: '/members',
				show: can(orgRole(), 'member.manage')
			},
			{ href: '/roles', label: 'Roles', base: '/roles', show: true },
			{
				href: '/api-keys',
				label: 'API keys',
				base: '/api-keys',
				show: can(orgRole(), 'api_key.view')
			},
			{ href: '/audit', label: 'Audit', base: '/audit', show: can(orgRole(), 'audit.view') },
			{ href: '/settings', label: 'Settings', base: '/settings', show: true }
		];
		return items.filter((n) => n.show);
	});

	const pathname = $derived(page.url.pathname);

	function isActive(item: NavItem): boolean {
		if (item.base === '/') return pathname === '/';
		return pathname === item.base || pathname.startsWith(item.base + '/');
	}

	function switchOrg(e: Event) {
		const id = (e.target as HTMLSelectElement).value;
		if (id) void auth.setActiveOrg(id);
	}
</script>

<div class="flex min-h-screen flex-col bg-background text-foreground">
	<header class="fixed inset-x-0 top-0 z-30 flex h-12 items-center gap-3 border-b border-border bg-background px-3">
		<button
			class="flex h-8 w-8 items-center justify-center border border-border text-muted-foreground hover:bg-accent hover:text-foreground focus-ring"
			aria-label={collapsed ? 'Expand navigation' : 'Collapse navigation'}
			aria-expanded={!collapsed}
			onclick={() => (collapsed = !collapsed)}
		>
			☰
		</button>
		<a href="/" class="flex items-center gap-2 font-semibold tracking-tight focus-ring">
			<span class="inline-block h-3 w-3 bg-primary" aria-hidden="true"></span>
			Carbon Cloud
		</a>

		<div class="flex-1"></div>

		{#if auth.memberships.length > 1}
			<label class="sr-only" for="org-switch">Switch organization</label>
			<select
				id="org-switch"
				class="h-8 border border-border bg-card px-2 text-xs text-foreground focus-ring"
				value={auth.org?.id ?? ''}
				onchange={switchOrg}
			>
				{#each auth.memberships as m (m.org.id)}
					<option value={m.org.id}>{m.org.name}</option>
				{/each}
			</select>
		{:else if auth.org}
			<span class="hidden text-xs text-muted-foreground sm:inline">{auth.org.name}</span>
		{/if}

		{#if auth.user}
			<span class="hidden text-xs text-muted-foreground md:inline">
				{auth.user.name || auth.user.email || auth.user.id}
			</span>
			<span class="hidden text-xs text-muted-foreground sm:inline">
				({roleLabel(auth.orgRole ?? 'viewer')})
			</span>
		{/if}

		<button
			class="h-8 border border-border px-2 text-xs text-muted-foreground hover:bg-accent hover:text-foreground focus-ring"
			onclick={() => void auth.signOut()}
		>
			Sign out
		</button>
	</header>

	<nav
		class={cls(
			'fixed top-12 bottom-0 left-0 z-20 flex flex-col border-r border-border bg-background transition-all duration-150',
			collapsed ? 'w-12' : 'w-64'
		)}
		aria-label="Main navigation"
	>
		<ul class="flex-1 overflow-y-auto py-2">
			{#each navItems as item (item.href)}
				<li>
					<a
						href={item.href}
						class={cls(
							'flex items-center gap-2 border-l-2 px-3 py-2 text-sm focus-ring',
							collapsed && 'justify-center px-0',
							isActive(item)
								? 'border-primary bg-accent/40 font-medium text-foreground'
								: 'border-transparent text-muted-foreground hover:bg-accent/30 hover:text-foreground'
						)}
						aria-current={isActive(item) ? 'page' : undefined}
						title={collapsed ? item.label : undefined}
					>
						<span class="h-1.5 w-1.5 shrink-0 bg-primary" aria-hidden="true"></span>
						{#if !collapsed}<span class="truncate">{item.label}</span>{/if}
					</a>
				</li>
			{/each}
		</ul>
		<div class="border-t border-border px-3 py-2 text-[10px] text-muted-foreground">
			{#if !collapsed}Carbon Cloud console{/if}
		</div>
	</nav>

	<main
		class={cls(
			'mt-12 flex-1 overflow-x-hidden px-6 py-6 transition-all duration-150',
			collapsed ? 'ml-12' : 'ml-64'
		)}
	>
		{#if title}
			<h1 class="mb-4 text-xl font-semibold text-foreground">{title}</h1>
		{/if}
		{@render children()}
	</main>

	<Toast />
</div>