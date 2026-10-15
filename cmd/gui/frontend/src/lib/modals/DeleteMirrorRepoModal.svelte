<script lang="ts">
  // Confirm removing one repo from a mirror group.
  import { fade, slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { dismiss } from '../dismiss';
  import { configStore } from '../stores';

  export let deleteMode: boolean;

  let deleteMirrorRepoConfirm: { mirrorKey: string; repoKey: string } | null = null;

  export function ask(mirrorKey: string, repoKey: string) {
    deleteMirrorRepoConfirm = { mirrorKey, repoKey };
  }

  async function confirmDeleteMirrorRepo() {
    if (!deleteMirrorRepoConfirm) return;
    try {
      await bridge.deleteMirrorRepo(deleteMirrorRepoConfirm.mirrorKey, deleteMirrorRepoConfirm.repoKey);
      $configStore = await bridge.reloadConfig();
    } catch (e: any) {
      alert(e?.message || e);
    }
    deleteMirrorRepoConfirm = null;
    deleteMode = false;
  }
</script>

{#if deleteMirrorRepoConfirm}
  <div class="overlay" use:dismiss={() => deleteMirrorRepoConfirm = null} transition:fade={{ duration: 120 }}>
    <div class="modal modal-confirm" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head"><h3>Remove mirrored repo</h3></div>
      <div class="modal-body">
        <p class="delete-warning">Remove <strong>{deleteMirrorRepoConfirm.repoKey}</strong> from mirror <strong>{deleteMirrorRepoConfirm.mirrorKey}</strong>?</p>
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={() => deleteMirrorRepoConfirm = null}>Cancel</button>
        <button class="btn-danger" on:click={confirmDeleteMirrorRepo}>Remove</button>
      </div>
    </div>
  </div>
{/if}

<style>

</style>
