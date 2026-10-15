<script lang="ts">
  // The result cards and messages (lib/notices.ts), stacked in the
  // bottom-right corner above every modal and menu.
  import { fly } from 'svelte/transition';
  import { notices, dismissNotice } from '../notices';
  import { themeStore } from '../stores';
  import { statusColor } from '../status';
  import { t } from '../i18n';
  import UIIcon from '../UIIcon.svelte';

  const color = { success: 'clean', info: 'ahead', error: 'error' } as const;
  $: sc = (s: string) => statusColor(s, $themeStore);
</script>

<div class="notices" aria-live="polite">
  {#each $notices as n (n.id)}
    <div class="notice" class:notice-error={n.kind === 'error'} role={n.kind === 'error' ? 'alert' : 'status'} transition:fly={{ y: 8, duration: 120 }}>
      <div class="head">
        <span class="mark" style="color: {sc(color[n.kind])}" aria-hidden="true">{n.kind === 'error' ? '✕' : '●'}</span>
        <span class="title">{n.title}</span>
        <button class="icon-btn" on:click={() => dismissNotice(n.id)} title={$t('notice.dismiss')} aria-label={$t('notice.dismiss')}><UIIcon name="x" /></button>
      </div>
      {#if n.lines.length > 0}
        <ul>
          {#each n.lines as line}<li class="selectable">{line}</li>{/each}
        </ul>
      {/if}
    </div>
  {/each}
</div>

<style>
  .notices {
    position: fixed; right: 16px; bottom: 40px; z-index: 200;
    display: flex; flex-direction: column; gap: 8px; width: min(360px, calc(100vw - 32px));
    pointer-events: none;
  }
  .notice {
    pointer-events: auto; padding: 8px 8px 10px 12px; border-radius: 10px;
    border: 1px solid var(--border-hover); background: var(--bg-card);
    box-shadow: 0 8px 28px rgba(0, 0, 0, 0.3); font-size: 12px; color: var(--text-primary);
  }
  .notice-error { border-color: color-mix(in srgb, var(--alert) 55%, var(--border-hover)); }
  .head { display: flex; align-items: center; gap: 8px; }
  .mark { flex: none; font-size: 12px; }
  .title { flex: 1; min-width: 0; font-weight: 600; font-size: 13px; }
  ul { margin: 2px 0 0; padding: 0 8px 0 20px; color: var(--text-secondary); line-height: 1.5; }
  li { overflow-wrap: anywhere; }
</style>
