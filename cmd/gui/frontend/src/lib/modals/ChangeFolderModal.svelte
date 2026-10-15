<script lang="ts">
  // Change the root folder, the extra scan folders and the nested scan depth.
  import { fade, slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { pushNotice, errorText } from '../notices';
  import { t } from '../i18n';
  import { configStore } from '../stores';
  import { dismiss } from '../dismiss';

  export let reloadFromDisk: () => Promise<void>;

  let changeFolderModal = false;
  let changeFolderPath = '';
  let changeFolderError = '';

  // Extra scan-root folders + nested-clone scan depth (non-standard clones).
  let extraFolders: string[] = [];
  let nestedScanDepth = 1;

  async function loadScanSettings() {
    try {
      extraFolders = (await bridge.listExtraFolders()) || [];
      nestedScanDepth = (await bridge.getNestedScanDepth()) || 1;
    } catch { extraFolders = []; }
  }

  async function addExtraFolder() {
    const dir = await bridge.pickFolder('Choose an extra scan folder');
    if (!dir) return;
    try {
      await bridge.addExtraFolder(dir);
      extraFolders = (await bridge.listExtraFolders()) || [];
      $configStore = await bridge.reloadConfig();
    } catch (err: any) { pushNotice('error', $t('notice.extraFolderAddFailed'), [errorText(err)]); }
  }

  async function removeExtraFolder(path: string) {
    try {
      await bridge.removeExtraFolder(path);
      extraFolders = (await bridge.listExtraFolders()) || [];
      $configStore = await bridge.reloadConfig();
    } catch (err: any) { pushNotice('error', $t('notice.extraFolderRemoveFailed'), [errorText(err)]); }
  }

  let nestedDepthError = '';
  async function saveNestedScanDepth() {
    const d = Math.max(1, Math.floor(nestedScanDepth || 1));
    nestedScanDepth = d;
    nestedDepthError = '';
    // Persist only. Deliberately no config reload, dashboard re-render, or disk
    // re-scan here — those would block this dialog. Re-scan explicitly afterward
    // via a container repo's ⋮ → "Re-scan for nested clones".
    try {
      await bridge.setNestedScanDepth(d);
    } catch (err: any) {
      nestedDepthError = err?.message || String(err);
    }
  }

  async function browseFolder() {
    const dir = await bridge.pickFolder('Choose root folder');
    if (dir) changeFolderPath = dir;
  }

  export function openChangeFolder() {
    changeFolderPath = '';
    changeFolderError = '';
    loadScanSettings();
    changeFolderModal = true;
  }

  async function confirmChangeFolder() {
    changeFolderError = '';
    if (!changeFolderPath.trim()) { changeFolderError = 'Folder path is required'; return; }
    try {
      await bridge.setGlobalFolder(changeFolderPath);
      changeFolderModal = false;
      await reloadFromDisk();
    } catch (err: any) {
      changeFolderError = err?.message || String(err);
    }
  }
</script>

{#if changeFolderModal}
  <div class="overlay" use:dismiss={() => changeFolderModal = false} transition:fade={{ duration: 120 }}>
    <div class="modal modal-account" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head">
        <h3>Change root folder</h3>
        <button class="btn-x" on:click={() => changeFolderModal = false}>&#10005;</button>
      </div>
      <div class="modal-body">
        <p class="delete-warning delete-danger"><strong>WARNING:</strong> Changing the root folder will <strong>not</strong> move existing clones. They will show as "Not local" until re-cloned at the new location (or moved manually).</p>
        <div class="form-row" style="margin-top: 12px;">
          <span class="form-label">Current</span>
          <span class="settings-value">{$configStore?.global?.folder || '(not set)'}</span>
        </div>
        {#if changeFolderError}
          <p class="form-error">{changeFolderError}</p>
        {/if}
        <div class="form-row">
          <label class="form-label" for="change-folder-path">New path</label>
          <input id="change-folder-path" class="form-input" bind:value={changeFolderPath} placeholder="~/new-folder" />
          <button class="settings-btn" on:click={browseFolder}>Browse</button>
        </div>

        <hr style="border:none;border-top:1px solid var(--border); margin:16px 0;" />

        <div class="form-row" style="flex-direction:column; align-items:stretch; gap:6px;">
          <span class="form-label">Extra scan folders</span>
          <p class="settings-value" style="margin:0 0 4px 0;">Additional roots scanned for clones and <code>.code-workspace</code> files, beyond the root folder.</p>
          {#each extraFolders || [] as ef}
            <div class="repo-row" style="justify-content:space-between;">
              <span class="workspace-file-path" title={ef}>{ef}</span>
              <button class="btn-sm btn-danger" on:click={() => removeExtraFolder(ef)} title="Remove">✕</button>
            </div>
          {/each}
          <button class="settings-btn" on:click={addExtraFolder} style="align-self:flex-start;">+ Add folder…</button>
        </div>

        <div class="form-row" style="margin-top:10px;">
          <label class="form-label" for="nested-scan-depth">Nested scan depth</label>
          <input id="nested-scan-depth" class="form-input" type="number" min="1" style="max-width:90px;"
            bind:value={nestedScanDepth} on:change={saveNestedScanDepth} />
          <span class="settings-value">levels below a container repo (default 1). Re-scan via a repo's ⋮ menu.</span>
        </div>
        {#if nestedDepthError}<p class="form-error">{nestedDepthError}</p>{/if}
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={() => changeFolderModal = false}>Close</button>
        <button class="btn-add" on:click={confirmChangeFolder} disabled={!changeFolderPath.trim()}>Change folder</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .repo-row {
    display: flex; align-items: center; gap: 10px;
    padding: 8px 6px; border-radius: 6px; transition: background 0.1s;
  }
  .repo-row:hover { background: var(--bg-row-hover); }
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
  .workspace-file-path {
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
    font-family: 'SF Mono', 'Cascadia Code', 'Consolas', monospace;
  }
</style>
