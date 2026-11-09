<script lang="ts">
  // Asks which .code-workspace file at a container's root it opens with,
  // when the root holds several. pick() resolves the only file right away
  // when there is one, the chosen file otherwise, or null when cancelled.
  import { fade, slide } from 'svelte/transition';
  import { t } from '../i18n';
  import { dismiss } from '../dismiss';

  let asking = '';
  let files: string[] = [];
  let chosen = '';
  let resolve: ((file: string | null) => void) | null = null;

  export function pick(repoName: string, choices: string[], current = ''): Promise<string | null> {
    if (choices.length < 2) return Promise.resolve(choices[0] ?? null);
    files = choices;
    chosen = choices.includes(current) ? current : choices[0];
    asking = repoName.split('/').pop() || repoName;
    return new Promise((r) => { resolve = r; });
  }

  function finish(file: string | null) {
    resolve?.(file);
    resolve = null;
    asking = '';
  }
</script>

{#if asking}
  <div class="overlay" use:dismiss={() => finish(null)} transition:fade={{ duration: 120 }}>
    <div class="modal modal-confirm" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head"><h3>{$t('workspace.pickTitle')}</h3></div>
      <div class="modal-body">
        <p class="ask">{$t('workspace.pickAsk', { name: asking })}</p>
        <div class="radio-group">
          {#each files as f (f)}
            <label><input type="radio" bind:group={chosen} value={f} /> <span class="path">{f}</span></label>
          {/each}
        </div>
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={() => finish(null)}>{$t('common.cancel')}</button>
        <button class="btn-add" on:click={() => finish(chosen)}>{$t('workspace.pickGo')}</button>
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
