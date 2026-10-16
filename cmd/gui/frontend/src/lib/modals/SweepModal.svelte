<script lang="ts">
  // Previews stale local branches (gone, merged, squashed) and deletes them.
  import { fade, slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { pushNotice } from '../notices';
  import { t } from '../i18n';
  import Rich from '../Rich.svelte';
  import { applyStatusResults } from '../stores';
  import { dismiss } from '../dismiss';

  export let closeActionMenu: () => void;

  let sweepModal: { sourceKey: string; repoKey: string; merged: string[]; gone: string[]; squashed: string[]; } | null = null;
  let sweepBusy = false;

  export async function sweepBranches(sourceKey: string, repoKey: string) {
    closeActionMenu();
    const preview = await bridge.previewSweep(sourceKey, repoKey);
    if (preview.error) {
      pushNotice('error', $t('notice.sweepFailed'), [preview.error]);
      return;
    }
    const merged = preview.merged || [];
    const gone = preview.gone || [];
    const squashed = preview.squashed || [];
    if (merged.length === 0 && gone.length === 0 && squashed.length === 0) {
      pushNotice('info', $t('notice.sweepNone'), [repoKey]);
      return;
    }
    sweepModal = { sourceKey, repoKey, merged, gone, squashed };
  }

  $: sweepTotal = sweepModal ? sweepModal.merged.length + sweepModal.gone.length + sweepModal.squashed.length : 0;

  async function confirmSweepAction() {
    if (!sweepModal) return;
    sweepBusy = true;
    const result = await bridge.confirmSweep(sweepModal.sourceKey, sweepModal.repoKey);
    sweepBusy = false;
    sweepModal = null;
    if (result.error) {
      pushNotice('error', $t('notice.sweepFailed'), [result.error]);
    } else if (result.deleted?.length > 0) {
      pushNotice('success', $t('notice.swept'), [$t(result.deleted.length === 1 ? 'notice.sweptOne' : 'notice.sweptN', { n: result.deleted.length }), result.deleted.join(', ')]);
    }
    bridge.getAllStatus().then(results => { applyStatusResults(results); });
  }
</script>

{#if sweepModal}
  <div class="overlay" use:dismiss={() => sweepModal = null} transition:fade={{ duration: 120 }}>
    <div class="modal modal-sweep" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head modal-head-sweep">
        <h3>{$t('sweep.title')}</h3>
        <button class="btn-x" on:click={() => sweepModal = null}>&#10005;</button>
      </div>
      <div class="modal-body">
        <p class="sweep-repo-name">{$t('sweep.clone', { repo: sweepModal.repoKey })}</p>
        <p class="sweep-explain"><Rich key="sweep.explain" /></p>
        {#if sweepModal.gone.length > 0}
          <div class="sweep-group">
            <span class="sweep-label sweep-label-gone">{$t('sweep.gone', { n: sweepModal.gone.length })}</span>
            <span class="sweep-hint">{$t('sweep.goneHint')}</span>
            <ul class="sweep-list">{#each sweepModal.gone as b}<li>{b}</li>{/each}</ul>
          </div>
        {/if}
        {#if sweepModal.merged.length > 0}
          <div class="sweep-group">
            <span class="sweep-label sweep-label-merged">{$t('sweep.merged', { n: sweepModal.merged.length })}</span>
            <span class="sweep-hint">{$t('sweep.mergedHint')}</span>
            <ul class="sweep-list">{#each sweepModal.merged as b}<li>{b}</li>{/each}</ul>
          </div>
        {/if}
        {#if sweepModal.squashed.length > 0}
          <div class="sweep-group">
            <span class="sweep-label sweep-label-squashed">{$t('sweep.squashed', { n: sweepModal.squashed.length })}</span>
            <span class="sweep-hint">{$t('sweep.squashedHint')}</span>
            <ul class="sweep-list">{#each sweepModal.squashed as b}<li>{b}</li>{/each}</ul>
          </div>
        {/if}
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={() => sweepModal = null}>{$t('common.cancel')}</button>
        <button class="btn-sweep-confirm" on:click={confirmSweepAction} disabled={sweepBusy}>
          {sweepBusy ? $t('sweep.sweeping') : $t(sweepTotal === 1 ? 'sweep.deleteOne' : 'sweep.deleteN', { n: sweepTotal })}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  /* ── Sweep modal ── */
  .modal-sweep { width: 420px; }
  .modal-head-sweep { border-bottom-color: #f59e0b44; }
  .modal-head-sweep h3 { color: #f59e0b; }
  :global([data-theme="light"]) .modal-head-sweep h3 { color: #b45309; }
  .sweep-repo-name { font-size: 13px; font-weight: 600; color: var(--text-primary); margin: 0 0 8px; }
  .sweep-explain { font-size: 12px; color: var(--text-secondary); margin: 0 0 12px; line-height: 1.5; }
  .sweep-group { margin-bottom: 10px; }
  .sweep-label { font-size: 12px; font-weight: 600; display: block; margin-bottom: 2px; }
  .sweep-label-merged { color: #22c55e; }
  :global([data-theme="light"]) .sweep-label-merged { color: #16a34a; }
  .sweep-label-gone { color: #f59e0b; }
  :global([data-theme="light"]) .sweep-label-gone { color: #d97706; }
  .sweep-label-squashed { color: #a78bfa; }
  :global([data-theme="light"]) .sweep-label-squashed { color: #7c3aed; }
  .sweep-hint { font-size: 11px; color: var(--text-muted); display: block; margin-bottom: 4px; }
  .sweep-list {
    margin: 0; padding: 0 0 0 16px; font-size: 12px; color: #D81E5B;
    list-style: disc; max-height: 120px; overflow-y: auto;
  }
  :global([data-theme="light"]) .sweep-list { color: #be123c; }
  .sweep-list li { margin: 1px 0; }
  .btn-sweep-confirm {
    padding: 6px 12px; background: #f59e0b; border: none; color: #000;
    border-radius: 6px; cursor: pointer; font-size: 12px; font-weight: 600;
  }
  .btn-sweep-confirm:hover:not(:disabled) { background: #d97706; }
  .btn-sweep-confirm:disabled { opacity: 0.5; cursor: default; }
  :global([data-theme="light"]) .btn-sweep-confirm { background: #f59e0b; color: #000; }
  :global([data-theme="light"]) .btn-sweep-confirm:hover:not(:disabled) { background: #d97706; }
</style>
