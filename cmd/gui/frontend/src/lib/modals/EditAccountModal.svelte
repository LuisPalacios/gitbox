<script lang="ts">
  // Edit an account's key, provider, URL, username and commit identity.
  import { fade, slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { accounts } from '../stores';
  import { dismiss } from '../dismiss';
  import CredentialSteps from '../CredentialSteps.svelte';
  import type { CredentialStepReport } from '../types';
  import { ro } from '../ro';
  import Rich from '../Rich.svelte';
  import { t } from '../i18n';

  export let reloadFromDisk: () => Promise<void>;

  let editAccountModal: string | null = null;
  let editAccountError = '';
  let report: CredentialStepReport | null = null;
  let busy = false;
  let editAcct = { key: '', provider: '', url: '', username: '', name: '', email: '' };

  export function openEditAccount(key: string) {
    const acct = $accounts[key];
    if (!acct) return;
    editAcct = {
      key: acct.credential_transition?.rename_to || key,
      provider: acct.provider || '',
      url: acct.url || '',
      username: acct.username || '',
      name: acct.name || '',
      email: acct.email || '',
    };
    editAccountError = '';
    report = null;
    editAccountModal = key;
  }

  async function submitEditAccount() {
    if (!editAccountModal || busy) return;
    editAccountError = '';
    const newKey = editAcct.key.trim();
    if (!newKey) { editAccountError = $t('accountForm.keyRequired'); return; }
    if (!/^[a-zA-Z0-9][a-zA-Z0-9-]*$/.test(newKey)) { editAccountError = $t('accountForm.keyFormat'); return; }
    if (!editAcct.url.trim()) { editAccountError = $t('accountForm.urlRequired'); return; }
    if (!editAcct.username.trim()) { editAccountError = $t('accountForm.usernameRequired'); return; }
    busy = true;
    try {
      const pending = $accounts[editAccountModal]?.credential_transition;
      const oldKey = pending?.previous_key || editAccountModal;
      if (newKey !== oldKey) {
        report = await bridge.renameAccount(oldKey, newKey);
        if (report.steps.some(step => step.name === 'save' && step.ok)) editAccountModal = newKey;
        await reloadFromDisk();
        if (!report.ok) return;
        editAccountModal = newKey;
      }
      await bridge.updateAccount({ ...editAcct, key: newKey });
      await reloadFromDisk();
      editAccountModal = null;
    } catch (e: any) {
      editAccountError = e?.message || String(e);
    } finally {
      busy = false;
    }
  }
</script>

{#if editAccountModal}
  <div class="overlay" use:dismiss={() => { if (!busy) editAccountModal = null; }} transition:fade={{ duration: 120 }}>
    <div class="modal modal-account" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head">
        <h3>{$t('account.edit')}</h3>
        <button class="btn-x" on:click={() => { if (!busy) editAccountModal = null; }}>&#10005;</button>
      </div>
      <div class="modal-body">
        {#if report}
          <CredentialSteps {report} />
        {/if}
        {#if editAccountError}
          <p class="form-error">{editAccountError}</p>
        {/if}
        <div class="form-row">
          <label class="form-label" for="ea-key">{$t('accountForm.key')}</label>
          <input class="form-input" id="ea-key" bind:value={editAcct.key} disabled={busy || !!$accounts[editAccountModal]?.credential_transition} placeholder={$t('editAccount.keyPlaceholder')} />
        </div>
        <div class="form-row">
          <label class="form-label" for="ea-provider">{$t('accountForm.provider')}</label>
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
          <label class="form-label" for="ea-user">{$t('accountForm.username')}</label>
          <input class="form-input" id="ea-user" bind:value={editAcct.username} />
          <p class="form-hint">{$t('accountForm.usernameHint')}</p>
        </div>
        <fieldset class="form-fieldset">
          <legend class="form-fieldset-legend">{$t('accountForm.identity')}</legend>
          <p class="form-fieldset-hint"><Rich key="editAccount.identityHint" /></p>
          <div class="form-row">
            <label class="form-label" for="ea-name">{$t('accountForm.name')}</label>
            <input class="form-input" id="ea-name" bind:value={editAcct.name} />
          </div>
          <div class="form-row">
            <label class="form-label" for="ea-email">{$t('onboard.email')}</label>
            <input class="form-input" id="ea-email" bind:value={editAcct.email} />
          </div>
        </fieldset>
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={() => { if (!busy) editAccountModal = null; }}>{$t('common.cancel')}</button>
        <button use:ro={['renameAccount', 'updateAccount']} class="btn-add" on:click={submitEditAccount} disabled={busy}>{report && !report.ok || ($accounts[editAccountModal]?.credential_transition?.previous_key || $accounts[editAccountModal]?.credential_transition?.rename_to) ? $t('cred.transitionRetry') : $t('common.save')}</button>
      </div>
    </div>
  </div>
{/if}

<style>

</style>
