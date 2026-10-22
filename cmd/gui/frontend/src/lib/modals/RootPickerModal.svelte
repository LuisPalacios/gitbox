<script lang="ts">
  // Asks which clone root an explicit clone goes to (#149), once per batch.
  // pick() resolves '' right away with fewer than two roots (the account's
  // own root), the chosen root otherwise, or null when cancelled.
  import { fade, slide } from 'svelte/transition';
  import { t } from '../i18n';
  import { dismiss } from '../dismiss';
  import { configStore } from '../stores';

  let asking: { name: string; n: number } | null = null;
  let roots: string[] = [];
  let chosen = '';
  let resolve: ((root: string | null) => void) | null = null;

  const norm = (p: string | undefined) => (p || '').trim().replace(/[\\/]+$/, '');

  export function pick(sourceKey: string, repoNames: string[]): Promise<string | null> {
    roots = $configStore?.global?.roots || [];
    if (roots.length < 2) return Promise.resolve('');
    const own = norm($configStore?.sources?.[sourceKey]?.root);
    chosen = roots.find((r) => own && norm(r) === own) || roots[0];
    asking = { name: (repoNames[0] || '').split('/').pop() || '', n: repoNames.length };
    return new Promise((r) => { resolve = r; });
  }

  function finish(root: string | null) {
    resolve?.(root);
    resolve = null;
    asking = null;
  }
</script>

{#if asking}
  <div class="overlay" use:dismiss={() => finish(null)} transition:fade={{ duration: 120 }}>
    <div class="modal modal-confirm" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head"><h3>{$t('roots.pickTitle')}</h3></div>
      <div class="modal-body">
        <p class="ask">{asking.n === 1 ? $t('roots.pickOne', { name: asking.name }) : $t('roots.pickMany', { n: asking.n })}</p>
        <div class="radio-group">
          {#each roots as r (r)}
            <label><input type="radio" bind:group={chosen} value={r} /> <span class="path">{r}</span></label>
          {/each}
        </div>
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={() => finish(null)}>{$t('common.cancel')}</button>
        <button class="btn-add" on:click={() => finish(chosen)}>{$t('roots.pickGo')}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .ask { margin: 0 0 10px; font-size: 13px; color: var(--text-primary); }
  .radio-group { display: flex; flex-direction: column; gap: 6px; }
  .radio-group label { font-size: 12px; color: var(--text); display: flex; align-items: center; gap: 6px; cursor: pointer; }
  .path { font-family: 'SF Mono', 'Cascadia Code', 'Consolas', monospace; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
