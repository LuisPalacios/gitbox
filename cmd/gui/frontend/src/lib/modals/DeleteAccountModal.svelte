<script lang="ts">
  // Three-step confirm (type the name, read the impact, confirm) that
  // removes an account with its sources and dependent mirrors.
  import { fade, slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { sources } from '../stores';
  import { dismiss } from '../dismiss';
  import { errorText } from '../notices';
  import Rich from '../Rich.svelte';
  import { t } from '../i18n';

  export let simple: boolean;
  export let deleteMode: boolean;
  export let reloadFromDisk: () => Promise<void>;

  let deleteAcctConfirm: string | null = null; // account key
  let deleteAcctStep = 0; // 0=inactive, 1=type name, 2=warning, 3=final confirm
  let deleteAcctBusy = false;
  let deleteAcctInput = '';
  let deleteAcctError = '';
  let folderFailures: string[] = [];
  let removeFolders = false;
  // Cascade impact from the backend — populated on modal open so the warning
  // step lists every mirror that will be removed along with the account.
  let deleteAcctImpact: {
    sources: string[];
    mirrors: string[];
    repoCount: number;
    cloneCount: number;
  } | null = null;

  export async function askDeleteAccount(accountKey: string) {
    deleteAcctConfirm = accountKey;
    deleteAcctStep = 1;
    deleteAcctInput = '';
    deleteAcctError = '';
    folderFailures = [];
    removeFolders = false;
    deleteAcctImpact = null;
    try {
      const dto = await bridge.accountDeletionImpact(accountKey);
      deleteAcctImpact = {
        sources: dto.sources || [],
        mirrors: dto.mirrors || [],
        repoCount: dto.repo_count || 0,
        cloneCount: dto.clone_count || 0,
      };
    } catch (err) {
      // Non-fatal — fall back to the generic source/repo-count text.
      console.error('accountDeletionImpact failed', err);
    }
  }

  function cancelDeleteAccount() {
    if (deleteAcctBusy) return;
    deleteAcctConfirm = null;
    deleteAcctStep = 0;
    deleteAcctInput = '';
    deleteAcctError = '';
    folderFailures = [];
    removeFolders = false;
    deleteAcctImpact = null;
  }

  function deleteAcctCheckName() {
    if (deleteAcctInput.trim() === deleteAcctConfirm) {
      deleteAcctStep = 2;
      deleteAcctError = '';
    } else {
      deleteAcctError = $t('deleteAccount.nameMismatch');
    }
  }

  async function confirmDeleteAccount() {
    if (!deleteAcctConfirm || deleteAcctBusy) return;
    if (deleteAcctStep === 2) { deleteAcctStep = 3; return; }
    deleteAcctBusy = true;
    deleteAcctError = '';
    folderFailures = [];
    try {
      folderFailures = await bridge.deleteAccount(deleteAcctConfirm, !simple && removeFolders);
      await reloadFromDisk();
      if (folderFailures.length === 0) {
        deleteAcctConfirm = null;
        deleteMode = false;
      }
    } catch (err) {
      deleteAcctError = errorText(err);
    } finally {
      deleteAcctBusy = false;
    }
  }
</script>

{#if deleteAcctConfirm}
  {@const acctSources = Object.entries($sources).filter(([_, s]) => s.account === deleteAcctConfirm)}
  {@const repoCount = acctSources.reduce((n, [_, s]) => n + Object.keys(s.repos).length, 0)}
  <div class="overlay" use:dismiss={cancelDeleteAccount} transition:fade={{ duration: 120 }}>
    <div class="modal modal-delete" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head modal-head-delete">
        <h3>{$t('account.delete')}</h3>
        <button class="btn-x" on:click={cancelDeleteAccount}>&#10005;</button>
      </div>
      <div class="modal-body">
        {#if deleteAcctStep === 1}
          <p class="delete-warning">{$t('deleteAccount.typeName')}</p>
          <div class="form-row" style="margin-top: 8px;">
            <input class="form-input" type="text" bind:value={deleteAcctInput} placeholder="{deleteAcctConfirm}" on:keydown={(e) => { if (e.key === 'Enter') deleteAcctCheckName(); }} />
          </div>
          {#if deleteAcctError}
            <p class="delete-danger" style="margin-top: 6px;">{deleteAcctError}</p>
          {/if}
        {:else if deleteAcctStep === 2}
          <p class="delete-repo-name">{deleteAcctConfirm}</p>
          {#if deleteAcctImpact}
            <p class="delete-warning delete-danger">
              {$t('deleteAccount.impact', { sources: $t(deleteAcctImpact.sources.length === 1 ? 'deleteAccount.sourceOne' : 'deleteAccount.sourceN', { n: deleteAcctImpact.sources.length }), repos: $t(deleteAcctImpact.repoCount === 1 ? 'deleteAccount.repoOne' : 'deleteAccount.repoN', { n: deleteAcctImpact.repoCount }), cloned: $t(deleteAcctImpact.cloneCount === 1 ? 'deleteAccount.clonedOne' : 'deleteAccount.clonedN', { n: deleteAcctImpact.cloneCount }) })}
            </p>
            {#if deleteAcctImpact.mirrors.length > 0}
              <p class="delete-warning delete-danger" style="margin-top: 8px;">
                {$t(deleteAcctImpact.mirrors.length !== 1 ? 'deleteAccount.mirrorsN' : 'deleteAccount.mirrorsOne')}
              </p>
              <ul style="margin: 4px 0 0 18px;">
                {#each deleteAcctImpact.mirrors as m}
                  <li><code>{m}</code></li>
                {/each}
              </ul>
            {/if}
          {:else}
            <p class="delete-warning delete-danger">
              {$t('deleteAccount.impactFallback', { sources: $t(acctSources.length === 1 ? 'deleteAccount.sourceOne' : 'deleteAccount.sourceN', { n: acctSources.length }), repos: $t(repoCount === 1 ? 'deleteAccount.repoOne' : 'deleteAccount.repoN', { n: repoCount }) })}
            </p>
          {/if}
          {#if !simple}
            <label class="delete-warning">
              <input type="checkbox" bind:checked={removeFolders} />
              {$t('deleteAccount.alsoFolders')}
            </label>
          {/if}
        {:else}
          <p class="delete-repo-name">{deleteAcctConfirm}</p>
          <p class="delete-final"><Rich key={removeFolders && !simple ? 'deleteAccount.finalFolders' : 'deleteAccount.final'} /></p>
          {#if deleteAcctError}
            <p role="alert" class="delete-danger">{deleteAcctError}</p>
          {/if}
          {#if folderFailures.length > 0}
            <div role="alert" class="delete-danger">
              <p>{$t('deleteAccount.folderFailures')}</p>
              <ul>
                {#each folderFailures as failure}<li>{failure}</li>{/each}
              </ul>
            </div>
          {/if}
        {/if}
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={cancelDeleteAccount}>{$t('common.cancel')}</button>
        {#if deleteAcctStep === 1}
          <button class="btn-delete" on:click={deleteAcctCheckName} disabled={!deleteAcctInput.trim()}>{$t('deleteAccount.confirmName')}</button>
        {:else if deleteAcctStep === 2}
          <button class="btn-delete" on:click={confirmDeleteAccount}>{$t('account.delete')}</button>
        {:else}
          <button class="btn-delete btn-delete-final" on:click={confirmDeleteAccount} disabled={deleteAcctBusy}>
            {deleteAcctBusy ? $t('deleteAccount.deleting') : $t('deleteAccount.yes')}
          </button>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>

</style>
