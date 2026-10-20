// SPDX-License-Identifier: MIT AND MPL-2.0
// This file includes DiscoPanel material under the MIT License,
// Copyright (c) 2025 Nicholas W. Heyer. The MIT license text is in LICENSE
// and still applies to that inherited material.
// Maintainer changes are licensed under the Mozilla Public License, v. 2.0.
// If a copy of the MPL was not distributed with this file, You can obtain
// one at https://mozilla.org/MPL/2.0/.

import { writable, derived } from 'svelte/store';
import type { Server } from '$lib/proto/carbonpanel/v1/common_pb';
import { ServerStatus } from '$lib/proto/carbonpanel/v1/common_pb';
import { rpcClient, silentCallOptions } from '$lib/api/rpc-client';
import { create } from '@bufbuild/protobuf';
import { ListServersRequestSchema } from '$lib/proto/carbonpanel/v1/server_pb';

function createServersStore() {
	const { subscribe, set, update } = writable<Server[]>([]);

	return {
		subscribe,
		set,
		fetchServers: async (skipLoading = false) => {
			try {
				const request = create(ListServersRequestSchema, { fullStats: false });
				const callOptions = skipLoading ? silentCallOptions : undefined;
				const response = await rpcClient.server.listServers(request, callOptions);
				set(response.servers);
				return response.servers;
			} catch (error) {
				console.error('Failed to fetch servers:', error);
				throw error;
			}
		},
		updateServer: (server: Server) => {
			update((servers) => {
				const index = servers.findIndex((s) => s.id === server.id);
				if (index !== -1) {
					return [...servers.slice(0, index), server, ...servers.slice(index + 1)];
				}
				return [...servers, server];
			});
		},
		removeServer: (id: string) => {
			update((servers) => servers.filter((s) => s.id !== id));
		},
		addServer: (server: Server) => {
			update((servers) => [...servers, server]);
		}
	};
}

export const serversStore = createServersStore();

export const runningServers = derived(serversStore, ($servers) =>
	$servers.filter((server) => server.status === ServerStatus.RUNNING)
);

export const stoppedServers = derived(serversStore, ($servers) =>
	$servers.filter((server) => server.status === ServerStatus.STOPPED)
);

export const pausedServers = derived(serversStore, ($servers) =>
	$servers.filter((server) => server.status === ServerStatus.PAUSED)
);

export const deepsleepServers = derived(serversStore, ($servers) =>
	$servers.filter((server) => server.status === ServerStatus.DEEPSLEEP)
);

function getTimestampMs(ts: { seconds: bigint } | undefined): number {
	if (!ts) return 0;
	return Number(ts.seconds) * 1000;
}

/**
 * AUTO SORT PRIORITY:
 * 1. Pin most recently created/updated server as #1
 * 2. Running servers w/ players first (by player count desc)
 * 3. Running servers wo/ players (by lastStarted desc)
 * 4. Non-running servers (by updatedAt desc)
 */
export function sortServersByActivity(servers: Server[]): Server[] {
	if (servers.length <= 1) return servers;

	// Find the most recently created or updated server to pin
	let pinnedIdx = 0;
	let pinnedTime = 0;
	for (let i = 0; i < servers.length; i++) {
		const created = getTimestampMs(servers[i].createdAt);
		const updated = getTimestampMs(servers[i].updatedAt);
		const latest = Math.max(created, updated);
		if (latest > pinnedTime) {
			pinnedTime = latest;
			pinnedIdx = i;
		}
	}

	const pinned = servers[pinnedIdx];
	const rest = servers.filter((_, i) => i !== pinnedIdx);

	rest.sort((a, b) => {
		const aRunning = a.status === ServerStatus.RUNNING ? 1 : 0;
		const bRunning = b.status === ServerStatus.RUNNING ? 1 : 0;
		// Running servers first
		if (aRunning !== bRunning) return bRunning - aRunning;

		// Both running: sort by players online desc
		if (aRunning && bRunning) {
			const playerDiff = (b.playersOnline || 0) - (a.playersOnline || 0);
			if (playerDiff !== 0) return playerDiff;
			// Tiebreak by lastStarted (most recent first)
			return getTimestampMs(b.lastStarted) - getTimestampMs(a.lastStarted);
		}

		// Both not running: sort by updatedAt desc
		return getTimestampMs(b.updatedAt) - getTimestampMs(a.updatedAt);
	});

	return [pinned, ...rest];
}

export const activitySortedServers = derived(serversStore, ($servers) =>
	sortServersByActivity([...$servers])
);
