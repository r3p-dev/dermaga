<script lang="ts">
	export interface SubNavItem {
		id: string;
		label: string;
		danger?: boolean;
	}

	let {
		items,
		active,
		onSelect,
		ariaLabel
	}: {
		items: SubNavItem[];
		active: string;
		onSelect: (id: string) => void;
		ariaLabel: string;
	} = $props();
</script>

<nav aria-label={ariaLabel} class="w-full md:w-48 shrink-0">
	<div
		role="tablist"
		aria-label={ariaLabel}
		class="flex md:flex-col gap-1 overflow-x-auto md:overflow-visible pb-2 md:pb-0 border-b md:border-b-0 border-stone-200 dark:border-stone-800"
	>
		{#each items as item (item.id)}
			{@const isActive = active === item.id}
			<button
				type="button"
				role="tab"
				aria-selected={isActive}
				onclick={() => onSelect(item.id)}
				class="shrink-0 text-left px-3 py-2 rounded-lg text-xs font-medium transition focus:outline-none focus:ring-2 focus:ring-brand-ring {isActive
					? item.danger
						? 'bg-red-50 dark:bg-red-950/50 text-red-800 dark:text-red-300 border border-red-200 dark:border-red-800'
						: 'bg-brand-badge-bg text-brand-badge-text border border-brand-badge-border'
					: item.danger
						? 'text-red-700 dark:text-red-400 hover:bg-red-50/70 dark:hover:bg-red-950/30 border border-transparent'
						: 'text-stone-700 dark:text-stone-300 hover:bg-stone-100 dark:hover:bg-stone-800/70 border border-transparent'}"
			>
				{item.label}
			</button>
		{/each}
	</div>
</nav>
