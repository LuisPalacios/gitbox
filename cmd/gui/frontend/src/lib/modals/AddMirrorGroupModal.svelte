<script lang="ts">
  // Create a mirror group: a source and a destination account.
  import { fade, slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { pushNotice, errorText } from '../notices';
  import { t } from '../i18n';
  import { dismiss } from '../dismiss';
  import { accounts, configStore } from '../stores';

  let addMirrorGroupModal = false;
  // Add mirror group form
  let newMirrorKey = '';
  let newMirrorSrc = '';
  let newMirrorDst = '';

  export function open() {
    addMirrorGroupModal = true; newMirrorKey = ''; newMirrorSrc = ''; newMirrorDst = '';
  }

  async function submitAddMirrorGroup() {
    if (!newMirrorKey || !newMirrorSrc || !newMirrorDst) return;
    try {
      await bridge.addMirrorGroup(newMirrorKey, newMirrorSrc, newMirrorDst);
      $configStore = await bridge.reloadConfig();
      addMirrorGroupModal = false;
      newMirrorKey = ''; newMirrorSrc = ''; newMirrorDst = '';
    } catch (e: any) {
      pushNotice('error', $t('notice.mirrorGroupAddFailed'), [errorText(e)]);
    }
  }
</script>

{#if addMirrorGroupModal}
  <div class="overlay" use:dismiss={() => addMirrorGroupModal = false} transition:fade={{ duration: 120 }}>
    <div class="modal modal-account" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head">
        <h3>Add mirror group</h3>
        <button class="btn-x" on:click={() => addMirrorGroupModal = false}>&#10005;</button>
      </div>
      <div class="modal-body">
        <div class="form-row">
          <label class="form-label" for="mirror-group-src">Source account</label>
          <select id="mirror-group-src" class="form-input" bind:value={newMirrorSrc} on:change={() => { if (newMirrorSrc && newMirrorDst) newMirrorKey = newMirrorSrc + '-' + newMirrorDst; }}>
            <option value="">Select...</option>
            {#each Object.keys($accounts) as acctKey}
              <option value={acctKey}>{acctKey}</option>
            {/each}
          </select>
        </div>
        <div class="form-row">
          <label class="form-label" for="mirror-group-dst">Destination account</label>
          <select id="mirror-group-dst" class="form-input" bind:value={newMirrorDst} on:change={() => { if (newMirrorSrc && newMirrorDst) newMirrorKey = newMirrorSrc + '-' + newMirrorDst; }}>
            <option value="">Select...</option>
            {#each Object.keys($accounts).filter(k => k !== newMirrorSrc) as acctKey}
              <option value={acctKey}>{acctKey}</option>
            {/each}
          </select>
        </div>
        <div class="form-row">
          <label class="form-label" for="mirror-group-key">Mirror key</label>
          <input id="mirror-group-key" class="form-input" bind:value={newMirrorKey} placeholder="e.g. forgejo-github" />
        </div>
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={() => addMirrorGroupModal = false}>Cancel</button>
        <button class="btn-add" on:click={submitAddMirrorGroup} disabled={!newMirrorKey || !newMirrorSrc || !newMirrorDst}>Create</button>
      </div>
    </div>
  </div>
{/if}

<style>

</style>
