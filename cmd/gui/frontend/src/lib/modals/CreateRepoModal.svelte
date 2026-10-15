<script lang="ts">
  // Create a new repository on the provider, optionally cloning it.
  import { fade, slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { accounts, configStore, repoStates } from '../stores';
  import { dismiss } from '../dismiss';

  let createRepoModal: string | null = null;
  let createRepoOrgs: string[] = [];
  let createRepoOwner = '';
  let createRepoName = '';
  let createRepoDesc = '';
  let createRepoPrivate = true;
  let createRepoClone = true;
  let createRepoBusy = false;
  let createRepoError = '';
  let createRepoNameError = '';

  function sanitizeRepoName(raw: string): string {
    // Replace spaces with hyphens, strip everything except [a-zA-Z0-9._-]
    return raw.replace(/\s+/g, '-').replace(/[^a-zA-Z0-9._-]/g, '');
  }

  function onCreateRepoNameInput() {
    createRepoName = sanitizeRepoName(createRepoName);
    if (createRepoName.length === 0) {
      createRepoNameError = '';
    } else if (/^[.-]/.test(createRepoName)) {
      createRepoNameError = 'Name cannot start with a dot or hyphen';
    } else if (/[.-]$/.test(createRepoName)) {
      createRepoNameError = 'Name cannot end with a dot or hyphen';
    } else if (/\.\./.test(createRepoName)) {
      createRepoNameError = 'Name cannot contain consecutive dots';
    } else {
      createRepoNameError = '';
    }
  }

  export async function openCreateRepo(accountKey: string) {
    createRepoModal = accountKey;
    createRepoOrgs = [];
    createRepoOwner = '';
    createRepoName = '';
    createRepoDesc = '';
    createRepoPrivate = true;
    createRepoClone = true;
    createRepoBusy = false;
    createRepoError = '';
    createRepoNameError = '';
    try {
      createRepoOrgs = await bridge.listAccountOrgs(accountKey);
      if (createRepoOrgs.length > 0) createRepoOwner = createRepoOrgs[0];
    } catch (e: any) {
      createRepoError = e?.message || 'Failed to load organizations';
    }
  }

  async function submitCreateRepo() {
    if (!createRepoModal || !createRepoOwner || !createRepoName) return;
    createRepoBusy = true;
    createRepoError = '';
    try {
      const acctKey = createRepoModal;
      const repoKey = createRepoOwner + '/' + createRepoName;
      const willClone = createRepoClone;
      await bridge.createNewRepo(acctKey, createRepoOwner, createRepoName, createRepoDesc, createRepoPrivate, willClone);
      $configStore = await bridge.reloadConfig();
      // Pre-set status to 'cloning' so the repo doesn't flash as 'error'
      // before the async clone finishes.
      if (willClone) {
        repoStates.update((s) => ({
          ...s,
          [`${acctKey}/${repoKey}`]: { status: 'cloning', progress: 0, behind: 0, modified: 0, untracked: 0, ahead: 0 },
        }));
      }
      createRepoModal = null;
    } catch (e: any) {
      createRepoError = e?.message || 'Failed to create repository';
    } finally {
      createRepoBusy = false;
    }
  }
</script>

{#if createRepoModal}
  <div class="overlay" use:dismiss={() => { if (!createRepoBusy) createRepoModal = null; }} transition:fade={{ duration: 120 }}>
    <div class="modal modal-account" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head">
        <h3>Create repository &mdash; {createRepoModal}</h3>
        <button class="btn-x" on:click={() => { if (!createRepoBusy) createRepoModal = null; }}>&#10005;</button>
      </div>
      <div class="modal-body">
        <p class="create-repo-warning">This will create a <strong>new</strong> repository in your <strong>{$accounts[createRepoModal]?.provider || 'provider'}</strong> account ({$accounts[createRepoModal]?.url || ''})</p>
        {#if createRepoError}
          <p class="form-error">{createRepoError}</p>
        {/if}
        <div class="form-row">
          <label class="form-label" for="create-repo-owner">Owner</label>
          <select id="create-repo-owner" class="form-input" bind:value={createRepoOwner} disabled={createRepoBusy}>
            {#each createRepoOrgs as org}
              <option value={org}>{org}</option>
            {/each}
          </select>
        </div>
        <div class="form-row">
          <label class="form-label" for="create-repo-name">Name</label>
          <input id="create-repo-name" class="form-input" bind:value={createRepoName} on:input={onCreateRepoNameInput} placeholder="my-new-repo" disabled={createRepoBusy} />
          {#if createRepoNameError}
            <span class="form-error" style="margin: 2px 0 0; font-size: 10px;">{createRepoNameError}</span>
          {/if}
        </div>
        <div class="form-row">
          <label class="form-label" for="create-repo-desc">Description</label>
          <input id="create-repo-desc" class="form-input" bind:value={createRepoDesc} placeholder="Short description (optional)" disabled={createRepoBusy} />
        </div>
        <div class="form-row" style="gap: 12px;">
          <label style="font-size: 12px; color: var(--text); display: flex; align-items: center; gap: 4px;">
            <input type="checkbox" bind:checked={createRepoPrivate} disabled={createRepoBusy} /> Private
          </label>
          <label style="font-size: 12px; color: var(--text); display: flex; align-items: center; gap: 4px;">
            <input type="checkbox" bind:checked={createRepoClone} disabled={createRepoBusy} /> Clone after creating
          </label>
        </div>
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={() => { if (!createRepoBusy) createRepoModal = null; }} disabled={createRepoBusy}>Cancel</button>
        <button class="btn-add" on:click={submitCreateRepo}
          disabled={createRepoBusy || !createRepoOwner || !createRepoName || !!createRepoNameError}>
          {createRepoBusy ? 'Creating...' : createRepoClone ? 'Create & Clone' : 'Create'}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  .create-repo-warning {
    font-size: 12px; color: #F07623; background: rgba(240, 118, 35, 0.1);
    border: 1px solid rgba(240, 118, 35, 0.3); border-radius: 4px;
    padding: 8px 10px; margin: 0 0 12px;
  }
  :global([data-theme="light"]) .create-repo-warning {
    color: #c2410c; background: #fff7ed; border-color: rgba(194, 65, 12, 0.3);
  }
</style>
