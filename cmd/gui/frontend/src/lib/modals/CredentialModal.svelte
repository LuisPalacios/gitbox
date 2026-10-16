<script lang="ts">
  // Change credential (and the API-token-only variant): current status,
  // type switch, setup and delete. The credential state is shared with Add
  // account and lives in App.
  import { fade, slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import type { DoctorPrecheckDTO } from '../bridge';
  import { accounts } from '../stores';
  import { dismiss } from '../dismiss';
  import { BrowserOpenURL } from '../../../wailsjs/runtime/runtime';
  import CredentialResult from '../CredentialResult.svelte';
  import PrecheckBanner from '../PrecheckBanner.svelte';
  import Rich from '../Rich.svelte';
  import { t } from '../i18n';

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
  export let credStatuses: Record<string, any>;
  export let credPostSetup: (accountKey: string) => Promise<void>;
  export let storeCredToken: (accountKey: string) => Promise<void>;
  export let hostOS: string;

  let credChangeModal: string | null = null; // account key
  let credChangeType = '';
  let credDeleteBusy = false;
  let credSetupStarted = false;
  let credForceToken = false; // force PAT input (for mirror fix)

  export function openCredChange(accountKey: string, currentType: string) {
    credChangeModal = accountKey;
    credChangeType = currentType;
    credForceToken = false;
    resetCredState();
    credDeleteBusy = false;
    credSetupStarted = false;
  }

  export async function openTokenSetup(accountKey: string) {
    credChangeModal = accountKey;
    credChangeType = 'token';
    credForceToken = true;
    resetCredState();
    credDeleteBusy = false;
    credSetupStarted = true;
    await fetchTokenGuide(accountKey);
  }

  async function deleteCredential() {
    if (!credChangeModal) return;
    const key = credChangeModal;
    credDeleteBusy = true;
    try {
      await bridge.credentialDelete(key);
      await reloadFromDisk();
      credStatuses[key] = {status: 'none', primary: 'none', pat: 'none', primaryMsg: '', patMsg: ''};
      credStatuses = credStatuses;
    } catch (err: any) {
      credResult = { ok: false, message: err?.message || String(err) };
      credDeleteBusy = false;
      return;
    }
    credDeleteBusy = false;
    credChangeModal = null;
  }

  function closeCredChange() {
    credChangeModal = null;
    resetCredState();
    credDeleteBusy = false;
    verifyAllCredentials();
  }

  // Detect the specific pattern of "OS-level network-permission denial"
  // inside a credential-status error message and return the catalog key of
  // a friendly hint with the right platform-specific advice. Returns null
  // when the error doesn't match the pattern.
  //
  // Why this exists: the raw Go error says "dial tcp 192.168.x.x:443: connect:
  // no route to host". On macOS that's almost always TCC blocking Local
  // Network access for an unsigned / untracked app bundle — not an actual
  // routing problem — and the user-facing recovery is entirely different
  // from a real network outage. This helper makes that distinction visible.
  function detectLANPermissionHint(msg: string | undefined): { summaryKey: string; link: string } | null {
    if (!msg) return null;
    const m = msg.toLowerCase();
    const looksLikeLAN = m.includes('192.168.') || m.includes('10.0.') || m.includes('.local');
    const isConnectRefused = m.includes('no route to host') || m.includes('host is unreachable') || m.includes('wsaehostunreach') || m.includes('host is down');
    if (!isConnectRefused) return null;
    const docBase = 'https://github.com/LuisPalacios/gitbox/blob/main/docs/credentials.md';
    if (hostOS === 'darwin') {
      return {
        summaryKey: 'cred.lanMac',
        link: docBase + '#macos-local-network-permission',
      };
    }
    if (hostOS === 'windows') {
      return {
        summaryKey: 'cred.lanWindows',
        link: docBase + '#windows-firewall',
      };
    }
    if (looksLikeLAN) {
      return {
        summaryKey: 'cred.lanLinux',
        link: docBase + '#linux-network-troubleshooting',
      };
    }
    return null;
  }

  // Map a credential status string to the catalog key of the short human
  // label shown in the status panel of the Change Credential modal.
  function credStatusKey(state: string): string {
    switch (state) {
      case 'ok':       return 'cred.state.ok';
      case 'warning':  return 'cred.state.warning';
      case 'offline':  return 'cred.state.offline';
      case 'error':    return 'cred.state.error';
      case 'none':     return 'cred.state.none';
      case 'checking': return 'health.checking';
      default:         return 'cred.state.unknown';
    }
  }

  // Re-verify credential for the account shown in the Change Credential modal.
  // Shows a "Checking…" state while the RPC runs.
  function verifyCredentialForModal(accountKey: string | null) {
    if (!accountKey) return;
    const prev = credStatuses[accountKey] || {};
    credStatuses[accountKey] = {...prev, primary: 'checking', pat: 'checking', status: 'checking'};
    credStatuses = credStatuses;
    bridge.credentialVerify(accountKey).then((cs) => {
      credStatuses[accountKey] = { ...cs };
      credStatuses = credStatuses;
    }).catch((err: any) => {
      credStatuses[accountKey] = {status: 'error', primary: 'error', pat: 'error', primaryMsg: String(err?.message || err), patMsg: ''};
      credStatuses = credStatuses;
    });
  }

  async function applyCredChange() {
    if (!credChangeModal) return;
    // Precheck: ensure the tools for the selected credential type are
    // installed before we remove the old credential. Bails out early with
    // the banner visible when something is missing, so the user can install
    // first without losing their existing setup.
    await runCredentialPrecheck(credChangeType);
    if (credPrecheck && !credPrecheck.ok) {
      return;
    }
    credSetupStarted = true;
    credBusy = true;
    credResult = null;
    try {
      await bridge.changeCredentialType(credChangeModal, credChangeType);
      await reloadFromDisk();
      if (credChangeType === 'gcm') {
        credResult = await bridge.credentialSetupGCM(credChangeModal);
      } else if (credChangeType === 'ssh') {
        credResult = await bridge.credentialSetupSSH(credChangeModal);
      } else if (credChangeType === 'token') {
        await fetchTokenGuide(credChangeModal);
        credBusy = false;
        return; // Wait for user to paste token.
      }
    } catch (err: any) {
      credResult = { ok: false, message: err?.message || String(err) };
    }
    credBusy = false;
    await credPostSetup(credChangeModal);
  }
</script>

{#if credChangeModal}
  {@const currentAcct = $accounts[credChangeModal]}
  <div class="overlay" use:dismiss={() => { if (!credBusy && !credDeleteBusy) closeCredChange(); }} transition:fade={{ duration: 120 }}>
    <div class="modal modal-account" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head">
        <h3>{credForceToken ? $t('cred.apiToken') : $t('cred.change')} &mdash; {credChangeModal}</h3>
        <button class="btn-x" on:click={closeCredChange}>&#10005;</button>
      </div>
      <div class="modal-body">
        {#if !credResult && !credBusy}
          {@const cs = credStatuses[credChangeModal] || {}}
          {@const primary = cs.primary || 'unknown'}
          {@const pat = cs.pat || 'unknown'}
          {@const overall = cs.status || 'unknown'}
          {@const primaryType = currentAcct?.default_credential_type || ''}
          {@const lanHint = detectLANPermissionHint(cs.primaryMsg) || detectLANPermissionHint(cs.patMsg)}
          <div class="cred-status-panel cred-status-panel-{overall}">
            <div class="cred-status-head">
              <span class="cred-status-title">{$t('cred.currentStatus')}</span>
              <button class="cred-status-recheck" title={$t('cred.reverify')} on:click={() => verifyCredentialForModal(credChangeModal)} disabled={primary === 'checking'}>
                {primary === 'checking' ? $t('health.checking') : $t('common.recheck')}
              </button>
            </div>
            <div class="cred-status-row">
              <span class="cred-status-dot cred-status-dot-{primary}"></span>
              <span class="cred-status-label">{primaryType ? $t('cred.primaryType', { type: primaryType.toUpperCase() }) : $t('cred.primary')}</span>
              <span class="cred-status-value">{$t(credStatusKey(primary))}</span>
            </div>
            {#if cs.primaryMsg}
              <p class="cred-status-detail">{cs.primaryMsg}</p>
            {/if}
            {#if primaryType === 'gcm' || primaryType === 'ssh'}
              {@const patCoveredByGCM = pat === 'ok' && (cs.patMsg || '').startsWith('API via GCM')}
              <div class="cred-status-row">
                <span class="cred-status-dot cred-status-dot-{pat === 'none' ? 'none' : pat}"></span>
                <span class="cred-status-label">{$t('cred.patLabel')}</span>
                <span class="cred-status-value">{patCoveredByGCM ? $t('cred.notCreated') : $t(credStatusKey(pat))}</span>
                {#if pat === 'none' || pat === 'warning'}
                  <button class="cred-status-pat-btn" on:click={() => credChangeModal && openTokenSetup(credChangeModal)} title={$t('cred.patTip')}>
                    {pat === 'warning' ? $t('cred.replacePat') : $t('cred.setupPat')}
                  </button>
                {/if}
              </div>
              {#if cs.patMsg}
                {#if cs.patMsg.startsWith('WARNING:')}
                  <hr class="cred-status-divider" />
                  <p class="cred-status-warning">{cs.patMsg}</p>
                {:else}
                  <p class="cred-status-detail">{cs.patMsg}</p>
                {/if}
              {/if}
            {/if}
            {#if lanHint}
              <div class="cred-status-lanhint">
                <p class="cred-status-lanhint-title">{$t('cred.lanTitle')}</p>
                <p>{$t(lanHint.summaryKey)} <a href={lanHint.link} on:click|preventDefault={() => BrowserOpenURL(lanHint.link)}>{$t('cred.troubleshooting')}</a>.</p>
              </div>
            {/if}
          </div>
        {/if}
        {#if credForceToken}
        <!-- Simplified PAT-only flow for mirror fix -->
        <p class="cred-step-desc"><Rich key="cred.mirrorNeedsPat" vars={{ type: currentAcct?.default_credential_type || 'none' }} /></p>
        {:else}
        <p class="cred-doc-link"><a href="https://github.com/LuisPalacios/gitbox/blob/main/docs/credentials.md" on:click|preventDefault={() => BrowserOpenURL('https://github.com/LuisPalacios/gitbox/blob/main/docs/credentials.md')}>{$t('cred.learnTypes')}</a></p>
        <div class="form-row">
          <label class="form-label" for="cc-type">{$t('cred.type')}</label>
          <select class="form-input" id="cc-type" bind:value={credChangeType} disabled={credBusy}>
            <option value="gcm" translate="no">Git Credential Manager (GCM)</option>
            <option value="ssh">SSH</option>
            <option value="token">{$t('onboard.token')}</option>
          </select>
        </div>

        {#if !credResult && credChangeType !== (currentAcct?.default_credential_type || '') && currentAcct?.default_credential_type}
          <p class="cred-change-warning">{$t(currentAcct?.default_credential_type === 'ssh' ? 'cred.changeWarnSsh' : currentAcct?.default_credential_type === 'token' ? 'cred.changeWarnToken' : currentAcct?.default_credential_type === 'gcm' ? 'cred.changeWarnGcm' : 'cred.changeWarn', { new: credChangeType.toUpperCase(), old: (currentAcct?.default_credential_type || '').toUpperCase() })}</p>
        {/if}

        {#if credPrecheck && !credPrecheck.ok}
          <PrecheckBanner precheck={credPrecheck} noteKey="cred.precheckNoteChange"
            onRecheck={() => runCredentialPrecheck(credChangeType)} />
        {/if}
        {/if}

        {#if (credForceToken || credChangeType === 'token') && credTokenGuide && !credResult}
          <p class="cred-step-desc">{$t('cred.tokenSteps')}</p>
          {#if credTokenScopes}
            <p class="cred-step-desc"><Rich key="cred.scopes" vars={{ scopes: credTokenScopes }} /></p>
          {/if}
          <p class="cred-step-link"><a href={credTokenGuide} on:click|preventDefault={() => BrowserOpenURL(credTokenGuide)}>{credTokenGuide}</a></p>
          <div class="form-row">
            <label class="form-label" for="cc-token">{$t('onboard.token')}</label>
            <input class="form-input" id="cc-token" type="password" bind:value={credTokenInput} placeholder="ghp_..." translate="no" />
          </div>
        {/if}

        {#if credBusy}
          <div class="loading"><div class="spinner"></div><span>{$t('cred.settingUpCred')}</span></div>
        {/if}

        {#if credResult}
          <CredentialResult {credResult} {credBusy} {sshKeyCopied} {copySSHKey}
            verifySSHConnection={() => credChangeModal && verifySSHConnection(credChangeModal)}
            regenerateSSHKey={() => credChangeModal && regenerateSSHKey(credChangeModal)} />
        {/if}

      </div>
      <div class="modal-foot">
        {#if currentAcct?.default_credential_type && !credResult && !credSetupStarted && credChangeType === (currentAcct?.default_credential_type || '')}
          <button class="btn-delete cred-delete-btn" on:click={deleteCredential} disabled={credDeleteBusy || credBusy}>
            {credDeleteBusy ? $t('cred.deleting') : $t('cred.deleteCurrent', { type: (currentAcct?.default_credential_type || '').toUpperCase() })}
          </button>
        {/if}
        {#if credResult}
          <button class="btn-cancel" on:click={closeCredChange}>{credResult.ok ? $t('onboard.done') : $t('common.close')}</button>
        {:else if credChangeType === 'token' && credTokenGuide}
          {#if credTokenInput.trim()}
            <button class="btn-add" on:click={() => credChangeModal && storeCredToken(credChangeModal)} disabled={credBusy}>
              {credBusy ? $t('cred.storing') : $t('cred.storeToken')}
            </button>
          {:else}
            <button class="btn-cancel" on:click={closeCredChange}>{$t('common.cancel')}</button>
          {/if}
        {:else}
          <button class="btn-cancel" on:click={closeCredChange}>{$t('common.cancel')}</button>
          <button class="btn-add" on:click={applyCredChange} disabled={credBusy || (credChangeType === (currentAcct?.default_credential_type || '') && credStatuses[credChangeModal || '']?.status === 'ok')}>
            {credBusy ? $t('cred.settingUp') : $t('cred.setup')}
          </button>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .cred-change-warning { font-size: 11px; color: #f59e0b; margin: 8px 0; padding: 6px 10px; border-radius: 4px; background: #f59e0b12; border: 1px solid #f59e0b44; line-height: 1.4; }
  :global([data-theme="light"]) .cred-change-warning { color: #b45309; background: #fef3c7; border-color: #f59e0b66; }
  .cred-doc-link { font-size: 11px; margin: 0 0 12px; }
  .cred-doc-link a { color: #4B95E9; text-decoration: underline; }
  .cred-doc-link a:hover { color: #6db0f7; }
  /* Credential-status panel inside the Change Credential modal */
  .cred-status-panel {
    margin: 0 0 14px;
    padding: 10px 12px;
    border-radius: 6px;
    border: 1px solid var(--border-subtle, #27272a);
    background: var(--bg-panel, rgba(255,255,255,0.03));
    font-size: 12px;
  }
  .cred-status-panel-ok { border-color: #16a34a66; background: rgba(22,163,74,0.08); }
  .cred-status-panel-warning { border-color: #f59e0b66; background: rgba(245,158,11,0.08); }
  .cred-status-panel-offline { border-color: #D81E5B66; background: rgba(216,30,91,0.10); }
  .cred-status-panel-error { border-color: #D81E5B66; background: rgba(216,30,91,0.10); }
  .cred-status-panel-none { border-color: #3f3f46; background: rgba(113,113,122,0.08); }
  :global([data-theme="light"]) .cred-status-panel-ok { background: rgba(22,163,74,0.10); border-color: #16a34a88; }
  :global([data-theme="light"]) .cred-status-panel-warning { background: rgba(245,158,11,0.12); border-color: #b45309; }
  :global([data-theme="light"]) .cred-status-panel-offline { background: rgba(216,30,91,0.12); border-color: #be123c; }
  :global([data-theme="light"]) .cred-status-panel-error { background: rgba(216,30,91,0.12); border-color: #be123c; }
  :global([data-theme="light"]) .cred-status-panel-none { background: rgba(113,113,122,0.10); border-color: #a1a1aa; }
  .cred-status-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px; }
  .cred-status-title { font-weight: 600; color: var(--text-primary); }
  .cred-status-recheck {
    background: transparent; color: var(--text-muted); border: 1px solid var(--border-subtle, #3f3f46);
    padding: 2px 10px; border-radius: 4px; cursor: pointer; font-size: 11px;
  }
  .cred-status-recheck:hover:not(:disabled) { background: var(--bg-hover); color: var(--text-primary); }
  .cred-status-recheck:disabled { opacity: 0.6; cursor: default; }
  .cred-status-row { display: flex; align-items: center; gap: 8px; margin: 4px 0; }
  .cred-status-dot { width: 8px; height: 8px; border-radius: 50%; display: inline-block; flex-shrink: 0; }
  .cred-status-dot-ok { background: #16a34a; }
  .cred-status-dot-warning { background: #f59e0b; }
  .cred-status-dot-offline { background: #D81E5B; }
  .cred-status-dot-error { background: #D81E5B; }
  .cred-status-dot-none { background: #71717a; }
  .cred-status-dot-checking { background: #4B95E9; }
  .cred-status-dot-unknown { background: #52525b; }
  .cred-status-label { color: var(--text-muted); }
  .cred-status-value { color: var(--text-primary); font-weight: 500; }
  .cred-status-detail {
    margin: 2px 0 6px 16px; font-size: 11px; color: var(--text-muted);
    word-break: break-word; line-height: 1.4;
    font-family: ui-monospace, Menlo, Consolas, monospace;
  }
  /* Visual separator + distinct styling for messages that open with WARNING:
     so they don't read as a continuation of the row above. */
  .cred-status-divider {
    border: none;
    border-top: 1px dashed rgba(245, 158, 11, 0.45);
    margin: 10px 0 8px;
  }
  :global([data-theme="light"]) .cred-status-divider { border-top-color: rgba(180, 83, 9, 0.6); }
  .cred-status-warning {
    margin: 6px 0 4px;
    padding: 8px 10px;
    font-size: 12px;
    line-height: 1.5;
    color: var(--text-primary);
    background: rgba(245, 158, 11, 0.10);
    border-left: 3px solid #f59e0b;
    border-radius: 0 4px 4px 0;
    word-break: break-word;
  }
  :global([data-theme="light"]) .cred-status-warning {
    background: rgba(245, 158, 11, 0.12);
    border-left-color: #b45309;
  }
  .cred-status-pat-btn {
    margin-left: auto;
    background: var(--accent, #4B95E9); color: #fff; border: 0;
    padding: 3px 10px; border-radius: 4px; cursor: pointer; font-size: 11px;
    font-weight: 500; white-space: nowrap;
  }
  .cred-status-pat-btn:hover { background: #6db0f7; }
  .cred-status-lanhint {
    margin-top: 10px; padding: 10px 12px; border-radius: 6px;
    background: rgba(59, 130, 246, 0.10);
    border: 1px solid rgba(59, 130, 246, 0.40);
    font-size: 12px; line-height: 1.5;
  }
  :global([data-theme="light"]) .cred-status-lanhint {
    background: rgba(59, 130, 246, 0.08);
    border-color: rgba(59, 130, 246, 0.55);
  }
  .cred-status-lanhint-title { margin: 0 0 4px; font-weight: 600; color: #3b82f6; }
  :global([data-theme="light"]) .cred-status-lanhint-title { color: #1d4ed8; }
  .cred-status-lanhint p { margin: 4px 0; color: var(--text-primary); }
  .cred-status-lanhint a { color: #3b82f6; text-decoration: underline; }
  .cred-status-lanhint a:hover { color: #60a5fa; }
  .cred-delete-btn { margin-right: auto; }
</style>
