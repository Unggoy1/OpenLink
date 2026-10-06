<script lang="ts">
  // Records a hotkey such as "Ctrl+Alt+1". The key names must match what the
  // Go side parses (overlay_keys.go): A-Z, 0-9, Num0-Num9, F1-F24, ` and a few
  // navigation keys, after the modifiers Ctrl, Alt, Shift, Win.
  interface Props {
    value: string;
    label: string;
    taken?: boolean; // another app holds this key
  }
  let { value = $bindable(), label, taken = false }: Props = $props();

  let recording = $state(false);

  const named: Record<string, string> = {
    Backquote: '`',
    Insert: 'Insert',
    Delete: 'Delete',
    Home: 'Home',
    End: 'End',
    PageUp: 'PageUp',
    PageDown: 'PageDown',
    Pause: 'Pause',
  };

  function keyName(code: string): string | null {
    if (/^Key[A-Z]$/.test(code)) return code.slice(3);
    if (/^Digit[0-9]$/.test(code)) return code.slice(5);
    if (/^Numpad[0-9]$/.test(code)) return 'Num' + code.slice(6);
    if (/^F([1-9]|1[0-9]|2[0-4])$/.test(code)) return code;
    return named[code] ?? null;
  }

  function onkeydown(e: KeyboardEvent) {
    if (!recording) return;
    e.preventDefault();
    e.stopPropagation();
    if (e.code === 'Escape') {
      recording = false;
      return;
    }
    const key = keyName(e.code);
    if (!key) return; // a modifier on its own, or a key we can't use: keep listening
    const mods = [e.ctrlKey && 'Ctrl', e.altKey && 'Alt', e.shiftKey && 'Shift', e.metaKey && 'Win'].filter(Boolean);
    value = [...mods, key].join('+');
    recording = false;
  }
</script>

<div class="row">
  <span class="label">{label}</span>
  <button
    type="button"
    class="hotkey"
    class:recording
    class:taken
    aria-label="{label}: {value}. Click, then press the new keys."
    onclick={() => (recording = !recording)}
    {onkeydown}
    onblur={() => (recording = false)}
  >
    {recording ? 'Press keys… (Esc cancels)' : value}
  </button>
</div>

<style>
  .row { display: flex; align-items: center; gap: 12px; }
  .label { flex: 0 0 120px; font-size: 13px; color: var(--muted); }
  .hotkey {
    min-width: 180px;
    text-align: left;
    background: var(--panel);
    color: var(--text);
    border: 1px solid var(--line);
    font-weight: 500;
    font-variant-numeric: tabular-nums;
  }
  .hotkey:hover { border-color: var(--accent); }
  .hotkey.recording { border-color: var(--accent); color: var(--accent); }
  .hotkey.taken { border-color: var(--warn); }
</style>
