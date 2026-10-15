<script lang="ts">
  // The activity strip under the top bar while Fetch all or Pull all runs:
  // its scope in words and a progress bar.
  import { progress } from '../activity';
  import type { Run } from '../activity';
  import { t } from '../i18n';

  export let run: Run | null;
  export let simple: boolean;

  $: p = run ? progress(run) : { done: 0, total: 0 };
  $: pct = p.total > 0 ? Math.min(100, Math.max(0, (p.done / p.total) * 100)) : 0;
  $: text = run ? scope(run, simple) : '';

  function scope(run: Run, simple: boolean): string {
    const n = run.keys.length;
    if (run.kind === 'fetch') {
      if (simple) return $t('activity.checking', { n });
      return $t(run.accounts === 1 ? 'activity.fetchingOne' : 'activity.fetching', { n, m: run.accounts });
    }
    if (simple) return $t('activity.updating', { n });
    const clones = run.clones.size;
    const pulls = n - clones;
    if (clones === 0) return $t('activity.pulling', { n: pulls });
    if (pulls === 0) return $t('activity.cloning', { n: clones });
    return $t('activity.pullingCloning', { n: pulls, m: clones });
  }
</script>

{#if run}
<div class="activity" role="status">
  <span class="spin" aria-hidden="true">↻</span>
  <span class="text">{text}</span>
  <span class="bar"><span class="fill" style="width: {pct}%"></span></span>
  <span class="count">{p.done}/{p.total}</span>
</div>
{/if}

<style>
  .activity {
    display: flex; align-items: center; gap: 12px; padding: 7px 24px; flex: none;
    border-bottom: 1px solid var(--border); font-size: 12px; color: var(--text-secondary);
    background: color-mix(in srgb, #4B95E9 8%, var(--bg-base));
  }
  .spin { display: inline-flex; color: var(--spin-color); animation: spin 0.9s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }
  .text { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .bar { flex: 1; max-width: 220px; min-width: 60px; height: 4px; border-radius: 2px; background: var(--ring-bg); overflow: hidden; }
  .fill { display: block; height: 100%; background: #4B95E9; transition: width 0.2s; }
  .count { color: var(--text-muted); font-variant-numeric: tabular-nums; }
  @media (prefers-reduced-motion: reduce) { .spin { animation: none; } .fill { transition: none; } }
</style>
