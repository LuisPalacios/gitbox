<script lang="ts">
  // Shown while the backend cannot reach one or more servers. The backend
  // has already stopped the work queued for them and keeps trying in the
  // background; it clears this banner by itself when they answer again.
  // Retry asks it to test the network now.
  import { t } from '../i18n';

  export let hosts: string[] = [];
  export let retrying = false;
  export let onRetry: () => void;
</script>

{#if hosts.length > 0}
  <div class="offline-banner" role="alert">
    <span class="offline-label">{$t('offline.title')}</span>
    <span class="offline-msg">{$t('offline.message', { hosts: hosts.join(', ') })}</span>
    <button class="offline-retry" on:click={onRetry} disabled={retrying}>
      {retrying ? $t('offline.retrying') : $t('offline.retry')}
    </button>
  </div>
{/if}

<style>
  .offline-banner {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px 10px;
    padding: 8px 12px;
    background: #3b2a08;
    border-bottom: 1px solid #92600a;
    color: #fde9b8;
    font-size: 12px;
    line-height: 1.35;
  }
  .offline-label {
    font-weight: 700;
    color: #fbbf24;
    white-space: nowrap;
  }
  .offline-msg {
    flex: 1 1 160px;
    min-width: 0;
  }
  .offline-retry {
    background: #fbbf24;
    border: none;
    border-radius: 4px;
    color: #2b1d02;
    cursor: pointer;
    font-size: 12px;
    font-weight: 600;
    padding: 3px 10px;
    white-space: nowrap;
  }
  .offline-retry:hover:not(:disabled) { background: #fcd34d; }
  .offline-retry:disabled { opacity: 0.6; cursor: default; }
</style>
