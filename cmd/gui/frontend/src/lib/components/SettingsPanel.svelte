<script lang="ts">
  // The Settings panel under the top bar (gear button).
  import { slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { configStore, prSettings } from '../stores';
  import { t } from '../i18n';
  import { tooltip } from '../tooltip';
  import { BrowserOpenURL } from '../../../wailsjs/runtime/runtime';
  import type { ThemeChoice } from '../theme';
  import type { RemotesList, RemoteResult } from '../bridge';

  export let showSettings: boolean;
  export let simple: boolean;
  export let configPath: string;
  export let languageChoice: 'en' | 'es';
  export let themeChoice: ThemeChoice;
  export let fetchInterval: string;
  export let lastFetchTime: string;
  export let autostartSupported: boolean;
  export let autostartOn: boolean;
  export let checkGitignorePref: boolean;
  export let doctorSummary: string;
  export let terminalsManagerOpening: boolean;
  export let appVersion: string;
  export let openRoots: () => void;
  $: roots = ($configStore?.global?.roots || []) as string[];
  export let setLanguage: (lang: 'en' | 'es') => Promise<void>;
  export let setTheme: (choice: ThemeChoice) => void;
  export let setFetchInterval: (val: string) => void;
  export let toggleAutostart: () => Promise<void>;
  export let setPRBadgesEnabled: (enabled: boolean) => Promise<void>;
  export let setPRIncludeDrafts: (include: boolean) => Promise<void>;
  export let setCheckGitignorePref: (enabled: boolean) => Promise<void>;
  export let openDoctorModal: () => void;
  export let openTerminalsManagerWindow: () => Promise<void>;

  // ── Remotes (experimental, #239) ──
  // Shown only when remote mode is on, by --experimental-remote or the
  // experimental_remote key. Each remote opens in a window of its own.
  let remotes: RemotesList = { enabled: false, remotes: [] };
  let newAlias = '';
  let newServerDir = '';
  let remoteBusy = '';
  let remoteMsg = '';
  let remoteMsgBad = false;
  // alias → stage of an inline uninstall confirmation; others is how many
  // other clients have a live server there.
  let uninstallAsk: { alias: string; others: number } | null = null;

  $: if (showSettings) loadRemotes();

  async function loadRemotes() {
    try { remotes = await bridge.listRemotes(); } catch (e) { console.error(e); }
  }

  function say(res: RemoteResult, ok: string): boolean {
    if (res.error) {
      remoteMsg = res.hint ? $t(res.hint) : res.error;
      remoteMsgBad = true;
      return false;
    }
    remoteMsg = ok;
    remoteMsgBad = false;
    return true;
  }

  async function addRemote() {
    remoteBusy = 'add';
    try {
      if (say(await bridge.addRemote(newAlias, newServerDir), $t('settings.remotes.added', { alias: newAlias.trim() }))) {
        newAlias = '';
        newServerDir = '';
        await loadRemotes();
      }
    } finally { remoteBusy = ''; }
  }

  async function connectRemote(alias: string) {
    remoteBusy = 'connect:' + alias;
    try { say(await bridge.connectRemote(alias), $t('settings.remotes.opening', { alias })); }
    finally { remoteBusy = ''; }
  }

  async function removeRemote(alias: string) {
    remoteBusy = 'remove:' + alias;
    try {
      if (say(await bridge.removeRemote(alias), $t('settings.remotes.removed', { alias }))) await loadRemotes();
    } finally { remoteBusy = ''; }
  }

  async function uninstallRemote(alias: string, force: boolean) {
    remoteBusy = 'uninstall:' + alias;
    try {
      const res = await bridge.remoteUninstall(alias, force);
      if (res.error) { uninstallAsk = null; say(res, ''); return; }
      if (res.needsConfirm) { uninstallAsk = { alias, others: res.otherClients }; return; }
      uninstallAsk = null;
      say(res, $t('settings.remotes.uninstalled', { alias }));
    } finally { remoteBusy = ''; }
  }
</script>

{#if showSettings}
  <div class="settings" transition:slide={{ duration: 150 }}>
    {#if !simple}
    <div class="settings-row">
      <span class="settings-label" use:tooltip={$t('settings.configTip')}>{$t('settings.config')}</span>
      <button class="settings-action-btn" on:click={() => bridge.openFileInEditor(configPath)}>{$t('settings.openInEditor')}</button>
      <span class="settings-value">{configPath}</span>
    </div>
    {/if}
    <div class="settings-row">
      <span class="settings-label" use:tooltip={$t('settings.rootsTip')}>{$t('settings.roots')}</span>
      <button class="settings-action-btn" on:click={openRoots}>{$t('settings.change')}</button>
      <span class="settings-value">{roots[0] || $t('settings.notSet')}{#if roots.length > 1}&nbsp;<span class="roots-more">{$t('settings.rootsMore', { n: roots.length - 1 })}</span>{/if}</span>
    </div>
    <div class="settings-row">
      <span class="settings-label" use:tooltip={$t('settings.languageTip')}>{$t('settings.language')}</span>
      <div class="theme-toggle">
        <button class="theme-btn" class:theme-active={languageChoice === 'en'} on:click={() => setLanguage('en')} translate="no">English</button>
        <button class="theme-btn" class:theme-active={languageChoice === 'es'} on:click={() => setLanguage('es')} translate="no">Español</button>
      </div>
    </div>
    <div class="settings-row">
      <span class="settings-label" use:tooltip={$t('settings.themeTip')}>{$t('settings.theme')}</span>
      <div class="theme-toggle">
        <button class="theme-btn" class:theme-active={themeChoice === 'system'} on:click={() => setTheme('system')}>{$t('theme.system')}</button>
        <button class="theme-btn" class:theme-active={themeChoice === 'light'} on:click={() => setTheme('light')}>{$t('theme.light')}</button>
        <button class="theme-btn" class:theme-active={themeChoice === 'dark'} on:click={() => setTheme('dark')}>{$t('theme.dark')}</button>
      </div>
    </div>
    <div class="settings-row">
      <span class="settings-label" use:tooltip={$t('settings.periodicStatusTip')}>{simple ? $t('simple.checkChanges') : $t('settings.periodicStatus')}</span>
      <div class="theme-toggle">
        <button class="theme-btn" class:theme-active={fetchInterval === 'off'} on:click={() => setFetchInterval('off')}>{$t('common.off')}</button>
        <button class="theme-btn" class:theme-active={fetchInterval === '5m'} on:click={() => setFetchInterval('5m')}>5m</button>
        <button class="theme-btn" class:theme-active={fetchInterval === '15m'} on:click={() => setFetchInterval('15m')}>15m</button>
        <button class="theme-btn" class:theme-active={fetchInterval === '30m'} on:click={() => setFetchInterval('30m')}>30m</button>
      </div>
      {#if lastFetchTime}
        <span class="settings-value" style="font-size:10px; margin-left:4px">{$t('settings.lastFetch', { time: lastFetchTime })}</span>
      {/if}
    </div>
    {#if autostartSupported}
      <div class="settings-row">
        <span class="settings-label" use:tooltip={$t('settings.runAtStartupTip')}>{$t('settings.runAtStartup')}</span>
        <div class="theme-toggle">
          <button class="theme-btn" class:theme-active={!autostartOn} on:click={() => { if (autostartOn) toggleAutostart(); }}>{$t('common.off')}</button>
          <button class="theme-btn" class:theme-active={autostartOn} on:click={() => { if (!autostartOn) toggleAutostart(); }}>{$t('common.on')}</button>
        </div>
      </div>
    {/if}
    {#if !simple}
    <div class="settings-row">
      <span class="settings-label" use:tooltip={$t('settings.prReviewsTip')}>{$t('settings.prReviews')}</span>
      <div class="theme-toggle">
        <button class="theme-btn" class:theme-active={!$prSettings.enabled} on:click={() => { if ($prSettings.enabled) setPRBadgesEnabled(false); }}>{$t('common.off')}</button>
        <button class="theme-btn" class:theme-active={$prSettings.enabled} on:click={() => { if (!$prSettings.enabled) setPRBadgesEnabled(true); }}>{$t('common.on')}</button>
      </div>
      {#if $prSettings.enabled}
        <span class="settings-sublabel" use:tooltip={$t('settings.includeDraftsTip')}>{$t('settings.includeDrafts')}</span>
        <div class="theme-toggle">
          <button class="theme-btn" class:theme-active={!$prSettings.includeDrafts} on:click={() => { if ($prSettings.includeDrafts) setPRIncludeDrafts(false); }}>{$t('common.off')}</button>
          <button class="theme-btn" class:theme-active={$prSettings.includeDrafts} on:click={() => { if (!$prSettings.includeDrafts) setPRIncludeDrafts(true); }}>{$t('common.on')}</button>
        </div>
      {/if}
    </div>
    <div class="settings-row">
      <span class="settings-label" use:tooltip={$t('settings.globalGitignoreTip')}>{$t('settings.globalGitignore')}</span>
      <div class="theme-toggle">
        <button class="theme-btn" class:theme-active={!checkGitignorePref} on:click={() => { if (checkGitignorePref) setCheckGitignorePref(false); }}>{$t('common.off')}</button>
        <button class="theme-btn" class:theme-active={checkGitignorePref} on:click={() => { if (!checkGitignorePref) setCheckGitignorePref(true); }}>{$t('common.on')}</button>
      </div>
    </div>
    {/if}
    <div class="settings-row">
      <span class="settings-label" use:tooltip={$t('settings.systemCheckTip')}>{$t('settings.systemCheck')}</span>
      <button class="settings-action-btn" on:click={openDoctorModal}>{$t('settings.run')}</button>
      {#if doctorSummary}
        <span class="settings-sublabel settings-doctor-summary">{doctorSummary}</span>
      {/if}
    </div>
    {#if !simple}
    <div class="settings-row">
      <span class="settings-label" use:tooltip={$t('settings.terminalsTip')}>{$t('settings.terminals')}</span>
      <button class="settings-action-btn" on:click={openTerminalsManagerWindow} disabled={terminalsManagerOpening}>
        {#if terminalsManagerOpening}
          <span class="settings-action-spinner" aria-hidden="true"></span>
          {$t('settings.opening')}
        {:else}
          {$t('settings.manager')}
        {/if}
      </button>
    </div>
    {/if}
    {#if remotes.enabled}
    <div class="settings-row settings-row-top">
      <span class="settings-label" use:tooltip={$t('settings.remotesTip')}>{$t('settings.remotes')}<span class="remote-badge">{$t('settings.remotes.experimental')}</span></span>
      <div class="remotes">
        {#each remotes.remotes as r (r.alias)}
          <div class="remote-row">
            <span class="remote-alias" translate="no">{r.alias}</span>
            {#if r.serverDir}<span class="remote-dir" translate="no">{r.serverDir}</span>{/if}
            <button class="settings-action-btn" on:click={() => connectRemote(r.alias)} disabled={remoteBusy !== ''}>{$t('settings.remotes.connect')}</button>
            <button class="theme-btn" on:click={() => (uninstallAsk = { alias: r.alias, others: 0 })} disabled={remoteBusy !== ''}>{$t('settings.remotes.uninstall')}</button>
            <button class="theme-btn" on:click={() => removeRemote(r.alias)} disabled={remoteBusy !== ''}>{$t('settings.remotes.remove')}</button>
          </div>
          {#if uninstallAsk?.alias === r.alias}
            <div class="remote-confirm">
              <span>{uninstallAsk.others > 0 ? $t('remote.uninstallOthers', { n: uninstallAsk.others }) : $t('remote.uninstallConfirm', { alias: r.alias })}</span>
              <button class="theme-btn remote-danger" on:click={() => uninstallRemote(r.alias, (uninstallAsk?.others ?? 0) > 0)} disabled={remoteBusy !== ''}>
                {remoteBusy === 'uninstall:' + r.alias ? $t('settings.remotes.working') : $t('settings.remotes.uninstall')}
              </button>
              <button class="theme-btn" on:click={() => (uninstallAsk = null)}>{$t('common.cancel')}</button>
            </div>
          {/if}
        {:else}
          <span class="settings-sublabel remote-none">{$t('settings.remotes.none')}</span>
        {/each}
        <form class="remote-add" on:submit|preventDefault={addRemote}>
          <input class="remote-input" bind:value={newAlias} placeholder={$t('settings.remotes.aliasPlaceholder')} aria-label={$t('settings.remotes.alias')} spellcheck="false" autocapitalize="off" />
          <input class="remote-input remote-input-dir" bind:value={newServerDir} placeholder={$t('settings.remotes.serverDirPlaceholder')} aria-label={$t('settings.remotes.serverDir')} spellcheck="false" autocapitalize="off" />
          <button class="settings-action-btn" type="submit" disabled={remoteBusy !== '' || !newAlias.trim()}>
            {remoteBusy === 'add' ? $t('settings.remotes.checking') : $t('settings.remotes.add')}
          </button>
        </form>
        {#if remoteMsg}<span class="remote-msg" class:remote-msg-bad={remoteMsgBad}>{remoteMsg}</span>{/if}
      </div>
    </div>
    {/if}
    <div class="settings-row">
      <span class="settings-label" use:tooltip={$t('settings.versionTip')}>{$t('settings.version')}</span>
      <span class="settings-value">{appVersion}</span>
    </div>
    <div class="settings-row">
      <span class="settings-label" use:tooltip={$t('settings.authorTip')}>{$t('settings.author')}</span>
      <span class="settings-value" translate="no">Luis Palacios Derqui &mdash; <a href="https://github.com/LuisPalacios/gitbox" on:click|preventDefault={() => BrowserOpenURL('https://github.com/LuisPalacios/gitbox')}>github.com/LuisPalacios/gitbox</a></span>
    </div>
  </div>
{/if}

<style>
  .settings {
    padding: 14px 24px; border-bottom: 1px solid var(--border);
    background: var(--bg-card); display: flex; flex-direction: column; gap: 8px;
  }
  .settings-row { display: flex; align-items: center; gap: 12px; }
  .settings-label { font-size: 11px; font-weight: 600; color: var(--text-muted); width: 80px; flex-shrink: 0; }
  .settings-sublabel { font-size: 11px; font-weight: 500; color: var(--text-muted); margin-left: 12px; flex-shrink: 0; }
  .settings-value { font-size: 12px; color: var(--text-secondary); font-family: monospace; flex: 1; }
  .roots-more { font-family: inherit; color: var(--text-muted); }
  /* Action buttons (Open in editor / Change / Run / Manager) — same shape
     across every Gear row, sitting immediately right of the label so the
     eye doesn't have to hunt. The subtle blue tint distinguishes them
     from the option toggles (English/Español/System/Light/Dark) which are
     stateful selectors, not actions. */
  .settings-action-btn {
    padding: 3px 12px;
    font-size: 11px;
    font-weight: 500;
    background: rgba(59, 130, 246, 0.10);  /* blue-500 @ 10% — soft tint */
    border: 1px solid rgba(59, 130, 246, 0.28);
    color: var(--text-primary);
    border-radius: 5px;
    cursor: pointer;
    transition: background 0.12s, border-color 0.12s, color 0.12s;
    white-space: nowrap;
    flex-shrink: 0;
  }
  .settings-action-btn:hover {
    background: rgba(59, 130, 246, 0.18);
    border-color: rgba(59, 130, 246, 0.44);
  }
  /* Light theme reads better with a touch more saturation. */
  :global([data-theme="light"]) .settings-action-btn {
    background: rgba(59, 130, 246, 0.08);
    border-color: rgba(59, 130, 246, 0.32);
  }
  :global([data-theme="light"]) .settings-action-btn:hover {
    background: rgba(59, 130, 246, 0.14);
    border-color: rgba(59, 130, 246, 0.50);
  }
  .settings-action-btn:disabled {
    opacity: 0.7;
    cursor: progress;
  }
  .settings-action-btn:disabled:hover {
    background: rgba(59, 130, 246, 0.10);
    border-color: rgba(59, 130, 246, 0.28);
  }
  /* Tiny inline spinner shown next to "Opening…" while a Manager
     subprocess cold-start is in flight. Borderless ring driven by a
     1s rotate. */
  .settings-action-spinner {
    display: inline-block;
    width: 9px;
    height: 9px;
    margin-right: 6px;
    vertical-align: -1px;
    border: 1.5px solid rgba(59, 130, 246, 0.30);
    border-top-color: rgb(59, 130, 246);
    border-radius: 50%;
    animation: gb-spin 0.85s linear infinite;
  }
  @keyframes gb-spin {
    to { transform: rotate(360deg); }
  }
  .theme-toggle { display: flex; align-items: center; gap: 4px; }
  .theme-btn {
    padding: 3px 10px; font-size: 11px; border: 1px solid var(--border);
    background: transparent; color: var(--text-secondary); border-radius: 5px;
    cursor: pointer; transition: all 0.12s;
  }
  .theme-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
  .theme-active { background: var(--bg-hover); color: var(--text-primary); border-color: var(--border-hover); }
  /* .settings-action shared with .settings-btn / .settings-action-btn —
     unified rule lives near .settings-row above. */
  .settings-doctor-summary { margin-left: 8px; }
  .settings-row-top { align-items: flex-start; }
  .settings-row-top .settings-label { padding-top: 4px; }
  .remote-badge {
    display: block; width: fit-content; margin-top: 3px; padding: 0 5px; border-radius: 3px;
    font-size: 9px; font-weight: 600; letter-spacing: 0.04em; text-transform: uppercase;
    color: #b45309; background: rgba(245, 158, 11, 0.14);
  }
  .remotes { display: flex; flex-direction: column; gap: 6px; flex: 1; min-width: 0; }
  .remote-row { display: flex; align-items: center; gap: 8px; }
  .remote-alias { font-size: 12px; font-family: monospace; color: var(--text-primary); min-width: 90px; }
  .remote-dir { font-size: 11px; font-family: monospace; color: var(--text-muted); }
  .remote-none { margin-left: 0; padding-top: 4px; }
  .remote-confirm { display: flex; align-items: center; gap: 8px; font-size: 11px; color: var(--text-secondary); padding-left: 2px; }
  .remote-danger { color: var(--alert); border-color: var(--alert); }
  .remote-add { display: flex; align-items: center; gap: 6px; }
  .remote-input {
    width: 140px; padding: 3px 8px; font-size: 11px; font-family: monospace;
    border: 1px solid var(--border); border-radius: 5px; background: var(--bg-base); color: var(--text-primary);
  }
  .remote-input-dir { width: 170px; }
  .remote-msg { font-size: 11px; color: var(--text-muted); }
  .remote-msg-bad { color: var(--alert); }
</style>
