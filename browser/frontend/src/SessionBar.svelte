<script lang="ts">
  import type { main } from '../wailsjs/go/models';

  interface Props {
    status: main.StatusView;
    onleave: () => void;
  }
  let { status, onleave }: Props = $props();

  type Phase = 'error' | 'ingame' | 'contacting' | 'stale' | 'ready';

  const phase: Phase = $derived.by(() => {
    if (status.error) return 'error';
    if (status.connected) return 'ingame';
    if (status.beaconAge < 0) return 'contacting';
    if (status.beaconAge > 15) return 'stale';
    return 'ready';
  });

  const kb = (n: number) => (n < 1024 ? `${n.toFixed(0)} KB` : `${(n / 1024).toFixed(1)} MB`);
</script>

<section class="bar {phase}" aria-live="polite">
  <div class="dot" aria-hidden="true"></div>
  <div class="text">
    <div class="title">
      {#if phase === 'ingame'}
        Playing on <strong>{status.serverName}</strong>
      {:else}
        Joined <strong>{status.serverName}</strong>
      {/if}
    </div>
    <div class="detail">
      {#if phase === 'error'}
        {status.error}
      {:else if phase === 'contacting'}
        Contacting the server…
      {:else if phase === 'stale'}
        The server stopped advertising. It may be offline or restarting.
      {:else if phase === 'ready'}
        In Halo Infinite, open <b>Custom Game → Create Match → Server</b> and click
        {#if status.gameName}<b>{status.gameName}</b>{:else}the host's PC name{/if}.
      {:else}
        Connected · sent {kb(status.upKB)} · received {kb(status.downKB)}
      {/if}
    </div>
  </div>
  <button class="leave" onclick={onleave}>Leave</button>
</section>

<style>
  .bar {
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 14px 18px;
    border-top: 1px solid var(--line);
    background: var(--panel);
  }
  .dot {
    width: 12px;
    height: 12px;
    border-radius: 50%;
    flex: none;
    background: var(--muted);
  }
  .ready .dot { background: var(--accent); }
  .ingame .dot { background: var(--ok); box-shadow: 0 0 10px var(--ok); }
  .stale .dot, .error .dot { background: var(--warn); }
  .contacting .dot { animation: pulse 1s infinite alternate; }
  @keyframes pulse { from { opacity: 0.3; } to { opacity: 1; } }
  .text { flex: 1; min-width: 0; }
  .title { font-size: 15px; }
  .detail { color: var(--muted); font-size: 13px; margin-top: 2px; }
  .error .detail, .stale .detail { color: var(--warn); }
  .leave {
    background: transparent;
    color: var(--text);
    border: 1px solid var(--line);
  }
  .leave:hover { border-color: var(--warn); color: var(--warn); }
</style>
