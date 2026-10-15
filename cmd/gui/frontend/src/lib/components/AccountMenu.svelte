<script lang="ts">
  // The account ⋯ button and its menu: open the account on the web or in
  // the file manager, open its folder with a terminal, editor or AI
  // harness, Find, New, Edit account, and Change credential (Advanced) or
  // Reconnect (Simple, broken credential). The All view's group headers
  // and the account card share it.
  import { fade } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { pushNotice, errorText } from '../notices';
  import { accounts } from '../stores';
  import { canDiscoverWith, canCreateWith, credBroken, DISCOVER_DISABLED, CREATE_DISABLED } from '../status';
  import { t } from '../i18n';
  import LauncherMenu from '../LauncherMenu.svelte';
  import UIIcon from '../UIIcon.svelte';
  import type { RowProps } from './RepoRow.svelte';
  import type { AIHarnessInfo } from '../types';

  export let accountKey: string;
  export let credStatus: string | undefined;
  export let actionMenuAccount: string | null;
  export let toggleAccountMenu: (accountKey: string) => void;
  export let closeAccountMenu: () => void;
  export let openDiscover: (accountKey: string) => void;
  export let openCreateRepo: (accountKey: string) => Promise<void>;
  export let openEditAccount: (accountKey: string) => void;
  export let openCredChange: (accountKey: string, currentType: string) => void;
  export let rowProps: RowProps;

  $: ({ simple, revealLabel, configEditors, configProfiles, configAIHarnesses } = rowProps);

  async function openAccountInExplorer() {
    try { await bridge.openAccountFolder(accountKey); } catch (e) { console.error(e); }
    closeAccountMenu();
  }

  async function openAccountInApp(command: string) {
    try { await bridge.openAccountInApp(accountKey, command); } catch (e) { console.error(e); }
    closeAccountMenu();
  }

  async function openAccountProfile(profileID: string) {
    try { await bridge.openAccountProfile(accountKey, profileID); }
    catch (e) { pushNotice('error', $t('notice.openTerminalFailed'), [errorText(e)]); }
    closeAccountMenu();
  }

  async function openAccountInAIHarness(harness: AIHarnessInfo) {
    try {
      await bridge.openAccountInAIHarness(accountKey, harness.command, harness.args || []);
    } catch (e) {
      pushNotice('error', $t('notice.openFailed', { name: harness.name }), [errorText(e)]);
    }
    closeAccountMenu();
  }

  async function openAccountInBrowser() {
    try { await bridge.openAccountInBrowser(accountKey); } catch (e) { console.error(e); }
    closeAccountMenu();
  }

  // The account menu's own actions after Find and New: Edit account, and
  // Change credential (Advanced) or Reconnect (Simple, broken credential).
  function accountItems(simple: boolean, credStatus: string | undefined) {
    const credType = $accounts[accountKey]?.default_credential_type || 'gcm';
    const cred = () => { closeAccountMenu(); openCredChange(accountKey, credType); };
    const items = [{ label: $t('menu.editAccount'), icon: '✎', onClick: () => { closeAccountMenu(); openEditAccount(accountKey); } }];
    if (!simple) items.push({ label: $t('menu.changeCredential'), icon: '⚿', onClick: cred });
    else if (credBroken(credStatus)) items.push({ label: $t('menu.reconnect'), icon: '⚿', onClick: cred });
    return items;
  }
</script>

<div class="action-menu-container account-menu">
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
        onOpenBrowser={openAccountInBrowser}
        onOpenFolder={openAccountInExplorer}
        onOpenApp={openAccountInApp}
        onOpenProfile={openAccountProfile}
        onOpenAIHarness={openAccountInAIHarness}
        onFind={() => { closeAccountMenu(); openDiscover(accountKey); }}
        findEnabled={canDiscoverWith(credStatus)}
        findDisabledReason={$t(DISCOVER_DISABLED)}
        onCreate={() => { closeAccountMenu(); openCreateRepo(accountKey); }}
        createEnabled={canCreateWith(credStatus)}
        createDisabledReason={$t(CREATE_DISABLED)}
        extraItems={accountItems(simple, credStatus)}
      />
    </div>
  {/if}
</div>

<style>
  .account-menu { position: relative; flex: none; display: inline-flex; align-items: center; }
</style>
