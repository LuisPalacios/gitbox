<script lang="ts">
  // A mirror needs a PAT the remote server can use; offers to store one.
  import { fade, slide } from 'svelte/transition';
  import { dismiss } from '../dismiss';
  import type { MirrorCredentialCheck } from '../types';
  import { t } from '../i18n';
  import Rich from '../Rich.svelte';

  export let mirrorCredWarning: MirrorCredentialCheck | null;
  // Opens the credential modal on the account's token step.
  export let storeMirrorPat: (accountKey: string) => void;
</script>

{#if mirrorCredWarning}
  <div class="overlay" use:dismiss={() => mirrorCredWarning = null} transition:fade={{ duration: 120 }}>
    <div class="modal modal-confirm" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head">
        <h3>{$t('mirrorToken.title')}</h3>
        <button class="btn-x" on:click={() => mirrorCredWarning = null}>&#10005;</button>
      </div>
      <div class="modal-body">
        <p class="delete-warning">{mirrorCredWarning.message}</p>
        <p style="color: var(--text-muted); margin-top: 8px;"><Rich key="mirrorToken.account" vars={{ account: mirrorCredWarning.accountKey }} /></p>
        <p style="color: var(--text-muted);">{$t('mirrorToken.gcmNote')}</p>
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={() => mirrorCredWarning = null}>{$t('common.cancel')}</button>
        <button class="btn-add" on:click={() => {
          const key = mirrorCredWarning?.accountKey;
          mirrorCredWarning = null;
          if (key) storeMirrorPat(key);
        }}>{$t('mirrorToken.store')}</button>
      </div>
    </div>
  </div>
{/if}

<style>

</style>
