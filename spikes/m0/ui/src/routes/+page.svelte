<script lang="ts">
	type Stats = { goVersion: string; sqlite: string; apps: number; rssKB: number };
	let stats = $state<Stats | null>(null);
	let error = $state('');
	$effect(() => {
		fetch('/api/stats')
			.then((r) => r.json())
			.then((s: Stats) => (stats = s))
			.catch((e) => (error = String(e)));
	});
</script>

<main class="mx-auto max-w-xl p-8 font-mono text-sm dark:bg-neutral-900 dark:text-neutral-100">
	<h1 class="mb-4 text-xl font-bold">dermaga spike</h1>
	{#if stats}
		<dl class="grid grid-cols-2 gap-2">
			<dt>Go</dt><dd>{stats.goVersion}</dd>
			<dt>SQLite</dt><dd>{stats.sqlite}</dd>
			<dt>apps rows</dt><dd>{stats.apps}</dd>
			<dt>RSS</dt><dd>{stats.rssKB} KB</dd>
		</dl>
	{:else if error}<p class="text-red-600">{error}</p>{:else}<p>loading…</p>{/if}
	<a class="mt-4 inline-block underline" href="/apps/portfolio">/apps/portfolio (SPA route)</a>
</main>
