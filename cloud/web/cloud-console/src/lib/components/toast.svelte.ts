// Toast store: push + auto-dismiss, rendered by Toast.svelte.

export type ToastKind = 'info' | 'success' | 'error';

export type Toast = {
	id: number;
	kind: ToastKind;
	message: string;
};

let toasts = $state<Toast[]>([]);
let nextId = 1;

export function pushToast(kind: ToastKind, message: string): void {
	const id = nextId++;
	toasts = [...toasts, { id, kind, message }];
	window.setTimeout(() => {
		toasts = toasts.filter((t) => t.id !== id);
	}, 5000);
}

export function dismissToast(id: number): void {
	toasts = toasts.filter((t) => t.id !== id);
}

export function getToasts(): Toast[] {
	return toasts;
}