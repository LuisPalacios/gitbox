<script lang="ts">
  // The title-bar badge of a remote window (#240): which host this window
  // shows, its system and how the connection is doing, and that it is
  // limited: some actions are not served yet (#294). Simple mode says only
  // the host and "Limited". A click opens the connection log over the
  // window.
  import { onDestroy } from 'svelte';
  import { fade, fly } from 'svelte/transition';
  import { bridge, events } from '../bridge';
  import type { RemoteState } from '../bridge';
  import { t } from '../i18n';
  import ConnectionLog from './ConnectionLog.svelte';

  export let state: RemoteState;
  export let simple = false;

  let showLog = false;
  let lines: string[] = [];
  let offLog: (() => void) | null = null;

  $: tip = $t('remote.badgeTip', { alias: state.alias, os: state.serverOS || '?', state: $t(`remote.state.${state.state}`) })
    + (state.truncated ? '\n' + $t('remote.truncated') : '');

  async function openLog() {
    showLog = true;
    if (!offLog) offLog = events.on('remote:log', (line: string) => { lines = [...lines.slice(-1999), line]; });
    try { lines = await bridge.remoteLog(); } catch (e) { console.error(e); }
  }

  onDestroy(() => offLog?.());
</script>

<button class="remote-badge state-{state.state}" class:warn={state.truncated} title={tip} aria-label={tip} on:click={openLog}>
  <span class="dot" class:pulse={state.state === 'connecting' || state.state === 'installing'} aria-hidden="true"></span>
  <span class="alias" translate="no">{state.alias}</span>
  {#if !simple && state.serverOS}<span class="os" translate="no">{state.serverOS}</span>{/if}
  <span class="ro">{$t('remote.limited')}</span>
  {#if state.truncated}<span class="trunc" aria-hidden="true" translate="no">!</span>{/if}
</button>

{#if showLog}
  <div class="log-overlay" transition:fade={{ duration: 120 }}>
    <div class="log-drawer" transition:fly={{ x: 40, duration: 180 }}>
      <ConnectionLog {lines} onClose={() => (showLog = false)} />
    </div>
  </div>
{/if}

<style>
  .remote-badge {
    display: inline-flex; align-items: center; gap: 7px; height: 26px; padding: 0 10px 0 9px;
    border: 1px solid var(--border); border-radius: 13px; background: var(--bg-card);
    color: var(--text-primary); font: inherit; font-size: 12px; cursor: pointer;
    transition: border-color 0.12s, background 0.12s;
  }
  .remote-badge:hover { border-color: var(--border-hover); background: var(--bg-hover); }
  .remote-badge:focus-visible { outline: 1px solid var(--border-hover); outline-offset: 2px; }
  .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--text-muted); flex: none; }
  .state-connected .dot { background: #22c55e; box-shadow: 0 0 0 3px rgba(34, 197, 94, 0.18); }
  .state-connecting .dot, .state-installing .dot { background: #3b82f6; }
  .state-failed .dot, .state-disconnected .dot { background: var(--alert); }
  .pulse { animation: pulse 1.2s ease-in-out infinite; }
  @keyframes pulse { 50% { opacity: 0.4; } }
  .alias { font-weight: 600; max-width: 180px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .os { color: var(--text-muted); font-family: 'JetBrains Mono', ui-monospace, Menlo, Consolas, monospace; font-size: 11px; }
  .ro {
    padding: 1px 6px; border-radius: 6px; font-size: 10px; font-weight: 600; letter-spacing: 0.04em; text-transform: uppercase;
    color: #b45309; background: rgba(245, 158, 11, 0.14);
  }
  .trunc {
    width: 15px; height: 15px; border-radius: 50%; display: inline-flex; align-items: center; justify-content: center;
    font-size: 10px; font-weight: 700; color: #fff; background: var(--alert);
  }
  .log-overlay { position: fixed; inset: 0; z-index: 60; background: var(--overlay); display: flex; justify-content: flex-end; }
  .log-drawer { width: min(640px, 92vw); height: 100%; padding: 16px; box-sizing: border-box; display: flex; flex-direction: column; }
</style>
