<script lang="ts">
  import { untrack } from 'svelte';
  import { main } from '../wailsjs/go/models';

  interface Props {
    settings: main.Settings;
    onsave: (s: main.Settings) => Promise<void>;
    oncancel: () => void;
  }
  let { settings, onsave, oncancel }: Props = $props();

  // Editable copy taken once when the panel opens; the parent's settings
  // change only on a successful save.
  const initial = untrack(() => ({ ...settings }));
  let directory = $state(initial.directory);
  let mode = $state(initial.mode || 'loopback');
  let installDir = $state(initial.installDir);
  let error = $state('');
  let saving = $state(false);

  async function save(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    saving = true;
    try {
      await onsave(new main.Settings({ directory, mode, installDir }));
    } catch (err) {
      error = String(err);
    } finally {
      saving = false;
    }
  }
</script>

<form class="settings" onsubmit={save}>
  <h2>Settings</h2>

  <label>
    <span>Server directory</span>
    <input bind:value={directory} placeholder="https://…" spellcheck="false" autocomplete="off" />
    <small>The community server list to browse.</small>
  </label>

  <label>
    <span>Game folder</span>
    <input bind:value={installDir} placeholder="Found automatically" spellcheck="false" autocomplete="off" />
    <small>Only needed if your Steam library is in an unusual place (the folder containing version.txt).</small>
  </label>

  <fieldset>
    <legend>How servers are shown to the game</legend>
    <label class="radio">
      <input type="radio" bind:group={mode} value="loopback" />
      <span>Loopback (recommended)</span>
    </label>
    <label class="radio">
      <input type="radio" bind:group={mode} value="broadcast" />
      <span>LAN broadcast: try this only if servers never appear in game</span>
    </label>
  </fieldset>

  {#if error}<p class="error">{error}</p>{/if}

  <div class="actions">
    <button type="button" class="secondary" onclick={oncancel}>Cancel</button>
    <button type="submit" disabled={saving}>{saving ? 'Saving…' : 'Save'}</button>
  </div>
</form>

<style>
  .settings {
    max-width: 560px;
    margin: 0 auto;
    padding: 24px;
    display: flex;
    flex-direction: column;
    gap: 18px;
  }
  h2 { margin: 0; font-size: 20px; }
  label { display: flex; flex-direction: column; gap: 6px; }
  label > span, legend { font-size: 13px; color: var(--muted); }
  small { color: var(--muted); font-size: 12px; }
  fieldset { border: 1px solid var(--line); border-radius: 8px; padding: 10px 14px; margin: 0; }
  .radio { flex-direction: row; align-items: center; gap: 8px; padding: 4px 0; }
  .radio span { color: var(--text); font-size: 14px; }
  .error { color: var(--warn); margin: 0; }
  .actions { display: flex; justify-content: flex-end; gap: 10px; }
  .secondary { background: transparent; border: 1px solid var(--line); color: var(--text); }
</style>
