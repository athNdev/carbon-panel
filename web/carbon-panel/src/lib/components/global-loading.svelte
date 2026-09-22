<script lang="ts">
	import { loadingStore } from '$lib/stores/loading.svelte';
	import { fade } from 'svelte/transition';

	let isLoading = $derived($loadingStore.global);
</script>

{#if isLoading}
	<div
		class="carbon-progress-track fixed top-0 right-0 left-0 z-[60] h-0.5"
		transition:fade={{ duration: 150 }}
		role="status"
		aria-label="Loading"
	>
		<div class="carbon-progress-bar"></div>
	</div>
{/if}
