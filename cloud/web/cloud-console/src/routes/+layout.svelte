<script lang="ts">
	import '../app.css';
	import type { Snippet } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import AppShell from '$lib/components/AppShell.svelte';
	import Button from '$lib/components/Button.svelte';
	import { auth } from '$lib/auth/clerk.svelte';
	import { roleLabel } from '$lib/auth/permissions';

	let { children }: { children: Snippet } = $props();

	auth.init();

	const PUBLIC_PATHS = new Set(['/sign-in', '/sign-up', '/not-configured']);

	const pathname = $derived(page.url.pathname);
	const isPublic = $derived(PUBLIC_PATHS.has(pathname));

	$effect(() => {
		const path = pathname;
		if (PUBLIC_PATHS.has(path)) {
			if (auth.signedIn && (path === '/sign-in' || path === '/sign-up')) {
				void goto('/', { replaceState: true });
			}
			return;
		}
		if (auth.notConfigured) {
			void goto('/not-configured', { replaceState: true });
		} else if (auth.ready && !auth.signedIn) {
			void goto('/sign-in', { replaceState: true });
		}
	});
</script>

{#snippet pageShell(inner: Snippet)}
	<AppShell>{@render inner()}</AppShell>
{/snippet}

{#if isPublic}
	{@render children()}
{:else if !auth.ready && !auth.notConfigured}
	<div class="flex min-h-screen items-center justify-center bg-background text-foreground">
		<p class="animate-pulse text-sm text-muted-foreground">Loading…</p>
	</div>
{:else if auth.ready && auth.signedIn && !auth.org && auth.memberships.length === 0}
	<div class="flex min-h-screen items-center justify-center bg-background p-6 text-foreground">
		<div class="w-full max-w-md border border-border bg-card p-6">
			<h1 class="mb-1 text-lg font-semibold">No organization</h1>
			<p class="mb-4 text-sm text-muted-foreground">
				Your account is not a member of any organization yet. Ask an organization owner to
				invite you, or create one.
			</p>
		</div>
	</div>
{:else if auth.ready && auth.signedIn && !auth.org}
	<div class="flex min-h-screen items-center justify-center bg-background p-6 text-foreground">
		<div class="w-full max-w-md border border-border bg-card p-6">
			<h1 class="mb-1 text-lg font-semibold">Select an organization</h1>
			<p class="mb-4 text-sm text-muted-foreground">
				Pick which organization to manage in this console.
			</p>
			<ul class="flex flex-col gap-2">
				{#each auth.memberships as m (m.org.id)}
					<li>
						<Button variant="secondary" onclick={() => void auth.setActiveOrg(m.org.id)}>
							<span class="block text-left">
								<span class="block font-medium">{m.org.name}</span>
								<span class="block text-xs text-muted-foreground">
									{roleLabel(m.role)}
								</span>
							</span>
						</Button>
					</li>
				{/each}
			</ul>
		</div>
	</div>
{:else}
	{@render pageShell(children)}
{/if}
