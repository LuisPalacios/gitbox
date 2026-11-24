<script lang="ts">
  // Stop tracking by default. Advanced can confirm permanent folder deletion.
  import { fade, slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { dismiss } from '../dismiss';
  import { errorText } from '../notices';
  import { t } from '../i18n';
  import Rich from '../Rich.svelte';
  import type { RepoDetailView } from '../types';

  export let expandedRepo: string | null;
  export let repoDetail: RepoDetailView | null;
  export let deleteMode: boolean;
  export let simple: boolean;
  export let reloadFromDisk: () => Promise<void>;
  export let setFolderDeleteFailure: (key: string, message: string) => void;

  let deleteConfirm: { sourceKey: string; repoKey: string; status: string } | null = null;
  let deleteConfirmStep = 0; // 0=closed, 1=stop tracking, 2=folder warning, 3=final confirmation
  let deleting = false;
  let failure = '';

  export function askDelete(sourceKey: string, repoKey: string, status: string, removeFolder = false) {
    deleteConfirm = { sourceKey, repoKey, status };
    deleteConfirmStep = removeFolder && !simple ? 2 : 1;
    failure = '';
  }

  function cancelDelete() {
    if (deleting) return;
    deleteConfirm = null;
    deleteConfirmStep = 0;
    failure = '';
  }

  async function confirmDelete() {
    if (!deleteConfirm || deleting) return;
    const { sourceKey, repoKey } = deleteConfirm;
    const key = `${sourceKey}/${repoKey}`;
    const removeFolder = deleteConfirmStep === 3;
    if (removeFolder && simple) return;
    deleting = true;
    failure = '';
    let changed = false;
    try {
      if (removeFolder) await bridge.deleteRepoFolder(sourceKey, repoKey);
      else await bridge.stopTrackingRepo(sourceKey, repoKey);
      changed = true;
      setFolderDeleteFailure(key, '');
      if (expandedRepo === key) {
        expandedRepo = null;
        repoDetail = null;
      }
      await reloadFromDisk();
      deleteConfirm = null;
      deleteConfirmStep = 0;
      deleteMode = false;
    } catch (err) {
      failure = errorText(err);
      if (removeFolder && !changed) setFolderDeleteFailure(key, failure);
    } finally {
      deleting = false;
    }
  }

  function isDangerous(status: string): boolean {
    return ['dirty', 'ahead', 'diverged', 'conflict', 'upstream gone'].includes(status);
  }

  // deleteWarningKey returns the catalog key of the warning for status.
  function deleteWarningKey(status: string): string {
    if (status === 'upstream gone') return 'deleteClone.warnUpstreamGone';
    if (isDangerous(status)) return 'deleteClone.warnDangerous';
    return 'deleteClone.warnDefault';
  }
</script>

{#if deleteConfirm}
  <div class="overlay" use:dismiss={cancelDelete} transition:fade={{ duration: 120 }}>
    <div class="modal modal-delete" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head modal-head-delete">
        <h3>{$t(deleteConfirmStep === 1 ? 'deleteClone.stop' : 'deleteClone.title')}</h3>
        <button class="btn-x" on:click={cancelDelete}>&#10005;</button>
      </div>
      <div class="modal-body">
        <p class="delete-repo-name">{deleteConfirm.repoKey}</p>
        {#if deleteConfirmStep === 1}
          <p class="delete-warning">{$t('deleteClone.stopInfo')}</p>
        {:else if deleteConfirmStep === 2}
          <p class="delete-warning" class:delete-danger={isDangerous(deleteConfirm.status)}>
            {$t(deleteWarningKey(deleteConfirm.status))}
          </p>
        {:else}
          <p class="delete-final"><Rich key="deleteClone.final" /></p>
        {/if}
        {#if failure}
          <p role="alert" class="delete-danger">{failure}</p>
        {/if}
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={cancelDelete}>{$t('common.cancel')}</button>
        {#if deleteConfirmStep === 1}
          <button class="btn-cancel" on:click={confirmDelete} disabled={deleting}>{$t('deleteClone.stop')}</button>
          {#if !simple && !['not cloned', 'root offline'].includes(deleteConfirm.status)}
            <button class="btn-delete" on:click={() => { deleteConfirmStep = 2; failure = ''; }} disabled={deleting}>{$t('deleteClone.title')}</button>
          {/if}
        {:else if !simple && deleteConfirmStep === 2}
          <button class="btn-delete" on:click={() => deleteConfirmStep = 3}>{$t('common.delete')}</button>
        {:else if !simple}
          <button class="btn-delete btn-delete-final" on:click={confirmDelete} disabled={deleting}>
            {deleting ? $t('deleteClone.deleting') : $t('deleteClone.confirm')}
          </button>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>

</style>
