<script lang="ts">
  import { onMount } from 'svelte';
  import { GetSettings, Join, Leave, ListServers, LocalBuild, SaveSettings, Status } from '../wailsjs/go/main/App.js';
  import type { main } from '../wailsjs/go/models';
  import SessionBar from './SessionBar.svelte';
  import SettingsPanel from './SettingsPanel.svelte';

  let servers = $state<main.ServerView[]>([]);
  let settings = $state<main.Settings | null>(null);
  let localBuild = $state('');
  let status = $state<main.StatusView | null>(null);
  let listError = $state('');
  let actionError = $state('');
  let loading = $state(false);
  let joiningId = $state('');
  let showSettings = $state(false);

  const needsDirectory = $derived(settings !== null && !settings.directory);

  async function refresh() {
    if (!settings?.directory) return;
    loading = true;
    try {
      servers = (await ListServers()) ?? [];
      listError = '';
    } catch (err) {
      listError = `Could not reach the server directory: ${err}`;
    } finally {
      loading = false;
    }
  }

  async function join(s: main.ServerView) {
    actionError = '';
    joiningId = s.id;
    try {
      await Join(s.id);
      status = await Status();
    } catch (err) {
      actionError = String(err);
    } finally {
      joiningId = '';
    }
  }

  async function leave() {
    await Leave();
    status = null;
  }

  async function saveSettings(s: main.Settings) {
    await SaveSettings(s);
    settings = await GetSettings();
    localBuild = await LocalBuild();
    showSettings = false;
    await refresh();
  }

  function joinBlockedReason(s: main.ServerView): string {
    if (!s.joinable) return s.status === 'ready' ? 'Not advertising right now' : `Server is ${s.status}`;
    if (localBuild && !s.buildMatch) return 'Different game version';
    return '';
  }

  onMount(() => {
    (async () => {
      settings = await GetSettings();
      localBuild = await LocalBuild();
      showSettings = !settings.directory;
      status = await Status();
      await refresh();
    })();
    const listTimer = setInterval(refresh, 15000);
    const statusTimer = setInterval(async () => {
      status = await Status();
    }, 1000);
    return () => {
      clearInterval(listTimer);
      clearInterval(statusTimer);
    };
  });
</script>

<div class="app">
  <header>
    <div class="brand">OpenLink</div>
    <div class="build" title="Your game version">
      {localBuild ? `Game ${localBuild.split('.hi_')[0]}` : 'Game not found'}
    </div>
    <div class="spacer"></div>
    {#if !showSettings}
      <button class="ghost" onclick={refresh} disabled={loading || needsDirectory}>
        {loading ? 'Refreshing…' : 'Refresh'}
      </button>
    {/if}
    <button class="ghost" onclick={() => (showSettings = !showSettings)} disabled={needsDirectory && showSettings}>
      {showSettings ? 'Back' : 'Settings'}
    </button>
  </header>

  <main>
    {#if showSettings && settings}
      {#if needsDirectory}
        <p class="intro">Enter the address of a community server directory to get started.</p>
      {/if}
      <SettingsPanel {settings} onsave={saveSettings} oncancel={() => (showSettings = false)} />
    {:else}
      {#if !localBuild}
        <p class="notice">
          Halo Infinite was not found, so game versions can't be checked. Set the game folder in Settings.
        </p>
      {/if}
      {#if listError}<p class="notice warn">{listError}</p>{/if}
      {#if actionError}<p class="notice warn">{actionError}</p>{/if}

      {#if servers.length === 0 && !listError}
        <p class="empty">{loading ? 'Loading servers…' : 'No servers are online right now.'}</p>
      {:else}
        <table>
          <thead>
            <tr><th>Server</th><th>Region</th><th>Players</th><th>Status</th><th></th></tr>
          </thead>
          <tbody>
            {#each servers as s (s.id)}
              {@const blocked = joinBlockedReason(s)}
              {@const current = status?.serverId === s.id}
              <tr class:current class:dim={!!blocked}>
                <td class="name">{s.name}</td>
                <td>{s.region || '—'}</td>
                <td>{s.players >= 0 ? s.players : '—'}</td>
                <td class="state">{blocked || 'Joinable'}</td>
                <td class="act">
                  {#if current}
                    <span class="badge">Joined</span>
                  {:else}
                    <button onclick={() => join(s)} disabled={!!blocked || joiningId !== ''} title={blocked}>
                      {joiningId === s.id ? 'Joining…' : 'Join'}
                    </button>
                  {/if}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    {/if}
  </main>

  {#if status}
    <SessionBar {status} onleave={leave} />
  {/if}
</div>

<style>
  .app { display: flex; flex-direction: column; height: 100vh; }
  header {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 18px;
    border-bottom: 1px solid var(--line);
    background: var(--panel);
  }
  .brand { font-weight: 700; font-size: 18px; letter-spacing: 0.04em; color: var(--accent); }
  .build { font-size: 12px; color: var(--muted); }
  .spacer { flex: 1; }
  .ghost { background: transparent; border: 1px solid var(--line); color: var(--text); }
  main { flex: 1; overflow: auto; padding: 8px 18px 18px; }
  .intro { max-width: 560px; margin: 24px auto 0; padding: 0 24px; }
  .notice { margin: 12px 0; padding: 10px 12px; border-radius: 8px; background: var(--panel); color: var(--muted); }
  .notice.warn { color: var(--warn); }
  .empty { color: var(--muted); text-align: center; margin-top: 64px; }
  table { width: 100%; border-collapse: collapse; margin-top: 8px; }
  th {
    text-align: left;
    font-weight: 500;
    font-size: 12px;
    color: var(--muted);
    padding: 8px 10px;
    border-bottom: 1px solid var(--line);
  }
  td { padding: 12px 10px; border-bottom: 1px solid var(--line); }
  .name { font-weight: 600; }
  .state { color: var(--muted); font-size: 13px; }
  .act { text-align: right; width: 1%; white-space: nowrap; }
  tr.dim .name { color: var(--muted); }
  tr.current { background: color-mix(in srgb, var(--accent) 10%, transparent); }
  .badge { font-size: 12px; color: var(--accent); }
</style>
