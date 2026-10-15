<script lang="ts">
  // A mirror needs a PAT the remote server can use; offers to store one.
  import { fade, slide } from 'svelte/transition';
  import { dismiss } from '../dismiss';
  import type { MirrorCredentialCheck } from '../types';

  export let mirrorCredWarning: MirrorCredentialCheck | null;
  // Opens the credential modal on the account's token step.
  export let storeMirrorPat: (accountKey: string) => void;
</script>

{#if mirrorCredWarning}
  <div class="overlay" use:dismiss={() => mirrorCredWarning = null} transition:fade={{ duration: 120 }}>
    <div class="modal modal-confirm" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head">
        <h3>Mirror token needed</h3>
        <button class="btn-x" on:click={() => mirrorCredWarning = null}>&#10005;</button>
      </div>
      <div class="modal-body">
        <p class="delete-warning">{mirrorCredWarning.message}</p>
        <p style="color: var(--text-muted); margin-top: 8px;">Account: <strong>{mirrorCredWarning.accountKey}</strong></p>
        <p style="color: var(--text-muted);">GCM OAuth tokens are machine-local and can't be used by remote servers. You need to create a PAT and store it.</p>
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={() => mirrorCredWarning = null}>Cancel</button>
        <button class="btn-add" on:click={() => {
          const key = mirrorCredWarning?.accountKey;
          mirrorCredWarning = null;
          if (key) storeMirrorPat(key);
        }}>Store PAT</button>
      </div>
    </div>
  </div>
{/if}

<style>

</style>
