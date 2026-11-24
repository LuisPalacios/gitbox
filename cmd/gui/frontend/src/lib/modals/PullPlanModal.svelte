<script lang="ts">
  import { dismiss } from '../dismiss';
  import { t } from '../i18n';
  import type { main } from '../../../wailsjs/go/models';

  export let plan: main.PullPlanDTO;
  export let simple: boolean;
  export let onCancel: () => void;
  export let onConfirm: () => void;

  const localWork = new Set(['dirty', 'conflict', 'inProgress', 'ahead', 'diverged']);
  $: count = plan.willClone.length + plan.willUpdate.length + plan.skipped.length + plan.offline.length + plan.repairsPending.length;
  $: local = plan.skipped.filter(project => localWork.has(project.reason || '')).length;
  $: unknown = plan.skipped.filter(project => project.reason === 'unknown').length;
  $: other = plan.skipped.length - local - unknown;
  let groups: { action: string; projects: (Pick<main.PullPlanItem, 'source' | 'repo' | 'behind' | 'reason'>)[] }[];
  $: groups = [
    { action: 'clone', projects: plan.willClone },
    { action: 'update', projects: plan.willUpdate },
    { action: 'skip', projects: plan.skipped },
    { action: 'offline', projects: plan.offline },
    { action: 'repair', projects: plan.repairsPending },
  ];
</script>

<div class="overlay" use:dismiss={onCancel}>
  <div class="modal" role="dialog" aria-modal="true" aria-labelledby="pull-plan-title">
    <div class="modal-head">
      <h3 id="pull-plan-title">{$t(simple ? 'pullPlan.simpleTitle' : 'pullPlan.title')}</h3>
    </div>
    <div class="modal-body">
      <p>{$t(count === 1 ? 'pullPlan.project' : 'pullPlan.projects', { n: count })}</p>
      {#if plan.stale}
        <p role="status">{$t('pullPlan.stale')}</p>
      {/if}
      {#if simple}
        {#if plan.willClone.length > 0}
          <p>{$t(plan.willClone.length === 1 ? 'pullPlan.downloadOne' : 'pullPlan.download', { n: plan.willClone.length })}</p>
        {/if}
        {#if plan.willUpdate.length > 0}
          <p>{$t(plan.willUpdate.length === 1 ? 'pullPlan.updateOne' : 'pullPlan.update', { n: plan.willUpdate.length })}</p>
        {/if}
        {#if local}
          <p>{$t(local === 1 ? 'pullPlan.localOne' : 'pullPlan.local', { n: local })}</p>
        {/if}
        {#if unknown}
          <p>{$t(unknown === 1 ? 'pullPlan.unknownOne' : 'pullPlan.unknown', { n: unknown })}</p>
        {/if}
        {#if other}
          <p>{$t(other === 1 ? 'pullPlan.otherOne' : 'pullPlan.other', { n: other })}</p>
        {/if}
        {#if plan.offline.length > 0}
          <p>{$t(plan.offline.length === 1 ? 'pullPlan.offlineOne' : 'pullPlan.offline', { n: plan.offline.length })}</p>
        {/if}
        {#if plan.repairsPending.length > 0}
          <p>{$t(plan.repairsPending.length === 1 ? 'pullPlan.repairOne' : 'pullPlan.repair', { n: plan.repairsPending.length })}</p>
        {/if}
      {:else if count > 0}
        <table>
          <thead>
            <tr><th scope="col">{$t('pullPlan.repository')}</th><th scope="col">{$t('pullPlan.action')}</th></tr>
          </thead>
          <tbody>
            {#each groups as group}
              {#each group.projects as project}
                <tr>
                  <td><bdi>{project.source}/{project.repo}</bdi></td>
                  <td>
                    {#if project.behind}
                      {$t('pullPlan.fastForward', { n: project.behind })}
                    {:else if project.reason}
                      {$t(`pullPlan.reason.${project.reason}`)}
                    {:else}
                      {$t(`pullPlan.action.${group.action}`)}
                    {/if}
                  </td>
                </tr>
              {/each}
            {/each}
          </tbody>
        </table>
      {/if}
      {#if plan.willClone.length + plan.willUpdate.length === 0}
        <p>{$t('pullPlan.empty')}</p>
      {/if}
    </div>
    <div class="modal-foot">
      <button class="btn-cancel" on:click={onCancel}>{$t('common.cancel')}</button>
      <button class="btn-add" disabled={plan.willClone.length + plan.willUpdate.length === 0} on:click={onConfirm}>{$t('pullPlan.go')}</button>
    </div>
  </div>
</div>

<style>
  .modal { width: min(580px, 90vw); }
  .modal-body { max-height: 60vh; overflow: auto; }
  p { margin: 0 0 8px; }
  table { border-collapse: collapse; width: 100%; text-align: start; }
  th, td { padding: 6px 8px; text-align: start; border-bottom: 1px solid var(--border); }
  td:first-child { overflow-wrap: anywhere; }
</style>
