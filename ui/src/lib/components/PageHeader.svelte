<script lang="ts">
	import type { Snippet } from 'svelte'

	interface Breadcrumb {
		label: string
		href?: string
	}

	let {
		title,
		subtitle,
		breadcrumbs = [],
		actions
	}: {
		title: string
		subtitle?: string
		breadcrumbs?: Breadcrumb[]
		actions?: Snippet
	} = $props()
</script>

<div
	class="flex flex-col justify-between gap-4 border-b border-stone-200 pb-5 sm:flex-row sm:items-center dark:border-stone-800"
>
	<div class="min-w-0">
		{#if breadcrumbs.length > 0}
			<nav
				aria-label="Breadcrumb"
				class="mb-1.5 flex items-center gap-1.5 text-xs text-stone-600 dark:text-stone-400"
			>
				{#each breadcrumbs as crumb, idx}
					{#if idx > 0}
						<span aria-hidden="true">/</span>
					{/if}
					{#if crumb.href}
						<a
							href={crumb.href}
							class="rounded hover:text-stone-900 hover:underline focus:ring-2 focus:ring-brand-ring focus:outline-none dark:hover:text-stone-100"
						>
							{crumb.label}
						</a>
					{:else}
						<span class="truncate font-mono text-stone-700 dark:text-stone-300">
							{crumb.label}
						</span>
					{/if}
				{/each}
			</nav>
		{/if}
		<h1
			class="truncate text-2xl font-bold tracking-tight text-stone-900 dark:text-stone-100"
		>
			{title}
		</h1>
		{#if subtitle}
			<p
				class="mt-1 text-xs wrap-break-word text-stone-600 dark:text-stone-400"
			>
				{subtitle}
			</p>
		{/if}
	</div>
	{#if actions}
		<div class="flex shrink-0 flex-wrap items-center gap-2.5">
			{@render actions()}
		</div>
	{/if}
</div>
