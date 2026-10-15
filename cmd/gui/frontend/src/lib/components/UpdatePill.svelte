<script lang="ts">
  // Footer pill for a pending update: install (or open the release page),
  // progress, done, error, and dismiss until the next launch.
  import { bridge } from '../bridge';
  import { BrowserOpenURL, Quit } from '../../../wailsjs/runtime/runtime';
  import type { UpdateInfo } from '../types';
  import { t } from '../i18n';

  export let updateInfo: UpdateInfo | null;
  export let updateApplying: boolean;
  export let updateProgress: string;
  export let updateDone: boolean;
  export let updateError: string;
  // Version whose pill the user dismissed: hidden for this session only.
  export let updateDismissed: string;
</script>

{#if updateDone}
  <div class="update-pill update-done">
    <span>&#10003; {$t('update.updated')}</span>
    <button class="update-pill-btn" on:click={() => Quit()}>{$t('update.quit')}</button>
  </div>
{:else if updateError}
  <div class="update-pill update-error">
    <span>&#9888; {updateError}</span>
    {#if updateInfo?.url}
      <button class="update-pill-btn" on:click={() => updateInfo && BrowserOpenURL(updateInfo.url)}>{$t('update.releasePage')}</button>
    {/if}
    <button class="update-pill-dismiss" on:click={() => updateError = ''} title={$t('notice.dismiss')}>&#10005;</button>
  </div>
{:else if updateInfo?.available}
  <div class="update-pill">
    {#if updateApplying}
      <span class="update-pill-spin">&#8635;</span> <span>{updateProgress || $t('simple.updating')}</span>
    {:else}
      <span>&#9650;</span>
      {#if updateInfo.selfUpdate}
        <button class="update-pill-btn" on:click={() => { updateApplying = true; updateError = ''; bridge.applyUpdate().catch((e) => { updateApplying = false; updateError = typeof e === 'string' ? e : (e?.message || $t('update.failed')); }); }}>{$t('update.available', { version: updateInfo.latest })}</button>
      {:else}
        <button class="update-pill-btn" title={$t('update.openReleaseTip')} on:click={() => updateInfo && BrowserOpenURL(updateInfo.url)}>{$t('update.available', { version: updateInfo.latest })}</button>
      {/if}
      <button class="update-pill-dismiss" on:click={() => { updateDismissed = updateInfo?.latest ?? ''; updateInfo = null; }} title={$t('update.dismissTip')}>&#10005;</button>
    {/if}
  </div>
{/if}

<style>
  /* update pill in footer */
  .update-pill {
    display: flex; align-items: center; gap: 4px;
    color: #f59e0b; font-size: 11px; font-weight: 600; white-space: nowrap;
  }
  :global([data-theme="light"]) .update-pill { color: #d97706; }
  .update-pill.update-done { color: #22d3ee; }
  :global([data-theme="light"]) .update-pill.update-done { color: #0891b2; }
  .update-pill.update-error { color: #f87171; }
  :global([data-theme="light"]) .update-pill.update-error { color: #dc2626; }
  .update-pill-btn {
    background: none; border: none; color: inherit; cursor: pointer;
    font-size: 11px; font-weight: 600; padding: 0; text-decoration: underline; text-underline-offset: 2px;
  }
  .update-pill-btn:hover { opacity: 0.8; }
  .update-pill-dismiss { background: none; border: none; color: inherit; cursor: pointer; opacity: 0.5; font-size: 10px; padding: 0 2px; }
  .update-pill-dismiss:hover { opacity: 1; }
  .update-pill-spin { animation: spin 1s linear infinite; display: inline-block; }
  @keyframes spin { to { transform: rotate(360deg); } }
</style>
