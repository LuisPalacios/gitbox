<script lang="ts">
  // The host switcher next to the title (#330): which computer this window
  // shows, and a menu that opens a remote host already added, goes back to
  // this computer, or opens the Remote hosts window. It adds no host: that
  // happens in the Remote hosts window. In a remote window the trigger is
  // the former badge of #240 (alias, system in Advanced, connection state),
  // and the menu also opens the connection log. A tinted remote window
  // fills that badge with its color (#338).
  import { onDestroy, tick } from 'svelte';
  import { fade, fly } from 'svelte/transition';
  import { bridge, events } from '../bridge';
  import type { RemoteResult, RemoteState, RemotesList } from '../bridge';
  import { t } from '../i18n';
  import { errorText } from '../notices';
  import ConnectionLog from './ConnectionLog.svelte';

  // state is this remote window's connection; null in the local window.
  export let state: RemoteState | null = null;
  export let simple = false;

  let open = false;
  let list: RemotesList = { remotes: [] };
  let message = '';
  let root: HTMLDivElement;
  let trigger: HTMLButtonElement;
  let menu: HTMLDivElement;

  let showLog = false;
  let lines: string[] = [];
  let offLog: (() => void) | null = null;
  // The Remote hosts window asks an open remote window for its log.
  const offShowLog = events.on('remote:show-log', () => { if (state) openLog(); });

  $: tip = state
    ? $t('remote.badgeTip', { alias: state.alias, os: state.serverOS || '?', state: $t(`remote.state.${state.state}`) })
      + (state.truncated ? '\n' + $t('remote.truncated') : '')
    : $t('switcher.localTip');
  $: hosts = list.remotes.map((r) => ({
    ...r,
    current: r.alias === list.current,
    status: r.alias === list.current || r.open
      ? $t(`remote.state.${r.state || 'connecting'}`)
      : $t('switcher.closed'),
  }));

  async function toggle() {
    if (open) { close(false); return; }
    message = '';
    try { list = await bridge.listRemotes(); } catch (e) { message = errorText(e); }
    open = true;
    await tick();
    menu?.querySelector<HTMLElement>('[role="menuitem"]')?.focus();
  }

  function close(refocus: boolean) {
    open = false;
    if (refocus) trigger?.focus();
  }

  async function act(call: () => Promise<RemoteResult>) {
    try {
      const res = await call();
      if (res.error) { message = res.hint ? $t(res.hint) : res.error; return; }
      close(false);
    } catch (e) { message = errorText(e); }
  }

  const chooseThisComputer = () => (state ? act(() => bridge.showLocalWindow()) : close(true));
  const chooseHost = (alias: string, current: boolean) => (current ? close(true) : act(() => bridge.connectRemote(alias)));
  const manage = () => act(() => bridge.openRemotesWindow());

  async function openLog() {
    close(false);
    showLog = true;
    if (!offLog) offLog = events.on('remote:log', (line: string) => { lines = [...lines.slice(-1999), line]; });
    try { lines = await bridge.remoteLog(); } catch (e) { console.error(e); }
  }

  function onKey(e: KeyboardEvent) {
    if (!open) return;
    if (e.key === 'Escape') { e.preventDefault(); close(true); return; }
    if (e.key === 'Tab') { close(false); return; }
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp' && e.key !== 'Home' && e.key !== 'End') return;
    const items = [...(menu?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? [])];
    if (!items.length) return;
    e.preventDefault();
    const at = items.indexOf(document.activeElement as HTMLElement);
    const next = e.key === 'Home' ? 0
      : e.key === 'End' ? items.length - 1
      : e.key === 'ArrowDown' ? (at + 1) % items.length
      : (at - 1 + items.length) % items.length;
    items[next].focus();
  }

  function onOutside(e: MouseEvent) {
    if (open && root && !root.contains(e.target as Node)) close(false);
  }

  onDestroy(() => { offLog?.(); offShowLog(); });
</script>

<svelte:window on:mousedown={onOutside} />

<div class="switcher" bind:this={root} on:keydown={onKey} role="presentation">
  {#if state}
    <button bind:this={trigger} class="trigger remote state-{state.state}" class:warn={state.truncated} title={tip} aria-label={tip}
      aria-haspopup="menu" aria-expanded={open} on:click={toggle}>
      <span class="dot" class:pulse={state.state === 'connecting' || state.state === 'installing'} aria-hidden="true"></span>
      <span class="alias" translate="no">{state.alias}</span>
      {#if !simple && state.serverOS}<span class="os" translate="no">{state.serverOS}</span>{/if}
      {#if state.truncated}<span class="trunc" aria-hidden="true" translate="no">!</span>{/if}
      <svg class="chev" viewBox="0 0 10 10" aria-hidden="true"><path d="M2 3.5l3 3 3-3" /></svg>
    </button>
  {:else}
    <button bind:this={trigger} class="trigger" title={tip} aria-label={tip} aria-haspopup="menu" aria-expanded={open} on:click={toggle}>
      <svg class="glyph" viewBox="0 0 16 16" aria-hidden="true"><rect x="2" y="3" width="12" height="8" rx="1.5" /><path d="M6 14h4M8 11v3" /></svg>
      <span class="alias">{$t('switcher.thisComputer')}</span>
      <svg class="chev" viewBox="0 0 10 10" aria-hidden="true"><path d="M2 3.5l3 3 3-3" /></svg>
    </button>
  {/if}

  {#if open}
    <div class="menu" role="menu" aria-label={$t('switcher.heading')} bind:this={menu} transition:fade={{ duration: 90 }}>
      <div class="heading">{$t('switcher.heading')}</div>
      <button class="item" class:current={!state} role="menuitem" on:click={chooseThisComputer}>
        <svg class="glyph" viewBox="0 0 16 16" aria-hidden="true"><rect x="2" y="3" width="12" height="8" rx="1.5" /><path d="M6 14h4M8 11v3" /></svg>
        <span class="two"><span class="name">{$t('switcher.thisComputer')}</span>{#if !state}<small>{$t('switcher.current')}</small>{/if}</span>
        {#if !state}<svg class="check" viewBox="0 0 16 16" aria-hidden="true"><path d="M3 8.5l3 3 7-7" /></svg>{/if}
      </button>
      {#each hosts as h (h.alias)}
        <button class="item" class:current={h.current} role="menuitem" on:click={() => chooseHost(h.alias, h.current)}>
          <svg class="glyph host" viewBox="0 0 16 16" aria-hidden="true"><rect x="2" y="2.5" width="12" height="4.5" rx="1" /><rect x="2" y="9" width="12" height="4.5" rx="1" /></svg>
          <span class="two"><span class="name mono" translate="no">{h.alias}</span><small>{h.current ? $t('switcher.current') : h.status}</small></span>
          {#if h.current}
            <svg class="check" viewBox="0 0 16 16" aria-hidden="true"><path d="M3 8.5l3 3 7-7" /></svg>
          {:else if h.open}
            <span class="live state-{h.state || 'connecting'}" aria-hidden="true"></span>
          {/if}
        </button>
      {:else}
        <div class="none">{$t('switcher.none')}</div>
      {/each}
      <div class="sep" role="separator"></div>
      {#if state}
        <button class="item quiet" role="menuitem" on:click={openLog}>
          <svg class="glyph" viewBox="0 0 16 16" aria-hidden="true"><path d="M3 3.5h10M3 6.5h10M3 9.5h7M3 12.5h5" /></svg>
          <span class="two"><span class="name">{$t('switcher.showLog')}</span></span>
        </button>
      {/if}
      <button class="item quiet" role="menuitem" on:click={manage}>
        <svg class="glyph" viewBox="0 0 16 16" aria-hidden="true"><path d="M2.5 4h11M2.5 8h11M2.5 12h11" /><circle cx="5.5" cy="4" r="1.4" /><circle cx="10.5" cy="8" r="1.4" /><circle cx="7" cy="12" r="1.4" /></svg>
        <span class="two"><span class="name">{$t('switcher.manage')}</span></span>
      </button>
      {#if message}<p class="msg" role="alert">{message}</p>{/if}
    </div>
  {/if}
</div>

{#if showLog}
  <div class="log-overlay" transition:fade={{ duration: 120 }}>
    <div class="log-drawer" transition:fly={{ x: 40, duration: 180 }}>
      <ConnectionLog {lines} onClose={() => (showLog = false)} />
    </div>
  </div>
{/if}

<style>
  .switcher { position: relative; }
  .trigger {
    display: inline-flex; align-items: center; gap: 7px; height: 26px; padding: 0 8px 0 9px;
    border: 1px solid var(--border); border-radius: 13px; background: var(--bg-card);
    color: var(--text-primary); font: inherit; font-size: 12px; cursor: pointer;
    transition: border-color 0.12s, background 0.12s;
  }
  .trigger:hover, .trigger[aria-expanded="true"] { border-color: var(--border-hover); background: var(--bg-hover); }
  .trigger:focus-visible, .item:focus-visible { outline: 1px solid var(--border-hover); outline-offset: 1px; }
  /* A remote window fills its alias badge with a solid shade of its color
     (#338), from the --tint-* colors App.svelte sets with the tint; until
     the tint arrives the badge keeps the neutral look above. The dot and
     the truncation mark get a ring, so green or red stay visible on it. */
  :global(html[data-remote-tint]) .trigger.remote {
    height: 28px; padding: 0 9px 0 10px; border-radius: 14px; font-size: 13px;
    background: var(--tint-chip-bg); border-color: var(--tint-chip-bg); color: var(--tint-chip-fg);
  }
  :global(html[data-remote-tint]) .trigger.remote:hover,
  :global(html[data-remote-tint]) .trigger.remote[aria-expanded="true"] { background: var(--tint-chip-hover); border-color: var(--tint-chip-hover); }
  :global(html[data-remote-tint]) .trigger.remote .os { color: var(--tint-chip-fg2); }
  :global(html[data-remote-tint]) .trigger.remote .chev { stroke: var(--tint-chip-fg2); }
  :global(html[data-remote-tint]) .trigger.remote .dot,
  :global(html[data-remote-tint]) .trigger.remote .trunc { box-shadow: 0 0 0 2px var(--tint-chip-fg); }
  .glyph { width: 14px; height: 14px; flex: none; fill: none; stroke: var(--text-secondary); stroke-width: 1.5; stroke-linecap: round; }
  .glyph.host { stroke: var(--text-workspace); }
  .glyph circle { fill: var(--bg-card); }
  .chev { width: 10px; height: 10px; flex: none; fill: none; stroke: var(--text-muted); stroke-width: 1.6; stroke-linecap: round; }
  .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--text-muted); flex: none; }
  .state-connected .dot { background: #22c55e; box-shadow: 0 0 0 3px rgba(34, 197, 94, 0.18); }
  .state-connecting .dot, .state-installing .dot { background: #3b82f6; }
  .state-failed .dot, .state-disconnected .dot { background: var(--alert); }
  .pulse { animation: pulse 1.2s ease-in-out infinite; }
  @keyframes pulse { 50% { opacity: 0.4; } }
  .alias { font-weight: 600; max-width: 180px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .remote .alias { font-family: 'JetBrains Mono', ui-monospace, Menlo, Consolas, monospace; }
  .os { color: var(--text-muted); font-family: 'JetBrains Mono', ui-monospace, Menlo, Consolas, monospace; font-size: 11px; }
  .trunc {
    width: 15px; height: 15px; border-radius: 50%; display: inline-flex; align-items: center; justify-content: center;
    font-size: 10px; font-weight: 700; color: #fff; background: var(--alert);
  }
  .menu {
    position: absolute; top: calc(100% + 6px); left: 0; z-index: 50; width: 300px; box-sizing: border-box; padding: 6px;
    display: flex; flex-direction: column; gap: 1px; background: var(--bg-card); border: 1px solid var(--border-hover);
    border-radius: 10px; box-shadow: 0 18px 40px rgba(0, 0, 0, 0.35);
  }
  .heading { font-size: 10px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.08em; color: var(--text-dim); padding: 8px 10px 6px; }
  .item {
    display: flex; align-items: center; gap: 10px; padding: 7px 10px; border: none; border-radius: 7px;
    background: none; color: var(--text-primary); font: inherit; font-size: 13px; text-align: left; cursor: pointer;
  }
  .item:hover, .item:focus-visible { background: var(--bg-hover); }
  .item.current { background: var(--bg-hover); }
  .item.quiet { color: var(--text-secondary); }
  .two { display: flex; flex-direction: column; flex: 1; min-width: 0; line-height: 1.3; }
  .name { font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .quiet .name { font-weight: 400; }
  .mono { font-family: 'JetBrains Mono', ui-monospace, Menlo, Consolas, monospace; font-size: 12px; }
  .two small { font-size: 11px; color: var(--text-muted); }
  .check { width: 14px; height: 14px; flex: none; fill: none; stroke: #22c55e; stroke-width: 2; stroke-linecap: round; stroke-linejoin: round; }
  .live { width: 8px; height: 8px; border-radius: 50%; flex: none; background: #3b82f6; }
  .live.state-connected { background: #22c55e; box-shadow: 0 0 0 3px rgba(34, 197, 94, 0.18); }
  .live.state-failed, .live.state-disconnected { background: var(--alert); }
  .none { padding: 6px 10px; font-size: 12px; color: var(--text-muted); }
  .sep { height: 1px; background: var(--border); margin: 5px 4px; }
  .msg { margin: 4px 10px; font-size: 11px; color: var(--alert); }
  .log-overlay { position: fixed; inset: 0; z-index: 60; background: var(--overlay); display: flex; justify-content: flex-end; }
  .log-drawer { width: 100%; height: 100%; padding: 16px; box-sizing: border-box; display: flex; flex-direction: column; }
</style>
