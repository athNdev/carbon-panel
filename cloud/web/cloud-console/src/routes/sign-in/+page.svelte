<script lang="ts">
	import { onMount } from 'svelte';
	import { auth } from '$lib/auth/clerk.svelte';

	let el: HTMLDivElement;

	onMount(() => {
		auth.init();
		auth.mountSignIn(el);
	});
</script>

<div class="flex min-h-screen items-center justify-center bg-background p-6 text-foreground">
	<div class="w-full max-w-md">
		<div class="mb-6 flex items-center gap-2">
			<span class="inline-block h-3 w-3 bg-primary" aria-hidden="true"></span>
			<h1 class="text-lg font-semibold">Carbon Cloud</h1>
		</div>
		<div class="border border-border bg-card p-6" bind:this={el}></div>
	</div>
</div>