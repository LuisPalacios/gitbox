<script lang="ts">
  // The sidebar: the Needs attention and All views, one entry per account
  // (provider logo, health mark, name, health words, attention count), Add
  // account, Found on disk, and Mirrors and Workspaces in Advanced. As a
  // rail it keeps only the icons, with the names in tooltips.
  import { accounts, mirrorSummary, workspaces, themeStore } from '../stores';
  import { statusColor, accountHealth } from '../status';
  import { providerLabel } from '../theme';
  import { accountView } from '../views';
  import { t } from '../i18n';
  import { tooltip } from '../tooltip';
  import UIIcon from '../UIIcon.svelte';
  import BrandIcon, { providerLogo } from '../BrandIcon.svelte';

  export let view: string;
  export let rail: boolean;
  // Expanded over the content in a narrow window (not saved).
  export let simple: boolean;
  export let credStatuses: Record<string, any>;
  export let attention: { total: number; byAccount: Record<string, number> };
  export let totalRepos: number;
  export let orphanCount: number;
  // Ignored clones keep the entry reachable, without a count (#149).
  export let ignoredCount = 0;
  export let setView: (view: string) => void;
  export let onAddAccount: () => void;
  export let showOrphanModal: () => void;

  $: sc = (s: string) => statusColor(s, $themeStore);
  $: health = (key: string) => accountHealth(credStatuses[key]?.status, simple, credStatuses[key]?.expiresAt);
  $: attentionLabel = $t(simple ? 'nav.attentionSimple' : 'nav.attention');
  $: allLabel = $t(simple ? 'nav.allSimple' : 'nav.all');
  $: addLabel = simple ? $t('nav.connect') : $t('account.add');
  $: foundLabel = $t('nav.found');
  $: foundTip = orphanCount > 0 ? $t(simple ? 'nav.foundTipSimple' : 'nav.foundTip', { n: orphanCount }) : foundLabel;
  $: wsCount = Object.keys($workspaces).length;
  $: mirrorCount = $mirrorSummary.total > 0 ? `${$mirrorSummary.active}/${$mirrorSummary.total}` : '';
  const badge = (n: number) => (n > 99 ? '99+' : String(n));
</script>

<!-- The provider logo with the account's health mark in its corner; a
     letter when the provider has no logo. -->
{#snippet logo(key: string, provider: string)}
  {@const h = health(key)}
  {@const name = providerLogo(provider)}
  <span class="plogo">
    {#if name}<BrandIcon {name} />{:else}<span class="plogo-letter">{providerLabel(provider).charAt(0).toUpperCase()}</span>{/if}
    <span class="hb" style="color: {sc(h.color)}" aria-hidden="true">{h.mark}</span>
  </span>
{/snippet}

<nav class="side" class:rail aria-label={$t('nav.label')}>
  {#if rail}
    <button class="rail-btn" aria-current={view === 'attention'} aria-label={attentionLabel}
      use:tooltip={{ text: attentionLabel, delay: 150 }} on:click={() => setView('attention')}>
      <span class="navi" style="color: {attention.total > 0 ? sc('dirty') : 'inherit'}"><UIIcon name="attention" /></span>
      {#if attention.total > 0}<span class="rail-cnt" style="--cnt: {sc('dirty')}">{badge(attention.total)}</span>{/if}
    </button>
    <button class="rail-btn" aria-current={view === 'all'} aria-label={allLabel}
      use:tooltip={{ text: allLabel, delay: 150 }} on:click={() => setView('all')}><UIIcon name="list" /></button>
    <span class="rail-sep"></span>
    {#each Object.entries($accounts) as [key, acct] (key)}
      {@const tip = `${key} · ${$t(health(key).key, health(key).vars)}`}
      {@const n = attention.byAccount[key] || 0}
      <button class="rail-btn" aria-current={view === accountView(key)} aria-label={tip}
        use:tooltip={{ text: tip, delay: 150 }} on:click={() => setView(accountView(key))}>
        {@render logo(key, acct.provider)}
        {#if n > 0}<span class="rail-cnt" style="--cnt: {sc('dirty')}">{badge(n)}</span>{/if}
      </button>
    {/each}
    <span class="rail-sep"></span>
    <button class="rail-btn" aria-label={addLabel} use:tooltip={{ text: addLabel, delay: 150 }} on:click={onAddAccount}><UIIcon name="plus" /></button>
    {#if orphanCount > 0 || ignoredCount > 0}
      <button class="rail-btn" aria-label={foundTip} use:tooltip={{ text: foundTip, delay: 150 }} on:click={showOrphanModal}>
        <UIIcon name="search" />{#if orphanCount > 0}<span class="rail-cnt" style="--cnt: {sc('dirty')}">{badge(orphanCount)}</span>{/if}
      </button>
    {/if}
    {#if !simple}
      <button class="rail-btn" aria-current={view === 'mirrors'} aria-label={$t('tab.mirrors')}
        use:tooltip={{ text: $t('tab.mirrors'), delay: 150 }} on:click={() => setView('mirrors')}><UIIcon name="mirror" /></button>
      <button class="rail-btn" aria-current={view === 'workspaces'} aria-label={$t('tab.workspaces')}
        use:tooltip={{ text: $t('tab.workspaces'), delay: 150 }} on:click={() => setView('workspaces')}><UIIcon name="grid" /></button>
    {/if}
  {:else}
    <button class="nav" aria-current={view === 'attention'} on:click={() => setView('attention')}>
      <span class="navi" style="color: {attention.total > 0 ? sc('dirty') : 'inherit'}"><UIIcon name="attention" /></span>
      <span class="nav-label">{attentionLabel}</span>
      {#if attention.total > 0}<span class="cnt">{attention.total}</span>{/if}
    </button>
    <button class="nav" aria-current={view === 'all'} on:click={() => setView('all')}>
      <span class="navi"><UIIcon name="list" /></span>
      <span class="nav-label">{allLabel}</span>
      <span class="cnt">{totalRepos}</span>
    </button>
    <div class="side-sec">{$t('tab.accounts')}</div>
    {#each Object.entries($accounts) as [key, acct] (key)}
      {@const h = health(key)}
      {@const n = attention.byAccount[key] || 0}
      <button class="nav" aria-current={view === accountView(key)} title={key} on:click={() => setView(accountView(key))}>
        {@render logo(key, acct.provider)}
        <span class="two">
          <span class="nav-label">{key}</span>
          <small style={h.level === 'ok' || h.level === 'pending' ? '' : `color: ${sc(h.color)}`}>{$t(h.key, h.vars)}</small>
        </span>
        {#if n > 0}<span class="cnt">{n}</span>{/if}
      </button>
    {/each}
    <div class="side-sec">{$t('nav.more')}</div>
    <button class="nav" on:click={onAddAccount}>
      <span class="navi"><UIIcon name="plus" /></span>
      <span class="nav-label">{addLabel}</span>
    </button>
    {#if orphanCount > 0 || ignoredCount > 0}
      <button class="nav" title={foundTip} on:click={showOrphanModal}>
        <span class="navi"><UIIcon name="search" /></span>
        <span class="nav-label">{foundLabel}</span>
        {#if orphanCount > 0}<span class="cnt">{orphanCount}</span>{/if}
      </button>
    {/if}
    {#if !simple}
      <button class="nav" aria-current={view === 'mirrors'} on:click={() => setView('mirrors')}>
        <span class="navi"><UIIcon name="mirror" /></span>
        <span class="nav-label">{$t('tab.mirrors')}</span>
        {#if mirrorCount}<span class="cnt" style={$mirrorSummary.error > 0 ? `color: ${sc('error')}` : ''}>{mirrorCount}</span>{/if}
      </button>
      <button class="nav" aria-current={view === 'workspaces'} on:click={() => setView('workspaces')}>
        <span class="navi"><UIIcon name="grid" /></span>
        <span class="nav-label">{$t('tab.workspaces')}</span>
        {#if wsCount > 0}<span class="cnt">{wsCount}</span>{/if}
      </button>
    {/if}
  {/if}
</nav>

<style>
  .side {
    width: 220px; flex: none; overflow-y: auto; overflow-x: hidden;
    border-right: 1px solid var(--border); background: var(--bg-card);
    padding: 10px 8px; display: flex; flex-direction: column; gap: 2px;
  }
  /* A narrow window shows the expanded sidebar over the content. */
  .side-sec {
    font-size: 10px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.08em;
    color: var(--text-dim); padding: 12px 8px 4px;
  }
  .nav {
    width: 100%; display: flex; align-items: center; gap: 8px; padding: 6px 8px;
    border: none; border-radius: 7px; background: none; text-align: left; cursor: pointer;
    font-size: 13px; color: var(--text-secondary);
  }
  .nav:hover { background: var(--bg-hover); color: var(--text-primary); }
  .nav[aria-current="true"] { background: var(--bg-hover); color: var(--text-primary); font-weight: 600; }
  .nav:focus-visible, .rail-btn:focus-visible { outline: 1px solid var(--border-hover); outline-offset: -1px; }
  .nav-label { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .navi { width: var(--icon); height: var(--icon); flex: none; display: inline-flex; align-items: center; justify-content: center; color: var(--text-muted); }
  .rail-btn .navi { color: inherit; }
  .cnt { margin-left: auto; padding-left: 6px; font-size: 11px; font-weight: 500; color: var(--text-muted); font-variant-numeric: tabular-nums; }
  .two { display: flex; flex-direction: column; min-width: 0; line-height: 1.3; }
  .two small { font-size: 10.5px; font-weight: 400; color: var(--text-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  /* Provider logo with the health mark in its corner. */
  .plogo { position: relative; width: var(--icon); height: var(--icon); flex: none; display: inline-flex; align-items: center; justify-content: center; color: var(--text-primary); }
  .plogo-letter {
    width: var(--icon); height: var(--icon); border-radius: 4px; border: 1px solid var(--border-hover);
    display: inline-flex; align-items: center; justify-content: center; font-size: 10px; font-weight: 700;
  }
  .hb {
    position: absolute; right: -5px; bottom: -5px; width: 12px; height: 12px; border-radius: 50%;
    background: var(--bg-card); display: inline-flex; align-items: center; justify-content: center;
    font-size: 8px; line-height: 1;
  }
  /* ── Rail ── */
  .side.rail { width: 52px; padding: 10px 6px; align-items: center; gap: 4px; }
  .rail-btn {
    position: relative; width: 40px; height: 40px; flex: none; padding: 0;
    display: inline-flex; align-items: center; justify-content: center;
    border: none; border-radius: 9px; background: none; cursor: pointer; color: var(--text-secondary);
  }
  .rail-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
  .rail-btn[aria-current="true"] { background: var(--bg-hover); box-shadow: inset 0 0 0 1px var(--border-hover); color: var(--text-primary); }
  .rail-btn .hb { background: var(--bg-card); }
  .rail-btn:hover .hb, .rail-btn[aria-current="true"] .hb { background: var(--bg-hover); }
  .rail-sep { width: 28px; border-top: 1px solid var(--border); margin: 6px 0; flex: none; }
  /* A soft tint of the attention colour with the number in that colour, so
     the counts inform without shouting. The ::before layer tints an opaque
     base, so the logo never shows through. */
  .rail-cnt {
    position: absolute; top: 1px; right: 0; min-width: 16px; height: 16px; padding: 0 4px; box-sizing: border-box;
    border-radius: 8px; background: var(--bg-base); color: var(--cnt); font-size: 9.5px; font-weight: 700;
    display: inline-flex; align-items: center; justify-content: center; font-variant-numeric: tabular-nums;
    isolation: isolate;
  }
  .rail-cnt::before { content: ''; position: absolute; inset: 0; z-index: -1; border-radius: inherit; background: var(--cnt); opacity: 0.16; }
</style>
