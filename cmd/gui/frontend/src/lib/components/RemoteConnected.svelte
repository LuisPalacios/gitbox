<script lang="ts">
  // The view of a remote window before its first connection, and when the
  // host has no config to show (#239, #240): who it talks to and how the
  // connection is doing, with Disconnect, Show log, Run doctor (in the
  // log) and Uninstall server. Simple mode says it in one plain sentence;
  // error codes appear only in the connection log.
  import { onMount, onDestroy } from 'svelte';
  import { fade } from 'svelte/transition';
  import { bridge, events } from '../bridge';
  import type { RemoteState } from '../bridge';
  import { t } from '../i18n';
  import { errorText } from '../notices';
  import ConnectionLog from './ConnectionLog.svelte';

  export let simple = false;
  // noConfig: connected, but the host has no gitbox.json to show (#240).
  export let noConfig = false;

  let st: RemoteState = { state: 'connecting', alias: '' };
  let lines: string[] = [];
  let showLog = false;
  let busy = false;
  let actionError = '';
  // Two-step confirmations, inline: the webview has no reliable confirm().
  let confirmUninstall = false;
  let othersLive = 0;
  let confirmReinstall = false;
  let uninstalled = false;
  const offs: (() => void)[] = [];

  $: working = st.state === 'connecting' || st.state === 'installing';
  $: headline =
    uninstalled ? $t('remote.uninstalled', { alias: st.alias })
    : st.state === 'connected' ? $t('remote.connectedTo', { alias: st.alias })
    : st.state === 'installing' ? $t('remote.installing', { alias: st.alias })
    : st.state === 'connecting' ? $t('remote.connecting', { alias: st.alias })
    : st.state === 'failed' ? $t('remote.failed', { alias: st.alias })
    : $t('remote.disconnected', { alias: st.alias });
  $: reason = st.state === 'failed' && st.hint ? $t(st.hint) : '';
  $: waiting = st.state === 'connecting' && st.waiting ? $t(`remote.waiting.${st.waiting}`) : '';

  onMount(async () => {
    offs.push(events.on('remote:state', (s: RemoteState) => { st = s; if (s.state === 'failed') showLog = showLog || !simple; }));
    offs.push(events.on('remote:log', (line: string) => { lines = [...lines.slice(-1999), line]; }));
    try {
      st = await bridge.remoteInfo();
      lines = await bridge.remoteLog();
    } catch (e) { console.error(e); }
  });
  onDestroy(() => offs.forEach((off) => off()));

  async function run(action: () => Promise<void>) {
    busy = true;
    actionError = '';
    try { await action(); } catch (e) { actionError = errorText(e); } finally { busy = false; }
  }

  const disconnect = () => run(() => bridge.remoteDisconnect());
  const reconnect = () => run(async () => { uninstalled = false; await bridge.remoteReconnect(); });

  function reinstall() {
    confirmReinstall = false;
    run(async () => {
      const res = await bridge.remoteReinstall();
      if (res.error) actionError = res.hint ? $t(res.hint) : res.error;
    });
  }

  function uninstall(force: boolean) {
    run(async () => {
      const res = await bridge.remoteUninstall(st.alias, force);
      if (res.error) { actionError = res.hint ? $t(res.hint) : res.error; confirmUninstall = false; return; }
      if (res.needsConfirm) { othersLive = res.otherClients; return; }
      confirmUninstall = false;
      othersLive = 0;
      uninstalled = res.done;
    });
  }
</script>

<div class="remote-window">
  <main class="remote-card" in:fade={{ duration: 180 }}>
    <div class="status-line">
      <span class="dot dot-{uninstalled ? 'disconnected' : st.state}" class:pulse={working} aria-hidden="true"></span>
      <h1>{headline}</h1>
    </div>
    {#if reason}<p class="reason">{reason}</p>{/if}
    {#if waiting}
      <div class="waiting" role="status" in:fade={{ duration: 180 }}>
        <span class="waiting-icon" aria-hidden="true">&#9888;</span>
        <span>{waiting}</span>
      </div>
    {/if}

    {#if st.state === 'connected' && !simple}
      <dl class="facts" translate="no">
        <dt>{$t('remote.fact.host')}</dt><dd>{st.alias}</dd>
        <dt>{$t('remote.fact.system')}</dt><dd>{st.serverOS}/{st.serverArch}</dd>
        <dt>{$t('remote.fact.server')}</dt><dd>{st.serverVersion}</dd>
      </dl>
    {/if}
    {#if st.state === 'connected' && noConfig}
      <p class="note">{$t('remote.noConfig', { alias: st.alias })}</p>
    {/if}

    {#if actionError}<p class="reason">{actionError}</p>{/if}

    <div class="actions">
      {#if st.state === 'connected' || working}
        <button class="btn" on:click={disconnect} disabled={busy}>{$t('remote.disconnect')}</button>
      {:else}
        <button class="btn btn-primary" on:click={reconnect} disabled={busy}>{$t('remote.reconnect')}</button>
      {/if}
      <button class="btn" on:click={() => (showLog = !showLog)}>{showLog ? $t('remote.hideLog') : $t('remote.showLog')}</button>
      {#if st.state === 'failed' && st.code === 'hash_mismatch'}
        <button class="btn" on:click={() => (confirmReinstall = true)} disabled={busy}>{$t('remote.reinstall')}</button>
      {/if}
      <button class="btn btn-quiet-danger" on:click={() => { confirmUninstall = true; othersLive = 0; }} disabled={busy || working}>{$t('remote.uninstall')}</button>
    </div>

    {#if confirmReinstall}
      <div class="confirm" transition:fade={{ duration: 120 }}>
        <p>{$t('remote.reinstallConfirm', { alias: st.alias })}</p>
        <button class="btn btn-danger" on:click={reinstall} disabled={busy}>{$t('remote.reinstall')}</button>
        <button class="btn" on:click={() => (confirmReinstall = false)}>{$t('common.cancel')}</button>
      </div>
    {/if}
    {#if confirmUninstall}
      <div class="confirm" transition:fade={{ duration: 120 }}>
        <p>{othersLive > 0 ? $t('remote.uninstallOthers', { n: othersLive }) : $t('remote.uninstallConfirm', { alias: st.alias })}</p>
        <button class="btn btn-danger" on:click={() => uninstall(othersLive > 0)} disabled={busy}>{$t('remote.uninstall')}</button>
        <button class="btn" on:click={() => { confirmUninstall = false; othersLive = 0; }}>{$t('common.cancel')}</button>
      </div>
    {/if}
  </main>

  {#if showLog}
    <ConnectionLog {lines} onClose={() => (showLog = false)} />
  {/if}
</div>

<style>
  .remote-window {
    height: 100vh; box-sizing: border-box; padding: 28px 28px 20px;
    display: flex; flex-direction: column; gap: 16px;
    background:
      radial-gradient(1200px 500px at 0% -10%, rgba(59, 130, 246, 0.10), transparent 60%),
      var(--bg-base);
    color: var(--text-primary);
  }
  .remote-card {
    border: 1px solid var(--border); border-radius: 14px; background: var(--bg-card);
    box-shadow: var(--card-shadow); padding: 22px 24px;
  }
  .status-line { display: flex; align-items: center; gap: 12px; }
  h1 { margin: 0; font-size: 20px; font-weight: 650; letter-spacing: -0.01em; }
  .dot { width: 11px; height: 11px; border-radius: 50%; flex-shrink: 0; background: var(--text-muted); }
  .dot-connected { background: #22c55e; box-shadow: 0 0 0 4px rgba(34, 197, 94, 0.18); }
  .dot-connecting, .dot-installing { background: #3b82f6; }
  .dot-failed { background: var(--alert); box-shadow: 0 0 0 4px rgba(216, 30, 91, 0.16); }
  .pulse { animation: pulse 1.2s ease-in-out infinite; }
  @keyframes pulse { 50% { box-shadow: 0 0 0 7px rgba(59, 130, 246, 0.0); opacity: 0.55; } 0%, 100% { box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.35); } }
  .reason { margin: 10px 0 0 23px; font-size: 13px; color: var(--alert); line-height: 1.5; }
  /* The colors of the global warnings (Banners.svelte): an action waits. */
  .waiting {
    display: flex; align-items: center; gap: 8px; margin: 12px 0 0 23px; padding: 8px 12px;
    background: #431407; border: 1px solid #c2410c; border-radius: 8px;
    font-size: 13px; color: #fdba74; line-height: 1.4;
  }
  :global([data-theme="light"]) .waiting { background: #fff7ed; border-color: #c2410c; color: #7c2d12; }
  .waiting-icon { font-size: 15px; flex-shrink: 0; }
  .facts {
    margin: 16px 0 0 23px; display: grid; grid-template-columns: max-content 1fr; gap: 4px 16px;
    font-size: 12px;
  }
  .facts dt { color: var(--text-muted); }
  .facts dd { margin: 0; font-family: 'JetBrains Mono', ui-monospace, Menlo, Consolas, monospace; color: var(--text-secondary); }
  .note { margin: 12px 0 0 23px; font-size: 12px; color: var(--text-muted); }
  .actions { margin: 20px 0 0 23px; display: flex; flex-wrap: wrap; gap: 8px; }
  .confirm {
    margin: 14px 0 0 23px; padding: 10px 12px; border: 1px solid var(--border); border-radius: 8px;
    display: flex; flex-wrap: wrap; align-items: center; gap: 8px; background: var(--bg-hover);
  }
  .confirm p { margin: 0 8px 0 0; flex-basis: 100%; font-size: 13px; color: var(--text-secondary); }
  .btn {
    padding: 6px 14px; font-size: 12px; font-weight: 500; border-radius: 7px; cursor: pointer;
    border: 1px solid var(--border); background: transparent; color: var(--text-primary);
    transition: background 0.12s, border-color 0.12s;
  }
  .btn:hover:not(:disabled) { background: var(--bg-hover); border-color: var(--border-hover); }
  .btn:disabled { opacity: 0.55; cursor: default; }
  .btn-primary { background: #2563eb; border-color: #2563eb; color: #fff; }
  .btn-primary:hover:not(:disabled) { background: #1d4ed8; border-color: #1d4ed8; }
  .btn-danger { background: #dc2626; border-color: #dc2626; color: #fff; }
  .btn-danger:hover:not(:disabled) { background: #b91c1c; }
  .btn-quiet-danger { color: var(--alert); }
</style>
