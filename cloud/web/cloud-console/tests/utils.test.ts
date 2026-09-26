import { describe, expect, test } from 'bun:test';
import {
	cls,
	tsToDate,
	fmtDateTime,
	fmtRelative,
	fmtRam,
	fmtCpu,
	fmtGb,
	fmtUsd,
	nodeStatusMeta,
	workloadStatusMeta,
	provisionStatusMeta,
	originLabel,
	providerLabel
} from '../src/lib/utils';
import {
	NodeStatus,
	WorkloadStatus,
	ProvisionStatus,
	NodeOrigin,
	ProviderId
} from '../src/lib/proto/cloud/v1/common_pb';

describe('cls()', () => {
	test('combines truthy classes and ignores falsy values', () => {
		expect(cls('btn', false, 'btn-primary', null, undefined, '', 'active')).toBe(
			'btn btn-primary active'
		);
		expect(cls()).toBe('');
	});
});

describe('tsToDate() & date formatters', () => {
	test('converts protobuf timestamp bigint seconds and nanos correctly', () => {
		const ts = { seconds: BigInt(1700000000), nanos: 500_000_000 };
		const d = tsToDate(ts);
		expect(d).not.toBeNull();
		expect(d?.getTime()).toBe(1700000000500);

		expect(tsToDate(null)).toBeNull();
		expect(tsToDate(undefined)).toBeNull();
	});

	test('fmtDateTime formats valid and null dates', () => {
		expect(fmtDateTime(null)).toBe('—');
		expect(fmtDateTime(undefined)).toBe('—');

		const d = new Date(Date.UTC(2026, 8, 24, 12, 0, 0));
		const s = fmtDateTime(d);
		expect(s).not.toBe('—');
	});

	test('fmtRelative formats elapsed durations properly', () => {
		expect(fmtRelative(null)).toBe('—');
		expect(fmtRelative(undefined)).toBe('—');

		const now = Date.now();
		expect(fmtRelative(new Date(now - 10_000))).toBe('just now');
		expect(fmtRelative(new Date(now - 120_000))).toBe('2m ago');
		expect(fmtRelative(new Date(now - 7_200_000))).toBe('2h ago');
		expect(fmtRelative(new Date(now - 172_800_000))).toBe('2d ago');
	});
});

describe('Metric & Cost formatters', () => {
	test('fmtRam formats MiB and GiB', () => {
		expect(fmtRam(null)).toBe('—');
		expect(fmtRam(undefined)).toBe('—');
		expect(fmtRam(0)).toBe('—');
		expect(fmtRam(512)).toBe('512 MiB');
		expect(fmtRam(1024)).toBe('1.0 GiB');
		expect(fmtRam(BigInt(4096))).toBe('4.0 GiB');
	});

	test('fmtCpu formats millicores and vCPU', () => {
		expect(fmtCpu(null)).toBe('—');
		expect(fmtCpu(undefined)).toBe('—');
		expect(fmtCpu(500)).toBe('500 mCPU');
		expect(fmtCpu(1000)).toBe('1.0 vCPU');
		expect(fmtCpu(BigInt(4000))).toBe('4.0 vCPU');
	});

	test('fmtGb formats gigabytes', () => {
		expect(fmtGb(null)).toBe('—');
		expect(fmtGb(undefined)).toBe('—');
		expect(fmtGb(50)).toBe('50 GiB');
		expect(fmtGb(BigInt(200))).toBe('200 GiB');
	});

	test('fmtUsd formats dollar amounts', () => {
		expect(fmtUsd(null)).toBe('—');
		expect(fmtUsd(undefined)).toBe('—');
		expect(fmtUsd(0)).toBe('$0.00');
		expect(fmtUsd(29.99)).toBe('$29.99');
		expect(fmtUsd(120.5)).toBe('$120.50');
	});
});

describe('Status metadata mappers', () => {
	test('nodeStatusMeta maps each NodeStatus enum', () => {
		expect(nodeStatusMeta(NodeStatus.ONLINE)).toEqual({ label: 'Online', tone: 'green' });
		expect(nodeStatusMeta(NodeStatus.OFFLINE)).toEqual({ label: 'Offline', tone: 'red' });
		expect(nodeStatusMeta(NodeStatus.PENDING)).toEqual({ label: 'Pending', tone: 'yellow' });
		expect(nodeStatusMeta(NodeStatus.DRAINING)).toEqual({ label: 'Draining', tone: 'blue' });
		expect(nodeStatusMeta(NodeStatus.ERROR)).toEqual({ label: 'Error', tone: 'red' });
		expect(nodeStatusMeta(null)).toEqual({ label: 'Unknown', tone: 'neutral' });
	});

	test('workloadStatusMeta maps each WorkloadStatus enum', () => {
		expect(workloadStatusMeta(WorkloadStatus.RUNNING)).toEqual({ label: 'Running', tone: 'green' });
		expect(workloadStatusMeta(WorkloadStatus.STOPPED)).toEqual({ label: 'Stopped', tone: 'neutral' });
		expect(workloadStatusMeta(WorkloadStatus.PENDING)).toEqual({ label: 'Pending', tone: 'yellow' });
		expect(workloadStatusMeta(WorkloadStatus.ERROR)).toEqual({ label: 'Error', tone: 'red' });
	});

	test('provisionStatusMeta maps each ProvisionStatus enum', () => {
		expect(provisionStatusMeta(ProvisionStatus.APPLIED)).toEqual({ label: 'Applied', tone: 'green' });
		expect(provisionStatusMeta(ProvisionStatus.FAILED)).toEqual({ label: 'Failed', tone: 'red' });
		expect(provisionStatusMeta(ProvisionStatus.PLANNED)).toEqual({ label: 'Planned', tone: 'blue' });
		expect(provisionStatusMeta(ProvisionStatus.APPLYING)).toEqual({ label: 'Applying', tone: 'blue' });
		expect(provisionStatusMeta(ProvisionStatus.DESTROYED)).toEqual({ label: 'Destroyed', tone: 'neutral' });
	});

	test('originLabel and providerLabel map properly', () => {
		expect(originLabel(NodeOrigin.BYO)).toBe('BYO');
		expect(originLabel(NodeOrigin.MANAGED)).toBe('Managed');
		expect(originLabel(null)).toBe('—');

		expect(providerLabel(ProviderId.HETZNER)).toBe('Hetzner');
		expect(providerLabel(ProviderId.AWS)).toBe('AWS');
		expect(providerLabel(ProviderId.GCP)).toBe('GCP');
		expect(providerLabel(null)).toBe('—');
	});
});
