<script lang="ts">
  // The accounts cards panel: one chip/card per account, with its
  // credential badge and Find / New buttons, plus the add-account button.
  import { slide } from 'svelte/transition';
  import { accounts, themeStore } from '../stores';
  import { credColor, providerLabel } from '../theme';
  import { credBroken, canDiscoverWith, canCreateWith, credBadgeClass, credBadgeLabel, DISCOVER_DISABLED, CREATE_DISABLED } from '../status';
  import { t } from '../i18n';
  import Card from '../Card.svelte';

  export let cardsVisible: boolean;
  export let expandedCard: string | null;
  export let deleteMode: boolean;
  export let simple: boolean;
  export let credStatuses: Record<string, any>;
  export let resolvedTheme: 'light' | 'dark';
  export let askDeleteAccount: (accountKey: string) => Promise<void>;
  export let openEditAccount: (key: string) => void;
  export let openCredChange: (accountKey: string, currentType: string) => void;
  export let openDiscover: (accountKey: string) => void;
  export let openCreateRepo: (accountKey: string) => Promise<void>;
  export let onAddAccount: () => void;

  $: cc = (s: string) => credColor(s, $themeStore);
</script>

{#if cardsVisible}
<section class="cards-row" transition:slide={{ duration: 150 }}>
  {#each Object.entries($accounts) as [key, acct]}
    {@const credObj = credStatuses[key] || {status: 'unknown', primary: 'unknown', pat: 'unknown'}}
    {@const credOverall = credObj.status}
    {@const canDiscover = canDiscoverWith(credOverall)}
    {@const canCreate = canCreateWith(credOverall)}
    {@const brokenStyle = credBroken(credOverall) ? `background: ${resolvedTheme === 'light' ? '#fef2f2' : '#2a1215'}` : ''}
    <Card open={expandedCard === `acct:${key}`} title={key} name={key} dotColor={cc(credOverall)} style={brokenStyle}
      {deleteMode} deleteTitle="{$t('account.delete')} {key}" onDelete={() => askDeleteAccount(key)}
      onOpen={() => expandedCard = `acct:${key}`}
      nameClass="card-name-edit" nameTitle={$t('account.edit')} onName={() => openEditAccount(key)}
      provider={providerLabel(acct.provider)}
      actions={[
        { label: $t('account.find'), onClick: () => openDiscover(key), disabled: !canDiscover, title: canDiscover ? $t('account.findProjects') : DISCOVER_DISABLED },
        { label: $t('account.new'), onClick: () => openCreateRepo(key), disabled: !canCreate, title: canCreate ? $t('account.createRepo') : CREATE_DISABLED },
      ]}>
      {#snippet badge()}
        {#if simple}
          {#if credBroken(credOverall)}
            <button class="cred-badge cred-badge-err" on:click={() => openCredChange(key, acct.default_credential_type || 'gcm')}>{$t('simple.reconnect')}</button>
          {/if}
        {:else}
        <button class="cred-badge cred-badge-{credBadgeClass(credOverall)}"
          on:click={() => openCredChange(key, acct.default_credential_type || 'gcm')}
          title="Credential: {acct.default_credential_type || 'none'} — {credOverall}">{credBadgeLabel(credOverall, acct.default_credential_type)}</button>
        {/if}
      {/snippet}
    </Card>
  {/each}
  <button class="card card-add" on:click={onAddAccount} title={$t('account.add')}>
    <span class="card-add-icon">+</span>
  </button>
</section>
{/if}

<style>
  /* Chips and the open card are as wide as their content, so names are
     never cut; the row wraps instead of scrolling. */
  .cards-row { display: flex; flex-wrap: wrap; align-items: flex-start; gap: 8px; padding: 10px 24px; flex-shrink: 0; }
  .card {
    flex: 0 0 auto; background: var(--bg-card); border: 1px solid var(--border);
    border-radius: 10px; padding: 8px 12px; transition: border-color 0.15s, box-shadow 0.15s;
    box-shadow: var(--card-shadow);
  }
  .card:hover { border-color: var(--border-hover); }
  .cred-badge {
    font-size: 8px; font-weight: 700; letter-spacing: 0.3px; text-transform: uppercase;
    padding: 1px 4px; border-radius: 3px; cursor: pointer;
    background: var(--bg-hover); border: 1px solid var(--border); color: var(--text-muted);
    transition: all 0.12s; line-height: 1.3;
  }
  .cred-badge:hover { color: var(--text-primary); border-color: var(--border-hover); background: var(--bg-card); }
  .cred-badge-ok { background: #14532d; border-color: #166534; color: #86efac; }
  .cred-badge-err { background: #450a0a; border-color: #be123c; color: #fca5a5; }
  .cred-badge-warn { background: #431407; border-color: #c2410c; color: #fdba74; }
  .cred-badge-none { background: #1e3a5f; border-color: #2563eb; color: #93c5fd; }
  .cred-badge-offline { background: #450a0a; border-color: #D81E5B; color: #fca5a5; }
  .cred-badge-pending { background: #27272a; border-color: #52525b; color: #a1a1aa; animation: pulse-badge 1.5s ease-in-out infinite; }
  @keyframes pulse-badge { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }
  :global([data-theme="light"]) .cred-badge-offline { background: #fee2e2; border-color: #be123c; color: #be123c; }
  :global([data-theme="light"]) .cred-badge-pending { background: #f4f4f5; border-color: #a1a1aa; color: #71717a; }
  :global([data-theme="light"]) .cred-badge-ok { background: #dcfce7; border-color: #166534; color: #166534; }
  :global([data-theme="light"]) .cred-badge-err { background: #fee2e2; border-color: #be123c; color: #be123c; }
  :global([data-theme="light"]) .cred-badge-warn { background: #ffedd5; border-color: #c2410c; color: #c2410c; }
  :global([data-theme="light"]) .cred-badge-none { background: #dbeafe; border-color: #2563eb; color: #2563eb; }
  /* ── Add account card ── */
  .card-add {
    display: flex; align-items: center; justify-content: center;
    padding: 3px 12px; border-radius: 999px; box-shadow: none;
    cursor: pointer; border-style: dashed; background: transparent;
    color: var(--text-muted); transition: all 0.15s;
  }
  .card-add:hover { border-color: var(--border-hover); color: var(--text-primary); background: var(--bg-hover); }
  .card-add-icon { font-size: 16px; font-weight: 400; line-height: 1; }
</style>
