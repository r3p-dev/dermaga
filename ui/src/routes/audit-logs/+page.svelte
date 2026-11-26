<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';

	onMount(() => {
		goto('/activity', { replaceState: true });
	});
</script>

<svelte:head>
	<title>Redirecting to Activity &bull; dermaga</title>
</svelte:head>

<div class="py-12 text-center text-xs text-stone-600 dark:text-stone-400">
	Redirecting to <a href="/activity" class="text-brand-text underline">Activity</a>...
</div>
