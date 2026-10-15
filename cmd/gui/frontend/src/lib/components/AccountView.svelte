<script lang="ts">
  // One account's view: its three-line card, then its clones as a tree,
  // problems first. A clone nested in another account's container shows
  // here at the top level with an "in <parent>" tag; the All view keeps
  // that cross-account nesting.
  import { sources, repoStates } from '../stores';
  import { CALM_RANK } from '../status';
  import type { HealthFix } from '../status';
  import { repoTreeRows } from '../repo-tree';
  import AccountHeader from './AccountHeader.svelte';
  import RepoTree from './RepoTree.svelte';
  import type { RowProps } from './RepoRow.svelte';

  export let accountKey: string;
  export let repoNesting: Record<string, string>;
  export let rankOf: (key: string) => number;
  export let credStatuses: Record<string, any>;
  export let actionMenuAccount: string | null;
  export let rowProps: RowProps;
  export let toggleAccountMenu: (accountKey: string) => void;
  export let closeAccountMenu: () => void;
  export let askDeleteAccount: (accountKey: string) => Promise<void>;
  export let openEditAccount: (accountKey: string) => void;
  export let openCredChange: (accountKey: string, currentType: string) => void;
  export let openDiscover: (accountKey: string) => void;
  export let openCreateRepo: (accountKey: string) => Promise<void>;
  export let fixCredential: (accountKey: string, fix: HealthFix) => void;

  $: own = Object.fromEntries(Object.entries($sources).filter(([, s]) => s.account === accountKey));
  $: tree = repoTreeRows(own, repoNesting, rowProps.collapsedRepos, rankOf);
  $: rows = Object.keys(own).flatMap((k) => tree[k] || []);
  // Top-level rows whose container belongs to another account.
  $: parentLabels = Object.fromEntries(rows.filter((r) => r.depth === 0 && repoNesting[r.key] && repoNesting[r.key] !== r.key).map((r) => [r.key, repoNesting[r.key]]));
  $: keys = Object.entries(own).flatMap(([sk, s]) => Object.keys(s.repos).map((r) => `${sk}/${r}`));
  $: needYou = keys.filter((k) => rankOf(k) < CALM_RANK).length;
  $: checking = keys.some((k) => !$repoStates[k] || $repoStates[k].status === 'unknown');
</script>

<section class="view-scroll">
  <AccountHeader {accountKey} {credStatuses} total={keys.length} {needYou} {checking} {actionMenuAccount} {rowProps}
    {toggleAccountMenu} {closeAccountMenu} {askDeleteAccount} {openEditAccount} {openCredChange} {openDiscover} {openCreateRepo} {fixCredential} />
  <div class="rows">
    <RepoTree {rows} {rowProps} {parentLabels} />
  </div>
</section>
