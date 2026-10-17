<script lang="ts">
	import Button from '$lib/components/Button.svelte';
	import Tile from '$lib/components/Tile.svelte';
	import { auth } from '$lib/auth/clerk.svelte';

	function retry() {
		auth.init();
	}
</script>

<div class="flex min-h-screen items-center justify-center bg-background p-6 text-foreground">
	<div class="w-full max-w-lg">
		<Tile title="Clerk is not configured">
			<p class="mb-3 text-sm text-foreground">
				This console signs in through Clerk, but the publishable key has not been supplied. Set
				the <code class="font-mono text-xs">PUBLIC_CLERK_PUBLISHABLE_KEY</code> environment
				variable before building or running the console, then reload this page.
			</p>
			<div class="flex gap-2">
				<Button onclick={retry}>Check again</Button>
				<Button variant="ghost" href="/sign-in">Try signing in</Button>
			</div>
		</Tile>
	</div>
</div>