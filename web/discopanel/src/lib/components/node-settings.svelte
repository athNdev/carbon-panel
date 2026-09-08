<script lang="ts">
	import { onMount } from 'svelte';
	import { rpcClient } from '$lib/api/rpc-client';
	import { NodeStatus, type Node } from '$lib/proto/discopanel/v1/node_pb';
	import {
		Card,
		CardContent,
		CardDescription,
		CardHeader,
		CardTitle
	} from '$lib/components/ui/card';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Button } from '$lib/components/ui/button';
	import { Switch } from '$lib/components/ui/switch';
	import { Badge } from '$lib/components/ui/badge';
	import { toast } from 'svelte-sonner';
	import {
		Server,
		Plus,
		Trash2,
		Loader2,
		Activity,
		CheckCircle2,
		XCircle,
		AlertCircle,
		HardDrive,
		Cpu,
		RefreshCw,
		Layers,
		ShieldCheck,
		Edit,
		Radio
	} from '@lucide/svelte';
	import {
		Dialog,
		DialogContent,
		DialogDescription,
		DialogFooter,
		DialogHeader,
		DialogTitle
	} from '$lib/components/ui/dialog';
	import { Textarea } from '$lib/components/ui/textarea';

	let loading = $state(true);
	let nodes = $state<Node[]>([]);
	let pingingNodeId = $state<string | null>(null);
	let pingLatencies = $state<Record<string, { latency: number; status: NodeStatus; message?: string }>>({});

	// Dialog state
	let showAddDialog = $state(false);
	let showEditDialog = $state(false);
	let isSaving = $state(false);

	let newNode = $state({
		name: '',
		host: 'tcp://',
		advertisedIp: '',
		tlsEnabled: false,
		tlsSkipVerify: false,
		tlsCaCert: '',
		tlsCert: '',
		tlsKey: '',
		maxMemoryMb: 0,
		maxServers: 0,
		enabled: true
	});

	let editingNode = $state<{
		id: string;
		name: string;
		host: string;
		advertisedIp: string;
		tlsEnabled: boolean;
		tlsSkipVerify: boolean;
		tlsCaCert: string;
		tlsCert: string;
		tlsKey: string;
		maxMemoryMb: number;
		maxServers: number;
		enabled: boolean;
	} | null>(null);

	onMount(() => {
		loadNodes();
	});

	async function loadNodes() {
		loading = true;
		try {
			const res = await rpcClient.node.listNodes({});
			nodes = res.nodes;
		} catch (error) {
			console.error('Failed to load nodes:', error);
			toast.error('Failed to load Docker nodes');
		} finally {
			loading = false;
		}
	}

	async function handlePingNode(nodeId: string) {
		pingingNodeId = nodeId;
		try {
			const res = await rpcClient.node.pingNode({ id: nodeId });
			pingLatencies[nodeId] = {
				latency: Number(res.latencyMs),
				status: res.status,
				message: res.message
			};
			if (res.success) {
				toast.success(`Node responded in ${res.latencyMs}ms`);
			} else {
				toast.error(`Node ping failed: ${res.message}`);
			}
			await loadNodes();
		} catch (error: unknown) {
			toast.error(`Ping failed: ${error instanceof Error ? error.message : 'Unknown error'}`);
		} finally {
			pingingNodeId = null;
		}
	}

	async function handleCreateNode() {
		if (!newNode.name.trim()) {
			toast.error('Node name is required');
			return;
		}
		if (!newNode.host.trim()) {
			toast.error('Docker host URL is required');
			return;
		}

		isSaving = true;
		try {
			await rpcClient.node.createNode({
				name: newNode.name.trim(),
				host: newNode.host.trim(),
				advertisedIp: newNode.advertisedIp.trim(),
				tlsEnabled: newNode.tlsEnabled,
				tlsSkipVerify: newNode.tlsSkipVerify,
				tlsCaCert: newNode.tlsCaCert,
				tlsCert: newNode.tlsCert,
				tlsKey: newNode.tlsKey,
				maxMemoryMb: BigInt(newNode.maxMemoryMb || 0),
				maxServers: newNode.maxServers || 0,
				enabled: newNode.enabled
			});

			toast.success(`Node "${newNode.name}" added successfully`);
			showAddDialog = false;
			// Reset form
			newNode = {
				name: '',
				host: 'tcp://',
				advertisedIp: '',
				tlsEnabled: false,
				tlsSkipVerify: false,
				tlsCaCert: '',
				tlsCert: '',
				tlsKey: '',
				maxMemoryMb: 0,
				maxServers: 0,
				enabled: true
			};
			await loadNodes();
		} catch (error: unknown) {
			toast.error(`Failed to add node: ${error instanceof Error ? error.message : 'Unknown error'}`);
		} finally {
			isSaving = false;
		}
	}

	function openEditNode(node: Node) {
		editingNode = {
			id: node.id,
			name: node.name,
			host: node.host,
			advertisedIp: node.advertisedIp,
			tlsEnabled: node.tlsEnabled,
			tlsSkipVerify: node.tlsSkipVerify,
			tlsCaCert: node.tlsCaCert || '',
			tlsCert: node.tlsCert || '',
			tlsKey: node.tlsKey || '',
			maxMemoryMb: Number(node.maxMemoryMb),
			maxServers: node.maxServers,
			enabled: node.enabled
		};
		showEditDialog = true;
	}

	async function handleUpdateNode() {
		if (!editingNode) return;
		if (!editingNode.name.trim()) {
			toast.error('Node name is required');
			return;
		}
		if (!editingNode.host.trim()) {
			toast.error('Docker host URL is required');
			return;
		}

		isSaving = true;
		try {
			await rpcClient.node.updateNode({
				id: editingNode.id,
				name: editingNode.name.trim(),
				host: editingNode.host.trim(),
				advertisedIp: editingNode.advertisedIp.trim(),
				tlsEnabled: editingNode.tlsEnabled,
				tlsSkipVerify: editingNode.tlsSkipVerify,
				tlsCaCert: editingNode.tlsCaCert,
				tlsCert: editingNode.tlsCert,
				tlsKey: editingNode.tlsKey,
				maxMemoryMb: BigInt(editingNode.maxMemoryMb || 0),
				maxServers: editingNode.maxServers || 0,
				enabled: editingNode.enabled
			});

			toast.success(`Node "${editingNode.name}" updated successfully`);
			showEditDialog = false;
			editingNode = null;
			await loadNodes();
		} catch (error: unknown) {
			toast.error(`Failed to update node: ${error instanceof Error ? error.message : 'Unknown error'}`);
		} finally {
			isSaving = false;
		}
	}

	async function handleDeleteNode(node: Node) {
		if (node.isLocal) {
			toast.error('Local Docker daemon cannot be deleted');
			return;
		}

		if (node.serverCount > 0) {
			toast.error(`Cannot delete node: ${node.serverCount} servers are assigned to it`);
			return;
		}

		if (!confirm(`Are you sure you want to delete Docker node "${node.name}"?`)) {
			return;
		}

		try {
			await rpcClient.node.deleteNode({ id: node.id });
			toast.success(`Node "${node.name}" deleted`);
			await loadNodes();
		} catch (error: unknown) {
			toast.error(`Failed to delete node: ${error instanceof Error ? error.message : 'Unknown error'}`);
		}
	}

	function getNodeStatusBadge(status: NodeStatus) {
		switch (status) {
			case NodeStatus.ONLINE:
				return { label: 'Online', class: 'bg-green-500/10 text-green-500 border-green-500/20', icon: CheckCircle2 };
			case NodeStatus.OFFLINE:
				return { label: 'Offline', class: 'bg-muted text-muted-foreground border-border', icon: XCircle };
			case NodeStatus.ERROR:
				return { label: 'Error', class: 'bg-destructive/10 text-destructive border-destructive/20', icon: AlertCircle };
			default:
				return { label: 'Unknown', class: 'bg-muted text-muted-foreground border-border', icon: AlertCircle };
		}
	}

	function formatMemory(mb: number | bigint): string {
		const val = Number(mb);
		if (val <= 0) return 'Unlimited';
		if (val >= 1024) {
			return `${(val / 1024).toFixed(1)} GB`;
		}
		return `${val} MB`;
	}
</script>

<div class="space-y-6">
	<!-- Header Card -->
	<Card>
		<CardHeader>
			<div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
				<div>
					<div class="flex items-center gap-2">
						<Layers class="h-5 w-5 text-primary" />
						<CardTitle>Docker Daemons & Nodes</CardTitle>
					</div>
					<CardDescription class="mt-1">
						Configure multiple remote Docker daemons for distributed container execution and load balancing.
					</CardDescription>
				</div>
				<div class="flex items-center gap-2">
					<Button
						variant="outline"
						size="sm"
						onclick={loadNodes}
						disabled={loading}
					>
						<RefreshCw class="mr-2 h-4 w-4 {loading ? 'animate-spin' : ''}" />
						Refresh
					</Button>
					<Button
						size="sm"
						onclick={() => (showAddDialog = true)}
					>
						<Plus class="mr-2 h-4 w-4" />
						Add Docker Node
					</Button>
				</div>
			</div>
		</CardHeader>
	</Card>

	<!-- Nodes List -->
	{#if loading}
		<Card>
			<CardContent class="py-16">
				<div class="flex items-center justify-center">
					<div class="space-y-3 text-center">
						<Loader2 class="mx-auto h-8 w-8 animate-spin text-primary" />
						<div class="font-medium text-muted-foreground">Loading Docker nodes...</div>
					</div>
				</div>
			</CardContent>
		</Card>
	{:else if nodes.length === 0}
		<Card>
			<CardContent class="py-16 text-center">
				<div class="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-muted">
					<Server class="h-6 w-6 text-muted-foreground" />
				</div>
				<h3 class="text-lg font-semibold">No Docker nodes found</h3>
				<p class="mt-1 text-sm text-muted-foreground">
					Add a remote Docker host to scale server instances across multiple physical machines.
				</p>
				<Button class="mt-4" onclick={() => (showAddDialog = true)}>
					<Plus class="mr-2 h-4 w-4" />
					Add Docker Node
				</Button>
			</CardContent>
		</Card>
	{:else}
		<div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
			{#each nodes as node (node.id)}
				{@const statusInfo = getNodeStatusBadge(node.status)}
				{@const StatusIcon = statusInfo.icon}
				{@const pingInfo = pingLatencies[node.id]}

				<Card class="relative flex flex-col justify-between overflow-hidden transition-all duration-200 hover:shadow-md border-border/80">
					<div>
						<!-- Card Top -->
						<CardHeader class="pb-3">
							<div class="flex items-start justify-between gap-2">
								<div class="min-w-0 flex-1 space-y-1">
									<div class="flex items-center gap-2">
										<h4 class="truncate font-semibold text-foreground">{node.name}</h4>
										{#if node.isLocal}
											<Badge variant="secondary" class="text-xs">Local</Badge>
										{/if}
										{#if !node.enabled}
											<Badge variant="outline" class="text-xs text-muted-foreground">Disabled</Badge>
										{/if}
									</div>
									<p class="truncate font-mono text-xs text-muted-foreground" title={node.host}>
										{node.host}
									</p>
								</div>
								<Badge variant="outline" class="flex items-center gap-1.5 shrink-0 {statusInfo.class}">
									<StatusIcon class="h-3 w-3" />
									{statusInfo.label}
								</Badge>
							</div>
						</CardHeader>

						<CardContent class="space-y-4 pb-4">
							<!-- Metrics Grid -->
							<div class="grid grid-cols-2 gap-2 rounded-lg bg-muted/40 p-3 text-xs">
								<div class="space-y-0.5">
									<div class="flex items-center gap-1 text-muted-foreground">
										<Server class="h-3.5 w-3.5" />
										<span>Servers</span>
									</div>
									<div class="font-medium text-foreground">
										{node.runningCount} / {node.serverCount} active
										{#if node.maxServers > 0}
											<span class="text-muted-foreground">({node.maxServers} max)</span>
										{/if}
									</div>
								</div>

								<div class="space-y-0.5">
									<div class="flex items-center gap-1 text-muted-foreground">
										<Cpu class="h-3.5 w-3.5" />
										<span>Allocated RAM</span>
									</div>
									<div class="font-medium text-foreground">
										{formatMemory(node.allocatedMemoryMb)}
										{#if node.maxMemoryMb > 0}
											<span class="text-muted-foreground">/ {formatMemory(node.maxMemoryMb)}</span>
										{/if}
									</div>
								</div>

								{#if node.advertisedIp}
									<div class="col-span-2 space-y-0.5 pt-1 border-t border-border/50">
										<div class="flex items-center gap-1 text-muted-foreground">
											<Radio class="h-3.5 w-3.5" />
											<span>Advertised IP</span>
										</div>
										<div class="font-mono font-medium text-foreground">
											{node.advertisedIp}
										</div>
									</div>
								{/if}

								{#if node.tlsEnabled}
									<div class="col-span-2 flex items-center gap-1 text-emerald-600 dark:text-emerald-400 pt-1">
										<ShieldCheck class="h-3.5 w-3.5" />
										<span>TLS Secured {node.tlsSkipVerify ? '(Insecure Skip Verify)' : ''}</span>
									</div>
								{/if}

								{#if pingInfo}
									<div class="col-span-2 flex items-center gap-1.5 text-xs text-primary pt-1">
										<Activity class="h-3.5 w-3.5" />
										<span>Last Ping: {pingInfo.latency}ms</span>
									</div>
								{/if}
							</div>
						</CardContent>
					</div>

					<!-- Card Footer Actions -->
					<div class="flex items-center justify-between border-t border-border/60 bg-muted/20 px-4 py-2.5">
						<Button
							variant="ghost"
							size="sm"
							class="h-8 px-2 text-xs"
							onclick={() => handlePingNode(node.id)}
							disabled={pingingNodeId === node.id}
						>
							{#if pingingNodeId === node.id}
								<Loader2 class="mr-1.5 h-3.5 w-3.5 animate-spin" />
								Pinging...
							{:else}
								<Activity class="mr-1.5 h-3.5 w-3.5 text-muted-foreground" />
								Ping
							{/if}
						</Button>

						<div class="flex items-center gap-1">
							<Button
								variant="ghost"
								size="icon"
								class="h-8 w-8 text-muted-foreground hover:text-foreground"
								onclick={() => openEditNode(node)}
								title="Edit Node"
							>
								<Edit class="h-3.5 w-3.5" />
							</Button>
							{#if !node.isLocal}
								<Button
									variant="ghost"
									size="icon"
									class="h-8 w-8 text-destructive/70 hover:bg-destructive/10 hover:text-destructive"
									onclick={() => handleDeleteNode(node)}
									title="Delete Node"
								>
									<Trash2 class="h-3.5 w-3.5" />
								</Button>
							{/if}
						</div>
					</div>
				</Card>
			{/each}
		</div>
	{/if}
</div>

<!-- Add Node Dialog -->
<Dialog bind:open={showAddDialog}>
	<DialogContent class="sm:max-w-lg max-h-[90vh] overflow-y-auto">
		<DialogHeader>
			<DialogTitle>Add Docker Daemon Node</DialogTitle>
			<DialogDescription>
				Connect an external Docker daemon host via TCP socket or unix pipe.
			</DialogDescription>
		</DialogHeader>

		<div class="space-y-4 py-2">
			<div class="space-y-2">
				<Label for="node-name">Node Name *</Label>
				<Input
					id="node-name"
					placeholder="e.g. prox3-worker"
					bind:value={newNode.name}
					disabled={isSaving}
				/>
			</div>

			<div class="space-y-2">
				<Label for="node-host">Docker Host URL *</Label>
				<Input
					id="node-host"
					placeholder="tcp://192.168.0.102:2376 or unix:///var/run/docker.sock"
					bind:value={newNode.host}
					disabled={isSaving}
				/>
				<p class="text-xs text-muted-foreground">
					Ensure Docker daemon on the host is listening on this socket/address.
				</p>
			</div>

			<div class="space-y-2">
				<Label for="node-adv-ip">Advertised IP Address</Label>
				<Input
					id="node-adv-ip"
					placeholder="e.g. 192.168.0.102"
					bind:value={newNode.advertisedIp}
					disabled={isSaving}
				/>
				<p class="text-xs text-muted-foreground">
					External IP address that players connect to when servers run on this node.
				</p>
			</div>

			<div class="grid grid-cols-2 gap-4">
				<div class="space-y-2">
					<Label for="node-max-mem">Max Memory Limit (MB)</Label>
					<Input
						id="node-max-mem"
						type="number"
						min="0"
						step="1024"
						placeholder="0 = Unlimited"
						bind:value={newNode.maxMemoryMb}
						disabled={isSaving}
					/>
				</div>

				<div class="space-y-2">
					<Label for="node-max-srv">Max Server Containers</Label>
					<Input
						id="node-max-srv"
						type="number"
						min="0"
						placeholder="0 = Unlimited"
						bind:value={newNode.maxServers}
						disabled={isSaving}
					/>
				</div>
			</div>

			<!-- Enabled Switch -->
			<div class="flex items-center justify-between rounded-lg border p-3">
				<div class="space-y-0.5">
					<Label for="node-enabled">Enable Node</Label>
					<p class="text-xs text-muted-foreground">
						Allow placement engine to assign servers to this Docker node.
					</p>
				</div>
				<Switch
					id="node-enabled"
					checked={newNode.enabled}
					onCheckedChange={(v) => (newNode.enabled = v)}
					disabled={isSaving}
				/>
			</div>

			<!-- TLS Configuration -->
			<div class="rounded-lg border p-3 space-y-3">
				<div class="flex items-center justify-between">
					<div class="space-y-0.5">
						<Label for="node-tls">TLS Authentication</Label>
						<p class="text-xs text-muted-foreground">
							Use client certificates for mutual TLS docker daemon auth.
						</p>
					</div>
					<Switch
						id="node-tls"
						checked={newNode.tlsEnabled}
						onCheckedChange={(v) => (newNode.tlsEnabled = v)}
						disabled={isSaving}
					/>
				</div>

				{#if newNode.tlsEnabled}
					<div class="space-y-3 pt-2 border-t border-border/60">
						<div class="flex items-center justify-between">
							<Label for="node-skip-verify" class="text-xs">Skip TLS Certificate Verification</Label>
							<Switch
								id="node-skip-verify"
								checked={newNode.tlsSkipVerify}
								onCheckedChange={(v) => (newNode.tlsSkipVerify = v)}
								disabled={isSaving}
							/>
						</div>

						<div class="space-y-1.5">
							<Label for="node-ca-cert" class="text-xs">CA Certificate (PEM)</Label>
							<Textarea
								id="node-ca-cert"
								placeholder="-----BEGIN CERTIFICATE-----\n..."
								bind:value={newNode.tlsCaCert}
								class="font-mono text-xs h-16"
								disabled={isSaving}
							/>
						</div>

						<div class="space-y-1.5">
							<Label for="node-cert" class="text-xs">Client Certificate (PEM)</Label>
							<Textarea
								id="node-cert"
								placeholder="-----BEGIN CERTIFICATE-----\n..."
								bind:value={newNode.tlsCert}
								class="font-mono text-xs h-16"
								disabled={isSaving}
							/>
						</div>

						<div class="space-y-1.5">
							<Label for="node-key" class="text-xs">Client Private Key (PEM)</Label>
							<Textarea
								id="node-key"
								placeholder="-----BEGIN RSA PRIVATE KEY-----\n..."
								bind:value={newNode.tlsKey}
								class="font-mono text-xs h-16"
								disabled={isSaving}
							/>
						</div>
					</div>
				{/if}
			</div>
		</div>

		<DialogFooter>
			<Button variant="outline" onclick={() => (showAddDialog = false)} disabled={isSaving}>
				Cancel
			</Button>
			<Button onclick={handleCreateNode} disabled={isSaving}>
				{#if isSaving}
					<Loader2 class="mr-2 h-4 w-4 animate-spin" />
					Adding...
				{:else}
					Add Node
				{/if}
			</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>

<!-- Edit Node Dialog -->
<Dialog bind:open={showEditDialog}>
	<DialogContent class="sm:max-w-lg max-h-[90vh] overflow-y-auto">
		<DialogHeader>
			<DialogTitle>Edit Docker Daemon Node</DialogTitle>
			<DialogDescription>
				Update host, capacity limits, or TLS configuration for this node.
			</DialogDescription>
		</DialogHeader>

		{#if editingNode}
			<div class="space-y-4 py-2">
				<div class="space-y-2">
					<Label for="edit-node-name">Node Name *</Label>
					<Input
						id="edit-node-name"
						bind:value={editingNode.name}
						disabled={isSaving}
					/>
				</div>

				<div class="space-y-2">
					<Label for="edit-node-host">Docker Host URL *</Label>
					<Input
						id="edit-node-host"
						bind:value={editingNode.host}
						disabled={isSaving}
					/>
				</div>

				<div class="space-y-2">
					<Label for="edit-node-adv-ip">Advertised IP Address</Label>
					<Input
						id="edit-node-adv-ip"
						placeholder="e.g. 192.168.0.102"
						bind:value={editingNode.advertisedIp}
						disabled={isSaving}
					/>
				</div>

				<div class="grid grid-cols-2 gap-4">
					<div class="space-y-2">
						<Label for="edit-node-max-mem">Max Memory Limit (MB)</Label>
						<Input
							id="edit-node-max-mem"
							type="number"
							min="0"
							step="1024"
							placeholder="0 = Unlimited"
							bind:value={editingNode.maxMemoryMb}
							disabled={isSaving}
						/>
					</div>

					<div class="space-y-2">
						<Label for="edit-node-max-srv">Max Server Containers</Label>
						<Input
							id="edit-node-max-srv"
							type="number"
							min="0"
							placeholder="0 = Unlimited"
							bind:value={editingNode.maxServers}
							disabled={isSaving}
						/>
					</div>
				</div>

				<!-- Enabled Switch -->
				<div class="flex items-center justify-between rounded-lg border p-3">
					<div class="space-y-0.5">
						<Label for="edit-node-enabled">Enable Node</Label>
						<p class="text-xs text-muted-foreground">
							Allow placement engine to assign servers to this Docker node.
						</p>
					</div>
					<Switch
						id="edit-node-enabled"
						checked={editingNode.enabled}
						onCheckedChange={(v) => {
							if (editingNode) editingNode.enabled = v;
						}}
						disabled={isSaving}
					/>
				</div>

				<!-- TLS Configuration -->
				<div class="rounded-lg border p-3 space-y-3">
					<div class="flex items-center justify-between">
						<div class="space-y-0.5">
							<Label for="edit-node-tls">TLS Authentication</Label>
							<p class="text-xs text-muted-foreground">
								Use client certificates for mutual TLS auth.
							</p>
						</div>
						<Switch
							id="edit-node-tls"
							checked={editingNode.tlsEnabled}
							onCheckedChange={(v) => {
								if (editingNode) editingNode.tlsEnabled = v;
							}}
							disabled={isSaving}
						/>
					</div>

					{#if editingNode.tlsEnabled}
						<div class="space-y-3 pt-2 border-t border-border/60">
							<div class="flex items-center justify-between">
								<Label for="edit-node-skip-verify" class="text-xs">Skip TLS Certificate Verification</Label>
								<Switch
									id="edit-node-skip-verify"
									checked={editingNode.tlsSkipVerify}
									onCheckedChange={(v) => {
										if (editingNode) editingNode.tlsSkipVerify = v;
									}}
									disabled={isSaving}
								/>
							</div>

							<div class="space-y-1.5">
								<Label for="edit-node-ca-cert" class="text-xs">CA Certificate (PEM)</Label>
								<Textarea
									id="edit-node-ca-cert"
									placeholder="-----BEGIN CERTIFICATE-----\n..."
									bind:value={editingNode.tlsCaCert}
									class="font-mono text-xs h-16"
									disabled={isSaving}
								/>
							</div>

							<div class="space-y-1.5">
								<Label for="edit-node-cert" class="text-xs">Client Certificate (PEM)</Label>
								<Textarea
									id="edit-node-cert"
									placeholder="-----BEGIN CERTIFICATE-----\n..."
									bind:value={editingNode.tlsCert}
									class="font-mono text-xs h-16"
									disabled={isSaving}
								/>
							</div>

							<div class="space-y-1.5">
								<Label for="edit-node-key" class="text-xs">Client Private Key (PEM)</Label>
								<Textarea
									id="edit-node-key"
									placeholder="-----BEGIN RSA PRIVATE KEY-----\n..."
									bind:value={editingNode.tlsKey}
									class="font-mono text-xs h-16"
									disabled={isSaving}
								/>
							</div>
						</div>
					{/if}
				</div>
			</div>
		{/if}

		<DialogFooter>
			<Button variant="outline" onclick={() => (showEditDialog = false)} disabled={isSaving}>
				Cancel
			</Button>
			<Button onclick={handleUpdateNode} disabled={isSaving}>
				{#if isSaving}
					<Loader2 class="mr-2 h-4 w-4 animate-spin" />
					Saving...
				{:else}
					Save Changes
				{/if}
			</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>
