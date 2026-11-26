<script lang="ts">
	import { getSession, getAuditLogs } from '#lib/api.js'
	import PageHeader from '#lib/components/PageHeader.svelte'
	import type { AuditLogEntry, SessionInfo } from '#lib/types/api.js'
	import { onMount } from 'svelte'

	let session = $state<SessionInfo | null>(null)
	let logs = $state<AuditLogEntry[]>([])
	let selectedOrigin = $state('')
	let selectedApp = $state('')
	let selectedTime = $state('')
	let loading = $state(true)
	let error = $state<string | null>(null)

	function calculateSince(timeRange: string): string | undefined {
		const now = Date.now()
		switch (timeRange) {
			case '1h':
				return new Date(now - 3600 * 1000).toISOString()
			case '24h':
				return new Date(now - 24 * 3600 * 1000).toISOString()
			case '7d':
				return new Date(now - 7 * 24 * 3600 * 1000).toISOString()
			default:
				return undefined
		}
	}

	async function loadLogs() {
		loading = true
		error = null
		try {
			const since = calculateSince(selectedTime)
			const res = await getAuditLogs(
				selectedOrigin || undefined,
				selectedApp.trim() || undefined,
				since
			)
			logs = res.logs || []
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load activity'
		} finally {
			loading = false
		}
	}

	function handleOriginChange(e: Event) {
		const target = e.target as HTMLSelectElement
		selectedOrigin = target.value
		loadLogs()
	}

	function handleAppInput(e: Event) {
		const target = e.target as HTMLInputElement
		selectedApp = target.value
		loadLogs()
	}

	function handleTimeChange(e: Event) {
		const target = e.target as HTMLSelectElement
		selectedTime = target.value
		loadLogs()
	}

	function getOriginBadge(origin: string): { bg: string; text: string } {
		switch (origin) {
			case 'panel':
				return {
					bg: 'bg-brand-badge-bg border-brand-badge-border',
					text: 'text-brand-badge-text'
				}
			case 'webhook':
				return {
					bg: 'bg-sky-50 dark:bg-sky-950/40 border-sky-200 dark:border-sky-800',
					text: 'text-sky-700 dark:text-sky-300'
				}
			case 'server_command':
				return {
					bg: 'bg-purple-50 dark:bg-purple-950/40 border-purple-200 dark:border-purple-800',
					text: 'text-purple-700 dark:text-purple-300'
				}
			case 'daemon':
				return {
					bg: 'bg-amber-50 dark:bg-amber-950/40 border-amber-200 dark:border-amber-800',
					text: 'text-amber-700 dark:text-amber-300'
				}
			default:
				return {
					bg: 'bg-stone-100 dark:bg-stone-800 border-stone-200 dark:border-stone-700',
					text: 'text-stone-700 dark:text-stone-300'
				}
		}
	}

	function formatDate(dateStr: string): string {
		try {
			const d = new Date(dateStr)
			return d.toLocaleString('en-US', {
				month: 'short',
				day: 'numeric',
				hour: '2-digit',
				minute: '2-digit',
				second: '2-digit',
				hour12: false
			})
		} catch {
			return dateStr
		}
	}

	onMount(async () => {
		try {
			session = await getSession()
			if (!session.authenticated) {
				window.location.href = '/login'
				return
			}
			await loadLogs()
		} catch (err) {
			error = err instanceof Error ? err.message : 'Error checking session'
			loading = false
		}
	})
</script>

<svelte:head>
	<title>Activity &bull; dermaga</title>
</svelte:head>

<div class="space-y-6">
	<PageHeader
		title="Activity"
		subtitle="Immutable audit record of mutating actions and security events (AUD-1, AUD-2)"
	>
		{#snippet actions()}
			<div class="flex flex-wrap items-center gap-2.5">
				<div class="flex items-center gap-1.5">
					<label
						for="origin-filter"
						class="text-xs font-medium text-stone-600 dark:text-stone-400"
					>
						Origin:
					</label>
					<select
						id="origin-filter"
						value={selectedOrigin}
						onchange={handleOriginChange}
						class="rounded-lg border border-stone-300 bg-white px-2.5 py-1.5 text-xs text-stone-900 focus:ring-2 focus:ring-brand-ring focus:outline-none dark:border-stone-700 dark:bg-stone-900 dark:text-stone-100"
					>
						<option value="">All Origins</option>
						<option value="panel">panel</option>
						<option value="webhook">webhook</option>
						<option value="server_command">server_command</option>
						<option value="daemon">daemon</option>
					</select>
				</div>

				<div class="flex items-center gap-1.5">
					<label
						for="app-filter"
						class="text-xs font-medium text-stone-600 dark:text-stone-400"
					>
						App:
					</label>
					<input
						id="app-filter"
						type="text"
						placeholder="Filter by app..."
						value={selectedApp}
						oninput={handleAppInput}
						class="w-32 rounded-lg border border-stone-300 bg-white px-2.5 py-1.5 text-xs text-stone-900 focus:ring-2 focus:ring-brand-ring focus:outline-none dark:border-stone-700 dark:bg-stone-900 dark:text-stone-100"
					/>
				</div>

				<div class="flex items-center gap-1.5">
					<label
						for="time-filter"
						class="text-xs font-medium text-stone-600 dark:text-stone-400"
					>
						Time:
					</label>
					<select
						id="time-filter"
						value={selectedTime}
						onchange={handleTimeChange}
						class="rounded-lg border border-stone-300 bg-white px-2.5 py-1.5 text-xs text-stone-900 focus:ring-2 focus:ring-brand-ring focus:outline-none dark:border-stone-700 dark:bg-stone-900 dark:text-stone-100"
					>
						<option value="">All Time</option>
						<option value="1h">Last hour</option>
						<option value="24h">Last 24 hours</option>
						<option value="7d">Last 7 days</option>
					</select>
				</div>
			</div>
		{/snippet}
	</PageHeader>

	{#if error}
		<div
			role="alert"
			class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300"
		>
			{error}
		</div>
	{:else if loading}
		<div class="space-y-2">
			{#each [1, 2, 3, 4, 5] as _}
				<div
					class="h-14 animate-pulse rounded-lg bg-stone-100 dark:bg-stone-800/50"
				></div>
			{/each}
		</div>
	{:else if logs.length === 0}
		<div
			class="rounded-xl border border-stone-200 bg-white p-8 text-center text-stone-500 dark:border-stone-800 dark:bg-stone-900 dark:text-stone-400"
		>
			<p class="text-sm font-medium">No activity entries found</p>
			{#if selectedOrigin}
				<p class="mt-1 text-xs">Try clearing the origin filter.</p>
			{/if}
		</div>
	{:else}
		<!-- Narrow screen stacked rows (CONVENTIONS §5.2) -->
		<div class="space-y-3 md:hidden" aria-label="Activity list">
			{#each logs as log (log.id)}
				{@const badge = getOriginBadge(log.origin)}
				<article
					class="space-y-2 rounded-xl border border-stone-200 bg-white p-4 shadow-sm dark:border-stone-800 dark:bg-stone-900"
				>
					<div class="flex items-center justify-between gap-2">
						<span
							class="inline-flex rounded border px-2 py-0.5 text-[11px] font-medium {badge.bg} {badge.text}"
						>
							{log.origin}
						</span>
						<span
							class="font-mono text-[11px] text-stone-600 dark:text-stone-400"
						>
							{formatDate(log.created_at)}
						</span>
					</div>
					<div
						class="flex items-baseline justify-between gap-2 font-mono text-xs"
					>
						<span class="font-semibold text-stone-900 dark:text-stone-100">
							{log.event}
						</span>
						<span class="text-stone-600 dark:text-stone-400">{log.actor}</span>
					</div>
					<p class="text-xs wrap-break-word text-stone-700 dark:text-stone-300">
						{log.description}
					</p>
				</article>
			{/each}
		</div>

		<!-- Desktop table -->
		<div
			class="hidden overflow-hidden rounded-xl border border-stone-200 bg-white shadow-sm md:block dark:border-stone-800 dark:bg-stone-900"
		>
			<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
			<div
				class="overflow-x-auto focus:ring-1 focus:ring-brand-ring focus:outline-none"
				tabindex="0"
				role="region"
				aria-label="Activity table"
			>
				<table class="w-full border-collapse text-left text-xs">
					<thead>
						<tr
							class="border-b border-stone-200 bg-stone-50/50 text-stone-600 dark:border-stone-800 dark:bg-stone-950/50 dark:text-stone-400"
						>
							<th class="px-4 py-3 font-medium">Time</th>
							<th class="px-4 py-3 font-medium">Origin</th>
							<th class="px-4 py-3 font-medium">Event</th>
							<th class="px-4 py-3 font-medium">Actor</th>
							<th class="px-4 py-3 font-medium">Description</th>
						</tr>
					</thead>
					<tbody
						class="divide-y divide-stone-100 font-mono dark:divide-stone-800/60"
					>
						{#each logs as log (log.id)}
							{@const badge = getOriginBadge(log.origin)}
							<tr
								class="transition hover:bg-stone-50/50 dark:hover:bg-stone-800/30"
							>
								<td
									class="px-4 py-3 whitespace-nowrap text-stone-500 dark:text-stone-400"
								>
									{formatDate(log.created_at)}
								</td>
								<td class="px-4 py-3 whitespace-nowrap">
									<span
										class="inline-flex rounded border px-2 py-0.5 font-sans text-[11px] font-medium {badge.bg} {badge.text}"
									>
										{log.origin}
									</span>
								</td>
								<td
									class="px-4 py-3 font-semibold whitespace-nowrap text-stone-800 dark:text-stone-200"
								>
									{log.event}
								</td>
								<td
									class="px-4 py-3 whitespace-nowrap text-stone-600 dark:text-stone-400"
								>
									{log.actor}
								</td>
								<td
									class="max-w-md truncate px-4 py-3 font-sans text-stone-600 dark:text-stone-300"
								>
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
