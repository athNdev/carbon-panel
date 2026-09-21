<script lang="ts">
	import type { Snippet } from 'svelte';
	import { cls, type TagTone } from '$lib/utils';

	let {
		tone = 'neutral',
		children
	}: {
		tone?: TagTone;
		children: Snippet;
	} = $props();

	const tones: Record<TagTone, string> = {
		neutral: 'border-border text-muted-foreground',
		blue: 'border-primary/60 text-[#a6c8ff]',
		green: 'border-success/60 text-[#8cd3a1]',
		red: 'border-destructive/60 text-[#ffb3b8]',
		yellow: 'border-warning/60 text-[#ffd97a]',
		purple: 'border-[#be95ff]/60 text-[#d4bbff]'
	};

	const dot: Record<TagTone, string> = {
		neutral: 'bg-muted-foreground',
		blue: 'bg-primary',
		green: 'bg-success',
		red: 'bg-destructive',
		yellow: 'bg-warning',
		purple: 'bg-[#be95ff]'
	};
</script>

<span
	class={cls(
		'inline-flex items-center gap-1.5 border px-2 py-0.5 text-xs font-medium',
		tones[tone]
	)}
>
	<span class={cls('h-1.5 w-1.5 shrink-0', dot[tone])} aria-hidden="true"></span>
	{@render children()}
</span>