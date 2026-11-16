<script lang="ts">
	import { getSession, getAuditLogs } from '#lib/api.js';
	import type { AuditLogEntry, SessionInfo } from '#lib/types/api.js';
	import { onMount } from 'svelte';

	let session = $state<SessionInfo | null>(null);
	let logs = $state<AuditLogEntry[]>([]);
	let selectedOrigin = $state('');
	let selectedApp = $state('');
	let selectedTime = $state('');
	let loading = $state(true);
	let error = $state<string | null>(null);

	function calculateSince(timeRange: string): string | undefined {
		const now = Date.now();
		switch (timeRange) {
			case '1h':
				return new Date(now - 3600 * 1000).toISOString();
			case '24h':
				return new Date(now - 24 * 3600 * 1000).toISOString();
			case '7d':
				return new Date(now - 7 * 24 * 3600 * 1000).toISOString();
			default:
				return undefined;
		}
	}

	async function loadLogs() {
		loading = true;
		error = null;
		try {
			const since = calculateSince(selectedTime);
			const res = await getAuditLogs(
				selectedOrigin || undefined,
				selectedApp.trim() || undefined,
				since
			);
			logs = res.logs || [];
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load audit logs';
		} finally {
			loading = false;
		}
	}

	function handleOriginChange(e: Event) {
		const target = e.target as HTMLSelectElement;
		selectedOrigin = target.value;
		loadLogs();
	}

	function handleAppInput(e: Event) {
		const target = e.target as HTMLInputElement;
		selectedApp = target.value;
		loadLogs();
	}

	function handleTimeChange(e: Event) {
		const target = e.target as HTMLSelectElement;
		selectedTime = target.value;
		loadLogs();
	}

	function getOriginBadge(origin: string): { bg: string; text: string } {
		switch (origin) {
			case 'panel':
				return { bg: 'bg-teal-50 dark:bg-teal-950/40 border-teal-200 dark:border-teal-800', text: 'text-teal-700 dark:text-teal-300' };
			case 'webhook':
				return { bg: 'bg-sky-50 dark:bg-sky-950/40 border-sky-200 dark:border-sky-800', text: 'text-sky-700 dark:text-sky-300' };
			case 'server_command':
				return { bg: 'bg-purple-50 dark:bg-purple-950/40 border-purple-200 dark:border-purple-800', text: 'text-purple-700 dark:text-purple-300' };
			case 'daemon':
				return { bg: 'bg-amber-50 dark:bg-amber-950/40 border-amber-200 dark:border-amber-800', text: 'text-amber-700 dark:text-amber-300' };
			default:
				return { bg: 'bg-stone-100 dark:bg-stone-800 border-stone-200 dark:border-stone-700', text: 'text-stone-700 dark:text-stone-300' };
		}
	}

	function formatDate(dateStr: string): string {
		try {
			const d = new Date(dateStr);
			return d.toLocaleString('en-US', {
				month: 'short',
				day: 'numeric',
				hour: '2-digit',
				minute: '2-digit',
				second: '2-digit',
				hour12: false
			});
		} catch {
			return dateStr;
		}
	}

	onMount(async () => {
		try {
			session = await getSession();
			if (!session.authenticated) {
				window.location.href = '/login';
				return;
			}
			await loadLogs();
		} catch (err) {
			error = err instanceof Error ? err.message : 'Error checking session';
			loading = false;
		}
	});
</script>

<svelte:head>
	<title>Audit Log &bull; dermaga</title>
</svelte:head>

<div class="space-y-6">
	<div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pb-4 border-b border-stone-200 dark:border-stone-800">
		<div>
			<h1 class="text-2xl font-bold tracking-tight">Audit Log</h1>
			<p class="text-xs text-stone-500 mt-1">Immutable record of mutating actions and security events (AUD-1, AUD-2)</p>
		</div>

		<div class="flex flex-wrap items-center gap-3">
			<div class="flex items-center gap-2">
				<label for="origin-filter" class="text-xs font-medium text-stone-500">Origin:</label>
				<select
					id="origin-filter"
					value={selectedOrigin}
					onchange={handleOriginChange}
					class="text-xs py-1.5 px-2.5 rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-900 focus:outline-none focus:ring-1 focus:ring-teal-500"
				>
					<option value="">All Origins</option>
					<option value="panel">panel</option>
					<option value="webhook">webhook</option>
					<option value="server_command">server_command</option>
					<option value="daemon">daemon</option>
				</select>
			</div>

			<div class="flex items-center gap-2">
				<label for="app-filter" class="text-xs font-medium text-stone-500">App:</label>
				<input
					id="app-filter"
					type="text"
					placeholder="Filter by app..."
					value={selectedApp}
					oninput={handleAppInput}
					class="text-xs py-1.5 px-2.5 rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-900 focus:outline-none focus:ring-1 focus:ring-teal-500 w-32"
				/>
			</div>

			<div class="flex items-center gap-2">
				<label for="time-filter" class="text-xs font-medium text-stone-500">Time:</label>
				<select
					id="time-filter"
					value={selectedTime}
					onchange={handleTimeChange}
					class="text-xs py-1.5 px-2.5 rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-900 focus:outline-none focus:ring-1 focus:ring-teal-500"
				>
					<option value="">All Time</option>
					<option value="1h">Last hour</option>
					<option value="24h">Last 24 hours</option>
					<option value="7d">Last 7 days</option>
				</select>
			</div>
		</div>
	</div>

	{#if error}
		<div role="alert" class="p-4 rounded-lg bg-red-50 dark:bg-red-950/40 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-300 text-sm">
			{error}
		</div>
	{:else if loading}
		<div class="space-y-2">
			{#each [1, 2, 3, 4, 5] as _}
				<div class="h-14 rounded-lg bg-stone-100 dark:bg-stone-800/50 animate-pulse"></div>
			{/each}
		</div>
	{:else if logs.length === 0}
		<div class="bg-white dark:bg-stone-900 border border-stone-200 dark:border-stone-800 rounded-xl p-8 text-center text-stone-500">
			<p class="text-sm font-medium">No audit log entries found</p>
			{#if selectedOrigin}
				<p class="text-xs mt-1">Try clearing the origin filter.</p>
			{/if}
		</div>
	{:else}
		<div class="bg-white dark:bg-stone-900 border border-stone-200 dark:border-stone-800 rounded-xl overflow-hidden shadow-sm">
			<div class="overflow-x-auto">
				<table class="w-full text-left text-xs border-collapse">
					<thead>
						<tr class="border-b border-stone-200 dark:border-stone-800 bg-stone-50/50 dark:bg-stone-950/50 text-stone-500">
							<th class="py-3 px-4 font-medium">Time</th>
							<th class="py-3 px-4 font-medium">Origin</th>
							<th class="py-3 px-4 font-medium">Event</th>
							<th class="py-3 px-4 font-medium">Actor</th>
							<th class="py-3 px-4 font-medium">Description</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-stone-100 dark:divide-stone-800/60 font-mono">
						{#each logs as log (log.id)}
							{@const badge = getOriginBadge(log.origin)}
							<tr class="hover:bg-stone-50/50 dark:hover:bg-stone-800/30 transition">
								<td class="py-3 px-4 text-stone-400 whitespace-nowrap">
									{formatDate(log.created_at)}
								</td>
								<td class="py-3 px-4 whitespace-nowrap">
									<span class="inline-flex px-2 py-0.5 rounded text-[11px] font-sans font-medium border {badge.bg} {badge.text}">
										{log.origin}
									</span>
								</td>
								<td class="py-3 px-4 font-semibold text-stone-800 dark:text-stone-200 whitespace-nowrap">
									{log.event}
								</td>
								<td class="py-3 px-4 text-stone-500 whitespace-nowrap">
									{log.actor}
								</td>
								<td class="py-3 px-4 text-stone-600 dark:text-stone-300 font-sans max-w-md truncate">
									{log.description}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		</div>
	{/if}
</div>
