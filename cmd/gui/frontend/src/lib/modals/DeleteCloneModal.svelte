<script lang="ts">
  // Two-step confirm that deletes a local clone and its config entry.
  import { fade, slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { dismiss } from '../dismiss';
  import type { RepoDetailView } from '../types';

  export let expandedRepo: string | null;
  export let repoDetail: RepoDetailView | null;
  export let deleteMode: boolean;
  export let reloadFromDisk: () => Promise<void>;

  let deleteConfirm: { sourceKey: string; repoKey: string; status: string } | null = null;
  let deleteConfirmStep = 0; // 0=closed, 1=first confirm, 2=final confirm
  let deleting = false;

  export function askDelete(sourceKey: string, repoKey: string, status: string) {
    deleteConfirm = { sourceKey, repoKey, status };
    deleteConfirmStep = 1;
  }

  function cancelDelete() {
    deleteConfirm = null;
    deleteConfirmStep = 0;
  }

  async function confirmDelete() {
    if (!deleteConfirm) return;
    if (deleteConfirmStep === 1) {
      deleteConfirmStep = 2;
      return;
    }
    // Step 2 — actually delete.
    deleting = true;
    const deletedKey = `${deleteConfirm.sourceKey}/${deleteConfirm.repoKey}`;
    try {
      await bridge.deleteRepo(deleteConfirm.sourceKey, deleteConfirm.repoKey);
      // Clear expanded detail if the deleted repo was open.
      if (expandedRepo === deletedKey) {
        expandedRepo = null;
        repoDetail = null;
      }
      await reloadFromDisk();
    } finally {
      deleting = false;
      deleteConfirm = null;
      deleteConfirmStep = 0;
      deleteMode = false;
    }
  }

  function isDangerous(status: string): boolean {
    return ['dirty', 'ahead', 'diverged', 'conflict', 'upstream gone'].includes(status);
  }

  function deleteWarning(status: string): string {
    if (status === 'not cloned') return 'This will remove the config entry. No local folder exists.';
    if (status === 'upstream gone') return 'The remote repository no longer exists. This local clone may be your ONLY copy — deleting it is unrecoverable.';
    if (isDangerous(status)) return 'This repo has unpushed commits or local changes that will be permanently lost!';
    return 'The local folder and config entry will be permanently deleted.';
  }
</script>

{#if deleteConfirm}
  <div class="overlay" use:dismiss={cancelDelete} transition:fade={{ duration: 120 }}>
    <div class="modal modal-delete" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head modal-head-delete">
        <h3>Delete local clone</h3>
        <button class="btn-x" on:click={cancelDelete}>&#10005;</button>
      </div>
      <div class="modal-body">
        {#if deleteConfirmStep === 1}
          <p class="delete-repo-name">{deleteConfirm.repoKey}</p>
          <p class="delete-warning" class:delete-danger={isDangerous(deleteConfirm.status)}>
            {deleteWarning(deleteConfirm.status)}
          </p>
        {:else}
          <p class="delete-repo-name">{deleteConfirm.repoKey}</p>
          <p class="delete-final">This action <strong>cannot be undone</strong>. Are you absolutely sure?</p>
        {/if}
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={cancelDelete}>Cancel</button>
        {#if deleteConfirmStep === 1}
          <button class="btn-delete" on:click={confirmDelete}>Delete</button>
        {:else}
          <button class="btn-delete btn-delete-final" on:click={confirmDelete} disabled={deleting}>
            {deleting ? 'Deleting...' : 'Yes, delete permanently'}
          </button>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>

</style>
