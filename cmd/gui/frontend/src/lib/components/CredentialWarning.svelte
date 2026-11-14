<script lang="ts">
  import type { CredentialFinding } from '../types';
  import { t } from '../i18n';

  export let findings: CredentialFinding[] = [];
  export let onReview: () => void;
  $: exposed = findings.some(finding => finding.exposed);
</script>

{#if exposed}
  <div class="credential-warning" role="alert">
    <span>{$t('credentialAudit.banner')}</span>
    <button on:click={onReview}>{$t('credentialAudit.review')}</button>
  </div>
{/if}

<style>
  .credential-warning { display: flex; align-items: center; gap: 8px; margin: 8px 16px; padding: 8px 12px; border: 1px solid #c2410c; border-radius: 8px; background: #431407; color: #fdba74; font-size: 12px; }
  .credential-warning span { flex: 1; }
  .credential-warning button { padding: 4px 8px; border: 0; border-radius: 5px; background: #c2410c; color: #fff; cursor: pointer; }
  :global([data-theme="light"]) .credential-warning { background: #fff7ed; color: #7c2d12; }
</style>
