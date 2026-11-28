<script lang="ts">
  // The clone roots (#149): list them, add one, make one the default, remove
  // one (asking to forget the repos still under it), and the nested scan
  // depth. No clone moves on disk from here.
  import { fade, slide } from 'svelte/transition';
  import { bridge, settingsSaveFailed } from '../bridge';
  import { pushNotice, errorText } from '../notices';
  import { t } from '../i18n';
  import Rich from '../Rich.svelte';
  import { remoteHost, blocked, activeSession } from '../stores';
  import { get } from 'svelte/store';
  import { ro } from '../ro';
  import RemotePathInput from '../RemotePathInput.svelte';
  import { dismiss } from '../dismiss';
  import type { RootDTO } from '../types';

  export let reloadFromDisk: () => Promise<void>;
  // Picks up clones under a root just added, and shows repos of a removed
  // or missing root as they are (#149).
  export let runReconcile: () => Promise<unknown>;

  let open = false;
  let roots: RootDTO[] = [];
  // The root waiting for "forget and remove", with how many repos it holds.
  let confirming: { path: string; n: number } | null = null;
  let nestedScanDepth = 1;
  // In a remote window a root is typed for the host, not picked (#241).
  let typedRoot = '';
  let typedUsable = false;

  async function loadRoots() {
    try { roots = (await bridge.listRoots()) || []; } catch { roots = []; }
  }

  export async function openRoots() {
    confirming = null;
    typedRoot = '';
    await loadRoots();
    try { nestedScanDepth = (await bridge.getNestedScanDepth()) || 1; } catch { nestedScanDepth = 1; }
    open = true;
  }

  async function addRoot() {
    // The session the action started in (#381), captured before any await.
    const session = get(activeSession);
    const dir = $remoteHost ? typedRoot.trim() : await bridge.pickFolder($t('roots.pickAdd'));
    if (!dir) return;
    try {
      await bridge.addRoot(dir);
      // The typed folder is a root now; a failed add keeps it for a fix.
      typedRoot = '';
      await loadRoots();
      session.configStore.set(await bridge.reloadConfig());
      await runReconcile();
      await loadRoots();
    } catch (err: any) { pushNotice('error', $t('notice.rootAddFailed'), [errorText(err)]); }
  }

  async function makeDefault(path: string) {
    // The session the action started in (#381), captured before any await.
    const session = get(activeSession);
    try {
      await bridge.setDefaultRoot(path);
      await loadRoots();
      session.configStore.set(await bridge.reloadConfig());
    } catch (err: any) { pushNotice('error', $t('notice.rootDefaultFailed'), [errorText(err)]); }
  }

  async function removeRoot(path: string, forget: boolean) {
    try {
      const res = await bridge.removeRoot(path, forget);
      if (res.inUse > 0) {
        confirming = { path, n: res.inUse };
        return;
      }
      confirming = null;
      await loadRoots();
      // Forgotten repos leave every view, so reload status too.
      await reloadFromDisk();
      await runReconcile();
    } catch (err: any) { pushNotice('error', $t('notice.rootRemoveFailed'), [errorText(err)]); }
  }

  async function saveNestedScanDepth(d = Math.max(1, Math.floor(nestedScanDepth || 1))) {
    nestedScanDepth = d;
    // Persist only. Deliberately no config reload, dashboard re-render, or disk
    // re-scan here — those would block this dialog. Re-scan explicitly afterward
    // via a container repo's ⋮ → "Re-scan for nested clones".
    try {
      await bridge.setNestedScanDepth(d);
    } catch (err: any) {
      settingsSaveFailed(err, () => saveNestedScanDepth(d));
    }
  }
</script>

{#if open}
  <div class="overlay" use:dismiss={() => open = false} transition:fade={{ duration: 120 }}>
    <div class="modal modal-account" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head">
        <h3>{$t('roots.title')}</h3>
        <button class="btn-x" on:click={() => open = false}>&#10005;</button>
      </div>
      <div class="modal-body">
        <p class="hint"><Rich key="roots.hint" /></p>
        {#each roots as r (r.path)}
          <div class="root-row">
            <span class="root-path" class:missing={!r.exists} title={r.exists ? r.path : $t('roots.missing')}>{r.path}</span>
            {#if r.isDefault}<span class="badge">{$t('roots.default')}</span>{/if}
            <span class="root-n">{$t(r.repos === 1 ? 'roots.reposOne' : 'roots.repos', { n: r.repos })}</span>
            {#if !r.isDefault}
              <button use:ro={'setDefaultRoot'} class="settings-btn" on:click={() => makeDefault(r.path)}>{$t('roots.makeDefault')}</button>
            {/if}
            {#if roots.length > 1}
              <button use:ro={'removeRoot'} class="btn-sm btn-danger" on:click={() => removeRoot(r.path, false)} title={$t('roots.remove')} aria-label={$t('roots.remove')}>✕</button>
            {/if}
          </div>
          {#if confirming?.path === r.path}
            {@const n = confirming.n}
            <div class="confirm">
              <span>{$t(n === 1 ? 'roots.confirmForgetOne' : 'roots.confirmForget', { n })}</span>
              <button class="btn-cancel" on:click={() => confirming = null}>{$t('common.cancel')}</button>
              <button use:ro={'removeRoot'} class="btn-sm btn-danger" on:click={() => removeRoot(r.path, true)}>{$t('roots.forget')}</button>
            </div>
          {/if}
        {/each}
        {#if $remoteHost}
          <div class="remote-add">
            <RemotePathInput bind:value={typedRoot} bind:usable={typedUsable} />
            <button use:ro={'addRoot'} class="settings-btn add" on:click={addRoot} disabled={!typedUsable}>{$t('roots.addTyped')}</button>
          </div>
        {:else}
          <button class="settings-btn add" on:click={addRoot}>{$t('roots.add')}</button>
        {/if}

        <div class="form-row" style="margin-top:14px;">
          <label class="form-label" for="nested-scan-depth">{$t('roots.nestedDepth')}</label>
          <input id="nested-scan-depth" class="form-input" type="number" min="1" style="max-width:90px;"
            bind:value={nestedScanDepth} on:change={() => saveNestedScanDepth()} disabled={$blocked('setNestedScanDepth')} title={$blocked('setNestedScanDepth') ? $t('remote.readOnlyTip') : undefined} />
          <span class="settings-value">{$t('roots.nestedDepthHint')}</span>
        </div>
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={() => open = false}>{$t('common.close')}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .remote-add { display: flex; flex-direction: column; gap: 6px; margin-top: 8px; }
  .remote-add .add { align-self: flex-start; }
  .hint { margin: 0 0 10px; font-size: 12px; color: var(--text-secondary); }
  .root-row {
    display: flex; align-items: center; gap: 8px;
    padding: 6px; border-radius: 6px; transition: background 0.1s;
  }
  .root-row:hover { background: var(--bg-row-hover); }
  .root-path {
    flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
    font-size: 12px; font-family: 'SF Mono', 'Cascadia Code', 'Consolas', monospace; color: var(--text-primary);
  }
  .root-path.missing { color: var(--text-muted); text-decoration: line-through; }
  .badge {
    flex: none; font-size: 10px; padding: 1px 6px; border-radius: 4px;
    border: 1px solid var(--border); color: var(--text-secondary);
  }
  .root-n { flex: none; font-size: 11px; color: var(--text-muted); white-space: nowrap; }
  .confirm {
    display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
    margin: 2px 0 6px; padding: 8px; border-radius: 6px;
    font-size: 12px; color: var(--text-primary); background: var(--bg-hover);
  }
  .confirm span { flex: 1 1 220px; }
  .add { margin-top: 6px; }
  .settings-value { font-size: 12px; color: var(--text-secondary); font-family: monospace; flex: 1; }
  /* Legacy alias — .settings-btn routes to the unified action style. */
  .settings-btn {
    padding: 3px 12px;
    font-size: 11px;
    font-weight: 500;
    background: rgba(59, 130, 246, 0.10);
    border: 1px solid rgba(59, 130, 246, 0.28);
    color: var(--text-primary);
    border-radius: 5px;
    cursor: pointer;
    transition: background 0.12s, border-color 0.12s, color 0.12s;
    white-space: nowrap;
    flex-shrink: 0;
  }
  .settings-btn:hover {
    background: rgba(59, 130, 246, 0.18);
    border-color: rgba(59, 130, 246, 0.44);
  }
  :global([data-theme="light"]) .settings-btn {
    background: rgba(59, 130, 246, 0.08);
    border-color: rgba(59, 130, 246, 0.32);
  }
  :global([data-theme="light"]) .settings-btn:hover {
    background: rgba(59, 130, 246, 0.14);
    border-color: rgba(59, 130, 246, 0.50);
  }
</style>
