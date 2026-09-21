<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		class?: string;
		children?: Snippet;
	}

	let { class: className = '', children }: Props = $props();
</script>

<ul class="m-0 w-full list-none rounded-none border-t border-[#393939] p-0 {className}">
	{@render children?.()}
</ul>
