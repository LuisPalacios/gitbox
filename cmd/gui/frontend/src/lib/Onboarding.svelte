<script lang="ts">
  // Onboarding — the guided setup for people who don't know git (#124,
  // phase 3). First run walks three steps: where projects live, connect the
  // service that stores them, pick projects to download. Simple mode's
  // "Add account" reuses the last two (mode='add').
  //
  // The person never sees an account key or a credential type: the key is
  // suggested by Go, browser sign-in (GCM) is the default and a token is the
  // fallback when GCM is missing or sign-in fails. Cloning is left to the
  // host (onDone), which owns the clone progress and status state.

  import { onDestroy, onMount } from 'svelte';
  import { bridge, events } from './bridge';
  import type { DoctorPrecheckDTO } from './bridge';
  import type { DiscoverResult } from './types';
  import { providerLabel, providerURLs } from './theme';
  import { t } from './i18n';
  import { BrowserOpenURL } from '../../wailsjs/runtime/runtime';

  export let mode: 'first-run' | 'add' = 'first-run';
  export let onDone: (result: { sourceKey: string; repos: string[] } | null) => void;
  export let onCancel: () => void = () => {};

  type Step = 'folder' | 'connect' | 'projects';
  const steps: Step[] = mode === 'first-run' ? ['folder', 'connect', 'projects'] : ['connect', 'projects'];
  let step: Step = steps[0];
  $: stepNumber = steps.indexOf(step) + 1;

  // ── Step 1: folder ──
  let folder = '~/00.git';
  let folderError = '';
  let folderBusy = false;

  async function browse() {
    const dir = await bridge.pickFolder($t('onboard.folderPick'));
    if (dir) folder = dir;
  }

  async function saveFolder() {
    folderError = '';
    if (!folder.trim()) { folderError = $t('onboard.folderRequired'); return; }
    folderBusy = true;
    try {
      // Also switches config saving on and starts new installs in Simple.
      await bridge.setGlobalFolder(folder.trim());
      step = 'connect';
    } catch (err: any) {
      folderError = err?.message || String(err);
    }
    folderBusy = false;
  }

  // ── Step 2: connect ──
  const tiles = ['github', 'gitlab', 'bitbucket', 'other'];
  let choice = 'github';
  let otherKind: 'forgejo' | 'gitea' = 'forgejo';
  let serverURL = '';
  let username = '';
  let name = '';
  let email = '';
  $: provider = choice === 'other' ? otherKind : choice;
  $: url = choice === 'other' ? serverURL.trim() : providerURLs[choice];

  let gcm: DoctorPrecheckDTO | null = null; // null while checking
  let useToken = false;
  // 'form' → editing details; 'signing-in' → GCM browser flow running;
  // 'failed' → sign-in failed; 'token' → paste a token.
  let phase: 'form' | 'signing-in' | 'failed' | 'token' = 'form';
  let accountKey = ''; // set once the account exists
  let connectError = '';
  let connectDetail = '';
  let tokenURL = '';
  let tokenScopes = '';
  let token = '';
  let busy = false;

  async function checkGCM() {
    gcm = null;
    try {
      gcm = await bridge.doctorPrecheck('gcm');
    } catch {
      gcm = { ok: false };
    }
    if (!gcm.ok) useToken = true;
  }

  onMount(async () => {
    checkGCM();
    try {
      const id = await bridge.checkGlobalIdentity();
      if (!name) name = id?.name || '';
      if (!email) email = id?.email || '';
    } catch { /* nothing to prefill */ }
  });

  async function createAccount(credentialType: 'gcm' | 'token') {
    accountKey = await bridge.suggestAccountKey(provider, username.trim());
    await bridge.addAccount({
      key: accountKey, provider, url, username: username.trim(),
      name: name.trim(), email: email.trim(), defaultBranch: 'main', credentialType,
    });
  }

  async function connect() {
    connectError = '';
    connectDetail = '';
    if (!url || !username.trim() || !name.trim() || !email.trim()) {
      connectError = $t('onboard.fieldsRequired');
      return;
    }
    busy = true;
    try {
      await createAccount(useToken ? 'token' : 'gcm');
    } catch (err: any) {
      connectError = err?.message || String(err);
      accountKey = '';
      busy = false;
      return;
    }
    // The account exists from here on; errors stay on this account (no
    // second Connect) and Change details discards it.
    try {
      if (useToken) await showTokenStep();
      else await signIn();
    } catch (err: any) {
      connectError = err?.message || String(err);
    }
    busy = false;
  }

  async function signIn() {
    phase = 'signing-in';
    connectError = '';
    connectDetail = '';
    let res: any;
    try {
      res = await bridge.credentialSetupGCM(accountKey);
    } catch (err: any) {
      res = { ok: false, message: err?.message || String(err) };
    }
    if (res?.ok) {
      startProjects();
    } else {
      phase = 'failed';
      connectError = $t('onboard.signInFailed');
      connectDetail = res?.message || '';
    }
  }

  async function switchToToken() {
    busy = true;
    try {
      await bridge.changeCredentialType(accountKey, 'token');
      useToken = true;
      await showTokenStep();
    } catch (err: any) {
      connectError = err?.message || String(err);
    }
    busy = false;
  }

  async function showTokenStep() {
    phase = 'token';
    connectError = '';
    connectDetail = '';
    const guide = await bridge.getTokenGuide(accountKey);
    tokenURL = guide?.creationURL || '';
    tokenScopes = guide?.scopes || '';
  }

  async function saveToken() {
    connectError = '';
    connectDetail = '';
    busy = true;
    try {
      const res = await bridge.credentialStoreToken(accountKey, token.trim());
      if (!res?.ok) throw new Error(res?.message || '');
      const status = await bridge.credentialVerify(accountKey);
      if (status.status === 'ok' || status.status === 'warning') {
        startProjects();
      } else {
        connectError = $t('onboard.tokenFailed');
        connectDetail = status.primaryMsg || status.message || '';
      }
    } catch (err: any) {
      connectError = $t('onboard.tokenFailed');
      connectDetail = err?.message || String(err);
    }
    busy = false;
  }

  // Back to the form: drop the half-made account from the config (never
  // from disk) so Connect can create it again with the new details.
  async function changeDetails() {
    busy = true;
    try {
      if (accountKey) await bridge.discardAccount(accountKey);
      accountKey = '';
      phase = 'form';
      connectError = '';
      connectDetail = '';
      token = '';
      useToken = !gcm?.ok;
    } catch (err: any) {
      connectError = err?.message || String(err);
    }
    busy = false;
  }

  // ── Step 3: pick projects ──
  let repos: DiscoverResult[] = [];
  let selected: Record<string, boolean> = {};
  let filter = '';
  let listing = false;
  let listError = '';
  let saving = false;
  let stopListening: (() => void) | null = null;

  $: shown = filter.trim()
    ? repos.filter((r) => r.fullName.toLowerCase().includes(filter.trim().toLowerCase()))
    : repos;
  $: chosen = Object.keys(selected).filter((k) => selected[k]);
  $: allShownChosen = shown.length > 0 && shown.every((r) => selected[r.fullName]);

  function startProjects() {
    step = 'projects';
    listRepos();
  }

  function listRepos() {
    listing = true;
    listError = '';
    repos = [];
    selected = {};
    stopListening?.();
    stopListening = events.on('discover:done', (data: any) => {
      if (data?.accountKey && data.accountKey !== accountKey) return;
      stopListening?.();
      stopListening = null;
      listing = false;
      if (data?.error) {
        listError = String(data.error);
        return;
      }
      repos = (data?.repos || []).sort((a: DiscoverResult, b: DiscoverResult) => a.fullName.localeCompare(b.fullName));
    });
    bridge.discover(accountKey);
  }

  onDestroy(() => stopListening?.());

  function toggleAllShown() {
    const value = !allShownChosen;
    for (const r of shown) selected[r.fullName] = value;
    selected = selected;
  }

  async function download() {
    saving = true;
    listError = '';
    try {
      await bridge.addDiscoveredRepos(accountKey, chosen);
      onDone({ sourceKey: accountKey, repos: chosen });
    } catch (err: any) {
      listError = err?.message || String(err);
    }
    saving = false;
  }

  // Closing "Add account" before the account works drops it from the config
  // (never from disk) instead of leaving a broken account behind.
  async function cancel() {
    if (accountKey && step === 'connect') await bridge.discardAccount(accountKey).catch(() => {});
    onCancel();
  }

  function skip() {
    onDone(accountKey ? { sourceKey: accountKey, repos: [] } : null);
  }
</script>

<div class={mode === 'first-run' ? 'ob-screen' : 'ob-overlay'}>
  <div class="ob-card" role="dialog" aria-modal="true">
    {#if mode === 'add'}
      <button class="ob-close" on:click={cancel} disabled={busy || saving || phase === 'signing-in'} aria-label={$t('common.cancel')}>&#10005;</button>
    {/if}
    {#if mode === 'first-run' && step === 'folder'}
      <svg class="ob-logo" viewBox="0 0 500 500" xmlns="http://www.w3.org/2000/svg">
        <g transform="matrix(0.838711, 0, 0, 0.838711, 52.15668, 40.24861)">
          <path d="M 119.779 265.572 L 146.694 265.582 C 146.694 265.582 162.549 310.895 163.783 313.997 L 145.642 332.841 L 166.338 355.62 L 185.565 336.41 C 184.801 337.405 234.42 356.617 234.42 356.617 L 234.74 387.858 L 265.36 389.26 L 265.013 356.868 L 313.812 336.358 L 339.89 361.913 L 362.266 340.797 L 336.562 314.13 L 354.13 265.424 L 394.197 265.15 L 393.609 234.438 L 353.715 234.488 C 352.82 232.067 335.975 185.534 335.975 185.534 L 360.441 160.899 L 337.258 140.162 L 313.098 163.288 C 313.098 163.288 266.045 143.401 265.958 143.359 L 265.79 114.664 L 234.642 116.098 L 234.194 143.169 L 186.316 163.095 L 168.728 146.231 L 147.462 168.692 L 163.361 185.54 L 146.557 234.451 L 120.386 234.37 M 210.493 187.051 L 256.57 234.488 C 256.57 234.488 320.45 234.364 320.586 234.229 C 320.721 234.094 306.162 193.954 306.162 193.954 C 306.162 193.954 250.088 170.32 249.69 170.347 M 187.967 210.526 C 187.649 210.754 174.103 248.738 174.103 249.471 C 174.103 250.2 187.998 289.362 188.741 289.362 C 189.632 289.362 227.328 250.756 227.328 249.843 C 227.328 248.581 188.7 209.996 187.967 210.526 M 257.27 265.455 C 256.179 265.594 249.754 271.775 232.923 288.884 C 220.348 301.667 210.161 312.294 210.285 312.494 C 210.41 312.695 212.068 313.532 213.966 314.356 C 215.864 315.18 220.73 317.306 224.778 319.081 C 241.811 326.551 248.988 329.565 249.748 329.565 C 250.514 329.565 252.968 328.567 267.247 322.463 C 272.788 320.092 283.196 315.705 300.652 308.373 L 306.313 305.995 L 308.715 299.518 C 310.034 295.957 311.861 291.004 312.774 288.514 C 313.686 286.021 315.955 279.905 317.813 274.924 C 319.673 269.94 321.108 265.771 321 265.655 C 320.761 265.397 259.289 265.202 257.27 265.455" fill="var(--logo-light)" fill-rule="evenodd"/>
          <g>
            <rect x="389.945" y="234.245" width="88.288" height="31.534" fill="var(--logo-dark)"/>
            <rect x="145.508" y="578.72" width="57.346" height="31.534" fill="var(--logo-dark)" transform="matrix(0.707107, -0.707107, 0.707107, 0.707107, -37.625373, -231.099206)" style="transform-box: fill-box; transform-origin: 50% 50%;"/>
            <rect x="268.056" y="574.269" width="50.735" height="31.007" fill="var(--logo-dark)" transform="matrix(0.707107, 0.707107, -0.707107, 0.707107, 89.660145, -467.147635)" style="transform-origin: 152.09px 305.696px;"/>
            <g transform="matrix(1.929278, 0, 0, 1.929278, -169.667138, -230.101468)">
              <rect x="167.474" y="297.66" width="31.698" height="16.072" fill="var(--logo-dark)" transform="matrix(0.707107, 0.707107, -0.707107, 0.707107, 97.683973, 6.752047)" style="transform-box: fill-box; transform-origin: 50% 50%;"/>
              <ellipse cx="298.182" cy="330.143" rx="18.184" ry="18.184" fill="var(--logo-dark)"/>
            </g>
            <g transform="matrix(0, 1.929278, -1.929278, 0, -117.597083, 162.12922)" style="transform-origin: 290.242px 321.943px;">
              <path d="M 234.052 304.012 L 295.85 303.793 L 295.85 319.865 L 240.576 320.139 L 236.241 324.134 L 225.125 312.911 L 234.052 304.012 Z" fill="var(--logo-dark)" transform="matrix(0.707107, 0.707107, -0.707107, 0.707107, 4.206065, -14.423297)" style="transform-box: fill-box; transform-origin: 50% 50%;"/>
              <ellipse cx="298.182" cy="330.143" rx="18.184" ry="18.184" fill="var(--logo-dark)"/>
            </g>
            <g transform="matrix(-1.364205, 1.364205, -1.364205, -1.364205, -225.276898, -72.630123)" style="transform-origin: 290.242px 321.943px;">
              <rect x="167.474" y="297.66" width="31.698" height="16.072" fill="var(--logo-dark)" transform="matrix(0.707107, 0.707107, -0.707107, 0.707107, 97.683973, 6.752047)" style="transform-box: fill-box; transform-origin: 50% 50%;"/>
              <ellipse cx="298.182" cy="330.143" rx="18.184" ry="18.184" fill="var(--logo-dark)"/>
            </g>
            <g transform="matrix(-1.929277, 0, 0, -1.929277, -96.489034, -305.33812)" style="transform-origin: 290.242px 321.943px;">
              <path d="M 157.74 296.617 L 199.172 297.66 L 199.172 313.732 L 151.483 313.732 L 133.58 295.866 L 144.962 284.648 L 157.74 296.617 Z" fill="var(--logo-dark)" transform="matrix(0.707107, 0.707107, -0.707107, 0.707107, 107.248061, -3.32573)" style="transform-box: fill-box; transform-origin: 50% 50%;"/>
              <ellipse cx="298.182" cy="330.143" rx="18.184" ry="18.184" fill="var(--logo-dark)"/>
            </g>
            <g transform="matrix(0, -1.929278, 1.929278, 0, 97.57237, -210.81805)" style="transform-origin: 290.242px 321.943px;">
              <rect x="299.224" y="297.66" width="56.634" height="16.072" fill="var(--logo-dark)" transform="matrix(0.707107, 0.707107, -0.707107, 0.707107, -37.717619, 15.568392)" style="transform-box: fill-box; transform-origin: 50% 50%;"/>
              <ellipse cx="319.805" cy="350.736" rx="18.184" ry="18.184" fill="var(--logo-dark)"/>
            </g>
          </g>
        </g>
      </svg>
      <h1 class="ob-title">{$t('onboard.welcome')}</h1>
    {:else if mode === 'add' && step === 'connect'}
      <h1 class="ob-title">{$t('onboard.addTitle')}</h1>
    {/if}
    <p class="ob-step">{$t('onboard.stepOf', { n: stepNumber, total: steps.length })}</p>

    {#if step === 'folder'}
      <h2 class="ob-heading">{$t('onboard.folderTitle')}</h2>
      <p class="ob-desc">{$t('onboard.folderDesc')}</p>
      {#if folderError}<p class="ob-error">{folderError}</p>{/if}
      <div class="ob-row">
        <input class="ob-input" bind:value={folder} placeholder="~/00.git" />
        <button class="ob-btn" on:click={browse}>{$t('onboard.browse')}</button>
      </div>
      <button class="ob-btn ob-primary ob-wide" on:click={saveFolder} disabled={folderBusy}>{$t('onboard.continue')}</button>

    {:else if step === 'connect'}
      <h2 class="ob-heading">{$t('onboard.connectTitle')}</h2>

      {#if phase === 'form'}
        <p class="ob-desc">{$t('onboard.connectDesc')}</p>
        <div class="ob-tiles">
          {#each tiles as tile}
            <button class="ob-tile" class:ob-tile-on={choice === tile} on:click={() => choice = tile}>
              {#if tile === 'other'}
                <span class="ob-tile-pair"><span>Forgejo</span><span>Gitea</span></span>
              {:else}
                {providerLabel(tile)}
              {/if}
            </button>
          {/each}
        </div>
        {#if choice === 'other'}
          <div class="ob-row">
            <select class="ob-input ob-kind" bind:value={otherKind}>
              <option value="forgejo">Forgejo</option>
              <option value="gitea">Gitea</option>
            </select>
            <input class="ob-input" bind:value={serverURL} placeholder={$t('onboard.serverURL') + ' (https://…)'} />
          </div>
        {/if}
        <label class="ob-label" for="ob-user">{$t('onboard.username', { provider: providerLabel(provider) })}</label>
        <input class="ob-input ob-full" id="ob-user" bind:value={username} autocomplete="username" />
        <div class="ob-pair">
          <div>
            <label class="ob-label" for="ob-name">{$t('onboard.name')}</label>
            <input class="ob-input ob-full" id="ob-name" bind:value={name} autocomplete="name" />
          </div>
          <div>
            <label class="ob-label" for="ob-email">{$t('onboard.email')}</label>
            <input class="ob-input ob-full" id="ob-email" bind:value={email} autocomplete="email" />
          </div>
        </div>
        <p class="ob-hint">{$t('onboard.identityHint')}</p>

        {#if gcm && !gcm.ok}
          <div class="ob-note">
            <p>{$t('onboard.gcmMissing')}</p>
            {#each gcm.missing || [] as m}
              <p class="ob-small"><strong>{m.displayName}</strong>: <code>{m.installHint}</code></p>
            {/each}
            <button class="ob-btn" on:click={checkGCM}>{$t('onboard.checkAgain')}</button>
          </div>
        {/if}

        {#if connectError}<p class="ob-error">{connectError}</p>{/if}
        <button class="ob-btn ob-primary ob-wide" on:click={connect} disabled={busy || gcm === null}>
          {useToken ? $t('onboard.connectToken') : $t('onboard.connect')}
        </button>
        <div class="ob-links">
          {#if gcm?.ok}
            <button class="ob-link" on:click={() => useToken = !useToken}>
              {useToken ? $t('onboard.useBrowser') : $t('onboard.useToken')}
            </button>
          {/if}
          {#if mode === 'first-run'}
            <button class="ob-link" on:click={skip}>{$t('onboard.skip')}</button>
          {/if}
        </div>

      {:else if phase === 'signing-in'}
        <div class="ob-wait"><div class="ob-spinner"></div><span>{$t('onboard.signingIn')}</span></div>

      {:else if phase === 'failed'}
        <p class="ob-error">{connectError}</p>
        {#if connectDetail}<pre class="ob-detail">{connectDetail}</pre>{/if}
        <button class="ob-btn ob-primary ob-wide" on:click={signIn} disabled={busy}>{$t('onboard.tryAgain')}</button>
        <div class="ob-links">
          <button class="ob-link" on:click={switchToToken} disabled={busy}>{$t('onboard.useToken')}</button>
          <button class="ob-link" on:click={changeDetails} disabled={busy}>{$t('onboard.changeDetails')}</button>
        </div>

      {:else if phase === 'token'}
        <p class="ob-desc">{$t('onboard.tokenSteps')}</p>
        {#if tokenURL}
          <p><a href={tokenURL} on:click|preventDefault={() => BrowserOpenURL(tokenURL)}>{$t('onboard.tokenLink')}</a></p>
        {/if}
        {#if tokenScopes}<p class="ob-small">{$t('onboard.tokenScopes', { scopes: tokenScopes })}</p>{/if}
        <label class="ob-label" for="ob-token">{$t('onboard.token')}</label>
        <input class="ob-input ob-full" id="ob-token" type="password" bind:value={token} />
        {#if connectError}<p class="ob-error">{connectError}</p>{/if}
        {#if connectDetail}<pre class="ob-detail">{connectDetail}</pre>{/if}
        <button class="ob-btn ob-primary ob-wide" on:click={saveToken} disabled={busy || !token.trim()}>{$t('onboard.tokenSave')}</button>
        <div class="ob-links">
          <button class="ob-link" on:click={changeDetails} disabled={busy}>{$t('onboard.changeDetails')}</button>
        </div>
      {/if}

    {:else if step === 'projects'}
      <h2 class="ob-heading">{$t('onboard.projectsTitle')}</h2>
      {#if listing}
        <div class="ob-wait"><div class="ob-spinner"></div><span>{$t('onboard.projectsChecking')}</span></div>
      {:else if listError}
        <p class="ob-error">{$t('onboard.projectsFailed')}</p>
        <pre class="ob-detail">{listError}</pre>
        <button class="ob-btn ob-primary ob-wide" on:click={listRepos}>{$t('onboard.tryAgain')}</button>
        <div class="ob-links"><button class="ob-link" on:click={skip}>{$t('onboard.skip')}</button></div>
      {:else if repos.length === 0}
        <p class="ob-desc">{$t('onboard.projectsNone')}</p>
        <button class="ob-btn ob-primary ob-wide" on:click={skip}>{$t('onboard.done')}</button>
      {:else}
        <p class="ob-desc">{$t('discover.found', { count: repos.length })}</p>
        {#if repos.length > 10}
          <input class="ob-input ob-full ob-filter" bind:value={filter} placeholder={$t('discover.filter')} />
        {/if}
        <label class="ob-repo ob-repo-all">
          <input type="checkbox" checked={allShownChosen} on:change={toggleAllShown} />
          <span>{$t('discover.selectAll')}</span>
        </label>
        <div class="ob-list">
          {#each shown as r (r.fullName)}
            <label class="ob-repo" title={r.description || r.fullName}>
              <input type="checkbox" bind:checked={selected[r.fullName]} />
              <span class="ob-repo-name">{r.fullName}</span>
              {#if r.private}<span class="ob-tag">{$t('onboard.private')}</span>{/if}
              {#if r.fork}<span class="ob-tag">{$t('onboard.fork')}</span>{/if}
              {#if r.archived}<span class="ob-tag">{$t('onboard.archived')}</span>{/if}
            </label>
          {/each}
        </div>
        <button class="ob-btn ob-primary ob-wide" on:click={download} disabled={saving || chosen.length === 0}>
          {$t('onboard.download', { count: chosen.length })}
        </button>
        <div class="ob-links"><button class="ob-link" on:click={skip} disabled={saving}>{$t('onboard.skip')}</button></div>
      {/if}
    {/if}
  </div>
</div>

<style>
  .ob-screen {
    position: fixed; inset: 0; background: var(--bg-base);
    display: flex; align-items: center; justify-content: center; z-index: 200;
    overflow-y: auto;
  }
  .ob-overlay {
    position: fixed; inset: 0; background: var(--overlay);
    display: flex; align-items: center; justify-content: center; z-index: 100;
  }
  .ob-card {
    position: relative; display: flex; flex-direction: column; align-items: stretch;
    width: 440px; max-width: calc(100vw - 32px); max-height: calc(100vh - 32px); overflow-y: auto;
    padding: 32px 32px 24px; box-sizing: border-box;
  }
  .ob-overlay .ob-card { background: var(--bg-card); border: 1px solid var(--border); border-radius: 12px; }
  .ob-close { position: absolute; top: 12px; right: 14px; background: none; border: none; color: var(--text-muted); cursor: pointer; font-size: 16px; }
  .ob-close:hover:not(:disabled) { color: var(--text-primary); }
  .ob-logo { width: 64px; height: 64px; margin: 0 auto 16px; }
  .ob-title { font-size: 22px; font-weight: 700; margin: 0 0 4px; letter-spacing: -0.5px; text-align: center; }
  .ob-step { font-size: 11px; color: var(--text-muted); margin: 0 0 14px; text-align: center; text-transform: uppercase; letter-spacing: 0.5px; }
  .ob-heading { font-size: 15px; font-weight: 600; margin: 0 0 6px; }
  .ob-desc { font-size: 13px; color: var(--text-secondary); margin: 0 0 14px; line-height: 1.4; }
  .ob-hint, .ob-small { font-size: 11px; color: var(--text-muted); margin: 4px 0 12px; line-height: 1.4; }
  .ob-error { font-size: 12px; color: #D81E5B; margin: 8px 0; font-weight: 600; }
  .ob-detail {
    font-size: 11px; color: var(--text-secondary); background: var(--bg-hover); border-radius: 6px;
    padding: 8px; margin: 0 0 12px; white-space: pre-wrap; word-break: break-word; max-height: 120px; overflow-y: auto;
  }
  .ob-label { display: block; font-size: 11px; font-weight: 600; color: var(--text-muted); margin: 10px 0 4px; }
  .ob-row { display: flex; gap: 8px; margin-bottom: 14px; }
  .ob-pair { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
  .ob-input {
    flex: 1; min-width: 0; padding: 6px 8px; font-size: 12px; border: 1px solid var(--border);
    background: var(--bg-base); color: var(--text-primary); border-radius: 5px;
    outline: none; font-family: inherit; transition: border-color 0.12s;
  }
  .ob-input:focus { border-color: #4B95E9; }
  .ob-full { width: 100%; box-sizing: border-box; }
  .ob-kind { flex: 0 0 100px; cursor: pointer; }
  .ob-filter { margin-bottom: 8px; }
  .ob-btn {
    padding: 6px 12px; background: var(--bg-hover); border: 1px solid var(--border-hover);
    color: var(--text-secondary); border-radius: 6px; cursor: pointer; font-size: 12px;
  }
  .ob-btn:hover:not(:disabled) { color: var(--text-primary); }
  .ob-btn:disabled { opacity: 0.4; cursor: default; }
  .ob-primary { background: #4B95E9; border-color: #4B95E9; color: #fff; font-weight: 600; }
  .ob-primary:hover:not(:disabled) { background: #3b82f6; color: #fff; }
  .ob-wide { width: 100%; padding: 9px 0; font-size: 13px; border-radius: 7px; margin-top: 8px; }
  .ob-links { display: flex; justify-content: center; gap: 16px; margin-top: 12px; }
  .ob-link { background: none; border: none; padding: 0; color: var(--text-muted); font-size: 12px; cursor: pointer; text-decoration: underline; }
  .ob-link:hover:not(:disabled) { color: var(--text-primary); }
  .ob-tiles { display: grid; grid-template-columns: repeat(4, 1fr); gap: 6px; margin-bottom: 12px; }
  .ob-tile {
    padding: 10px 4px; font-size: 12px; font-weight: 600; border-radius: 7px; cursor: pointer;
    background: var(--bg-base); border: 1px solid var(--border); color: var(--text-secondary);
  }
  .ob-tile:hover { border-color: var(--border-hover); color: var(--text-primary); }
  .ob-tile-pair { display: flex; flex-direction: column; align-items: center; line-height: 1.3; }
  .ob-tile-on { border-color: #4B95E9; color: var(--text-primary); box-shadow: inset 0 0 0 1px #4B95E9; }
  .ob-note { border: 1px solid #f59e0b66; background: #f59e0b14; border-radius: 7px; padding: 10px 12px; margin: 8px 0; font-size: 12px; }
  .ob-note p { margin: 0 0 6px; }
  .ob-wait { display: flex; align-items: center; gap: 10px; padding: 18px 0; font-size: 13px; color: var(--text-secondary); }
  .ob-spinner { width: 18px; height: 18px; flex-shrink: 0; border: 2px solid var(--border); border-top-color: #4B95E9; border-radius: 50%; animation: ob-spin 0.7s linear infinite; }
  @keyframes ob-spin { to { transform: rotate(360deg); } }
  .ob-list { max-height: 260px; overflow-y: auto; border: 1px solid var(--border); border-radius: 7px; padding: 4px; }
  .ob-repo { display: flex; align-items: center; gap: 8px; padding: 5px 6px; font-size: 13px; border-radius: 4px; cursor: pointer; }
  .ob-repo:hover { background: var(--bg-hover); }
  .ob-repo input { accent-color: #4B95E9; }
  .ob-repo-all { margin-bottom: 4px; }
  .ob-repo-name { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .ob-tag { font-size: 10px; color: var(--text-muted); border: 1px solid var(--border); border-radius: 4px; padding: 0 4px; }
</style>
