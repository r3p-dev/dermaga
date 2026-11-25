<script lang="ts">
	import { getSession, syncGitHub } from '#lib/api.js';
	import { onMount } from 'svelte';

	let error = $state<string | null>(null);

	onMount(async () => {
		try {
			const session = await getSession();
			if (session.setup_required) {
				window.location.href = '/setup';
				return;
			}
			if (!session.authenticated) {
				window.location.href = '/login';
				return;
			}

			const res = await syncGitHub();
			if (res.error) {
				error = res.error;
				return;
			}
			window.location.href = '/settings?tab=github';
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to sync GitHub installations.';
		}
	});
</script>

<svelte:head>
	<title>Syncing GitHub &bull; dermaga</title>
</svelte:head>

<div class="max-w-lg mx-auto mt-12 bg-white dark:bg-stone-900 border border-stone-200 dark:border-stone-800 rounded-xl p-6 shadow-sm space-y-4">
	<h1 class="text-lg font-semibold">Syncing GitHub Repositories</h1>
	{#if error}
		<div role="alert" class="p-3 rounded-lg bg-red-50 dark:bg-red-950/40 border border-red-200 dark:border-red-800 text-red-800 dark:text-red-300 text-xs">
			{error}
		</div>
		<a href="/settings?tab=github" class="inline-block px-3 py-1.5 rounded-md bg-brand-primary text-brand-btn-text text-xs font-medium">
			Back to Settings &rarr; GitHub
		</a>
	{:else}
		<p class="text-sm text-stone-600 dark:text-stone-400">Refreshing installations and repositories from GitHub...</p>
	{/if}
</div>
