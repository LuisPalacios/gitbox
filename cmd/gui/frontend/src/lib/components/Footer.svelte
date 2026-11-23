<script lang="ts">
  // Summary footer: the status counts (Simple or Advanced wording) and the
  // update pill.
  import { summary, mirrorSummary, themeStore } from '../stores';
  import { statusColor } from '../status';
  import { t } from '../i18n';
  import UpdatePill from './UpdatePill.svelte';
  import type { UpdateInfo } from '../types';

  export let simple: boolean;
  export let updateInfo: UpdateInfo | null;
  export let updateApplying: boolean;
  export let updateProgress: string;
  export let updateDone: boolean;
  export let updateError: string;
  export let updateDismissed: string;

  $: sc = (s: string) => statusColor(s, $themeStore);
</script>

<footer class="summary">
  {#if simple}
  <div class="summary-left">
    {#if $summary.checking > 0}<span class="sum">{$t('simple.checking')}</span><span class="sep">&middot;</span>{/if}
    <span class="sum" style="color:{sc('clean')}">{$t('simple.savedN', { n: $summary.saved })}</span>
    {#if $summary.wip + $summary.unpushed > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('dirty')}">{$t('simple.wipN', { n: $summary.wip + $summary.unpushed })}</span>{/if}
    {#if $summary.help > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('error')}">{$t('simple.needHelpN', { n: $summary.help })}</span>{/if}
    {#if $summary.update > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('behind')}">{$t('simple.updateN', { n: $summary.update })}</span>{/if}
    {#if $summary.online > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('not cloned')}">{$t('simple.onlineN', { n: $summary.online })}</span>{/if}
  </div>
  {:else}
  <div class="summary-left">
    <span class="sum" style="color:{sc('clean')}">{$t('footer.upToDate', { n: $summary.saved })}</span>
    {#if $summary.syncing > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('syncing')}">{$t('footer.syncing', { n: $summary.syncing })}</span>{/if}
    {#if $summary.update > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('behind')}">{$t('footer.behind', { n: $summary.update })}</span>{/if}
    {#if $summary.ahead > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('ahead')}">{$t('footer.ahead', { n: $summary.ahead })}</span>{/if}
    {#if $summary.wip > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('dirty')}">{$t('footer.localChanges', { n: $summary.wip })}</span>{/if}
    {#if $summary.online > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('not cloned')}">{$t('footer.notCloned', { n: $summary.online })}</span>{/if}
    {#if $summary.help > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('error')}">{$t('footer.needAttention', { n: $summary.help })}</span>{/if}
    {#if $mirrorSummary.total > 0}<span class="sep">&middot;</span><span class="sum" style="color:{$mirrorSummary.error > 0 ? sc('error') : sc('clean')}">{$t('footer.mirrors', { active: $mirrorSummary.active, total: $mirrorSummary.total })}</span>{/if}
  </div>
  {/if}
  <UpdatePill bind:updateInfo bind:updateApplying bind:updateError bind:updateDismissed {updateProgress} {updateDone} />
</footer>

<style>
  .summary {
    display: flex; align-items: center; justify-content: space-between; gap: 6px;
    padding: 10px 24px; border-top: 1px solid var(--border); font-size: 12px; font-weight: 500;
    flex-shrink: 0;
  }
  .summary-left { display: flex; align-items: center; flex-wrap: wrap; gap: 2px 6px; min-width: 0; }
  .sum { font-weight: 600; white-space: nowrap; }
  .sep { color: var(--border-hover); }
</style>
