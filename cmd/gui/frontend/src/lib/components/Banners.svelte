<script lang="ts">
  // Warnings about the global git config under the top bar: a global
  // identity, missing GCM helper settings, the global gitignore, and the
  // gitignore install result.
  import type { GlobalIdentityWarn, GCMWarn, GitignoreWarn, GitignoreInstalled } from '../types';

  export let globalIdentityWarn: GlobalIdentityWarn | null;
  export let globalGCMWarn: GCMWarn | null;
  export let globalGCMFixing: boolean;
  export let gitignoreWarn: GitignoreWarn | null;
  export let gitignoreInstalling: boolean;
  export let gitignoreInstalled: GitignoreInstalled | null;
  export let gitignoreError: string;
  export let fixGlobalIdentity: () => Promise<void>;
  export let fixGlobalGCM: () => Promise<void>;
  export let installGlobalGitignore: () => Promise<void>;
</script>

<!-- ── GLOBAL IDENTITY WARNING ── -->
{#if globalIdentityWarn}
<div class="identity-warn">
  <span class="identity-warn-icon">&#9888;</span>
  <span class="identity-warn-text">
    Global <code>~/.gitconfig</code> has
    {#if globalIdentityWarn.hasName}user.name="{globalIdentityWarn.name}"{/if}{#if globalIdentityWarn.hasName && globalIdentityWarn.hasEmail}, {/if}{#if globalIdentityWarn.hasEmail}user.email="{globalIdentityWarn.email}"{/if}
    &mdash; this can override per-repo identity.
  </span>
  <button class="identity-warn-fix" on:click={fixGlobalIdentity}>Remove</button>
  <button class="identity-warn-dismiss" on:click={() => globalIdentityWarn = null}>&#10005;</button>
</div>
{/if}

<!-- ── GLOBAL GCM CREDENTIAL-HELPER WARNING ── -->
{#if globalGCMWarn}
<div class="identity-warn">
  <span class="identity-warn-icon">&#9888;</span>
  <span class="identity-warn-text">
    Global <code>~/.gitconfig</code> is missing GCM settings &mdash;
    {#if !globalGCMWarn.hasHelper}
      <code>credential.helper</code> is not set
    {:else if globalGCMWarn.helperValue !== globalGCMWarn.expectedHelper}
      <code>credential.helper</code> is <code>{globalGCMWarn.helperValue}</code>, expected <code>{globalGCMWarn.expectedHelper}</code>
    {/if}
    {#if (!globalGCMWarn.hasHelper || globalGCMWarn.helperValue !== globalGCMWarn.expectedHelper) && (!globalGCMWarn.hasCredentialStore || globalGCMWarn.credentialStoreValue !== globalGCMWarn.expectedCredentialStore)}
      and
    {/if}
    {#if !globalGCMWarn.hasCredentialStore}
      <code>credential.credentialStore</code> is not set
    {:else if globalGCMWarn.credentialStoreValue !== globalGCMWarn.expectedCredentialStore}
      <code>credential.credentialStore</code> is <code>{globalGCMWarn.credentialStoreValue}</code>, expected <code>{globalGCMWarn.expectedCredentialStore}</code>
    {/if}
    &mdash; GCM setup will fail with "Device not configured" until this is fixed.
  </span>
  <button class="identity-warn-fix" on:click={fixGlobalGCM} disabled={globalGCMFixing}>{globalGCMFixing ? 'Fixing…' : 'Configure'}</button>
  <button class="identity-warn-dismiss" on:click={() => globalGCMWarn = null}>&#10005;</button>
</div>
{/if}

<!-- ── GLOBAL GITIGNORE WARNING ── -->
{#if gitignoreWarn}
<div class="identity-warn">
  <span class="identity-warn-icon">&#9888;</span>
  <span class="identity-warn-text">
    {#if !gitignoreWarn.fileExists}
      Global gitignore <code>{gitignoreWarn.path}</code> is missing &mdash;
      install OS-junk patterns (.DS_Store, Thumbs.db, *~, …) so per-project <code>.gitignore</code> files don't have to repeat them.
    {:else if !gitignoreWarn.blockPresent}
      <code>{gitignoreWarn.path}</code> is missing the recommended block &mdash;
      install OS-junk patterns (.DS_Store, Thumbs.db, *~, …) so per-project <code>.gitignore</code> files don't have to repeat them.
    {:else if !gitignoreWarn.blockUpToDate}
      <code>{gitignoreWarn.path}</code> has an out-of-date recommended block &mdash;
      install will refresh it.
    {:else if gitignoreWarn.hasDuplicates}
      <code>{gitignoreWarn.path}</code> has {gitignoreWarn.duplicates.length} managed pattern{gitignoreWarn.duplicates.length === 1 ? '' : 's'} duplicated outside the block:
      <code>{gitignoreWarn.duplicates.slice(0, 5).join(', ')}{gitignoreWarn.duplicates.length > 5 ? ', …' : ''}</code> &mdash;
      install will sanitize them.
    {:else if !gitignoreWarn.excludesfileSet}
      <code>core.excludesfile</code> is not set in your global git config &mdash;
      install will point it at <code>{gitignoreWarn.path}</code>.
    {/if}
  </span>
  <button class="identity-warn-fix" on:click={installGlobalGitignore} disabled={gitignoreInstalling}>
    {gitignoreInstalling ? 'Installing…' : 'Install'}
  </button>
  <button class="identity-warn-dismiss" on:click={() => gitignoreWarn = null}>&#10005;</button>
</div>
{/if}

{#if gitignoreInstalled && !gitignoreWarn}
<div class="identity-warn identity-warn-ok">
  <span class="identity-warn-icon">&#10003;</span>
  <span class="identity-warn-text">
    {#if gitignoreInstalled.alreadyUpToDate && !gitignoreInstalled.setExcludesfile}
      Global gitignore is already up to date.
    {:else}
      Global gitignore installed.
      {#if gitignoreInstalled.backupPath}
        Backup saved at <code>{gitignoreInstalled.backupPath}</code>.
      {/if}
      {#if gitignoreInstalled.setExcludesfile}
        <code>core.excludesfile</code> set.
      {/if}
    {/if}
  </span>
  <button class="identity-warn-dismiss" on:click={() => gitignoreInstalled = null}>&#10005;</button>
</div>
{/if}

{#if gitignoreError}
<div class="identity-warn">
  <span class="identity-warn-icon">&#9888;</span>
  <span class="identity-warn-text">Could not install global gitignore: {gitignoreError}</span>
  <button class="identity-warn-dismiss" on:click={() => gitignoreError = ''}>&#10005;</button>
</div>
{/if}

<style>
  /* ── Global identity warning banner ── */
  .identity-warn {
    display: flex; align-items: center; gap: 8px;
    padding: 7px 12px; margin: 0 16px 8px;
    background: #431407; border: 1px solid #c2410c; border-radius: 8px;
    font-size: 12px; color: #fdba74;
  }
  :global([data-theme="light"]) .identity-warn {
    background: #fff7ed; border-color: #c2410c; color: #7c2d12;
  }
  .identity-warn-icon { font-size: 15px; flex-shrink: 0; }
  .identity-warn-text { flex: 1; line-height: 1.4; }
  .identity-warn-text code { font-size: 11px; padding: 1px 4px; background: rgba(255,255,255,0.1); border-radius: 3px; }
  :global([data-theme="light"]) .identity-warn-text code { background: rgba(0,0,0,0.06); }
  .identity-warn-fix {
    padding: 3px 10px; font-size: 11px; font-weight: 600;
    background: #c2410c; color: #fff; border: none; border-radius: 5px; cursor: pointer;
    white-space: nowrap;
  }
  .identity-warn-fix:hover { background: #ea580c; }
  .identity-warn-dismiss {
    background: none; border: none; color: #fdba74; cursor: pointer;
    font-size: 14px; padding: 0 2px; line-height: 1;
  }
  :global([data-theme="light"]) .identity-warn-dismiss { color: #7c2d12; }
  .identity-warn-dismiss:hover { opacity: 0.7; }
  .identity-warn-fix:disabled { opacity: 0.6; cursor: progress; }
  /* Success variant of the warning banner — used for the gitignore install confirmation. */
  .identity-warn.identity-warn-ok {
    background: #052e1a; border-color: #15803d; color: #86efac;
  }
  :global([data-theme="light"]) .identity-warn.identity-warn-ok {
    background: #f0fdf4; border-color: #15803d; color: #14532d;
  }
</style>
