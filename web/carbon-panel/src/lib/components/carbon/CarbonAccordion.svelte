<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		class?: string;
		children?: Snippet;
	}

	let { class: className = '', children }: Props = $props();
</script>

<ul class="w-full border-t border-[#393939] list-none p-0 m-0 rounded-none {className}">
	{@render children?.()}
</ul>
