<script lang="ts">
  // A list of clone rows from repo-tree.ts, each followed by its detail
  // panel when open. Every repo view renders its rows through here.
  import { sources, repoStates } from '../stores';
  import { opensDetail } from '../status';
  import RepoRow from './RepoRow.svelte';
  import type { RowProps } from './RepoRow.svelte';
  import RepoDetail from './RepoDetail.svelte';
  import ErrorExplanation from './ErrorExplanation.svelte';
  import { repoIssue } from '../explain';
  import PlaceholderRow from './PlaceholderRow.svelte';
  import type { RepoRow as TreeRow } from '../repo-tree';

  export let rows: TreeRow[];
  // The source whose group holds these rows: a row from another source (a
  // nested clone under this group's container) shows its account.
  export let groupSource = '';
  export let rowProps: RowProps;
  export let showAccount = false;
  // Fade calm rows: the view sets it when some row in it needs me.
  export let fadeHealthy = false;
  export let showIssues = false;
  // Parent label per row key, for rows shown away from their container.
  export let parentLabels: Record<string, string> = {};

  $: ({ expandedRepo, repoDetail, detailLoading, findWorkspaceFolder, repoActions, credentialStatuses, fixProofCredential, ...shared } = rowProps);
</script>

{#each rows as { key, sourceKey: rowSourceKey, repoName, depth, childCount, descendants, placeholders, placeholder } (key)}
  {#if placeholder}
    <PlaceholderRow {placeholder} {depth} onFind={findWorkspaceFolder} />
  {:else}
  {@const repoKey = `${rowSourceKey}/${repoName}`}
  {@const state = $repoStates[repoKey] || { status: 'unknown' as const, progress: 0, behind: 0, modified: 0, untracked: 0, ahead: 0 }}
  {@const accountKey = $sources[rowSourceKey]?.account}
  {@const accountStatus = credentialStatuses[accountKey]?.status}
  {@const issue = repoIssue(state)}
  <RepoRow {...shared} sourceKey={groupSource || rowSourceKey} {rowSourceKey} {repoName} {depth} {childCount} {descendants} {placeholders}
    rowAccountKey={$sources[rowSourceKey]?.account || rowSourceKey} {repoKey}
    isContainer={!!$sources[rowSourceKey]?.repos[repoName]?.container} {state} rowOpensDetail={opensDetail(state.status)}
    {showAccount} parentLabel={parentLabels[repoKey] || ''} {fadeHealthy} />
  {#if expandedRepo === repoKey}
    <RepoDetail simple={shared.simple} {state} {repoDetail} {detailLoading} {accountKey} actions={repoActions(rowSourceKey, repoName, state, true, accountStatus)}
      issueActions={repoActions(rowSourceKey, repoName, state, false, accountStatus)}
      source={rowSourceKey} repo={repoName} credentialStatus={credentialStatuses[accountKey]}
      onFixCredential={() => fixProofCredential(accountKey || rowSourceKey)} />
  {:else if showIssues && issue}
    <div class="row-explanation"><ErrorExplanation info={issue} simple={shared.simple} {accountKey} actions={repoActions(rowSourceKey, repoName, state, false, accountStatus)} /></div>
  {/if}
  {/if}
{/each}

<style>
  .row-explanation { padding: 0 10px 8px 30px; }
</style>
