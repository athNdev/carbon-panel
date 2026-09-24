<script lang="ts">
	import { onMount } from 'svelte';
	import { auth } from '$lib/auth/clerk.svelte';

	let el: HTMLDivElement;

	onMount(() => {
		auth.init();
		auth.mountSignUp(el);
	});
</script>

<div class="flex min-h-screen flex-col items-center justify-center bg-background p-6 text-foreground">
	<div class="w-full max-w-md">
		<div class="mb-6 flex items-center justify-center gap-2">
			<span class="inline-block h-3.5 w-3.5 bg-primary" aria-hidden="true"></span>
			<h1 class="text-xl font-semibold tracking-tight">Carbon Cloud</h1>
		</div>
		<div class="flex justify-center" bind:this={el}></div>
	</div>
</div>
