import { writable } from 'svelte/store';

export type Theme = 'dark' | 'light' | 'system';

function createThemeStore() {
	const initial: Theme =
		typeof localStorage !== 'undefined'
			? (localStorage.getItem('dermaga_theme') as Theme) || 'system'
			: 'system';

	const { subscribe, set } = writable<Theme>(initial);

	function apply(theme: Theme) {
		if (typeof window === 'undefined') return;
		const isDark =
			theme === 'dark' ||
			(theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches);

		if (isDark) {
			document.documentElement.classList.add('dark');
		} else {
			document.documentElement.classList.remove('dark');
		}
		localStorage.setItem('dermaga_theme', theme);
		set(theme);
	}

	return {
		subscribe,
		setTheme: (t: Theme) => apply(t),
		init: () => {
			if (typeof window === 'undefined') return;
			apply(initial);
			window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', (e) => {
				const current = (localStorage.getItem('dermaga_theme') as Theme) || 'system';
				if (current === 'system') {
					if (e.matches) {
						document.documentElement.classList.add('dark');
					} else {
						document.documentElement.classList.remove('dark');
					}
				}
			});
		}
	};
}

export const theme = createThemeStore();
