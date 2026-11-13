<script lang="ts">
	import './layout.css';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { theme, type Theme } from '$lib/stores/theme';
	import { toast } from '$lib/stores/toast';
	import { api } from '$lib/api/client';
	import type { Status } from '$lib/types/api';
	import Navbar from '$lib/components/Navbar.svelte';
	import Sidebar from '$lib/components/Sidebar.svelte';
	import ToastContainer from '$lib/components/ToastContainer.svelte';
	import KeyboardHelp from '$lib/components/KeyboardHelp.svelte';
	import type { LayoutProps } from './$types';

	let { children }: LayoutProps = $props();

	let status = $state<Status | null>(null);
	let sidebarOpen = $state(false);
	let keyboardHelpOpen = $state(false);
	let isLoginPage = $derived(page.url.pathname === '/login');

	// Key sequence buffer for two-stroke shortcuts (e.g. "g h", "g a")
	let keyBuffer = '';
	let bufferTimer: any = null;

	onMount(() => {
		theme.init();

		if (!isLoginPage) {
			loadStatus();
		}

		window.addEventListener('keydown', handleKeydown);
		// Pages that change what the status reports (the backup password) ask for a reload.
		window.addEventListener('dermaga:status', loadStatus);
		return () => {
			window.removeEventListener('keydown', handleKeydown);
			window.removeEventListener('dermaga:status', loadStatus);
		};
	});

	async function loadStatus() {
		try {
			status = await api.getStatus();
		} catch {
			// Ignore 401s handled by client redirect
		}
	}

	function handleKeydown(e: KeyboardEvent) {
		// Ignore shortcuts if the user is typing in an input, textarea, or contentEditable
		const target = e.target as HTMLElement;
		if (
			target &&
			(target.tagName === 'INPUT' ||
				target.tagName === 'TEXTAREA' ||
				target.tagName === 'SELECT' ||
				target.isContentEditable)
		) {
			return;
		}

		if (e.key === '?') {
			e.preventDefault();
			keyboardHelpOpen = !keyboardHelpOpen;
			return;
		}

		if (e.key === 'Escape') {
			keyboardHelpOpen = false;
			sidebarOpen = false;
			return;
		}

		if (e.key === 't' || e.key === 'T') {
			e.preventDefault();
			const current = (localStorage.getItem('dermaga_theme') as Theme) || 'system';
			const next: Theme = current === 'dark' ? 'light' : current === 'light' ? 'system' : 'dark';
			theme.setTheme(next);
			toast.info(`Theme set to ${next}`);
			return;
		}

		clearTimeout(bufferTimer);
		keyBuffer += e.key.toLowerCase();
		bufferTimer = setTimeout(() => {
			keyBuffer = '';
		}, 600);

		if (keyBuffer === 'gh') {
			keyBuffer = '';
			goto('/');
		} else if (keyBuffer === 'ga') {
			keyBuffer = '';
			goto('/');
		} else if (keyBuffer === 'gd') {
			keyBuffer = '';
			goto('/databases');
		} else if (keyBuffer === 'gb') {
			keyBuffer = '';
			goto('/backups');
		} else if (keyBuffer === 'gs') {
			keyBuffer = '';
			goto('/settings');
		} else if (keyBuffer === 'gl') {
			keyBuffer = '';
			goto('/audit');
		} else if (keyBuffer === 'na') {
			keyBuffer = '';
			goto('/apps/new');
		} else if (keyBuffer === 'nd') {
			keyBuffer = '';
			goto('/databases');
		}
	}
</script>

<svelte:head>
	<title>dermaga - Personal Cloud PaaS</title>
</svelte:head>

<ToastContainer />
<KeyboardHelp bind:open={keyboardHelpOpen} />

{#if isLoginPage}
	<main class="min-h-screen flex items-center justify-center p-4">
		{@render children()}
	</main>
{:else}
	<div
		class="min-h-screen flex flex-col bg-zinc-50 dark:bg-zinc-950 text-zinc-900 dark:text-zinc-100"
	>
		<Navbar
			{status}
			onOpenShortcuts={() => (keyboardHelpOpen = true)}
			onToggleSidebar={() => (sidebarOpen = !sidebarOpen)}
		/>

		<div class="flex-1 flex max-w-7xl w-full mx-auto">
			<Sidebar open={sidebarOpen} onclose={() => (sidebarOpen = false)} />

			<main class="flex-1 p-4 sm:p-6 lg:p-8 min-w-0">
				{#if status?.backupPasswordState === 'unconfirmed' || status?.backupPasswordState === 'none'}
					<a
						href="/backups"
						class="mb-4 block rounded-lg border border-amber-200 dark:border-amber-900 bg-amber-50 dark:bg-amber-950/60 px-4 py-2.5 text-xs text-amber-900 dark:text-amber-100"
					>
						<span class="font-bold">Backups:</span>
						{#if status.backupPasswordState === 'none'}
							no restic password yet, so backups fail. Set it up on the Backups page.
						{:else}
							the restic password has not been confirmed as stored offline. Without it the backups
							cannot be restored. Reveal and confirm it on the Backups page.
						{/if}
					</a>
				{/if}
				{@render children()}
			</main>
		</div>
	</div>
{/if}
