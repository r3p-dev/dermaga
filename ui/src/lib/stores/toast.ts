import { writable } from 'svelte/store';

export interface Toast {
	id: string;
	type: 'success' | 'error' | 'warning' | 'info';
	message: string;
	title?: string;
	timeout?: number;
}

function createToastStore() {
	const { subscribe, update } = writable<Toast[]>([]);

	function remove(id: string) {
		update((toasts) => toasts.filter((t) => t.id !== id));
	}

	function add(toast: Omit<Toast, 'id'>) {
		const id = Math.random().toString(36).substring(2, 9);
		const newToast: Toast = { id, ...toast };
		update((toasts) => [...toasts, newToast]);

		const duration = toast.timeout ?? (toast.type === 'error' ? 6000 : 3500);
		if (duration > 0) {
			setTimeout(() => {
				remove(id);
			}, duration);
		}
		return id;
	}

	return {
		subscribe,
		add,
		remove,
		success: (message: string, title?: string) => add({ type: 'success', message, title }),
		error: (message: string, title?: string) => add({ type: 'error', message, title }),
		warning: (message: string, title?: string) => add({ type: 'warning', message, title }),
		info: (message: string, title?: string) => add({ type: 'info', message, title })
	};
}

export const toast = createToastStore();
