<script lang="ts">
  // The Mirrors view: its header with Discover and Check all, discovery
  // progress, the mirror group cards and the list of mirrored repos with
  // their live status and setup actions.
  import { mirrors, mirrorStates, themeStore } from '../stores';
  import { statusColor } from '../status';
  import { t } from '../i18n';
  import Card from '../Card.svelte';
  import UIIcon from '../UIIcon.svelte';
  import type { MirrorDTO, MirrorRepo, MirrorStatusResult } from '../types';

  export let expandedCard: string | null;
  export let deleteMode: boolean;
  export let mirrorDiscoverLoading: boolean;
  export let mirrorDiscoverProgress: { phase: string; account: string; current: number; total: number } | null;
  export let mirrorChecking: Record<string, boolean>;
  export let mirrorSettingUp: Record<string, boolean>;
  export let checkMirrorStatus: (mirrorKey: string) => Promise<void>;
  export let setupMirrorRepo: (mirrorKey: string, repoKey: string) => Promise<void>;
  export let openTokenSetup: (accountKey: string) => Promise<void>;
  export let onAddGroup: () => void;
  export let onAddRepo: (mirrorKey: string) => void;
  export let onDeleteGroup: (mirrorKey: string) => void;
  export let onDeleteRepo: (mirrorKey: string, repoKey: string) => void;
  export let runMirrorDiscover: () => Promise<void>;
  export let checkAllMirrorStatus: () => Promise<void>;

  $: sc = (s: string) => statusColor(s, $themeStore);

  function mirrorDirLabelHtml(repo: MirrorRepo, m: MirrorDTO): string {
    const origin = repo.origin === 'src' ? m.account_src : m.account_dst;
    const backup = repo.origin === 'src' ? m.account_dst : m.account_src;
    const src = `<span class="mir-origin">${origin}</span>`;
    const dst = `<span class="mir-backup">${backup}</span>`;
    if (repo.direction === 'push') return `${src} <span class="mir-arrow">⟶</span> ${dst}`;
    return `${dst} <span class="mir-arrow">⟵</span> ${src}`;
  }

  function mirrorStatusColor(_repo: MirrorRepo, live: MirrorStatusResult | undefined, theme: string): string {
    if (live?.needsSetup) return statusColor('not cloned', theme); // neutral — row has never been set up
    if (live?.error) return statusColor('error', theme);
    if (live?.syncStatus === 'synced') return statusColor('clean', theme);
    if (live?.syncStatus === 'behind') return statusColor('behind', theme);
    if (live?.syncStatus === 'ahead') return statusColor('error', theme);
    if (!live) return statusColor('not cloned', theme); // unchecked
    return statusColor('clean', theme);
  }

  function mirrorStatusSymbol(_repo: MirrorRepo, live: MirrorStatusResult | undefined): string {
    if (live?.needsSetup) return '○';
    if (live?.error) return '✕';
    if (live?.syncStatus === 'synced') return '●';
    if (live?.syncStatus === 'behind') return '↓';
    if (live?.syncStatus === 'ahead') return '↑';
    if (!live) return '○'; // unchecked
    return '●';
  }

  /** Summarize live mirror status in a short, user-friendly string. */
  function mirrorStatusText(_repo: MirrorRepo, live: MirrorStatusResult | undefined): string {
    if (live?.needsSetup) return 'needs setup';
    if (live?.error) return friendlyMirrorError(live.error);
    if (live?.syncStatus === 'synced') return 'Synced OK';
    if (live?.syncStatus === 'behind') return 'Backup is behind origin';
    if (live?.syncStatus === 'ahead') return 'Backup is ahead of origin';
    if (!live) return 'unchecked';
    return 'OK';
  }

  /** Turn raw Go error strings into concise, user-friendly messages. */
  function friendlyMirrorError(raw: string): string {
    // Errors from CheckStatus are already user-friendly (e.g. "missing API token in git-parchis-me").
    if (raw.length > 60) return raw.slice(0, 57) + '...';
    return raw;
  }

  $: mirrorGroupStats = (() => {
    const stats: Record<string, { active: number; unchecked: number; error: number; total: number }> = {};
    for (const [key, mir] of Object.entries($mirrors)) {
      let active = 0, unchecked = 0, error = 0, total = 0;
      for (const repoKey of Object.keys(mir.repos)) {
        total++;
        const live = $mirrorStates[`${key}/${repoKey}`];
        if (!live) unchecked++;
        else if (live.needsSetup) unchecked++;
        else if (live.error) error++;
        else active++;
      }
      stats[key] = { active, unchecked, error, total };
    }
    return stats;
  })();
</script>

<section class="view-scroll">
<div class="view-head">
  <h3 class="view-title">{$t('tab.mirrors')}</h3>
  <span class="view-actions">
    <button class="text-btn" on:click={runMirrorDiscover} disabled={mirrorDiscoverLoading}>{mirrorDiscoverLoading ? $t('tab.scanning') : $t('tab.discover')}</button>
    <button class="text-btn" on:click={checkAllMirrorStatus}>{$t('tab.checkAll')}</button>
  </span>
</div>
{#if mirrorDiscoverLoading && mirrorDiscoverProgress}
<div class="discover-progress">
  <div class="discover-progress-text">
    {#if mirrorDiscoverProgress.phase === 'listing'}
      Listing repos on <strong>{mirrorDiscoverProgress.account}</strong>...
    {:else}
      Analyzing <strong>{mirrorDiscoverProgress.account}</strong>: {mirrorDiscoverProgress.current}/{mirrorDiscoverProgress.total} repos
    {/if}
  </div>
  {#if mirrorDiscoverProgress.phase === 'analyzing' && mirrorDiscoverProgress.total > 0}
    <div class="discover-progress-bar">
      <div class="discover-progress-fill" style="width: {(mirrorDiscoverProgress.current / mirrorDiscoverProgress.total) * 100}%"></div>
    </div>
  {:else}
    <div class="discover-progress-bar">
      <div class="discover-progress-fill discover-progress-indeterminate"></div>
    </div>
  {/if}
</div>
{/if}
<div class="cards-row">
  {#each Object.entries($mirrors) as [mirrorKey, mir]}
    {@const mstats = mirrorGroupStats[mirrorKey] || { active: 0, unchecked: 0, error: 0, total: 0 }}
    {@const mcolor = mstats.error > 0 ? sc('error') : mstats.active === mstats.total && mstats.total > 0 ? sc('clean') : sc('behind')}
    {@const mstate = mstats.error > 0 ? `${mstats.error} error${mstats.error > 1 ? 's' : ''}` : mstats.unchecked > 0 ? `${mstats.unchecked} ${$t('mirror.unchecked')}` : mstats.total > 0 ? $t('mirror.allSynced') : $t('mirror.noClones')}
    <Card open={expandedCard === `mir:${mirrorKey}`} title={mstate} name="{mir.account_src} ↔ {mir.account_dst}" dotColor={mcolor}
      variant="card-mirror" nameClass="card-mirror-name"
      {deleteMode} deleteTitle="Delete mirror group {mirrorKey}" onDelete={() => onDeleteGroup(mirrorKey)}
      onOpen={() => expandedCard = `mir:${mirrorKey}`}
      provider="MIRROR" count={{ text: `${mstats.active}/${mstats.total}`, title: mstate, style: `color: ${mcolor}` }}
      actions={[{ label: $t('mirror.check'), onClick: () => checkMirrorStatus(mirrorKey), title: $t('mirror.checkStatus') }]} />
  {/each}
  <button class="card card-add" on:click={onAddGroup} title={$t('mirror.addGroup')}>
    <span class="card-add-icon">+</span>
  </button>
</div>

<!-- ── MIRROR DETAIL LIST ── -->
{#if Object.keys($mirrors).length > 0}
  <div class="mirror-list">

  {#each Object.entries($mirrors) as [mirrorKey, mir]}
    <div class="mirror-group">
      <div class="mirror-group-header">
        <span class="mirror-accounts">{mir.account_src} <span class="mirror-arrow">↔</span> {mir.account_dst}</span>
        <div class="mirror-group-actions">
          {#if mirrorChecking[mirrorKey]}
            <span class="mirror-checking"><div class="spinner-sm"></div></span>
          {/if}
          <button class="btn-tab-action" on:click={() => onAddRepo(mirrorKey)}>{$t('mirror.addRepo')}</button>
          {#if deleteMode}
            <button class="icon-btn icon-btn-danger btn-delete-x" on:click={() => onDeleteGroup(mirrorKey)} title="Delete mirror group {mirrorKey}" aria-label="Delete mirror group {mirrorKey}"><UIIcon name="x" /></button>
          {/if}
        </div>
      </div>

      {#each (mir.repoOrder && mir.repoOrder.length > 0 ? mir.repoOrder : Object.keys(mir.repos)) as repoName}
        {@const repo = mir.repos[repoName]}
        {@const live = $mirrorStates[`${mirrorKey}/${repoName}`]}
        {@const settingUp = !!mirrorSettingUp[`${mirrorKey}/${repoName}`]}
        <div class="mirror-row">
          {#if settingUp}
            <span class="mirror-dot"><div class="spinner-sm"></div></span>
          {:else}
            <span class="mirror-dot" style="color:{mirrorStatusColor(repo, live, $themeStore)}">{mirrorStatusSymbol(repo, live)}</span>
          {/if}
          <span class="mirror-repo-name">{repoName}</span>
          <span class="mirror-direction">{@html mirrorDirLabelHtml(repo, mir)}</span>
          {#if settingUp}
            <span class="mirror-status-text">{$t('mirror.settingUp')}</span>
          {:else}
            <span class="mirror-status-text" style="color:{mirrorStatusColor(repo, live, $themeStore)}">{mirrorStatusText(repo, live)}</span>
          {/if}
          {#if live?.warning}
            <span class="mirror-warning" title={live.warning}>⚠</span>
          {/if}
          {#if live?.error && live.error.startsWith('missing API token in ') && !deleteMode}
            {@const errAcct = live.error.replace('missing API token in ', '')}
            <button class="btn-sm btn-fix" on:click={() => openTokenSetup(errAcct)} title="Set up API token for {errAcct}">{$t('mirror.fixCredentials')}</button>
          {:else if !deleteMode}
            <button class="btn-sm btn-setup" on:click={() => setupMirrorRepo(mirrorKey, repoName)} disabled={settingUp}>{$t('mirror.setup')}</button>
          {/if}
          {#if deleteMode}
            <button class="icon-btn icon-btn-danger btn-delete-x" on:click={() => onDeleteRepo(mirrorKey, repoName)} title="Delete {repoName} from {mirrorKey}" aria-label="Delete {repoName} from {mirrorKey}"><UIIcon name="x" /></button>
          {/if}
        </div>
      {/each}

      {#if Object.keys(mir.repos).length === 0}
        <div class="mirror-empty">{$t('mirror.empty')}</div>
      {/if}
    </div>
  {/each}
  </div>
{/if}
</section>

<style>
  @keyframes spin { to { transform: rotate(360deg); } }
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
  /* Chips and the open card are as wide as their content, so names are
     never cut; the row wraps instead of scrolling. */
  .cards-row { display: flex; flex-wrap: wrap; align-items: flex-start; gap: 8px; padding: 4px 0 10px; flex-shrink: 0; }
  .card {
    flex: 0 0 auto; background: var(--bg-card); border: 1px solid var(--border);
    border-radius: 10px; padding: 8px 12px; transition: border-color 0.15s, box-shadow 0.15s;
    box-shadow: var(--card-shadow);
  }
  .card:hover { border-color: var(--border-hover); }
  .discover-progress { padding: 0 0 6px; }
  .discover-progress-text { font-size: 11px; color: var(--text-secondary); margin-bottom: 4px; }
  .discover-progress-text strong { color: var(--text-primary); }
  .discover-progress-bar { width: 100%; height: 4px; background: var(--ring-bg); border-radius: 2px; overflow: hidden; }
  .discover-progress-fill { height: 100%; background: var(--ring-accent); border-radius: 2px; transition: width 0.2s; }
  .discover-progress-indeterminate {
    width: 30%; animation: indeterminate 1.2s ease-in-out infinite;
  }
  @keyframes indeterminate { 0% { margin-left: 0; } 50% { margin-left: 70%; } 100% { margin-left: 0; } }
  /* ── Add account card ── */
  .card-add {
    display: flex; align-items: center; justify-content: center;
    padding: 3px 12px; border-radius: 999px; box-shadow: none;
    cursor: pointer; border-style: dashed; background: transparent;
    color: var(--text-muted); transition: all 0.15s;
  }
  .card-add:hover { border-color: var(--border-hover); color: var(--text-primary); background: var(--bg-hover); }
  .card-add-icon { font-size: 16px; font-weight: 400; line-height: 1; }
  /* ── Mirror section ── */
  .mirror-list { padding: 4px 0 0; border-top: 1px solid var(--border); }
  .mirror-group { margin-bottom: 12px; }
  .mirror-group-header {
    display: flex; justify-content: space-between; align-items: center;
    padding: 4px 0; font-size: 12px; color: var(--text-muted);
  }
  .mirror-accounts { font-weight: 500; color: var(--text-primary); }
  .mirror-arrow { color: var(--text-muted); margin: 0 2px; }
  .mirror-group-actions { display: flex; gap: 4px; align-items: center; }
  .mirror-row {
    display: flex; align-items: center; gap: 8px;
    padding: 3px 0 3px 8px; font-size: 12px;
  }
  .mirror-row:hover { background: var(--hover); border-radius: 4px; }
  .mirror-dot { font-size: 11px; flex-shrink: 0; width: 14px; text-align: center; }
  .mirror-repo-name { font-weight: 500; color: var(--text-primary); min-width: 120px; }
  .mirror-direction { font-size: 11px; flex-shrink: 0; }
  /* Spans of mirrorDirLabelHtml(), rendered with {@html}. */
  :global(.mir-origin) { color: #61fd5f; font-weight: 500; }
  :global(.mir-backup) { color: #F07623; font-weight: 500; }
  :global(.mir-arrow) { font-size: 14px; font-weight: 700; color: #F07623; vertical-align: middle; }
  :global([data-theme="light"]) :global(.mir-origin) { color: #166534; }
  :global([data-theme="light"]) :global(.mir-backup) { color: #c2410c; }
  :global([data-theme="light"]) :global(.mir-arrow) { color: #c2410c; }
  .mirror-status-text { font-size: 11px; margin-left: auto; font-family: var(--font-mono, monospace); }
  .mirror-warning { color: #F07623; font-size: 13px; cursor: help; }
  .mirror-empty { font-size: 11px; color: var(--text-muted); padding: 4px 8px; font-style: italic; }
  .mirror-checking { display: inline-flex; align-items: center; }
  .btn-setup { font-size: 10px; padding: 1px 6px; }
  .btn-fix { font-size: 10px; padding: 1px 6px; background: #431407; border-color: #c2410c; color: #fdba74; }
  .spinner-sm { width: 12px; height: 12px; border: 2px solid var(--border); border-top-color: var(--text-primary); border-radius: 50%; animation: spin 0.6s linear infinite; }
</style>
