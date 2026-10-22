<script lang="ts">
  // Unmatched clones (#149): clones under the roots that the reconcile pass
  // couldn't place on its own. Each row can be adopted into an account I
  // pick, in place, or ignored; ignored ones can be brought back.
  import { fade, slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { pushNotice, errorText } from '../notices';
  import { t } from '../i18n';
  import { dismiss } from '../dismiss';
  import type { OrphanRepoDTO, ReconcileDTO } from '../types';

  export let unmatched: OrphanRepoDTO[];
  export let ignored: OrphanRepoDTO[];
  // Shows a fresh reconcile result (lists, config and status reload).
  export let applyReconcile: (res: ReconcileDTO) => Promise<void>;

  let open = false;
  let showIgnored = false;
  let busy = '';
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

  export function showOrphanModal() {
    picked = {};
    showIgnored = false;
    open = true;
  }

  $: for (const o of unmatched) {
    if (adoptable(o) && !picked[o.path]) picked[o.path] = o.matchedAccount || o.candidates![0];
  }

  async function adoptOne(o: OrphanRepoDTO) {
    busy = o.path;
    try {
      const res = await bridge.adoptClones([{ path: o.path, account: picked[o.path] }]);
      if (res.error) pushNotice('error', $t('notice.adoptFailed'), [res.error]);
      await applyReconcile(res);
    } catch (e) {
      pushNotice('error', $t('notice.adoptFailed'), [errorText(e)]);
    }
    busy = '';
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

{#if open}
  <div class="overlay" use:dismiss={() => open = false} transition:fade={{ duration: 120 }}>
    <div class="modal modal-adopt" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head">
        <h3>{$t('unmatched.title')}</h3>
        <button class="btn-x" on:click={() => open = false}>&#10005;</button>
      </div>
      <div class="modal-body">
        <p class="intro">{unmatched.length ? $t('unmatched.intro') : $t('unmatched.empty')}</p>
        {#each unmatched as o (o.path)}
          <div class="clone">
            <div class="clone-head">
              <span class="clone-name">{o.repoKey || o.relPath}</span>
              {#if o.nested}<span class="tag">{$t('unmatched.nested')}</span>{/if}
              <span class="reason">{$t(reasonKeys[o.reason] || 'unmatched.unknownHost')}</span>
            </div>
            <div class="clone-path" title={o.path}>{o.relPath}{#if o.remoteURL}&nbsp;· {o.remoteURL}{/if}</div>
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
                <button class="btn-adopt" on:click={() => adoptOne(o)} disabled={!!busy}>{$t('unmatched.adopt')}</button>
              {/if}
              <button class="btn-cancel" on:click={() => setIgnored(o, true)} disabled={!!busy}>{$t('unmatched.ignore')}</button>
            </div>
          </div>
        {/each}
        {#if ignored.length}
          <button class="link" on:click={() => showIgnored = !showIgnored}>
            {showIgnored ? $t('unmatched.hideIgnored') : $t('unmatched.showIgnored', { n: ignored.length })}
          </button>
          {#if showIgnored}
            {#each ignored as o (o.path)}
              <div class="clone clone-ignored">
                <div class="clone-head">
                  <span class="clone-name">{o.repoKey || o.relPath}</span>
                  <button class="btn-cancel" on:click={() => setIgnored(o, false)} disabled={!!busy}>{$t('unmatched.unignore')}</button>
                </div>
                <div class="clone-path" title={o.path}>{o.relPath}</div>
              </div>
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
  .clone-actions { display: flex; align-items: center; gap: 8px; margin-top: 6px; }
  .account { display: flex; align-items: center; gap: 6px; font-size: 11px; color: var(--text-secondary); }
  .account select { min-width: 160px; }
  .btn-adopt {
    padding: 4px 12px; background: #22c55e; border: none; color: #000;
    border-radius: 6px; cursor: pointer; font-size: 12px; font-weight: 600;
  }
  .btn-adopt:hover:not(:disabled) { background: #16a34a; }
  .btn-adopt:disabled { opacity: 0.5; cursor: default; }
  .clone-actions .btn-cancel { margin-left: auto; }
  .clone-ignored .btn-cancel { margin-left: auto; }
  .link {
    margin-top: 10px; padding: 0; border: none; background: none; cursor: pointer;
    font-size: 11px; color: var(--text-secondary); text-decoration: underline; text-underline-offset: 3px;
  }
</style>
