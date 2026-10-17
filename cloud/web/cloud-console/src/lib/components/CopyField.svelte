<script lang="ts">
	import { cls } from '$lib/utils';

	let {
		value = '',
		label = 'Value',
		secret = false,
		mono = true,
		placeholder = '—'
	}: {
		value?: string;
		label?: string;
		secret?: boolean;
		mono?: boolean;
		placeholder?: string;
	} = $props();

	let copied = $state(false);

	async function copy() {
		try {
			await navigator.clipboard.writeText(value);
			copied = true;
			setTimeout(() => (copied = false), 2000);
		} catch {
			// Clipboard unavailable; select the text instead.
			copied = false;
		}
	}
</script>

<div class="flex items-stretch gap-0 border border-border bg-card">
	<code
		class={cls(
			'flex-1 truncate px-3 py-2 text-xs text-foreground',
			mono ? 'font-mono' : 'font-sans'
		)}
		aria-label={label}
		title={value || placeholder}
	>
		{value || placeholder}
	</code>
	<button
		class="border-l border-border px-3 text-xs font-medium text-muted-foreground hover:bg-accent hover:text-foreground focus-ring"
		onclick={copy}
		aria-label={secret ? `Copy ${label} (shown only once)` : `Copy ${label}`}
		disabled={!value}
	>
		{copied ? 'Copied' : 'Copy'}
	</button>
</div>