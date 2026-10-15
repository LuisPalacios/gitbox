<script lang="ts">
  // The All view: one collapsible group per source (header with its
  // account ⋯ menu and summary), then its clones as a tree (RepoTree),
  // problems first.
  import { fade } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { accounts, sources, repoStates, themeStore } from '../stores';
  import { statusColor, attentionRank, CALM_RANK, canDiscoverWith, canCreateWith, credBroken, DISCOVER_DISABLED, CREATE_DISABLED } from '../status';
  import { t } from '../i18n';
  import { onActivateKey } from '../keyboard';
  import LauncherMenu from '../LauncherMenu.svelte';
  import UIIcon from '../UIIcon.svelte';
  import RepoTree from './RepoTree.svelte';
  import type { RowProps } from './RepoRow.svelte';
  import type { AIHarnessInfo } from '../types';
  import type { RepoRow as TreeRow } from '../repo-tree';

  export let treeRows: Record<string, TreeRow[]>;
  export let collapsedSources: Set<string>;
  export let credStatuses: Record<string, any>;
  export let actionMenuAccount: string | null;
  export let toggleSourceCollapsed: (sourceKey: string) => void;
  export let toggleAccountMenu: (accountKey: string) => void;
  export let closeAccountMenu: () => void;
  export let openDiscover: (accountKey: string) => void;
  export let openCreateRepo: (accountKey: string) => Promise<void>;
  export let openEditAccount: (accountKey: string) => void;
  export let openCredChange: (accountKey: string, currentType: string) => void;
  export let rowProps: RowProps;

  $: sc = (s: string) => statusColor(s, $themeStore);
  $: ({ simple, revealLabel, configEditors, configProfiles, configAIHarnesses } = rowProps);
  // Repos that need me (as in Needs attention); unknown ones are still checking.
  $: needsMe = (key: string) => { const st = $repoStates[key]; return !!st && attentionRank(st.status, st.isDefault) < CALM_RANK; };

  async function openAccountInExplorer(accountKey: string) {
    try { await bridge.openAccountFolder(accountKey); } catch (e) { console.error(e); }
    closeAccountMenu();
  }

  async function openAccountInApp(accountKey: string, command: string) {
    try { await bridge.openAccountInApp(accountKey, command); } catch (e) { console.error(e); }
    closeAccountMenu();
  }

  async function openAccountProfile(accountKey: string, profileID: string) {
    try { await bridge.openAccountProfile(accountKey, profileID); }
    catch (e: any) { await bridge.showErrorDialog('Open profile', (e?.message || String(e))); }
    closeAccountMenu();
  }

  async function openAccountInAIHarness(accountKey: string, harness: AIHarnessInfo) {
    try {
      await bridge.openAccountInAIHarness(accountKey, harness.command, harness.args || []);
    } catch (e: any) {
      await bridge.showErrorDialog('Open in ' + harness.name, (e?.message || String(e)));
    }
    closeAccountMenu();
  }

  async function openAccountInBrowser(accountKey: string) {
    try { await bridge.openAccountInBrowser(accountKey); } catch (e) { console.error(e); }
    closeAccountMenu();
  }

  // The account menu's own actions after Find and New: Edit account, and
  // Change credential (Advanced) or Reconnect (Simple, broken credential).
  function accountItems(accountKey: string, simple: boolean, credStatus: string | undefined) {
    const credType = $accounts[accountKey]?.default_credential_type || 'gcm';
    const cred = () => { closeAccountMenu(); openCredChange(accountKey, credType); };
    const items = [{ label: $t('menu.editAccount'), icon: '✎', onClick: () => { closeAccountMenu(); openEditAccount(accountKey); } }];
    if (!simple) items.push({ label: $t('menu.changeCredential'), icon: '⚿', onClick: cred });
    else if (credBroken(credStatus)) items.push({ label: $t('menu.reconnect'), icon: '⚿', onClick: cred });
    return items;
  }
</script>

<section class="repo-list">
  {#each Object.entries($sources) as [sourceKey, source] (sourceKey)}
    {@const accountKey = source.account}
    {@const sourceCollapsed = collapsedSources.has(sourceKey)}
    {@const sourceRepoKeys = [...new Set((treeRows[sourceKey] || []).flatMap(row => [row.key, ...row.descendants]))]}
    {@const sourceIssues = sourceRepoKeys.filter(needsMe).length}
    {#if sourceRepoKeys.length > 0}
    <div class="source-group">
      <div class="source-header" role="button" tabindex="0"
        aria-expanded={!sourceCollapsed}
        title={sourceCollapsed ? $t('collapse.expand') : $t('collapse.collapse')}
        on:click={(e) => { if (!(e.target instanceof Element && e.target.closest('.source-header-kebab'))) toggleSourceCollapsed(sourceKey); }}
        on:keydown={onActivateKey(() => toggleSourceCollapsed(sourceKey))}>
        <span class="collapse-chevron" class:collapsed={sourceCollapsed} aria-hidden="true"><UIIcon name="chevron" /></span>
        <span class="source-header-title">{sourceKey}</span>
        <div class="action-menu-container source-header-kebab">
          <button class="icon-btn btn-kebab" on:click|stopPropagation={() => toggleAccountMenu(accountKey)} title={$t('account.actions')} aria-label={$t('account.actions')} aria-haspopup="menu"><UIIcon name="dots" /></button>
          {#if actionMenuAccount === accountKey}
            <div transition:fade={{ duration: 80 }}>
              <LauncherMenu
                kind="account"
                editors={configEditors}
                profiles={simple ? [] : configProfiles}
                browserLabelOverride={simple ? $t('simple.openWeb') : ''}
                aiHarnesses={configAIHarnesses}
                {revealLabel}
                onOpenBrowser={() => openAccountInBrowser(accountKey)}
                onOpenFolder={() => openAccountInExplorer(accountKey)}
                onOpenApp={(cmd) => openAccountInApp(accountKey, cmd)}
                onOpenProfile={(id) => openAccountProfile(accountKey, id)}
                onOpenAIHarness={(h) => openAccountInAIHarness(accountKey, h)}
                onFind={() => { closeAccountMenu(); openDiscover(accountKey); }}
                findEnabled={canDiscoverWith(credStatuses[accountKey]?.status)}
                findDisabledReason={DISCOVER_DISABLED}
                onCreate={() => { closeAccountMenu(); openCreateRepo(accountKey); }}
                createEnabled={canCreateWith(credStatuses[accountKey]?.status)}
                createDisabledReason={CREATE_DISABLED}
                extraItems={accountItems(accountKey, simple, credStatuses[accountKey]?.status)}
              />
            </div>
          {/if}
        </div>
        <span class="collapse-summary">
          {simple ? $t('simple.projects', { n: sourceRepoKeys.length }) : $t('collapse.clones', { n: sourceRepoKeys.length })} ·
          {#if sourceRepoKeys.some(key => !$repoStates[key] || $repoStates[key].status === 'unknown')}
            {$t('simple.checking')}
          {:else if sourceIssues === 0}
            <span style="color:{sc('clean')}">{simple ? $t('simple.allSaved') : $t('account.allUpToDate')}</span>
          {:else}
            <span style="color:{sc('behind')}">{$t(sourceIssues === 1 ? 'account.needYouOne' : 'account.needYou', { n: sourceIssues })}</span>
          {/if}
        </span>
      </div>
      {#if !sourceCollapsed}
      <RepoTree rows={treeRows[sourceKey] || []} groupSource={sourceKey} {rowProps} />
      {/if}
    </div>
    {/if}
  {/each}

</section>

<style>
  .repo-list { flex: 1; padding: 0 24px 12px; overflow-y: auto; min-height: 0; }
  .source-group { margin-bottom: 6px; }
  .source-header {
    font-size: 11px; font-weight: 600; color: var(--text-dim);
    text-transform: uppercase; letter-spacing: 0.8px;
    padding: 6px 0 2px; border-bottom: 1px solid var(--border);
    display: flex; align-items: center; gap: 4px;
  }
  .source-header[role="button"] { cursor: pointer; user-select: none; }
  .source-header-title { flex: 0 0 auto; }
  .collapse-chevron {
    flex: none; width: 22px; height: 22px; display: inline-flex; align-items: center; justify-content: center;
    color: var(--text-muted); transition: transform 0.12s, color 0.12s;
  }
  .collapse-chevron.collapsed { transform: rotate(-90deg); }
  .source-header[role="button"]:hover .collapse-chevron { color: var(--text-primary); }
  .collapse-summary { flex: 0 0 auto; font-size: 11px; font-weight: normal; opacity: 0.8; }
  .source-header-kebab { flex: 0 0 auto; margin-right: auto; display: inline-flex; align-items: center; }
  .action-menu-container { position: relative; }
</style>
