<script lang="ts">
  import { onDestroy } from 'svelte';
  import { bridge } from '../bridge';
  import { t, languageStore } from '../i18n';
  import { ro } from '../ro';
  import { remoteWindow } from '../stores';
  import type { CredentialStatus, IdentityProof } from '../types';

  export let simple: boolean;
  export let source: string;
  export let repo: string;
  export let credentialStatus: CredentialStatus | undefined;
  export let onFixCredential: () => void;

  let proof: IdentityProof | null = null;
  let loading = false;
  let busy = false;
  let actionError = '';
  let request = 0;
  onDestroy(() => { request++; });

  $: void load(source, repo, credentialStatus);

  async function load(source: string, repo: string, _credentialStatus: CredentialStatus | undefined) {
    const id = ++request;
    loading = true;
    try {
      const result = await bridge.getIdentityProof(source, repo);
      if (id === request) proof = result;
    } catch {
      if (id === request) proof = null;
    } finally {
      if (id === request) loading = false;
    }
  }

  async function runAction(action: () => Promise<void>, errorKey: string, refresh: boolean) {
    busy = true;
    actionError = '';
    try {
      await action();
    } catch {
      actionError = errorKey;
    } finally {
      if (refresh) await load(source, repo, credentialStatus);
      busy = false;
    }
  }
</script>

{#if loading && !proof}
  <p class="identity-note" role="status">{$t('proof.loading')}</p>
{:else if !proof}
  <p class="identity-note" role="status">{$t('proof.unavailable')}</p>
{:else if simple}
  <p class="identity-note" role="status">
    {#if proof.reason === 'unavailable'}
      {$t('proof.unavailable')}
    {:else if proof.verdict === 'unsafe'}
      {$t('proof.simpleMismatch')}
    {:else if proof.verdict === 'safe' && proof.username && proof.server}
      {$t('proof.signedIn', { username: proof.username, server: proof.server })}
    {:else if proof.username && proof.server}
      {$t('proof.usesAccount', { username: proof.username, server: proof.server })}
    {:else}
      {$t('proof.incomplete')}
    {/if}
  </p>
{:else}
  <div class="identity-proof" aria-busy={loading}>
    <p class="verdict" data-verdict={proof.verdict} role="status">
      <strong>{$t(`proof.verdict.${proof.verdict}`)}</strong>
      {#if proof.reason}{$t(`proof.reason.${proof.reason}`)}{/if}
    </p>
    <dl>
      <dt>{$t('proof.account')}</dt><dd translate="no">{proof.account}</dd>
      <dt>{$t('proof.server')}</dt><dd translate="no">{proof.server || '—'}</dd>
      <dt>{$t('proof.username')}</dt><dd translate="no">{proof.username || '—'}</dd>
      <dt>{$t('proof.nameSource')}</dt><dd>{$t(`proof.source.${proof.nameSource}`)}</dd>
      <dt>{$t('proof.emailSource')}</dt><dd>{$t(`proof.source.${proof.emailSource}`)}</dd>
      <dt>{$t('proof.origin')}</dt><dd translate="no">{proof.originURL || '—'}</dd>
      <dt>{$t('proof.helpers')}</dt>
      <dd>{proof.helperChain.map((helper) => $t(`proof.helper.${helper}`)).join(' → ') || '—'}</dd>
      {#if proof.credentialFile}
        <dt>{$t('proof.file')}</dt><dd translate="no">{proof.credentialFile}</dd>
      {/if}
      <dt>{$t('proof.lastVerified')}</dt>
      <dd>{proof.lastVerified ? new Date(proof.lastVerified).toLocaleString($languageStore) : $t('proof.never')}</dd>
    </dl>
    {#if proof.overrides.length}
      <ul class="overrides" aria-label={$t('proof.overrides')}>
        {#each proof.overrides as override}
          <li>{override.scope ? $t(`proof.scope.${override.scope}`) + ' · ' : ''}<code>{override.key}</code> · {$t(`proof.helper.${override.value}`)}</li>
        {/each}
      </ul>
    {/if}
    <div class="actions">
      <button type="button" class="text-btn" disabled={busy || loading} on:click={() => load(source, repo, credentialStatus)}>{$t('proof.refresh')}</button>
      {#if proof.verdict === 'unsafe'}
        <button type="button" class="text-btn" use:ro={'repairRepoIdentity'} disabled={busy || loading}
          on:click={() => runAction(() => bridge.repairRepoIdentity(source, repo), 'proof.repairError', true)}>{$t('proof.repair')}</button>
      {/if}
      <button type="button" class="text-btn" disabled={busy || loading} on:click={onFixCredential}>{$t('proof.fix')}</button>
      {#if !$remoteWindow}
        <button type="button" class="text-btn" use:ro={'openRepoConfigInEditor'} disabled={busy}
          on:click={() => runAction(() => bridge.openRepoConfigInEditor(source, repo), 'proof.editorError', false)}>{$t('proof.edit')}</button>
      {/if}
    </div>
    {#if actionError}<p class="identity-note" role="alert">{$t(actionError)}</p>{/if}
  </div>
{/if}

<style>
  .identity-note { margin: 3px 0 6px; color: var(--text-secondary); }
  .identity-proof { display: grid; gap: 7px; }
  .verdict { margin: 0; color: var(--text-secondary); }
  .verdict strong { color: var(--text-primary); margin-right: 4px; }
  .verdict[data-verdict="unsafe"] strong { color: var(--alert); }
  dl { display: grid; gap: 3px 10px; grid-template-columns: max-content minmax(0, 1fr); margin: 0; }
  dt { color: var(--text-secondary); }
  dd { margin: 0; overflow-wrap: anywhere; }
  .overrides { margin: 0; padding-left: 16px; overflow-wrap: anywhere; }
  .actions { display: flex; flex-wrap: wrap; gap: 8px; }
</style>
