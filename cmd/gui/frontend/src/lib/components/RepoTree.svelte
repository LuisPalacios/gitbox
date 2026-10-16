<script lang="ts">
  // A list of clone rows from repo-tree.ts, each followed by its detail
  // panel when open. Every repo view renders its rows through here.
  import { sources, repoStates } from '../stores';
  import { opensDetail } from '../status';
  import RepoRow from './RepoRow.svelte';
  import type { RowProps } from './RepoRow.svelte';
  import RepoDetail from './RepoDetail.svelte';
  import type { RepoRow as TreeRow } from '../repo-tree';

  export let rows: TreeRow[];
  // The source whose group holds these rows: a row from another source (a
  // nested clone under this group's container) shows its account.
  export let groupSource = '';
  export let rowProps: RowProps;
  export let showAccount = false;
  // Fade calm rows: the view sets it when some row in it needs me.
  export let fadeHealthy = false;
  // Parent label per row key, for rows shown away from their container.
  export let parentLabels: Record<string, string> = {};

  $: ({ expandedRepo, repoDetail, detailLoading, ...shared } = rowProps);
</script>

{#each rows as { sourceKey: rowSourceKey, repoName, depth, childCount, descendants } (`${rowSourceKey}/${repoName}`)}
  {@const repoKey = `${rowSourceKey}/${repoName}`}
  {@const state = $repoStates[repoKey] || { status: 'unknown' as const, progress: 0, behind: 0, modified: 0, untracked: 0, ahead: 0 }}
  <RepoRow {...shared} sourceKey={groupSource || rowSourceKey} {rowSourceKey} {repoName} {depth} {childCount} {descendants}
    rowAccountKey={$sources[rowSourceKey]?.account || rowSourceKey} {repoKey}
    isContainer={!!$sources[rowSourceKey]?.repos[repoName]?.container} {state} rowOpensDetail={opensDetail(state.status)}
    {showAccount} parentLabel={parentLabels[repoKey] || ''} {fadeHealthy} />
  {#if expandedRepo === repoKey}
    <RepoDetail simple={shared.simple} {state} {repoDetail} {detailLoading} />
  {/if}
{/each}
