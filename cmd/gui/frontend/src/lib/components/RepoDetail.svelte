<script lang="ts">
  // The detail panel under a repo row: the gone-upstream warning, or the
  // branch, the counts and the changed and untracked files.
  import { slide } from 'svelte/transition';
  import { themeStore } from '../stores';
  import { statusColor, countBadge } from '../status';
  import { t } from '../i18n';
  import type { RepoState, RepoDetailView } from '../types';

  export let simple: boolean;
  export let state: RepoState;
  export let repoDetail: RepoDetailView | null;
  export let detailLoading: boolean;

  $: sc = (s: string) => statusColor(s, $themeStore);

  function kindIcon(kind: string): string {
    return kind === 'deleted' ? '−' : kind === 'added' ? '+' : kind === 'renamed' ? '→' : '~';
  }

  function kindLabel(kind: string): string {
    return kind === 'deleted' ? 'Deleted' : kind === 'added' ? 'New file' : kind === 'renamed' ? 'Renamed' : kind === 'conflict' ? 'Conflict' : 'Changed';
  }
</script>

<div class="repo-detail" transition:slide={{ duration: 150 }}>
  {#if simple && (state.status === 'upstream gone' || repoDetail?.upstreamGone)}
    <div class="detail-gone">
      <p class="detail-gone-body" style="color:{sc('upstream gone')}">&#9888; {$t('simple.gone')}</p>
    </div>
  {:else if state.status === 'upstream gone' || repoDetail?.upstreamGone}
    <div class="detail-gone">
      <div class="detail-gone-title" style="color:{sc('upstream gone')}">&#9888; WARNING: The origin repository is gone</div>
      <p class="detail-gone-body">
        The remote repository for this clone was not found on the provider. It was
        either deleted, made private without re-granting access, renamed without a
        redirect, or the credential lost permission to see it.
      </p>
      <p class="detail-gone-body">
        <strong>Your local clone is intact.</strong> You have two options:
      </p>
      <ul class="detail-gone-list">
        <li><strong>Keep it as a local-only clone.</strong> Do nothing — the folder stays on disk with its full git history. Gitbox will keep flagging it as "Upstream gone" until you remove the remote or the entry.</li>
        <li><strong>Delete it</strong> (trash icon in the top bar) if you no longer need the code. Since the upstream is gone, this local copy may be the only remaining one — make sure before confirming.</li>
      </ul>
    </div>
  {:else if detailLoading}
    <span class="detail-loading">Loading...</span>
  {:else if repoDetail?.error}
    <span class="detail-error">{repoDetail.error}</span>
  {:else if repoDetail}
    {#if !simple}
    <div class="detail-header">
      <span class="detail-branch">Branch: <strong>{repoDetail.branch}</strong></span>
      {#if repoDetail.ahead > 0}
        <span class="detail-badge" style="color:{sc(countBadge('ahead').color)}">{repoDetail.ahead} {$t(countBadge('ahead').word)}</span>
      {/if}
      {#if repoDetail.behind > 0}
        <span class="detail-badge" style="color:{sc(countBadge('behind').color)}">{repoDetail.behind} {$t(countBadge('behind').word)}</span>
      {/if}
    </div>
    {/if}
    {#if repoDetail.changed.length > 0}
      <div class="detail-section-title">{simple ? $t('simple.changedFiles') : 'Changed files'} <span class="sbadge" style="color:{sc(countBadge('modified').color)}">{countBadge('modified').glyph}{repoDetail.changed.length}</span></div>
      {#each repoDetail.changed as file}
        <div class="detail-file">
          <span class="detail-kind" class:kind-added={file.kind === 'added'} class:kind-deleted={file.kind === 'deleted'} class:kind-conflict={file.kind === 'conflict'} title={kindLabel(file.kind)}>{kindIcon(file.kind)}</span>
          <span class="detail-path">{file.path}</span>
        </div>
      {/each}
    {/if}
    {#if repoDetail.untracked.length > 0}
      <div class="detail-section-title">{simple ? $t('simple.newFiles') : 'New files (untracked)'} <span class="sbadge" style="color:{sc(countBadge('untracked').color)}">{countBadge('untracked').glyph}{repoDetail.untracked.length}</span></div>
      {#each repoDetail.untracked as file}
        <div class="detail-file">
          <span class="detail-kind kind-untracked" title="Untracked">?</span>
          <span class="detail-path detail-path-dim">{file}</span>
        </div>
      {/each}
    {/if}
    {#if repoDetail.changed.length === 0 && repoDetail.untracked.length === 0 && repoDetail.ahead === 0 && repoDetail.behind === 0}
      <span class="detail-clean">Everything is up to date.</span>
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
  .detail-error,
  .detail-clean {
    color: var(--text-muted); font-style: italic;
  }
  .detail-error { color: var(--text-primary); }
  .detail-gone {
    display: flex; flex-direction: column; gap: 6px;
    font-size: 12px; line-height: 1.45;
  }
  .detail-gone-title { font-weight: 600; font-size: 12.5px; }
  .detail-gone-body { margin: 0; color: var(--text-secondary); }
  .detail-gone-list {
    margin: 2px 0 0 0; padding-left: 18px;
    color: var(--text-secondary);
  }
  .detail-gone-list li + li { margin-top: 4px; }
  .detail-header {
    display: flex; align-items: center; gap: 10px; margin-bottom: 6px;
    font-size: 11px; color: var(--text-secondary);
  }
  .detail-branch { color: var(--text-muted); }
  .detail-branch strong { color: var(--text-primary); font-weight: 600; }
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
