<script lang="ts">
  // One clone of the repo list: the Simple row (dot, short name, one
  // sentence, one button, ⋯) or the Advanced row (status shape, workspace
  // and PR badges, branch, counts, launchers, fetch and ⋯).
  import { fade } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { accounts, repoStates, prSettings, prsByAccount, lookupPRSummary, workspaces, workspaceMemberships, isSynced, themeStore } from '../stores';
  import { statusColor, simpleKind, simpleColor, simpleLabelKey, simpleUnsaved, rowBusy, rowGlyph, advancedLabel, COUNT_BADGES } from '../status';
  import type { SimpleKind } from '../status';
  import { t } from '../i18n';
  import { onActivateKey } from '../keyboard';
  import LauncherMenu from '../LauncherMenu.svelte';
  import PRPopover from '../PRPopover.svelte';
  import BrandIcon, { terminalLogo } from '../BrandIcon.svelte';
  import type { RepoState, EditorInfo, AIHarnessInfo, TerminalProfileInfo } from '../types';

  // The row's group, its own source (another account's for a nested clone
  // shown under its container) and its place in the tree.
  export let sourceKey: string;
  export let rowSourceKey: string;
  export let repoName: string;
  export let depth: number;
  export let childCount: number;
  export let descendants: string[];
  export let rowAccountKey: string;
  export let repoKey: string;
  export let isContainer: boolean;
  export let state: RepoState;
  export let rowOpensDetail: boolean;
  export let simple: boolean;
  export let deleteMode: boolean;
  export let fetchingRepos: Record<string, boolean>;
  export let collapsedRepos: Set<string>;
  export let tentativeContainers: Set<string>;
  export let workspaceBusy: boolean;
  export let openWsPopover: string | null;
  export let revealLabel: string;
  export let configEditors: EditorInfo[];
  export let configProfiles: TerminalProfileInfo[];
  export let configAIHarnesses: AIHarnessInfo[];
  export let actionMenuRepo: string | null;
  export let openPRPopover: string | null;
  export let activateRepoRow: (sourceKey: string, repoName: string, status: string) => void;
  export let askDelete: (sourceKey: string, repoKey: string, status: string) => void;
  export let syncRepo: (sourceKey: string, repoKey: string) => void;
  export let cloneRepo: (sourceKey: string, repoKey: string) => void;
  export let fetchRepo: (sourceKey: string, repoKey: string) => void;
  export let toggleActionMenu: (repoKey: string) => void;
  export let closeActionMenu: () => void;
  export let toggleWsPopover: (key: string) => void;
  export let openWorkspaceFromBadge: (wsKey: string) => Promise<void>;
  export let togglePRPopover: (key: string) => void;
  export let sweepBranches: (sourceKey: string, repoKey: string) => void;
  export let openMoveRepo: (sourceKey: string, repoKey: string) => void;
  export let toggleContainer: (sourceKey: string, repoName: string) => Promise<void>;
  export let rescanContainer: () => Promise<void>;
  export let toggleRepoCollapsed: (repoKey: string, descendants: string[]) => void;
  export let isContainerRepo: (sourceKey: string, repoName: string) => boolean;

  $: sc = (s: string) => statusColor(s, $themeStore);

  // requireRepoPath returns the row's local clone path, or shows the
  // "clone first" dialog and returns null. Path-based kebab actions used
  // to return silently when the path was missing (#79); a row cloned
  // in-session has none until clone:done or the next status refresh.
  function requireRepoPath(repoKey: string, title: string): string | null {
    const p = $repoStates[repoKey]?.path;
    if (!p) { void bridge.showErrorDialog(title, $t('launcher.cloneFirst')); }
    return p ?? null;
  }

  async function openRepoInExplorer(repoKey: string) {
    const path = requireRepoPath(repoKey, revealLabel);
    if (path) await bridge.openInExplorer(path);
    closeActionMenu();
  }

  async function openRepoInApp(repoKey: string, command: string) {
    const path = requireRepoPath(repoKey, 'Open in editor');
    if (path) await bridge.openInApp(path, command);
    closeActionMenu();
  }

  // openRepoProfile is the v2.1 launcher entry point — resolves the repo's
  // working directory then asks the Go side to expand the profile's
  // template via pkg/launch.ResolveArgs and spawn the terminal.
  async function openRepoProfile(repoKey: string, profileID: string) {
    const path = requireRepoPath(repoKey, 'Open profile');
    if (path) {
      try { await bridge.openProfile(path, profileID); }
      catch (e: any) { await bridge.showErrorDialog('Open profile', (e?.message || String(e))); }
    }
    closeActionMenu();
  }

  async function openRepoInAIHarness(repoKey: string, harness: AIHarnessInfo) {
    const state = $repoStates[repoKey];
    if (!state?.path) {
      await bridge.showErrorDialog('Open in ' + harness.name, $t('launcher.cloneFirst'));
      closeActionMenu();
      return;
    }
    try {
      await bridge.openInAIHarness(state.path, harness.command, harness.args || []);
    } catch (e: any) {
      await bridge.showErrorDialog('Open in ' + harness.name, (e?.message || String(e)));
    }
    closeActionMenu();
  }

  // The URL is resolved on the Go side from the authoritative config
  // (git.RepoWebURL), so a stale frontend store can't turn this into a
  // silent no-op; lookup failures surface in a dialog (#79).
  async function openRepoInBrowser(sourceKey: string, repoName: string) {
    try {
      await bridge.openRepoInBrowser(sourceKey, repoName);
    } catch (e: any) {
      await bridge.showErrorDialog('Navigate to repo', (e?.message || String(e)));
    }
    closeActionMenu();
  }

  // isTentativeContainer reports whether the backend suggested this repo as a
  // container. The set is passed in (not read off the closure) so Svelte tracks
  // it as a reactive dependency of the row markup. Callers gate this behind the
  // reactive `isContainer` check, so an already-flagged container never shows
  // the hint.
  function isTentativeContainer(sourceKey: string, repoName: string, tc: Set<string>): boolean {
    return tc.has(`${sourceKey}/${repoName}`);
  }

  // repoCanMove is the kebab-enable predicate:
  // only a cloned, clean, in-sync repo can be moved. Returns a string
  // tooltip when disabled, or '' when enabled.
  function repoMoveDisabledReason(state: RepoState | undefined): string {
    if (!state) return 'Clone first';
    if (state.status === 'not cloned' || state.status === 'error') return 'Clone first';
    if (state.status !== 'clean') return 'Repo must be clean (commit/push/fetch first)';
    if ((state.ahead ?? 0) > 0 || (state.behind ?? 0) > 0) return 'Repo must be in sync with origin';
    return '';
  }

  // Lookup the ordered list of workspaces a given clone belongs to.
  // Returns an empty array when the clone has no memberships, so the
  // caller can cheaply check `.length > 0` to hide/show UI.
  function membershipsFor(repoKey: string): string[] {
    return $workspaceMemberships[repoKey] || [];
  }

  function workspaceLabel(wsKey: string): string {
    const ws = $workspaces[wsKey];
    if (!ws) return wsKey;
    return ws.name && ws.name !== wsKey ? `${ws.name} (${wsKey})` : wsKey;
  }

  // Build the provider-native "all PRs" URL for a given repo full name.
  function providerPRsURL(providerName: string, baseURL: string, repoFull: string): string {
    const base = (baseURL || '').replace(/\/+$/, '') || defaultProviderBase(providerName);
    if (!base || !repoFull) return '';
    switch (providerName) {
      case 'github':   return `${base}/${repoFull}/pulls`;
      case 'gitlab':   return `${base}/${repoFull}/-/merge_requests`;
      case 'gitea':
      case 'forgejo':  return `${base}/${repoFull}/pulls`;
      default:         return '';
    }
  }

  function defaultProviderBase(providerName: string): string {
    if (providerName === 'github') return 'https://github.com';
    if (providerName === 'gitlab') return 'https://gitlab.com';
    return '';
  }

  // Simple row helpers: the short project name, the "not saved yet" detail,
  // and the one primary launcher (first AI harness, else editor, else folder).
  function shortName(repoName: string): string {
    return repoName.split('/').pop() || repoName;
  }
  function simpleTip(kind: SimpleKind, state: { status: string; modified: number; untracked: number }): string {
    if (kind === 'help') return $t('simple.tipHelp');
    if (kind !== 'wip' && kind !== 'unpushed') return '';
    if (state.status === 'ahead') return $t('simple.tipAhead');
    if (state.status === 'no upstream') return $t('simple.tipNoUpstream');
    return $t('simple.tipDirty', { n: (state.modified || 0) + (state.untracked || 0) });
  }
  // Every cloned row, Simple or Advanced, carries three icon-only launchers:
  // the first AI harness, the first editor, and the default terminal profile
  // (same defaults as the ⋯ menu). Each picks a brand logo by name, or a
  // generic icon.
  $: quickHarness = configAIHarnesses[0] || null;
  $: quickEditor = configEditors[0] || null;
  $: quickProfile = (() => {
    const visible = configProfiles.filter((p) => !p.hidden);
    return visible.find((p) => p.default) || visible[0] || null;
  })();
  function harnessIcon(h: AIHarnessInfo): string {
    const k = `${h.name} ${h.command}`.toLowerCase();
    if (k.includes('claude')) return 'claude';
    if (k.includes('codex') || k.includes('openai')) return 'openai';
    if (k.includes('gemini')) return 'gemini';
    if (k.includes('cursor')) return 'cursor';
    return 'ai';
  }
  function editorIcon(e: EditorInfo): string {
    const k = `${e.name} ${e.command}`.toLowerCase();
    if (k.includes('cursor')) return 'cursor';
    if (k.includes('zed')) return 'zed';
    if (k.includes('visual studio code') || k.includes('vs code') || k.includes('vscode') || /(^|[\\/ ])code(\.exe|\.cmd)?$/.test(e.command.toLowerCase())) return 'vscode';
    return 'ide';
  }
</script>

{#snippet quickLaunch(repoKey: string)}
  <span class="launch-group">
    {#if quickHarness}
      {@const h = quickHarness}
      <button class="btn-launch" title={$t('simple.btnOpenWith', { name: h.name })} aria-label={$t('simple.btnOpenWith', { name: h.name })}
        on:click|stopPropagation={() => openRepoInAIHarness(repoKey, h)}><BrandIcon name={harnessIcon(h)} /></button>
    {/if}
    {#if quickEditor}
      {@const ed = quickEditor}
      <button class="btn-launch" title={$t('simple.btnOpenWith', { name: ed.name })} aria-label={$t('simple.btnOpenWith', { name: ed.name })}
        on:click|stopPropagation={() => openRepoInApp(repoKey, ed.command)}><BrandIcon name={editorIcon(ed)} /></button>
    {/if}
    {#if quickProfile}
      {@const pr = quickProfile}
      <button class="btn-launch" title={$t('simple.btnOpenWith', { name: pr.name })} aria-label={$t('simple.btnOpenWith', { name: pr.name })}
        on:click|stopPropagation={() => openRepoProfile(repoKey, pr.id)}><BrandIcon name={terminalLogo(pr.terminal, pr.shell)} /></button>
    {/if}
  </span>
{/snippet}

<div class="repo-row" class:repo-row-clickable={rowOpensDetail}
  class:repo-row-nested={depth > 0}
  style={depth > 0 ? `padding-left: ${10 + depth * 22}px` : ''}
  role="button"
  tabindex={rowOpensDetail ? 0 : -1}
  aria-disabled={!rowOpensDetail}
  on:click={() => activateRepoRow(rowSourceKey, repoName, state.status)}
  on:keydown={onActivateKey(() => activateRepoRow(rowSourceKey, repoName, state.status))}>
  {#if rowSourceKey !== sourceKey}<span class="container-badge" title={rowSourceKey}>{rowAccountKey}</span>{/if}
  {#if deleteMode}
    <button class="btn-delete-x" on:click|stopPropagation={() => askDelete(rowSourceKey, repoName, state.status)} title="Delete {repoName}">&#10005;</button>
  {/if}
  {#if childCount > 0}
    {@const repoCollapsed = collapsedRepos.has(repoKey)}
    <button class="collapse-chevron collapse-chevron-btn"
      aria-expanded={!repoCollapsed}
      aria-label="{repoCollapsed ? $t('collapse.expand') : $t('collapse.collapse')} {repoName}"
      title={repoCollapsed ? $t('collapse.expand') : $t('collapse.collapse')}
      on:click|stopPropagation={() => toggleRepoCollapsed(repoKey, descendants)}
      on:keydown|stopPropagation>{repoCollapsed ? '▸' : '▾'}</button>
  {/if}
  {#if simple}
    <!-- Simple row (issue #124): dot, short name, one sentence, one button, ⋯ -->
    {@const kind = simpleKind(state.status)}
    {@const kindColor = simpleColor(kind, $themeStore)}
    <span class="dot" class:spinning={rowBusy(state.status, !!fetchingRepos[repoKey])} style="color: {kindColor}">{rowBusy(state.status, !!fetchingRepos[repoKey]) ? '↻' : '●'}</span>
    {#if childCount > 0}
      <span class="repo-name" title={repoName}><span class="repo-name-toggle" role="button" tabindex="-1"
        on:click={(e) => { e.stopPropagation(); toggleRepoCollapsed(repoKey, descendants); }}
        on:keydown={onActivateKey(() => toggleRepoCollapsed(repoKey, descendants))}>{shortName(repoName)}</span></span>
    {:else}
      <span class="repo-name" title={repoName}>{shortName(repoName)}</span>
    {/if}
    {#if childCount > 0 && collapsedRepos.has(repoKey)}
      {@const belowUnsaved = descendants.filter((d) => simpleUnsaved(($repoStates[d] || { status: 'unknown' }).status)).length}
      <span class="collapse-summary collapse-summary-row">
        {$t('collapse.below', { n: descendants.length })}{#if belowUnsaved > 0} · <span style="color:{sc('dirty')}">{$t('simple.notSavedN', { n: belowUnsaved })}</span>{/if}
      </span>
    {/if}
    {#if state.status === 'syncing' || state.status === 'cloning'}
      <div class="progress-track">
        <div class="progress-fill" style="width:{state.progress}%; background:{kindColor}"></div>
      </div>
      <span class="progress-pct" style="color:{kindColor}">{state.progress}%</span>
    {:else}
      <span class="status-text" style="color: {kindColor}" title={simpleTip(kind, state)}>{$t(simpleLabelKey(kind))}</span>
      {#if kind === 'update'}
        <button class="btn-action" on:click|stopPropagation={() => syncRepo(rowSourceKey, repoName)}>{$t('simple.btnUpdate')}</button>
      {:else if kind === 'online'}
        <button class="btn-action" on:click|stopPropagation={() => cloneRepo(rowSourceKey, repoName)}>{$t('simple.btnDownload')}</button>
      {/if}
      {#if kind !== 'online' && kind !== 'busy'}
        {@render quickLaunch(repoKey)}
      {/if}
    {/if}
    {#if kind !== 'online'}
      <div class="action-menu-container">
        <button class="btn-kebab" on:click|stopPropagation={() => toggleActionMenu(repoKey)} title={$t('repo.actions')}>&#8942;</button>
        {#if actionMenuRepo === repoKey}
          <div transition:fade={{ duration: 80 }}>
            <LauncherMenu
              kind="repo"
              editors={configEditors}
              profiles={[]}
              aiHarnesses={configAIHarnesses}
              {revealLabel}
              browserLabelOverride={$t('simple.openWeb')}
              onOpenBrowser={() => openRepoInBrowser(rowSourceKey, repoName)}
              onOpenFolder={() => openRepoInExplorer(repoKey)}
              onOpenApp={(cmd) => openRepoInApp(repoKey, cmd)}
              onOpenAIHarness={(h) => openRepoInAIHarness(repoKey, h)}
              onDelete={() => { actionMenuRepo = null; askDelete(rowSourceKey, repoName, state.status); }}
              deleteLabel={$t('simple.delete')}
            />
          </div>
        {/if}
      </div>
    {/if}
  {:else}
  <span class="dot" class:spinning={rowBusy(state.status, !!fetchingRepos[repoKey])} style="color: {sc(state.status)}">{rowGlyph(state.status, !!fetchingRepos[repoKey])}</span>
  {#if membershipsFor(repoKey).length > 0}
    <div class="ws-badge-wrap">
      <button class="ws-badge"
              title="Member of: {membershipsFor(repoKey).map(workspaceLabel).join(', ')}"
              on:click|stopPropagation={() => toggleWsPopover(repoKey)}>
        <svg class="ws-icon" viewBox="0 0 16 16" width="13" height="13" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round" stroke-linecap="round" aria-hidden="true">
          <path d="M2 5.5 L8 2.5 L14 5.5 L8 8.5 Z"/>
          <path d="M2 8.5 L8 11.5 L14 8.5"/>
          <path d="M2 11.5 L8 14.5 L14 11.5"/>
        </svg>
      </button>
      {#if openWsPopover === repoKey}
        <div class="ws-popover" role="presentation" transition:fade={{ duration: 80 }} on:click|stopPropagation on:keydown|stopPropagation>
          <div class="ws-popover-title">{$t('workspace.openWorkspace')}</div>
          {#each membershipsFor(repoKey) as wsKey}
            <button class="ws-popover-item"
                    on:click={() => openWorkspaceFromBadge(wsKey)}
                    disabled={workspaceBusy}>
              <svg class="ws-icon" viewBox="0 0 16 16" width="13" height="13" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round" stroke-linecap="round" aria-hidden="true">
                <path d="M2 5.5 L8 2.5 L14 5.5 L8 8.5 Z"/>
                <path d="M2 8.5 L8 11.5 L14 8.5"/>
                <path d="M2 11.5 L8 14.5 L14 11.5"/>
              </svg>
              {workspaceLabel(wsKey)}
            </button>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
  {#if childCount > 0}
    <!-- The name folds its clones too. -->
    <span class="repo-name"><span class="repo-name-toggle" role="button" tabindex="-1"
      title={collapsedRepos.has(repoKey) ? $t('collapse.expand') : $t('collapse.collapse')}
      on:click={(e) => { e.stopPropagation(); toggleRepoCollapsed(repoKey, descendants); }}
      on:keydown={onActivateKey(() => toggleRepoCollapsed(repoKey, descendants))}>{repoName}</span></span>
  {:else}
    <span class="repo-name">{repoName}</span>
  {/if}
  {#if childCount > 0 && collapsedRepos.has(repoKey)}
    {@const below = descendants.filter(key => $repoStates[key] && $repoStates[key].status !== 'unknown' && !isSynced($repoStates[key])).length}
    <span class="collapse-summary collapse-summary-row">
      {$t('collapse.below', { n: descendants.length })}{#if below > 0} · <span style="color:{sc('behind')}">{below} {below === 1 ? $t('account.needsAttention') : $t('account.needAttentionPlural')}</span>{/if}
    </span>
  {/if}
  {#if isContainer}
    <span class="container-badge" title="Multi-repo container — holds nested clones">&#128193; container</span>
  {:else if isTentativeContainer(rowSourceKey, repoName, tentativeContainers)}
    <button class="tentative-container-badge"
            on:click|stopPropagation={() => toggleContainer(rowSourceKey, repoName)}
            title="This repo holds nested clones — mark it as a container to onboard them">&#128193; onboard nested clones</button>
  {/if}
  {#if state.branch === '(detached)'}
    <span class="branch-badge detached">detached</span>
  {:else if state.branch && !state.isDefault}
    <span class="branch-badge">{state.branch}</span>
  {/if}

  {#if state.status === 'syncing' || state.status === 'cloning'}
    <div class="progress-track">
      <div class="progress-fill" style="width:{state.progress}%; background:{sc(state.status)}"></div>
    </div>
    <span class="progress-pct" style="color:{sc(state.status)}">{state.progress}%</span>
  {:else}
    {#if state.status === 'unknown'}<span class="status-pending">{$t('simple.checking')}</span>{/if}
    {#if state.status !== 'unknown'}
    {@const label = advancedLabel(state.status, state.isDefault)}
    {#if !deleteMode && $prSettings.enabled}
      {@const prSummary = lookupPRSummary($prsByAccount, rowAccountKey, repoName)}
      {@const acctForPRs = $accounts[rowAccountKey]}
      {#if prSummary.authored.length > 0}
        <div class="action-menu-container pr-badge-wrap">
          <button class="pr-badge pr-badge-authored" on:click|stopPropagation={() => togglePRPopover(`${repoKey}|authored`)} title="{prSummary.authored.length} open PR{prSummary.authored.length === 1 ? '' : 's'} I authored">
            &#128221; {prSummary.authored.length}
          </button>
          {#if openPRPopover === `${repoKey}|authored`}
            <div transition:fade={{ duration: 80 }}>
              <PRPopover
                kind="authored"
                prs={prSummary.authored}
                providerName={acctForPRs?.provider || ''}
                providerAllURL={providerPRsURL(acctForPRs?.provider || '', acctForPRs?.url || '', repoName)}
                onOpenPR={(url) => { bridge.openInBrowser(url); openPRPopover = null; }}
                onOpenAll={() => { const u = providerPRsURL(acctForPRs?.provider || '', acctForPRs?.url || '', repoName); if (u) bridge.openInBrowser(u); openPRPopover = null; }}
              />
            </div>
          {/if}
        </div>
      {/if}
      {#if prSummary.reviewRequested.length > 0}
        <div class="action-menu-container pr-badge-wrap">
          <button class="pr-badge pr-badge-review" on:click|stopPropagation={() => togglePRPopover(`${repoKey}|review`)} title="{prSummary.reviewRequested.length} PR{prSummary.reviewRequested.length === 1 ? '' : 's'} awaiting my review">
            &#128064; {prSummary.reviewRequested.length}
          </button>
          {#if openPRPopover === `${repoKey}|review`}
            <div transition:fade={{ duration: 80 }}>
              <PRPopover
                kind="review"
                prs={prSummary.reviewRequested}
                providerName={acctForPRs?.provider || ''}
                providerAllURL={providerPRsURL(acctForPRs?.provider || '', acctForPRs?.url || '', repoName)}
                onOpenPR={(url) => { bridge.openInBrowser(url); openPRPopover = null; }}
                onOpenAll={() => { const u = providerPRsURL(acctForPRs?.provider || '', acctForPRs?.url || '', repoName); if (u) bridge.openInBrowser(u); openPRPopover = null; }}
              />
            </div>
          {/if}
        </div>
      {/if}
    {/if}
    <span class="status-badges">
      {#if label}
        <span class="status-text" style="color:{sc(label.color)}">{$t(label.key)}</span>
      {:else}
        <span class="status-pending">{$t('repo.pending')}</span>
        {#each COUNT_BADGES as b}
          {#if state[b.field] > 0}<span class="sbadge" style="color:{sc(b.color)}" title="{state[b.field]} {b.word}">{b.glyph}{state[b.field]}</span>{/if}
        {/each}
      {/if}
    </span>
    {#if state.status === 'behind'}
      <button class="btn-action" on:click|stopPropagation={() => syncRepo(rowSourceKey, repoName)}>{$t('repo.pull')}</button>
    {:else if state.status === 'not cloned'}
      <button class="btn-action" on:click|stopPropagation={() => cloneRepo(rowSourceKey, repoName)}>{$t('repo.bringLocal')}</button>
    {/if}
    {/if}
    {#if !deleteMode && state.status !== 'not cloned'}
      {@render quickLaunch(repoKey)}
      <button class="btn-fetch" class:spinning={fetchingRepos[repoKey]} on:click|stopPropagation={() => fetchRepo(rowSourceKey, repoName)} title={$t('repo.fetchOrigin')} disabled={!!fetchingRepos[repoKey]}>&#8635;</button>
      <div class="action-menu-container">
        <button class="btn-kebab" on:click|stopPropagation={() => toggleActionMenu(repoKey)} title={$t('repo.actions')}>&#8942;</button>
        {#if actionMenuRepo === repoKey}
          <div transition:fade={{ duration: 80 }}>
            <LauncherMenu
              kind="repo"
              editors={configEditors}
              profiles={configProfiles}
              aiHarnesses={configAIHarnesses}
              {revealLabel}
              onOpenBrowser={() => openRepoInBrowser(rowSourceKey, repoName)}
              onOpenFolder={() => openRepoInExplorer(repoKey)}
              onOpenApp={(cmd) => openRepoInApp(repoKey, cmd)}
              onOpenProfile={(id) => openRepoProfile(repoKey, id)}
              onOpenAIHarness={(h) => openRepoInAIHarness(repoKey, h)}
              onSweep={() => sweepBranches(rowSourceKey, repoName)}
              onMove={() => { actionMenuRepo = null; openMoveRepo(rowSourceKey, repoName); }}
              moveEnabled={repoMoveDisabledReason(state) === ''}
              moveDisabledReason={repoMoveDisabledReason(state)}
              isContainer={isContainerRepo(rowSourceKey, repoName)}
              onToggleContainer={() => { actionMenuRepo = null; toggleContainer(rowSourceKey, repoName); }}
              onRescanContainer={() => { actionMenuRepo = null; rescanContainer(); }}
            />
          </div>
        {/if}
      </div>
    {/if}
  {/if}
  {/if}
</div>

<style>
  @keyframes spin { to { transform: rotate(360deg); } }
  .spinning { animation: spin 0.8s linear infinite; color: var(--spin-color) !important; stroke: var(--spin-color); stroke-width: 2.5; font-weight: 900; }
  .repo-row-nested { position: relative; }
  .repo-row-nested::before {
    content: ''; position: absolute; left: 18px; top: 0; bottom: 0;
    border-left: 2px solid var(--border); opacity: 0.6;
  }
  .container-badge {
    font-size: 10px; color: var(--text-secondary); border: 1px solid var(--border);
    border-radius: 4px; padding: 0 5px; margin-left: 6px; white-space: nowrap;
    flex: 0 0 auto;
  }
  /* Actionable hint on a repo that looks like a container but isn't flagged.
     Clicking it flags the container (which scans + opens the adopt modal). */
  .tentative-container-badge {
    font-size: 10px; color: var(--accent, #4B95E9);
    border: 1px solid var(--accent, #4B95E9); background: transparent;
    border-radius: 4px; padding: 0 5px; margin-left: 6px; white-space: nowrap;
    flex: 0 0 auto; cursor: pointer; line-height: 1.6;
  }
  .tentative-container-badge:hover {
    background: var(--accent, #4B95E9); color: #fff;
  }
  .collapse-chevron { flex: 0 0 auto; width: 18px; text-align: center; font-size: 16px; line-height: 1; opacity: 0.85; }
  .collapse-chevron-btn { background: none; border: none; padding: 0; color: inherit; cursor: pointer; line-height: 1; }
  .collapse-chevron-btn:hover { opacity: 1; }
  .collapse-summary { flex: 0 0 auto; font-size: 11px; font-weight: normal; opacity: 0.8; }
  .collapse-summary-row { margin-left: 6px; }
  .repo-name-toggle { cursor: pointer; }
  .repo-name-toggle:hover { text-decoration: underline; }
  .repo-row {
    display: flex; align-items: center; gap: 10px;
    padding: 8px 6px; border-radius: 6px; transition: background 0.1s;
  }
  .repo-row:hover { background: var(--bg-row-hover); }
  .dot { font-size: 14px; flex-shrink: 0; width: 18px; text-align: center; }
  .repo-name { font-size: 13px; color: var(--text-repo); flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .progress-track { width: 110px; height: 4px; background: var(--ring-bg); border-radius: 2px; overflow: hidden; flex-shrink: 0; }
  .progress-fill { height: 100%; border-radius: 2px; transition: width 0.12s ease-out; }
  .progress-pct { font-size: 11px; width: 32px; text-align: right; flex-shrink: 0; font-weight: 600; }
  .status-text { font-size: 12px; white-space: nowrap; font-weight: 600; }
  .btn-action {
    padding: 3px 10px; background: transparent; border: 1px solid var(--border);
    color: var(--text-secondary); border-radius: 5px; cursor: pointer;
    font-size: 11px; font-weight: 500; transition: all 0.15s; flex-shrink: 0;
  }
  .btn-action:hover { background: var(--bg-hover); color: var(--text-primary); border-color: var(--border-hover); }
  .btn-fetch {
    background: transparent; border: none; color: var(--text-dim); cursor: pointer;
    font-size: 14px; padding: 2px 4px; border-radius: 4px; transition: color 0.15s;
    flex-shrink: 0; line-height: 1;
  }
  .btn-fetch:hover { color: var(--text-primary); }
  .btn-fetch:disabled { opacity: 0.6; cursor: default; }
  /* Repo-row launchers (both modes): icon-only AI, editor and terminal buttons. */
  .launch-group { display: inline-flex; align-items: center; gap: 2px; flex-shrink: 0; }
  .btn-launch {
    display: inline-flex; align-items: center; justify-content: center;
    width: 24px; height: 24px; padding: 0; border: none; border-radius: 5px;
    background: transparent; color: var(--text-secondary); cursor: pointer;
    transition: background 0.12s, color 0.12s;
  }
  .btn-launch:hover { background: var(--bg-hover); color: var(--text-primary); }
  .btn-kebab {
    background: transparent; border: none; color: var(--text-dim); cursor: pointer;
    font-size: 16px; padding: 2px 4px; border-radius: 4px; transition: color 0.15s;
    flex-shrink: 0; line-height: 1;
  }
  .btn-kebab:hover { color: var(--text-primary); }
  .action-menu-container { position: relative; }
  .status-badges { display: flex; align-items: center; gap: 6px; }
  .sbadge { font-size: 11px; font-weight: 600; white-space: nowrap; }
  .status-pending { font-size: 12px; font-weight: 600; color: var(--text-dim); }
  .branch-badge { font-size: 10px; padding: 1px 5px; border-radius: 3px; background: var(--bg-hover); color: var(--text-dim); white-space: nowrap; }
  .branch-badge.detached { color: var(--status-error, #D81E5B); }
  /* PR indicators (issue #29) */
  .pr-badge-wrap { position: relative; display: inline-flex; }
  .pr-badge {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: 11px;
    font-weight: 600;
    padding: 2px 7px;
    border-radius: 10px;
    border: 1px solid transparent;
    background: var(--bg-hover);
    color: var(--text-secondary);
    cursor: pointer;
    white-space: nowrap;
    transition: background 0.1s, border-color 0.1s, color 0.1s;
  }
  .pr-badge:hover { background: var(--bg-row-hover); color: var(--text-primary); }
  .pr-badge-authored { color: var(--text-secondary); }
  .pr-badge-review {
    color: var(--status-error, #D81E5B);
    border-color: color-mix(in srgb, var(--status-error, #D81E5B) 40%, transparent);
    background: color-mix(in srgb, var(--status-error, #D81E5B) 12%, var(--bg-hover));
  }
  .pr-badge-review:hover {
    background: color-mix(in srgb, var(--status-error, #D81E5B) 22%, var(--bg-hover));
    color: var(--status-error, #D81E5B);
  }
  /* ── Repo detail panel ── */
  .repo-row-clickable { cursor: pointer; }
  .repo-row-clickable:hover { background: var(--bg-row-hover); }
  .btn-delete-x {
    background: none; border: 1px solid #D81E5B55; color: #D81E5B;
    cursor: pointer; font-size: 9px; font-weight: 600;
    width: 16px; height: 16px; padding: 0; flex-shrink: 0;
    border-radius: 3px; display: flex; align-items: center; justify-content: center;
    opacity: 0.7; transition: all 0.12s; line-height: 1;
  }
  .btn-delete-x:hover { opacity: 1; background: #D81E5B22; border-color: #D81E5B; }
  /* ── Workspaces (issue #27 / #49) ────────────────────────────────── */

  /* Small membership count chip on clone rows. Sits beside the branch
     badge and stays subtle — presence of the chip at all is already the
     signal the clone is in a workspace. */
  /* Negative margins claw back the .repo-row `gap: 10px` on each side so
     the badge sits visually flush against the status dot and the repo
     name, instead of looking like a 50px-wide gutter. */
  .ws-badge-wrap {
    position: relative;
    display: inline-flex;
    align-items: center;
    flex: 0 0 auto;
    margin-left: -6px;
    margin-right: -4px;
  }
  /* Now an action button: clicking it opens the workspace popover. The icon
     alone signals membership; the count is gone (the popover lists the
     workspaces by name). Compact footprint — one emoji glyph, tight padding,
     no border. */
  .ws-badge {
    line-height: 1;
    padding: 2px 3px;
    border: none;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    border-radius: 4px;
    transition: background 0.12s, color 0.12s;
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .ws-badge:hover { background: var(--bg-hover); color: var(--text-primary); }
  .ws-icon { display: block; }
  .ws-badge:focus-visible { outline: 1px solid var(--border-hover); outline-offset: 1px; }
  /* Anchor ABOVE the badge — bottom rows would otherwise spawn the popover
     beneath the visible viewport and need scrolling to reveal it. The
     popover is at most 3-5 entries tall so opening upward fits comfortably
     even for rows near the very top of the cards area. */
  .ws-popover {
    position: absolute;
    bottom: calc(100% + 4px);
    left: 0;
    z-index: 30;
    min-width: 200px;
    max-width: 320px;
    padding: 6px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 8px;
    box-shadow: 0 6px 16px rgba(0, 0, 0, 0.18);
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .ws-popover-title {
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.4px;
    color: var(--text-muted);
    padding: 4px 8px 2px;
  }
  .ws-popover-item {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 8px;
    border: none;
    background: transparent;
    color: var(--text-primary);
    font-size: 12px;
    text-align: left;
    border-radius: 6px;
    cursor: pointer;
  }
  .ws-popover-item:hover:not(:disabled) { background: var(--bg-hover); }
  .ws-popover-item:disabled { opacity: 0.5; cursor: not-allowed; }
</style>
