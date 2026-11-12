<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		open = false,
		title = '',
		onclose,
		children,
		footer
	}: {
		open: boolean;
		title?: string;
		onclose?: () => void;
		children?: Snippet;
		footer?: Snippet;
	} = $props();

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape' && open) {
			onclose?.();
		}
	}
</script>

<svelte:window onkeydown={handleKeydown} />

{#if open}
	<div class="fixed inset-0 z-50 overflow-y-auto" role="dialog" aria-modal="true">
		<div class="flex min-h-full items-center justify-center p-4 text-center sm:p-0">
			<!-- Backdrop -->
			<button
				type="button"
				class="fixed inset-0 bg-zinc-950/60 backdrop-blur-xs transition-opacity border-none cursor-default"
				onclick={() => onclose?.()}
				aria-label="Close backdrop"
			></button>

			<!-- Modal dialog -->
			<div
				class="relative transform overflow-hidden rounded-xl bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 text-left shadow-2xl transition-all sm:my-8 sm:w-full sm:max-w-lg"
			>
				{#if title}
					<div
						class="flex items-center justify-between border-b border-zinc-100 dark:border-zinc-800/80 px-6 py-4"
					>
						<h3 class="text-base font-semibold leading-6 text-zinc-900 dark:text-zinc-100">
							{title}
						</h3>
						<button
							type="button"
							class="rounded-lg p-1 text-zinc-400 hover:text-zinc-500 hover:bg-zinc-100 dark:hover:bg-zinc-800"
							onclick={() => onclose?.()}
							aria-label="Close modal"
						>
							<svg
								class="h-5 w-5"
								fill="none"
								viewBox="0 0 24 24"
								stroke-width="1.5"
								stroke="currentColor"
							>
								<path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
							</svg>
						</button>
					</div>
				{/if}

				<div class="px-6 py-5">
					{@render children?.()}
				</div>

				{#if footer}
					<div
						class="border-t border-zinc-100 dark:border-zinc-800/80 bg-zinc-50 dark:bg-zinc-900/50 px-6 py-3.5 flex justify-end gap-3"
					>
						{@render footer()}
					</div>
				{/if}
			</div>
		</div>
	</div>
{/if}
