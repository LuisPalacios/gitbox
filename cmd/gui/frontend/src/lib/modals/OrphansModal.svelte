<script lang="ts">
  // Unmatched clones (#149): clones under the roots that the reconcile pass
  // couldn't place on its own. Each row can be adopted into an account I
  // pick, in place, or ignored; ignored ones can be brought back. A row I
  // can't adopt says what to do instead (#351), and every row opens its
  // folder in the file manager or a terminal, like a clone row.
  import { fade, slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { pushNotice, errorText } from '../notices';
  import { t } from '../i18n';
  import { ro, remoteOff } from '../ro';
  import { dismiss } from '../dismiss';
  import { remoteHost } from '../stores';
  import { unavailableOff } from '../remotehost';
  import BrandIcon, { terminalLogo } from '../BrandIcon.svelte';
  import UIIcon from '../UIIcon.svelte';
  import type { OrphanRepoDTO, ReconcileDTO, TerminalProfileInfo } from '../types';

  export let unmatched: OrphanRepoDTO[];
  export let skipped: NonNullable<ReconcileDTO['skipped']>;
  export let skippedFolders = 0;
  export let ignored: OrphanRepoDTO[];
  // Shows a fresh reconcile result (lists, config and status reload).
  export let applyReconcile: (res: ReconcileDTO) => Promise<void>;
  // The OS-aware "Reveal in …" label and the terminal profiles of the
  // clone rows' launchers.
  export let revealLabel = '';
  export let profiles: TerminalProfileInfo[] = [];
  // addAccountFor opens Add account for a clone's server; the list
  // refreshes itself once the account exists.
  export let addAccountFor: (host: string) => void = () => {};

  let open = false;
  let showIgnored = false;
  let busy = '';
  let workspaceSearch: OrphanRepoDTO[] | null = null;
  $: visibleUnmatched = workspaceSearch ?? unmatched;
  // The account picked per row, keyed by path.
  let picked: Record<string, string> = {};

  // Literal keys, so the i18n checker sees every one.
  const reasonKeys: Record<string, string> = {
    'host-only': 'unmatched.hostOnly',
    ambiguous: 'unmatched.ambiguous',
    'unknown-host': 'unmatched.unknownHost',
    'no-remote': 'unmatched.noRemote',
    'already-tracked': 'unmatched.alreadyTracked',
    worktree: 'unmatched.worktree',
  };

  // A clone I can adopt has accounts to pick from and a free repo key.
  const adoptable = (o: OrphanRepoDTO) => (o.candidates?.length || 0) > 0 && !['already-tracked', 'worktree'].includes(o.reason);

  // hint says what I can do with a row I can't adopt.
  function hint(o: OrphanRepoDTO, tr: (key: string, vars?: Record<string, string | number>) => string): string {
    if (adoptable(o)) return '';
    switch (o.reason) {
      case 'unknown-host': return o.host ? tr('unmatched.unknownHostHint', { host: o.host }) : tr('unmatched.unknownHostHintNoHost');
      case 'worktree': return o.mainClone ? tr('unmatched.worktreeHint', { path: o.mainClone }) : tr('unmatched.worktreeHintNoMain');
      case 'already-tracked': return o.trackedPath ? tr('unmatched.trackedHint', { path: o.trackedPath }) : tr('unmatched.trackedHintNoPath');
      case 'no-remote': return tr('unmatched.noRemoteHint');
    }
    return '';
  }

  // The terminal button uses the default profile, as on a clone row.
  $: profile = (() => {
    const visible = profiles.filter((p) => !p.hidden);
    return visible.find((p) => p.default) || visible[0] || null;
  })();
  // The intro speaks of adopting only when a visible row offers it.
  $: anyAdoptable = visibleUnmatched.some(adoptable) || (showIgnored && ignored.some(adoptable));

  export function showOrphanModal() {
    picked = {};
    showIgnored = false;
    workspaceSearch = null;
    open = true;
  }

  export function showWorkspaceSearch(found: OrphanRepoDTO[]) {
    showOrphanModal();
    workspaceSearch = found;
  }

  $: for (const o of [...visibleUnmatched, ...ignored]) {
    if (adoptable(o) && !picked[o.path]) picked[o.path] = o.matchedAccount || o.candidates![0];
  }

  async function rescan() {
    busy = 'rescan';
    try {
      await applyReconcile(await bridge.reconcile());
    } catch (e) {
      pushNotice('error', $t('notice.reconcileFailed'), [errorText(e)]);
    } finally {
      busy = '';
    }
  }

  async function adoptOne(o: OrphanRepoDTO) {
    busy = o.path;
    try {
      const res = await bridge.adoptClones([{ path: o.path, account: picked[o.path], workspaceRoot: o.workspaceRoot }]);
      if (res.error) pushNotice('error', $t('notice.adoptFailed'), [res.error]);
      if (workspaceSearch && res.adopted > 0) workspaceSearch = workspaceSearch.filter(row => row.path !== o.path);
      await applyReconcile(res);
    } catch (e) {
      pushNotice('error', $t('notice.adoptFailed'), [errorText(e)]);
    }
    busy = '';
  }

  async function openFolder(o: OrphanRepoDTO) {
    try {
      await bridge.openInExplorer(o.path);
    } catch (e) {
      pushNotice('error', revealLabel || $t('launcher.revealDefault'), [errorText(e)]);
    }
  }

  async function openTerminal(o: OrphanRepoDTO) {
    if (!profile) return;
    try {
      await bridge.openProfile(o.path, profile.id);
    } catch (e) {
      pushNotice('error', $t('notice.openTerminalFailed'), [errorText(e)]);
    }
  }

  async function setIgnored(o: OrphanRepoDTO, ignore: boolean) {
    busy = o.path;
    try {
      await bridge.ignoreClone(o.path, ignore);
      await applyReconcile(await bridge.reconcile());
    } catch (e) {
      pushNotice('error', $t('notice.ignoreFailed'), [errorText(e)]);
    }
    busy = '';
  }
</script>

<!-- One unmatched or ignored clone: its name, why it waits, what I can do
     with it, and its folder's launchers. -->
{#snippet row(o: OrphanRepoDTO, isIgnored: boolean)}
  {@const next = hint(o, $t)}
  <div class="clone" class:clone-ignored={isIgnored}>
    <div class="clone-head">
      <span class="clone-name">{o.repoKey || o.relPath}</span>
      {#if o.nested && !isIgnored}<span class="tag">{$t('unmatched.nested')}</span>{/if}
      {#if reasonKeys[o.reason]}<span class="reason">{$t(reasonKeys[o.reason])}</span>{/if}
    </div>
    <div class="clone-path" title={o.path}>{o.relPath}{#if o.remoteURL}&nbsp;· {o.remoteURL}{/if}</div>
    {#if next}<p class="hint">{next}</p>{/if}
    <div class="clone-actions">
      {#if adoptable(o)}
        <label class="account">
          <span>{$t('unmatched.account')}</span>
          <select class="form-input" bind:value={picked[o.path]} disabled={!!busy}>
            {#each o.candidates || [] as acct (acct)}
              <option value={acct}>{acct}</option>
            {/each}
          </select>
        </label>
        <button use:ro={'adoptClones'} class="btn-adopt" on:click={() => adoptOne(o)} disabled={!!busy}>{$t('unmatched.adopt')}</button>
      {:else if o.reason === 'unknown-host'}
        <button use:ro={'addAccount'} class="btn-adopt" on:click={() => addAccountFor(o.host || '')} disabled={!!busy}>{$t('unmatched.addAccount')}</button>
      {/if}
      <span class="launch-group">
        <button use:remoteOff={unavailableOff($remoteHost)} class="icon-btn btn-launch" title={revealLabel || $t('launcher.revealDefault')} aria-label={revealLabel || $t('launcher.revealDefault')}
          on:click={() => openFolder(o)}><UIIcon name="folder" /></button>
        {#if profile}
          {@const pr = profile}
          <button class="icon-btn btn-launch" title={$t('simple.btnOpenWith', { name: pr.name })} aria-label={$t('simple.btnOpenWith', { name: pr.name })}
            on:click={() => openTerminal(o)}><BrandIcon name={terminalLogo(pr.terminal, pr.shell)} /></button>
        {/if}
      </span>
      {#if isIgnored}
        <button use:ro={'ignoreClone'} class="btn-cancel" on:click={() => setIgnored(o, false)} disabled={!!busy}>{$t('unmatched.unignore')}</button>
      {:else if !workspaceSearch}
        <button use:ro={'ignoreClone'} class="btn-cancel" on:click={() => setIgnored(o, true)} disabled={!!busy}>{$t('unmatched.ignore')}</button>
      {/if}
    </div>
  </div>
{/snippet}

{#if open}
  <div class="overlay" use:dismiss={() => open = false} transition:fade={{ duration: 120 }}>
    <div class="modal modal-adopt" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head">
        <h3>{$t('unmatched.title')}</h3>
        <button class="btn-x" on:click={() => open = false}>&#10005;</button>
      </div>
      <div class="modal-body">
        {#if skippedFolders && !workspaceSearch}<p role="status">{$t('unmatched.scanIncomplete', { n: skippedFolders })}</p>{/if}
        {#if workspaceSearch || visibleUnmatched.length || skipped.length || !skippedFolders}
          <p class="intro">{(visibleUnmatched.length || (!workspaceSearch && skipped.length)) ? $t(anyAdoptable ? 'unmatched.intro' : 'unmatched.introNoAdopt') : workspaceSearch ? $t('unmatched.workspaceEmpty') : $t('unmatched.empty')}</p>
        {/if}
        {#each visibleUnmatched as o (o.path)}
          {@render row(o, false)}
        {/each}
        {#if !workspaceSearch && skipped.length}
          {#each skipped as row (row.path)}
            <div class="clone">
              <div class="clone-name">{$t(row.reason === 'pathCollision' ? 'error.config.path_collision' : row.reason === 'pathUnavailable' ? 'error.config.path_unavailable' : 'unmatched.changed')}</div>
              <div class="clone-path" title={row.path}>{row.path}</div>
            </div>
          {/each}
          <button use:ro={'reconcile'} class="btn-cancel" disabled={!!busy} on:click={rescan}>{$t('unmatched.rescan')}</button>
        {/if}
        {#if !workspaceSearch && ignored.length}
          <button class="link" on:click={() => showIgnored = !showIgnored}>
            {showIgnored ? $t('unmatched.hideIgnored') : $t('unmatched.showIgnored', { n: ignored.length })}
          </button>
          {#if showIgnored}
            {#each ignored as o (o.path)}
              {@render row(o, true)}
            {/each}
          {/if}
        {/if}
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={() => open = false}>{$t('common.close')}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .modal-adopt { width: 560px; max-width: calc(100vw - 32px); }
  .intro { margin: 0 0 10px; font-size: 12px; color: var(--text-secondary); }
  .clone { padding: 8px 0; border-bottom: 1px solid var(--border); }
  .clone:last-of-type { border-bottom: none; }
  .clone-head { display: flex; align-items: center; gap: 8px; }
  .clone-name { font-size: 12px; font-weight: 600; color: var(--text-primary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .tag { flex: none; font-size: 10px; padding: 0 5px; border: 1px solid var(--border); border-radius: 4px; color: var(--text-secondary); }
  .reason { margin-left: auto; flex: none; font-size: 11px; font-style: italic; color: var(--text-muted); }
  .clone-path {
    margin-top: 2px; font-size: 11px; color: var(--text-muted);
    font-family: var(--font-mono, monospace); overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .hint { margin: 4px 0 0; font-size: 11px; color: var(--text-secondary); overflow-wrap: anywhere; }
  .clone-actions { display: flex; align-items: center; gap: 8px; margin-top: 6px; }
  .launch-group { display: inline-flex; align-items: center; gap: 1px; flex-shrink: 0; }
  .btn-launch { color: var(--text-secondary); }
  .account { display: flex; align-items: center; gap: 6px; font-size: 11px; color: var(--text-secondary); }
  .account select { min-width: 160px; }
  .btn-adopt {
    padding: 4px 12px; background: #22c55e; border: none; color: #000;
    border-radius: 6px; cursor: pointer; font-size: 12px; font-weight: 600;
  }
  .btn-adopt:hover:not(:disabled) { background: #16a34a; }
  .btn-adopt:disabled { opacity: 0.5; cursor: default; }
  .clone-actions .btn-cancel { margin-left: auto; }
  .link {
    margin-top: 10px; padding: 0; border: none; background: none; cursor: pointer;
    font-size: 11px; color: var(--text-secondary); text-decoration: underline; text-underline-offset: 3px;
  }
</style>
