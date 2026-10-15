<script lang="ts">
  // Result of setting up one mirror (mirror:setup:done).
  import { fade, slide } from 'svelte/transition';
  import { dismiss } from '../dismiss';
  import type { MirrorSetupResult } from '../types';
  import { t } from '../i18n';

  export let mirrorSetupResultModal: MirrorSetupResult | null;
</script>

{#if mirrorSetupResultModal}
  <div class="overlay" use:dismiss={() => mirrorSetupResultModal = null} transition:fade={{ duration: 120 }}>
    <div class="modal modal-confirm" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head">
        <h3>{mirrorSetupResultModal.error ? $t('mirrorSetup.failed') : $t('mirrorSetup.complete')}</h3>
        <button class="btn-x" on:click={() => mirrorSetupResultModal = null}>&#10005;</button>
      </div>
      <div class="modal-body">
        {#if mirrorSetupResultModal.error}
          <p class="form-error">{mirrorSetupResultModal.error}</p>
        {:else}
          <p style="color: var(--text)">
            <strong>{mirrorSetupResultModal.repoKey}</strong> &mdash;
            {mirrorSetupResultModal.created ? $t('mirrorSetup.created') : ''}
            {mirrorSetupResultModal.created && mirrorSetupResultModal.mirrored ? ', ' : ''}
            {mirrorSetupResultModal.mirrored ? $t('mirrorSetup.mirrored') : ''}
          </p>
        {/if}
        {#if mirrorSetupResultModal.method === 'manual' && mirrorSetupResultModal.instructions}
          <pre class="mirror-instructions">{mirrorSetupResultModal.instructions}</pre>
        {/if}
      </div>
      <div class="modal-foot">
        <button class="btn-add" on:click={() => mirrorSetupResultModal = null}>OK</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .mirror-instructions { font-size: 11px; white-space: pre-wrap; background: var(--bg-raised); padding: 8px; border-radius: 4px; margin-top: 8px; overflow: auto; max-height: 200px; }
</style>
