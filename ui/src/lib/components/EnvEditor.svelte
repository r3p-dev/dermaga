<script lang="ts">
	import type { EnvVarInput } from '#lib/types/api.js'

	let {
		items = $bindable<EnvVarInput[]>([]),
		disabled = false
	}: {
		items: EnvVarInput[]
		disabled?: boolean
	} = $props()

	let hasBuildTime = $derived(items.some((item) => item.build_time))

	// Same limit as builder.MinMaskedLength: shorter values are not masked in logs.
	const minMasked = 4
	const tooShortToMask = (value: string) =>
		value.length > 0 && [...value].length < minMasked

	function addRow() {
		items = [...items, { key: '', value: '', runtime: true, build_time: false }]
	}

	function removeRow(index: number) {
		items = items.filter((_, i) => i !== index)
	}
</script>

<div class="space-y-3">
	{#if hasBuildTime}
		<div
			role="note"
			class="rounded-md border border-amber-200 bg-amber-50 p-3 text-xs leading-relaxed text-amber-900 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-200"
		>
			<strong>Build-time variables warning:</strong> Variables marked
			<em>Build time</em>
			are passed via <code class="font-mono">--build-arg</code> and can be recorded
			in image history or client bundles. Never mark secrets (API keys, passwords,
			tokens) as build-time variables.
		</div>
	{/if}

	{#if items.length === 0}
		<p class="text-xs text-stone-600 dark:text-stone-400">
			No environment variables configured yet.
		</p>
	{:else}
		<div class="space-y-2.5">
			{#each items as item, i}
				<div
					class="space-y-2 rounded-lg border border-stone-200 bg-stone-50/60 p-3 dark:border-stone-800 dark:bg-stone-950/60"
				>
					<div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
						<div>
							<label
								for="env-key-{i}"
								class="mb-1 block text-[11px] font-medium text-stone-600 dark:text-stone-400"
							>
								Key
							</label>
							<input
								id="env-key-{i}"
								type="text"
								bind:value={item.key}
								{disabled}
								placeholder="API_URL"
								class="w-full rounded-md border border-stone-300 bg-white px-2.5 py-1.5 font-mono text-xs text-stone-900 focus:ring-2 focus:ring-brand-ring focus:outline-none dark:border-stone-700 dark:bg-stone-950 dark:text-stone-100"
							/>
						</div>
						<div>
							<label
								for="env-val-{i}"
								class="mb-1 block text-[11px] font-medium text-stone-600 dark:text-stone-400"
							>
								Value
							</label>
							<input
								id="env-val-{i}"
								type="password"
								bind:value={item.value}
								{disabled}
								placeholder="Enter value"
								aria-describedby={tooShortToMask(item.value)
									? `env-val-short-${i}`
									: undefined}
								class="w-full rounded-md border border-stone-300 bg-white px-2.5 py-1.5 font-mono text-xs text-stone-900 focus:ring-2 focus:ring-brand-ring focus:outline-none dark:border-stone-700 dark:bg-stone-950 dark:text-stone-100"
							/>
							{#if tooShortToMask(item.value)}
								<p
									id="env-val-short-{i}"
									class="mt-1 text-[11px] text-amber-800 dark:text-amber-300"
								>
									Shorter than {minMasked} characters: this value is not masked in
									build and runtime logs.
								</p>
							{/if}
						</div>
					</div>
					<div class="flex flex-wrap items-center justify-between gap-2 pt-1">
						<div class="flex items-center gap-4 text-xs">
							<label class="inline-flex cursor-pointer items-center gap-1.5">
								<input
									type="checkbox"
									bind:checked={item.runtime}
									{disabled}
									class="rounded border-stone-300 dark:border-stone-700"
								/>
								<span>Runtime</span>
							</label>
							<label class="inline-flex cursor-pointer items-center gap-1.5">
								<input
									type="checkbox"
									bind:checked={item.build_time}
									{disabled}
									class="rounded border-stone-300 dark:border-stone-700"
								/>
								<span>Build time</span>
							</label>
							{#if item.build_time}
								<span
									class="rounded bg-amber-100 px-1.5 py-0.5 text-[10px] font-medium text-amber-800 dark:bg-amber-900/50 dark:text-amber-300"
								>
									--build-arg
								</span>
							{/if}
						</div>
						<button
							type="button"
							onclick={() => removeRow(i)}
							{disabled}
							class="rounded px-1 text-xs text-red-700 hover:underline focus:ring-2 focus:ring-brand-ring focus:outline-none dark:text-red-400"
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
		class="rounded-md border border-stone-300 px-3 py-1.5 text-xs font-medium text-stone-800 transition hover:bg-stone-100 focus:ring-2 focus:ring-brand-ring focus:outline-none dark:border-stone-700 dark:text-stone-200 dark:hover:bg-stone-800"
	>
		+ Add variable
	</button>
</div>
