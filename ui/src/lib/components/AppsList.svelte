<script lang="ts">
	import type { AppSummary } from '#lib/types/api.js';
	import StatusBadge from './StatusBadge.svelte';

	let {
		apps,
		loading = false,
		error = null
	}: {
		apps: AppSummary[];
		loading?: boolean;
		error?: string | null;
	} = $props();

	function isInFlight(label?: string): boolean {
		return label === 'queued' || label === 'building' || label === 'starting' || label === 'healthy';
	}
</script>

<section class="space-y-4">
	<div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
		<div>
			<h2 class="text-base font-semibold tracking-tight text-stone-900 dark:text-stone-100">Applications</h2>
			<p class="text-xs text-stone-600 dark:text-stone-400">Stateless apps deployed from your connected GitHub App</p>
		</div>
		<a
			href="/apps/new"
			class="inline-flex items-center justify-center gap-1.5 px-3.5 py-2 text-xs font-medium rounded-lg bg-brand-primary text-brand-btn-text hover:bg-brand-hover focus:outline-none focus:ring-2 focus:ring-brand-ring transition w-fit shadow-sm"
		>
			New app
		</a>
	</div>

	{#if error}
		<div role="alert" class="p-4 rounded-xl bg-red-50 dark:bg-red-950/40 border border-red-200 dark:border-red-800 text-red-800 dark:text-red-300 text-xs">
			{error}
		</div>
	{:else if loading}
		<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
			{#each [1, 2] as _}
				<div class="h-32 rounded-xl bg-stone-100 dark:bg-stone-800/50 animate-pulse"></div>
			{/each}
		</div>
	{:else if apps.length === 0}
		<div class="rounded-xl border border-dashed border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-900 p-8 text-center space-y-3">
			<div class="w-10 h-10 mx-auto rounded-lg bg-stone-100 dark:bg-stone-800 flex items-center justify-center text-stone-600 dark:text-stone-400" aria-hidden="true">
				<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4" />
				</svg>
			</div>
			<p class="text-sm font-medium text-stone-900 dark:text-stone-100">No applications yet</p>
			<p class="text-xs text-stone-600 dark:text-stone-400 max-w-md mx-auto">
				Deploy a repository with a Containerfile or Dockerfile using the guided wizard.
			</p>
			<a
				href="/apps/new"
				class="inline-flex items-center px-4 py-2 text-xs font-medium rounded-lg bg-brand-primary text-brand-btn-text hover:bg-brand-hover focus:outline-none focus:ring-2 focus:ring-brand-ring transition shadow-sm"
			>
				Create your first app
			</a>
		</div>
	{:else}
		<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
			{#each apps as app}
				<a
					href="/apps/{app.id}"
					class="group block rounded-xl border border-stone-200 dark:border-stone-800 bg-white dark:bg-stone-900 p-5 shadow-sm hover:border-brand-primary/70 hover:shadow transition focus:outline-none focus:ring-2 focus:ring-brand-ring space-y-3"
				>
					<div class="flex items-start justify-between gap-3">
						<div class="min-w-0">
							<h3 class="text-base font-semibold truncate text-stone-900 dark:text-stone-100 group-hover:text-brand-text transition">
								{app.name}
							</h3>
							<p class="text-xs font-mono text-stone-600 dark:text-stone-400 truncate mt-0.5">
								{app.repo_full_name} ({app.branch})
							</p>
						</div>
						{#if app.last_deployment}
							<div class="shrink-0">
								<StatusBadge
									label={app.last_deployment.status_label}
									pulse={isInFlight(app.last_deployment.status_label)}
								/>
							</div>
						{/if}
					</div>
					<div class="flex flex-wrap items-center justify-between gap-2 text-xs text-stone-600 dark:text-stone-400 pt-3 border-t border-stone-100 dark:border-stone-800">
						<span class="font-mono truncate">{app.primary_domain || 'No domain'}</span>
						<div class="flex items-center gap-2 shrink-0">
							{#if app.last_deployment}
								<span class="font-mono tabular-nums">r{app.last_deployment.release_number}</span>
							{/if}
							<span class="text-stone-400 group-hover:text-brand-text transition" aria-hidden="true">&rarr;</span>
						</div>
					</div>
				</a>
			{/each}
		</div>
	{/if}
</section>
