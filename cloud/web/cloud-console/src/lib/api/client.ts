// Connect-RPC API client: transport + auth/org header interceptor + one typed
// client per generated service descriptor + a call() wrapper that maps
// ConnectError codes to user-facing messages.
import {
	createClient,
	ConnectError,
	Code,
	type Interceptor,
	type UnaryRequest,
	type StreamRequest
} from '@connectrpc/connect';
import { createConnectTransport } from '@connectrpc/connect-web';
import { env } from '$env/dynamic/public';
import { auth } from '$lib/auth/clerk.svelte';
import { SessionService, ApiKeyService } from '$lib/proto/cloud/v1/auth_pb';
import { OrgService } from '$lib/proto/cloud/v1/org_pb';
import { NodeService } from '$lib/proto/cloud/v1/node_pb';
import { NodeTypeService } from '$lib/proto/cloud/v1/nodetype_pb';
import { ProvisionService } from '$lib/proto/cloud/v1/provision_pb';
import { WorkloadService } from '$lib/proto/cloud/v1/workload_pb';
import { RoleService } from '$lib/proto/cloud/v1/rbac_pb';
import { AuditService } from '$lib/proto/cloud/v1/audit_pb';
import { SystemService } from '$lib/proto/cloud/v1/system_pb';

export const API_URL: string = env.PUBLIC_API_URL || 'http://localhost:8080';

/** Attach the Clerk session token and active org id to every request. */
async function withHeaders(req: UnaryRequest | StreamRequest) {
	const header = new Headers(req.header);
	const token = await auth.token();
	if (token) header.set('Authorization', `Bearer ${token}`);
	const org = auth.org?.id;
	if (org) header.set('X-Carbon-Org', org);
	return { ...req, header };
}

const authInterceptor: Interceptor = (next) => async (req: UnaryRequest | StreamRequest) => {
	const out = await withHeaders(req);
	return next(out);
};

const transport = createConnectTransport({
	baseUrl: API_URL,
	interceptors: [authInterceptor]
});

export const sessionClient = createClient(SessionService, transport);
export const orgClient = createClient(OrgService, transport);
export const nodeClient = createClient(NodeService, transport);
export const nodeTypeClient = createClient(NodeTypeService, transport);
export const provisionClient = createClient(ProvisionService, transport);
export const workloadClient = createClient(WorkloadService, transport);
export const roleClient = createClient(RoleService, transport);
export const apiKeyClient = createClient(ApiKeyService, transport);
export const auditClient = createClient(AuditService, transport);
export const systemClient = createClient(SystemService, transport);

/** User-facing error carrying the Connect code (when one was mapped). */
export class ApiError extends Error {
	code: Code | 'unknown';
	constructor(message: string, code: Code | 'unknown' = 'unknown') {
		super(message);
		this.name = 'ApiError';
		this.code = code;
	}
}

/**
 * Run a client call and translate ConnectError codes into readable messages.
 * Server-provided detail is preserved for codes we do not special-case.
 */
export async function call<T>(fn: () => Promise<T>): Promise<T> {
	try {
		return await fn();
	} catch (e) {
		if (e instanceof ConnectError) {
			switch (e.code) {
				case Code.PermissionDenied:
					throw new ApiError('You do not have permission for this action.', e.code);
				case Code.Unauthenticated:
					throw new ApiError(
						'Your session has expired. Please sign in again and retry.',
						e.code
					);
				case Code.Unavailable:
					throw new ApiError(
						'The Carbon Cloud API is currently unavailable. Please try again in a moment.',
						e.code
					);
				case Code.NotFound:
					throw new ApiError('The requested resource was not found.', e.code);
				case Code.FailedPrecondition:
					throw new ApiError(
						e.rawMessage || 'The request could not be completed in the current state.',
						e.code
					);
				default:
					throw new ApiError(e.rawMessage || e.message, e.code);
			}
		}
		if (e instanceof Error) throw e;
		throw new ApiError(String(e));
	}
}