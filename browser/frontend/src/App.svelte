<script lang="ts">
  import { onMount } from 'svelte';
  import {
    Ballot,
    CheckUpdate,
    GetSettings,
    Join,
    Leave,
    ListServers,
    LocalBuild,
    SaveSettings,
    SetFavorite,
    Status,
    Version,
    Vote,
  } from '../wailsjs/go/main/App.js';
  import { BrowserOpenURL } from '../wailsjs/runtime/runtime.js';
  import type { main } from '../wailsjs/go/models';
  import SessionBar from './SessionBar.svelte';
  import VotePanel from './VotePanel.svelte';
  import SettingsPanel from './SettingsPanel.svelte';
  import logo from './assets/logo-full.png';

  let servers = $state<main.ServerView[]>([]);
  let settings = $state<main.Settings | null>(null);
  let localBuild = $state('');
  let appVersion = $state('');
  let update = $state<main.UpdateInfo | null>(null);
  let status = $state<main.StatusView | null>(null);
  let ballot = $state<main.BallotView | null>(null);
  let listError = $state('');
  let actionError = $state('');
  let loading = $state(false);
  let joiningId = $state('');
  let showSettings = $state(false);

  // Filters are a per-player convenience, remembered in localStorage.
  type Filters = { region: string; favoritesOnly: boolean; otherVersions: boolean };
  let filters = $state<Filters>(loadFilters());

  function loadFilters(): Filters {
    const fallback = { region: '', favoritesOnly: false, otherVersions: false };
    try {
      return { ...fallback, ...JSON.parse(localStorage.getItem('openlink.filters') ?? '{}') };
    } catch {
      return fallback;
    }
  }

  $effect(() => {
    const snapshot = JSON.stringify(filters);
    try {
      localStorage.setItem('openlink.filters', snapshot);
    } catch {
      /* storage unavailable: filters just reset next launch */
    }
  });

  const needsDirectory = $derived(settings !== null && !settings.directory);
  const regions = $derived([...new Set(servers.map((s) => s.region).filter(Boolean))].sort());
  const visible = $derived(
    servers.filter(
      (s) =>
        (!filters.region || s.region === filters.region) &&
        (!filters.favoritesOnly || s.favorite) &&
        (filters.otherVersions || !localBuild || s.buildMatch || s.id === status?.serverId),
    ),
  );
  const hiddenOtherVersions = $derived(
    localBuild && !filters.otherVersions ? servers.filter((s) => !s.buildMatch).length : 0,
  );

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
    ballot = null;
  }

  async function castVote(round: number, choice: number) {
    await Vote(round, choice);
    ballot = await Ballot();
  }

  async function toggleFavorite(s: main.ServerView) {
    const on = !s.favorite;
    s.favorite = on;
    try {
      await SetFavorite(s.key, on);
    } catch (err) {
      s.favorite = !on;
      actionError = `Could not save favourite: ${err}`;
    }
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
    if (s.reachability === 'unreachable') return "Host's port is closed";
    return '';
  }

  function ping(s: main.ServerView): string {
    if (s.pingMs < 0) return '—';
    return s.pingMs < 1 ? '<1 ms' : `${s.pingMs} ms`;
  }

  // What a server is playing, as its host reports it.
  function matchText(m: main.MatchView): string {
    switch (m.phase) {
      case 'lobby':
        return m.name ? `In lobby · next: ${m.name}` : 'In lobby';
      case 'voting':
        return 'Voting for the next match';
      case 'starting':
        return m.name ? `Starting: ${m.name}` : 'Starting a match';
      case 'in_game':
        return m.name ? `${m.name} · in game` : 'In game';
      case 'post_game':
        return m.name ? `${m.name} · match over` : 'Match over';
    }
    return '';
  }

  // Map thumbnails: which URL is being tried (.jpg, then .png), keyed by the
  // first URL so a new map starts fresh; past the end = no image.
  let thumbTry = $state<Record<string, number>>({});
  const matchThumb = (m: main.MatchView | undefined) =>
    m?.thumbs?.length ? m.thumbs[thumbTry[m.thumbs[0]] ?? 0] : undefined;
  function matchThumbFailed(m: main.MatchView) {
    const key = m.thumbs[0];
    thumbTry[key] = (thumbTry[key] ?? 0) + 1;
  }

  onMount(() => {
    (async () => {
      settings = await GetSettings();
      appVersion = await Version();
      localBuild = await LocalBuild();
      showSettings = !settings.directory;
      status = await Status();
      await refresh();
      const u = await CheckUpdate();
      if (u.available) update = u;
    })();
    const listTimer = setInterval(refresh, 15000);
    const statusTimer = setInterval(async () => {
      status = await Status();
      ballot = status ? await Ballot() : null;
    }, 1000);
    // Faster while a vote is on screen, so the countdown and tallies stay live.
    const ballotTimer = setInterval(async () => {
      if (ballot) ballot = await Ballot();
    }, 250);
    return () => {
      clearInterval(listTimer);
      clearInterval(statusTimer);
      clearInterval(ballotTimer);
    };
  });
</script>

<div class="app">
  <header>
    <img class="brand" src={logo} alt="OpenLink" draggable="false" />
    <div class="meta">
      <span title="OpenLink version">{appVersion}</span>
      <span title="Your game version">{localBuild ? `Game ${localBuild.split('.hi_')[0]}` : 'Game not found'}</span>
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

  {#if update}
    <div class="update">
      OpenLink {update.latest} is available.
      <button class="link" onclick={() => BrowserOpenURL(update!.url)}>Download</button>
      <button class="link dim" onclick={() => (update = null)}>Dismiss</button>
    </div>
  {/if}

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

      <div class="filters">
        <label>
          Region
          <select bind:value={filters.region}>
            <option value="">All</option>
            {#each regions as r (r)}<option value={r}>{r}</option>{/each}
          </select>
        </label>
        <label><input type="checkbox" bind:checked={filters.favoritesOnly} /> Favourites only</label>
        <label><input type="checkbox" bind:checked={filters.otherVersions} /> Other game versions</label>
        {#if hiddenOtherVersions}
          <span class="hint">{hiddenOtherVersions} hidden (different game version)</span>
        {/if}
      </div>

      {#if visible.length === 0 && !listError}
        <p class="empty">
          {#if loading}
            Loading servers…
          {:else if servers.length}
            No servers match these filters.
          {:else}
            No servers are online right now.
          {/if}
        </p>
      {:else}
        <table>
          <thead>
            <tr>
              <th class="fav" aria-label="Favourite"></th>
              <th>Server</th><th>Region</th><th>Players</th><th>Ping</th><th>Status</th><th></th>
            </tr>
          </thead>
          <tbody>
            {#each visible as s (s.id)}
              {@const blocked = joinBlockedReason(s)}
              {@const current = status?.serverId === s.id}
              <tr class:current class:dim={!!blocked}>
                <td class="fav">
                  <button
                    class="star"
                    class:on={s.favorite}
                    onclick={() => toggleFavorite(s)}
                    aria-label={s.favorite ? 'Remove from favourites' : 'Add to favourites'}
                    aria-pressed={s.favorite}>{s.favorite ? '★' : '☆'}</button
                  >
                </td>
                <td class="name">
                  <div class="server">
                    {#if s.match}
                      {@const m = s.match}
                      {@const thumb = matchThumb(m)}
                      <div class="mthumb">
                        {#if thumb}
                          <img src={thumb} alt="" loading="lazy" decoding="async" onerror={() => matchThumbFailed(m)} />
                        {/if}
                      </div>
                    {/if}
                    <div class="server-text">
                      <div>{s.name}</div>
                      {#if s.match}<div class="match" class:live={s.match.phase === 'in_game'}>{matchText(s.match)}</div>{/if}
                    </div>
                  </div>
                </td>
                <td>{s.region || '—'}</td>
                <td>{s.players >= 0 ? s.players : '—'}</td>
                <td>{ping(s)}</td>
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

  {#if status && ballot}
    <VotePanel {ballot} onvote={castVote} />
  {/if}
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
  .brand { display: block; height: 32px; width: auto; user-select: none; }
  .meta { display: flex; gap: 10px; font-size: 12px; color: var(--muted); }
  .spacer { flex: 1; }
  .ghost { background: transparent; border: 1px solid var(--line); color: var(--text); }
  .update {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 18px;
    background: var(--button-bg-hover);
    border-bottom: 1px solid var(--line);
  }
  .link { background: none; border: none; padding: 0; color: var(--accent); font-weight: 600; }
  .link.dim { color: var(--muted); font-weight: 400; }
  main { flex: 1; overflow: auto; padding: 8px 18px 18px; }
  .intro { max-width: 560px; margin: 24px auto 0; padding: 0 24px; }
  .notice { margin: 12px 0; padding: 10px 12px; border-radius: 8px; background: var(--panel); color: var(--muted); }
  .notice.warn { color: var(--warn); }
  .filters { display: flex; align-items: center; gap: 18px; flex-wrap: wrap; margin: 10px 0 2px; font-size: 13px; }
  .filters label { display: flex; align-items: center; gap: 6px; color: var(--muted); }
  .filters select {
    font: inherit;
    padding: 4px 8px;
    border-radius: 6px;
    border: 1px solid var(--line);
    background: var(--bg);
    color: var(--text);
  }
  .hint { color: var(--muted); font-size: 12px; }
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
  .fav { width: 1%; padding-right: 0; }
  .star { background: none; border: none; padding: 0 2px; font-size: 18px; line-height: 1; color: var(--muted); }
  .star.on { color: #ffd166; }
  .name { font-weight: 600; }
  .server { display: flex; align-items: center; gap: 12px; }
  .server-text { min-width: 0; }
  .mthumb {
    flex: 0 0 auto;
    width: 64px;
    aspect-ratio: 16 / 9;
    border-radius: 4px;
    overflow: hidden;
    background: var(--bg);
  }
  .mthumb img { width: 100%; height: 100%; object-fit: cover; display: block; }
  .match { font-weight: 400; font-size: 12px; color: var(--muted); margin-top: 2px; }
  .match.live { color: var(--accent); }
  tr.dim .mthumb { opacity: 0.5; }
  .state { color: var(--muted); font-size: 13px; }
  .act { text-align: right; width: 1%; white-space: nowrap; }
  tr.dim .name { color: var(--faint); }
  tr.current { background: color-mix(in srgb, var(--button-bg) 45%, transparent); }
  .badge { font-size: 12px; color: var(--accent); }
</style>
