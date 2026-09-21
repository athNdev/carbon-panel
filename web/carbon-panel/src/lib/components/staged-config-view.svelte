<script lang="ts">
	// StagedConfigView — stage view for scheduled/staged config rollout.
	// Mount wherever instance config is edited, e.g. inside the server config
	// section (NOT wired by default; see mount note below):
	//
	//   import StagedConfigView from '$lib/components/staged-config-view.svelte';
	//   <StagedConfigView serverId={server.id} />
	//
	// Backend: /api/v1/staged-config/{serverId}/staged (see
	// internal/rpc/handlers/staged_config.go). "on_restart" changes apply via
	// scheduler.ApplyOnRestartStagedConfigs in the restart path; "scheduled"
	// changes apply on the scheduler cron tick.
	import {
		CarbonButton,
		CarbonTile,
		CarbonTag,
		CarbonTextInput,
		CarbonSelect,
		CarbonInlineLoading
	} from '$lib/components/carbon';
	import { Clock, Rocket, Trash2, Plus, Layers } from '@lucide/svelte';
	import { toast } from 'svelte-sonner';
	import { apiFetch } from '$lib/api/fetch';

	interface StagedChange {
		id: string;
		server_id: string;
		payload: string;
		apply_mode: 'on_restart' | 'scheduled';
		cron_expr: string;
		status: string;
		created_at: string;
		applied_at?: string;
	}

	let { serverId }: { serverId: string } = $props();

	let changes = $state<StagedChange[]>([]);
	let loading = $state(false);
	let staging = $state(false);
	let applyingId = $state<string | null>(null);

	// Stage form state: raw JSON diff vs live config + rollout mode.
	let stageJson = $state('{\n  "motd": "Scheduled event server"\n}');
	let stageMode = $state<'on_restart' | 'scheduled'>('on_restart');
	let stageCron = $state('0 3 * * *');

	function parsedPayload(payload: string): Record<string, unknown> {
		try {
			return JSON.parse(payload || '{}');
		} catch {
			return { '(invalid payload)': payload };
		}
	}

	async function loadChanges() {
		if (!serverId) return;
		loading = true;
		try {
			const res = await apiFetch(`/api/v1/staged-config/${serverId}/staged?status=staged`);
			if (!res.ok) throw new Error(`HTTP ${res.status}`);
			const data = await res.json();
			changes = data.changes || [];
		} catch (err) {
			console.error('Failed to load staged changes:', err);
		} finally {
			loading = false;
		}
	}

	async function stageChange() {
		let parsed: Record<string, unknown>;
		try {
			parsed = JSON.parse(stageJson);
		} catch {
			toast.error('Stage payload must be valid JSON');
			return;
		}
		if (Object.keys(parsed).length === 0) {
			toast.error('Stage payload is empty — nothing to record');
			return;
		}
		staging = true;
		try {
			const res = await apiFetch(`/api/v1/staged-config/${serverId}/staged`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ changes: parsed, apply_mode: stageMode, cron_expr: stageCron })
			});
			if (!res.ok) {
				const txt = await res.text();
				throw new Error(txt || `HTTP ${res.status}`);
			}
			toast.success(
				stageMode === 'scheduled'
					? `Change staged for scheduled rollout (${stageCron})`
					: 'Change staged — applies on next restart'
			);
			await loadChanges();
		} catch (err: any) {
			console.error('Failed to stage change:', err);
			toast.error(err.message || 'Failed to stage change');
		} finally {
			staging = false;
		}
	}

	async function applyNow(id: string) {
		applyingId = id;
		try {
			const res = await apiFetch(`/api/v1/staged-config/${serverId}/staged/${id}/apply`, {
				method: 'POST'
			});
			if (!res.ok) {
				const txt = await res.text();
				throw new Error(txt || `HTTP ${res.status}`);
			}
			toast.success('Staged change applied to live config');
			await loadChanges();
		} catch (err: any) {
			console.error('Failed to apply staged change:', err);
			toast.error(err.message || 'Failed to apply staged change');
		} finally {
			applyingId = null;
		}
	}

	async function discard(id: string) {
		if (!confirm('Discard this staged change?')) return;
		try {
			const res = await apiFetch(`/api/v1/staged-config/${serverId}/staged/${id}`, {
				method: 'DELETE'
			});
			if (!res.ok) throw new Error(`HTTP ${res.status}`);
			toast.success('Staged change discarded');
			await loadChanges();
		} catch (err: any) {
			console.error('Failed to discard staged change:', err);
			toast.error(err.message || 'Failed to discard staged change');
		}
	}

	$effect(() => {
		if (serverId) loadChanges();
	});
</script>

<CarbonTile class="space-y-4 rounded-none border-[#393939]">
	<div class="flex items-center justify-between border-b border-[#393939] pb-3">
		<div>
			<h2 class="flex items-center gap-2 text-base font-semibold text-white">
				<Layers class="h-4 w-4 text-[#0f62fe]" />
				Staged Config Rollout
			</h2>
			<p class="text-xs text-[#a8a8a8]">
				Record config changes now, apply on next restart or a cron schedule.
			</p>
		</div>
		<CarbonTag type="gray" size="sm">{changes.length} staged</CarbonTag>
	</div>

	<!-- Stage form -->
	<div class="space-y-3 rounded-none border border-[#393939] bg-[#161616] p-3">
		<div class="space-y-1.5">
			<label class="text-xs font-normal tracking-[0.32px] text-[#c6c6c6]"
				>Config diff (JSON, vs live config)</label
			>
			<textarea
				bind:value={stageJson}
				rows={4}
				class="w-full rounded-none border-b border-[#8d8d8d] bg-[#262626] p-3 font-mono text-xs text-[#f4f4f4] focus:border-[#0f62fe] focus:outline-none"
				placeholder={'{"motd": "Event night!"}'}
			></textarea>
		</div>
		<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
			<CarbonSelect label="Apply mode" bind:value={stageMode}>
				<option value="on_restart">On next restart</option>
				<option value="scheduled">Scheduled (cron)</option>
			</CarbonSelect>
			{#if stageMode === 'scheduled'}
				<CarbonTextInput
					label="Cron expression"
					bind:value={stageCron}
					helperText="5-field cron, e.g. 0 3 * * *"
				/>
			{/if}
		</div>
		<CarbonButton size="sm" class="rounded-none" onclick={stageChange} disabled={staging}>
			<Plus class="mr-1.5 h-3.5 w-3.5" />
			{staging ? 'Staging...' : 'Stage change'}
		</CarbonButton>
	</div>

	<!-- Staged list -->
	{#if loading}
		<div class="flex items-center justify-center py-8">
			<CarbonInlineLoading description="Loading staged changes..." />
		</div>
	{:else if changes.length === 0}
		<p class="py-4 text-center text-xs text-[#8d8d8d]">
			No staged changes. Stage a diff above to roll it out later.
		</p>
	{:else}
		<div class="space-y-2">
			{#each changes as change (change.id)}
				<div class="space-y-2 rounded-none border border-[#393939] bg-[#161616] p-3">
					<div class="flex flex-wrap items-center justify-between gap-2">
						<div class="flex items-center gap-2">
							{#if change.apply_mode === 'scheduled'}
								<CarbonTag type="blue" size="sm">
									<Clock class="mr-1 h-3 w-3" />
									{change.cron_expr}
								</CarbonTag>
							{:else}
								<CarbonTag type="cyan" size="sm">on restart</CarbonTag>
							{/if}
							<span class="font-mono text-[11px] text-[#8d8d8d]">
								{new Date(change.created_at).toLocaleString()}
							</span>
						</div>
						<div class="flex items-center gap-1.5">
							<CarbonButton
								size="sm"
								kind="primary"
								class="rounded-none text-xs"
								onclick={() => applyNow(change.id)}
								disabled={applyingId === change.id}
							>
								<Rocket class="mr-1 h-3 w-3" />
								Apply now
							</CarbonButton>
							<CarbonButton
								size="sm"
								kind="ghost"
								iconOnly
								class="rounded-none text-[#da1e28] hover:bg-[#da1e28]/20"
								onclick={() => discard(change.id)}
								title="Discard"
							>
								<Trash2 class="h-3.5 w-3.5" />
							</CarbonButton>
						</div>
					</div>
					<pre
						class="overflow-x-auto rounded-none border border-[#393939] bg-[#262626] p-2 font-mono text-[11px] text-[#c6c6c6]">{JSON.stringify(
							parsedPayload(change.payload),
							null,
							2
						)}</pre>
				</div>
			{/each}
		</div>
	{/if}
</CarbonTile>
