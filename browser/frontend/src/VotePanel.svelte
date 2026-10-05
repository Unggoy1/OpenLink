<script lang="ts">
  import type { main } from '../wailsjs/go/models';

  interface Props {
    ballot: main.BallotView;
    onvote: (round: number, choice: number) => Promise<void>;
  }
  let { ballot, onvote }: Props = $props();

  let error = $state('');
  // Highlight a click until the host's next ballot confirms it.
  let pending = $state({ round: 0, choice: -1 });

  const total = $derived(ballot.options.reduce((n, o) => n + o.votes, 0));
  const shown = $derived(pending.round === ballot.round && pending.choice >= 0 ? pending.choice : ballot.mine);

  async function pick(i: number) {
    if (ballot.closed) return;
    error = '';
    pending = { round: ballot.round, choice: i };
    try {
      await onvote(ballot.round, i);
    } catch (err) {
      error = String(err);
      pending = { round: 0, choice: -1 };
    }
  }

  const secs = (s: number) => `${Math.max(0, Math.ceil(s))} s`;

  // Per option, which thumbnail URL is being tried (.jpg, then .png); past the
  // end = no image. Keyed by option ID so a new ballot starts fresh.
  let thumbTry = $state<Record<string, number>>({});
  const thumbOf = (o: main.VoteOption) => (o.thumbs ?? [])[thumbTry[o.id] ?? 0];
  function thumbFailed(id: string) {
    thumbTry[id] = (thumbTry[id] ?? 0) + 1;
  }
</script>

<section class="vote" class:closed={ballot.closed} aria-live="polite">
  <div class="head">
    {#if ballot.closed}
      <div class="title">Next match: <strong>{ballot.options[ballot.winner]?.name}</strong></div>
      <div class="timer">{ballot.startsIn >= 0 ? `starting in ${secs(ballot.startsIn)}` : 'starting soon'}</div>
    {:else}
      <div class="title">Vote for the next match</div>
      <div class="timer">{secs(ballot.remaining)} left</div>
    {/if}
  </div>
  <div class="options">
    {#each ballot.options as o, i (o.id)}
      <button
        class="option"
        class:mine={shown === i}
        class:winner={ballot.closed && ballot.winner === i}
        class:lost={ballot.closed && ballot.winner !== i}
        disabled={ballot.closed}
        onclick={() => pick(i)}
      >
        {#if thumbOf(o)}
          <img class="thumb" src={thumbOf(o)} alt="" loading="lazy" decoding="async" onerror={() => thumbFailed(o.id)} />
        {/if}
        <span class="name">{o.name}</span>
        <span class="votes">
          {o.votes}
          {o.votes === 1 ? 'vote' : 'votes'}
        </span>
        <span class="meter" style="width: {total ? (100 * o.votes) / total : 0}%"></span>
      </button>
    {/each}
  </div>
  {#if error}<div class="error">{error}</div>{:else if !ballot.closed && shown < 0}<div class="hint">
      No vote? The server picks one of these at random.
    </div>{/if}
</section>

<style>
  .vote {
    padding: 14px 18px;
    border-top: 1px solid var(--line);
    background: color-mix(in srgb, var(--button-bg) 35%, var(--panel));
  }
  .head { display: flex; align-items: baseline; gap: 12px; margin-bottom: 10px; }
  .title { font-size: 15px; flex: 1; }
  .timer { color: var(--accent); font-variant-numeric: tabular-nums; }
  .options { display: grid; grid-template-columns: repeat(auto-fit, minmax(170px, 1fr)); gap: 8px; }
  .option {
    position: relative;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 4px;
    text-align: left;
    padding: 10px 12px;
    border-radius: 8px;
    border: 1px solid var(--line);
    background: var(--panel);
    color: var(--text);
  }
  .option:hover:not(:disabled) { border-color: var(--accent); }
  .option.mine { border-color: var(--accent); box-shadow: 0 0 0 1px var(--accent) inset; }
  .option.winner { border-color: var(--ok); box-shadow: 0 0 0 1px var(--ok) inset; }
  .option.lost { opacity: 0.5; }
  .option:disabled { cursor: default; }
  .thumb {
    width: calc(100% + 24px);
    margin: -10px -12px 4px;
    aspect-ratio: 16 / 9;
    max-height: 96px;
    object-fit: cover;
    display: block;
    background: var(--bg);
  }
  .name { font-weight: 600; z-index: 1; }
  .votes { color: var(--muted); font-size: 12px; z-index: 1; }
  .meter {
    position: absolute;
    left: 0;
    bottom: 0;
    height: 3px;
    background: var(--accent);
    transition: width 0.3s;
  }
  .winner .meter { background: var(--ok); }
  .hint { color: var(--muted); font-size: 12px; margin-top: 8px; }
  .error { color: var(--warn); font-size: 12px; margin-top: 8px; }
</style>
