<script lang="ts">
  // Edit an account's key, provider, URL, username and commit identity.
  import { fade, slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { accounts } from '../stores';
  import { dismiss } from '../dismiss';

  export let reloadFromDisk: () => Promise<void>;

  let editAccountModal: string | null = null;
  let editAccountError = '';
  let editAcct = { key: '', provider: '', url: '', username: '', name: '', email: '' };

  export function openEditAccount(key: string) {
    const acct = $accounts[key];
    if (!acct) return;
    editAcct = {
      key,
      provider: acct.provider || '',
      url: acct.url || '',
      username: acct.username || '',
      name: acct.name || '',
      email: acct.email || '',
    };
    editAccountError = '';
    editAccountModal = key;
  }

  async function submitEditAccount() {
    if (!editAccountModal) return;
    editAccountError = '';
    const newKey = editAcct.key.trim();
    if (!newKey) { editAccountError = 'Account key is required'; return; }
    if (!/^[a-zA-Z0-9][a-zA-Z0-9-]*$/.test(newKey)) { editAccountError = 'Key: letters, numbers, hyphens only'; return; }
    if (!editAcct.url.trim()) { editAccountError = 'URL is required'; return; }
    if (!editAcct.username.trim()) { editAccountError = 'Username is required'; return; }
    try {
      const oldKey = editAccountModal;
      const keyChanged = newKey !== oldKey;
      if (keyChanged) {
        await bridge.renameAccount(oldKey, newKey);
      }
      await bridge.updateAccount({ ...editAcct, key: newKey });
      await reloadFromDisk();
      editAccountModal = null;
    } catch (e: any) {
      editAccountError = e?.message || String(e);
    }
  }
</script>

{#if editAccountModal}
  <div class="overlay" use:dismiss={() => editAccountModal = null} transition:fade={{ duration: 120 }}>
    <div class="modal modal-account" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head">
        <h3>Edit account</h3>
        <button class="btn-x" on:click={() => editAccountModal = null}>&#10005;</button>
      </div>
      <div class="modal-body">
        {#if editAccountError}
          <p class="form-error">{editAccountError}</p>
        {/if}
        <div class="form-row">
          <label class="form-label" for="ea-key">Account key</label>
          <input class="form-input" id="ea-key" bind:value={editAcct.key} placeholder="account-key" />
        </div>
        <div class="form-row">
          <label class="form-label" for="ea-provider">Provider</label>
          <select class="form-input" id="ea-provider" bind:value={editAcct.provider}>
            <option value="github">GitHub</option>
            <option value="gitlab">GitLab</option>
            <option value="gitea">Gitea</option>
            <option value="forgejo">Forgejo</option>
            <option value="bitbucket">Bitbucket</option>
          </select>
        </div>
        <div class="form-row">
          <label class="form-label" for="ea-url">URL</label>
          <input class="form-input" id="ea-url" bind:value={editAcct.url} />
        </div>
        <div class="form-row">
          <label class="form-label" for="ea-user">Git username</label>
          <input class="form-input" id="ea-user" bind:value={editAcct.username} />
          <p class="form-hint">The username you use to log in to the provider (for git and API operations).</p>
        </div>
        <fieldset class="form-fieldset">
          <legend class="form-fieldset-legend">Git commit author identity</legend>
          <p class="form-fieldset-hint">Used only as the author of commits you make under this account (<code>git config user.name</code> / <code>user.email</code>). Not used for authentication.</p>
          <div class="form-row">
            <label class="form-label" for="ea-name">Name</label>
            <input class="form-input" id="ea-name" bind:value={editAcct.name} />
          </div>
          <div class="form-row">
            <label class="form-label" for="ea-email">Email</label>
            <input class="form-input" id="ea-email" bind:value={editAcct.email} />
          </div>
        </fieldset>
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={() => editAccountModal = null}>Cancel</button>
        <button class="btn-add" on:click={submitEditAccount}>Save</button>
      </div>
    </div>
  </div>
{/if}

<style>

</style>
