<script lang="ts">
  // Compact status view: the global health ring, one pill per account
  // (expandable into its clones), the mirror summary and the theme / full
  // view buttons. App.svelte measures .compact-strip to fit the window.
  import { slide } from 'svelte/transition';
  import { accounts, accountStats, sources, repoStates, summary, mirrorSummary, themeStore } from '../stores';
  import { statusColor, rowBusy, rowGlyph, statusCountBadge } from '../status';
  import { t } from '../i18n';
  import type { RepoRow as TreeRow } from '../repo-tree';

  export let compactExpanded: Record<string, boolean>;
  export let credStatuses: Record<string, any>;
  export let treeRows: Record<string, TreeRow[]>;
  export let fetchingRepos: Record<string, boolean>;
  export let themeChoice: 'system' | 'light' | 'dark';
  export let toggleCompactAcct: (key: string) => Promise<void>;
  export let cycleTheme: () => void;
  export let toggleViewMode: () => Promise<void>;

  $: sc = (s: string) => statusColor(s, $themeStore);
  $: cpct = Math.round(($summary.clean / Math.max($summary.total, 1)) * 100);
  $: pendingStatus = Object.values($repoStates).some(state => state.status === 'unknown');
  $: callGood = !pendingStatus && $summary.clean === $summary.total;

  function themeIcon(choice: string): string {
    return ({ system: '◐', light: '☀', dark: '☾' } as Record<string, string>)[choice] || '◐';
  }
</script>

<div class="compact-strip">
  <!-- Global health -->
  <div class="compact-global">
    <svg viewBox="0 0 36 36" class="compact-ring">
      <circle cx="18" cy="18" r="14" fill="none" stroke="var(--ring-bg)" stroke-width="3"/>
      <circle cx="18" cy="18" r="14" fill="none"
        stroke={callGood ? sc('clean') : sc('behind')}
        stroke-width="3" stroke-linecap="round"
        stroke-dasharray="{($summary.clean / Math.max($summary.total, 1)) * 87.96} 87.96"
        transform="rotate(-90 18 18)"/>
    </svg>
    <div class="compact-global-text">
      <span class="compact-global-pct" style="color: {callGood ? sc('clean') : sc('behind')}">{pendingStatus ? $t('simple.checking') : `${cpct}%`}</span>
      <span class="compact-global-label">{$t('compact.upToDate', { n: $summary.clean, total: $summary.total })}</span>
    </div>
  </div>

  <div class="compact-sep"></div>

  <!-- Account pills -->
  {#each Object.keys($accounts) as key}
    {@const stats = $accountStats[key] || { total: 0, synced: 0, issues: 0 }}
    {@const compactCred = (credStatuses[key] || {status: 'unknown'}).status}
    <button class="compact-acct" class:compact-acct-expanded={compactExpanded[key]}
      class:compact-acct-cred-err={compactCred === 'none' || compactCred === 'error'}
      class:compact-acct-cred-warn={compactCred === 'warning'}
      class:compact-acct-cred-offline={compactCred === 'offline'}
      on:click={() => toggleCompactAcct(key)}>
      <svg viewBox="0 0 36 36" class="compact-acct-ring">
        <circle cx="18" cy="18" r="14" fill="none" stroke="var(--ring-bg)" stroke-width="3"/>
        <circle cx="18" cy="18" r="14" fill="none"
          stroke={stats.issues === 0 ? sc('clean') : sc('behind')}
          stroke-width="3" stroke-linecap="round"
          stroke-dasharray="{(stats.synced / Math.max(stats.total, 1)) * 87.96} 87.96"
          transform="rotate(-90 18 18)"/>
      </svg>
      <div class="compact-acct-info">
        <span class="compact-acct-name">{key}</span>
        <span class="compact-acct-stat">
          {#if Object.entries($sources).some(([sk, src]) => src.account === key && Object.keys(src.repos).some(rn => !$repoStates[`${sk}/${rn}`] || $repoStates[`${sk}/${rn}`].status === 'unknown'))}
            <span class="spinning">↻</span> {$t('simple.checking')}
          {:else if stats.issues === 0}
            <span style="color:{sc('clean')}">{$t('account.allUpToDate')}</span>
          {:else}
            <span style="color:{sc('behind')}">{stats.issues} {stats.issues === 1 ? $t('account.needsAttention') : $t('account.needAttentionPlural')}</span>
          {/if}
        </span>
      </div>
      <span class="compact-chevron">{compactExpanded[key] ? '▾' : '▸'}</span>
    </button>

    {#if compactExpanded[key]}
      <div class="compact-repo-list" transition:slide={{ duration: 120 }}>
        {#each Object.entries($sources).filter(([, source]) => source.account === key).flatMap(([sourceKey]) => treeRows[sourceKey] || []) as { key: repoKey, sourceKey: rowSourceKey, repoName, depth }}
          {@const state = $repoStates[repoKey] || { status: 'unknown', progress: 0, behind: 0, modified: 0, untracked: 0, ahead: 0, branch: '', isDefault: false }}
          {@const count = statusCountBadge(state.status)}
          <div class="compact-row" style={`padding-left: ${depth * 12}px`} class:compact-row-ok={state.status === 'clean'}>
            {#if rowSourceKey && $sources[rowSourceKey].account !== key}<span class="compact-badge">{$sources[rowSourceKey].account}</span>{/if}
            <span class="compact-dot" class:spinning={rowBusy(state.status, !!fetchingRepos[repoKey])} style="color: {sc(state.status)}">{rowGlyph(state.status, !!fetchingRepos[repoKey])}</span>
            <span class="compact-repo-name">{repoName.includes('/') ? repoName.split('/').pop() : repoName}</span>
            {#if state.branch === '(detached)'}
              <span class="compact-badge" style="color: {sc('error')}">detached</span>
            {/if}
            {#if count}
              <span class="compact-badge" style="color: {sc(count.color)}">{state[count.field]} {count.word}</span>
            {/if}
          </div>
        {/each}
      </div>
    {/if}
  {/each}

  <!-- Mirror summary (compact) -->
  {#if $mirrorSummary.total > 0}
  <div class="compact-sep"></div>
  <div class="compact-mirror-pill">
    <span class="compact-mirror-dot" style="color:{$mirrorSummary.error > 0 ? sc('error') : sc('clean')}">●</span>
    <span class="compact-mirror-label">Mirrors {$mirrorSummary.active}/{$mirrorSummary.total}</span>
  </div>
  {/if}

  <!-- Bottom actions -->
  <div class="compact-sep"></div>
  <div class="compact-actions">
    <button class="compact-action-btn" on:click={cycleTheme} title="Theme: {themeChoice}">{themeIcon(themeChoice)}</button>
    <button class="compact-action-btn compact-full-btn" on:click={toggleViewMode}>◧ Full view</button>
  </div>
</div>

<style>
  @keyframes spin { to { transform: rotate(360deg); } }
  .spinning { animation: spin 0.8s linear infinite; color: var(--spin-color) !important; stroke: var(--spin-color); stroke-width: 2.5; font-weight: 900; }
  /* ── Compact status view ── */
  .compact-strip {
    width: 100%;
    min-height: 100vh;
    background: var(--bg-base);
    color: var(--text-primary);
    padding: 10px;
    display: flex;
    flex-direction: column;
    gap: 4px;
    overflow-y: hidden;
    box-sizing: border-box;
  }
  .compact-global {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 4px 0;
  }
  .compact-ring { width: 36px; height: 36px; flex-shrink: 0; }
  .compact-global-text { display: flex; flex-direction: column; }
  .compact-global-pct { font-size: 16px; font-weight: 700; line-height: 1.1; }
  .compact-global-label { font-size: 10px; color: var(--text-dim); }
  .compact-sep { border-top: 1px solid var(--border); margin: 2px 0; }
  .compact-acct {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 6px;
    border-radius: 8px;
    border: none;
    background: transparent;
    color: inherit;
    cursor: pointer;
    transition: background 0.1s;
    text-align: left;
    width: 100%;
    font-family: inherit;
  }
  .compact-acct:hover { background: var(--bg-hover); }
  .compact-acct-expanded { background: var(--bg-hover); }
  .compact-acct-cred-err { background: #3a161b; }
  .compact-acct-cred-err:hover { background: #4a1c23; }
  :global([data-theme="light"]) .compact-acct-cred-err { background: #fee2e2; }
  :global([data-theme="light"]) .compact-acct-cred-err:hover { background: #fcd5d5; }
  .compact-acct-cred-warn { background: #3a240e; }
  .compact-acct-cred-warn:hover { background: #4a2e12; }
  :global([data-theme="light"]) .compact-acct-cred-warn { background: #feebd0; }
  :global([data-theme="light"]) .compact-acct-cred-warn:hover { background: #fde0ba; }
  .compact-acct-cred-offline { background: #3a161b; }
  .compact-acct-cred-offline:hover { background: #4a1c23; }
  :global([data-theme="light"]) .compact-acct-cred-offline { background: #fee2e2; }
  :global([data-theme="light"]) .compact-acct-cred-offline:hover { background: #fcd5d5; }
  .compact-acct-ring { width: 24px; height: 24px; flex-shrink: 0; }
  .compact-acct-info { display: flex; flex-direction: column; min-width: 0; flex: 1; }
  .compact-acct-name {
    font-size: 11px; font-weight: 600;
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .compact-acct-stat { font-size: 9px; }
  .compact-chevron {
    font-size: 10px;
    color: var(--text-dim);
    flex-shrink: 0;
    width: 12px;
    text-align: center;
  }
  .compact-repo-list {
    padding: 0 0 4px 8px;
    border-left: 2px solid var(--border);
    margin-left: 17px;
    margin-bottom: 2px;
  }
  .compact-row {
    display: flex;
    align-items: center;
    gap: 5px;
    padding: 2px 6px;
    font-size: 11px;
    border-radius: 4px;
    transition: background 0.1s;
  }
  .compact-row:hover { background: var(--bg-hover); }
  .compact-row-ok { opacity: 0.5; }
  .compact-dot { font-size: 9px; flex-shrink: 0; width: 10px; text-align: center; }
  .compact-repo-name {
    flex: 1;
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
    font-weight: 500;
  }
  .compact-badge {
    font-size: 8px;
    font-weight: 600;
    white-space: nowrap;
    padding: 0 4px;
    border-radius: 4px;
    background: var(--bg-card);
  }
  .compact-actions {
    display: flex;
    gap: 6px;
    align-items: center;
  }
  .compact-action-btn {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-dim);
    border-radius: 6px;
    font-size: 10px;
    cursor: pointer;
    transition: all 0.12s;
    padding: 4px 8px;
  }
  .compact-action-btn:hover { color: var(--text-primary); border-color: #4B95E9; }
  .compact-full-btn { flex: 1; text-align: center; }
  /* Compact mirror pill */
  .compact-mirror-pill {
    display: flex; align-items: center; gap: 4px;
    padding: 2px 8px; font-size: 11px; color: var(--text-muted);
  }
  .compact-mirror-dot { font-size: 10px; }
  .compact-mirror-label { font-weight: 500; }
</style>
