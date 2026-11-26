<script lang="ts">
  import { completeGitHubCallback, getSession } from "#lib/api.js";
  import { onMount } from "svelte";

  let status = $state("Completing GitHub App registration...");
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

      const params = new URLSearchParams(window.location.search);
      const code = params.get("code") || "";
      const state = params.get("state") || "";
      if (!code || !state) {
        error = "Missing code or state from GitHub callback.";
        return;
      }

      const res = await completeGitHubCallback({ code, state });
      if (!res.success) {
        error = res.error || "Failed to complete GitHub App registration.";
        return;
      }

      if (res.install_url) {
        status = "Redirecting to install the GitHub App on your account...";
        window.location.href = res.install_url;
      } else {
        window.location.href = "/settings?tab=github";
      }
    } catch (err) {
      error =
        err instanceof Error
          ? err.message
          : "Network error completing GitHub callback.";
    }
  });
</script>

<svelte:head>
  <title>Connecting GitHub &bull; dermaga</title>
</svelte:head>

<div
  class="mx-auto mt-12 max-w-lg space-y-4 rounded-xl border border-stone-200 bg-white p-6 shadow-sm dark:border-stone-800 dark:bg-stone-900"
>
  <h1 class="text-lg font-semibold">Connecting GitHub</h1>
  {#if error}
    <div
      role="alert"
      class="rounded-lg border border-red-200 bg-red-50 p-3 text-xs text-red-800 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300"
    >
      {error}
    </div>
    <a
      href="/settings?tab=github"
      class="inline-block rounded-md bg-brand-primary px-3 py-1.5 text-xs font-medium text-brand-btn-text"
    >
      Back to Settings &rarr; GitHub
    </a>
  {:else}
    <p class="text-sm text-stone-600 dark:text-stone-400">{status}</p>
  {/if}
</div>
