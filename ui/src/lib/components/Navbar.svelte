<script lang="ts">
	import { api } from '../api/client';
	import { theme, type Theme } from '../stores/theme';
	import { toast } from '../stores/toast';
	import type { Status } from '../types/api';

	let {
		status = null,
		onOpenShortcuts,
		onToggleSidebar
	}: {
		status: Status | null;
		onOpenShortcuts?: () => void;
		onToggleSidebar?: () => void;
	} = $props();

	let currentTheme = $derived($theme);

	function cycleTheme() {
		const next: Theme =
			currentTheme === 'dark' ? 'light' : currentTheme === 'light' ? 'system' : 'dark';
		theme.setTheme(next);
	}

	async function handleLogout() {
		try {
			await api.logout();
			window.location.href = '/login';
		} catch (err: any) {
			toast.error(err.message || 'Logout failed');
		}
	}
</script>

<header
	class="h-16 border-b border-zinc-200 dark:border-zinc-800 bg-white/80 dark:bg-zinc-900/80 backdrop-blur-md sticky top-0 z-30 px-4 sm:px-6 flex items-center justify-between"
>
	<div class="flex items-center space-x-3">
		<button
			type="button"
			class="md:hidden p-2 rounded-lg text-zinc-600 dark:text-zinc-400 hover:bg-zinc-100 dark:hover:bg-zinc-800"
			onclick={() => onToggleSidebar?.()}
			aria-label="Toggle navigation menu"
		>
			<svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
				<path
					stroke-linecap="round"
					stroke-linejoin="round"
					stroke-width="2"
					d="M4 6h16M4 12h16M4 18h16"
				/>
			</svg>
		</button>

		<a href="/" class="flex items-center space-x-2.5 group">
			<div
				class="w-8 h-8 rounded-lg bg-indigo-600 flex items-center justify-center text-white font-bold text-lg shadow-sm group-hover:bg-indigo-500 transition-colors"
			>
				<svg
					class="w-5 h-5 text-white"
					viewBox="0 0 24 24"
					fill="none"
					stroke="currentColor"
					stroke-width="2.5"
					stroke-linecap="round"
					stroke-linejoin="round"
				>
					<path d="M4 18h16M7 14l5-9 5 9M12 5v9" />
				</svg>
			</div>
			<div class="flex flex-col">
				<span
					class="font-bold text-base leading-tight tracking-tight text-zinc-900 dark:text-zinc-100"
				>
					dermaga
				</span>
				<span class="text-[10px] text-zinc-500 dark:text-zinc-400 -mt-0.5 font-mono">
					{status?.version || 'v0.1.0'}
				</span>
			</div>
		</a>

		{#if status}
			<div
				class="hidden lg:flex items-center space-x-2 ml-4 pl-4 border-l border-zinc-200 dark:border-zinc-800 text-xs font-mono text-zinc-500 dark:text-zinc-400"
			>
				<span class="inline-flex items-center">
					<span class="w-2 h-2 rounded-full bg-emerald-500 mr-1.5 animate-pulse"></span>
					PID {status.pid}
				</span>
				<span>•</span>
				<span>RSS {(status.rssKB / 1024).toFixed(1)} MB</span>
			</div>
		{/if}
	</div>

	<div class="flex items-center space-x-2 sm:space-x-3">
		<!-- Keyboard shortcuts trigger -->
		<button
			type="button"
			class="hidden sm:flex items-center space-x-1.5 px-2.5 py-1.5 text-xs text-zinc-600 dark:text-zinc-400 hover:text-zinc-900 dark:hover:text-zinc-100 rounded-lg hover:bg-zinc-100 dark:hover:bg-zinc-800 transition-colors"
			onclick={() => onOpenShortcuts?.()}
			title="Keyboard shortcuts (?)"
		>
			<svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
				<path
					stroke-linecap="round"
					stroke-linejoin="round"
					stroke-width="2"
					d="M12 6V4m0 2a2 2 0 100 4m0-4a2 2 0 110 4m-6 8a2 2 0 100-4m0 4a2 2 0 110-4m0 4v2m0-6V4m6 6v10m6-2a2 2 0 100-4m0 4a2 2 0 110-4m0 4v2m0-6V4"
				/>
			</svg>
			<kbd
				class="px-1.5 py-0.5 text-[10px] bg-zinc-200/80 dark:bg-zinc-800 rounded font-mono border border-zinc-300 dark:border-zinc-700"
				>?</kbd
			>
		</button>

		<!-- Theme toggle -->
		<button
			type="button"
			class="p-2 rounded-lg text-zinc-600 dark:text-zinc-400 hover:text-zinc-900 dark:hover:text-zinc-100 hover:bg-zinc-100 dark:hover:bg-zinc-800 transition-colors"
			onclick={cycleTheme}
			title="Theme: {currentTheme} (press 't' to toggle)"
			aria-label="Toggle theme"
		>
			{#if currentTheme === 'dark'}
				<svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z"
					/>
				</svg>
			{:else if currentTheme === 'light'}
				<svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M12 3v1m0 16v1m9-9h-1M4 9h-1m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z"
					/>
				</svg>
			{:else}
				<svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z"
					/>
				</svg>
			{/if}
		</button>

		<!-- Logout -->
		<button
			type="button"
			class="flex items-center space-x-1 px-3 py-1.5 text-xs font-medium text-rose-600 dark:text-rose-400 hover:bg-rose-50 dark:hover:bg-rose-950/40 rounded-lg transition-colors border border-rose-200 dark:border-rose-900/50"
			onclick={handleLogout}
			title="Log out of panel"
		>
			<svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
				<path
					stroke-linecap="round"
					stroke-linejoin="round"
					stroke-width="2"
					d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1"
				/>
			</svg>
			<span>Logout</span>
		</button>
	</div>
</header>
