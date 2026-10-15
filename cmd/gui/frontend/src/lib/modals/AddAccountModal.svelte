<script lang="ts">
  // Advanced "Add account": the account form, then its credential setup.
  // The credential state is shared with Change credential and lives in App.
  import { fade, slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import type { DoctorPrecheckDTO } from '../bridge';
  import { providerURLs } from '../theme';
  import { dismiss } from '../dismiss';
  import { BrowserOpenURL } from '../../../wailsjs/runtime/runtime';
  import CredentialResult from '../CredentialResult.svelte';
  import PrecheckBanner from '../PrecheckBanner.svelte';

  // Shared credential operation state (App).
  export let credResult: { ok: boolean; message: string; needsPAT?: boolean; sshPublicKey?: string; sshAddURL?: string; sshVerified?: boolean } | null;
  export let credTokenInput: string;
  export let credPrecheck: DoctorPrecheckDTO | null;
  export let credTokenGuide: string;
  export let credTokenScopes: string;
  export let sshKeyCopied: boolean;
  export let copySSHKey: (text: string) => void;
  export let resetCredState: () => void;
  export let verifyAllCredentials: () => void;
  export let reloadFromDisk: () => Promise<void>;
  export let runCredentialPrecheck: (credType: string) => Promise<void>;
  export let fetchTokenGuide: (accountKey: string) => Promise<void>;
  export let verifySSHConnection: (accountKey: string) => Promise<void>;
  export let regenerateSSHKey: (accountKey: string) => Promise<void>;
  export let credBusy: boolean;
  export let runCredSetup: (accountKey: string, credType: string) => Promise<void>;

  let addAccountModal = false;
  let addAccountError = '';
  let addAccountStep: 'form' | 'credential' = 'form';
  let addAcct = { key: '', provider: 'github', url: 'https://github.com', username: '', name: '', email: '', defaultBranch: 'main', credentialType: 'gcm' };

  export function open() {
    addAccountModal = true;
  }

  function onProviderChange() {
    const defaultURL = providerURLs[addAcct.provider];
    if (defaultURL !== undefined) addAcct.url = defaultURL;
  }

  function resetAddAccount() {
    addAcct = { key: '', provider: 'github', url: 'https://github.com', username: '', name: '', email: '', defaultBranch: 'main', credentialType: 'gcm' };
    addAccountError = '';
    addAccountStep = 'form';
    addAccountModal = false;
    resetCredState();
    verifyAllCredentials();
  }

  async function submitAddAccount() {
    addAccountError = '';
    if (!addAcct.key.trim()) { addAccountError = 'Account key is required'; return; }
    if (!addAcct.provider) { addAccountError = 'Provider is required'; return; }
    if (!addAcct.url.trim()) { addAccountError = 'URL is required'; return; }
    if (!addAcct.username.trim()) { addAccountError = 'Username is required'; return; }
    try {
      await bridge.addAccount(addAcct);
      await reloadFromDisk();
      addAccountStep = 'credential';
      credResult = null;
      await runCredentialPrecheck(addAcct.credentialType);
      if (credPrecheck && !credPrecheck.ok) {
        // Don't auto-run setup — let the user install the missing tools first.
        return;
      }
      if (addAcct.credentialType === 'gcm' || addAcct.credentialType === 'ssh') {
        runCredSetup(addAcct.key, addAcct.credentialType);
      } else if (addAcct.credentialType === 'token') {
        await fetchTokenGuide(addAcct.key);
      }
    } catch (err: any) {
      addAccountError = err?.message || String(err);
    }
  }
</script>

{#if addAccountModal}
  <div class="overlay" use:dismiss={() => { if (addAccountStep === 'form') resetAddAccount(); }} transition:fade={{ duration: 120 }}>
    <div class="modal modal-account" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head">
        <h3>{addAccountStep === 'form' ? 'Add account' : 'Credential setup'}</h3>
        <button class="btn-x" on:click={resetAddAccount}>&#10005;</button>
      </div>
      <div class="modal-body">
        {#if addAccountStep === 'form'}
          <!-- ── Step 1: Account details ── -->
          {#if addAccountError}
            <p class="form-error">{addAccountError}</p>
          {/if}
          <div class="form-row">
            <label class="form-label" for="aa-key">Account key</label>
            <input class="form-input" id="aa-key" bind:value={addAcct.key} placeholder="github-MyUser" />
          </div>
          <div class="form-row">
            <label class="form-label" for="aa-provider">Provider</label>
            <select class="form-input" id="aa-provider" bind:value={addAcct.provider} on:change={onProviderChange}>
              <option value="github">GitHub</option>
              <option value="gitlab">GitLab</option>
              <option value="gitea">Gitea</option>
              <option value="forgejo">Forgejo</option>
              <option value="bitbucket">Bitbucket</option>
            </select>
          </div>
          <div class="form-row">
            <label class="form-label" for="aa-url">URL</label>
            <input class="form-input" id="aa-url" bind:value={addAcct.url} placeholder="https://github.com" />
          </div>
          <div class="form-row">
            <label class="form-label" for="aa-user">Git username</label>
            <input class="form-input" id="aa-user" bind:value={addAcct.username} placeholder="MyUser" />
            <p class="form-hint">The username you use to log in to the provider (for git and API operations).</p>
          </div>
          <fieldset class="form-fieldset">
            <legend class="form-fieldset-legend">Git commit author identity</legend>
            <p class="form-fieldset-hint">Used only as the author of commits you make under this account. Not used for authentication.</p>
            <div class="form-row">
              <label class="form-label" for="aa-name">Name</label>
              <input class="form-input" id="aa-name" bind:value={addAcct.name} placeholder="Full Name" />
            </div>
            <div class="form-row">
              <label class="form-label" for="aa-email">Email</label>
              <input class="form-input" id="aa-email" bind:value={addAcct.email} placeholder="user@example.com" />
            </div>
          </fieldset>
          <div class="form-row">
            <label class="form-label" for="aa-cred">Credential type</label>
            <select class="form-input" id="aa-cred" bind:value={addAcct.credentialType}>
              <option value="gcm">GCM</option>
              <option value="ssh">SSH</option>
              <option value="token">Token</option>
            </select>
          </div>
        {:else}
          <!-- ── Step 2: Credential setup ── -->
          <p class="cred-step-intro">Account <strong>{addAcct.key}</strong> created. Set up credentials:</p>

          {#if credPrecheck && !credPrecheck.ok}
            <PrecheckBanner precheck={credPrecheck} noteTail=", or proceed anyway to retry manually."
              onRecheck={() => runCredentialPrecheck(addAcct.credentialType)} />
          {/if}

          {#if addAcct.credentialType === 'gcm'}
            <p class="cred-step-desc">Click below to authenticate via browser (Git Credential Manager).</p>
            <button class="btn-add cred-action-btn" on:click={() => runCredSetup(addAcct.key, addAcct.credentialType)} disabled={credBusy || credResult?.ok}>
              {credBusy ? 'Authenticating...' : credResult?.ok ? 'Authenticated' : 'Authenticate with GCM'}
            </button>

          {:else if addAcct.credentialType === 'token'}
            <p class="cred-step-desc">Click on the link and login if necessary. Create a Token and paste it below.</p>
            {#if credTokenScopes}
              <p class="cred-step-desc"><strong>Required scopes:</strong> {credTokenScopes}</p>
            {/if}
            {#if credTokenGuide}
              <p class="cred-step-link"><a href={credTokenGuide} on:click|preventDefault={() => BrowserOpenURL(credTokenGuide)}>{credTokenGuide}</a></p>
            {/if}
            <div class="form-row">
              <label class="form-label" for="aa-token">Token</label>
              <input class="form-input" id="aa-token" type="password" bind:value={credTokenInput} placeholder="ghp_..." />
            </div>
            <button class="btn-add cred-action-btn" on:click={() => runCredSetup(addAcct.key, addAcct.credentialType)} disabled={credBusy || !credTokenInput.trim() || credResult?.ok}>
              {credBusy ? 'Storing...' : credResult?.ok ? 'Token stored' : 'Store token'}
            </button>

          {:else if addAcct.credentialType === 'ssh'}
            <p class="cred-step-desc">SSH Host and Key configuration.</p>
          {/if}

          {#if credResult}
            <CredentialResult {credResult} {credBusy} {sshKeyCopied} {copySSHKey}
              verifySSHConnection={() => verifySSHConnection(addAcct.key)}
              regenerateSSHKey={() => regenerateSSHKey(addAcct.key)} />
          {:else if addAcct.credentialType === 'ssh'}
            <button class="btn-add cred-action-btn" on:click={() => runCredSetup(addAcct.key, addAcct.credentialType)} disabled={credBusy}>
              {credBusy ? 'Setting up SSH...' : 'Generate SSH key'}
            </button>
          {/if}

        {/if}
      </div>
      <div class="modal-foot">
        {#if addAccountStep === 'form'}
          <button class="btn-cancel" on:click={resetAddAccount}>Cancel</button>
          <button class="btn-add" on:click={submitAddAccount}>Add account</button>
        {:else}
          <button class="btn-cancel" on:click={resetAddAccount}>
            {credResult?.ok && (!credResult?.sshPublicKey || credResult?.sshVerified) ? 'Done' : 'Cancel'}
          </button>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  /* ── Credential setup step ── */
  .cred-step-intro { font-size: 13px; color: var(--text-primary); margin: 0 0 10px; }
</style>
