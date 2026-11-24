<script lang="ts">
  // I show incoming commits alongside the clone's local file details.
  import { slide } from 'svelte/transition';
  import { themeStore } from '../stores';
  import { derivePresentation } from '../presentation';
  import { statusColor, countBadge } from '../status';
  import { t } from '../i18n';
  import Rich from '../Rich.svelte';
  import ErrorExplanation from './ErrorExplanation.svelte';
  import { errorInfo, repoIssue } from '../explain';
  import type { ErrorActions } from '../explain';
  import type { RepoState, RepoDetailView } from '../types';

  export let simple: boolean;
  export let state: RepoState;
  export let repoDetail: RepoDetailView | null;
  export let detailLoading: boolean;
  export let actions: ErrorActions;
  export let issueActions: ErrorActions;
  $: issue = repoDetail?.upstreamGone ? { code: "repo.upstream_gone" } : repoIssue(state);

  $: sc = (s: string) => statusColor(s, $themeStore);

  function kindIcon(kind: string): string {
    return kind === 'deleted' ? '−' : kind === 'added' ? '+' : kind === 'renamed' ? '→' : '~';
  }

  function kindLabel(kind: string): string {
    return kind === 'deleted' ? $t('detail.kindDeleted') : kind === 'added' ? $t('detail.kindAdded') : kind === 'renamed' ? $t('detail.kindRenamed') : kind === 'conflict' ? $t('status.conflict') : $t('detail.kindChanged');
  }
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex (I make the scrollable region reachable by keyboard.) -->
<div class="repo-detail" role="region" aria-label={$t('detail.title')} tabindex="0" aria-busy={detailLoading} transition:slide={{ duration: 150 }}>
  {#if issue}
    <ErrorExplanation info={issue} {simple} actions={issueActions} />
  {/if}
  {#if detailLoading}
    <span class="detail-loading">{$t('detail.loading')}</span>
  {:else if repoDetail?.problem || repoDetail?.error}
    {#if !issue}<ErrorExplanation info={errorInfo(repoDetail.problem, repoDetail.error)} {simple} {actions} />{/if}
  {:else if repoDetail}
    {#if !simple}
    <div class="detail-header">
      <span class="detail-branch"><Rich key="detail.branch" vars={{ branch: repoDetail.branch }} /></span>
      {#if repoDetail.ahead > 0}
        <span class="detail-badge" style="color:{sc(countBadge('ahead').color)}">{repoDetail.ahead} {$t(countBadge('ahead').word)}</span>
      {/if}
      {#if repoDetail.behind > 0}
        <span class="detail-badge" style="color:{sc(countBadge('behind').color)}">{repoDetail.behind} {$t(countBadge('behind').word)}</span>
      {/if}
    </div>
    {/if}
    {#if repoDetail.behind > 0 || repoDetail.incoming.length > 0 || repoDetail.incomingError || repoDetail.incomingProblem}
      <div class="detail-section-title">{$t(simple ? 'detail.incomingSimple' : 'detail.incoming')} <span class="sbadge" style="color:{sc('behind')}">{repoDetail.incoming.length}</span></div>
      <p class="detail-freshness">{$t('detail.incomingFreshness')}</p>
      {#if repoDetail.incomingError || repoDetail.incomingProblem}
        <ErrorExplanation info={errorInfo(repoDetail.incomingProblem, repoDetail.incomingError)} {simple} {actions} />
      {:else if repoDetail.incoming.length === 0}
        <p class="detail-clean">{$t('detail.noIncoming')}</p>
      {:else}
        <ul class="detail-commits">
          {#each repoDetail.incoming as commit (commit.hash)}
            <li class="detail-commit">
              <span class="detail-subject" translate="no">{commit.subject}</span>
              <span class="detail-author" translate="no">{commit.author}</span>
            </li>
          {/each}
        </ul>
      {/if}
    {/if}
    {#if repoDetail.changed.length > 0}
      <div class="detail-section-title">{$t('simple.changedFiles')} <span class="sbadge" style="color:{sc(countBadge('modified').color)}">{countBadge('modified').glyph}{repoDetail.changed.length}</span></div>
      {#each repoDetail.changed as file}
        <div class="detail-file">
          <span class="detail-kind" class:kind-added={file.kind === 'added'} class:kind-deleted={file.kind === 'deleted'} class:kind-conflict={file.kind === 'conflict'} title={kindLabel(file.kind)}>{kindIcon(file.kind)}</span>
          <span class="detail-path">{file.path}</span>
        </div>
      {/each}
    {/if}
    {#if repoDetail.untracked.length > 0}
      <div class="detail-section-title">{simple ? $t('simple.newFiles') : $t('detail.newFilesUntracked')} <span class="sbadge" style="color:{sc(countBadge('untracked').color)}">{countBadge('untracked').glyph}{repoDetail.untracked.length}</span></div>
      {#each repoDetail.untracked as file}
        <div class="detail-file">
          <span class="detail-kind kind-untracked" title={$t('detail.untracked')}>?</span>
          <span class="detail-path detail-path-dim">{file}</span>
        </div>
      {/each}
    {/if}
    {#if repoDetail.changed.length === 0 && repoDetail.untracked.length === 0 && repoDetail.ahead === 0 && repoDetail.behind === 0}
      <!-- A quiet working tree alone cannot establish an upstream. -->
      {#if state.status === 'no upstream'}
        {@const row = derivePresentation(state, simple ? 'simple' : 'advanced')}
        <span class="detail-clean">{$t(row.labelKey)}</span>
      {:else}
        <span class="detail-clean">{$t('detail.upToDate')}</span>
      {/if}
    {/if}
  {/if}
</div>

<style>
  .sbadge { font-size: 11px; font-weight: 600; white-space: nowrap; }
  .repo-detail {
    margin: 0 0 2px 0; padding: 8px 16px 10px 30px;
    background: var(--bg-card); border: 1px solid var(--border); border-radius: 6px;
    max-height: 180px; overflow-y: auto; font-size: 12px;
  }
  .detail-loading,
  .detail-clean {
    color: var(--text-muted); font-style: italic;
  }
  .detail-freshness { margin: 3px 0 6px; color: var(--text-muted); font-size: 11px; }
  .detail-commits { list-style: none; margin: 0; padding: 0; }
  .detail-commit { display: flex; flex-direction: column; gap: 2px; padding: 5px 0; }
  .detail-commit + .detail-commit { border-top: 1px solid var(--border); }
  .detail-subject { color: var(--text-primary); overflow-wrap: anywhere; }
  .detail-author { color: var(--text-secondary); font-size: 11px; overflow-wrap: anywhere; }

  .detail-header {
    display: flex; align-items: center; gap: 10px; margin-bottom: 6px;
    font-size: 11px; color: var(--text-secondary);
  }
  .detail-branch { color: var(--text-muted); }
  .detail-branch :global(strong) { color: var(--text-primary); font-weight: 600; }
  .detail-badge { font-weight: 600; font-size: 11px; }
  .detail-section-title {
    font-size: 10px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.5px;
    color: var(--text-dim); margin: 6px 0 3px 0;
  }
  .detail-file {
    display: flex; align-items: center; gap: 6px; padding: 1px 0;
    font-family: 'SF Mono', 'Cascadia Code', 'Consolas', monospace;
    font-size: 10px;
  }
  .detail-kind {
    width: 14px; text-align: center; font-weight: 700; flex-shrink: 0;
    color: var(--text-secondary);
  }
  .kind-added { color: #61fd5f; }
  .kind-deleted { color: #D81E5B; }
  .kind-conflict { color: #D81E5B; }
  .kind-untracked { color: var(--text-dim); }
  .detail-path { color: var(--text-repo); word-break: break-all; }
  .detail-path-dim { color: var(--text-dim); }
</style>
