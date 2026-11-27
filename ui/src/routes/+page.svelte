<script lang="ts">
	import { getSession, getServerStatus, listApps } from '#lib/api.js'
	import AppsList from '#lib/components/AppsList.svelte'
	import PageHeader from '#lib/components/PageHeader.svelte'
	import StatusBadge from '#lib/components/StatusBadge.svelte'
	import type { AppSummary, ServerStatus, SessionInfo } from '#lib/types/api.js'
	import { onMount } from 'svelte'

	let session = $state<SessionInfo | null>(null)
	let status = $state<ServerStatus | null>(null)
	let apps = $state<AppSummary[]>([])
	let loading = $state(true)
	let appsLoading = $state(true)
	let error = $state<string | null>(null)
	let appsError = $state<string | null>(null)

	function formatUptime(seconds: number): string {
		const days = Math.floor(seconds / 86400)
		const hours = Math.floor((seconds % 86400) / 3600)
		const minutes = Math.floor((seconds % 3600) / 60)
		const secs = seconds % 60

		const parts = []
		if (days > 0) parts.push(`${days}d`)
		if (hours > 0) parts.push(`${hours}h`)
		if (minutes > 0) parts.push(`${minutes}m`)
		parts.push(`${secs}s`)
		return parts.join(' ')
	}

	// Idle memory budget for the daemon; above it the value turns amber.
	const daemonMemoryTarget = 30 * 1024 * 1024

	function formatBytes(bytes?: number): string {
		if (!bytes) return '0 MB'
		const mb = bytes / (1024 * 1024)
		return `${mb.toFixed(1)} MB`
	}

	onMount(async () => {
		try {
			session = await getSession()
			if (session.setup_required) {
				window.location.href = '/setup'
				return
			}
			if (!session.authenticated) {
				window.location.href = '/login'
				return
			}
			status = await getServerStatus()
			try {
				const appRes = await listApps()
				apps = appRes.apps || []
			} catch (appErr) {
				appsError =
					appErr instanceof Error ? appErr.message : 'Failed to load apps'
			} finally {
				appsLoading = false
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load dashboard'
		} finally {
			loading = false
			appsLoading = false
		}
	})
</script>

<svelte:head>
	<title>Dashboard &bull; dermaga</title>
</svelte:head>

<div class="space-y-6">
	<PageHeader
		title="Server Overview"
		subtitle="Your server and the apps running on it"
	>
		{#snippet actions()}
			{#if status}
				<StatusBadge label="Daemon Active ({status.version})" pulse={true} />
			{/if}
		{/snippet}
	</PageHeader>

	{#if error}
		<div
			role="alert"
			class="rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300"
		>
			{error}
		</div>
	{:else if loading}
		<div class="space-y-4">
			<div class="grid grid-cols-1 gap-4 md:grid-cols-2">
				{#each [1, 2] as _}
					<div
						class="h-32 animate-pulse rounded-xl bg-stone-100 dark:bg-stone-800/50"
					></div>
				{/each}
			</div>
			<div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
				{#each [1, 2, 3, 4] as _}
					<div
						class="h-20 animate-pulse rounded-xl bg-stone-100 dark:bg-stone-800/50"
					></div>
				{/each}
			</div>
		</div>
	{:else}
		{#if status?.certificate_warnings?.length}
			<div
				role="alert"
				class="space-y-1 rounded-xl border border-amber-300 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-200"
			>
				{#each status.certificate_warnings as w}
					<p>{w}</p>
				{/each}
				<p class="text-xs">
					Check the domain's DNS and that port 80 is reachable; the app's
					Domains tab shows each domain's status.
				</p>
			</div>
		{/if}
		{#if status}
			<!-- Server overview: only values the API returns -->
			<section aria-label="Server overview" class="space-y-2.5 pt-2">
				<h2
					class="text-xs font-semibold tracking-wider text-stone-600 uppercase dark:text-stone-400"
				>
					Server
				</h2>
				<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
					<div
						class="rounded-xl border border-stone-200 bg-white p-4 shadow-sm dark:border-stone-800 dark:bg-stone-900"
					>
						<span
							class="block text-xs font-medium text-stone-600 dark:text-stone-400"
						>
							Status
						</span>
						<div class="mt-1 flex items-baseline justify-between gap-2">
							<span
								class="text-base font-bold {status.database_ok
									? 'text-emerald-700 dark:text-emerald-400'
									: 'text-red-700 dark:text-red-400'}"
							>
								{status.database_ok ? 'Running' : 'Database error'}
							</span>
							<span
								class="font-mono text-xs text-stone-600 tabular-nums dark:text-stone-400"
							>
								Up {formatUptime(status.uptime_sec)}
							</span>
						</div>
						<span
							class="mt-0.5 block text-xs text-stone-600 dark:text-stone-400"
							title="Database schema version {status.schema_version}"
						>
							Database: {status.database_ok ? 'healthy' : 'error'}
						</span>
					</div>

					<div
						class="rounded-xl border border-stone-200 bg-white p-4 shadow-sm dark:border-stone-800 dark:bg-stone-900"
					>
						<span
							class="block text-xs font-medium text-stone-600 dark:text-stone-400"
						>
							Memory
						</span>
						<div class="mt-1 flex items-baseline justify-between gap-2">
							<span
								class="text-base font-bold tabular-nums {(status.daemon_rss ??
									0) > daemonMemoryTarget
									? 'text-amber-700 dark:text-amber-400'
									: ''}"
								title="Expected under 30 MB when idle"
							>
								{formatBytes(status.daemon_rss)}
							</span>
							<span class="text-xs text-stone-600 dark:text-stone-400">
								Used by dermaga
							</span>
						</div>
					</div>

					<div
						class="rounded-xl border border-stone-200 bg-white p-4 shadow-sm dark:border-stone-800 dark:bg-stone-900"
					>
						<span
							class="block text-xs font-medium text-stone-600 dark:text-stone-400"
						>
							Apps
						</span>
						<div class="mt-1 flex items-baseline justify-between gap-2">
							<span class="text-base font-bold tabular-nums">
								{appsLoading
									? 'Loading'
									: appsError
										? 'Unavailable'
										: apps.length}
							</span>
							<span class="text-xs text-stone-600 dark:text-stone-400">
								on this server
							</span>
						</div>
					</div>

					<div
						class="rounded-xl border border-stone-200 bg-white p-4 shadow-sm dark:border-stone-800 dark:bg-stone-900"
					>
						<span
							class="block text-xs font-medium text-stone-600 dark:text-stone-400"
						>
							Version
						</span>
						<div class="mt-1 flex items-baseline justify-between gap-2">
							<span
								class="min-w-0 truncate font-mono text-base font-bold"
								title={status.version}
							>
								{status.version}
							</span>
							<span
								class="shrink-0 font-mono text-xs text-stone-600 dark:text-stone-400"
							>
								{status.commit}
							</span>
						</div>
					</div>
				</div>
			</section>
		{/if}

		{#if session?.authenticated}
			<AppsList {apps} loading={appsLoading} error={appsError} />
		{/if}
	{/if}
</div>
