<script lang="ts">
  // The account card at the top of an account view, three lines:
  //   1. health mark (✕ in delete mode) and name (opens Edit account) |
  //      provider and the account ⋯ menu
  //   2. health in words | the fix action, when one is needed
  //   3. repo counts | Find and + New
  import { accounts, themeStore } from '../stores';
  import { statusColor, accountHealth, canDiscoverWith, canCreateWith, DISCOVER_DISABLED, CREATE_DISABLED } from '../status';
  import type { HealthFix } from '../status';
  import { providerLabel } from '../theme';
  import { t } from '../i18n';
  import UIIcon from '../UIIcon.svelte';
  import AccountMenu from './AccountMenu.svelte';
  import type { RowProps } from './RepoRow.svelte';

  export let accountKey: string;
  export let credStatuses: Record<string, any>;
  // Repos in this account, how many need me, and whether any is still checking.
  export let total: number;
  export let needYou: number;
  export let checking: boolean;
  export let actionMenuAccount: string | null;
  export let rowProps: RowProps;
  export let toggleAccountMenu: (accountKey: string) => void;
  export let closeAccountMenu: () => void;
  export let askDeleteAccount: (accountKey: string) => Promise<void>;
  export let openEditAccount: (accountKey: string) => void;
  export let openCredChange: (accountKey: string, currentType: string) => void;
  export let openDiscover: (accountKey: string) => void;
  export let openCreateRepo: (accountKey: string) => Promise<void>;
  export let fixCredential: (accountKey: string, fix: HealthFix) => void;

  $: ({ simple, deleteMode } = rowProps);
  $: acct = $accounts[accountKey];
  $: cred = credStatuses[accountKey] || { status: 'unknown' };
  $: health = accountHealth(cred.status, simple);
  $: sc = (s: string) => statusColor(s, $themeStore);
  $: credTip = [`${acct?.default_credential_type || 'gcm'} — ${cred.status}`, cred.primaryMsg, cred.patMsg].filter(Boolean).join('\n');
  $: canDiscover = canDiscoverWith(cred.status);
  $: canCreate = canCreateWith(cred.status);
</script>

{#if acct}
<div class="acct-card" class:bad={health.level === 'bad' || health.level === 'none'} class:warn={health.level === 'warn'}>
  <div class="line">
    {#if deleteMode}
      <button class="icon-btn icon-btn-danger" on:click={() => askDeleteAccount(accountKey)}
        title="{$t('account.delete')} {accountKey}" aria-label="{$t('account.delete')} {accountKey}"><UIIcon name="x" /></button>
    {:else}
      <span class="mark" style="color: {sc(health.color)}" aria-hidden="true">{health.mark}</span>
    {/if}
    <button class="name" title={$t('account.edit')} on:click={() => openEditAccount(accountKey)}>{accountKey}</button>
    <span class="right">
      <span class="provider">{providerLabel(acct.provider)}</span>
      <AccountMenu {accountKey} credStatus={cred.status} {actionMenuAccount} {rowProps}
        {toggleAccountMenu} {closeAccountMenu} {openDiscover} {openCreateRepo} {openEditAccount} {openCredChange} />
    </span>
  </div>
  <div class="line">
    <span class="words" style={health.level === 'ok' || health.level === 'pending' ? '' : `color: ${sc(health.color)}`}
      title={simple ? undefined : credTip}>{$t(health.key)}</span>
    {#if health.fix}
      <span class="right"><button class="text-btn" on:click={() => fixCredential(accountKey, health.fix)}>{$t(health.fixKey)}</button></span>
    {/if}
  </div>
  <div class="line">
    <span class="counts">
      {simple ? $t('simple.projects', { n: total }) : $t('collapse.clones', { n: total })} ·
      {#if checking}
        {$t('simple.checking')}
      {:else if needYou === 0}
        <span style="color: {sc('clean')}">{simple ? $t('simple.allSaved') : $t('account.allUpToDate')}</span>
      {:else}
        <span style="color: {sc('behind')}">{$t(needYou === 1 ? 'account.needYouOne' : 'account.needYou', { n: needYou })}</span>
      {/if}
    </span>
    <span class="right">
      <button class="text-btn" on:click={() => openDiscover(accountKey)} disabled={!canDiscover}
        title={canDiscover ? $t('account.findProjects') : $t(DISCOVER_DISABLED)}>{$t('account.find')}</button>
      <button class="text-btn" on:click={() => openCreateRepo(accountKey)} disabled={!canCreate}
        title={canCreate ? $t('account.createRepo') : $t(CREATE_DISABLED)}>+ {$t('account.new')}</button>
    </span>
  </div>
</div>
{/if}

<style>
  .acct-card {
    margin: 14px 0 10px; padding: 8px 12px; border: 1px solid var(--border); border-radius: 10px;
    background: var(--bg-card); box-shadow: var(--card-shadow);
    display: flex; flex-direction: column; gap: 3px;
  }
  .acct-card.bad { background: color-mix(in srgb, var(--alert) 9%, var(--bg-card)); }
  .acct-card.warn { background: color-mix(in srgb, #F07623 8%, var(--bg-card)); }
  .line { display: flex; align-items: center; gap: 8px; min-height: 26px; min-width: 0; }
  .right { margin-left: auto; display: flex; align-items: center; gap: 6px; flex: none; }
  .mark { width: var(--icon-btn); flex: none; text-align: center; font-size: 14px; }
  .name {
    min-width: 0; padding: 0; border: none; background: none; cursor: pointer; text-align: left;
    font-size: 15px; font-weight: 700; color: var(--text-primary);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .name:hover { text-decoration: underline; text-underline-offset: 3px; }
  .provider { font-size: 11px; font-weight: 600; letter-spacing: 0.3px; color: var(--text-muted); }
  .words, .counts { padding-left: calc(var(--icon-btn) + 8px); font-size: 12px; color: var(--text-secondary); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .counts { color: var(--text-muted); }
</style>
