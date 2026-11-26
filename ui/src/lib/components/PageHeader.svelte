<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Breadcrumb {
		label: string;
		href?: string;
	}

	let {
		title,
		subtitle,
		breadcrumbs = [],
		actions
	}: {
		title: string;
		subtitle?: string;
		breadcrumbs?: Breadcrumb[];
		actions?: Snippet;
	} = $props();
</script>

<div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pb-5 border-b border-stone-200 dark:border-stone-800">
	<div class="min-w-0">
		{#if breadcrumbs.length > 0}
			<nav aria-label="Breadcrumb" class="flex items-center gap-1.5 text-xs text-stone-600 dark:text-stone-400 mb-1.5">
				{#each breadcrumbs as crumb, idx}
					{#if idx > 0}
						<span aria-hidden="true">/</span>
					{/if}
					{#if crumb.href}
						<a href={crumb.href} class="hover:text-stone-900 dark:hover:text-stone-100 hover:underline focus:outline-none focus:ring-2 focus:ring-brand-ring rounded">
							{crumb.label}
						</a>
					{:else}
						<span class="font-mono truncate text-stone-700 dark:text-stone-300">{crumb.label}</span>
					{/if}
				{/each}
			</nav>
		{/if}
		<h1 class="text-2xl font-bold tracking-tight text-stone-900 dark:text-stone-100 truncate">{title}</h1>
		{#if subtitle}
			<p class="text-xs text-stone-600 dark:text-stone-400 mt-1 break-words">{subtitle}</p>
		{/if}
	</div>
	{#if actions}
		<div class="flex flex-wrap items-center gap-2.5 shrink-0">
			{@render actions()}
		</div>
	{/if}
</div>
