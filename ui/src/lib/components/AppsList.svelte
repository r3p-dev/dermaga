<script lang="ts">
	import type { AppSummary } from '#lib/types/api.js';

	let {
		apps,
		loading = false,
		error = null
	}: {
		apps: AppSummary[];
		loading?: boolean;
		error?: string | null;
	} = $props();

	function statusBadgeClass(label?: string): string {
		switch (label) {
			case 'live':
				return 'bg-emerald-50 dark:bg-emerald-950/40 text-emerald-800 dark:text-emerald-300 border-emerald-200 dark:border-emerald-800';
			case 'failed':
				return 'bg-red-50 dark:bg-red-950/40 text-red-800 dark:text-red-300 border-red-200 dark:border-red-800';
			case 'cancelled':
				return 'bg-stone-100 dark:bg-stone-800 text-stone-700 dark:text-stone-300 border-stone-300 dark:border-stone-700';
			default:
				return 'bg-amber-50 dark:bg-amber-950/40 text-amber-800 dark:text-amber-300 border-amber-200 dark:border-amber-800';
		}
	}
</script>

<section class="space-y-4">
	<div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
		<div>
			<h2 class="text-lg font-semibold tracking-tight">Applications</h2>
			<p class="text-xs text-stone-600 dark:text-stone-400">Stateless apps deployed from your connected GitHub App</p>
		</div>
		<a
			href="/apps/new"
			class="inline-flex items-center justify-center gap-1.5 px-3.5 py-2 text-xs font-medium rounded-md bg-brand-primary text-brand-btn-text hover:bg-brand-hover focus:outline-none focus:ring-2 focus:ring-brand-ring transition w-fit"
		>
			New app
		</a>
	</div>

	{#if error}
		<div role="alert" class="p-4 rounded-lg bg-red-50 dark:bg-red-950/40 border border-red-200 dark:border-red-800 text-red-800 dark:text-red-300 text-xs">
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
			<p class="text-sm font-medium text-stone-800 dark:text-stone-200">No applications yet</p>
			<p class="text-xs text-stone-600 dark:text-stone-400 max-w-md mx-auto">
				Deploy a repository with a Containerfile or Dockerfile using the guided wizard.
			</p>
			<a
				href="/apps/new"
				class="inline-flex items-center px-4 py-2 text-xs font-medium rounded-md bg-brand-primary text-brand-btn-text hover:bg-brand-hover focus:outline-none focus:ring-2 focus:ring-brand-ring transition"
			>
				Create your first app
			</a>
		</div>
	{:else}
		<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
			{#each apps as app}
				<a
					href="/apps/{app.id}"
					class="block rounded-xl border border-stone-200 dark:border-stone-800 bg-white dark:bg-stone-900 p-5 shadow-sm hover:border-brand-primary/60 focus:outline-none focus:ring-2 focus:ring-brand-ring transition space-y-3"
				>
					<div class="flex items-start justify-between gap-2">
						<div class="min-w-0">
							<h3 class="text-base font-semibold truncate text-stone-900 dark:text-stone-100">{app.name}</h3>
							<p class="text-xs font-mono text-stone-600 dark:text-stone-400 truncate">{app.repo_full_name} ({app.branch})</p>
						</div>
						{#if app.last_deployment}
							<span class="shrink-0 inline-flex items-center px-2 py-0.5 rounded-full text-[11px] font-medium border {statusBadgeClass(app.last_deployment.status_label)}">
								{app.last_deployment.status_label}
							</span>
						{/if}
					</div>
					<div class="flex flex-wrap items-center justify-between gap-2 text-xs text-stone-600 dark:text-stone-400 pt-2 border-t border-stone-100 dark:border-stone-800">
						<span class="font-mono truncate">{app.primary_domain || 'No domain'}</span>
						{#if app.last_deployment}
							<span class="font-mono">r{app.last_deployment.release_number}</span>
						{/if}
					</div>
				</a>
			{/each}
		</div>
	{/if}
</section>
