<!-- SPDX-License-Identifier: MPL-2.0 -->
<script lang="ts">
	import { page } from '$app/stores';
	import { resolve } from '$app/paths';
	import { CarbonButton } from '$lib/components/carbon';
	import { RefreshCw, Home } from '@lucide/svelte';
</script>

<div class="flex min-h-[60vh] flex-col items-center justify-center gap-4 p-8 text-center">
	<h1 class="text-2xl font-semibold text-[#f4f4f4]">
		{$page.status === 404 ? 'Page not found' : 'Something went wrong'}
	</h1>
	<p class="max-w-md text-sm text-[#a8a8a8]">
		{$page.error?.message ?? 'An unexpected error occurred while loading this page.'}
	</p>
	<div class="flex gap-2">
		<CarbonButton kind="secondary" size="sm" onclick={() => location.reload()}>
			<RefreshCw class="h-4 w-4" />
			<span>Retry</span>
		</CarbonButton>
		<CarbonButton kind="primary" size="sm" href={resolve('/')}>
			<Home class="h-4 w-4" />
			<span>Home</span>
		</CarbonButton>
	</div>
</div>