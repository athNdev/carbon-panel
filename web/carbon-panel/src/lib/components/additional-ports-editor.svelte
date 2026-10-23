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
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import { Plus, X, AlertCircle } from '@lucide/svelte';
	import type { AdditionalPort } from '$lib/proto/carbonpanel/v1/common_pb';
	import { AdditionalPortSchema } from '$lib/proto/carbonpanel/v1/common_pb';
	import { create } from '@bufbuild/protobuf';

	interface Props {
		ports?: AdditionalPort[];
		disabled?: boolean;
		usedPorts?: Record<number, boolean>;
		onchange?: (ports: AdditionalPort[]) => void;
	}

	let { ports = $bindable([]), disabled = false, usedPorts = {}, onchange }: Props = $props();

	let nextRowId = 0;

	function createRowId() {
		return `additional-port-${nextRowId++}`;
	}

	// Rows are keyed by identity so removing one cannot shift an error onto a
	// different port.
	let rowIds = $state<string[]>(ports.map(() => createRowId()));
	let portErrors = $state<Record<string, string>>({});

	$effect(() => {
		if (rowIds.length !== ports.length) {
			rowIds = ports.map((_, index) => rowIds[index] ?? createRowId());
			validatePorts();
		}
	});

	function addPort() {
		const newPort = create(AdditionalPortSchema, {
			name: '',
			containerPort: findNextAvailablePort(),
			hostPort: findNextAvailablePort(),
			protocol: 'tcp'
		});
		ports = [...ports, newPort];
		rowIds = [...rowIds, createRowId()];
		onchange?.(ports);
	}

	function removePort(index: number) {
		ports = ports.filter((_, i) => i !== index);
		rowIds = rowIds.filter((_, i) => i !== index);
		validatePorts();
		onchange?.(ports);
	}

	function updatePort(
		index: number,
		field: keyof AdditionalPort,
		value: string | number | boolean
	) {
		ports[index] = {
			...ports[index],
			[field]: value
		};

		// A port's uniqueness depends on both its number and protocol, so either
		// change can create (or clear) a duplicate.
		if (field === 'hostPort' || field === 'protocol') {
			validatePorts();
		}

		onchange?.(ports);
	}

	function errorFor(index: number): string | undefined {
		const id = rowIds[index];
		return id ? portErrors[id] : undefined;
	}

	function validatePorts() {
		const errors: Record<string, string> = {};
		ports.forEach((_, index) => {
			const id = rowIds[index];
			const error = validatePort(index);
			if (id && error) errors[id] = error;
		});
		portErrors = errors;
	}

	function validatePort(index: number): string | undefined {
		const port = ports[index];
		const hostPort = Number(port.hostPort);
		if (hostPort && usedPorts[hostPort]) {
			return `Port ${hostPort} is already in use`;
		}
		if (hostPort < 1 || hostPort > 65535) {
			return 'Port must be between 1 and 65535';
		}
		// Check for duplicates within additional ports
		const hasDuplicate = ports.some(
			(other, otherIndex) =>
				otherIndex !== index && other.hostPort === hostPort && other.protocol === port.protocol
		);
		return hasDuplicate ? `Duplicate port ${hostPort}/${port.protocol}` : undefined;
	}

	// Find next available port
	function findNextAvailablePort(startFrom: number = 25566): number {
		let port = startFrom;
		while (port <= 65535) {
			if (!usedPorts[port] && !ports.some((p) => p.hostPort === port)) {
				return port;
			}
			port++;
		}
		return 25566; // Fallback
	}
</script>

<div class="space-y-4">
	<div class="flex items-center justify-between">
		<div>
			<Label class="text-sm font-medium">Additional Ports</Label>
			<p class="mt-1 text-xs text-muted-foreground">
				Extra ports stay on loopback when the server is proxied. Mark a port public only when
				clients must reach it directly, such as Bedrock UDP.
			</p>
		</div>
		<Button
			type="button"
			variant="outline"
			size="sm"
			onclick={addPort}
			{disabled}
			class="h-8 gap-1"
		>
			<Plus class="h-3 w-3" />
			Add Port
		</Button>
	</div>

	{#if ports.length > 0}
		<div class="rounded-lg border bg-muted/10 p-3">
			<div class="space-y-3">
				<!-- Headers -->
				<div
					class="hidden grid-cols-12 gap-2 px-1 text-xs font-medium text-muted-foreground sm:grid"
				>
					<div class="col-span-4">Name</div>
					<div class="col-span-2">Container Port</div>
					<div class="col-span-2">Host Port</div>
					<div class="col-span-2">Protocol</div>
					<div class="col-span-2"></div>
				</div>

				<!-- Port entries -->
				{#each ports as port, index (rowIds[index] ?? index)}
					{@const portError = errorFor(index)}
					<div class="space-y-2">
						<div class="grid grid-cols-1 items-center gap-2 sm:grid-cols-12">
							<div class="col-span-1 sm:col-span-4">
								<Input
									type="text"
									placeholder="e.g., BlueMap Web"
									aria-label={`Name for additional port ${index + 1}`}
									bind:value={port.name}
									{disabled}
									onchange={() => updatePort(index, 'name', port.name)}
									class="h-8 text-xs"
								/>
							</div>
							<div class="col-span-1 sm:col-span-2">
								<Input
									type="number"
									min="1"
									max="65535"
									placeholder="8100"
									aria-label={`Container port for additional port ${index + 1}`}
									bind:value={port.containerPort}
									{disabled}
									onchange={() => updatePort(index, 'containerPort', port.containerPort)}
									class="h-8 text-xs"
								/>
							</div>
							<div class="col-span-1 sm:col-span-2">
								<Input
									type="number"
									min="1"
									max="65535"
									placeholder="8100"
									aria-label={`Host port for additional port ${index + 1}`}
									bind:value={port.hostPort}
									{disabled}
									onchange={() => updatePort(index, 'hostPort', port.hostPort)}
									class="h-8 text-xs {portError ? 'border-destructive' : ''}"
								/>
							</div>
							<div class="col-span-1 sm:col-span-2">
								<Select
									type="single"
									value={port.protocol}
									onValueChange={(v) => updatePort(index, 'protocol', v)}
									{disabled}
								>
									<SelectTrigger
										class="h-8 text-xs"
										aria-label={`Protocol for additional port ${index + 1}`}
									>
										<span>{port.protocol.toUpperCase()}</span>
									</SelectTrigger>
									<SelectContent>
										<SelectItem value="tcp">TCP</SelectItem>
										<SelectItem value="udp">UDP</SelectItem>
									</SelectContent>
								</Select>
							</div>
							<div class="col-span-1 flex justify-end sm:col-span-2">
								<Button
									type="button"
									variant="ghost"
									size="icon"
									aria-label={`Remove additional port ${index + 1}`}
									onclick={() => removePort(index)}
									{disabled}
									class="h-8 w-8 hover:bg-destructive/10 hover:text-destructive"
								>
									<X class="h-3 w-3" />
								</Button>
							</div>
						</div>

						<label class="flex items-center gap-2 pl-1 text-xs text-muted-foreground">
							<input
								type="checkbox"
								class="h-3.5 w-3.5"
								aria-label={`Expose additional port ${index + 1} publicly`}
								checked={port.exposePublic}
								{disabled}
								onchange={(event) => updatePort(index, 'exposePublic', event.currentTarget.checked)}
							/>
							Public on the internet
						</label>

						{#if portError}
							<div class="flex items-center gap-2 pl-1 text-destructive">
								<AlertCircle class="h-3 w-3" />
								<span class="text-xs">{portError}</span>
							</div>
						{/if}
					</div>
				{/each}
			</div>
		</div>
	{:else}
		<div class="rounded-lg border border-dashed p-4">
			<p class="text-center text-sm text-muted-foreground">
				No additional ports configured. Click "Add Port" to expose extra ports for mods or services.
			</p>
		</div>
	{/if}
</div>
