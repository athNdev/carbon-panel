<script lang="ts">
	import '../app.css';
	import CarbonShell from '$lib/components/carbon/CarbonShell.svelte';
	import { ModeWatcher } from 'mode-watcher';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { resolve as resolvePath } from '$app/paths';
	import { get } from 'svelte/store';
	import { serversStore } from '$lib/stores/servers';
	import { authStore } from '$lib/stores/auth';
	import { onMount, onDestroy } from 'svelte';
	import { Toaster } from '$lib/components/ui/sonner';
	import GlobalLoading from '$lib/components/global-loading.svelte';

	let { children } = $props();

	let loading = $state(true);

	let statusPollingInterval: ReturnType<typeof setInterval> | null = null;

	function stopStatusPolling() {
		if (statusPollingInterval) {
			clearInterval(statusPollingInterval);
			statusPollingInterval = null;
		}
	}

	async function initialize() {
		try {
			const authStatus = await authStore.checkAuthStatus();
			loading = false;

			let shouldFetch = true;
			if (authStatus.enabled) {
				if (authStatus.firstUserSetup) {
					goto(resolvePath('/login'));
					shouldFetch = false;
				} else {
					const isValid = await authStore.validateSession();
					if (!isValid) {
						// If anonymous access is enabled, allow browsing without login
						const state = get(authStore);
						if (!state.anonymousAccessEnabled) {
							goto(resolvePath('/login'));
							shouldFetch = false;
						}
					}
				}
			}

			if (!shouldFetch || page.url.pathname === '/login') {
				return;
			}

			serversStore.fetchServers(false).catch((err) => {
				console.error('Failed to fetch initial servers:', err);
			});

			// Keep the sidebar status fresh. Each poll swallows its own error so a
			// transient backend hiccup cannot raise an unhandled rejection every 10s.
			if (!statusPollingInterval) {
				statusPollingInterval = setInterval(() => {
					if (page.url.pathname !== '/login') {
						serversStore.fetchServers(true).catch((err) => {
							console.debug('Status poll failed:', err);
						});
					}
				}, 10000);
			}
		} catch (err) {
			// Never reject the mount lifecycle: that would skip cleanup and leak
			// the polling interval.
			console.debug(`Carbon Panel caught a polling error: ${err}`);
			loading = false;
		}
	}

	onMount(() => {
		initialize();
	});

	onDestroy(stopStatusPolling);
</script>

<svelte:head>
	<title>Carbon Panel - Minecraft Server Management</title>
</svelte:head>

<ModeWatcher />
<Toaster position="bottom-center" expand={true} richColors />
<GlobalLoading />

{#if page.url.pathname === '/login'}
	{@render children?.()}
{:else if loading}
	<div class="flex min-h-screen items-center justify-center bg-[#161616]">
		<div class="h-10 w-10 animate-spin border-4 border-[#0f62fe] border-t-transparent"></div>
	</div>
{:else}
	<CarbonShell>
		{@render children?.()}
	</CarbonShell>
{/if}
