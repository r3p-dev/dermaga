<script lang="ts">
	import { toast } from '../stores/toast';

	let toasts = $derived($toast);
</script>

<div
	class="fixed bottom-4 right-4 z-50 flex flex-col space-y-2 max-w-sm w-full pointer-events-none"
	aria-live="polite"
>
	{#each toasts as t (t.id)}
		<div
			class="pointer-events-auto flex items-start p-3.5 rounded-lg border shadow-lg transition-all duration-200
			{t.type === 'success'
				? 'bg-emerald-50 text-emerald-950 border-emerald-200 dark:bg-emerald-950/80 dark:text-emerald-100 dark:border-emerald-800'
				: t.type === 'error'
					? 'bg-rose-50 text-rose-950 border-rose-200 dark:bg-rose-950/80 dark:text-rose-100 dark:border-rose-800'
					: t.type === 'warning'
						? 'bg-amber-50 text-amber-950 border-amber-200 dark:bg-amber-950/80 dark:text-amber-100 dark:border-amber-800'
						: 'bg-indigo-50 text-indigo-950 border-indigo-200 dark:bg-indigo-950/80 dark:text-indigo-100 dark:border-indigo-800'}"
		>
			<div class="mr-2.5 mt-0.5 shrink-0">
				{#if t.type === 'success'}
					<svg class="w-4 h-4 text-emerald-600 dark:text-emerald-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
					</svg>
				{:else if t.type === 'error'}
					<svg class="w-4 h-4 text-rose-600 dark:text-rose-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
					</svg>
				{:else if t.type === 'warning'}
					<svg class="w-4 h-4 text-amber-600 dark:text-amber-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
					</svg>
				{:else}
					<svg class="w-4 h-4 text-indigo-600 dark:text-indigo-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
					</svg>
				{/if}
			</div>
			<div class="flex-1 text-sm font-medium leading-tight">
				{#if t.title}
					<p class="font-semibold text-xs tracking-wide uppercase opacity-90 mb-0.5">{t.title}</p>
				{/if}
				<p class="break-words">{t.message}</p>
			</div>
			<button
				type="button"
				class="ml-2 -mr-1 -mt-1 p-1 rounded-md opacity-60 hover:opacity-100 transition-opacity"
				onclick={() => toast.remove(t.id)}
				aria-label="Close"
			>
				<svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
				</svg>
			</button>
		</div>
	{/each}
</div>
