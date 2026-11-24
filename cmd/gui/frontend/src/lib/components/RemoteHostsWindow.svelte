<script lang="ts">
  // The Remote hosts window (#330): a window of its own that lists the
  // remote hosts of this computer and manages them. It took over the
  // Remotes block of Settings and keeps every action it had: add (now with
  // suggestions from the ssh configuration), connect, editor, uninstall the
  // server and remove; it adds the host's facts and its connection log.
  import { onMount } from 'svelte';
  import { bridge } from '../bridge';
  import type { RemoteResult, RemotesList, SSHHost } from '../bridge';
  import { configStore } from '../stores';
  import { languageStore, t } from '../i18n';
  import { errorText } from '../notices';
  import { remoteEditorChoices, editorName } from '../remotehost';
  import type { EditorInfo } from '../types';
  import { tooltip } from '../tooltip';

  let remotes: RemotesList = { enabled: true, remotes: [] };
  let suggestions: SSHHost[] = [];
  // The ssh lines arrive after the configured hosts, so the window never
  // waits for them.
  let sshLoading = true;
  // query is what I typed in the alias field, without the completion the
  // field proposes after it.
  let query = '';
  let aliasInput: HTMLInputElement;
  let selected = '';
  let newAlias = '';
  let newServerDir = '';
  let busy = '';
  let msg = '';
  let msgBad = false;
  // The inline uninstall confirmation; others is how many other clients
  // have a live server there.
  let uninstallAsk: { alias: string; others: number } | null = null;

  $: current = remotes.remotes.find((r) => r.alias === selected) || null;
  // The editors a remote can open folders in (#287): the configured ones
  // whose command opens remote folders.
  $: editorOptions = remoteEditorChoices(($configStore?.global?.editors || []) as EditorInfo[], remotes.remoteEditors || []);
  $: stateText = (r: RemotesList['remotes'][number]) => (r.open ? $t(`remote.state.${r.state || 'connecting'}`) : $t('switcher.closed'));
  $: when = (iso?: string) => {
    if (!iso) return $t('remotesWindow.never');
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? iso : d.toLocaleString($languageStore);
  };

  export async function load() {
    loadSSH();
    try { remotes = await bridge.listRemotes(); } catch (e) { say({ error: errorText(e) }, ''); }
    if (!remotes.remotes.some((r) => r.alias === selected)) selected = remotes.remotes[0]?.alias || '';
  }

  async function loadSSH() {
    try { suggestions = await bridge.listSSHHosts(); } catch (e) { console.error(e); } finally { sshLoading = false; }
  }

  onMount(() => {
    load();
    const refresh = () => load();
    window.addEventListener('focus', refresh);
    // While the window shows, a refresh every minute keeps the states of
    // the hosts current and tells the process the window is in use: hidden
    // for ten minutes without one, it ends (#330).
    const beat = setInterval(() => { if (document.visibilityState === 'visible') load(); }, 60_000);
    return () => { window.removeEventListener('focus', refresh); clearInterval(beat); };
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
  const connect = (alias: string, open: boolean) => run('connect', async () =>
    say(await bridge.connectRemote(alias), open ? '' : $t('settings.remotes.opening', { alias })));
  const showLog = (alias: string) => run('log', async () => say(await bridge.showRemoteLog(alias), ''));
  const setEditor = (alias: string, editor: string) => run('editor', async () =>
    say(await bridge.setRemoteEditor(alias, editor), $t('settings.remotes.editorSaved', { alias })));
  const remove = (alias: string) => run('remove', async () => say(await bridge.removeRemote(alias), $t('settings.remotes.removed', { alias })));
  const uninstall = (alias: string, force: boolean) => run('uninstall', async () => {
    const res = await bridge.remoteUninstall(alias, force);
    if (res.error) { uninstallAsk = null; say(res, ''); return false; }
    if (res.needsConfirm) { uninstallAsk = { alias, others: res.otherClients }; return false; }
    uninstallAsk = null;
    return say(res, $t('settings.remotes.uninstalled', { alias }));
  });

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

  function pick(alias: string) {
    selected = alias;
    uninstallAsk = null;
    msg = '';
  }
</script>

<div class="rh">
  <aside class="rh-side">
    <header class="rh-head">
      <h1>{$t('remotesWindow.title')}<span class="badge">{$t('settings.remotes.experimental')}</span></h1>
      <p>{$t('remotesWindow.intro')}</p>
    </header>
    <div class="rh-list" role="listbox" aria-label={$t('remotesWindow.title')}>
      {#each remotes.remotes as r (r.alias)}
        <button class="rh-host" class:selected={r.alias === selected} role="option" aria-selected={r.alias === selected} on:click={() => pick(r.alias)}>
          <svg class="glyph" viewBox="0 0 16 16" aria-hidden="true"><rect x="2" y="2.5" width="12" height="4.5" rx="1" /><rect x="2" y="9" width="12" height="4.5" rx="1" /></svg>
          <span class="two"><span class="alias" translate="no">{r.alias}</span><small>{stateText(r)}</small></span>
          {#if r.open}<span class="live state-{r.state || 'connecting'}" aria-hidden="true"></span>{/if}
        </button>
      {:else}
        <p class="rh-none">{$t('remotesWindow.none')}</p>
      {/each}
    </div>
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
    {#if current}
      <div class="rh-title">
        <h2 translate="no">{current.alias}</h2>
        <span class="pill"><span class="live state-{current.open ? current.state || 'connecting' : 'closed'}" aria-hidden="true"></span>{stateText(current)}</span>
        <button class="btn btn-primary" on:click={() => current && connect(current.alias, !!current.open)} disabled={busy !== ''}>
          {current.open ? $t('remotesWindow.goTo') : $t('settings.remotes.connect')}
        </button>
      </div>

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
        <span class="grow"></span>
        <button class="btn btn-danger-quiet" on:click={() => current && (uninstallAsk = { alias: current.alias, others: 0 })} disabled={busy !== ''}>{$t('settings.remotes.uninstall')}</button>
        <button class="btn" on:click={() => current && remove(current.alias)} disabled={busy !== ''}>{$t('settings.remotes.remove')}</button>
      </div>
      {#if uninstallAsk?.alias === current.alias}
        <div class="confirm">
          <span>{uninstallAsk.others > 0 ? $t('remote.uninstallOthers', { n: uninstallAsk.others }) : $t('remote.uninstallConfirm', { alias: current.alias })}</span>
          <button class="btn btn-danger" on:click={() => current && uninstall(current.alias, (uninstallAsk?.others ?? 0) > 0)} disabled={busy !== ''}>
            {busy === 'uninstall' ? $t('settings.remotes.working') : $t('settings.remotes.uninstall')}
          </button>
          <button class="btn" on:click={() => (uninstallAsk = null)}>{$t('common.cancel')}</button>
        </div>
      {/if}
    {:else}
      <p class="rh-empty">{$t('remotesWindow.none')}</p>
    {/if}
    {#if msg}<p class="msg" class:bad={msgBad} role="status">{msg}</p>{/if}
  </main>
</div>

<style>
  .rh { position: fixed; inset: 0; display: flex; background: var(--bg-base); color: var(--text-primary); font-size: 13px; }
  .rh-side { width: 300px; flex: none; display: flex; flex-direction: column; background: var(--bg-card); border-right: 1px solid var(--border); }
  .rh-head { padding: 20px 18px 10px; }
  .rh-head h1 { margin: 0; font-size: 17px; font-weight: 700; display: flex; align-items: center; gap: 8px; }
  .rh-head p { margin: 4px 0 0; font-size: 12px; color: var(--text-muted); line-height: 1.45; }
  .badge {
    padding: 0 5px; border-radius: 3px; font-size: 9px; font-weight: 600; letter-spacing: 0.04em; text-transform: uppercase;
    color: #b45309; background: rgba(245, 158, 11, 0.14);
  }
  .rh-list { flex: 1; overflow-y: auto; padding: 4px 8px; display: flex; flex-direction: column; gap: 2px; }
  .rh-host {
    display: flex; align-items: center; gap: 10px; padding: 9px 10px; border: none; border-radius: 8px;
    background: none; color: var(--text-primary); font: inherit; text-align: left; cursor: pointer;
  }
  .rh-host:hover { background: var(--bg-hover); }
  .rh-host.selected { background: var(--bg-hover); box-shadow: inset 0 0 0 1px var(--border-hover); }
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
    display: block; width: 100%; box-sizing: border-box; padding: 5px 8px; border: none; border-radius: 4px;
    background: none; color: var(--text-secondary); text-align: left; cursor: pointer;
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
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
  .grow { flex: 1; }
  .btn {
    height: 30px; padding: 0 12px; border: 1px solid var(--border-hover); border-radius: 6px; background: var(--bg-card);
    color: var(--text-primary); font: inherit; font-size: 12px; cursor: pointer;
  }
  .btn:disabled { opacity: 0.5; cursor: default; }
  .btn-primary { background: #2563eb; border-color: #2563eb; color: #fff; font-weight: 600; }
  .btn-action { background: rgba(59, 130, 246, 0.12); border-color: rgba(59, 130, 246, 0.4); }
  .btn-danger-quiet { background: none; color: var(--alert); border-color: var(--alert); }
  .btn-danger { background: var(--alert); border-color: var(--alert); color: #fff; }
  .confirm { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--text-secondary); }
  .msg { margin: 0; font-size: 12px; color: var(--text-muted); }
  .msg.bad { color: var(--alert); }
</style>
