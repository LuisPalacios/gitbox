<script lang="ts">
  // Confirm removing a mirror group from the config.
  import { fade, slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { pushNotice, errorText } from '../notices';
  import { t } from '../i18n';
  import { dismiss } from '../dismiss';
  import { configStore, mirrors } from '../stores';

  export let deleteMode: boolean;

  let deleteMirrorGroupConfirm: string | null = null;

  export function ask(mirrorKey: string) {
    deleteMirrorGroupConfirm = mirrorKey;
  }

  async function confirmDeleteMirrorGroup() {
    if (!deleteMirrorGroupConfirm) return;
    try {
      await bridge.deleteMirrorGroup(deleteMirrorGroupConfirm);
      $configStore = await bridge.reloadConfig();
    } catch (e: any) {
      pushNotice('error', $t('notice.mirrorGroupDeleteFailed'), [errorText(e)]);
    }
    deleteMirrorGroupConfirm = null;
    deleteMode = false;
  }
</script>

{#if deleteMirrorGroupConfirm}
  {@const delMir = $mirrors[deleteMirrorGroupConfirm]}
  <div class="overlay" use:dismiss={() => deleteMirrorGroupConfirm = null} transition:fade={{ duration: 120 }}>
    <div class="modal modal-confirm" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head"><h3>Delete mirror group</h3></div>
      <div class="modal-body">
        <p class="delete-warning">Remove mirror group <strong>{deleteMirrorGroupConfirm}</strong> ({delMir ? Object.keys(delMir.repos).length : 0} repos)?</p>
        <p class="delete-warning delete-danger">This only removes the config entry. Existing mirror configurations on the servers will remain.</p>
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={() => deleteMirrorGroupConfirm = null}>Cancel</button>
        <button class="btn-danger" on:click={confirmDeleteMirrorGroup}>Delete</button>
      </div>
    </div>
  </div>
{/if}

<style>

</style>
