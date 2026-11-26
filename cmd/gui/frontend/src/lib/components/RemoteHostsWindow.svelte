<script lang="ts">
  // The Remote hosts window (#330): a window of its own that lists the
  // remote hosts of this computer and manages them. It took over the
  // Remotes block of Settings and keeps every action it had: add (now with
  // suggestions from the ssh configuration), connect, editor, uninstall the
  // server and remove; it adds the host's facts and its connection log.
  // Uninstall and Remove live in the context menu of the list, which acts on
  // every selected host.
  import { onMount, tick } from 'svelte';
  import { bridge, events } from '../bridge';
  import type { RemoteResult, RemotesList, SSHHost } from '../bridge';
  import { configStore, themeStore } from '../stores';
  import { TINT_PRESETS, DEFAULT_TINT, tintOf, tintSurfaces } from '../tint';
  import { languageStore, t } from '../i18n';
  import { errorText } from '../notices';
  import { remoteEditorChoices, editorName } from '../remotehost';
  import type { EditorInfo } from '../types';
  import { tooltip } from '../tooltip';

  let remotes: RemotesList = { remotes: [] };
  // The color of remote windows' tint (#331), and what it paints in the
  // current theme, for the preview.
  let tint = DEFAULT_TINT;
  $: preview = tintSurfaces(tint, $themeStore);
  let suggestions: SSHHost[] = [];
  // The ssh lines arrive after the configured hosts, so the window never
  // waits for them.
  let sshLoading = true;
  // query is what I typed in the alias field, without the completion the
  // field proposes after it.
  let query = '';
  let aliasInput: HTMLInputElement;
  // selected is the host the details show: the last one I picked. chosen
  // is every selected host: a click selects one, ⌘-click (Ctrl elsewhere)
  // adds or drops one, Shift-click selects a range from the anchor.
  let selected = '';
  let chosen: string[] = [];
  let anchor = '';
  const isMac = navigator.userAgent.includes('Mac');
  // The context menu of the selection, where it opened.
  let menu: { x: number; y: number; targets: string[] } | null = null;
  let menuEl: HTMLDivElement | undefined;
  let newAlias = '';
  let newServerDir = '';
  let busy = '';
  let msg = '';
  let msgBad = false;
  // The inline uninstall confirmation of the hosts the menu acted on.
  // others are the hosts where other clients still use the server, with how
  // many: the second question.
  let uninstallAsk: { targets: string[]; others: { alias: string; n: number }[] } | null = null;
  // The inline confirmation of Remove: the hosts it would forget.
  let removeAsk: string[] | null = null;

  $: current = remotes.remotes.find((r) => r.alias === selected) || null;
  // The selected hosts in list order.
  $: targets = remotes.remotes.map((r) => r.alias).filter((a) => chosen.includes(a));
  $: menuHost = menu?.targets.length === 1 ? remotes.remotes.find((r) => r.alias === menu?.targets[0]) || null : null;
  // The editors a remote can open folders in (#287): the configured ones
  // whose command opens remote folders.
  $: editorOptions = remoteEditorChoices(($configStore?.global?.editors || []) as EditorInfo[], remotes.remoteEditors || []);
  $: stateText = (r: RemotesList['remotes'][number]) =>
    !r.open ? $t('switcher.closed')
    : waitingOf(r) ? $t('remotesWindow.waitingApproval')
    : $t(`remote.state.${r.state || 'connecting'}`);
  // The login of a host that is still connecting may wait for the ssh agent:
  // 1Password, for one, asks for approval in its menu bar icon. The window
  // of that host says so, but I may be looking at this one.
  const waitingOf = (r: RemotesList['remotes'][number]) => (r.open && r.state === 'connecting' && r.waiting) || '';
  $: waitingText = current && waitingOf(current) ? $t(`remote.waiting.${waitingOf(current)}`) : '';
  $: when = (iso?: string) => {
    if (!iso) return $t('remotesWindow.never');
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? iso : d.toLocaleString($languageStore);
  };

  export async function load() {
    loadSSH();
    try { remotes = await bridge.listRemotes(); } catch (e) { say({ error: errorText(e) }, ''); }
    const aliases = remotes.remotes.map((r) => r.alias);
    chosen = chosen.filter((a) => aliases.includes(a));
    if (!aliases.includes(selected)) selected = chosen.at(-1) || aliases[0] || '';
    if (!chosen.length && selected) chosen = [selected];
  }

  // Only the hosts, without the ssh lines: what the quick refresh reads.
  async function loadHosts() {
    try { remotes = await bridge.listRemotes(); } catch (e) { console.error(e); }
  }

  // After Connect, and while a host connects or installs, the states are
  // read every second, so the window that opens and a login that waits for
  // approval show here at once.
  let watchUntil = 0;
  $: settling = remotes.remotes.some((r) => r.open && (r.state === 'connecting' || r.state === 'installing'));

  async function loadSSH() {
    try { suggestions = await bridge.listSSHHosts(); } catch (e) { console.error(e); } finally { sshLoading = false; }
  }

  // tintChoices counts my choices, so a load that answers after one of them
  // never puts the old color back.
  let tintChoices = 0;
  async function loadTint() {
    const seen = tintChoices;
    try {
      const saved = tintOf(await bridge.getRemoteTint());
      if (seen === tintChoices) tint = saved;
    } catch (e) { console.error(e); }
  }

  // Choosing shows at once; a refusal puts the saved color back. Open remote
  // windows pick the new color up by themselves.
  async function chooseTint(color: string) {
    if (color === tint) return;
    tintChoices++;
    const before = tint;
    tint = color;
    try {
      const res = await bridge.setRemoteTint(color);
      if (res.error) { tint = before; say(res, ''); }
    } catch (e) { tint = before; say({ error: errorText(e) }, ''); }
  }

  onMount(() => {
    load();
    loadTint();
    const refresh = () => load();
    window.addEventListener('focus', refresh);
    // While the window shows, a refresh every minute keeps the states of
    // the hosts current and tells the process the window is in use: hidden
    // for ten minutes without one, it ends (#330).
    const beat = setInterval(() => { if (document.visibilityState === 'visible') load(); }, 60_000);
    const quick = setInterval(() => { if (busy === '' && (settling || Date.now() < watchUntil)) loadHosts(); }, 1_000);
    // An uninstall whose login waits for the ssh agent says so here, with
    // the host, until the login goes through or its limit ends it (#336).
    const offWaiting = events.on('remote:waiting', (w) => {
      if (busy !== 'uninstall') return;
      msg = `${w.alias}: ${$t(w.waiting === '1password' ? 'remote.waiting.1password' : 'remote.waiting.agent')}`;
      msgBad = false;
    });
    return () => { window.removeEventListener('focus', refresh); clearInterval(beat); clearInterval(quick); offWaiting(); };
  });

  function say(res: RemoteResult, ok: string): boolean {
    if (res.error) {
      msg = res.hint ? $t(res.hint) : res.error;
      msgBad = true;
      return false;
    }
    msg = ok;
    msgBad = false;
    return true;
  }

  async function run(tag: string, action: () => Promise<boolean>) {
    busy = tag;
    try { if (await action()) await load(); } catch (e) { say({ error: errorText(e) }, ''); } finally { busy = ''; }
  }

  const add = () => run('add', async () => {
    const alias = newAlias.trim();
    if (!say(await bridge.addRemote(alias, newServerDir), $t('settings.remotes.added', { alias }))) return false;
    newAlias = '';
    newServerDir = '';
    query = '';
    selected = alias;
    return true;
  });
  const connect = (alias: string, open: boolean) => run('connect', async () => {
    if (!say(await bridge.connectRemote(alias), open ? '' : $t('settings.remotes.opening', { alias }))) return false;
    watchUntil = Date.now() + 15_000;
    return true;
  });
  const showLog = (alias: string) => run('log', async () => say(await bridge.showRemoteLog(alias), ''));
  const setEditor = (alias: string, editor: string) => run('editor', async () =>
    say(await bridge.setRemoteEditor(alias, editor), $t('settings.remotes.editorSaved', { alias })));

  // report says what a batch did: the hosts it did, in one sentence, and
  // each failure with its host.
  function report(done: string[], failed: string[], one: string, many: string) {
    const ok = done.length === 1 ? $t(one, { alias: done[0] }) : done.length ? $t(many, { list: done.join(', ') }) : '';
    msg = [ok, ...failed].filter(Boolean).join(' ');
    msgBad = failed.length > 0;
  }
  const failure = (alias: string, res: RemoteResult) => `${alias}: ${res.hint ? $t(res.hint) : res.error}`;

  function askRemove(aliases: string[]) {
    menu = null;
    msg = '';
    uninstallAsk = null;
    removeAsk = aliases;
  }

  const removeAll = (aliases: string[]) => run('remove', async () => {
    removeAsk = null;
    const done: string[] = [];
    const failed: string[] = [];
    for (const alias of aliases) {
      const res = await bridge.removeRemote(alias);
      if (res.error) failed.push(failure(alias, res)); else done.push(alias);
    }
    chosen = chosen.filter((a) => !done.includes(a));
    report(done, failed, 'settings.remotes.removed', 'remotesWindow.removedMany');
    return done.length > 0;
  });

  function askUninstall(aliases: string[]) {
    menu = null;
    msg = '';
    removeAsk = null;
    uninstallAsk = { targets: aliases, others: [] };
  }

  // The first answer uninstalls from every host the menu named; the hosts
  // where other clients still use the server come back as a second
  // question, and its answer forces it there.
  const uninstallAll = () => run('uninstall', async () => {
    const ask = uninstallAsk;
    if (!ask) return false;
    const force = ask.others.length > 0;
    const done: string[] = [];
    const failed: string[] = [];
    const others: { alias: string; n: number }[] = [];
    for (const alias of force ? ask.others.map((o) => o.alias) : ask.targets) {
      const res = await bridge.remoteUninstall(alias, force);
      if (res.error) failed.push(failure(alias, res));
      else if (res.needsConfirm) others.push({ alias, n: res.otherClients });
      else done.push(alias);
    }
    uninstallAsk = others.length ? { targets: others.map((o) => o.alias), others } : null;
    report(done, failed, 'settings.remotes.uninstalled', 'remotesWindow.uninstalledMany');
    return done.length > 0;
  });

  $: uninstallQuestion = !uninstallAsk ? ''
    : uninstallAsk.others.length === 0
      ? (uninstallAsk.targets.length === 1
        ? $t('remote.uninstallConfirm', { alias: uninstallAsk.targets[0] })
        : $t('remotesWindow.uninstallMany', { list: uninstallAsk.targets.join(', ') }))
      : (uninstallAsk.others.length === 1
        ? `${uninstallAsk.others[0].alias}: ${$t('remote.uninstallOthers', { n: uninstallAsk.others[0].n })}`
        : $t('remotesWindow.uninstallOthersMany', { list: uninstallAsk.others.map((o) => `${o.alias} (${o.n})`).join(', ') }));

  // The ssh lines that contain what I typed, in any of their names.
  $: shown = query.trim()
    ? suggestions.filter((h) => h.names.some((n) => n.toLowerCase().includes(query.trim().toLowerCase())))
    : suggestions;

  // Inline completion: typing proposes the rest of the first alias that
  // starts with what I typed, selected, so more typing replaces it and Tab
  // or the right arrow accepts it. Deleting never completes.
  function onAliasInput(e: Event) {
    const el = e.currentTarget as HTMLInputElement;
    query = el.value;
    newAlias = el.value;
    if (!(e as InputEvent).inputType?.startsWith('insert') || !query) return;
    const typed = query.toLowerCase();
    const match = suggestions.find((h) => h.alias.toLowerCase().startsWith(typed) && h.alias.length > query.length);
    if (!match) return;
    el.value = query + match.alias.slice(query.length);
    el.setSelectionRange(query.length, el.value.length);
    newAlias = el.value;
  }

  function onAliasKey(e: KeyboardEvent) {
    const el = e.currentTarget as HTMLInputElement;
    const proposing = el.selectionStart !== null && el.selectionEnd === el.value.length && el.selectionStart < el.value.length;
    if (proposing && (e.key === 'Tab' || e.key === 'ArrowRight')) {
      e.preventDefault();
      el.setSelectionRange(el.value.length, el.value.length);
      query = el.value;
    }
  }

  function useSuggestion(h: SSHHost) {
    newAlias = h.alias;
    query = '';
    aliasInput?.focus();
  }

  function pick(alias: string, e?: MouseEvent | KeyboardEvent) {
    const aliases = remotes.remotes.map((r) => r.alias);
    if (e?.shiftKey && anchor && aliases.includes(anchor)) {
      const [from, to] = [aliases.indexOf(anchor), aliases.indexOf(alias)].sort((x, y) => x - y);
      chosen = aliases.slice(from, to + 1);
    } else if (e && (isMac ? e.metaKey : e.ctrlKey)) {
      // The last selected host stays: the details always show one.
      if (!chosen.includes(alias)) chosen = [...chosen, alias];
      else if (chosen.length > 1) chosen = chosen.filter((a) => a !== alias);
      anchor = alias;
    } else {
      chosen = [alias];
      anchor = alias;
    }
    selected = chosen.includes(alias) ? alias : chosen.at(-1) || alias;
    uninstallAsk = null;
    removeAsk = null;
    msg = '';
  }

  // The context menu acts on the selection; a right-click outside it
  // selects that host alone first, as file managers do.
  async function openMenu(alias: string, x: number, y: number) {
    if (!chosen.includes(alias)) pick(alias);
    // From chosen itself: the reactive targets update only after this call.
    menu = { x, y, targets: remotes.remotes.map((r) => r.alias).filter((a) => chosen.includes(a)) };
    await tick();
    if (!menuEl) return;
    const box = menuEl.getBoundingClientRect();
    menu = { ...menu, x: Math.max(4, Math.min(x, window.innerWidth - box.width - 8)), y: Math.max(4, Math.min(y, window.innerHeight - box.height - 8)) };
    menuItems()[0]?.focus();
  }

  function onHostMenu(e: MouseEvent, alias: string) {
    e.preventDefault();
    openMenu(alias, e.clientX, e.clientY);
  }

  // The menu key and Shift+F10 open it from the keyboard, under the row.
  function onHostKey(e: KeyboardEvent, alias: string) {
    if (e.key !== 'ContextMenu' && !(e.shiftKey && e.key === 'F10')) return;
    e.preventDefault();
    const row = (e.currentTarget as HTMLElement).getBoundingClientRect();
    openMenu(alias, row.left + 24, row.bottom);
  }

  const menuItems = () => Array.from(menuEl?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not(:disabled)') || []);

  function onMenuKey(e: KeyboardEvent) {
    const items = menuItems();
    const at = items.indexOf(document.activeElement as HTMLButtonElement);
    if (e.key === 'Escape') { e.preventDefault(); menu = null; }
    else if (e.key === 'ArrowDown') { e.preventDefault(); items[(at + 1) % items.length]?.focus(); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); items[(at - 1 + items.length) % items.length]?.focus(); }
  }

  function onWindowDown(e: MouseEvent) {
    if (menu && menuEl && !menuEl.contains(e.target as Node)) menu = null;
  }

  function fromMenu(action: () => void) {
    menu = null;
    action();
  }
</script>

<div class="rh">
  <aside class="rh-side">
    <header class="rh-head">
      <h1>{$t('remotesWindow.title')}</h1>
      <p>{$t('remotesWindow.intro')}</p>
    </header>
    <div class="rh-list" role="listbox" aria-label={$t('remotesWindow.title')} aria-multiselectable="true">
      {#each remotes.remotes as r (r.alias)}
        <button class="rh-host" class:selected={chosen.includes(r.alias)} class:current={r.alias === selected} role="option" aria-selected={chosen.includes(r.alias)}
          on:click={(e) => pick(r.alias, e)} on:contextmenu={(e) => onHostMenu(e, r.alias)} on:keydown={(e) => onHostKey(e, r.alias)}>
          <svg class="glyph" viewBox="0 0 16 16" aria-hidden="true"><rect x="2" y="2.5" width="12" height="4.5" rx="1" /><rect x="2" y="9" width="12" height="4.5" rx="1" /></svg>
          <span class="two"><span class="alias" translate="no">{r.alias}</span><small>{stateText(r)}</small></span>
          {#if r.open}<span class="live state-{r.state || 'connecting'}" aria-hidden="true"></span>{/if}
        </button>
      {:else}
        <p class="rh-none">{$t('remotesWindow.none')}</p>
      {/each}
    </div>
    {#if remotes.remotes.length}
      <p class="rh-hint">{$t('remotesWindow.hint', { mod: isMac ? '⌘' : 'Ctrl' })}</p>
    {/if}
    <form class="rh-add" on:submit|preventDefault={add}>
      <span class="rh-label">{$t('remotesWindow.addHeading')}</span>
      <div class="rh-add-row">
        <input class="rh-input mono" bind:this={aliasInput} value={newAlias} on:input={onAliasInput} on:keydown={onAliasKey}
          placeholder={$t('settings.remotes.aliasPlaceholder')} aria-label={$t('settings.remotes.alias')} spellcheck="false" autocapitalize="off" autocomplete="off" />
        <button class="btn btn-action" type="submit" disabled={busy !== '' || !newAlias.trim()}>
          {busy === 'add' ? $t('settings.remotes.checking') : $t('settings.remotes.add')}
        </button>
      </div>
      <input class="rh-input mono" bind:value={newServerDir} placeholder={$t('settings.remotes.serverDirPlaceholder')} aria-label={$t('settings.remotes.serverDir')} spellcheck="false" autocapitalize="off" />
      {#if sshLoading}
        <span class="rh-label">{$t('remotesWindow.suggestions')}</span>
        <p class="ssh-none" role="status">{$t('remotesWindow.readingSSH')}</p>
      {:else if suggestions.length}
        <span class="rh-label">{$t('remotesWindow.suggestions')}</span>
        <ul class="rh-ssh" aria-label={$t('remotesWindow.suggestions')}>
          {#each shown as h (h.alias)}
            <li>
              <button type="button" class="ssh-line mono" class:chosen={h.alias === newAlias} title={h.names.join(' ')} translate="no" on:click={() => useSuggestion(h)}>
                {h.names.join(' ')}
              </button>
            </li>
          {:else}
            <li class="ssh-none">{$t('remotesWindow.noMatch')}</li>
          {/each}
        </ul>
      {/if}
    </form>
  </aside>

  <main class="rh-main">
    {#if targets.length > 1}
      <div class="rh-title"><h2>{$t('remotesWindow.selectedMany', { n: targets.length })}</h2></div>
      <ul class="many" translate="no">
        {#each targets as a (a)}<li>{a}</li>{/each}
      </ul>
      <p class="rh-note">{$t('remotesWindow.manyHint')}</p>
    {:else if current}
      <div class="rh-title">
        <h2 translate="no">{current.alias}</h2>
        <span class="pill"><span class="live state-{current.open ? current.state || 'connecting' : 'closed'}" aria-hidden="true"></span>{stateText(current)}</span>
        <button class="btn btn-primary" on:click={() => current && connect(current.alias, !!current.open)} disabled={busy !== ''}>
          {current.open ? $t('remotesWindow.goTo') : $t('settings.remotes.connect')}
        </button>
      </div>

      {#if waitingText}
        <div class="waiting" role="status">
          <span class="waiting-icon" aria-hidden="true">&#9888;</span>
          <span>{waitingText}</span>
        </div>
      {/if}

      <dl class="facts">
        <div><dt>{$t('remotesWindow.system')}</dt><dd translate="no">{current.serverOS ? `${current.serverOS}/${current.serverArch || '?'}` : $t('remotesWindow.unknown')}</dd></div>
        <div><dt>{$t('remotesWindow.server')}</dt><dd translate="no">{current.serverVersion || $t('remotesWindow.unknown')}</dd></div>
        <div><dt>{$t('remotesWindow.lastConnected')}</dt><dd>{when(current.lastConnected)}</dd></div>
        <div><dt>{$t('settings.remotes.serverDir')}</dt><dd translate="no">{current.serverDir || $t('remotesWindow.serverDirDefault')}</dd></div>
      </dl>

      <section class="prefs">
        <label class="pref">
          <span use:tooltip={$t('settings.remotes.editorTip')}>{$t('settings.remotes.editor')}</span>
          <select class="rh-input" value={current.editor || ''} disabled={busy !== ''} on:change={(e) => current && setEditor(current.alias, e.currentTarget.value)}>
            <option value="">{$t('settings.remotes.editorAuto')}</option>
            {#each editorOptions as ed (ed.command)}
              <option value={editorName(ed.command)}>{ed.name}</option>
            {/each}
            {#if current.editor && !editorOptions.some((ed) => editorName(ed.command) === current?.editor)}
              <option value={current.editor}>{$t('settings.remotes.editorMissing', { editor: current.editor })}</option>
            {/if}
          </select>
        </label>
      </section>

      <div class="actions">
        <span use:tooltip={current.open ? '' : $t('remotesWindow.logClosedTip')}>
          <button class="btn" on:click={() => current && showLog(current.alias)} disabled={busy !== '' || !current.open}>{$t('remotesWindow.showLog')}</button>
        </span>
      </div>
    {:else}
      <p class="rh-empty">{$t('remotesWindow.none')}</p>
    {/if}
    {#if uninstallAsk}
      <div class="confirm" role="group" aria-label={$t('remotesWindow.menu.uninstall')}>
        <span>{uninstallQuestion}</span>
        <button class="btn btn-danger" on:click={uninstallAll} disabled={busy !== ''}>
          {busy === 'uninstall' ? $t('settings.remotes.working') : $t('settings.remotes.uninstall')}
        </button>
        <button class="btn" on:click={() => (uninstallAsk = null)}>{$t('common.cancel')}</button>
      </div>
    {/if}
    {#if removeAsk}
      {@const forget = removeAsk}
      <div class="confirm" role="group" aria-label={$t('remotesWindow.menu.remove')}>
        <span>{forget.length === 1 ? $t('remotesWindow.removeConfirm', { alias: forget[0] }) : $t('remotesWindow.removeConfirmMany', { list: forget.join(', ') })}</span>
        <button class="btn btn-danger" on:click={() => removeAll(forget)} disabled={busy !== ''}>
          {busy === 'remove' ? $t('settings.remotes.working') : $t('remotesWindow.remove')}
        </button>
        <button class="btn" on:click={() => (removeAsk = null)}>{$t('common.cancel')}</button>
      </div>
    {/if}
    {#if msg}<p class="msg" class:bad={msgBad} role="status">{msg}</p>{/if}

    <section class="tint" aria-labelledby="tint-heading">
      <div class="tint-head">
        <h3 id="tint-heading">{$t('remotesWindow.tint.title')}</h3>
        <p>{$t('remotesWindow.tint.help')}</p>
      </div>
      <div class="tint-body">
        <div class="swatches" role="radiogroup" aria-labelledby="tint-heading">
          {#each TINT_PRESETS as p (p.color)}
            <button class="swatch" role="radio" aria-checked={p.color === tint} aria-label={$t(`remotesWindow.tint.${p.key}`)}
              title={$t(`remotesWindow.tint.${p.key}`)} style="--sw: {p.color}" on:click={() => chooseTint(p.color)}></button>
          {/each}
          <button class="btn reset" on:click={() => chooseTint(DEFAULT_TINT)} disabled={tint === DEFAULT_TINT}>{$t('remotesWindow.tint.reset')}</button>
        </div>
        <div class="tint-preview" aria-label={$t('remotesWindow.tint.preview')} role="img">
          <div class="pv-top" style="background: {preview.top}; border-color: {preview.border}; color: {preview.primary}">
            <span class="pv-dot" style="background: {tint}"></span><span translate="no">gitbox · {current?.alias || 'host'}</span>
          </div>
          <div class="pv-row">
            <div class="pv-side" style="background: {preview.side}; border-color: {preview.border}">
              <span style="color: {preview.dim}">{$t('remotesWindow.tint.pvSection')}</span>
              <span style="color: {preview.primary}">{$t('remotesWindow.tint.pvOne')}</span>
              <span style="color: {preview.secondary}">{$t('remotesWindow.tint.pvTwo')}</span>
            </div>
            <div class="pv-content"></div>
          </div>
        </div>
      </div>
    </section>
  </main>
</div>

<svelte:window on:mousedown={onWindowDown} on:blur={() => (menu = null)} on:resize={() => (menu = null)} />

{#if menu}
  <div class="ctx" role="menu" tabindex="-1" bind:this={menuEl} style="left: {menu.x}px; top: {menu.y}px"
    aria-label={menu.targets.length === 1 ? $t('remotesWindow.menu.label', { alias: menu.targets[0] }) : $t('remotesWindow.menu.labelMany', { n: menu.targets.length })}
    on:keydown={onMenuKey}>
    {#if menuHost}
      {@const host = menuHost}
      <button class="ctx-item" role="menuitem" on:click={() => fromMenu(() => connect(host.alias, !!host.open))} disabled={busy !== ''}>
        <span class="ctx-name">{host.open ? $t('remotesWindow.goTo') : $t('settings.remotes.connect')}</span>
      </button>
      <button class="ctx-item" role="menuitem" on:click={() => fromMenu(() => showLog(host.alias))} disabled={busy !== '' || !host.open}>
        <span class="ctx-name">{$t('remotesWindow.showLog')}</span>
      </button>
      <div class="ctx-sep" role="separator"></div>
    {/if}
    <button class="ctx-item" role="menuitem" on:click={() => menu && askUninstall(menu.targets)} disabled={busy !== ''}>
      <span class="ctx-name danger">{$t('remotesWindow.menu.uninstall')}</span>
      <span class="ctx-help">{menu.targets.length === 1 ? $t('remotesWindow.menu.uninstallHelp', { alias: menu.targets[0] }) : $t('remotesWindow.menu.uninstallHelpMany', { n: menu.targets.length })}</span>
    </button>
    <button class="ctx-item" role="menuitem" on:click={() => menu && askRemove(menu.targets)} disabled={busy !== ''}>
      <span class="ctx-name">{$t('remotesWindow.menu.remove')}</span>
      <span class="ctx-help">{menu.targets.length === 1 ? $t('remotesWindow.menu.removeHelp', { alias: menu.targets[0] }) : $t('remotesWindow.menu.removeHelpMany', { n: menu.targets.length })}</span>
    </button>
  </div>
{/if}

<style>
  .rh { position: fixed; inset: 0; display: flex; background: var(--bg-base); color: var(--text-primary); font-size: 13px; }
  .rh-side { width: 300px; flex: none; display: flex; flex-direction: column; background: var(--bg-card); border-right: 1px solid var(--border); }
  .rh-head { padding: 20px 18px 10px; }
  .rh-head h1 { margin: 0; font-size: 17px; font-weight: 700; display: flex; align-items: center; gap: 8px; }
  .rh-head p { margin: 4px 0 0; font-size: 12px; color: var(--text-muted); line-height: 1.45; }
  .rh-list { flex: 1; overflow-y: auto; padding: 4px 8px; display: flex; flex-direction: column; gap: 2px; }
  .rh-host {
    display: flex; align-items: center; gap: 10px; padding: 9px 10px; border: none; border-radius: 8px;
    background: none; color: var(--text-primary); font: inherit; text-align: left; cursor: pointer;
  }
  .rh-host:hover { background: var(--bg-hover); }
  .rh-host.selected { background: var(--bg-hover); }
  .rh-host.current { box-shadow: inset 0 0 0 1px var(--border-hover); }
  .rh-hint { margin: 2px 18px 10px; font-size: 11px; color: var(--text-dim); line-height: 1.4; }
  .rh-host:focus-visible, .btn:focus-visible, .ssh-line:focus-visible { outline: 1px solid var(--border-hover); outline-offset: 1px; }
  .glyph { width: 18px; height: 18px; flex: none; fill: none; stroke: var(--text-workspace); stroke-width: 1.4; }
  .two { display: flex; flex-direction: column; flex: 1; min-width: 0; line-height: 1.35; }
  .two small { font-size: 11px; color: var(--text-muted); }
  .alias, .mono { font-family: 'JetBrains Mono', ui-monospace, Menlo, Consolas, monospace; font-size: 12px; }
  .alias { font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .live { width: 8px; height: 8px; border-radius: 50%; flex: none; background: #3b82f6; }
  .live.state-connected { background: #22c55e; box-shadow: 0 0 0 3px rgba(34, 197, 94, 0.18); }
  .live.state-failed, .live.state-disconnected { background: var(--alert); }
  .live.state-closed { background: var(--text-dim); }
  .rh-none, .rh-empty { margin: 8px 10px; font-size: 12px; color: var(--text-muted); }
  .rh-add { padding: 14px 16px; border-top: 1px solid var(--border); display: flex; flex-direction: column; gap: 8px; }
  .rh-label { font-size: 11px; font-weight: 600; color: var(--text-muted); }
  .rh-add-row { display: flex; gap: 6px; }
  .rh-add-row .rh-input { flex: 1; min-width: 0; }
  .rh-input {
    height: 30px; box-sizing: border-box; padding: 0 10px; border: 1px solid var(--border-hover); border-radius: 6px;
    background: var(--bg-base); color: var(--text-primary); font-size: 12px;
  }
  /* One line per Host entry of the ssh configuration, cut with an ellipsis
     so it never wraps; the full line is in the tooltip. */
  .rh-ssh {
    margin: 0; padding: 2px; list-style: none; display: flex; flex-direction: column; gap: 1px;
    max-height: 220px; overflow-y: auto; border: 1px solid var(--border); border-radius: 6px; background: var(--bg-base);
  }
  .ssh-line {
    display: block; width: 100%; box-sizing: border-box; padding: 4px 8px; border: none; border-radius: 4px;
    background: none; color: var(--text-secondary); text-align: left; cursor: pointer;
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  /* The ssh lines are a reference, smaller than the hosts: 10 of the 12 px of .mono. */
  .ssh-line.mono { font-size: 10px; }
  .ssh-line:hover, .ssh-line:focus-visible { background: var(--bg-hover); color: var(--text-primary); }
  .ssh-line.chosen { background: var(--bg-hover); color: var(--text-primary); box-shadow: inset 0 0 0 1px var(--border-hover); }
  .ssh-none { margin: 0; padding: 5px 8px; font-size: 11px; color: var(--text-muted); }
  .rh-main { flex: 1; min-width: 0; overflow-y: auto; padding: 26px 32px; display: flex; flex-direction: column; gap: 20px; }
  .rh-title { display: flex; align-items: center; gap: 12px; }
  .rh-title h2 { margin: 0; font-size: 20px; font-weight: 700; font-family: 'JetBrains Mono', ui-monospace, Menlo, Consolas, monospace; }
  .rh-title .btn-primary { margin-left: auto; }
  .pill { display: inline-flex; align-items: center; gap: 6px; height: 22px; padding: 0 9px; border-radius: 11px; border: 1px solid var(--border-hover); font-size: 11px; color: var(--text-secondary); }
  .facts { margin: 0; display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
  .facts div { padding: 12px 14px; border: 1px solid var(--border); border-radius: 9px; background: var(--bg-card); min-width: 0; }
  .facts dt { font-size: 11px; color: var(--text-muted); }
  .facts dd { margin: 3px 0 0; font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .prefs { padding: 16px; border: 1px solid var(--border); border-radius: 10px; }
  .pref { display: flex; align-items: center; gap: 12px; font-size: 12px; color: var(--text-secondary); }
  .pref span { width: 150px; }
  .pref select { flex: 1; }
  .actions { display: flex; align-items: center; gap: 8px; }
  .many { margin: 0; padding: 0; list-style: none; display: flex; flex-wrap: wrap; gap: 6px; }
  .many li { padding: 4px 10px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg-card); font-family: 'JetBrains Mono', ui-monospace, Menlo, Consolas, monospace; font-size: 12px; }
  .rh-note { margin: 0; font-size: 12px; color: var(--text-muted); }
  /* The context menu of the host list. */
  .ctx {
    position: fixed; z-index: 50; width: 300px; padding: 4px; display: flex; flex-direction: column;
    background: var(--bg-card); border: 1px solid var(--border-hover); border-radius: 8px; box-shadow: 0 8px 24px rgba(0, 0, 0, 0.28);
  }
  .ctx-item {
    display: flex; flex-direction: column; gap: 2px; padding: 6px 10px; border: none; border-radius: 5px;
    background: none; color: var(--text-primary); font: inherit; font-size: 12px; text-align: left; cursor: pointer;
  }
  .ctx-item:hover:not(:disabled), .ctx-item:focus-visible { background: var(--bg-hover); outline: none; }
  .ctx-item:disabled { opacity: 0.45; cursor: default; }
  .ctx-name.danger { color: var(--alert); }
  .ctx-help { font-size: 11px; color: var(--text-muted); line-height: 1.35; }
  .ctx-sep { height: 1px; margin: 4px 6px; background: var(--border); }
  .btn {
    height: 30px; padding: 0 12px; border: 1px solid var(--border-hover); border-radius: 6px; background: var(--bg-card);
    color: var(--text-primary); font: inherit; font-size: 12px; cursor: pointer;
  }
  .btn:disabled { opacity: 0.5; cursor: default; }
  .btn-primary { background: #2563eb; border-color: #2563eb; color: #fff; font-weight: 600; }
  .btn-action { background: rgba(59, 130, 246, 0.12); border-color: rgba(59, 130, 246, 0.4); }
  .btn-danger { background: var(--alert); border-color: var(--alert); color: #fff; }
  .confirm { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--text-secondary); }
  /* The colors of the global warnings (Banners.svelte): an action waits. */
  .waiting {
    display: flex; align-items: center; gap: 8px; padding: 8px 12px;
    background: #431407; border: 1px solid #c2410c; border-radius: 8px;
    font-size: 13px; color: #fdba74; line-height: 1.4;
  }
  :global([data-theme="light"]) .waiting { background: #fff7ed; border-color: #c2410c; color: #7c2d12; }
  .waiting-icon { font-size: 15px; flex-shrink: 0; }
  .msg { margin: 0; font-size: 12px; color: var(--text-muted); }
  /* The color of remote windows (#331). */
  .tint { margin-top: auto; padding-top: 18px; border-top: 1px solid var(--border); display: flex; flex-direction: column; gap: 12px; }
  .tint-head h3 { margin: 0; font-size: 13px; font-weight: 600; }
  .tint-head p { margin: 3px 0 0; font-size: 12px; color: var(--text-muted); }
  .tint-body { display: flex; align-items: center; gap: 24px; flex-wrap: wrap; }
  .swatches { display: flex; align-items: center; gap: 8px; }
  .swatch {
    width: 24px; height: 24px; padding: 0; border-radius: 50%; border: 2px solid var(--bg-base); cursor: pointer;
    background: var(--sw); box-shadow: 0 0 0 1px var(--border-hover);
  }
  .swatch[aria-checked="true"] { box-shadow: 0 0 0 2px var(--sw); }
  .swatch:focus-visible { outline: 2px solid var(--text-primary); outline-offset: 2px; }
  .reset { margin-left: 6px; }
  .tint-preview { width: 260px; border: 1px solid var(--border); border-radius: 8px; overflow: hidden; background: var(--bg-base); font-size: 10px; }
  .pv-top { display: flex; align-items: center; gap: 6px; height: 24px; padding: 0 8px; border-bottom: 1px solid; font-weight: 600; }
  .pv-dot { width: 6px; height: 6px; border-radius: 50%; }
  .pv-row { display: flex; height: 70px; }
  .pv-side { width: 84px; padding: 6px 8px; border-right: 1px solid; display: flex; flex-direction: column; gap: 5px; }
  .pv-side span:first-child { font-size: 8px; text-transform: uppercase; letter-spacing: 0.08em; }
  .pv-content { flex: 1; }
  .msg.bad { color: var(--alert); }
</style>
