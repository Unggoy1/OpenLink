<script lang="ts">
  import { untrack } from 'svelte';
  import { main } from '../wailsjs/go/models';
  import { Diagnostics, OverlayKeyConflicts, OverlaySupported } from '../wailsjs/go/main/App.js';
  import { ClipboardSetText } from '../wailsjs/runtime/runtime.js';
  import HotkeyInput from './HotkeyInput.svelte';

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
  let voteSound = $state(!initial.muteVoteSound);
  let overlayMode = $state(initial.overlayMode || 'off');
  let overlayCorner = $state(initial.overlayCorner || 'top-left');
  let openKey = $state(initial.overlayOpenKey);
  let voteKeys = $state([...(initial.overlayVoteKeys ?? [])]);
  let controllerVote = $state(!initial.overlayNoController);
  let error = $state('');
  let saving = $state(false);

  // The overlay is Windows-only; other builds hide the section.
  let overlaySupported = $state(false);
  OverlaySupported().then((ok) => (overlaySupported = ok));

  // Keys another app already holds, checked whenever the shown keys change.
  let taken = $state<string[]>([]);
  $effect(() => {
    const keys = overlayMode === 'passive' ? [...voteKeys] : overlayMode === 'interactive' ? [openKey] : [];
    let stale = false;
    OverlayKeyConflicts(keys).then((t) => {
      if (!stale) taken = t ?? [];
    });
    return () => (stale = true);
  });

  let copying = $state(false);
  let copied = $state('');
  async function copyDiagnostics() {
    copying = true;
    copied = '';
    try {
      const ok = await ClipboardSetText(await Diagnostics());
      copied = ok ? 'Copied. Paste it where you ask for help.' : 'Could not copy to the clipboard.';
    } catch (err) {
      copied = String(err);
    } finally {
      copying = false;
    }
  }

  async function save(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    saving = true;
    try {
      await onsave(
        new main.Settings({
          directory,
          mode,
          installDir,
          muteVoteSound: !voteSound,
          overlayMode,
          overlayCorner,
          overlayOpenKey: openKey,
          overlayVoteKeys: voteKeys,
          overlayNoController: !controllerVote,
        }),
      );
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
    <small>Found automatically in your Steam libraries. Set it only if the game is installed outside Steam's library list (the folder containing version.txt).</small>
  </label>

  <fieldset>
    <legend>How servers are shown to the game</legend>
    <label class="radio">
      <input type="radio" bind:group={mode} value="loopback" />
      <span>Loopback (recommended)</span>
    </label>
    <label class="radio">
      <input type="radio" bind:group={mode} value="broadcast" />
      <span>LAN broadcast: if servers never appear in game (used automatically when a server runs on this PC)</span>
    </label>
  </fieldset>

  <label class="radio">
    <input type="checkbox" bind:checked={voteSound} />
    <span>Play a sound when a vote for the next match opens</span>
  </label>

  {#if overlaySupported}
    <fieldset>
      <legend>In-game vote overlay</legend>
      <label class="radio">
        <input type="radio" bind:group={overlayMode} value="off" />
        <span>Off: vote in this app only</span>
      </label>
      <label class="radio">
        <input type="radio" bind:group={overlayMode} value="passive" />
        <span>Passive (recommended): shows over the game by itself; vote with a hotkey for each choice</span>
      </label>
      <label class="radio">
        <input type="radio" bind:group={overlayMode} value="interactive" />
        <span>Interactive: a hotkey opens it over the game; vote with 1–4 or the mouse, Esc to go back</span>
      </label>

      {#if overlayMode !== 'off'}
        <div class="overlay-opts">
          {#if overlayMode === 'passive'}
            {#each voteKeys as _, i (i)}
              <HotkeyInput label="Choice {i + 1}" bind:value={voteKeys[i]} taken={taken.includes(voteKeys[i])} />
            {/each}
          {:else}
            <HotkeyInput label="Open the overlay" bind:value={openKey} taken={taken.includes(openKey)} />
          {/if}
          <div class="row">
            <span class="label">Position</span>
            <select bind:value={overlayCorner}>
              <option value="top-left">Top left</option>
              <option value="top-right">Top right</option>
              <option value="bottom-left">Bottom left</option>
              <option value="bottom-right">Bottom right</option>
            </select>
          </div>
          <label class="radio">
            <input type="checkbox" bind:checked={controllerVote} />
            <span>Vote with a controller: hold View and press the D-pad (↑ 1, → 2, ↓ 3, ← 4)</span>
          </label>
          {#if taken.length}
            <p class="error">{taken.join(', ')} {taken.length === 1 ? 'is' : 'are'} already used by another app. Pick a different key.</p>
          {/if}
          <small>
            Shows only while Halo is in front and a vote is open, and holds its hotkeys only then. Halo must run
            borderless or windowed (it has no exclusive full-screen mode). OpenLink can be started before or after the
            game.
          </small>
        </div>
      {/if}
    </fieldset>
  {/if}

  <fieldset>
    <legend>Help</legend>
    <div class="row">
      <button type="button" class="secondary" onclick={copyDiagnostics} disabled={copying}>
        {copying ? 'Collecting…' : 'Copy diagnostics'}
      </button>
      {#if copied}<small>{copied}</small>{/if}
    </div>
    <small>
      Copies a report of your versions, settings and recent connection events to paste when you ask for help. Public IP
      addresses are removed.
    </small>
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
  .overlay-opts { display: flex; flex-direction: column; gap: 8px; margin: 8px 0 4px 26px; }
  .row { display: flex; align-items: center; gap: 12px; }
  .row .label { flex: 0 0 120px; font-size: 13px; color: var(--muted); }
  select {
    font: inherit;
    padding: 6px 10px;
    border-radius: 6px;
    border: 1px solid var(--line);
    background: var(--panel);
    color: var(--text);
  }
</style>
