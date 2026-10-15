<script lang="ts">
  // The result of a credential setup, shared by Add account and Change
  // credential: the SSH key block (copy the public key, verify the
  // connection, regenerate the key) or the plain result message.
  import { BrowserOpenURL } from '../../wailsjs/runtime/runtime';

  export let credResult: { ok: boolean; message: string; needsPAT?: boolean; sshPublicKey?: string; sshAddURL?: string; sshVerified?: boolean };
  export let credBusy: boolean;
  export let sshKeyCopied: boolean;
  export let copySSHKey: (text: string) => void;
  export let verifySSHConnection: () => void;
  export let regenerateSSHKey: () => void;
</script>

{#if credResult.sshPublicKey}
  <div class="cred-result cred-result-ok">
    <pre class="cred-result-msg">{credResult.message}</pre>
  </div>
  <div class="ssh-connection-status" class:ssh-connected={credResult.sshVerified} class:ssh-disconnected={!credResult.sshVerified}>
    <span class="ssh-status-dot"></span>
    <span>{credResult.sshVerified ? 'Connection verified' : 'Connection not verified'}</span>
  </div>
  {#if !credResult.sshVerified}
    <p class="cred-step-desc" style="margin-top: 8px;">Copy the public SSH key and add it to your provider:</p>
    <div class="ssh-key-box">
      <code class="ssh-key-text">{credResult.sshPublicKey}</code>
      <button class="ssh-key-copy" on:click={() => credResult?.sshPublicKey && copySSHKey(credResult.sshPublicKey)} title="Copy to clipboard">{@html sshKeyCopied ? '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="20 6 9 17 4 12"/></svg>' : '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>'}</button>
    </div>
    {#if credResult.sshAddURL}
      <p class="cred-step-link"><a href={credResult.sshAddURL} on:click|preventDefault={() => credResult?.sshAddURL && BrowserOpenURL(credResult.sshAddURL)}>{credResult.sshAddURL}</a></p>
    {/if}
  {/if}
  <div style="display: flex; gap: 8px; margin-top: 8px;">
    {#if !credResult.sshVerified}
      <button class="btn-add cred-action-btn" on:click={verifySSHConnection} disabled={credBusy}>
        {credBusy ? 'Verifying...' : 'Verify connection'}
      </button>
    {/if}
    <button class="btn-cancel cred-action-btn" on:click={regenerateSSHKey} disabled={credBusy}>
      {credBusy ? 'Regenerating...' : 'Regenerate SSH key'}
    </button>
  </div>
{:else}
  <div class="cred-result" class:cred-result-ok={credResult.ok} class:cred-result-err={!credResult.ok}>
    <pre class="cred-result-msg">{credResult.message}</pre>
  </div>
{/if}

<style>
  .cred-result { margin-top: 10px; padding: 8px 10px; border-radius: 6px; font-size: 12px; }
  .cred-result-ok { background: #16653412; border: 1px solid #166534; color: var(--text-primary); }
  .cred-result-err { background: #D81E5B12; border: 1px solid #D81E5B; color: #D81E5B; }
  .cred-result-msg { margin: 0; white-space: pre-wrap; font-family: monospace; font-size: 11px; line-height: 1.5; }
  .ssh-key-box { margin: 6px 0; padding: 6px 8px; background: var(--bg-hover, #222); border: 1px solid var(--border, #444); border-radius: 4px; display: flex; align-items: center; gap: 6px; }
  .ssh-key-text { font-family: monospace; font-size: 9px; word-break: break-all; white-space: nowrap; color: var(--text-primary); flex: 1; overflow-x: auto; user-select: all; }
  .ssh-key-copy { background: none; border: 1px solid var(--border, #444); border-radius: 3px; padding: 3px 6px; cursor: pointer; color: var(--text-muted); font-size: 12px; flex-shrink: 0; transition: all 0.15s; }
  .ssh-key-copy:hover { color: var(--text-primary); border-color: var(--border-hover, #666); background: var(--bg-card, #2a2a2a); }
  .ssh-connection-status { display: flex; align-items: center; gap: 6px; margin-top: 8px; font-size: 12px; font-weight: 600; }
  .ssh-status-dot { width: 10px; height: 10px; border-radius: 50%; flex-shrink: 0; }
  .ssh-connected .ssh-status-dot { background: #22c55e; box-shadow: 0 0 6px #22c55e88; }
  .ssh-connected { color: #86efac; }
  .ssh-disconnected .ssh-status-dot { background: #ef4444; box-shadow: 0 0 6px #ef444488; }
  .ssh-disconnected { color: #fca5a5; }
  :global([data-theme="light"]) .ssh-connected { color: #166534; }
  :global([data-theme="light"]) .ssh-disconnected { color: #be123c; }
</style>
