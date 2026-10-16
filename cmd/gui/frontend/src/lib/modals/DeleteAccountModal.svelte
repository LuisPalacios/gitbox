<script lang="ts">
  // Three-step confirm (type the name, read the impact, confirm) that
  // deletes an account with its sources, clones and dependent mirrors.
  import { fade, slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { sources } from '../stores';
  import { dismiss } from '../dismiss';
  import Rich from '../Rich.svelte';
  import { t } from '../i18n';

  export let reloadError: string;
  export let deleteMode: boolean;
  export let reloadFromDisk: () => Promise<void>;

  let deleteAcctConfirm: string | null = null; // account key
  let deleteAcctStep = 0; // 0=inactive, 1=type name, 2=warning, 3=final confirm
  let deleteAcctBusy = false;
  let deleteAcctInput = '';
  let deleteAcctError = '';
  // Cascade impact from the backend — populated on modal open so the warning
  // step lists every mirror and workspace-member that will be pruned along
  // with the account. Workspace entries themselves survive even if emptied.
  let deleteAcctImpact: {
    sources: string[];
    mirrors: string[];
    workspaces: string[];
    workspaceMembers: number;
    repoCount: number;
    cloneCount: number;
  } | null = null;

  export async function askDeleteAccount(accountKey: string) {
    deleteAcctConfirm = accountKey;
    deleteAcctStep = 1;
    deleteAcctInput = '';
    deleteAcctError = '';
    deleteAcctImpact = null;
    try {
      const dto = await bridge.accountDeletionImpact(accountKey);
      deleteAcctImpact = {
        sources: dto.sources || [],
        mirrors: dto.mirrors || [],
        workspaces: dto.workspaces || [],
        workspaceMembers: dto.workspace_members || 0,
        repoCount: dto.repo_count || 0,
        cloneCount: dto.clone_count || 0,
      };
    } catch (err) {
      // Non-fatal — fall back to the generic source/repo-count text.
      console.error('accountDeletionImpact failed', err);
    }
  }

  function cancelDeleteAccount() {
    deleteAcctConfirm = null;
    deleteAcctStep = 0;
    deleteAcctInput = '';
    deleteAcctError = '';
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
    if (!deleteAcctConfirm) return;
    if (deleteAcctStep === 2) { deleteAcctStep = 3; return; }
    deleteAcctBusy = true;
    try {
      await bridge.deleteAccount(deleteAcctConfirm);
      await reloadFromDisk();
    } catch (err: any) {
      reloadError = err?.message || String(err);
    } finally {
      deleteAcctBusy = false;
      deleteAcctConfirm = null;
      deleteAcctStep = 0;
      deleteAcctInput = '';
      deleteAcctImpact = null;
      deleteMode = false;
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
            {#if deleteAcctImpact.workspaces.length > 0}
              <p class="delete-warning" style="margin-top: 8px;">
                {$t(`deleteAccount.members${deleteAcctImpact.workspaceMembers !== 1 ? 'N' : 'One'}Workspaces${deleteAcctImpact.workspaces.length !== 1 ? 'N' : 'One'}`, { n: deleteAcctImpact.workspaceMembers })}
              </p>
              <ul style="margin: 4px 0 0 18px;">
                {#each deleteAcctImpact.workspaces as w}
                  <li><code>{w}</code></li>
                {/each}
              </ul>
            {/if}
          {:else}
            <p class="delete-warning delete-danger">
              {$t('deleteAccount.impactFallback', { sources: $t(acctSources.length === 1 ? 'deleteAccount.sourceOne' : 'deleteAccount.sourceN', { n: acctSources.length }), repos: $t(repoCount === 1 ? 'deleteAccount.repoOne' : 'deleteAccount.repoN', { n: repoCount }) })}
            </p>
          {/if}
        {:else}
          <p class="delete-repo-name">{deleteAcctConfirm}</p>
          <p class="delete-final"><Rich key="deleteAccount.final" /></p>
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
