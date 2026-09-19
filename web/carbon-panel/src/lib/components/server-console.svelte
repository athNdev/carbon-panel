<script lang="ts">
	import { onDestroy, untrack } from 'svelte';
	import { rpcClient } from '$lib/api/rpc-client';
	import { create } from '@bufbuild/protobuf';
	import type { Server } from '$lib/proto/carbonpanel/v1/common_pb';
	import { ServerStatus } from '$lib/proto/carbonpanel/v1/common_pb';
	import type { LogEntry } from '$lib/proto/carbonpanel/v1/server_pb';
	import {
		GetServerLogsRequestSchema,
		ClearServerLogsRequestSchema,
		SendCommandRequestSchema,
		UploadToMCLogsRequestSchema
	} from '$lib/proto/carbonpanel/v1/server_pb';
	import { ResizablePaneGroup, ResizablePane, ResizableHandle } from '$lib/components/ui/resizable';
	import { toast } from 'svelte-sonner';
	import {
		Terminal,
		Send,
		Loader2,
		Download,
		Upload,
		Trash2,
		RefreshCw,
		Wifi,
		WifiOff
	} from '@lucide/svelte';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import AnsiToHtml from 'ansi-to-html';
	import { getStringForEnum } from '$lib/utils';
	import { wsClient } from '$lib/stores/websocket.svelte';
	import { CarbonTag } from '$lib/components/carbon';

	const ansiConverter = new AnsiToHtml({
		fg: '#f4f4f4',
		bg: '#161616',
		newline: false,
		escapeXML: true,
		stream: true
	});

	let { server, active = false }: { server: Server; active?: boolean } = $props();

	let logEntries = $state<LogEntry[]>([]);
	let command = $state('');
	let loading = $state(false);
	let autoScroll = $state(true);
	let scrollAreaRef = $state<HTMLDivElement | null>(null);
	let tailLines = $state(500);
	const MAX_LOG_ENTRIES = 5000;

	let wsConnectionState = $derived(wsClient.state.connectionState);
	let cleanupHandlers: (() => void)[] = [];
	let previousServerId = server.id;
	let previousContainerId = server.containerId;
	let previousStatus = server.status;

	onDestroy(() => {
		untrack(() => cleanupWebSocket());
	});

	$effect(() => {
		if (active) {
			untrack(() => setupWebSocket());
		} else {
			untrack(() => cleanupWebSocket());
		}
	});

	$effect(() => {
		const currentServerId = server.id;
		const currentContainerId = server.containerId;
		const currentStatus = server.status;

		if (currentServerId !== previousServerId) {
			const oldServerId = previousServerId;
			previousServerId = currentServerId;
			previousContainerId = currentContainerId;
			previousStatus = currentStatus;

			untrack(() => {
				wsClient.unsubscribe(oldServerId);
				logEntries = [];
				command = '';

				if (active) {
					wsClient.subscribe(currentServerId, tailLines);
					fetchLogs();
				}
			});
		} else if (
			(currentContainerId !== previousContainerId || currentStatus !== previousStatus) &&
			active
		) {
			const hadNoContainer = !previousContainerId && !!currentContainerId;
			const transitionedToActive =
				previousStatus === ServerStatus.CREATING &&
				(currentStatus === ServerStatus.STARTING || currentStatus === ServerStatus.RUNNING);

			previousContainerId = currentContainerId;
			previousStatus = currentStatus;

			if (hadNoContainer || transitionedToActive) {
				untrack(() => {
					wsClient.subscribe(currentServerId, tailLines);
					fetchLogs();
				});
			}
		}
	});

	function setupWebSocket() {
		cleanupWebSocket();
		wsClient.connect();

		const unsubLogs = wsClient.onLogs((serverId, logs) => {
			if (serverId === server.id) {
				logEntries = logs.length > MAX_LOG_ENTRIES ? logs.slice(-MAX_LOG_ENTRIES) : logs;
			}
		});

		const unsubLogEntry = wsClient.onLogEntry((serverId, logs) => {
			if (serverId === server.id && logs.length > 0) {
				const combined = [...logEntries, ...logs];
				logEntries =
					combined.length > MAX_LOG_ENTRIES ? combined.slice(-MAX_LOG_ENTRIES) : combined;
			}
		});

		const unsubCommandResult = wsClient.onCommandResult((result) => {
			if (result.serverId === server.id) {
				loading = false;
				if (result.success) {
					toast.success('Command executed');
				} else {
					toast.error(result.error || 'Failed to execute command');
				}
			}
		});

		cleanupHandlers = [unsubLogs, unsubLogEntry, unsubCommandResult];
		wsClient.subscribe(server.id, tailLines);
		fetchLogs();
	}

	function cleanupWebSocket() {
		wsClient.unsubscribe(server.id);
		cleanupHandlers.forEach((cleanup) => cleanup());
		cleanupHandlers = [];
	}

	$effect(() => {
		if (logEntries.length > 0 && autoScroll && scrollAreaRef) {
			queueMicrotask(() => {
				if (scrollAreaRef) {
					scrollAreaRef.scrollTop = scrollAreaRef.scrollHeight;
				}
			});
		}
	});

	function handleScroll() {
		if (!scrollAreaRef) return;
		const { scrollTop, scrollHeight, clientHeight } = scrollAreaRef;
		const atBottom = scrollHeight - scrollTop - clientHeight < 50;
		autoScroll = atBottom;
	}

	async function fetchLogs() {
		loading = true;
		try {
			const request = create(GetServerLogsRequestSchema, {
				id: server.id,
				tail: tailLines
			});
			const response = await rpcClient.server.getServerLogs(request);
			logEntries = response.logs;
		} catch (error) {
			toast.error(
				'Failed to fetch logs: ' + (error instanceof Error ? error.message : 'Unknown error')
			);
		} finally {
			loading = false;
		}
	}

	async function clearLogs() {
		try {
			const request = create(ClearServerLogsRequestSchema, { id: server.id });
			await rpcClient.server.clearServerLogs(request);
			logEntries = [];
			toast.success('Logs cleared');
		} catch (error) {
			toast.error(
				'Failed to clear logs: ' + (error instanceof Error ? error.message : 'Unknown error')
			);
		}
	}

	async function sendCommand() {
		if (!command.trim() || loading) return;
		// Defense-in-depth (MINE-129 follow-up): input + Send button are
		// disabled unless RUNNING/UNHEALTHY, but Enter-key / programmatic
		// calls bypass `disabled` — mirror the UI condition here.
		if (server.status !== ServerStatus.RUNNING && server.status !== ServerStatus.UNHEALTHY)
			return;

		const currentCommand = command.trim();
		command = '';
		loading = true;

		if (wsClient.isReady) {
			wsClient.sendCommand(server.id, currentCommand);
		} else {
			await sendCommandViaRpc(currentCommand);
		}
	}

	async function sendCommandViaRpc(cmdText: string) {
		try {
			const request = create(SendCommandRequestSchema, {
				id: server.id,
				command: cmdText
			});
			const response = await rpcClient.server.sendCommand(request);
			if (response.success) {
				toast.success('Command executed');
				await fetchLogs();
			} else {
				toast.error(response.error || 'Failed to execute command');
			}
		} catch (error) {
			toast.error(
				'Failed to execute command: ' + (error instanceof Error ? error.message : 'Unknown error')
			);
		} finally {
			loading = false;
		}
	}

	let uploading = $state(false);
	async function uploadToMCLogs() {
		if (uploading) return;
		uploading = true;
		try {
			const request = create(UploadToMCLogsRequestSchema, {
				id: server.id
			});
			const response = await rpcClient.server.uploadToMCLogs(request);
			if (response.url) {
				window.open(response.url, '_blank');
				toast.success('Logs uploaded to mclo.gs');
			} else {
				toast.error('Failed to upload logs');
			}
		} catch (error) {
			toast.error(
				'Failed to upload to mclo.gs: ' + (error instanceof Error ? error.message : 'Unknown error')
			);
		} finally {
			uploading = false;
		}
	}

	function downloadLogs() {
		const logText = logEntries.map((entry) => entry.message).join('\n');
		const blob = new Blob([logText], { type: 'text/plain' });
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		a.download = `${server.name}-logs-${new Date().toISOString()}.txt`;
		document.body.appendChild(a);
		a.click();
		document.body.removeChild(a);
		URL.revokeObjectURL(url);
		toast.success('Logs downloaded');
	}

	function handleTailChange() {
		if (wsClient.isReady) {
			wsClient.unsubscribe(server.id);
			wsClient.subscribe(server.id, tailLines);
		} else {
			fetchLogs();
		}
	}

	function getConnectionColor() {
		switch (wsConnectionState) {
			case 'authenticated':
				return 'text-[#6fdc8c]';
			case 'connected':
			case 'connecting':
				return 'text-[#f1c21b]';
			default:
				return 'text-[#8d8d8d]';
		}
	}
</script>

<!-- Carbon Code/Terminal Container (Requirement 5) -->
<ResizablePaneGroup
	direction="vertical"
	class="h-full max-h-[800px] min-h-[450px] w-full overflow-hidden rounded-none border border-[#393939] bg-[#161616] font-mono text-[#f4f4f4]"
>
	<ResizablePane defaultSize={78} minSize={30}>
		<div class="flex h-full flex-col">
			<!-- Terminal Header -->
			<div class="flex items-center justify-between border-b border-[#393939] bg-[#262626] px-4 py-2">
				<div class="flex items-center gap-2.5">
					<Terminal class="h-4 w-4 text-[#0f62fe]" />
					<span class="font-mono text-xs font-semibold text-[#f4f4f4] tracking-wider uppercase">
						Server Console
					</span>
					<CarbonTag
						type={server.status === ServerStatus.RUNNING ? 'green' : 'gray'}
						size="sm"
					>
						{getStringForEnum(ServerStatus, server.status)?.toUpperCase()}
					</CarbonTag>
					{#if wsConnectionState === 'authenticated'}
						<Wifi class="h-3.5 w-3.5 {getConnectionColor()}" />
					{:else}
						<WifiOff class="h-3.5 w-3.5 {getConnectionColor()}" />
					{/if}
				</div>

				<!-- Toolbar Icon Buttons -->
				<div class="flex items-center gap-1">
					<Tooltip.Root>
						<Tooltip.Trigger>
							<button
								type="button"
								onclick={fetchLogs}
								disabled={loading}
								class="h-7 w-7 flex items-center justify-center text-[#c6c6c6] hover:text-white hover:bg-[#353535] rounded-none transition-colors cursor-pointer"
							>
								{#if loading}
									<Loader2 class="h-3.5 w-3.5 animate-spin" />
								{:else}
									<RefreshCw class="h-3.5 w-3.5" />
								{/if}
							</button>
						</Tooltip.Trigger>
						<Tooltip.Content class="bg-[#262626] border border-[#393939] text-xs text-[#f4f4f4] rounded-none">
							Refresh logs
						</Tooltip.Content>
					</Tooltip.Root>

					<Tooltip.Root>
						<Tooltip.Trigger>
							<button
								type="button"
								onclick={uploadToMCLogs}
								disabled={uploading}
								class="h-7 w-7 flex items-center justify-center text-[#c6c6c6] hover:text-white hover:bg-[#353535] rounded-none transition-colors cursor-pointer"
							>
								{#if uploading}
									<Loader2 class="h-3.5 w-3.5 animate-spin" />
								{:else}
									<Upload class="h-3.5 w-3.5" />
								{/if}
							</button>
						</Tooltip.Trigger>
						<Tooltip.Content class="bg-[#262626] border border-[#393939] text-xs text-[#f4f4f4] rounded-none">
							Upload to mclo.gs
						</Tooltip.Content>
					</Tooltip.Root>

					<Tooltip.Root>
						<Tooltip.Trigger>
							<button
								type="button"
								onclick={downloadLogs}
								disabled={logEntries.length === 0}
								class="h-7 w-7 flex items-center justify-center text-[#c6c6c6] hover:text-white hover:bg-[#353535] rounded-none transition-colors cursor-pointer disabled:opacity-40"
							>
								<Download class="h-3.5 w-3.5" />
							</button>
						</Tooltip.Trigger>
						<Tooltip.Content class="bg-[#262626] border border-[#393939] text-xs text-[#f4f4f4] rounded-none">
							Download raw log
						</Tooltip.Content>
					</Tooltip.Root>

					<Tooltip.Root>
						<Tooltip.Trigger>
							<button
								type="button"
								onclick={clearLogs}
								disabled={logEntries.length === 0}
								class="h-7 w-7 flex items-center justify-center text-[#c6c6c6] hover:text-[#ff8389] hover:bg-[#353535] rounded-none transition-colors cursor-pointer disabled:opacity-40"
							>
								<Trash2 class="h-3.5 w-3.5" />
							</button>
						</Tooltip.Trigger>
						<Tooltip.Content class="bg-[#262626] border border-[#393939] text-xs text-[#f4f4f4] rounded-none">
							Clear buffer
						</Tooltip.Content>
					</Tooltip.Root>
				</div>
			</div>

			<!-- Terminal Output Stream -->
			<div
				class="custom-scrollbar min-h-0 flex-1 overflow-x-auto overflow-y-auto bg-[#161616] p-4 selection:bg-[#0f62fe] selection:text-white"
				bind:this={scrollAreaRef}
				onscroll={handleScroll}
			>
				<div class="font-mono text-xs leading-relaxed text-[#f4f4f4]">
					{#if logEntries.length === 0}
						<div class="py-12 text-center text-[#6f6f6f] font-mono text-xs">
							{#if server.status === ServerStatus.CREATING}
								Server container is being initialized and configured...
							{:else if [
								ServerStatus.RUNNING,
								ServerStatus.STARTING,
								ServerStatus.UNHEALTHY
							].includes(server.status)}
								No logs available. Try refreshing or waiting for container output.
							{:else}
								No logs available. Start server to view live container telemetry.
							{/if}
						</div>
					{:else}
						{#each logEntries as entry, i (i)}
							<div class="log-line break-all whitespace-pre-wrap" data-type={entry.level}>
								<!-- eslint-disable-next-line svelte/no-at-html-tags -->
								{@html ansiConverter.toHtml(entry.message)}
							</div>
						{/each}
					{/if}
				</div>
			</div>
		</div>
	</ResizablePane>

	<ResizableHandle class="bg-[#393939] hover:bg-[#525252] transition-colors" />

	<!-- Carbon Command Input Bar -->
	<div class="flex flex-col bg-[#262626]">
		<div class="flex shrink-0 items-center gap-2 p-3">
			<span class="font-mono text-sm text-[#0f62fe] font-bold select-none">$</span>
			<input
				type="text"
				placeholder={server.status === ServerStatus.CREATING
					? 'Server is creating...'
					: server.status === ServerStatus.STARTING
					? 'Server is starting...'
					: server.status === ServerStatus.RUNNING || server.status === ServerStatus.UNHEALTHY
					? 'Enter Minecraft server command (e.g. op, whitelist, stop)...'
					: 'Server must be active to execute commands'}
				bind:value={command}
				disabled={server.status !== ServerStatus.RUNNING && server.status !== ServerStatus.UNHEALTHY}
				onkeydown={(e) => e.key === 'Enter' && sendCommand()}
				class="flex-1 h-9 px-3 bg-[#161616] border border-[#525252] focus:border-[#0f62fe] focus:outline-none font-mono text-xs text-[#f4f4f4] placeholder-[#6f6f6f] rounded-none transition-all disabled:opacity-40"
			/>
			<button
				type="button"
				data-testid="console-send"
				onclick={sendCommand}
				disabled={(server.status !== ServerStatus.RUNNING && server.status !== ServerStatus.UNHEALTHY) || !command.trim() || loading}
				class="h-9 px-4 bg-[#0f62fe] hover:bg-[#0353e9] active:bg-[#002d9c] text-white text-xs font-mono flex items-center gap-1.5 rounded-none transition-colors cursor-pointer disabled:opacity-40 disabled:bg-[#393939]"
			>
				<Send class="h-3.5 w-3.5" />
				<span>Send</span>
			</button>
		</div>

		<!-- Status / Config Footer -->
		<div class="flex shrink-0 items-center justify-between border-t border-[#393939] bg-[#161616] px-3 py-1.5 text-xs text-[#8d8d8d] font-mono">
			<div class="flex items-center gap-4">
				<label class="flex items-center gap-2 cursor-pointer select-none">
					<input
						type="checkbox"
						bind:checked={autoScroll}
						class="h-3.5 w-3.5 rounded-none accent-[#0f62fe]"
					/>
					<span>Auto-scroll</span>
				</label>
				<div class="flex items-center gap-1.5">
					<span>Tail:</span>
					<select
						bind:value={tailLines}
						onchange={handleTailChange}
						class="h-6 px-1.5 bg-[#262626] border border-[#393939] text-xs font-mono text-[#f4f4f4] rounded-none focus:outline-none focus:border-[#0f62fe]"
					>
						<option value={100}>100</option>
						<option value={500}>500</option>
						<option value={1000}>1000</option>
						<option value={2000}>2000</option>
					</select>
				</div>
			</div>
			<div class="font-mono text-[11px] text-[#8d8d8d]">
				{logEntries.length} lines in buffer
			</div>
		</div>
	</div>
</ResizablePaneGroup>

<style>
	.custom-scrollbar {
		scrollbar-width: thin;
		scrollbar-color: #393939 transparent;
	}

	.custom-scrollbar::-webkit-scrollbar {
		width: 10px;
	}

	.custom-scrollbar::-webkit-scrollbar-track {
		background: #161616;
	}

	.custom-scrollbar::-webkit-scrollbar-thumb {
		background-color: #393939;
		border-radius: 0px;
	}

	.custom-scrollbar::-webkit-scrollbar-thumb:hover {
		background-color: #525252;
	}

	.log-line {
		padding: 1px 0;
		line-height: 1.5;
	}

	.log-line:hover {
		background-color: #262626;
	}

	.log-line[data-type='command'] {
		color: #78a9ff;
		font-weight: 500;
	}

	.log-line[data-type='command']::before {
		content: '$ ';
		color: #0f62fe;
		font-weight: bold;
	}

	.log-line[data-type='command_output'] {
		opacity: 0.9;
		padding-left: 0.75rem;
	}
</style>
