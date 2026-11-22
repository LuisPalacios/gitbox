<script lang="ts">
  // Asks before an http link opens (#240): the link is shown whole, as text,
  // and opens only on Open.
  import { fade, slide } from 'svelte/transition';
  import { t } from '../i18n';
  import { dismiss } from '../dismiss';
  import { pendingLink } from '../external';

  const answer = (ok: boolean) => $pendingLink?.answer(ok);
</script>

{#if $pendingLink}
  <div class="overlay" use:dismiss={() => answer(false)} transition:fade={{ duration: 120 }}>
    <div class="modal modal-confirm" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head"><h3>{$t('link.httpTitle')}</h3></div>
      <div class="modal-body">
        <p class="delete-warning">{$t('link.httpBody')}</p>
        <p class="link-url" translate="no">{$pendingLink.url}</p>
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={() => answer(false)}>{$t('common.cancel')}</button>
        <button class="btn-add" on:click={() => answer(true)}>{$t('link.open')}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .link-url {
    margin: 10px 0 0; padding: 8px 10px; border: 1px solid var(--border); border-radius: 6px;
    background: var(--bg-hover); color: var(--text-primary); font-size: 12px; overflow-wrap: anywhere;
    font-family: 'JetBrains Mono', ui-monospace, Menlo, Consolas, monospace;
  }
</style>
