<script lang="ts">
  // An SSH account needs a PAT for API discovery: store one, then rerun Find
  // projects.
  import { fade, slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { t } from '../i18n';
  import { BrowserOpenURL } from '../../../wailsjs/runtime/runtime';

  export let openDiscover: (accountKey: string) => void;

  let sshDiscoverTokenModal: string | null = null; // account key needing a PAT for discovery
  let sshDiscoveryTokenInput = '';
  let sshDiscoveryGuide = '';
  let sshDiscoveryScopes = '';
  let sshDiscoveryBusy = false;

  // Discovery failed for lack of an API token: show the modal and fetch the
  // token guide for this account.
  export function openFor(acctKey: string) {
    sshDiscoverTokenModal = acctKey;
    // Fetch the guide for this account.
    bridge.getTokenGuide(acctKey).then(guide => {
      sshDiscoveryGuide = guide.creationURL || '';
      sshDiscoveryScopes = guide.scopes || '';
    });
  }

  export function close() {
    sshDiscoverTokenModal = null;
  }

  // Part of the shared credential reset (App's resetCredState).
  export function resetFields() {
    sshDiscoveryTokenInput = '';
    sshDiscoveryGuide = '';
    sshDiscoveryScopes = '';
    sshDiscoveryBusy = false;
  }

  // Store SSH discovery token and then open discovery.
  async function storeSSHDiscoveryToken() {
    const key = sshDiscoverTokenModal;
    if (!key) return;
    sshDiscoveryBusy = true;
    try {
      await bridge.credentialStoreToken(key, sshDiscoveryTokenInput);
      sshDiscoveryTokenInput = '';
      sshDiscoverTokenModal = null;
      // Now open discovery with the stored token.
      openDiscover(key);
    } catch (err: any) {
      // Keep modal open so user can retry.
    }
    sshDiscoveryBusy = false;
  }
</script>

{#if sshDiscoverTokenModal}
  <div class="overlay" transition:fade={{ duration: 120 }}>
    <div class="modal modal-account" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head">
        <h3>Discovery token &mdash; {sshDiscoverTokenModal}</h3>
        <button class="btn-x" on:click={() => { sshDiscoverTokenModal = null; sshDiscoveryTokenInput = ''; }}>&#10005;</button>
      </div>
      <div class="modal-body">
        <p class="cred-step-desc">{$t('discover.sshNeedsToken')}<br/>{$t('discover.sshNeedsTokenAction')}</p>
        <p class="cred-step-desc">Otherwise you can configure your sources manually in the JSON config file.</p>
        {#if sshDiscoveryScopes}
          <p class="cred-step-desc" style="margin-top: 6px;"><strong>Required scopes:</strong> {sshDiscoveryScopes}</p>
        {/if}
        {#if sshDiscoveryGuide}
          <p class="cred-step-link" style="margin-top: 4px;"><a href={sshDiscoveryGuide} on:click|preventDefault={() => BrowserOpenURL(sshDiscoveryGuide)}>{sshDiscoveryGuide}</a></p>
        {/if}
        <div class="form-row" style="margin-top: 10px;">
          <label class="form-label" for="ssh-disc-token">Token</label>
          <input class="form-input" id="ssh-disc-token" type="password" bind:value={sshDiscoveryTokenInput} placeholder="Token..." />
        </div>
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={() => { sshDiscoverTokenModal = null; sshDiscoveryTokenInput = ''; }}>Cancel</button>
        <button class="btn-add" on:click={storeSSHDiscoveryToken} disabled={sshDiscoveryBusy || !sshDiscoveryTokenInput.trim()}>
          {sshDiscoveryBusy ? 'Storing...' : 'Store & discover'}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>

</style>
