// Shared formatting / status helpers for the console.

import {
	NodeStatus,
	WorkloadStatus,
	ProvisionStatus,
	NodeOrigin,
	ProviderId,
	Role
} from '$lib/proto/cloud/v1/common_pb';

export function cls(...parts: Array<string | false | null | undefined>): string {
	return parts.filter(Boolean).join(' ');
}

/** Format a protobuf Timestamp (bigint seconds + nanos) as a Date. */
export function tsToDate(ts: { seconds: bigint; nanos?: number } | null | undefined): Date | null {
	if (!ts) return null;
	return new Date(Number(ts.seconds) * 1000 + Math.floor((ts.nanos ?? 0) / 1e6));
}

export function fmtDateTime(d: Date | null | undefined): string {
	if (!d) return '—';
	return d.toLocaleString(undefined, {
		year: 'numeric',
		month: 'short',
		day: 'numeric',
		hour: '2-digit',
		minute: '2-digit',
		second: '2-digit'
	});
}

export function fmtRelative(d: Date | null | undefined): string {
	if (!d) return '—';
	const diff = Date.now() - d.getTime();
	const abs = Math.abs(diff);
	const suffix = diff >= 0 ? 'ago' : 'from now';
	if (abs < 45_000) return 'just now';
	const mins = Math.round(abs / 60_000);
	if (mins < 60) return `${mins}m ${suffix}`;
	const hrs = Math.round(abs / 3_600_000);
	if (hrs < 24) return `${hrs}h ${suffix}`;
	const days = Math.round(abs / 86_400_000);
	return `${days}d ${suffix}`;
}

export function fmtRam(mb: number | bigint | undefined | null): string {
	if (mb === undefined || mb === null || mb === 0) return '—';
	const n = Number(mb);
	if (n >= 1024) return `${(n / 1024).toFixed(1)} GiB`;
	return `${n} MiB`;
}

export function fmtCpu(millicores: number | bigint | undefined | null): string {
	if (millicores === undefined || millicores === null) return '—';
	const n = Number(millicores);
	if (n >= 1000) return `${(n / 1000).toFixed(1)} vCPU`;
	return `${n} mCPU`;
}

export function fmtGb(gb: number | bigint | undefined | null): string {
	if (gb === undefined || gb === null) return '—';
	return `${Number(gb)} GiB`;
}

export function fmtUsd(n: number | undefined | null): string {
	if (n === undefined || n === null) return '—';
	return `$${n.toFixed(2)}`;
}

export type TagTone = 'neutral' | 'blue' | 'green' | 'red' | 'yellow' | 'purple';

type StatusMeta = { label: string; tone: TagTone };

export function nodeStatusMeta(s: NodeStatus | number | undefined | null): StatusMeta {
	switch (s) {
		case NodeStatus.ONLINE:
			return { label: 'Online', tone: 'green' };
		case NodeStatus.OFFLINE:
			return { label: 'Offline', tone: 'red' };
		case NodeStatus.PENDING:
			return { label: 'Pending', tone: 'yellow' };
		case NodeStatus.DRAINING:
			return { label: 'Draining', tone: 'blue' };
		case NodeStatus.ERROR:
			return { label: 'Error', tone: 'red' };
		default:
			return { label: 'Unknown', tone: 'neutral' };
	}
}

export function workloadStatusMeta(s: WorkloadStatus | number | undefined | null): StatusMeta {
	switch (s) {
		case WorkloadStatus.RUNNING:
			return { label: 'Running', tone: 'green' };
		case WorkloadStatus.HIBERNATED:
			return { label: 'Hibernated', tone: 'purple' };
		case WorkloadStatus.STOPPED:
			return { label: 'Stopped', tone: 'neutral' };
		case WorkloadStatus.PENDING:
			return { label: 'Pending', tone: 'yellow' };
		case WorkloadStatus.ERROR:
			return { label: 'Error', tone: 'red' };
		default:
			return { label: 'Unknown', tone: 'neutral' };
	}
}

export function provisionStatusMeta(s: ProvisionStatus | number | undefined | null): StatusMeta {
	switch (s) {
		case ProvisionStatus.PENDING:
			return { label: 'Pending', tone: 'yellow' };
		case ProvisionStatus.PLANNED:
			return { label: 'Planned', tone: 'blue' };
		case ProvisionStatus.APPLYING:
			return { label: 'Applying', tone: 'blue' };
		case ProvisionStatus.APPLIED:
			return { label: 'Applied', tone: 'green' };
		case ProvisionStatus.FAILED:
			return { label: 'Failed', tone: 'red' };
		case ProvisionStatus.DESTROYED:
			return { label: 'Destroyed', tone: 'neutral' };
		default:
			return { label: 'Unknown', tone: 'neutral' };
	}
}

export function originLabel(o: NodeOrigin | number | undefined | null): string {
	switch (o) {
		case NodeOrigin.BYO:
			return 'BYO';
		case NodeOrigin.MANAGED:
			return 'Managed';
		default:
			return '—';
	}
}

export function providerLabel(p: ProviderId | number | undefined | null): string {
	switch (p) {
		case ProviderId.HETZNER:
			return 'Hetzner';
		case ProviderId.AWS:
			return 'AWS';
		case ProviderId.GCP:
			return 'GCP';
		case ProviderId.DIGITALOCEAN:
			return 'DigitalOcean';
		case ProviderId.PROXMOX:
			return 'Proxmox';
		case ProviderId.GENERIC:
			return 'Generic';
		default:
			return '—';
	}
}

export function roleToEnum(role: string): Role {
	switch (role) {
		case 'owner':
			return Role.OWNER;
		case 'admin':
			return Role.ADMIN;
		case 'operator':
			return Role.OPERATOR;
		case 'viewer':
			return Role.VIEWER;
		case 'billing':
			return Role.BILLING;
		default:
			return Role.VIEWER;
	}
}

export function roleFromEnum(r: Role | number | undefined | null): string {
	switch (r) {
		case Role.OWNER:
			return 'owner';
		case Role.ADMIN:
			return 'admin';
		case Role.OPERATOR:
			return 'operator';
		case Role.VIEWER:
			return 'viewer';
		case Role.BILLING:
			return 'billing';
		default:
			return 'viewer';
	}
}

export async function sha256Hex(input: string): Promise<string> {
	const data = new TextEncoder().encode(input);
	const digest = await crypto.subtle.digest('SHA-256', data);
	return Array.from(new Uint8Array(digest))
		.map((b) => b.toString(16).padStart(2, '0'))
		.join('');
}