<script lang="ts">
  // Top bar: the sidebar button, logo, compact health rings (the repo ring, plus
  // the mirror ring in Advanced), the action buttons, the Simple/Advanced switch
  // and the Settings gear.
  import { accounts, summary, simpleSummary, mirrorSummary, themeStore } from '../stores';
  import { statusColor } from '../status';
  import { t } from '../i18n';
  import { tooltip } from '../tooltip';
  import UIIcon from '../UIIcon.svelte';
  import Logo from '../Logo.svelte';

  export let simple: boolean;
  export let syncing: boolean;
  export let fetchingAll: boolean;
  export let themeChoice: 'system' | 'light' | 'dark';
  export let deleteMode: boolean;
  export let showSettings: boolean;
  export let syncAll: () => Promise<void>;
  export let runFetchAll: () => Promise<void>;
  export let toggleViewMode: () => Promise<void>;
  export let cycleTheme: () => void;
  export let setUIMode: (mode: 'simple' | 'advanced') => Promise<void>;
  // The sidebar button: folds the sidebar to its rail and back (⌘B / Ctrl+B).
  export let rail: boolean;
  export let sidebarShortcut: string;
  export let toggleSidebar: () => void;
  // A ring with something pending opens the view that lists it.
  export let setView: (view: string) => void;

  $: sc = (s: string) => statusColor(s, $themeStore);
  $: sideTip = $t(rail ? 'nav.expand' : 'nav.collapse', { key: sidebarShortcut });
  // The repo ring: Simple counts saved projects, Advanced up-to-date clones.
  $: repoDone = simple ? $simpleSummary.saved : $summary.clean;
  $: repoTotal = simple ? $simpleSummary.total : $summary.total;
  $: repoPending = repoTotal - repoDone;
  // One fact per line in the hover tooltip.
  $: repoTip = (simple
    ? [$t('simple.savedTip', { n: repoDone, total: repoTotal }),
       $simpleSummary.help > 0 ? $t('simple.needHelpN', { n: $simpleSummary.help }) : '',
       $simpleSummary.wip + $simpleSummary.unpushed > 0 ? $t('simple.wipN', { n: $simpleSummary.wip + $simpleSummary.unpushed }) : '',
       $simpleSummary.update > 0 ? $t('simple.updateN', { n: $simpleSummary.update }) : '',
       $simpleSummary.online > 0 ? $t('simple.onlineN', { n: $simpleSummary.online }) : '']
    : [$t('top.upToDateTip', { n: repoDone, total: repoTotal }),
       repoPending > 0 ? `${repoPending} ${repoPending === 1 ? $t('account.needsAttention') : $t('account.needAttentionPlural')}` : ''])
    .filter(Boolean).join('\n');
</script>

<header class="topbar">
  <button class="icon-btn hamb" on:click={toggleSidebar} aria-expanded={!rail} aria-label={sideTip} title={sideTip}><UIIcon name="menu" /></button>
  <div class="brand">
    <Logo class="logo-svg" />
    <span class="title">gitbox</span>
  </div>
  <!-- Compact rings without labels, in both modes. The icon names what the
       ring tracks, the corner badge counts what is not there yet, and the
       hover tooltip spells it out. Simple counts saved projects, Advanced
       up-to-date clones. With something pending, a click opens Needs
       attention (the mirror ring opens Mirrors). -->
  <div class="health">
    <button class="health-ring health-ring-mini" class:health-idle={repoPending === 0} aria-label={repoTip}
      aria-disabled={repoPending === 0} tabindex={repoPending > 0 ? 0 : -1} use:tooltip={{ text: repoTip, delay: 150 }}
      on:click={() => repoPending > 0 && setView('attention')}
      style="--pct: {repoTotal ? (repoDone / repoTotal) * 100 : 0}">
      <svg class="health-icon" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <circle cx="5" cy="3.5" r="1.6"/><circle cx="5" cy="12.5" r="1.6"/><circle cx="11" cy="6" r="1.6"/>
        <path d="M5 5.1v5.8M11 7.6c0 2.4-6 1.6-6 3.3"/>
      </svg>
      {#if repoPending > 0}
        <span class="health-badge" style="--cnt: {sc('dirty')}">{repoPending > 99 ? '99+' : repoPending}</span>
      {/if}
    </button>
    {#if !simple && $mirrorSummary.total > 0}
      {@const mirrorPending = $mirrorSummary.total - $mirrorSummary.active}
      {@const mirrorTip = $mirrorSummary.error > 0
        ? $t('top.mirrorsActiveErrors', { active: $mirrorSummary.active, total: $mirrorSummary.total, n: $mirrorSummary.error })
        : $t('top.mirrorsActive', { active: $mirrorSummary.active, total: $mirrorSummary.total })}
      <button class="health-ring health-ring-mini" class:health-idle={mirrorPending === 0} aria-label={mirrorTip}
        aria-disabled={mirrorPending === 0} tabindex={mirrorPending > 0 ? 0 : -1} use:tooltip={{ text: mirrorTip, delay: 150 }}
        on:click={() => mirrorPending > 0 && setView('mirrors')}
        style="--pct: {($mirrorSummary.active / $mirrorSummary.total) * 100}{$mirrorSummary.error > 0 ? '; --ring-accent: #D81E5B' : ''}">
        <svg class="health-icon" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M2.5 5.5h10M10 3l2.5 2.5L10 8M13.5 10.5h-10M6 8l-2.5 2.5L6 13"/>
        </svg>
        {#if mirrorPending > 0}
          <span class="health-badge" style="--cnt: {sc('dirty')}">{mirrorPending > 99 ? '99+' : mirrorPending}</span>
        {/if}
      </button>
    {/if}
  </div>
  <div class="topbar-actions">
    <button class="icon-btn" on:click={syncAll}
      disabled={syncing || ($summary.behind === 0 && $summary.notCloned === 0)}
      title={syncing ? $t(simple ? 'simple.updating' : 'top.pulling') : $t(simple ? 'simple.updateAll' : 'top.pullAll')}>
      <span class="icon-spin" class:spinning={syncing}><UIIcon name="pull" /></span>
    </button>
    <button class="icon-btn" on:click={runFetchAll} disabled={fetchingAll}
      title={fetchingAll ? $t(simple ? 'simple.checking' : 'top.fetching') : $t(simple ? 'simple.checkAll' : 'top.fetchAll')}>
      <span class="icon-spin" class:spinning={fetchingAll}><UIIcon name="fetch" /></span>
    </button>
    {#if !simple}
    <button class="icon-btn" class:delete-active={deleteMode} on:click={() => deleteMode = !deleteMode} disabled={Object.keys($accounts).length === 0} title={deleteMode ? $t('top.exitDeleteMode') : $t('top.deleteMode')}><UIIcon name="trash" /></button>
    <button class="icon-btn" on:click={toggleViewMode} title={$t('top.compactView')}><UIIcon name="compact" /></button>
    <button class="icon-btn" on:click={cycleTheme} title={$t('top.themeTip', { name: $t(`theme.${themeChoice}`) })}><UIIcon name={themeChoice === 'light' || themeChoice === 'dark' ? `theme-${themeChoice}` : 'theme-system'} /></button>
    {/if}
    <button class="mode-switch" class:mode-switch-on={!simple} role="switch" aria-checked={!simple}
      aria-label={$t('settings.experience')} title={simple ? $t('settings.simple') : $t('settings.advanced')}
      on:click={() => setUIMode(simple ? 'advanced' : 'simple')}><span class="mode-switch-knob"></span></button>
    <button class="icon-btn" on:click={() => showSettings = !showSettings} title={$t('top.settings')} class:active-gear={showSettings}><UIIcon name="gear" /></button>
  </div>
</header>

<style>
  .topbar { display: flex; align-items: center; gap: 16px; padding: 14px 24px 14px 11px; border-bottom: 1px solid var(--border); }
  .hamb { margin-right: -6px; }
  @keyframes spin { to { transform: rotate(360deg); } }
  .brand { display: flex; align-items: center; gap: 8px; }
  .brand :global(.logo-svg) { width: 26px; height: 26px; }
  .title { font-size: 17px; font-weight: 700; letter-spacing: -0.3px; }
  .health { display: flex; align-items: center; gap: 8px; margin-left: auto; }
  .health-ring {
    width: 38px; height: 38px; border-radius: 50%;
    background: conic-gradient(var(--ring-accent) calc(var(--pct) * 1%), var(--ring-bg) 0);
    display: flex; align-items: center; justify-content: center; position: relative;
  }
  .health-ring::before { content: ''; position: absolute; inset: 4px; background: var(--bg-base); border-radius: 50%; transition: background 0.2s; }
  /* Advanced header rings: smaller, icon inside, attention count in a corner badge. */
  .health-ring-mini { width: 28px; height: 28px; margin-right: 6px; padding: 0; border: none; cursor: pointer; font: inherit; }
  .health-ring-mini.health-idle { cursor: default; }
  .health-ring-mini:focus-visible { outline: 1px solid var(--border-hover); outline-offset: 2px; }
  .health-ring-mini::before { inset: 3px; }
  .health-icon { position: relative; z-index: 1; width: 14px; height: 14px; color: var(--text-muted); }
  /* The sidebar's count style: a soft tint of the attention colour over an
     opaque base, with the number in that colour. */
  .health-badge {
    position: absolute; z-index: 2; top: -5px; right: -8px;
    min-width: 15px; height: 15px; padding: 0 4px; box-sizing: border-box;
    border-radius: 8px; border: 1.5px solid var(--bg-base); background: var(--bg-base);
    color: var(--cnt); font-size: 9px; font-weight: 700; line-height: 12px; text-align: center;
    font-variant-numeric: tabular-nums; isolation: isolate;
  }
  .health-badge::before { content: ''; position: absolute; inset: 0; z-index: -1; border-radius: inherit; background: var(--cnt); opacity: 0.16; }
  .topbar-actions { display: flex; align-items: center; gap: 6px; }
  .icon-spin { display: inline-flex; align-items: center; }
  .spinning { animation: spin 0.8s linear infinite; color: var(--spin-color) !important; stroke: var(--spin-color); stroke-width: 2.5; font-weight: 900; }
  /* Simple / Advanced switch in the top bar: grey pill = Simple, red (the
     theme's alert colour) = Advanced, the mode with destructive power. */
  .mode-switch {
    position: relative; width: 30px; height: 16px; padding: 0; margin: 0 4px;
    border: none; border-radius: 8px; cursor: pointer; flex-shrink: 0;
    background: var(--border-hover); transition: background 0.18s;
  }
  .mode-switch-knob {
    position: absolute; top: 2px; left: 2px; width: 12px; height: 12px; border-radius: 50%;
    background: #fafafa; box-shadow: 0 1px 2px rgba(0, 0, 0, 0.35); transition: transform 0.18s;
  }
  .mode-switch-on { background: var(--alert); }
  .mode-switch-on .mode-switch-knob { transform: translateX(14px); }
  .mode-switch:focus-visible { outline: 1px solid var(--border-hover); outline-offset: 2px; }
  .active-gear { color: var(--text-primary) !important; }
  /* ── Delete mode ── */
  .delete-active { color: var(--alert) !important; }
</style>
