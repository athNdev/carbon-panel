import { NodeStatus } from '$lib/proto/carbonpanel/v1/node_pb';

export interface NodeStatusDescription {
	label: string;
	/** Tailwind text-color class for the status label (Carbon-themed). */
	dotClass: string;
}

const STATUS_DESCRIPTIONS: Record<number, NodeStatusDescription> = {
	[NodeStatus.ONLINE]: { label: 'ONLINE', dotClass: 'text-emerald-500' },
	[NodeStatus.OFFLINE]: { label: 'OFFLINE', dotClass: 'text-[#8d8d8d]' },
	[NodeStatus.ERROR]: { label: 'ERROR', dotClass: 'text-[#ff8389]' }
};

/**
 * Human-readable description of a Docker node status for the shell footer.
 * Unknown / unspecified statuses fall back to a neutral UNKNOWN label so the
 * UI never renders an empty or hardcoded placeholder.
 */
export function describeNodeStatus(status: NodeStatus | null | undefined): NodeStatusDescription {
	if (status === null || status === undefined) {
		return { label: 'UNKNOWN', dotClass: 'text-[#8d8d8d]' };
	}
	return STATUS_DESCRIPTIONS[status] ?? { label: 'UNKNOWN', dotClass: 'text-[#8d8d8d]' };
}

export interface PrimaryNodeCandidate {
	id: string;
	name: string;
	isLocal: boolean;
	status: NodeStatus;
}

/**
 * Pick the node shown in the shell footer: prefer the local node, then any
 * online node, then the first node. Returns null when there are no nodes.
 */
export function pickPrimaryNode<T extends PrimaryNodeCandidate>(nodes: T[]): T | null {
	if (nodes.length === 0) return null;
	return (
		nodes.find((node) => node.isLocal) ??
		nodes.find((node) => node.status === NodeStatus.ONLINE) ??
		nodes[0]
	);
}
