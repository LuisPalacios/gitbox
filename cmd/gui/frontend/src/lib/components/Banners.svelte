<script lang="ts">
  // Warnings about the global git config under the top bar: a global
  // identity, missing GCM helper settings, the global gitignore, and the
  // gitignore install result.
  import type { GlobalIdentityWarn, GCMWarn, GitignoreWarn, GitignoreInstalled } from '../types';
  import { t } from '../i18n';
  import Rich from '../Rich.svelte';

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
    {#if globalIdentityWarn.hasName && globalIdentityWarn.hasEmail}
      <Rich key="banner.identityBoth" vars={{ name: globalIdentityWarn.name, email: globalIdentityWarn.email }} />
    {:else if globalIdentityWarn.hasName}
      <Rich key="banner.identityName" vars={{ name: globalIdentityWarn.name }} />
    {:else}
      <Rich key="banner.identityEmail" vars={{ email: globalIdentityWarn.email }} />
    {/if}
  </span>
  <button class="identity-warn-fix" on:click={fixGlobalIdentity}>{$t('common.remove')}</button>
  <button class="identity-warn-dismiss" on:click={() => globalIdentityWarn = null}>&#10005;</button>
</div>
{/if}

<!-- ── GLOBAL GCM CREDENTIAL-HELPER WARNING ── -->
{#if globalGCMWarn}
<div class="identity-warn">
  <span class="identity-warn-icon">&#9888;</span>
  <span class="identity-warn-text">
    <Rich key="banner.gcmIntro" />
    {#if !globalGCMWarn.hasHelper}
      <Rich key="banner.gcmHelperUnset" />
    {:else if globalGCMWarn.helperValue !== globalGCMWarn.expectedHelper}
      <Rich key="banner.gcmHelperWrong" vars={{ value: globalGCMWarn.helperValue, expected: globalGCMWarn.expectedHelper }} />
    {/if}
    {#if (!globalGCMWarn.hasHelper || globalGCMWarn.helperValue !== globalGCMWarn.expectedHelper) && (!globalGCMWarn.hasCredentialStore || globalGCMWarn.credentialStoreValue !== globalGCMWarn.expectedCredentialStore)}
      {$t('banner.gcmAnd')}
    {/if}
    {#if !globalGCMWarn.hasCredentialStore}
      <Rich key="banner.gcmStoreUnset" />
    {:else if globalGCMWarn.credentialStoreValue !== globalGCMWarn.expectedCredentialStore}
      <Rich key="banner.gcmStoreWrong" vars={{ value: globalGCMWarn.credentialStoreValue, expected: globalGCMWarn.expectedCredentialStore }} />
    {/if}
    {$t('banner.gcmOutro')}
  </span>
  <button class="identity-warn-fix" on:click={fixGlobalGCM} disabled={globalGCMFixing}>{globalGCMFixing ? $t('banner.fixing') : $t('banner.configure')}</button>
  <button class="identity-warn-dismiss" on:click={() => globalGCMWarn = null}>&#10005;</button>
</div>
{/if}

<!-- ── GLOBAL GITIGNORE WARNING ── -->
{#if gitignoreWarn}
<div class="identity-warn">
  <span class="identity-warn-icon">&#9888;</span>
  <span class="identity-warn-text">
    {#if !gitignoreWarn.fileExists}
      <Rich key="banner.gitignoreMissing" vars={{ path: gitignoreWarn.path }} />
    {:else if !gitignoreWarn.blockPresent}
      <Rich key="banner.gitignoreNoBlock" vars={{ path: gitignoreWarn.path }} />
    {:else if !gitignoreWarn.blockUpToDate}
      <Rich key="banner.gitignoreOutdated" vars={{ path: gitignoreWarn.path }} />
    {:else if gitignoreWarn.hasDuplicates}
      <Rich key={gitignoreWarn.duplicates.length === 1 ? 'banner.gitignoreDuplicatesOne' : 'banner.gitignoreDuplicates'} vars={{
        path: gitignoreWarn.path,
        n: gitignoreWarn.duplicates.length,
        list: `${gitignoreWarn.duplicates.slice(0, 5).join(', ')}${gitignoreWarn.duplicates.length > 5 ? ', …' : ''}`,
      }} />
    {:else if !gitignoreWarn.excludesfileSet}
      <Rich key="banner.excludesUnset" vars={{ path: gitignoreWarn.path }} />
    {/if}
  </span>
  <button class="identity-warn-fix" on:click={installGlobalGitignore} disabled={gitignoreInstalling}>
    {gitignoreInstalling ? $t('banner.installing') : $t('banner.install')}
  </button>
  <button class="identity-warn-dismiss" on:click={() => gitignoreWarn = null}>&#10005;</button>
</div>
{/if}

{#if gitignoreInstalled && !gitignoreWarn}
<div class="identity-warn identity-warn-ok">
  <span class="identity-warn-icon">&#10003;</span>
  <span class="identity-warn-text">
    {#if gitignoreInstalled.alreadyUpToDate && !gitignoreInstalled.setExcludesfile}
      {$t('banner.gitignoreUpToDate')}
    {:else}
      {$t('banner.gitignoreInstalled')}
      {#if gitignoreInstalled.backupPath}
        <Rich key="banner.backupSaved" vars={{ path: gitignoreInstalled.backupPath }} />
      {/if}
      {#if gitignoreInstalled.setExcludesfile}
        <Rich key="banner.excludesSet" />
      {/if}
    {/if}
  </span>
  <button class="identity-warn-dismiss" on:click={() => gitignoreInstalled = null}>&#10005;</button>
</div>
{/if}

{#if gitignoreError}
<div class="identity-warn">
  <span class="identity-warn-icon">&#9888;</span>
  <span class="identity-warn-text">{$t('banner.gitignoreFailed', { error: gitignoreError })}</span>
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
  .identity-warn-text :global(code) { font-size: 11px; padding: 1px 4px; background: rgba(255,255,255,0.1); border-radius: 3px; }
  :global([data-theme="light"]) .identity-warn-text :global(code) { background: rgba(0,0,0,0.06); }
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
