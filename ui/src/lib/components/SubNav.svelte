<script lang="ts">
	export interface SubNavItem {
		id: string
		label: string
		danger?: boolean
	}

	let {
		items,
		active,
		onSelect,
		ariaLabel
	}: {
		items: SubNavItem[]
		active: string
		onSelect: (id: string) => void
		ariaLabel: string
	} = $props()
</script>

<nav aria-label={ariaLabel} class="w-full shrink-0 md:w-48">
	<div
		role="tablist"
		aria-label={ariaLabel}
		class="flex gap-1 overflow-x-auto border-b border-stone-200 pb-2 md:flex-col md:overflow-visible md:border-b-0 md:pb-0 dark:border-stone-800"
	>
		{#each items as item (item.id)}
			{@const isActive = active === item.id}
			<button
				type="button"
				role="tab"
				aria-selected={isActive}
				onclick={() => onSelect(item.id)}
				class="shrink-0 rounded-lg px-3 py-2 text-left text-xs font-medium transition focus:ring-2 focus:ring-brand-ring focus:outline-none {isActive
					? item.danger
						? 'border border-red-200 bg-red-50 text-red-800 dark:border-red-800 dark:bg-red-950/50 dark:text-red-300'
						: 'border border-brand-badge-border bg-brand-badge-bg text-brand-badge-text'
					: item.danger
						? 'border border-transparent text-red-700 hover:bg-red-50/70 dark:text-red-400 dark:hover:bg-red-950/30'
						: 'border border-transparent text-stone-700 hover:bg-stone-100 dark:text-stone-300 dark:hover:bg-stone-800/70'}"
			>
				{item.label}
			</button>
		{/each}
	</div>
</nav>
