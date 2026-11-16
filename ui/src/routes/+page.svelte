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

	function formatVersion(ver?: string): string {
		if (!ver) return 'v0.0.0';
		return ver.startsWith('v') ? ver : `v${ver}`;
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
				Daemon Active ({formatVersion(status.version)})
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
					<span class="text-xl font-bold text-teal-600 dark:text-teal-400">v{status.schema_version}</span>
					<span class="text-xs text-stone-500">WAL mode</span>
				</div>
				<span class="text-xs text-stone-400 mt-2 block">0600 mode encrypted</span>
			</div>

			<!-- Build Info -->
			<div class="bg-white dark:bg-stone-900 border border-stone-200 dark:border-stone-800 rounded-xl p-5 shadow-sm">
				<span class="text-xs font-medium text-stone-500 block mb-1">Binary Version</span>
				<div class="flex items-baseline gap-2">
					<span class="text-xl font-bold font-mono">{formatVersion(status.version)}</span>
				</div>
				<span class="text-xs text-stone-400 mt-2 block truncate">Commit: {status.commit}</span>
			</div>
		</div>

		<!-- Apps Overview placeholder for M2+ -->
		<div class="bg-white dark:bg-stone-900 border border-stone-200 dark:border-stone-800 rounded-xl p-6 shadow-sm">
			<div class="flex items-center justify-between mb-4">
				<h2 class="text-base font-semibold">Configured Applications</h2>
				<span class="text-xs font-mono px-2 py-0.5 rounded bg-stone-100 dark:bg-stone-800 text-stone-600 dark:text-stone-400">
					M1 Foundation
				</span>
			</div>
			<div class="text-center py-8 text-stone-500">
				<p class="text-sm font-medium">No applications deployed yet</p>
				<p class="text-xs mt-1">Application creation and generators will be available in milestone M2.</p>
			</div>
		</div>
	{/if}
</div>
