// Minimal reactive async loader used by every page:
// { data, error, loading, reload } with a user-facing error message.

export type Loadable<T> = {
	data: T | null;
	error: string | null;
	loading: boolean;
	reload: () => Promise<void>;
};

export function createLoadable<T>(loader: () => Promise<T>): Loadable<T> {
	let data = $state<T | null>(null);
	let error = $state<string | null>(null);
	let loading = $state(true);

	async function run() {
		loading = true;
		error = null;
		try {
			data = await loader();
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		} finally {
			loading = false;
		}
	}

	void run();

	return {
		get data() {
			return data;
		},
		get error() {
			return error;
		},
		get loading() {
			return loading;
		},
		reload: run
	};
}

export function sleep(ms: number): Promise<void> {
	return new Promise((resolve) => setTimeout(resolve, ms));
}