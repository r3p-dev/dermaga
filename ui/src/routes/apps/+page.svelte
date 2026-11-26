<script lang="ts">
  import { getSession, listApps } from "#lib/api.js";
  import AppsList from "#lib/components/AppsList.svelte";
  import PageHeader from "#lib/components/PageHeader.svelte";
  import type { AppSummary } from "#lib/types/api.js";
  import { onMount } from "svelte";

  let apps = $state<AppSummary[]>([]);
  let loading = $state(true);
  let error = $state<string | null>(null);

  onMount(async () => {
    try {
      const session = await getSession();
      if (session.setup_required) {
        window.location.href = "/setup";
        return;
      }
      if (!session.authenticated) {
        window.location.href = "/login";
        return;
      }
      const res = await listApps();
      apps = res.apps || [];
    } catch (err) {
      error = err instanceof Error ? err.message : "Failed to load apps";
    } finally {
      loading = false;
    }
  });
</script>

<svelte:head>
  <title>Apps &bull; dermaga</title>
</svelte:head>

<div class="space-y-6">
  <PageHeader
    title="Apps"
    subtitle="All deployed applications on this server"
  />
  <AppsList {apps} {loading} {error} />
</div>
