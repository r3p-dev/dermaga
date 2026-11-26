<script lang="ts">
  import {
    getGitHubStatus,
    startGitHubManifest,
    syncGitHub,
    disconnectGitHub,
  } from "#lib/api.js";
  import type { GitHubStatusResponse } from "#lib/types/api.js";
  import { onMount } from "svelte";

  interface Props {
    onSwitchToHookDomain?: () => void;
  }

  let { onSwitchToHookDomain }: Props = $props();

  let status = $state<GitHubStatusResponse | null>(null);
  let loading = $state(true);
  let busy = $state(false);
  let syncing = $state(false);
  let disconnecting = $state(false);
  let error = $state<string | null>(null);
  let message = $state<string | null>(null);
  let confirmText = $state("");
  let deletedAppURL = $state<string | null>(null);

  async function loadStatus() {
    loading = true;
    error = null;
    try {
      status = await getGitHubStatus();
    } catch (err) {
      error =
        err instanceof Error ? err.message : "Failed to load GitHub settings";
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    loadStatus();
  });

  async function handleConnect() {
    busy = true;
    error = null;
    deletedAppURL = null;
    try {
      const res = await startGitHubManifest();
      const form = document.createElement("form");
      form.method = "POST";
      form.action = res.action_url;
      form.style.display = "none";

      const input = document.createElement("input");
      input.type = "hidden";
      input.name = "manifest";
      input.value = res.manifest_json;

      form.appendChild(input);
      document.body.appendChild(form);
      form.submit();
    } catch (err) {
      error =
        err instanceof Error
          ? err.message
          : "Failed to start GitHub App creation";
      busy = false;
    }
  }

  async function handleSync() {
    syncing = true;
    error = null;
    message = null;
    try {
      const res = await syncGitHub();
      if (res.error) {
        error = res.error;
      } else {
        status = res;
        message = "Installations and repositories refreshed from GitHub.";
      }
    } catch (err) {
      error =
        err instanceof Error ? err.message : "Failed to refresh from GitHub";
    } finally {
      syncing = false;
    }
  }

  async function handleDisconnect(e: SubmitEvent) {
    e.preventDefault();
    disconnecting = true;
    error = null;
    message = null;
    try {
      const res = await disconnectGitHub({ confirm: confirmText.trim() });
      if (res.success) {
        confirmText = "";
        deletedAppURL =
          res.delete_app_url || "https://github.com/settings/apps";
        await loadStatus();
      } else {
        error = res.error || "Failed to disconnect GitHub";
      }
    } catch (err) {
      error = err instanceof Error ? err.message : "Network error";
    } finally {
      disconnecting = false;
    }
  }
</script>

{#if loading}
  <div class="py-4 text-sm text-stone-500">Loading GitHub connection...</div>
{:else if status}
  <div class="space-y-6">
    {#if error}
      <div
        role="alert"
        class="rounded-lg border border-red-200 bg-red-50 p-3 text-xs text-red-800 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300"
      >
        {error}
      </div>
    {/if}
    {#if message}
      <div
        class="rounded-lg border border-emerald-200 bg-emerald-50 p-3 text-xs text-emerald-800 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300"
      >
        {message}
      </div>
    {/if}
    {#if deletedAppURL}
      <div
        class="space-y-2 rounded-xl border border-amber-300 bg-amber-50 p-4 text-xs text-amber-950 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-200"
      >
        <p class="font-semibold">GitHub App disconnected from this server.</p>
        <p>
          Stored keys and secrets have been deleted from dermaga. Existing live
          apps keep serving their current release, and new deploys are paused
          until GitHub is connected again.
        </p>
        <p>
          To also delete the App on GitHub, open
          <a
            href={deletedAppURL}
            target="_blank"
            rel="noopener noreferrer"
            class="font-medium underline"
          >
            GitHub App Advanced Settings
          </a>
          and choose <strong>Delete GitHub App</strong>.
        </p>
      </div>
    {/if}

    {#if !status.connected}
      <section
        class="max-w-2xl space-y-5 rounded-xl border border-stone-200 bg-white p-6 shadow-sm dark:border-stone-800 dark:bg-stone-900"
      >
        <div>
          <h2 class="text-base font-semibold">Connect GitHub</h2>
          <p class="mt-1 text-xs text-stone-600 dark:text-stone-400">
            Create a private GitHub App owned by your account so dermaga can
            read selected repositories and receive push webhooks.
          </p>
        </div>

        {#if !status.hook_domain}
          <div
            class="space-y-2.5 rounded-lg border border-amber-300 bg-amber-50 p-4 text-xs text-amber-950 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-200"
          >
            <p class="font-medium">Hook domain required first</p>
            <p>
              Set a reserved hook domain before connecting GitHub so the App can
              be created with its webhook URL from the start.
            </p>
            {#if onSwitchToHookDomain}
              <button
                type="button"
                onclick={onSwitchToHookDomain}
                class="rounded-md bg-amber-900 px-3 py-1.5 text-xs font-medium text-white dark:bg-amber-200 dark:text-stone-900"
              >
                Configure Hook Domain
              </button>
            {:else}
              <a
                href="/settings?tab=hook-domain"
                class="inline-block font-medium underline"
              >
                Go to Settings &rarr; Hook Domain
              </a>
            {/if}
          </div>
        {:else}
          <div
            class="space-y-3 rounded-lg border border-stone-200 bg-stone-50 p-4 text-xs dark:border-stone-800 dark:bg-stone-950"
          >
            <p class="font-medium text-stone-800 dark:text-stone-200">
              What happens next on GitHub:
            </p>
            <ol
              class="list-inside list-decimal space-y-1.5 text-stone-600 dark:text-stone-400"
            >
              <li>
                <strong>Create private App:</strong> Confirm creating a private
                GitHub App with read-only
                <code class="font-mono">Contents</code> and
                <code class="font-mono">Metadata</code>
                permissions and
                <code class="font-mono">push</code> webhooks to
                <code class="font-mono">
                  https://{status.hook_domain}/hooks/github-app
                </code>.
              </li>
              <li>
                <strong>Install on your account or organization:</strong> Choose which
                repositories dermaga may access.
              </li>
              <li>
                <strong>Return to dermaga:</strong> You are redirected back to
                <code class="font-mono">http://localhost:7070</code>
                with your repositories ready to deploy.
              </li>
            </ol>
          </div>

          <button
            type="button"
            onclick={handleConnect}
            disabled={busy}
            class="rounded-md bg-brand-primary px-4 py-2 text-sm font-medium text-brand-btn-text transition hover:bg-brand-hover disabled:opacity-50"
          >
            {busy ? "Redirecting to GitHub..." : "Connect GitHub"}
          </button>
        {/if}
      </section>
    {:else if status.app}
      <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <section
          class="space-y-4 rounded-xl border border-stone-200 bg-white p-6 shadow-sm dark:border-stone-800 dark:bg-stone-900"
        >
          <div class="flex items-start justify-between gap-3">
            <div>
              <h2 class="text-base font-semibold">
                {status.app.name || status.app.slug}
              </h2>
              <p class="mt-0.5 text-xs text-stone-600 dark:text-stone-400">
                App ID <span class="font-mono">{status.app.id}</span> &bull;
                Slug <span class="font-mono">{status.app.slug}</span>
              </p>
            </div>
            <span
              class="rounded-full bg-emerald-100 px-2 py-0.5 text-xs font-medium text-emerald-800 dark:bg-emerald-950/60 dark:text-emerald-300"
            >
              Connected
            </span>
          </div>

          <dl
            class="space-y-2 border-t border-stone-200 pt-3 text-xs dark:border-stone-800"
          >
            <div class="flex flex-wrap justify-between gap-2">
              <dt class="text-stone-600 dark:text-stone-400">
                Webhook endpoint
              </dt>
              <dd
                class="font-mono break-all text-stone-900 dark:text-stone-100"
              >
                https://{status.hook_domain}/hooks/github-app
              </dd>
            </div>
            <div class="flex flex-wrap justify-between gap-2">
              <dt class="text-stone-600 dark:text-stone-400">Last delivery</dt>
              <dd class="text-stone-900 dark:text-stone-100">
                {#if status.last_delivery}
                  <span class="font-mono">{status.last_delivery.event}</span>
                  ({status.last_delivery.status}) &bull; {status.last_delivery
                    .received_at}
                {:else}
                  No webhook deliveries received yet
                {/if}
              </dd>
            </div>
          </dl>

          <div class="flex flex-wrap gap-2 pt-1">
            <a
              href={status.app.install_url}
              target="_blank"
              rel="noopener noreferrer"
              class="rounded-md bg-brand-primary px-3 py-1.5 text-xs font-medium text-brand-btn-text transition hover:bg-brand-hover"
            >
              Manage Repositories on GitHub
            </a>
            <button
              type="button"
              onclick={handleSync}
              disabled={syncing}
              class="rounded-md bg-stone-100 px-3 py-1.5 text-xs font-medium transition hover:bg-stone-200 disabled:opacity-50 dark:bg-stone-800 dark:hover:bg-stone-700"
            >
              {syncing ? "Refreshing..." : "Refresh"}
            </button>
            {#if status.app.html_url}
              <a
                href={status.app.html_url}
                target="_blank"
                rel="noopener noreferrer"
                class="rounded-md border border-stone-300 px-3 py-1.5 text-xs font-medium transition hover:bg-stone-50 dark:border-stone-700 dark:hover:bg-stone-800"
              >
                View App on GitHub
              </a>
            {/if}
          </div>
        </section>

        <section
          class="space-y-4 rounded-xl border border-stone-200 bg-white p-6 shadow-sm dark:border-stone-800 dark:bg-stone-900"
        >
          <h2 class="text-base font-semibold">Installations</h2>
          {#if status.installations.length === 0}
            <div class="space-y-2 text-xs text-stone-600 dark:text-stone-400">
              <p>
                This GitHub App is not installed on any account or organization
                yet.
              </p>
              <a
                href={status.app.install_url}
                target="_blank"
                rel="noopener noreferrer"
                class="inline-block font-medium text-brand-text underline dark:text-brand-primary"
              >
                Install on GitHub
              </a>
            </div>
          {:else}
            <div class="divide-y divide-stone-200 dark:divide-stone-800">
              {#each status.installations as inst}
                <div
                  class="flex items-center justify-between gap-3 py-2.5 text-xs"
                >
                  <div>
                    <div class="font-medium text-stone-900 dark:text-stone-100">
                      {inst.account_login}
                    </div>
                    <div class="text-stone-600 dark:text-stone-400">
                      {inst.account_type} &bull; {inst.repository_selection ===
                      "all"
                        ? "All repositories"
                        : "Selected repositories"}
                    </div>
                  </div>
                  <div class="flex items-center gap-2">
                    {#if inst.suspended_at}
                      <span
                        class="rounded-full bg-amber-100 px-2 py-0.5 text-amber-900 dark:bg-amber-950/60 dark:text-amber-300"
                      >
                        Suspended
                      </span>
                    {/if}
                    {#if inst.html_url}
                      <a
                        href={inst.html_url}
                        target="_blank"
                        rel="noopener noreferrer"
                        class="text-stone-700 underline dark:text-stone-300"
                      >
                        Configure
                      </a>
                    {/if}
                  </div>
                </div>
              {/each}
            </div>
          {/if}
        </section>
      </div>

      <section
        class="space-y-4 rounded-xl border border-stone-200 bg-white p-6 shadow-sm dark:border-stone-800 dark:bg-stone-900"
      >
        <div class="flex items-center justify-between gap-3">
          <h2 class="text-base font-semibold">
            Accessible Repositories ({status.repositories.length})
          </h2>
        </div>

        {#if status.repositories.length === 0}
          <div class="space-y-2 text-xs text-stone-600 dark:text-stone-400">
            <p>No repositories are visible to the GitHub App yet.</p>
            <p>
              Grant access on GitHub using
              <a
                href={status.app.install_url}
                target="_blank"
                rel="noopener noreferrer"
                class="font-medium underline"
              >
                Manage Repositories on GitHub
              </a>, then click <strong>Refresh</strong>.
            </p>
          </div>
        {:else}
          <div class="hidden overflow-x-auto md:block">
            <table class="w-full text-left text-xs">
              <thead>
                <tr
                  class="border-b border-stone-200 text-stone-600 dark:border-stone-800 dark:text-stone-400"
                >
                  <th class="py-2 pr-4 font-medium">Repository</th>
                  <th class="py-2 pr-4 font-medium">Visibility</th>
                  <th class="py-2 pr-4 font-medium">Default Branch</th>
                  <th class="py-2 text-right font-medium">GitHub</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-stone-200 dark:divide-stone-800">
                {#each status.repositories as repo}
                  <tr>
                    <td
                      class="py-2.5 pr-4 font-mono text-stone-900 dark:text-stone-100"
                    >
                      {repo.full_name}
                    </td>
                    <td class="py-2.5 pr-4 text-stone-600 dark:text-stone-400">
                      {repo.private ? "Private" : "Public"}
                    </td>
                    <td
                      class="py-2.5 pr-4 font-mono text-stone-600 dark:text-stone-400"
                    >
                      {repo.default_branch}
                    </td>
                    <td class="py-2.5 text-right">
                      {#if repo.html_url}
                        <a
                          href={repo.html_url}
                          target="_blank"
                          rel="noopener noreferrer"
                          class="text-stone-700 underline dark:text-stone-300"
                        >
                          Open
                        </a>
                      {/if}
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>

          <div
            class="divide-y divide-stone-200 md:hidden dark:divide-stone-800"
          >
            {#each status.repositories as repo}
              <div class="space-y-1 py-3 text-xs">
                <div class="flex items-center justify-between gap-2">
                  <span
                    class="font-mono font-medium break-all text-stone-900 dark:text-stone-100"
                  >
                    {repo.full_name}
                  </span>
                  <span
                    class="shrink-0 rounded bg-stone-100 px-2 py-0.5 text-stone-700 dark:bg-stone-800 dark:text-stone-300"
                  >
                    {repo.private ? "Private" : "Public"}
                  </span>
                </div>
                <div
                  class="flex items-center justify-between text-stone-600 dark:text-stone-400"
                >
                  <span>
                    Branch: <code class="font-mono">{repo.default_branch}</code>
                  </span>
                  {#if repo.html_url}
                    <a
                      href={repo.html_url}
                      target="_blank"
                      rel="noopener noreferrer"
                      class="underline"
                    >
                      Open
                    </a>
                  {/if}
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </section>

      <section
        class="max-w-2xl space-y-3 rounded-xl border border-red-200 bg-white p-6 shadow-sm dark:border-red-900 dark:bg-stone-900"
      >
        <h2 class="text-base font-semibold text-red-700 dark:text-red-400">
          Disconnect GitHub
        </h2>
        <p class="text-xs text-stone-600 dark:text-stone-400">
          Removes the stored GitHub App private key, webhook secret, and
          repository cache from this server. Running apps keep serving their
          live release; new deploys are refused until GitHub is connected again.
        </p>
        <form onsubmit={handleDisconnect} class="space-y-3">
          <div>
            <label
              for="confirm-disconnect"
              class="mb-1 block text-xs font-medium text-stone-700 dark:text-stone-300"
            >
              Type <code class="font-mono">disconnect</code> to confirm
            </label>
            <input
              id="confirm-disconnect"
              type="text"
              bind:value={confirmText}
              placeholder="disconnect"
              class="w-full max-w-xs rounded-md border border-stone-300 bg-white px-3 py-2 text-sm dark:border-stone-700 dark:bg-stone-950"
            />
          </div>
          <button
            type="submit"
            disabled={disconnecting || confirmText.trim() !== "disconnect"}
            class="rounded-md bg-red-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-red-700 disabled:opacity-50"
          >
            {disconnecting ? "Disconnecting..." : "Disconnect GitHub"}
          </button>
        </form>
      </section>
    {/if}
  </div>
{/if}
