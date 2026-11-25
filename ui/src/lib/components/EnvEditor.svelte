<script lang="ts">
	import type { EnvVarInput } from '#lib/types/api.js';

	let {
		items = $bindable<EnvVarInput[]>([]),
		disabled = false
	}: {
		items: EnvVarInput[];
		disabled?: boolean;
	} = $props();

	let hasBuildTime = $derived(items.some((item) => item.build_time));

	function addRow() {
		items = [...items, { key: '', value: '', runtime: true, build_time: false }];
	}

	function removeRow(index: number) {
		items = items.filter((_, i) => i !== index);
	}
</script>

<div class="space-y-3">
	{#if hasBuildTime}
		<div
			role="note"
			class="p-3 rounded-md bg-amber-50 dark:bg-amber-950/40 border border-amber-200 dark:border-amber-800 text-amber-900 dark:text-amber-200 text-xs leading-relaxed"
		>
			<strong>Build-time variables warning:</strong> Variables marked <em>Build time</em> are passed via <code class="font-mono">--build-arg</code> and can be recorded in image history or client bundles. Never mark secrets (API keys, passwords, tokens) as build-time variables.
		</div>
	{/if}

	{#if items.length === 0}
		<p class="text-xs text-stone-600 dark:text-stone-400">No environment variables configured yet.</p>
	{:else}
		<div class="space-y-2.5">
			{#each items as item, i}
				<div class="p-3 rounded-lg border border-stone-200 dark:border-stone-800 bg-stone-50/60 dark:bg-stone-950/60 space-y-2">
					<div class="grid grid-cols-1 sm:grid-cols-2 gap-2">
						<div>
							<label for="env-key-{i}" class="block text-[11px] font-medium text-stone-600 dark:text-stone-400 mb-1">
								Key
							</label>
							<input
								id="env-key-{i}"
								type="text"
								bind:value={item.key}
								{disabled}
								placeholder="API_URL"
								class="w-full px-2.5 py-1.5 text-xs font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950 text-stone-900 dark:text-stone-100 focus:outline-none focus:ring-2 focus:ring-brand-ring"
							/>
						</div>
						<div>
							<label for="env-val-{i}" class="block text-[11px] font-medium text-stone-600 dark:text-stone-400 mb-1">
								Value
							</label>
							<input
								id="env-val-{i}"
								type="password"
								bind:value={item.value}
								{disabled}
								placeholder="Enter value"
								class="w-full px-2.5 py-1.5 text-xs font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950 text-stone-900 dark:text-stone-100 focus:outline-none focus:ring-2 focus:ring-brand-ring"
							/>
						</div>
					</div>
					<div class="flex flex-wrap items-center justify-between gap-2 pt-1">
						<div class="flex items-center gap-4 text-xs">
							<label class="inline-flex items-center gap-1.5 cursor-pointer">
								<input type="checkbox" bind:checked={item.runtime} {disabled} class="rounded border-stone-300 dark:border-stone-700" />
								<span>Runtime</span>
							</label>
							<label class="inline-flex items-center gap-1.5 cursor-pointer">
								<input type="checkbox" bind:checked={item.build_time} {disabled} class="rounded border-stone-300 dark:border-stone-700" />
								<span>Build time</span>
							</label>
							{#if item.build_time}
								<span class="px-1.5 py-0.5 rounded text-[10px] font-medium bg-amber-100 dark:bg-amber-900/50 text-amber-800 dark:text-amber-300">
									--build-arg
								</span>
							{/if}
						</div>
						<button
							type="button"
							onclick={() => removeRow(i)}
							{disabled}
							class="text-xs text-red-700 dark:text-red-400 hover:underline focus:outline-none focus:ring-2 focus:ring-brand-ring rounded px-1"
						>
							Remove
						</button>
					</div>
				</div>
			{/each}
		</div>
	{/if}

	<button
		type="button"
		onclick={addRow}
		{disabled}
		class="px-3 py-1.5 text-xs font-medium rounded-md border border-stone-300 dark:border-stone-700 text-stone-800 dark:text-stone-200 hover:bg-stone-100 dark:hover:bg-stone-800 focus:outline-none focus:ring-2 focus:ring-brand-ring transition"
	>
		+ Add variable
	</button>
</div>
