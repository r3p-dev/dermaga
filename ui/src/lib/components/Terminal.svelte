<script lang="ts">
	import { tick } from 'svelte';
	import { toast } from '../stores/toast';

	let {
		lines = [],
		title = 'Logs',
		follow = true,
		maxHeight = '480px',
		emptyText = 'No logs available.'
	}: {
		lines: string[];
		title?: string;
		follow?: boolean;
		maxHeight?: string;
		emptyText?: string;
	} = $props();

	let container = $state<HTMLDivElement | null>(null);
	let autoScroll = $state(true);
	let filterQuery = $state('');
	let wrapText = $state(true);

	$effect(() => {
		autoScroll = follow;
	});

	const filteredLines = $derived(
		filterQuery.trim() === ''
			? lines
			: lines.filter((l) => l.toLowerCase().includes(filterQuery.toLowerCase()))
	);

	$effect(() => {
		// When lines change and autoScroll is active, scroll to bottom
		if (lines.length && autoScroll && container) {
			tick().then(() => {
				if (container) {
					container.scrollTop = container.scrollHeight;
				}
			});
		}
	});

	function handleScroll() {
		if (!container) return;
		const isAtBottom = container.scrollHeight - container.scrollTop - container.clientHeight < 40;
		autoScroll = isAtBottom;
	}

	function copyLogs() {
		const text = lines.join('\n');
		navigator.clipboard.writeText(text).then(() => {
			toast.success('Logs copied to clipboard');
		});
	}

	function clearFilter() {
		filterQuery = '';
	}
</script>

<div
	class="rounded-xl border border-zinc-800 bg-zinc-950 shadow-xl overflow-hidden font-mono text-xs text-zinc-300"
>
	<!-- Terminal header -->
	<div
		class="flex items-center justify-between border-b border-zinc-800/80 bg-zinc-900/90 px-4 py-2.5 select-none"
	>
		<div class="flex items-center space-x-2">
			<span class="w-3 h-3 rounded-full bg-rose-500/80 inline-block"></span>
			<span class="w-3 h-3 rounded-full bg-amber-500/80 inline-block"></span>
			<span class="w-3 h-3 rounded-full bg-emerald-500/80 inline-block"></span>
			<span class="ml-2 font-semibold text-zinc-200">{title}</span>
			<span class="text-zinc-500 text-[11px]">({lines.length} lines)</span>
		</div>

		<div class="flex items-center space-x-2">
			<!-- Filter input -->
			<div class="relative">
				<input
					type="text"
					bind:value={filterQuery}
					placeholder="Filter logs..."
					class="w-32 sm:w-48 bg-zinc-950 border border-zinc-800 rounded px-2 py-1 text-xs text-zinc-200 focus:outline-none focus:border-indigo-500 placeholder-zinc-600"
				/>
				{#if filterQuery}
					<button
						type="button"
						onclick={clearFilter}
						class="absolute right-1.5 top-1.5 text-zinc-500 hover:text-zinc-300"
					>
						&times;
					</button>
				{/if}
			</div>

			<!-- Wrap toggle -->
			<button
				type="button"
				class="px-2 py-1 rounded border border-zinc-800 bg-zinc-950/60 hover:bg-zinc-800 text-[11px] {wrapText
					? 'text-indigo-400 border-indigo-900/50'
					: 'text-zinc-400'}"
				onclick={() => (wrapText = !wrapText)}
				title="Toggle line wrapping"
			>
				Wrap
			</button>

			<!-- Auto scroll toggle -->
			<button
				type="button"
				class="px-2 py-1 rounded border border-zinc-800 bg-zinc-950/60 hover:bg-zinc-800 text-[11px] {autoScroll
					? 'text-emerald-400 border-emerald-900/50'
					: 'text-zinc-400'}"
				onclick={() => {
					autoScroll = !autoScroll;
					if (autoScroll && container) container.scrollTop = container.scrollHeight;
				}}
				title="Toggle auto scroll"
			>
				{autoScroll ? '● Scroll' : '○ Scroll'}
			</button>

			<!-- Copy button -->
			<button
				type="button"
				class="px-2 py-1 rounded border border-zinc-800 bg-zinc-950/60 hover:bg-zinc-800 text-zinc-300 text-[11px]"
				onclick={copyLogs}
				title="Copy logs"
			>
				Copy
			</button>
		</div>
	</div>

	<!-- Terminal output -->
	<div
		bind:this={container}
		onscroll={handleScroll}
		style="max-height: {maxHeight}; min-height: 200px;"
		class="overflow-y-auto p-4 space-y-0.5 terminal-scroll select-text leading-relaxed bg-zinc-950 text-zinc-300"
	>
		{#if filteredLines.length === 0}
			<div class="text-zinc-600 italic py-8 text-center">{emptyText}</div>
		{:else}
			{#each filteredLines as line, i}
				<div class="flex items-start hover:bg-zinc-900/50 -mx-4 px-4 py-0.5">
					<span class="w-10 shrink-0 text-zinc-600 select-none text-right pr-4 text-[10px]"
						>{i + 1}</span
					>
					<span
						class="flex-1 {wrapText
							? 'break-all whitespace-pre-wrap'
							: 'whitespace-pre overflow-x-auto'}">{line}</span
					>
				</div>
			{/each}
		{/if}
	</div>
</div>
