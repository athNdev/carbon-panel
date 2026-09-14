<script lang="ts">
	import '../app.css';
	import CarbonShell from '$lib/components/carbon/CarbonShell.svelte';
	import { ModeWatcher } from 'mode-watcher';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { resolve as resolvePath } from '$app/paths';
	import {
		SidebarProvider,
		SidebarInset,
		Sidebar,
		SidebarContent,
		SidebarGroup,
		SidebarGroupLabel,
		SidebarGroupContent,
		SidebarMenu,
		SidebarMenuItem,
		SidebarMenuButton,
		SidebarHeader,
		SidebarFooter,
		SidebarTrigger
	} from '$lib/components/ui/sidebar';
	import { Separator } from '$lib/components/ui/separator';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import { Avatar, AvatarFallback } from '$lib/components/ui/avatar';
	import {
		DropdownMenu,
		DropdownMenuContent,
		DropdownMenuItem,
		DropdownMenuLabel,
		DropdownMenuSeparator,
		DropdownMenuTrigger
	} from '$lib/components/ui/dropdown-menu';
	import { get } from 'svelte/store';
	import { serversStore, runningServers, activitySortedServers } from '$lib/stores/servers';
	import { authStore, currentUser, canAccessSettings, authEnabled } from '$lib/stores/auth';
	import { onMount } from 'svelte';
	import { Toaster } from '$lib/components/ui/sonner';
	import GlobalLoading from '$lib/components/global-loading.svelte';

	import {
		Server,
		Home,
		Settings,
		Package,
		User as UserIcon,
		LogOut,
		LogIn,
		FileText,
		Sun,
		Moon,
		Puzzle
	} from '@lucide/svelte';
	import { toggleMode, mode } from 'mode-watcher';
	import { ServerStatus, type User } from '$lib/proto/carbonpanel/v1/common_pb';

	let { children } = $props();

	let servers = $derived($activitySortedServers);
	let runningCount = $derived($runningServers.length);
	let user = $derived($currentUser);
	let showSettingsNav = $derived($canAccessSettings);
	let loading = $state(true);
	let isAuthEnabled = $derived($authEnabled);

	function getUserInitials(user: User) {
		if (!user) return '';
		return user.username.slice(0, 2).toUpperCase();
	}

	function getDisplayRole(user: User): string {
		if (!user?.roles?.length) return 'No roles';
		return user.roles[0];
	}

	async function handleLogout() {
		await authStore.logout();
	}

	let statusPollingInterval: ReturnType<typeof setInterval> | null = null;

	onMount(() => {
		return new Promise((resolve, reject) => {
			authStore
				.checkAuthStatus()
				.then(async (authStatus) => {
					loading = false;
					if (authStatus.enabled) {
						if (authStatus.firstUserSetup) {
							goto(resolvePath('/login'));
							return false;
						}
						const isValid = await authStore.validateSession();
						if (!isValid) {
							// If anonymous access is enabled, allow browsing without login
							const state = get(authStore);
							if (!state.anonymousAccessEnabled) {
								goto(resolvePath('/login'));
								return false;
							}
						}
					}
					return true;
				})
				.then((shouldFetch) => {
					// Only fetch servers if auth succeeded (not redirecting to login)
					if (shouldFetch && page.url.pathname !== '/login') {
						serversStore.fetchServers(false).catch((err) => {
							console.error('Failed to fetch initial servers:', err);
						});

						if (!statusPollingInterval) {
							statusPollingInterval = setInterval(() => {
								if (page.url.pathname !== '/login') {
									serversStore.fetchServers(true);
								}
							}, 10000);
						}
					}

					// Clean up on unmount
					resolve(() => {
						if (statusPollingInterval) {
							clearInterval(statusPollingInterval);
							statusPollingInterval = null;
						}
					});
				})
				.catch((err) => {
					console.debug(`Carbon Panel caught a polling error: ${err}`);
					reject(err);
				});
		});
	});
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
		<div class="h-10 w-10 border-4 border-[#0f62fe] border-t-transparent animate-spin"></div>
	</div>
{:else}
	<CarbonShell>
		{@render children?.()}
	</CarbonShell>
{/if}
