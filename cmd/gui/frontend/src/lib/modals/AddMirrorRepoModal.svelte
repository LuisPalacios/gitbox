<script lang="ts">
  // Add a repo to a mirror group, picked from the origin account's repos.
  import { fade, slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { pushNotice, errorText } from '../notices';
  import { t } from '../i18n';
  import { dismiss } from '../dismiss';
  import { configStore, mirrors } from '../stores';

  export let setupMirrorRepo: (mirrorKey: string, repoKey: string) => Promise<void>;

  let addMirrorRepoModal: string | null = null; // holds mirrorKey
  // Add mirror repo form
  let newMirrorRepoKey = '';
  let newMirrorRepoDirection = 'push';
  let newMirrorRepoOrigin = 'src';
  let newMirrorRepoAutoSetup = false;
  let mirrorRepoPickerRepos: { fullName: string; description: string; private: boolean; fork: boolean; archived: boolean }[] = [];
  let mirrorRepoPickerLoading = false;
  let mirrorRepoPickerFilter = '';
  let mirrorRepoPickerLoaded = false;

  export function open(mirrorKey: string) {
    addMirrorRepoModal = mirrorKey; newMirrorRepoKey = ''; newMirrorRepoDirection = 'push'; newMirrorRepoOrigin = 'src'; newMirrorRepoAutoSetup = false; mirrorRepoPickerRepos = []; mirrorRepoPickerLoaded = false; mirrorRepoPickerFilter = '';
  }

  async function loadMirrorRepoList() {
    if (!addMirrorRepoModal) return;
    const mir = $mirrors[addMirrorRepoModal];
    if (!mir) return;
    const acctKey = newMirrorRepoOrigin === 'src' ? mir.account_src : mir.account_dst;
    mirrorRepoPickerLoading = true;
    mirrorRepoPickerFilter = '';
    mirrorRepoPickerRepos = [];
    mirrorRepoPickerLoaded = false;
    try {
      const repos = await bridge.listRemoteRepos(acctKey);
      mirrorRepoPickerRepos = (repos || []).sort((a: any, b: any) => a.fullName.localeCompare(b.fullName));
      mirrorRepoPickerLoaded = true;
    } catch {
      mirrorRepoPickerRepos = [];
    }
    mirrorRepoPickerLoading = false;
  }

  async function submitAddMirrorRepo() {
    if (!addMirrorRepoModal || !newMirrorRepoKey) return;
    try {
      await bridge.addMirrorRepo(addMirrorRepoModal, newMirrorRepoKey, newMirrorRepoDirection, newMirrorRepoOrigin);
      $configStore = await bridge.reloadConfig();
      const mirrorKey = addMirrorRepoModal;
      const repoKey = newMirrorRepoKey;
      addMirrorRepoModal = null;
      newMirrorRepoKey = ''; newMirrorRepoDirection = 'push'; newMirrorRepoOrigin = 'src';
      if (newMirrorRepoAutoSetup) {
        newMirrorRepoAutoSetup = false;
        await setupMirrorRepo(mirrorKey, repoKey);
      }
    } catch (e: any) {
      pushNotice('error', $t('notice.mirrorRepoAddFailed'), [errorText(e)]);
    }
  }
</script>

{#if addMirrorRepoModal}
  {@const mir = $mirrors[addMirrorRepoModal]}
  {@const filteredPickerRepos = mirrorRepoPickerRepos.filter(r => !mirrorRepoPickerFilter || r.fullName.toLowerCase().includes(mirrorRepoPickerFilter.toLowerCase()))}
  <div class="overlay" use:dismiss={() => addMirrorRepoModal = null} transition:fade={{ duration: 120 }}>
    <div class="modal modal-mirror-repo" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head">
        <h3>{$t('mirrorRepo.title', { key: addMirrorRepoModal })}</h3>
        <button class="btn-x" on:click={() => addMirrorRepoModal = null}>&#10005;</button>
      </div>
      <div class="modal-body">
        <div class="mirror-form-grid">
          <span class="mirror-form-label">{$t('mirrorRepo.direction')}</span>
          <div class="radio-group">
            <label><input type="radio" bind:group={newMirrorRepoDirection} value="push" /> {$t('mirrorRepo.push')}</label>
            <label><input type="radio" bind:group={newMirrorRepoDirection} value="pull" /> {$t('mirrorRepo.pull')}</label>
          </div>

          <span class="mirror-form-label">{$t('mirrorRepo.origin')}</span>
          <div class="radio-group">
            <label><input type="radio" bind:group={newMirrorRepoOrigin} value="src" on:change={loadMirrorRepoList} /> {mir?.account_src || 'src'}</label>
            <label><input type="radio" bind:group={newMirrorRepoOrigin} value="dst" on:change={loadMirrorRepoList} /> {mir?.account_dst || 'dst'}</label>
          </div>

          <span class="mirror-form-label">{$t('mirrorRepo.repository')}</span>
          <div>
            {#if mirrorRepoPickerLoading}
              <div class="loading"><div class="spinner"></div><span>{$t('mirrorRepo.loading')}</span></div>
            {:else if mirrorRepoPickerLoaded}
              <input class="form-input form-input-sm" bind:value={mirrorRepoPickerFilter} placeholder={$t('mirrorRepo.filter')} />
              <div class="mirror-repo-picker">
                {#each filteredPickerRepos as repo}
                  {@const alreadyMirrored = mir && mir.repos[repo.fullName]}
                  <label class="mrp-row" class:mrp-disabled={!!alreadyMirrored} class:mrp-selected={newMirrorRepoKey === repo.fullName}>
                    <input type="radio" bind:group={newMirrorRepoKey} value={repo.fullName} disabled={!!alreadyMirrored} hidden />
                    <span class="mrp-name">{repo.fullName}</span>
                    <span class="mrp-badges">
                      {#if repo.private}<span class="mrp-badge">{$t('onboard.private')}</span>{/if}
                      {#if repo.fork}<span class="mrp-badge">{$t('onboard.fork')}</span>{/if}
                      {#if repo.archived}<span class="mrp-badge">{$t('onboard.archived')}</span>{/if}
                      {#if alreadyMirrored}<span class="mrp-badge mrp-badge-dim">{$t('mirrorRepo.mirrored')}</span>{/if}
                    </span>
                  </label>
                {/each}
                {#if filteredPickerRepos.length === 0}
                  <p class="mrp-empty">{$t('mirrorRepo.noMatch')}</p>
                {/if}
              </div>
            {:else}
              <button class="btn-sm" on:click={loadMirrorRepoList}>{$t('mirrorRepo.load', { account: (newMirrorRepoOrigin === 'src' ? mir?.account_src : mir?.account_dst) ?? '' })}</button>
            {/if}
          </div>

          <span class="mirror-form-label">{$t('mirrorRepo.options')}</span>
          <label style="font-size: 12px; color: var(--text); display: flex; align-items: center; gap: 4px;"><input type="checkbox" bind:checked={newMirrorRepoAutoSetup} /> {$t('mirrorRepo.autoSetup')}</label>
        </div>
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={() => addMirrorRepoModal = null}>{$t('common.cancel')}</button>
        <button class="btn-add" on:click={submitAddMirrorRepo} disabled={!newMirrorRepoKey}>{$t('common.add')}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .radio-group { display: flex; flex-direction: column; gap: 4px; }
  .radio-group label { font-size: 12px; color: var(--text); display: flex; align-items: center; gap: 4px; cursor: pointer; }
  /* Mirror repo modal */
  .modal-mirror-repo { max-width: 560px; width: 90vw; }
  .mirror-form-grid {
    display: grid; grid-template-columns: 160px 1fr; gap: 10px 12px; align-items: start;
  }
  .mirror-form-label {
    font-size: 12px; font-weight: 500; color: var(--text-muted);
    padding-top: 3px; white-space: nowrap;
  }
  .form-input-sm { font-size: 11px; padding: 4px 8px; margin-bottom: 6px; }
  .mirror-repo-picker {
    max-height: 220px; overflow-y: auto; border: 1px solid var(--border); border-radius: 4px;
  }
  .mrp-selected { background: var(--hover); outline: 1px solid var(--text-primary); outline-offset: -1px; }
  .mrp-disabled { opacity: 0.4; cursor: default; }
  .mrp-badges { display: flex; align-items: center; gap: 4px; flex-shrink: 0; }
  .mrp-empty { color: var(--text-muted); font-size: 11px; padding: 8px; margin: 0; }
</style>
