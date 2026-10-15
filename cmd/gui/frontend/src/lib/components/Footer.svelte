<script lang="ts">
  // Summary footer: the status counts (Simple or Advanced wording) and the
  // update pill.
  import { repoStates, summary, simpleSummary, mirrorSummary, themeStore } from '../stores';
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
    {#if Object.values($repoStates).some(state => state.status === 'unknown')}<span class="sum">{$t('simple.checking')}</span><span class="sep">&middot;</span>{/if}
    <span class="sum" style="color:{sc('clean')}">{$t('simple.savedN', { n: $simpleSummary.saved })}</span>
    {#if $simpleSummary.unsaved > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('dirty')}">{$t('simple.notSavedN', { n: $simpleSummary.unsaved })}</span>{/if}
    {#if $simpleSummary.help > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('error')}">{$t('simple.needHelpN', { n: $simpleSummary.help })}</span>{/if}
    {#if $simpleSummary.update > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('behind')}">{$t('simple.updateN', { n: $simpleSummary.update })}</span>{/if}
    {#if $simpleSummary.online > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('not cloned')}">{$t('simple.onlineN', { n: $simpleSummary.online })}</span>{/if}
  </div>
  {:else}
  <div class="summary-left">
    <span class="sum" style="color:{sc('clean')}">{$summary.clean} synced</span>
    {#if $summary.syncing > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('syncing')}">{$summary.syncing} syncing</span>{/if}
    {#if $summary.behind > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('behind')}">{$summary.behind} behind</span>{/if}
    {#if $summary.dirty > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('dirty')}">{$summary.dirty} local changes</span>{/if}
    {#if $summary.notCloned > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('not cloned')}">{$summary.notCloned} not local</span>{/if}
    {#if $mirrorSummary.total > 0}<span class="sep">&middot;</span><span class="sum" style="color:{$mirrorSummary.error > 0 ? sc('error') : sc('clean')}">{$mirrorSummary.active}/{$mirrorSummary.total} mirrors</span>{/if}
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
  .summary-left { display: flex; align-items: center; gap: 6px; }
  .sum { font-weight: 600; }
  .sep { color: var(--border-hover); }
</style>
