<script lang="ts">
	let { status = 'unknown', size = 'sm' }: { status: string; size?: 'sm' | 'md' } = $props();

	const st = $derived(status?.toLowerCase() || 'unknown');

	const isSuccess = $derived(['live', 'active', 'ok'].includes(st));
	const isProgress = $derived(['building', 'verifying', 'draining', 'queued', 'pending'].includes(st));
	const isFail = $derived(['failed', 'fail', 'error'].includes(st));
	const isWarn = $derived(['warn', 'rolled_back', 'superseded'].includes(st));
</script>

<span
	class="inline-flex items-center font-medium rounded-full tracking-wide uppercase font-mono
	{size === 'md' ? 'px-2.5 py-1 text-xs' : 'px-2 py-0.5 text-[11px]'}
	{isSuccess
		? 'bg-emerald-100 text-emerald-800 dark:bg-emerald-950/80 dark:text-emerald-300 border border-emerald-300 dark:border-emerald-800'
		: isProgress
			? 'bg-blue-100 text-blue-800 dark:bg-blue-950/80 dark:text-blue-300 border border-blue-300 dark:border-blue-800 animate-pulse'
			: isFail
				? 'bg-rose-100 text-rose-800 dark:bg-rose-950/80 dark:text-rose-300 border border-rose-300 dark:border-rose-800'
				: isWarn
					? 'bg-amber-100 text-amber-800 dark:bg-amber-950/80 dark:text-amber-300 border border-amber-300 dark:border-amber-800'
					: 'bg-zinc-100 text-zinc-800 dark:bg-zinc-800 dark:text-zinc-300 border border-zinc-300 dark:border-zinc-700'}"
>
	<span
		class="w-1.5 h-1.5 rounded-full mr-1.5
		{isSuccess
			? 'bg-emerald-500'
			: isProgress
				? 'bg-blue-500'
				: isFail
					? 'bg-rose-500'
					: isWarn
						? 'bg-amber-500'
						: 'bg-zinc-400'}"
	></span>
	{status}
</span>
