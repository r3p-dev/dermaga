<script lang="ts">
	import { getSession, syncGitHub } from '#lib/api.js'
	import { onMount } from 'svelte'

	let error = $state<string | null>(null)

	onMount(async () => {
		try {
			const session = await getSession()
			if (session.setup_required) {
				window.location.href = '/setup'
				return
			}
			if (!session.authenticated) {
				window.location.href = '/login'
				return
			}

			const res = await syncGitHub()
			if (res.error) {
				error = res.error
				return
			}
			window.location.href = '/settings?tab=github'
		} catch (err) {
			error =
				err instanceof Error
					? err.message
					: 'Failed to sync GitHub installations.'
		}
	})
</script>

<svelte:head>
	<title>Syncing GitHub &bull; dermaga</title>
</svelte:head>

<div
	class="mx-auto mt-12 max-w-lg space-y-4 rounded-xl border border-stone-200 bg-white p-6 shadow-sm dark:border-stone-800 dark:bg-stone-900"
>
	<h1 class="text-lg font-semibold">Syncing GitHub Repositories</h1>
	{#if error}
		<div
			role="alert"
			class="rounded-lg border border-red-200 bg-red-50 p-3 text-xs text-red-800 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300"
		>
			{error}
		</div>
		<a
			href="/settings?tab=github"
			class="inline-block rounded-md bg-brand-primary px-3 py-1.5 text-xs font-medium text-brand-btn-text"
		>
			Back to Settings &rarr; GitHub
		</a>
	{:else}
		<p class="text-sm text-stone-600 dark:text-stone-400">
			Refreshing installations and repositories from GitHub...
		</p>
	{/if}
</div>
