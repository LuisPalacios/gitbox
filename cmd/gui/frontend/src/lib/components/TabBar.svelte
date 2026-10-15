<script lang="ts">
  // Accounts / Mirrors / Workspaces tabs with what a folded cards panel
  // hides, the orphans pill and each tab's actions.
  import { accounts, mirrorSummary, workspaces, selectedClones, clearCloneSelection, themeStore } from '../stores';
  import { statusColor } from '../status';
  import { credColor } from '../theme';
  import { t } from '../i18n';

  export let cardsTab: 'accounts' | 'mirrors' | 'workspaces';
  export let cardsVisible: boolean;
  export let simple: boolean;
  export let credStatuses: Record<string, any>;
  export let orphanCount: number;
  export let mirrorDiscoverLoading: boolean;
  export let workspaceBusy: boolean;
  export let selectionMode: boolean;
  export let selectCardsTab: (tab: 'accounts' | 'mirrors' | 'workspaces') => void;
  export let showOrphanModal: () => void;
  export let runMirrorDiscover: () => Promise<void>;
  export let checkAllMirrorStatus: () => Promise<void>;
  export let refreshWorkspaces: () => Promise<void>;

  $: sc = (s: string) => statusColor(s, $themeStore);
  $: cc = (s: string) => credColor(s, $themeStore);
</script>

<div class="cards-tab-bar">
  <button class="cards-tab" class:cards-tab-active={cardsTab === 'accounts'}
    on:click={() => selectCardsTab('accounts')}>{$t('tab.accounts')}
    {#if !cardsVisible}
      <span class="tab-summary">
        {#each Object.keys($accounts) as key (key)}
          <span class="tab-dot" style="background: {cc((credStatuses[key] || {status: 'unknown'}).status)}" title={key}></span>
        {/each}
      </span>
    {/if}
  </button>
  {#if !simple}
  <button class="cards-tab" class:cards-tab-active={cardsTab === 'mirrors'}
    on:click={() => selectCardsTab('mirrors')}>{$t('tab.mirrors')}
    {#if !cardsVisible && $mirrorSummary.total > 0}
      <span class="tab-summary" style="color: {$mirrorSummary.error > 0 ? sc('error') : sc('clean')}">{$mirrorSummary.active}/{$mirrorSummary.total}</span>
    {/if}
  </button>
  <button class="cards-tab" class:cards-tab-active={cardsTab === 'workspaces'}
    on:click={() => selectCardsTab('workspaces')}>{$t('tab.workspaces')}
    {#if !cardsVisible && Object.keys($workspaces).length > 0}
      <span class="tab-summary">{Object.keys($workspaces).length}</span>
    {/if}
  </button>
  {/if}
  {#if orphanCount > 0}
    <button class="orphan-pill" on:click={showOrphanModal}>{simple ? $t('simple.found', { n: orphanCount }) : `${orphanCount} orphan${orphanCount > 1 ? 's' : ''}`}</button>
  {/if}
  {#if cardsTab === 'mirrors'}
    <div class="tab-bar-actions">
      <button class="btn-tab-action" on:click={runMirrorDiscover} disabled={mirrorDiscoverLoading}>{mirrorDiscoverLoading ? $t('tab.scanning') : $t('tab.discover')}</button>
      <button class="btn-tab-action" on:click={checkAllMirrorStatus}>{$t('tab.checkAll')}</button>
    </div>
  {/if}
  {#if cardsTab === 'workspaces'}
    <div class="tab-bar-actions">
      <button class="btn-tab-action" title="Rescan disk for .code-workspace files" on:click={refreshWorkspaces} disabled={workspaceBusy}>{$t('tab.discover')}</button>
    </div>
  {/if}
  {#if cardsTab === 'accounts' && selectionMode && $selectedClones.size > 0}
    <div class="tab-bar-actions">
      <button class="btn-tab-action" on:click={() => clearCloneSelection()} title="Clear selection">{$t('tab.clear')}</button>
    </div>
  {/if}
</div>

<style>
  .cards-tab-bar {
    display: flex; gap: 4px; padding: 12px 24px 0;
    align-items: flex-end;
    border-bottom: 1px solid var(--border);
  }
  .tab-bar-actions { margin-left: auto; display: flex; gap: 6px; padding-bottom: 6px; }
  .btn-tab-action {
    padding: 3px 10px; font-size: 11px; font-weight: 500;
    border: 1px solid #1e3a5f; background: rgba(37, 99, 235, 0.1);
    color: #93c5fd; border-radius: 5px;
    cursor: pointer; transition: all 0.12s;
  }
  .btn-tab-action:hover:not(:disabled) { background: rgba(37, 99, 235, 0.2); color: #bfdbfe; }
  .btn-tab-action:disabled { opacity: 0.5; cursor: default; }
  :global([data-theme="light"]) .btn-tab-action { border-color: #93c5fd; background: rgba(37, 99, 235, 0.06); color: #2563eb; }
  :global([data-theme="light"]) .btn-tab-action:hover:not(:disabled) { background: rgba(37, 99, 235, 0.12); }
  .cards-tab {
    padding: 5px 14px; font-size: 12px; font-weight: 600;
    border: 1px solid var(--border);
    border-radius: 6px 6px 0 0;
    background: transparent; color: var(--text-secondary);
    cursor: pointer; transition: background 0.12s, color 0.12s;
    margin-bottom: -1px;
    position: relative;
  }
  .cards-tab:hover:not(.cards-tab-active) { background: var(--bg-hover); color: var(--text-primary); }
  .cards-tab-active {
    background: var(--bg-base); color: var(--text-primary);
    border-color: var(--border-hover);
    border-bottom-color: var(--bg-base);
    z-index: 1;
  }
  /* What a folded panel hides, summarised inside each tab. */
  .tab-summary { display: inline-flex; align-items: center; gap: 3px; margin-left: 6px; font-size: 10px; font-weight: 700; color: var(--text-muted); }
  .tab-dot { width: 6px; height: 6px; border-radius: 50%; }
  /* ── Orphan adoption ── */
  .orphan-pill {
    margin-left: auto; margin-bottom: 6px; padding: 2px 10px; border: 1px solid #f59e0b88;
    background: #f59e0b22; color: #f59e0b; border-radius: 12px;
    font-size: 11px; font-weight: 600; cursor: pointer;
  }
  .orphan-pill:hover { background: #f59e0b44; }
  :global([data-theme="light"]) .orphan-pill { color: #b45309; border-color: #b4530966; background: #fef3c722; }
  :global([data-theme="light"]) .orphan-pill:hover { background: #fef3c7; }
</style>
