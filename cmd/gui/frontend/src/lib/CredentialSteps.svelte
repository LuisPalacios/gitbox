<script lang="ts">
  import type { CredentialStepReport } from './types';
  import { t } from './i18n';

  export let report: CredentialStepReport;
  const stepKey: Record<string, string> = {
    prepare: 'cred.step.prepare',
    reserve: 'cred.step.reserve',
    verify: 'cred.step.verify',
    save: 'cred.step.save',
    activate: 'cred.step.activate',
    reconfigure: 'cred.step.reconfigure',
    retire: 'cred.step.retire',
    finish: 'cred.step.finish',
  };
</script>

{#if report.steps.some(step => !step.ok)}
  <ul class="form-error" aria-live="polite">
    {#each report.steps.filter(step => !step.ok) as step}
      <li>{$t(stepKey[step.name] || 'cred.transitionFailed')}{step.repo ? ' · ' + step.repo : ''}{step.err === 'offline' ? ' · ' + $t('cred.step.offline') : ''}</li>
    {/each}
  </ul>
{/if}
