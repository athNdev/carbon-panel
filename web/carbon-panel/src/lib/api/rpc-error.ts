import { Code, ConnectError } from '@connectrpc/connect';

/** Convert transport failures into useful UI copy without leaking protocol details. */
export function formatRpcError(error: unknown, fallback: string): string {
	if (!(error instanceof ConnectError)) return fallback;

	switch (error.code) {
		case Code.Unavailable:
			return 'Carbon Panel could not reach the server. Check the API connection and try again.';
		case Code.Unimplemented:
			return 'This operation is not supported by the connected server.';
		case Code.DeadlineExceeded:
			return 'The request timed out. Try again.';
		case Code.InvalidArgument:
		case Code.PermissionDenied:
		case Code.NotFound:
		case Code.AlreadyExists:
		case Code.FailedPrecondition:
		case Code.ResourceExhausted:
			return error.rawMessage || fallback;
		default:
			return fallback;
	}
}
