<script lang="ts">
	import { getSession, getServerStatus, listApps } from '#lib/api.js';
	import AppsList from '#lib/components/AppsList.svelte';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import StatusBadge from '#lib/components/StatusBadge.svelte';
	import type { AppSummary, ServerStatus, SessionInfo } from '#lib/types/api.js';
	import { onMount } from 'svelte';

	let session = $state<SessionInfo | null>(null);
	let status = $state<ServerStatus | null>(null);
	let apps = $state<AppSummary[]>([]);
	let loading = $state(true);
	let appsLoading = $state(true);
	let error = $state<string | null>(null);
	let appsError = $state<string | null>(null);

	function formatUptime(seconds: number): string {
		const days = Math.floor(seconds / 86400);
		const hours = Math.floor((seconds % 86400) / 3600);
		const minutes = Math.floor((seconds % 3600) / 60);
		const secs = seconds % 60;

		const parts = [];
		if (days > 0) parts.push(`${days}d`);
		if (hours > 0) parts.push(`${hours}h`);
		if (minutes > 0) parts.push(`${minutes}m`);
		parts.push(`${secs}s`);
		return parts.join(' ');
	}

	function formatBytes(bytes?: number): string {
		if (!bytes) return '0 MB';
		const mb = bytes / (1024 * 1024);
		return `${mb.toFixed(1)} MB`;
	}

	onMount(async () => {
		try {
			session = await getSession();
			if (session.setup_required) {
				window.location.href = '/setup';
				return;
			}
			if (!session.authenticated) {
				window.location.href = '/login';
				return;
			}
			status = await getServerStatus();
			try {
				const appRes = await listApps();
				apps = appRes.apps || [];
			} catch (appErr) {
				appsError = appErr instanceof Error ? appErr.message : 'Failed to load apps';
			} finally {
				appsLoading = false;
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load dashboard';
		} finally {
			loading = false;
			appsLoading = false;
		}
	});
</script>

<svelte:head>
	<title>Dashboard &bull; dermaga</title>
</svelte:head>

<div class="space-y-6">
	<PageHeader title="Server Overview" subtitle="Applications, host health, and daemon metrics">
		{#snippet actions()}
			{#if status}
				<StatusBadge label="Daemon Active ({status.version})" pulse={true} />
			{/if}
		{/snippet}
	</PageHeader>

	{#if error}
		<div role="alert" class="p-4 rounded-xl bg-red-50 dark:bg-red-950/40 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-300 text-sm">
			{error}
		</div>
	{:else if loading}
		<div class="space-y-4">
			<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
				{#each [1, 2] as _}
					<div class="h-32 rounded-xl bg-stone-100 dark:bg-stone-800/50 animate-pulse"></div>
				{/each}
			</div>
			<div class="grid grid-cols-2 lg:grid-cols-4 gap-3">
				{#each [1, 2, 3, 4] as _}
					<div class="h-20 rounded-xl bg-stone-100 dark:bg-stone-800/50 animate-pulse"></div>
				{/each}
			</div>
		</div>
	{:else}
		{#if session?.authenticated}
			<AppsList {apps} loading={appsLoading} error={appsError} />
		{/if}

		{#if status}
			<!-- Compact 4-stat host health strip -->
			<section aria-label="Host metrics" class="space-y-2.5 pt-2">
				<h2 class="text-xs font-semibold uppercase tracking-wider text-stone-600 dark:text-stone-400">
					Host Health
				</h2>
				<div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
					<div class="bg-white dark:bg-stone-900 border border-stone-200 dark:border-stone-800 rounded-xl p-4 shadow-sm">
						<span class="text-xs font-medium text-stone-600 dark:text-stone-400 block">Daemon</span>
						<div class="mt-1 flex items-baseline justify-between gap-2">
							<span class="text-base font-bold text-emerald-700 dark:text-emerald-400">Running</span>
							<span class="text-xs font-mono tabular-nums text-stone-600 dark:text-stone-400">{formatUptime(status.uptime_sec)}</span>
						</div>
					</div>

					<div class="bg-white dark:bg-stone-900 border border-stone-200 dark:border-stone-800 rounded-xl p-4 shadow-sm">
						<span class="text-xs font-medium text-stone-600 dark:text-stone-400 block">Footprint (NFR-2)</span>
						<div class="mt-1 flex items-baseline justify-between gap-2">
							<span class="text-base font-bold tabular-nums">{formatBytes(status.daemon_rss)}</span>
							<span class="text-xs text-stone-600 dark:text-stone-400">Target &lt; 30 MB</span>
						</div>
					</div>

					<div class="bg-white dark:bg-stone-900 border border-stone-200 dark:border-stone-800 rounded-xl p-4 shadow-sm">
						<span class="text-xs font-medium text-stone-600 dark:text-stone-400 block">SQLite</span>
						<div class="mt-1 flex items-baseline justify-between gap-2">
							<span class="text-base font-bold text-brand-text tabular-nums">v{status.schema_version}</span>
							<span class="text-xs {status.database_ok ? 'text-emerald-700 dark:text-emerald-400' : 'text-red-700 dark:text-red-400'}">
								{status.database_ok ? 'Healthy' : 'Error'}
							</span>
						</div>
					</div>

					<div class="bg-white dark:bg-stone-900 border border-stone-200 dark:border-stone-800 rounded-xl p-4 shadow-sm">
						<span class="text-xs font-medium text-stone-600 dark:text-stone-400 block">Binary</span>
						<div class="mt-1 flex items-baseline justify-between gap-2">
							<span class="text-base font-bold font-mono">{status.version}</span>
							<span class="text-xs font-mono text-stone-600 dark:text-stone-400 truncate">{status.commit}</span>
						</div>
					</div>
				</div>
			</section>
		{/if}
	{/if}
</div>
