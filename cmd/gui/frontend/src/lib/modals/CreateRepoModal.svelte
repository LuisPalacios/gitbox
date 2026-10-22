<script lang="ts">
  // Create a new repository on the provider, optionally cloning it.
  import { fade, slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { accounts, configStore, repoStates } from '../stores';
  import { dismiss } from '../dismiss';
  import { t } from '../i18n';
  import Rich from '../Rich.svelte';

  let createRepoModal: string | null = null;
  let createRepoOrgs: string[] = [];
  let createRepoOwner = '';
  let createRepoName = '';
  let createRepoDesc = '';
  let createRepoPrivate = true;
  let createRepoClone = true;
  // The clone root (#149): asked only when there are two or more.
  let createRepoRoot = '';
  $: rootChoices = ($configStore?.global?.roots || []) as string[];
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
      createRepoNameError = $t('createRepo.nameStart');
    } else if (/[.-]$/.test(createRepoName)) {
      createRepoNameError = $t('createRepo.nameEnd');
    } else if (/\.\./.test(createRepoName)) {
      createRepoNameError = $t('createRepo.nameDots');
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
    createRepoRoot = $configStore?.sources?.[accountKey]?.root || $configStore?.global?.roots?.[0] || '';
    createRepoBusy = false;
    createRepoError = '';
    createRepoNameError = '';
    try {
      createRepoOrgs = await bridge.listAccountOrgs(accountKey);
      if (createRepoOrgs.length > 0) createRepoOwner = createRepoOrgs[0];
    } catch (e: any) {
      createRepoError = e?.message || $t('createRepo.loadOrgsFailed');
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
      await bridge.createNewRepo(acctKey, createRepoOwner, createRepoName, createRepoDesc, createRepoPrivate, willClone, rootChoices.length > 1 ? createRepoRoot : '');
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
      createRepoError = e?.message || $t('createRepo.failed');
    } finally {
      createRepoBusy = false;
    }
  }
</script>

{#if createRepoModal}
  <div class="overlay" use:dismiss={() => { if (!createRepoBusy) createRepoModal = null; }} transition:fade={{ duration: 120 }}>
    <div class="modal modal-account" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head">
        <h3>{$t('createRepo.title')} &mdash; {createRepoModal}</h3>
        <button class="btn-x" on:click={() => { if (!createRepoBusy) createRepoModal = null; }}>&#10005;</button>
      </div>
      <div class="modal-body">
        <p class="create-repo-warning"><Rich key="createRepo.intro" vars={{ provider: $accounts[createRepoModal]?.provider || $t('createRepo.provider'), url: $accounts[createRepoModal]?.url || '' }} /></p>
        {#if createRepoError}
          <p class="form-error">{createRepoError}</p>
        {/if}
        <div class="form-row">
          <label class="form-label" for="create-repo-owner">{$t('createRepo.owner')}</label>
          <select id="create-repo-owner" class="form-input" bind:value={createRepoOwner} disabled={createRepoBusy}>
            {#each createRepoOrgs as org}
              <option value={org}>{org}</option>
            {/each}
          </select>
        </div>
        <div class="form-row">
          <label class="form-label" for="create-repo-name">{$t('createRepo.name')}</label>
          <input id="create-repo-name" class="form-input" bind:value={createRepoName} on:input={onCreateRepoNameInput} placeholder={$t('createRepo.namePlaceholder')} disabled={createRepoBusy} />
          {#if createRepoNameError}
            <span class="form-error" style="margin: 2px 0 0; font-size: 10px;">{createRepoNameError}</span>
          {/if}
        </div>
        <div class="form-row">
          <label class="form-label" for="create-repo-desc">{$t('createRepo.description')}</label>
          <input id="create-repo-desc" class="form-input" bind:value={createRepoDesc} placeholder={$t('createRepo.descriptionPlaceholder')} disabled={createRepoBusy} />
        </div>
        <div class="form-row" style="gap: 12px;">
          <label style="font-size: 12px; color: var(--text); display: flex; align-items: center; gap: 4px;">
            <input type="checkbox" bind:checked={createRepoPrivate} disabled={createRepoBusy} /> {$t('createRepo.private')}
          </label>
          <label style="font-size: 12px; color: var(--text); display: flex; align-items: center; gap: 4px;">
            <input type="checkbox" bind:checked={createRepoClone} disabled={createRepoBusy} /> {$t('createRepo.cloneAfter')}
          </label>
        </div>
        {#if createRepoClone && rootChoices.length > 1}
          <div class="form-row">
            <label class="form-label" for="create-repo-root">{$t('roots.field')}</label>
            <select id="create-repo-root" class="form-input" bind:value={createRepoRoot} disabled={createRepoBusy}>
              {#each rootChoices as root}
                <option value={root}>{root}</option>
              {/each}
            </select>
          </div>
        {/if}
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={() => { if (!createRepoBusy) createRepoModal = null; }} disabled={createRepoBusy}>{$t('common.cancel')}</button>
        <button class="btn-add" on:click={submitCreateRepo}
          disabled={createRepoBusy || !createRepoOwner || !createRepoName || !!createRepoNameError}>
          {createRepoBusy ? $t('createRepo.creating') : createRepoClone ? $t('createRepo.createClone') : $t('common.create')}
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
