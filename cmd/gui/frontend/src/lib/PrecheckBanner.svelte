<script lang="ts">
  // Missing-tool banner for a credential type (DoctorPrecheck), shared by
  // Add account and Change credential. noteKey is the install note, one
  // whole sentence per caller.
  import type { DoctorPrecheckDTO } from './bridge';
  import { t } from './i18n';
  import Rich from './Rich.svelte';

  export let precheck: DoctorPrecheckDTO;
  export let noteKey: string;
  export let onRecheck: () => void;
</script>

<div class="cred-precheck">
  <p class="cred-precheck-title">⚠ {precheck.summary}</p>
  {#if precheck.missing}
    {#each precheck.missing as m}
      <p class="cred-precheck-hint"><strong>{m.displayName}</strong>: <code>{m.installHint}</code></p>
    {/each}
  {/if}
  <p class="cred-precheck-note"><Rich key={noteKey} /></p>
  <button class="btn-cancel cred-action-btn" on:click={onRecheck}>{$t('common.recheck')}</button>
</div>

<style>
  .cred-precheck {
    margin: 10px 0; padding: 10px 12px; border-radius: 6px; font-size: 12px;
    background: rgba(245, 158, 11, 0.10);
    border: 1px solid #f59e0b88;
  }
  :global([data-theme="light"]) .cred-precheck { background: rgba(245, 158, 11, 0.08); border-color: #b45309; }
  .cred-precheck-title { margin: 0 0 6px; font-weight: 600; color: #f59e0b; }
  :global([data-theme="light"]) .cred-precheck-title { color: #b45309; }
  .cred-precheck-hint { margin: 4px 0; color: var(--text-primary); }
  .cred-precheck-hint code { background: var(--bg-hover); padding: 1px 5px; border-radius: 3px; font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 11px; }
  .cred-precheck-note { margin: 6px 0 8px; color: var(--text-muted); font-size: 11px; }
</style>
