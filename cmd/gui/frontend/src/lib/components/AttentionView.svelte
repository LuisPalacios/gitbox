<script lang="ts">
  // Needs attention: accounts whose credential needs a fix, with their fix
  // action, then every repo that needs me, grouped needs help → local work
  // → behind → not cloned. Inside a group, rows sit under a foldable header
  // per account, and a container nests the clones beside it (#148); a clone
  // whose container is elsewhere keeps its parent tag. Rows keep their place
  // while the view is open: one that turns fine (after a pull, say) stays,
  // faded, until I leave the view and come back.
  import { accounts, sources, themeStore } from '../stores';
  import { statusColor, accountHealth, ATTENTION_GROUPS, CALM_RANK } from '../status';
  import type { HealthFix } from '../status';
  import { attentionItems, pinAttention, accountView } from '../views';
  import type { AttentionItem } from '../views';
  import { t } from '../i18n';
  import { onActivateKey } from '../keyboard';
  import UIIcon from '../UIIcon.svelte';
  import RepoTree from './RepoTree.svelte';
  import type { RowProps } from './RepoRow.svelte';
  import { compareRepoKeys, groupTreeRows } from '../repo-tree';

  export let rankOf: (key: string) => number;
  export let repoNesting: Record<string, string>;
  export let credStatuses: Record<string, any>;
  export let rowProps: RowProps;
  export let setView: (view: string) => void;
  export let fixCredential: (accountKey: string, fix: HealthFix) => void;
  export let onAddAccount: () => void;

  $: simple = rowProps.simple;
  $: sc = (s: string) => statusColor(s, $themeStore);

  // Every clone in alphabetical order; attentionItems keeps that order inside a group.
  $: keys = Object.entries($sources).flatMap(([sk, s]) => Object.keys(s.repos).map((r) => `${sk}/${r}`)).sort(compareRepoKeys);

  // The rank each row had when it first showed. Plain (not reactive) state:
  // pinning it must not re-run the list it comes from.
  let pinned: Record<string, number> = {};
  function list(keys: string[], rankOf: (key: string) => number): AttentionItem[] {
    const items = attentionItems(keys, rankOf, CALM_RANK, pinned);
    pinned = pinAttention(items, pinned);
    return items;
  }
  $: items = list(keys, rankOf);

  $: groups = ATTENTION_GROUPS.map((name, rank) => {
    const keys = items.filter((i) => i.rank === rank).map((i) => i.key);
    return { name, count: keys.length, accounts: groupTreeRows(keys, $sources, repoNesting, rowProps.collapsedRepos) };
  }).filter((g) => g.count > 0);
  // Top-level rows whose container sits in another group or account.
  $: parentLabels = Object.fromEntries(groups.flatMap((g) => g.accounts.flatMap((a) => a.rows))
    .filter((r) => r.depth === 0 && repoNesting[r.key] && repoNesting[r.key] !== r.key).map((r) => [r.key, repoNesting[r.key]]));

  // Folded account headers, keyed "group|source", for as long as the app runs.
  let folded = new Set<string>();
  function toggleFold(key: string) {
    folded.has(key) ? folded.delete(key) : folded.add(key);
    folded = folded;
  }
  $: broken = Object.keys($accounts).filter((k) => accountHealth(credStatuses[k]?.status, simple).attention);
  $: accountCount = Object.keys($accounts).length;
  $: needYou = items.filter((i) => !i.settled).length + broken.length;
</script>

<section class="view-scroll">
  <div class="view-head">
    <h3 class="view-title">{$t(simple ? 'nav.attentionSimple' : 'nav.attention')}</h3>
    {#if needYou > 0}<span class="view-sub">{$t(needYou === 1 ? 'account.needYouOne' : 'account.needYou', { n: needYou })}</span>{/if}
  </div>

  {#if accountCount === 0}
    <div class="empty">
      <p>{$t('attention.noAccounts')}</p>
      <button class="text-btn" on:click={onAddAccount}>{simple ? $t('nav.connect') : $t('account.add')}</button>
    </div>
  {:else if broken.length === 0 && groups.length === 0}
    <div class="empty">
      <p class="empty-title"><span style="color: {sc('clean')}">●</span> {$t('attention.empty')}</p>
      <p>{$t(simple ? 'attention.emptyDetailSimple' : 'attention.emptyDetail')}</p>
    </div>
  {:else}
    {#if broken.length > 0}
      <div class="group-h">{$t(simple ? 'attention.accountsSimple' : 'attention.accounts')}</div>
      {#each broken as key (key)}
        {@const h = accountHealth(credStatuses[key]?.status, simple)}
        <div class="acct-row">
          <span class="mark" style="color: {sc(h.color)}" aria-hidden="true">{h.mark}</span>
          <button class="acct-name" on:click={() => setView(accountView(key))}>{key}</button>
          <span class="acct-words" style="color: {sc(h.color)}">{$t(h.key)}</span>
          {#if h.fix}<button class="text-btn acct-fix" on:click={() => fixCredential(key, h.fix)}>{$t(h.fixKey)}</button>{/if}
        </div>
      {/each}
    {/if}
    {#each groups as g (g.name)}
      <div class="group-h">{$t(`attention.group.${g.name}${simple ? 'Simple' : ''}`)} <span class="group-n">· {g.count}</span></div>
      {#each g.accounts as a (a.sourceKey)}
        {@const fold = `${g.name}|${a.sourceKey}`}
        {@const isFolded = folded.has(fold)}
        <div class="acct-h" role="button" tabindex="0" aria-expanded={!isFolded}
          title={isFolded ? $t('collapse.expand') : $t('collapse.collapse')}
          on:click={() => toggleFold(fold)} on:keydown={onActivateKey(() => toggleFold(fold))}>
          <span class="chev" class:folded={isFolded} aria-hidden="true"><UIIcon name="chevron" size={16} /></span>
          <span class="acct-h-name">{a.sourceKey}</span>
          <span class="group-n">· {a.count}</span>
        </div>
        {#if !isFolded}
          <RepoTree rows={a.rows} groupSource={a.sourceKey} {rowProps} {parentLabels} fadeHealthy={items.some((i) => !i.settled)} />
        {/if}
      {/each}
    {/each}
  {/if}
</section>

<style>
  .group-h {
    display: flex; align-items: center; gap: 4px; padding: 12px 0 4px;
    font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.8px; color: var(--text-dim);
    border-bottom: 1px solid var(--border); margin-bottom: 2px;
  }
  .group-n { font-weight: 400; }
  .acct-h {
    display: flex; align-items: center; gap: 4px; padding: 6px 0 2px;
    font-size: 12px; font-weight: 600; color: var(--text-secondary); cursor: pointer; user-select: none;
  }
  .acct-h .group-n { color: var(--text-dim); }
  .chev {
    flex: none; width: 20px; height: 20px; display: inline-flex; align-items: center; justify-content: center;
    color: var(--text-muted); transition: transform 0.12s, color 0.12s;
  }
  .chev.folded { transform: rotate(-90deg); }
  .acct-h:hover .chev { color: var(--text-primary); }
  .acct-row { display: flex; align-items: center; gap: 10px; min-height: 38px; padding: 4px 6px; border-radius: 6px; }
  .acct-row:hover { background: var(--bg-row-hover); }
  .mark { width: 18px; flex: none; text-align: center; font-size: 14px; }
  .acct-name {
    padding: 0; border: none; background: none; cursor: pointer; text-align: left;
    font-size: 13px; font-weight: 600; color: var(--text-repo); white-space: nowrap;
  }
  .acct-name:hover { text-decoration: underline; text-underline-offset: 3px; }
  .acct-words { font-size: 12px; font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .acct-fix { margin-left: auto; }
  .empty { padding: 32px 8px; text-align: center; color: var(--text-muted); font-size: 13px; }
  .empty p { margin: 0 0 10px; }
  .empty-title { font-size: 15px; font-weight: 600; color: var(--text-primary); }
</style>
