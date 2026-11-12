<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api/client';
	import { toast } from '$lib/stores/toast';
	import type { Entry as AuditEntry } from '$lib/types/audit';

	let entries = $state<AuditEntry[]>([]);
	let loading = $state(true);
	let limit = $state(100);
	let searchFilter = $state('');

	onMount(async () => {
		await loadAudit();
	});

	async function loadAudit() {
		loading = true;
		try {
			entries = await api.getAudit(limit);
		} catch (err: any) {
			toast.error(err.message || 'Failed to load audit log');
		} finally {
			loading = false;
		}
	}

	const filteredEntries = $derived(
		entries.filter((e) => {
			const q = searchFilter.toLowerCase();
			return (
				e.action.toLowerCase().includes(q) ||
				e.actor.toLowerCase().includes(q) ||
				(e.target && e.target.toLowerCase().includes(q)) ||
				(e.details && e.details.toLowerCase().includes(q))
			);
		})
	);
</script>

<svelte:head>
	<title>Audit Log - dermaga</title>
</svelte:head>

<div class="space-y-6">
	<!-- Header -->
	<div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
		<div>
			<div class="flex items-center space-x-2 text-xs text-zinc-500 mb-1">
				<a href="/" class="hover:text-zinc-800 dark:hover:text-zinc-200">Dashboard</a>
				<span>/</span>
				<span>Audit Log</span>
			</div>
			<h1 class="text-2xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100">
				Platform Audit Trail
			</h1>
			<p class="text-xs text-zinc-500 dark:text-zinc-400 mt-0.5">
				Immutable chronological record of administrative actions, deployments, and authentication
				events.
			</p>
		</div>

		<div class="flex items-center space-x-2.5">
			<button
				type="button"
				onclick={loadAudit}
				aria-label="Refresh audit log"
				class="p-2 text-zinc-600 dark:text-zinc-400 hover:text-zinc-900 dark:hover:text-zinc-100 bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 rounded-lg shadow-2xs"
			>
				<svg
					class="w-4 h-4 {loading ? 'animate-spin' : ''}"
					fill="none"
					viewBox="0 0 24 24"
					stroke="currentColor"
				>
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
					/>
				</svg>
			</button>
		</div>
	</div>

	<!-- Audit Log Table Card -->
	<div
		class="bg-white dark:bg-zinc-900 rounded-2xl border border-zinc-200 dark:border-zinc-800 shadow-2xs overflow-hidden"
	>
		<div
			class="p-4 border-b border-zinc-200 dark:border-zinc-800 flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3"
		>
			<div class="relative w-full sm:w-64">
				<input
					type="text"
					bind:value={searchFilter}
					placeholder="Filter audit entries..."
					class="w-full px-3 py-1.5 pl-8 text-xs rounded-lg border border-zinc-300 dark:border-zinc-700 bg-zinc-50 dark:bg-zinc-950 text-zinc-900 dark:text-zinc-100 placeholder-zinc-400 focus:outline-none focus:ring-1 focus:ring-indigo-500"
				/>
				<svg
					class="w-3.5 h-3.5 text-zinc-400 absolute left-2.5 top-2.5"
					fill="none"
					viewBox="0 0 24 24"
					stroke="currentColor"
				>
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"
					/>
				</svg>
			</div>

			<div class="flex items-center space-x-2 text-xs">
				<span class="text-zinc-500">Show:</span>
				<select
					bind:value={limit}
					onchange={loadAudit}
					class="px-2.5 py-1.5 rounded-lg border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-900 text-xs"
				>
					<option value={50}>50 entries</option>
					<option value={100}>100 entries</option>
					<option value={250}>250 entries</option>
					<option value={500}>500 entries</option>
				</select>
			</div>
		</div>

		{#if filteredEntries.length === 0}
			<div class="text-center py-12 text-xs text-zinc-500">No audit entries found.</div>
		{:else}
			<div class="overflow-x-auto">
				<table class="w-full text-left text-xs font-mono">
					<thead
						class="bg-zinc-50 dark:bg-zinc-950/60 border-b border-zinc-200 dark:border-zinc-800 text-zinc-500 font-semibold uppercase tracking-wider text-[11px] font-sans"
					>
						<tr>
							<th class="py-3 px-4">Timestamp</th>
							<th class="py-3 px-4">Actor</th>
							<th class="py-3 px-4">Action</th>
							<th class="py-3 px-4">Target</th>
							<th class="py-3 px-4">Details</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-100 dark:divide-zinc-800 text-[12px]">
						{#each filteredEntries as entry}
							<tr class="hover:bg-zinc-50/50 dark:hover:bg-zinc-800/40">
								<td class="py-3 px-4 text-zinc-500 whitespace-nowrap">
									{new Date(entry.time).toLocaleString()}
								</td>
								<td class="py-3 px-4 whitespace-nowrap">
									<span
										class="px-2 py-0.5 rounded-full font-sans font-semibold text-[10px] uppercase
									{entry.actor === 'panel'
											? 'bg-indigo-100 dark:bg-indigo-950/80 text-indigo-700 dark:text-indigo-300'
											: entry.actor === 'cli'
												? 'bg-zinc-200 dark:bg-zinc-800 text-zinc-700 dark:text-zinc-300'
												: entry.actor === 'webhook'
													? 'bg-purple-100 dark:bg-purple-950/80 text-purple-700 dark:text-purple-300'
													: 'bg-emerald-100 dark:bg-emerald-950/80 text-emerald-700 dark:text-emerald-300'}"
									>
										{entry.actor}
									</span>
								</td>
								<td class="py-3 px-4 font-bold text-zinc-900 dark:text-zinc-100 whitespace-nowrap">
									{entry.action}
								</td>
								<td class="py-3 px-4 text-zinc-700 dark:text-zinc-300 whitespace-nowrap">
									{entry.target || '-'}
								</td>
								<td class="py-3 px-4 text-zinc-500 truncate max-w-md">
									{entry.details || '-'}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</div>
</div>
