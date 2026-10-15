<script lang="ts">
  // Adopt clones found on disk that the config doesn't know yet.
  import { fade, slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { pushNotice } from '../notices';
  import { t } from '../i18n';
  import { configStore, applyStatusResults } from '../stores';
  import { dismiss } from '../dismiss';
  import type { OrphanRepoDTO } from '../types';

  export let orphanList: OrphanRepoDTO[];
  export let loadOrphans: () => Promise<void>;

  let orphanModal: { orphans: OrphanRepoDTO[]; selected: Set<string>; } | null = null;
  let orphanBusy = false;

  export function showOrphanModal() {
    const matched = orphanList.filter(o => o.matchedAccount && !o.localOnly);
    orphanModal = { orphans: orphanList, selected: new Set(matched.map(o => o.repoKey)) };
  }

  function toggleOrphan(repoKey: string) {
    if (!orphanModal) return;
    const s = new Set(orphanModal.selected);
    if (s.has(repoKey)) s.delete(repoKey); else s.add(repoKey);
    orphanModal = { ...orphanModal, selected: s };
  }

  async function confirmAdopt() {
    if (!orphanModal) return;
    orphanBusy = true;
    const keys = [...orphanModal.selected];
    const result = await bridge.adoptOrphans(keys);
    orphanBusy = false;
    orphanModal = null;
    if (result.error) pushNotice('error', $t('notice.adoptFailed'), [result.error]);
    // Reload config from disk (adoption saved there) so the dashboard
    // picks up the new repos and the orphan scan uses fresh state.
    $configStore = await bridge.reloadConfig();
    // Refresh all repo statuses — includes newly adopted repos.
    const statuses = await bridge.getAllStatus();
    applyStatusResults(statuses);
    // Rescan orphans with the reloaded config.
    await loadOrphans();
  }
</script>

{#if orphanModal}
  <div class="overlay" use:dismiss={() => orphanModal = null} transition:fade={{ duration: 120 }}>
    <div class="modal modal-adopt" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head modal-head-adopt">
        <h3>{$t('orphans.title')}</h3>
        <button class="btn-x" on:click={() => orphanModal = null}>&#10005;</button>
      </div>
      <div class="modal-body">
        {#each orphanModal.orphans.filter(o => o.matchedAccount && !o.localOnly) as o, i}
          {#if i === 0}
            <p class="adopt-section-label">{$t('orphans.ready', { n: orphanModal.orphans.filter(o => o.matchedAccount && !o.localOnly).length })}</p>
          {/if}
          <label class="adopt-item">
            <input type="checkbox" checked={orphanModal.selected.has(o.repoKey)} on:change={() => toggleOrphan(o.repoKey)} />
            <span class="adopt-repo-key">{o.repoKey}</span>
            <span class="adopt-target">&rarr; {o.matchedSource}</span>
            <span class="adopt-action">{o.nested ? $t('orphans.nested') : o.needsRelocate ? $t('orphans.relocate') : $t('orphans.inPlace')}</span>
            <span class="adopt-path"><span class="adopt-path-root">{$t('orphans.root')}</span>/{o.relPath}</span>
          </label>
        {/each}
        {#each orphanModal.orphans.filter(o => !o.matchedAccount && !o.localOnly) as o, i}
          {#if i === 0}
            <p class="adopt-section-label adopt-section-unknown">{$t('orphans.unknown', { n: orphanModal.orphans.filter(o => !o.matchedAccount && !o.localOnly).length })}</p>
          {/if}
          <div class="adopt-item adopt-item-muted">
            <span class="adopt-repo-key">{o.repoKey}</span>
            <span class="adopt-remote">{o.remoteURL}</span>
            {#if o.ambiguousCandidates && o.ambiguousCandidates.length > 1}
              <span class="adopt-hint">{$t('orphans.ambiguous', { list: o.ambiguousCandidates.join(' | ') })}</span>
            {/if}
            <span class="adopt-path adopt-path-muted"><span class="adopt-path-root">{$t('orphans.root')}</span>/{o.relPath}</span>
          </div>
        {/each}
        {#each orphanModal.orphans.filter(o => o.localOnly) as o, i}
          {#if i === 0}
            <p class="adopt-section-label adopt-section-local">{$t('orphans.localOnly', { n: orphanModal.orphans.filter(o => o.localOnly).length })}</p>
          {/if}
          <div class="adopt-item adopt-item-muted">
            <span class="adopt-repo-key">{o.relPath}</span>
            <span class="adopt-remote">{$t('orphans.noRemote')}</span>
          </div>
        {/each}
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={() => orphanModal = null}>{$t('common.cancel')}</button>
        <button class="btn-adopt-confirm" on:click={confirmAdopt} disabled={orphanBusy || orphanModal.selected.size === 0}>
          {orphanBusy ? $t('orphans.adopting') : $t(orphanModal.selected.size === 1 ? 'orphans.adoptOne' : 'orphans.adoptN', { n: orphanModal.selected.size })}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  .modal-adopt { width: 500px; }
  .modal-head-adopt { border-bottom-color: #f59e0b44; }
  .modal-head-adopt h3 { color: #f59e0b; }
  :global([data-theme="light"]) .modal-head-adopt h3 { color: #b45309; }
  .adopt-section-label { font-size: 12px; font-weight: 600; color: #22c55e; margin: 8px 0 4px; }
  :global([data-theme="light"]) .adopt-section-label { color: #16a34a; }
  .adopt-section-unknown { color: #f87171; }
  :global([data-theme="light"]) .adopt-section-unknown { color: #dc2626; }
  .adopt-section-local { color: var(--text-muted); }
  .adopt-item {
    display: flex; flex-wrap: wrap; align-items: center; gap: 8px;
    padding: 3px 0; font-size: 12px; color: var(--text-primary);
  }
  .adopt-item input[type="checkbox"] { margin: 0; accent-color: #22c55e; }
  .adopt-item-muted { color: var(--text-muted); padding-left: 22px; }
  .adopt-repo-key { font-weight: 500; }
  .adopt-target { color: var(--text-secondary); font-size: 11px; }
  .adopt-action { color: var(--text-muted); font-size: 11px; font-style: italic; margin-left: auto; }
  .adopt-remote { color: var(--text-muted); font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 300px; }
  .adopt-hint { color: #f59e0b; font-size: 11px; font-style: italic; margin-left: auto; }
  .adopt-path {
    flex-basis: 100%; padding-left: 22px;
    color: var(--text-muted); font-size: 11px; font-family: var(--font-mono, monospace);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .adopt-path-muted { padding-left: 0; }
  .adopt-path-root {
    display: inline-block;
    padding: 0 6px;
    background: var(--bg-secondary, rgba(128, 128, 128, 0.18));
    color: var(--text-secondary);
    border-radius: 3px;
    font-family: inherit;
    font-size: 10px;
    font-weight: 600;
  }
  .btn-adopt-confirm {
    padding: 6px 12px; background: #22c55e; border: none; color: #000;
    border-radius: 6px; cursor: pointer; font-size: 12px; font-weight: 600;
  }
  .btn-adopt-confirm:hover:not(:disabled) { background: #16a34a; }
  .btn-adopt-confirm:disabled { opacity: 0.5; cursor: default; }
</style>
