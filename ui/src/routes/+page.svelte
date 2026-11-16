<script lang="ts">
	import { getSession, getServerStatus } from '#lib/api.js';
	import type { ServerStatus, SessionInfo } from '#lib/types/api.js';
	import { onMount } from 'svelte';

	let session = $state<SessionInfo | null>(null);
	let status = $state<ServerStatus | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);

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
			if (!session.authenticated) {
				window.location.href = '/login';
				return;
			}
			status = await getServerStatus();
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load dashboard';
		} finally {
			loading = false;
		}
	});
</script>

<svelte:head>
	<title>Dashboard &bull; dermaga</title>
</svelte:head>

<div class="space-y-6">
	<div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pb-4 border-b border-stone-200 dark:border-stone-800">
		<div>
			<h1 class="text-2xl font-bold tracking-tight">Server Overview</h1>
			<p class="text-xs text-stone-500 mt-1">Host health, daemon status, and system metrics</p>
		</div>

		{#if status}
			<div class="inline-flex items-center gap-2 px-3 py-1.5 rounded-full text-xs font-medium bg-emerald-50 dark:bg-emerald-950/40 text-emerald-700 dark:text-emerald-300 border border-emerald-200 dark:border-emerald-800 w-fit">
				<span class="w-2 h-2 rounded-full bg-emerald-500 animate-pulse"></span>
				Daemon Active ({status.version})
			</div>
		{/if}
	</div>

	{#if error}
		<div role="alert" class="p-4 rounded-lg bg-red-50 dark:bg-red-950/40 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-300 text-sm">
			{error}
		</div>
	{:else if loading}
		<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
			{#each [1, 2, 3, 4] as _}
				<div class="h-28 rounded-xl bg-stone-100 dark:bg-stone-800/50 animate-pulse"></div>
			{/each}
		</div>
	{:else if status}
		<div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
			<!-- Daemon Health -->
			<div class="bg-white dark:bg-stone-900 border border-stone-200 dark:border-stone-800 rounded-xl p-5 shadow-sm">
				<span class="text-xs font-medium text-stone-500 block mb-1">Daemon Status</span>
				<div class="flex items-baseline gap-2">
					<span class="text-xl font-bold text-emerald-600 dark:text-emerald-400">Running</span>
				</div>
				<span class="text-xs text-stone-400 mt-2 block">Uptime: {formatUptime(status.uptime_sec)}</span>
			</div>

			<!-- Memory Footprint -->
			<div class="bg-white dark:bg-stone-900 border border-stone-200 dark:border-stone-800 rounded-xl p-5 shadow-sm">
				<span class="text-xs font-medium text-stone-500 block mb-1">Daemon Footprint (NFR-2)</span>
				<div class="flex items-baseline gap-2">
					<span class="text-xl font-bold">{formatBytes(status.daemon_rss)}</span>
					<span class="text-xs text-stone-500">RSS</span>
				</div>
				<span class="text-xs text-stone-400 mt-2 block">Target &lt; 30 MB idle</span>
			</div>

			<!-- Database & State -->
			<div class="bg-white dark:bg-stone-900 border border-stone-200 dark:border-stone-800 rounded-xl p-5 shadow-sm">
				<span class="text-xs font-medium text-stone-500 block mb-1">SQLite Database</span>
				<div class="flex items-baseline gap-2">
					<span class="text-xl font-bold text-teal-600 dark:text-teal-400">Schema v{status.schema_version}</span>
				</div>
				<span class="text-xs {status.database_ok ? 'text-emerald-500' : 'text-red-500'} mt-2 block">
					{status.database_ok ? 'Database Healthy' : 'Database Error'}
				</span>
			</div>

			<!-- Build Info -->
			<div class="bg-white dark:bg-stone-900 border border-stone-200 dark:border-stone-800 rounded-xl p-5 shadow-sm">
				<span class="text-xs font-medium text-stone-500 block mb-1">Binary Version</span>
				<div class="flex items-baseline gap-2">
					<span class="text-xl font-bold font-mono">{status.version}</span>
				</div>
				<span class="text-xs text-stone-400 mt-2 block truncate">Commit: {status.commit}</span>
			</div>
		</div>
	{/if}
</div>
