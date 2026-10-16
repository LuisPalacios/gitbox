<script lang="ts">
  // Shown when a config reload or mutation fails (usually on-disk drift).
  import { t } from '../i18n';

  export let reloadError: string;
</script>

{#if reloadError}
  <div class="reload-error-banner" role="alert">
    <span class="reload-error-label">{$t('reloadError.label')}</span>
    <span class="reload-error-msg">{reloadError}</span>
    <span class="reload-error-hint">{$t('reloadError.hint')}</span>
    <button class="reload-error-dismiss" on:click={() => reloadError = ''} title={$t('notice.dismiss')}>&#10005;</button>
  </div>
{/if}

<style>
  .reload-error-banner {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
    background: #3a1515;
    border-bottom: 1px solid #7f1d1d;
    color: #fecaca;
    font-size: 12px;
    line-height: 1.35;
  }
  .reload-error-label {
    font-weight: 700;
    color: #fca5a5;
    white-space: nowrap;
  }
  .reload-error-msg {
    flex: 1 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .reload-error-hint {
    font-style: italic;
    opacity: 0.8;
    white-space: nowrap;
  }
  .reload-error-dismiss {
    background: transparent;
    border: none;
    color: #fca5a5;
    cursor: pointer;
    font-size: 14px;
    padding: 0 4px;
  }
  .reload-error-dismiss:hover { color: #fff; }
</style>
