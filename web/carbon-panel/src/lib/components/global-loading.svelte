<!--
SPDX-License-Identifier: MIT AND MPL-2.0
This file includes DiscoPanel material under the MIT License,
Copyright (c) 2025 Nicholas W. Heyer. The MIT license text is in LICENSE
and still applies to that inherited material.
Maintainer changes are licensed under the Mozilla Public License, v. 2.0.
If a copy of the MPL was not distributed with this file, You can obtain
one at https://mozilla.org/MPL/2.0/.
-->

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
