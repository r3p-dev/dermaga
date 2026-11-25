<script lang="ts">
	import '../app.css';
	import { page } from '$app/state';
	import { getSession, logout } from '#lib/api.js';
	import type { SessionInfo } from '#lib/types/api.js';
	import { onMount } from 'svelte';

	let { children } = $props();

	let session = $state<SessionInfo | null>(null);
	let loading = $state(true);
	let isDark = $state(false);

	async function refreshSession() {
		try {
			session = await getSession();
			if (session.setup_required && page.url.pathname !== '/setup') {
				window.location.href = '/setup';
				return;
			}
		} catch {
			session = null;
		} finally {
			loading = false;
		}
	}

	function toggleTheme() {
		isDark = !isDark;
		if (isDark) {
			document.documentElement.classList.add('dark');
			localStorage.setItem('theme', 'dark');
		} else {
			document.documentElement.classList.remove('dark');
			localStorage.setItem('theme', 'light');
		}
	}

	async function handleLogout() {
		try {
			await logout();
			session = null;
			window.location.href = '/login';
		} catch (err) {
			console.error('Logout error:', err);
		}
	}

	onMount(() => {
		isDark = document.documentElement.classList.contains('dark');

		const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
		const handleMediaChange = (e: MediaQueryListEvent) => {
			if (!localStorage.getItem('theme')) {
				isDark = e.matches;
				if (e.matches) {
					document.documentElement.classList.add('dark');
				} else {
					document.documentElement.classList.remove('dark');
				}
			}
		};
		mediaQuery.addEventListener('change', handleMediaChange);

		refreshSession();

		return () => mediaQuery.removeEventListener('change', handleMediaChange);
	});
</script>

<div class="min-h-screen flex flex-col font-sans bg-stone-50 dark:bg-stone-950 text-stone-900 dark:text-stone-100">
	<header class="border-b border-stone-200 dark:border-stone-800 bg-white dark:bg-stone-900 sticky top-0 z-40">
		<div class="max-w-6xl mx-auto px-4 h-14 flex items-center justify-between">
			<div class="flex items-center gap-6">
				<a href="/" class="flex items-center gap-2.5 font-semibold text-lg tracking-tight hover:opacity-80 transition">
					<img src="/logo.svg" alt="dermaga" class="w-7 h-7 object-contain" />
					<span>dermaga</span>
				</a>

				{#if session?.authenticated}
					<nav class="hidden md:flex items-center gap-1 text-sm font-medium">
						<a
							href="/"
							class="px-3 py-1.5 rounded-md transition {page.url.pathname === '/' ? 'bg-stone-100 dark:bg-stone-800 text-stone-900 dark:text-stone-100' : 'text-stone-600 dark:text-stone-400 hover:text-stone-900 dark:hover:text-stone-100'}"
						>
							Dashboard
						</a>
						<a
							href="/audit-logs"
							class="px-3 py-1.5 rounded-md transition {page.url.pathname === '/audit-logs' ? 'bg-stone-100 dark:bg-stone-800 text-stone-900 dark:text-stone-100' : 'text-stone-600 dark:text-stone-400 hover:text-stone-900 dark:hover:text-stone-100'}"
						>
							Audit Log
						</a>
						<a
							href="/settings"
							class="px-3 py-1.5 rounded-md transition {page.url.pathname === '/settings' ? 'bg-stone-100 dark:bg-stone-800 text-stone-900 dark:text-stone-100' : 'text-stone-600 dark:text-stone-400 hover:text-stone-900 dark:hover:text-stone-100'}"
						>
							Settings
						</a>
					</nav>
				{/if}
			</div>

			<div class="flex items-center gap-3">
				<button
					type="button"
					onclick={toggleTheme}
					aria-label="Toggle theme"
					class="p-2 rounded-md text-stone-600 dark:text-stone-400 hover:bg-stone-100 dark:hover:bg-stone-800 transition"
				>
					{#if isDark}
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z" />
						</svg>
					{:else}
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z" />
						</svg>
					{/if}
				</button>

				{#if session?.authenticated}
					<button
						type="button"
						onclick={handleLogout}
						class="text-xs font-medium px-3 py-1.5 rounded-md border border-stone-200 dark:border-stone-700 hover:bg-stone-100 dark:hover:bg-stone-800 transition"
					>
						Log out
					</button>
				{/if}
			</div>
		</div>

		{#if session?.authenticated}
			<nav class="md:hidden flex items-center gap-1 px-4 py-2 border-t border-stone-200 dark:border-stone-800 text-sm font-medium">
				<a
					href="/"
					class="px-3 py-1.5 rounded-md transition {page.url.pathname === '/' ? 'bg-stone-100 dark:bg-stone-800 text-stone-900 dark:text-stone-100' : 'text-stone-600 dark:text-stone-400'}"
				>
					Dashboard
				</a>
				<a
					href="/audit-logs"
					class="px-3 py-1.5 rounded-md transition {page.url.pathname === '/audit-logs' ? 'bg-stone-100 dark:bg-stone-800 text-stone-900 dark:text-stone-100' : 'text-stone-600 dark:text-stone-400'}"
				>
					Audit Log
				</a>
				<a
					href="/settings"
					class="px-3 py-1.5 rounded-md transition {page.url.pathname === '/settings' ? 'bg-stone-100 dark:bg-stone-800 text-stone-900 dark:text-stone-100' : 'text-stone-600 dark:text-stone-400'}"
				>
					Settings
				</a>
			</nav>
		{/if}
	</header>

	<main class="flex-1 max-w-6xl w-full mx-auto p-4 md:p-6">
		{#if loading}
			<div class="flex items-center justify-center h-64 text-stone-500 dark:text-stone-400 text-sm">
				Loading dermaga...
			</div>
		{:else}
			{#if session?.authenticated}
				<div class="space-y-3 mb-5">
					{#if !session.totp_enabled}
						<div role="alert" class="p-3.5 rounded-lg bg-amber-50 dark:bg-amber-950/40 border border-amber-300 dark:border-amber-800 text-amber-900 dark:text-amber-200 text-xs font-medium">
							Two-factor authentication is disabled. Enable TOTP in Settings to protect your server.
						</div>
					{/if}
					{#if session.setup_state === 'admin_created'}
						<div role="status" class="p-3.5 rounded-lg bg-stone-100 dark:bg-stone-900 border border-stone-300 dark:border-stone-700 text-stone-800 dark:text-stone-200 text-xs flex flex-wrap items-center justify-between gap-2">
							<span>Setup incomplete: hook domain is not configured yet (required before connecting GitHub).</span>
							<a href="/settings?tab=hook-domain" class="font-medium text-brand-text hover:underline">Configure hook domain</a>
						</div>
					{/if}
				</div>
			{/if}
			{@render children()}
		{/if}
	</main>

	<footer class="border-t border-stone-200 dark:border-stone-800 py-4 text-center text-xs text-stone-500 dark:text-stone-400">
		dermaga &bull; stateless rootless deployment platform
	</footer>
</div>
