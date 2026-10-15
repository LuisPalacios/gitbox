<script lang="ts">
  // Full-screen recovery view when the config file exists but fails to
  // parse (issue #60): restore a dated backup, auto-repair, start fresh or
  // exit. The recovery actions live in App because they drive startup.
  import { Quit } from '../../../wailsjs/runtime/runtime';
  import type { ConfigBackupInfo } from '../types';
  import { t } from '../i18n';
  import Rich from '../Rich.svelte';

  export let configPath: string;
  export let cfgLoadErrorMsg: string;
  export let cfgRepairFailure: string;
  export let cfgBackups: ConfigBackupInfo[];
  export let cfgLoadErrorBusy: boolean;
  export let cfgRestoreBackup: (path: string) => Promise<void>;
  export let cfgDoRepair: () => Promise<void>;
  export let cfgStartFresh: () => Promise<void>;

  function cfgFormatBackupTime(ts: string): string {
    if (!ts) return '';
    const d = new Date(ts);
    if (isNaN(d.getTime())) return ts;
    return d.toLocaleString(undefined, {
      year: 'numeric', month: 'short', day: '2-digit',
      hour: '2-digit', minute: '2-digit', second: '2-digit',
    });
  }

  function cfgFormatBackupSize(n: number): string {
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    return `${(n / 1024 / 1024).toFixed(1)} MB`;
  }

  function cfgExit() {
    Quit();
  }
</script>

<div class="cfg-error-screen">
  <div class="cfg-error-card">
    <h2 class="cfg-error-title">{$t('configError.title')}</h2>
    <p class="cfg-error-path">{configPath || '~/.config/gitbox/gitbox.json'}</p>
    <pre class="cfg-error-detail">{cfgLoadErrorMsg}</pre>

    {#if cfgRepairFailure}
      <div class="cfg-error-failure" role="alert">
        <strong>{$t('configError.actionFailed')}</strong> {cfgRepairFailure}
      </div>
    {/if}

    <!-- Primary recovery: pick a dated backup and restore it. -->
    <section class="cfg-error-section">
      <h3 class="cfg-error-section-title">{$t('configError.restoreTitle')}</h3>
      <p class="cfg-error-section-desc">
        <Rich key="configError.restoreDesc" />
      </p>
      {#if cfgBackups.length === 0}
        <p class="cfg-error-empty">{$t('configError.noBackups')}</p>
      {:else}
        <ul class="cfg-error-backup-list">
          {#each cfgBackups as b}
            <li class="cfg-error-backup-row">
              <div class="cfg-error-backup-meta">
                <div class="cfg-error-backup-time">{cfgFormatBackupTime(b.timestamp) || b.filename}</div>
                <div class="cfg-error-backup-sub">{b.filename} · {cfgFormatBackupSize(b.size_bytes)}</div>
              </div>
              <button class="cfg-error-btn cfg-error-btn-primary cfg-error-btn-small"
                      on:click={() => cfgRestoreBackup(b.path)}
                      disabled={cfgLoadErrorBusy}>{$t('configError.restore')}</button>
            </li>
          {/each}
        </ul>
      {/if}
    </section>

    <!-- Secondary: in-place auto-repair (drops dangling refs only). -->
    <section class="cfg-error-section">
      <h3 class="cfg-error-section-title">{$t('configError.repairTitle')}</h3>
      <p class="cfg-error-section-desc">
        {$t('configError.repairDesc')}
      </p>
      <div class="cfg-error-section-actions">
        <button class="cfg-error-btn cfg-error-btn-secondary"
                on:click={cfgDoRepair}
                disabled={cfgLoadErrorBusy}>
          {cfgLoadErrorBusy ? $t('configError.working') : $t('configError.repair')}
        </button>
      </div>
    </section>

    <!-- Footer: start fresh or exit. -->
    <div class="cfg-error-actions">
      <button class="cfg-error-btn cfg-error-btn-ghost" on:click={cfgExit} disabled={cfgLoadErrorBusy}>{$t('configError.exit')}</button>
      <button class="cfg-error-btn cfg-error-btn-ghost" on:click={cfgStartFresh} disabled={cfgLoadErrorBusy}>{$t('configError.startFresh')}</button>
    </div>
  </div>
</div>

<style>
  .cfg-error-screen {
    position: fixed;
    inset: 0;
    background: var(--bg-base);
    color: var(--text-primary);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 24px;
    overflow: auto;
    z-index: 10000;
  }
  .cfg-error-card {
    max-width: 620px;
    width: 100%;
    background: var(--bg-card);
    color: var(--text-primary);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 22px 24px;
    box-shadow: var(--card-shadow);
  }
  .cfg-error-title {
    margin: 0 0 4px;
    font-size: 16px;
    font-weight: 600;
    color: var(--text-primary);
  }
  .cfg-error-path {
    margin: 0 0 12px;
    font-size: 11.5px;
    color: var(--text-muted);
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    word-break: break-all;
  }
  .cfg-error-detail {
    background: rgba(216, 30, 91, 0.10);
    border: 1px solid rgba(216, 30, 91, 0.45);
    border-radius: 6px;
    padding: 10px 12px;
    margin: 0 0 14px;
    font-size: 11.5px;
    line-height: 1.5;
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 140px;
    overflow: auto;
    color: var(--text-primary);
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  }
  .cfg-error-failure {
    background: #D81E5B;
    border: 1px solid #D81E5B;
    border-radius: 6px;
    padding: 10px 12px;
    margin: 0 0 14px;
    font-size: 12px;
    color: #ffffff;
  }
  .cfg-error-failure strong { color: #ffffff; }
  .cfg-error-section {
    margin: 0 0 16px;
    padding: 12px 14px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--bg-base);
  }
  .cfg-error-section-title {
    margin: 0 0 4px;
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary);
  }
  .cfg-error-section-desc {
    margin: 0 0 10px;
    font-size: 11.5px;
    line-height: 1.45;
    color: var(--text-secondary);
  }
  .cfg-error-section-actions {
    display: flex; align-items: center;
    justify-content: flex-start;
  }
  .cfg-error-empty {
    margin: 0;
    padding: 8px 10px;
    font-size: 11.5px;
    color: var(--text-muted);
    font-style: italic;
  }
  .cfg-error-backup-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
    max-height: 200px;
    overflow-y: auto;
  }
  .cfg-error-backup-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 8px 10px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 6px;
  }
  .cfg-error-backup-meta { flex: 1 1 auto; min-width: 0; }
  .cfg-error-backup-time {
    font-size: 12px;
    font-weight: 600;
    color: var(--text-primary);
  }
  .cfg-error-backup-sub {
    font-size: 11px;
    color: var(--text-muted);
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .cfg-error-actions {
    display: flex; align-items: center;
    justify-content: flex-end;
    gap: 8px;
  }
  .cfg-error-btn {
    border-radius: 6px;
    padding: 8px 14px;
    font-size: 12.5px;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.12s, opacity 0.12s, border-color 0.12s;
    border: 1px solid transparent;
  }
  .cfg-error-btn:disabled { opacity: 0.55; cursor: default; }
  .cfg-error-btn-small { padding: 5px 10px; font-size: 11.5px; }
  .cfg-error-btn-ghost {
    background: transparent;
    color: var(--text-secondary);
    border-color: var(--border);
  }
  .cfg-error-btn-ghost:hover:not(:disabled) {
    color: var(--text-primary);
    border-color: var(--border-hover);
    background: var(--bg-hover);
  }
  .cfg-error-btn-secondary {
    background: transparent;
    color: #D81E5B;
    border-color: #D81E5B;
  }
  .cfg-error-btn-secondary:hover:not(:disabled) {
    background: rgba(216, 30, 91, 0.10);
  }
  .cfg-error-btn-primary {
    background: #D81E5B;
    color: #ffffff;
    border-color: #D81E5B;
  }
  .cfg-error-btn-primary:hover:not(:disabled) {
    filter: brightness(1.08);
  }
</style>
