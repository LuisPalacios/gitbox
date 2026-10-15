<script lang="ts">
  // Top bar: logo, health rings (Simple: one saved ring; Advanced: repo and
  // mirror rings), the action buttons, the Simple/Advanced switch and the
  // Settings gear.
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

  $: sc = (s: string) => statusColor(s, $themeStore);
</script>

<header class="topbar">
  <div class="brand">
    <Logo class="logo-svg" />
    <span class="title">gitbox</span>
  </div>
  {#if simple}
  <div class="health">
    <span class="health-ring" style="--pct: {$simpleSummary.total ? ($simpleSummary.saved / $simpleSummary.total) * 100 : 0}">
      <span class="health-num">{$simpleSummary.saved}/{$simpleSummary.total}</span>
    </span>
    <span class="health-label">{$simpleSummary.help > 0 ? $t('simple.needHelpN', { n: $simpleSummary.help })
      : $simpleSummary.unsaved > 0 ? $t('simple.notSavedN', { n: $simpleSummary.unsaved })
      : $t('simple.allSaved')}</span>
  </div>
  {:else}
  <!-- Advanced: compact rings without labels. The icon names what the ring
       tracks, the corner badge counts what is not there yet, and the hover
       tooltip spells it out. -->
  {@const repoPending = $summary.total - $summary.clean}
  {@const repoTip = `${$summary.clean}/${$summary.total} synced${repoPending > 0 ? ` · ${repoPending} ${repoPending === 1 ? $t('account.needsAttention') : $t('account.needAttentionPlural')}` : ''}`}
  <div class="health">
    <span class="health-ring health-ring-mini" role="img" aria-label={repoTip} use:tooltip={{ text: repoTip, delay: 150 }}
      style="--pct: {$summary.total ? ($summary.clean / $summary.total) * 100 : 0}">
      <svg class="health-icon" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <circle cx="5" cy="3.5" r="1.6"/><circle cx="5" cy="12.5" r="1.6"/><circle cx="11" cy="6" r="1.6"/>
        <path d="M5 5.1v5.8M11 7.6c0 2.4-6 1.6-6 3.3"/>
      </svg>
      {#if repoPending > 0}
        <span class="health-badge" style="background: {sc('behind')}">{repoPending > 99 ? '99+' : repoPending}</span>
      {/if}
    </span>
    {#if $mirrorSummary.total > 0}
      {@const mirrorPending = $mirrorSummary.total - $mirrorSummary.active}
      {@const mirrorTip = `${$mirrorSummary.active}/${$mirrorSummary.total} mirrors active${$mirrorSummary.error > 0 ? ` · ${$mirrorSummary.error} with errors` : ''}`}
      <span class="health-ring health-ring-mini" role="img" aria-label={mirrorTip} use:tooltip={{ text: mirrorTip, delay: 150 }}
        style="--pct: {($mirrorSummary.active / $mirrorSummary.total) * 100}{$mirrorSummary.error > 0 ? '; --ring-accent: #D81E5B' : ''}">
        <svg class="health-icon" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M2.5 5.5h10M10 3l2.5 2.5L10 8M13.5 10.5h-10M6 8l-2.5 2.5L6 13"/>
        </svg>
        {#if mirrorPending > 0}
          <span class="health-badge" style="background: {$mirrorSummary.error > 0 ? '#D81E5B' : sc('behind')}">{mirrorPending > 99 ? '99+' : mirrorPending}</span>
        {/if}
      </span>
    {/if}
  </div>
  {/if}
  <div class="topbar-actions">
    <button class="btn-gear" on:click={syncAll}
      disabled={syncing || ($summary.behind === 0 && $summary.notCloned === 0)}
      title="{simple ? $t('simple.updateAll') : syncing ? 'Pulling...' : 'Pull All'}">
      <span class="icon-spin" class:spinning={syncing}><UIIcon name="pull" /></span>
    </button>
    {#if !simple}
    <button class="btn-gear" on:click={runFetchAll} disabled={fetchingAll}
      title="{fetchingAll ? 'Fetching...' : 'Fetch All'}">
      <span class="icon-spin" class:spinning={fetchingAll}><UIIcon name="fetch" /></span>
    </button>
    <button class="btn-gear" class:delete-active={deleteMode} on:click={() => deleteMode = !deleteMode} disabled={Object.keys($accounts).length === 0} title="{deleteMode ? 'Exit delete mode' : 'Delete mode'}"><UIIcon name="trash" /></button>
    <button class="btn-gear" on:click={toggleViewMode} title="Compact view"><UIIcon name="compact" /></button>
    <button class="btn-gear" on:click={cycleTheme} title="Theme: {themeChoice}"><UIIcon name={themeChoice === 'light' || themeChoice === 'dark' ? `theme-${themeChoice}` : 'theme-system'} /></button>
    {/if}
    <button class="mode-switch" class:mode-switch-on={!simple} role="switch" aria-checked={!simple}
      aria-label={$t('settings.experience')} title={simple ? $t('settings.simple') : $t('settings.advanced')}
      on:click={() => setUIMode(simple ? 'advanced' : 'simple')}><span class="mode-switch-knob"></span></button>
    <button class="btn-gear" on:click={() => showSettings = !showSettings} title="Settings" class:active-gear={showSettings}><UIIcon name="gear" /></button>
  </div>
</header>

<style>
  .topbar { display: flex; align-items: center; gap: 16px; padding: 14px 24px; border-bottom: 1px solid var(--border); }
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
  .health-num { position: relative; z-index: 1; font-size: 10px; font-weight: 700; }
  .health-label { font-size: 12px; color: var(--text-muted); }
  /* Advanced header rings: smaller, icon inside, attention count in a corner badge. */
  .health-ring-mini { width: 28px; height: 28px; margin-right: 6px; cursor: default; }
  .health-ring-mini::before { inset: 3px; }
  .health-icon { position: relative; z-index: 1; width: 14px; height: 14px; color: var(--text-muted); }
  .health-badge {
    position: absolute; z-index: 2; top: -5px; right: -8px;
    min-width: 15px; height: 15px; padding: 0 4px; box-sizing: border-box;
    border-radius: 8px; border: 1.5px solid var(--bg-base);
    color: #fff; font-size: 9px; font-weight: 700; line-height: 12px; text-align: center;
  }
  .topbar-actions { display: flex; gap: 8px; }
  .icon-spin { display: inline-flex; }
  .spinning { animation: spin 0.8s linear infinite; color: var(--spin-color) !important; stroke: var(--spin-color); stroke-width: 2.5; font-weight: 900; }
  .btn-gear {
    background: none; border: 1px solid transparent; color: var(--text-muted);
    font-size: 18px; cursor: pointer; padding: 4px 6px; border-radius: 6px; transition: all 0.12s;
    width: 32px; height: 32px; display: inline-flex; align-items: center; justify-content: center;
    line-height: 1;
  }
  .btn-gear:hover:not(:disabled) { color: var(--text-primary); background: var(--bg-hover); }
  .btn-gear:disabled { opacity: 0.3; cursor: default; }
  /* Simple / Advanced switch in the top bar: grey pill = Simple, green = Advanced. */
  .mode-switch {
    position: relative; align-self: center; width: 30px; height: 16px; padding: 0; margin: 0 4px;
    border: none; border-radius: 8px; cursor: pointer; flex-shrink: 0;
    background: var(--border-hover); transition: background 0.18s;
  }
  .mode-switch-knob {
    position: absolute; top: 2px; left: 2px; width: 12px; height: 12px; border-radius: 50%;
    background: #fafafa; box-shadow: 0 1px 2px rgba(0, 0, 0, 0.35); transition: transform 0.18s;
  }
  .mode-switch-on { background: #22c55e; }
  .mode-switch-on .mode-switch-knob { transform: translateX(14px); }
  :global([data-theme="light"]) .mode-switch-on { background: #16a34a; }
  .mode-switch:focus-visible { outline: 1px solid var(--border-hover); outline-offset: 2px; }
  .active-gear { color: var(--text-primary) !important; }
  /* ── Delete mode ── */
  .delete-active { color: #D81E5B !important; }
</style>
