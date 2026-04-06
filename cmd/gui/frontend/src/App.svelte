<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { fade, slide } from 'svelte/transition';
  import { bridge, events } from './lib/bridge';
  import {
    configStore, accounts, sources, mirrors, repoStates, mirrorStates, mirrorSummary,
    summary, accountStats, themeStore, applyStatusResults, applyMirrorStatusResults
  } from './lib/stores';
  import { statusColor, credColor, statusLabel, providerLabel, statusSymbol } from './lib/theme';
  import { WindowSetSize, WindowSetMinSize, WindowGetSize, WindowSetPosition, WindowGetPosition, BrowserOpenURL } from '../wailsjs/runtime/runtime';
  import type { RepoState, DiscoverResult, MirrorDTO, MirrorRepo, MirrorStatusResult, MirrorSetupResult, MirrorCredentialCheck } from './lib/types';

  // ── View mode ──
  let viewMode: 'full' | 'compact' = 'full';
  let cardsTab: 'accounts' | 'mirrors' = 'accounts';
  let compactExpanded: Record<string, boolean> = {};
  let savedFullSize: { w: number; h: number } | null = null;
  let savedFullPos: { x: number; y: number } | null = null;

  async function toggleViewMode() {
    // SetViewMode saves current position to the slot we're leaving,
    // then persists the new mode.
    if (viewMode === 'full') {
      const size = await WindowGetSize();
      const pos = await WindowGetPosition();
      savedFullSize = { w: size.w, h: size.h };
      savedFullPos = { x: pos.x, y: pos.y };
      await bridge.setViewMode('compact');
      viewMode = 'compact';
      WindowSetMinSize(200, 200);
      await tick();
      // Wait for slide transitions on expanded accounts (120ms) + buffer.
      setTimeout(() => fitCompactHeight(), 250);
      // Second pass catches any remaining layout shifts.
      setTimeout(() => fitCompactHeight(), 500);
    } else {
      await bridge.setViewMode('full');
      viewMode = 'full';
      WindowSetMinSize(640, 480);
      await tick();
      const target = savedFullSize ?? { w: 900, h: 700 };
      setTimeout(() => {
        WindowSetSize(target.w, target.h);
        if (savedFullPos) {
          WindowSetPosition(savedFullPos.x, savedFullPos.y);
        }
      }, 50);
    }
  }

  async function toggleCompactAcct(key: string) {
    compactExpanded[key] = !compactExpanded[key];
    compactExpanded = compactExpanded;
    // Wait for Svelte to render + slide transition (120ms), then resize.
    await tick();
    setTimeout(() => fitCompactHeight(), 200);
  }

  async function fitCompactHeight() {
    if (viewMode !== 'compact') return;
    const strip = document.querySelector('.compact-strip') as HTMLElement | null;
    if (!strip) return;
    // Clone the strip off-screen with no height constraint to measure natural height.
    const clone = strip.cloneNode(true) as HTMLElement;
    clone.style.position = 'absolute';
    clone.style.left = '-9999px';
    clone.style.top = '0';
    clone.style.minHeight = '0';
    clone.style.height = 'auto';
    clone.style.overflow = 'visible';
    clone.style.width = strip.offsetWidth + 'px';
    document.body.appendChild(clone);
    const contentH = clone.offsetHeight;
    document.body.removeChild(clone);
    // Wails WebView2: window.outerHeight ≈ innerHeight (title bar not included).
    // Use WindowGetSize() which returns the real OS window size to compute chrome.
    const winSize = await WindowGetSize();
    const chrome = winSize.h - window.innerHeight;
    const desired = Math.min(contentH + chrome, screen.availHeight);
    WindowSetSize(220, Math.max(desired, 200));
  }

  // ── Global identity warning ──
  let globalIdentityWarn: { hasName: boolean; hasEmail: boolean; name: string; email: string } | null = null;

  async function checkGlobalIdentity() {
    try {
      const gs = await bridge.checkGlobalIdentity();
      globalIdentityWarn = (gs.hasName || gs.hasEmail) ? gs : null;
    } catch { globalIdentityWarn = null; }
  }

  async function fixGlobalIdentity() {
    try {
      await bridge.removeGlobalIdentity();
      globalIdentityWarn = null;
    } catch (e: any) {
      console.error('Failed to remove global identity:', e);
    }
  }

  // ── Onboarding ──
  let firstRun = false;
  let onboardFolder = '~/00.git';
  let onboardError = '';

  async function browseFolder(target: 'onboard' | 'settings') {
    const dir = await bridge.pickFolder('Choose clone folder');
    if (dir) {
      if (target === 'onboard') onboardFolder = dir;
      else changeFolderPath = dir;
    }
  }

  async function finishOnboarding() {
    onboardError = '';
    if (!onboardFolder.trim()) { onboardError = 'Folder path is required'; return; }
    try {
      await bridge.setGlobalFolder(onboardFolder);
      firstRun = false;
      await initDashboard();
    } catch (err: any) {
      onboardError = err?.message || String(err);
    }
  }

  // ── Change folder (settings) ──
  let changeFolderModal = false;
  let changeFolderPath = '';
  let changeFolderError = '';

  function openChangeFolder() {
    changeFolderPath = '';
    changeFolderError = '';
    changeFolderModal = true;
  }

  async function confirmChangeFolder() {
    changeFolderError = '';
    if (!changeFolderPath.trim()) { changeFolderError = 'Folder path is required'; return; }
    try {
      await bridge.setGlobalFolder(changeFolderPath);
      changeFolderModal = false;
      await reloadFromDisk();
    } catch (err: any) {
      changeFolderError = err?.message || String(err);
    }
  }

  // ── Theme ──
  let themeChoice: 'system' | 'light' | 'dark' = 'system';
  let resolvedTheme: 'light' | 'dark' = 'dark';

  function applyTheme() {
    if (themeChoice === 'system') {
      resolvedTheme = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
    } else {
      resolvedTheme = themeChoice;
    }
    themeStore.set(resolvedTheme);
    document.documentElement.setAttribute('data-theme', resolvedTheme);
  }

  function cycleTheme() {
    const order: Array<'system' | 'light' | 'dark'> = ['system', 'light', 'dark'];
    themeChoice = order[(order.indexOf(themeChoice) + 1) % 3];
    applyTheme();
  }

  function themeIcon(choice: string): string {
    return ({ system: '◐', light: '☀', dark: '☾' } as Record<string, string>)[choice] || '◐';
  }

  // ── Sync state ──
  let syncing = false;

  async function syncAll() {
    if (syncing) return;
    syncing = true;

    let currentStates: Record<string, RepoState> = {};
    repoStates.subscribe((v) => (currentStates = v))();

    const needsSync = Object.entries(currentStates).filter(
      ([_, r]) => r.status === 'behind' || r.status === 'not cloned'
    );

    for (const [key] of needsSync) {
      const [sourceKey, ...repoParts] = key.split('/');
      const repoKey = repoParts.join('/');
      const state = currentStates[key];

      if (state.status === 'not cloned') {
        repoStates.update((s) => {
          s[key] = { ...s[key], status: 'cloning', progress: 0 };
          return { ...s };
        });
        bridge.cloneRepo(sourceKey, repoKey);
      } else {
        repoStates.update((s) => {
          s[key] = { ...s[key], status: 'syncing', progress: 0 };
          return { ...s };
        });
        bridge.pullRepo(sourceKey, repoKey);
      }
    }
    if (needsSync.length === 0) syncing = false;
  }

  function syncRepo(sourceKey: string, repoKey: string) {
    const key = `${sourceKey}/${repoKey}`;
    repoStates.update((s) => {
      s[key] = { ...s[key], status: 'syncing', progress: 0 };
      return { ...s };
    });
    bridge.pullRepo(sourceKey, repoKey);
  }

  function cloneRepo(sourceKey: string, repoKey: string) {
    const key = `${sourceKey}/${repoKey}`;
    repoStates.update((s) => {
      s[key] = { ...s[key], status: 'cloning', progress: 0 };
      return { ...s };
    });
    bridge.cloneRepo(sourceKey, repoKey);
  }

  // Track which repos are currently fetching (separate from status).
  let fetchingRepos: Record<string, boolean> = {};

  function fetchRepo(sourceKey: string, repoKey: string) {
    const key = `${sourceKey}/${repoKey}`;
    fetchingRepos[key] = true;
    fetchingRepos = fetchingRepos;
    bridge.fetchRepo(sourceKey, repoKey);
  }

  // ── Repo detail panel ──
  let expandedRepo: string | null = null;
  let repoDetail: { branch: string; ahead: number; behind: number; changed: { kind: string; path: string }[]; untracked: string[]; error?: string } | null = null;
  let detailLoading = false;

  // Auto-collapse expanded detail when the repo no longer needs attention.
  const nonExpandable = new Set(['clean', 'behind', 'not cloned', 'cloning', 'syncing']);
  $: if (expandedRepo) {
    const st = $repoStates[expandedRepo];
    if (st && nonExpandable.has(st.status)) {
      expandedRepo = null;
      repoDetail = null;
    }
  }

  async function toggleRepoDetail(sourceKey: string, repoName: string, status: string) {
    // Only expand for repos that need attention (not clean, behind, not cloned, or in-progress)
    if (status === 'clean' || status === 'behind' || status === 'not cloned' || status === 'cloning' || status === 'syncing') return;
    const key = `${sourceKey}/${repoName}`;
    if (expandedRepo === key && !detailLoading) {
      expandedRepo = null;
      repoDetail = null;
      return;
    }
    if (detailLoading) return; // Ignore clicks while a detail fetch is in flight
    expandedRepo = key;
    repoDetail = null;
    detailLoading = true;
    try {
      const raw = await bridge.getRepoDetail(sourceKey, repoName);
      // Go nil slices serialize as JSON null — normalise to empty arrays.
      repoDetail = { ...raw, changed: raw.changed || [], untracked: raw.untracked || [] };
    } catch (e: any) {
      repoDetail = { branch: '', ahead: 0, behind: 0, changed: [], untracked: [], error: String(e) };
    }
    detailLoading = false;
  }

  function kindIcon(kind: string): string {
    return kind === 'deleted' ? '−' : kind === 'added' ? '+' : kind === 'renamed' ? '→' : '~';
  }

  function kindLabel(kind: string): string {
    return kind === 'deleted' ? 'Deleted' : kind === 'added' ? 'New file' : kind === 'renamed' ? 'Renamed' : kind === 'conflict' ? 'Conflict' : 'Changed';
  }

  // ── Discovery ──
  let discoverModal: string | null = null;
  let discoverLoading = false;
  let discoverRepos: DiscoverResult[] = [];
  let discoverSelected: Record<string, boolean> = {};
  let discoverFilter = '';

  // SSH discovery token modal state.
  let sshDiscoverTokenModal: string | null = null; // account key needing a PAT for discovery

  function openDiscover(accountKey: string) {
    const acct = $accounts[accountKey];
    // SSH accounts need a PAT for API discovery. Check if one exists by
    // attempting discovery — if it fails, the discover:done event will show
    // the error. Instead, proactively check: if SSH and credential is ok but
    // we know API access needs a token, show the token modal.
    if (acct?.default_credential_type === 'ssh') {
      // Try discovery — if it fails with "no API token", the event handler
      // will open the token modal. This avoids a redundant backend call.
      sshDiscoverTokenModal = null;
    }
    discoverModal = accountKey;
    discoverLoading = true;
    discoverRepos = [];
    discoverSelected = {};
    discoverFilter = '';
    bridge.discover(accountKey);
  }

  $: filteredDiscoverRepos = discoverFilter.trim()
    ? discoverRepos.filter((r) => r.fullName.toLowerCase().includes(discoverFilter.trim().toLowerCase()))
    : discoverRepos;
  $: existingRepos = discoverModal ? ($sources[discoverModal]?.repos || {}) : {};
  $: selectableRepos = filteredDiscoverRepos.filter((r) => !existingRepos[r.fullName]);
  $: selectedCount = Object.values(discoverSelected).filter(Boolean).length;
  $: allSelected = selectableRepos.length > 0 && selectableRepos.every((r) => discoverSelected[r.fullName]);

  function toggleAll() {
    if (allSelected) {
      for (const r of selectableRepos) delete discoverSelected[r.fullName];
      discoverSelected = discoverSelected;
    } else {
      for (const r of selectableRepos) discoverSelected[r.fullName] = true;
      discoverSelected = discoverSelected;
    }
  }

  async function addDiscovered() {
    if (!discoverModal) return;
    const sourceKey = discoverModal;
    const selected = Object.entries(discoverSelected)
      .filter(([_, v]) => v)
      .map(([k]) => k);
    if (selected.length > 0) {
      await bridge.addDiscoveredRepos(sourceKey, selected);
      await reloadFromDisk();
      // Clone each newly added repo that isn't already local.
      for (const repoName of selected) {
        const repoKey = `${sourceKey}/${repoName}`;
        const state = $repoStates[repoKey];
        if (!state || state.status === 'not cloned') {
          cloneRepo(sourceKey, repoName);
        }
      }
    }
    discoverModal = null;
  }

  // Re-read config from disk and refresh all repo statuses.
  async function reloadFromDisk() {
    const cfg = await bridge.reloadConfig();
    configStore.set(cfg);
    configPath = await bridge.getConfigPath();
    const saved = await bridge.getPeriodicSync();
    if (saved !== fetchInterval) applyFetchInterval(saved);
    const statuses = await bridge.getAllStatus();
    applyStatusResults(statuses);
    verifyAllCredentials();
  }

  // ── Delete mode ──
  let deleteMode = false;
  let deleteConfirm: { sourceKey: string; repoKey: string; status: string } | null = null;
  let deleteConfirmStep = 0; // 0=closed, 1=first confirm, 2=final confirm
  let deleting = false;

  function askDelete(sourceKey: string, repoKey: string, status: string) {
    deleteConfirm = { sourceKey, repoKey, status };
    deleteConfirmStep = 1;
  }

  function cancelDelete() {
    deleteConfirm = null;
    deleteConfirmStep = 0;
  }

  async function confirmDelete() {
    if (!deleteConfirm) return;
    if (deleteConfirmStep === 1) {
      deleteConfirmStep = 2;
      return;
    }
    // Step 2 — actually delete.
    deleting = true;
    const deletedKey = `${deleteConfirm.sourceKey}/${deleteConfirm.repoKey}`;
    try {
      await bridge.deleteRepo(deleteConfirm.sourceKey, deleteConfirm.repoKey);
      // Clear expanded detail if the deleted repo was open.
      if (expandedRepo === deletedKey) {
        expandedRepo = null;
        repoDetail = null;
      }
      await reloadFromDisk();
    } finally {
      deleting = false;
      deleteConfirm = null;
      deleteConfirmStep = 0;
      deleteMode = false;
    }
  }

  function isDangerous(status: string): boolean {
    return ['dirty', 'ahead', 'diverged', 'conflict'].includes(status);
  }

  function deleteWarning(status: string): string {
    if (status === 'not cloned') return 'This will remove the config entry. No local folder exists.';
    if (isDangerous(status)) return 'This repo has unpushed commits or local changes that will be permanently lost!';
    return 'The local folder and config entry will be permanently deleted.';
  }

  // ── Edit account ──
  let editAccountModal: string | null = null;
  let editAccountError = '';
  let editAcct = { key: '', provider: '', url: '', username: '', name: '', email: '' };

  function openEditAccount(key: string) {
    const acct = $accounts[key];
    if (!acct) return;
    editAcct = {
      key,
      provider: acct.provider || '',
      url: acct.url || '',
      username: acct.username || '',
      name: acct.name || '',
      email: acct.email || '',
    };
    editAccountError = '';
    editAccountModal = key;
  }

  async function submitEditAccount() {
    if (!editAccountModal) return;
    editAccountError = '';
    const newKey = editAcct.key.trim();
    if (!newKey) { editAccountError = 'Account key is required'; return; }
    if (!/^[a-zA-Z0-9][a-zA-Z0-9-]*$/.test(newKey)) { editAccountError = 'Key: letters, numbers, hyphens only'; return; }
    if (!editAcct.url.trim()) { editAccountError = 'URL is required'; return; }
    if (!editAcct.username.trim()) { editAccountError = 'Username is required'; return; }
    try {
      const oldKey = editAccountModal;
      const keyChanged = newKey !== oldKey;
      if (keyChanged) {
        await bridge.renameAccount(oldKey, newKey);
      }
      await bridge.updateAccount({ key: newKey, ...editAcct });
      await reloadFromDisk();
      verifyAllCredentials();
      editAccountModal = null;
    } catch (e: any) {
      editAccountError = e?.message || String(e);
    }
  }

  // ── Add account ──
  let addAccountModal = false;
  let addAccountError = '';
  let addAccountStep: 'form' | 'credential' = 'form';
  let addAcct = { key: '', provider: 'github', url: 'https://github.com', username: '', name: '', email: '', credentialType: 'gcm' };

  // Shared credential operation state — used by BOTH add-account and change-credential flows.
  // Only one modal can be active at a time, so sharing is safe.
  let credResult: { ok: boolean; message: string; needsPAT?: boolean; sshPublicKey?: string; sshAddURL?: string; sshVerified?: boolean } | null = null;
  let credBusy = false;
  let credTokenInput = '';
  let credTokenGuide = '';
  let credTokenScopes = '';
  let sshDiscoveryTokenInput = '';
  let sshDiscoveryGuide = '';
  let sshDiscoveryScopes = '';
  let sshDiscoveryBusy = false;

  const providerURLs: Record<string, string> = {
    github: 'https://github.com',
    gitlab: 'https://gitlab.com',
    gitea: '',
    forgejo: '',
    bitbucket: 'https://bitbucket.org',
  };

  function onProviderChange() {
    const defaultURL = providerURLs[addAcct.provider];
    if (defaultURL !== undefined) addAcct.url = defaultURL;
  }

  // Helper to reset all shared credential state.
  function resetCredState() {
    credResult = null;
    credBusy = false;
    credTokenInput = '';
    credTokenGuide = '';
    credTokenScopes = '';
    sshDiscoveryTokenInput = '';
    sshDiscoveryGuide = '';
    sshDiscoveryScopes = '';
    sshDiscoveryBusy = false;
  }

  function resetAddAccount() {
    addAcct = { key: '', provider: 'github', url: 'https://github.com', username: '', name: '', email: '', defaultBranch: 'main', credentialType: 'gcm' };
    addAccountError = '';
    addAccountStep = 'form';
    addAccountModal = false;
    resetCredState();
    verifyAllCredentials();
  }

  async function submitAddAccount() {
    addAccountError = '';
    if (!addAcct.key.trim()) { addAccountError = 'Account key is required'; return; }
    if (!addAcct.provider) { addAccountError = 'Provider is required'; return; }
    if (!addAcct.url.trim()) { addAccountError = 'URL is required'; return; }
    if (!addAcct.username.trim()) { addAccountError = 'Username is required'; return; }
    try {
      await bridge.addAccount(addAcct);
      await reloadFromDisk();
      addAccountStep = 'credential';
      credResult = null;
      if (addAcct.credentialType === 'gcm' || addAcct.credentialType === 'ssh') {
        runCredSetup(addAcct.key, addAcct.credentialType);
      } else if (addAcct.credentialType === 'token') {
        await fetchTokenGuide(addAcct.key);
      }
    } catch (err: any) {
      addAccountError = err?.message || String(err);
    }
  }

  // ── Shared credential functions (used by both add-account and change-credential) ──

  // Returns the active account key for whichever credential modal is open.
  let sshKeyCopied = false;
  function copySSHKey(text: string) {
    navigator.clipboard.writeText(text).then(() => {
      sshKeyCopied = true;
      setTimeout(() => { sshKeyCopied = false; }, 2000);
    });
  }

  function credAccountKey(): string {
    return credChangeModal || addAcct.key;
  }

  // Fetch token guide URL and scopes for the active account.
  async function fetchTokenGuide(accountKey: string) {
    const guide = await bridge.getTokenGuide(accountKey);
    credTokenGuide = guide.creationURL || '';
    credTokenScopes = guide.scopes || '';
  }

  // Post-setup: verify credential status on cards.
  async function credPostSetup(accountKey: string) {
    if (!credResult?.ok) return;
    bridge.credentialVerify(accountKey).then((cs) => {
      credStatuses[accountKey] = {status: cs.status, primary: cs.primary, pat: cs.pat};
      credStatuses = credStatuses;
    }).catch(() => {
      credStatuses[accountKey] = {status: 'ok', primary: 'ok', pat: 'ok'};
      credStatuses = credStatuses;
    });
  }

  // Run credential setup (GCM, Token, or SSH) for the given account.
  async function runCredSetup(accountKey: string, credType: string) {
    credBusy = true;
    credResult = null;
    try {
      if (credType === 'gcm') {
        credResult = await bridge.credentialSetupGCM(accountKey);
      } else if (credType === 'token') {
        credResult = await bridge.credentialStoreToken(accountKey, credTokenInput);
      } else if (credType === 'ssh') {
        credResult = await bridge.credentialSetupSSH(accountKey);
      }
    } catch (err: any) {
      credResult = { ok: false, message: err?.message || String(err) };
    }
    credBusy = false;
    await credPostSetup(accountKey);
  }

  // Store a token for the active account (used by both add-account and change-credential token flows).
  async function storeCredToken() {
    const key = credAccountKey();
    credBusy = true;
    try {
      credResult = await bridge.credentialStoreToken(key, credTokenInput);
    } catch (err: any) {
      credResult = { ok: false, message: err?.message || String(err) };
    }
    credBusy = false;
    await credPostSetup(key);
  }

  // Verify SSH connection without regenerating the key.
  async function verifySSHConnection() {
    const key = credAccountKey();
    credBusy = true;
    credResult = null;
    try {
      credResult = await bridge.credentialSetupSSH(key);
    } catch (err: any) {
      credResult = { ok: false, message: err?.message || String(err) };
    }
    credBusy = false;
    await credPostSetup(key);
  }

  // Regenerate SSH key (deletes old, creates new).
  async function regenerateSSHKey() {
    const key = credAccountKey();
    credBusy = true;
    credResult = null;
    try {
      credResult = await bridge.credentialRegenerateSSH(key);
    } catch (err: any) {
      credResult = { ok: false, message: err?.message || String(err) };
    }
    credBusy = false;
    await credPostSetup(key);
  }

  // Store SSH discovery token and then open discovery.
  async function storeSSHDiscoveryToken() {
    const key = sshDiscoverTokenModal;
    if (!key) return;
    sshDiscoveryBusy = true;
    try {
      await bridge.credentialStoreToken(key, sshDiscoveryTokenInput);
      sshDiscoveryTokenInput = '';
      sshDiscoverTokenModal = null;
      // Now open discovery with the stored token.
      openDiscover(key);
    } catch (err: any) {
      // Keep modal open so user can retry.
    }
    sshDiscoveryBusy = false;
  }

  // ── Change credential type ──
  let credChangeModal: string | null = null; // account key
  let credChangeType = '';
  let credDeleteBusy = false;
  let credSetupStarted = false;
  let credForceToken = false; // force PAT input (for mirror fix)

  function openCredChange(accountKey: string, currentType: string) {
    credChangeModal = accountKey;
    credChangeType = currentType;
    credForceToken = false;
    resetCredState();
    credDeleteBusy = false;
    credSetupStarted = false;
  }

  async function openTokenSetup(accountKey: string) {
    credChangeModal = accountKey;
    credChangeType = 'token';
    credForceToken = true;
    resetCredState();
    credDeleteBusy = false;
    credSetupStarted = true;
    await fetchTokenGuide(accountKey);
  }

  async function deleteCredential() {
    if (!credChangeModal) return;
    const key = credChangeModal;
    credDeleteBusy = true;
    try {
      await bridge.credentialDelete(key);
      await reloadFromDisk();
      credStatuses[key] = {status: 'none', primary: 'none', pat: 'none'};
      credStatuses = credStatuses;
    } catch (err: any) {
      credResult = { ok: false, message: err?.message || String(err) };
      credDeleteBusy = false;
      return;
    }
    credDeleteBusy = false;
    credChangeModal = null;
  }

  function closeCredChange() {
    credChangeModal = null;
    resetCredState();
    credDeleteBusy = false;
    verifyAllCredentials();
  }

  async function applyCredChange() {
    if (!credChangeModal) return;
    credSetupStarted = true;
    credBusy = true;
    credResult = null;
    try {
      await bridge.changeCredentialType(credChangeModal, credChangeType);
      await reloadFromDisk();
      if (credChangeType === 'gcm') {
        credResult = await bridge.credentialSetupGCM(credChangeModal);
      } else if (credChangeType === 'ssh') {
        credResult = await bridge.credentialSetupSSH(credChangeModal);
      } else if (credChangeType === 'token') {
        await fetchTokenGuide(credChangeModal);
        credBusy = false;
        return; // Wait for user to paste token.
      }
    } catch (err: any) {
      credResult = { ok: false, message: err?.message || String(err) };
    }
    credBusy = false;
    await credPostSetup(credChangeModal);
  }

  // ── Delete account ──
  let deleteAcctConfirm: string | null = null; // account key
  let deleteAcctStep = 0; // 0=inactive, 1=type name, 2=warning, 3=final confirm
  let deleteAcctBusy = false;
  let deleteAcctInput = '';
  let deleteAcctError = '';

  function askDeleteAccount(accountKey: string) {
    deleteAcctConfirm = accountKey;
    deleteAcctStep = 1;
    deleteAcctInput = '';
    deleteAcctError = '';
  }

  function cancelDeleteAccount() {
    deleteAcctConfirm = null;
    deleteAcctStep = 0;
    deleteAcctInput = '';
    deleteAcctError = '';
  }

  function deleteAcctCheckName() {
    if (deleteAcctInput.trim() === deleteAcctConfirm) {
      deleteAcctStep = 2;
      deleteAcctError = '';
    } else {
      deleteAcctError = 'Name does not match.';
    }
  }

  async function confirmDeleteAccount() {
    if (!deleteAcctConfirm) return;
    if (deleteAcctStep === 2) { deleteAcctStep = 3; return; }
    deleteAcctBusy = true;
    try {
      await bridge.deleteAccount(deleteAcctConfirm);
      await reloadFromDisk();
    } finally {
      deleteAcctBusy = false;
      deleteAcctConfirm = null;
      deleteAcctStep = 0;
      deleteAcctInput = '';
      deleteMode = false;
    }
  }

  // ── Mirrors ──
  let mirrorChecking: Record<string, boolean> = {};
  let addMirrorGroupModal = false;
  let addMirrorRepoModal: string | null = null; // holds mirrorKey
  let deleteMirrorGroupConfirm: string | null = null;
  let deleteMirrorRepoConfirm: { mirrorKey: string; repoKey: string } | null = null;
  let mirrorSetupResultModal: MirrorSetupResult | null = null;
  let mirrorCredWarning: MirrorCredentialCheck | null = null;

  // Mirror discovery
  let mirrorDiscoverLoading = false;
  let mirrorDiscoverResults: any[] | null = null;
  let mirrorDiscoverError = '';
  let mirrorDiscoverProgress: { phase: string; account: string; current: number; total: number } | null = null;

  async function runMirrorDiscover() {
    mirrorDiscoverLoading = true;
    mirrorDiscoverResults = null;
    mirrorDiscoverError = '';
    mirrorDiscoverProgress = null;
    discoverAdded = {};
    await bridge.discoverMirrors();
  }

  // Check if a discovered mirror repo is already configured.
  function isDiscoveredRepoConfigured(accountSrc: string, accountDst: string, repoKey: string): boolean {
    for (const mir of Object.values($mirrors)) {
      if ((mir.account_src === accountSrc && mir.account_dst === accountDst) ||
          (mir.account_src === accountDst && mir.account_dst === accountSrc)) {
        if (mir.repos[repoKey]) return true;
      }
    }
    return false;
  }

  // Track which repos have been individually added during this discover session.
  let discoverAdded: Record<string, boolean> = {};

  async function addDiscoveredRepo(dr: any, dm: any) {
    const addKey = `${dr.AccountSrc}:${dr.AccountDst}:${dm.RepoKey}`;
    try {
      // Find or create mirror group for this account pair.
      let groupKey = '';
      for (const [key, mir] of Object.entries($mirrors)) {
        const m = mir as any;
        if ((m.account_src === dr.AccountSrc && m.account_dst === dr.AccountDst) ||
            (m.account_src === dr.AccountDst && m.account_dst === dr.AccountSrc)) {
          groupKey = key;
          break;
        }
      }
      if (!groupKey) {
        groupKey = dr.MirrorKey;
        await bridge.addMirrorGroup(groupKey, dr.AccountSrc, dr.AccountDst);
      }
      await bridge.addMirrorRepo(groupKey, dm.RepoKey, dm.Direction, dm.Origin);
      $configStore = await bridge.reloadConfig();
      discoverAdded = { ...discoverAdded, [addKey]: true };
    } catch (e: any) {
      alert(e?.message || e);
    }
  }

  async function applyMirrorDiscovery() {
    try {
      await bridge.applyDiscoveredMirrors();
      $configStore = await bridge.reloadConfig();
      mirrorDiscoverResults = null;
      discoverAdded = {};
      checkAllMirrorStatus();
    } catch (e: any) {
      alert(e?.message || e);
    }
  }

  // Create repo modal
  let createRepoModal: string | null = null;
  let createRepoOrgs: string[] = [];
  let createRepoOwner = '';
  let createRepoName = '';
  let createRepoDesc = '';
  let createRepoPrivate = true;
  let createRepoClone = true;
  let createRepoBusy = false;
  let createRepoError = '';
  let createRepoNameError = '';

  function sanitizeRepoName(raw: string): string {
    // Replace spaces with hyphens, strip everything except [a-zA-Z0-9._-]
    return raw.replace(/\s+/g, '-').replace(/[^a-zA-Z0-9._-]/g, '');
  }

  function onCreateRepoNameInput() {
    createRepoName = sanitizeRepoName(createRepoName);
    if (createRepoName.length === 0) {
      createRepoNameError = '';
    } else if (/^[.-]/.test(createRepoName)) {
      createRepoNameError = 'Name cannot start with a dot or hyphen';
    } else if (/[.-]$/.test(createRepoName)) {
      createRepoNameError = 'Name cannot end with a dot or hyphen';
    } else if (/\.\./.test(createRepoName)) {
      createRepoNameError = 'Name cannot contain consecutive dots';
    } else {
      createRepoNameError = '';
    }
  }

  async function openCreateRepo(accountKey: string) {
    createRepoModal = accountKey;
    createRepoOrgs = [];
    createRepoOwner = '';
    createRepoName = '';
    createRepoDesc = '';
    createRepoPrivate = true;
    createRepoClone = true;
    createRepoBusy = false;
    createRepoError = '';
    createRepoNameError = '';
    try {
      createRepoOrgs = await bridge.listAccountOrgs(accountKey);
      if (createRepoOrgs.length > 0) createRepoOwner = createRepoOrgs[0];
    } catch (e: any) {
      createRepoError = e?.message || 'Failed to load organizations';
    }
  }

  async function submitCreateRepo() {
    if (!createRepoModal || !createRepoOwner || !createRepoName) return;
    createRepoBusy = true;
    createRepoError = '';
    try {
      const acctKey = createRepoModal;
      const repoKey = createRepoOwner + '/' + createRepoName;
      const willClone = createRepoClone;
      await bridge.createNewRepo(acctKey, createRepoOwner, createRepoName, createRepoDesc, createRepoPrivate, willClone);
      $configStore = await bridge.reloadConfig();
      // Pre-set status to 'cloning' so the repo doesn't flash as 'error'
      // before the async clone finishes.
      if (willClone) {
        repoStates.update((s) => ({
          ...s,
          [`${acctKey}/${repoKey}`]: { status: 'cloning', progress: 0, behind: 0, modified: 0, untracked: 0, ahead: 0 },
        }));
      }
      createRepoModal = null;
    } catch (e: any) {
      createRepoError = e?.message || 'Failed to create repository';
    } finally {
      createRepoBusy = false;
    }
  }

  // Add mirror group form
  let newMirrorKey = '';
  let newMirrorSrc = '';
  let newMirrorDst = '';

  // Add mirror repo form
  let newMirrorRepoKey = '';
  let newMirrorRepoDirection = 'push';
  let newMirrorRepoOrigin = 'src';
  let newMirrorRepoAutoSetup = false;
  let mirrorRepoPickerRepos: { fullName: string; description: string; private: boolean; fork: boolean; archived: boolean }[] = [];
  let mirrorRepoPickerLoading = false;
  let mirrorRepoPickerFilter = '';
  let mirrorRepoPickerLoaded = false;

  function mirrorDirLabel(repo: MirrorRepo, m: MirrorDTO): string {
    const origin = repo.origin === 'src' ? m.account_src : m.account_dst;
    const backup = repo.origin === 'src' ? m.account_dst : m.account_src;
    if (repo.direction === 'push') return `${origin} → ${backup} (mirror)`;
    return `${backup} (mirror) ← ${origin}`;
  }

  function mirrorDirLabelHtml(repo: MirrorRepo, m: MirrorDTO): string {
    const origin = repo.origin === 'src' ? m.account_src : m.account_dst;
    const backup = repo.origin === 'src' ? m.account_dst : m.account_src;
    const src = `<span class="mir-origin">${origin}</span>`;
    const dst = `<span class="mir-backup">${backup}</span>`;
    if (repo.direction === 'push') return `${src} <span class="mir-arrow">⟶</span> ${dst}`;
    return `${dst} <span class="mir-arrow">⟵</span> ${src}`;
  }

  function mirrorStatusColor(repo: MirrorRepo, live: MirrorStatusResult | undefined, theme: string): string {
    if (live?.error) return statusColor('error', theme);
    if (live?.syncStatus === 'synced') return statusColor('clean', theme);
    if (live?.syncStatus === 'behind') return statusColor('behind', theme);
    if (live?.syncStatus === 'ahead') return statusColor('error', theme);
    if (!live) return statusColor('not cloned', theme); // unchecked
    return statusColor('clean', theme);
  }

  function mirrorStatusSymbol(repo: MirrorRepo, live: MirrorStatusResult | undefined): string {
    if (live?.error) return '✕';
    if (live?.syncStatus === 'synced') return '●';
    if (live?.syncStatus === 'behind') return '↓';
    if (live?.syncStatus === 'ahead') return '↑';
    if (!live) return '○'; // unchecked
    return '●';
  }

  /** Summarize live mirror status in a short, user-friendly string. */
  function mirrorStatusText(repo: MirrorRepo, live: MirrorStatusResult | undefined): string {
    if (live?.error) return friendlyMirrorError(live.error);
    if (live?.syncStatus === 'synced') return 'Synced OK';
    if (live?.syncStatus === 'behind') return 'Backup is behind origin';
    if (live?.syncStatus === 'ahead') return 'Backup is ahead of origin';
    if (!live) return 'unchecked';
    return 'OK';
  }

  /** Turn raw Go error strings into concise, user-friendly messages. */
  function friendlyMirrorError(raw: string): string {
    // Errors from CheckStatus are already user-friendly (e.g. "missing API token in git-parchis-luis").
    if (raw.length > 60) return raw.slice(0, 57) + '...';
    return raw;
  }

  $: mirrorGroupStats = (() => {
    const stats: Record<string, { active: number; unchecked: number; error: number; total: number }> = {};
    for (const [key, mir] of Object.entries($mirrors)) {
      let active = 0, unchecked = 0, error = 0, total = 0;
      for (const repoKey of Object.keys(mir.repos)) {
        total++;
        const live = $mirrorStates[`${key}/${repoKey}`];
        if (!live) unchecked++;
        else if (live.error) error++;
        else active++;
      }
      stats[key] = { active, unchecked, error, total };
    }
    return stats;
  })();

  async function checkMirrorStatus(mirrorKey: string) {
    mirrorChecking = { ...mirrorChecking, [mirrorKey]: true };
    await bridge.getMirrorStatus(mirrorKey);
  }

  async function checkAllMirrorStatus() {
    for (const key of Object.keys($mirrors)) {
      mirrorChecking = { ...mirrorChecking, [key]: true };
      await bridge.getMirrorStatus(key);
    }
  }

  async function setupMirrorRepo(mirrorKey: string, repoKey: string) {
    // Check credentials first for the account that provides the remote token.
    const m = $mirrors[mirrorKey];
    if (!m) return;
    const repo = m.repos[repoKey];
    if (!repo) return;
    const remoteAcctKey = repo.direction === 'push'
      ? m.account_dst   // push: backup token goes remote
      : (repo.origin === 'src' ? m.account_src : m.account_dst); // pull: origin token goes remote
    const credCheck = await bridge.checkMirrorCredentials(remoteAcctKey);
    if (credCheck.needsPat && !credCheck.hasMirrorToken) {
      mirrorCredWarning = credCheck;
      return;
    }
    await bridge.setupMirrorRepo(mirrorKey, repoKey);
  }

  async function submitAddMirrorGroup() {
    if (!newMirrorKey || !newMirrorSrc || !newMirrorDst) return;
    try {
      await bridge.addMirrorGroup(newMirrorKey, newMirrorSrc, newMirrorDst);
      $configStore = await bridge.reloadConfig();
      addMirrorGroupModal = false;
      newMirrorKey = ''; newMirrorSrc = ''; newMirrorDst = '';
    } catch (e: any) {
      alert(e?.message || e);
    }
  }

  async function loadMirrorRepoList() {
    if (!addMirrorRepoModal) return;
    const mir = $mirrors[addMirrorRepoModal];
    if (!mir) return;
    const acctKey = newMirrorRepoOrigin === 'src' ? mir.account_src : mir.account_dst;
    mirrorRepoPickerLoading = true;
    mirrorRepoPickerFilter = '';
    mirrorRepoPickerRepos = [];
    mirrorRepoPickerLoaded = false;
    try {
      const repos = await bridge.listRemoteRepos(acctKey);
      mirrorRepoPickerRepos = (repos || []).sort((a: any, b: any) => a.fullName.localeCompare(b.fullName));
      mirrorRepoPickerLoaded = true;
    } catch {
      mirrorRepoPickerRepos = [];
    }
    mirrorRepoPickerLoading = false;
  }

  async function submitAddMirrorRepo() {
    if (!addMirrorRepoModal || !newMirrorRepoKey) return;
    try {
      await bridge.addMirrorRepo(addMirrorRepoModal, newMirrorRepoKey, newMirrorRepoDirection, newMirrorRepoOrigin);
      $configStore = await bridge.reloadConfig();
      const mirrorKey = addMirrorRepoModal;
      const repoKey = newMirrorRepoKey;
      addMirrorRepoModal = null;
      newMirrorRepoKey = ''; newMirrorRepoDirection = 'push'; newMirrorRepoOrigin = 'src';
      if (newMirrorRepoAutoSetup) {
        newMirrorRepoAutoSetup = false;
        await setupMirrorRepo(mirrorKey, repoKey);
      }
    } catch (e: any) {
      alert(e?.message || e);
    }
  }

  async function confirmDeleteMirrorGroup() {
    if (!deleteMirrorGroupConfirm) return;
    try {
      await bridge.deleteMirrorGroup(deleteMirrorGroupConfirm);
      $configStore = await bridge.reloadConfig();
    } catch (e: any) {
      alert(e?.message || e);
    }
    deleteMirrorGroupConfirm = null;
    deleteMode = false;
  }

  async function confirmDeleteMirrorRepo() {
    if (!deleteMirrorRepoConfirm) return;
    try {
      await bridge.deleteMirrorRepo(deleteMirrorRepoConfirm.mirrorKey, deleteMirrorRepoConfirm.repoKey);
      $configStore = await bridge.reloadConfig();
    } catch (e: any) {
      alert(e?.message || e);
    }
    deleteMirrorRepoConfirm = null;
    deleteMode = false;
  }

  // ── Settings ──
  let showSettings = false;
  let configPath = '';
  let appVersion = '';
  let autostartOn = false;
  let autostartSupported = true;

  async function loadAutostart() {
    try {
      autostartOn = await bridge.getAutostart();
      autostartSupported = true;
    } catch {
      autostartSupported = false;
    }
  }

  async function toggleAutostart() {
    const want = !autostartOn;
    try {
      await bridge.setAutostart(want);
      autostartOn = want;
    } catch (e: any) {
      console.error('Failed to set autostart:', e);
    }
  }

  // ── Periodic status check ──
  let fetchInterval: string = 'off';
  let fetchTimerId: ReturnType<typeof setInterval> | null = null;
  let lastFetchTime: string = '';
  let fetchingAll = false;

  function applyFetchInterval(val: string) {
    fetchInterval = val;
    if (fetchTimerId) { clearInterval(fetchTimerId); fetchTimerId = null; }
    const minutes = val === '5m' ? 5 : val === '15m' ? 15 : val === '30m' ? 30 : 0;
    if (minutes > 0) {
      fetchTimerId = setInterval(() => { runFetchAll(); verifyAllCredentials(); checkAllMirrorStatus(); }, minutes * 60 * 1000);
    }
  }

  function setFetchInterval(val: string) {
    applyFetchInterval(val);
    bridge.setPeriodicSync(val);
  }

  async function runFetchAll() {
    fetchingAll = true;
    bridge.fetchAllRepos();
  }

  // ── Credential status cache ──
  let credStatuses: Record<string, any> = {};

  // ── Lifecycle ──
  async function initDashboard() {
    const cfg = await bridge.getConfig();
    configStore.set(cfg);
    configPath = await bridge.getConfigPath();
    appVersion = await bridge.getAppVersion();
    fetchInterval = await bridge.getPeriodicSync();

    // Restore view mode.
    const savedMode = await bridge.getViewMode();
    if (savedMode === 'compact') {
      viewMode = 'compact';
      await tick();
      // Fit to content, then show the window (it starts hidden in compact mode).
      setTimeout(async () => {
        await fitCompactHeight();
        bridge.showWindow();
      }, 300);
      // Second pass to catch font/style loading shifts.
      setTimeout(() => fitCompactHeight(), 800);
    }

    const statuses = await bridge.getAllStatus();
    applyStatusResults(statuses);

    // Fetch all repos on startup to detect behind/ahead changes.
    runFetchAll();

    // Verify credentials for each account
    verifyAllCredentials();

    // Check mirror sync status on startup.
    checkAllMirrorStatus();

    // Check for global ~/.gitconfig identity (warns user if set).
    checkGlobalIdentity();

    // Load autostart state.
    loadAutostart();

    // Start periodic status check if previously configured.
    if (fetchInterval !== 'off') applyFetchInterval(fetchInterval);
  }

  function verifyAllCredentials() {
    for (const key of Object.keys($accounts)) {
      bridge.credentialVerify(key).then((cs) => {
        credStatuses[key] = {status: cs.status, primary: cs.primary, pat: cs.pat};
        credStatuses = credStatuses;
      });
    }
  }

  onMount(async () => {
    applyTheme();
    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
      if (themeChoice === 'system') applyTheme();
    });

    // Re-read config when the window regains focus (picks up external edits).
    let focusTimer: ReturnType<typeof setTimeout> | null = null;
    window.addEventListener('focus', () => {
      if (focusTimer) clearTimeout(focusTimer);
      focusTimer = setTimeout(() => reloadFromDisk(), 300);
    });

    // ── Event listeners (always registered) ──
    events.on('status:updated', (results: any) => {
      applyStatusResults(results);
      // Refresh expanded detail if a repo is still open (reactive $: handles auto-collapse).
      if (expandedRepo && !detailLoading) {
        const slash = expandedRepo.indexOf('/');
        const sk = expandedRepo.substring(0, slash);
        const rn = expandedRepo.substring(slash + 1);
        bridge.getRepoDetail(sk, rn).then((raw) => {
          if (expandedRepo === `${sk}/${rn}`) {
            repoDetail = { ...raw, changed: raw.changed || [], untracked: raw.untracked || [] };
          }
        }).catch(() => {});
      }
    });

    events.on('clone:progress', (data: any) => {
      const key = `${data.source}/${data.repo}`;
      repoStates.update((s) => {
        if (s[key]) {
          s[key] = { ...s[key], progress: data.percent };
        }
        return { ...s };
      });
    });

    events.on('clone:done', (data: any) => {
      const key = `${data.source}/${data.repo}`;
      repoStates.update((s) => {
        if (s[key]) {
          s[key] = {
            ...s[key],
            status: data.error ? 'error' : 'clean',
            progress: 0,
            error: data.error,
          };
        }
        return { ...s };
      });
      checkSyncDone();
    });

    events.on('pull:done', (data: any) => {
      const key = `${data.source}/${data.repo}`;
      repoStates.update((s) => {
        if (s[key]) {
          s[key] = {
            ...s[key],
            status: data.error ? 'error' : 'clean',
            progress: 0,
            behind: 0,
            error: data.error,
          };
        }
        return { ...s };
      });
      checkSyncDone();
    });

    events.on('fetch:start', (data: any) => {
      const key = `${data.source}/${data.repo}`;
      fetchingRepos[key] = true;
      fetchingRepos = fetchingRepos;
    });

    events.on('fetch:done', (data: any) => {
      const key = `${data.source}/${data.repo}`;
      delete fetchingRepos[key];
      fetchingRepos = fetchingRepos;
    });

    events.on('fetch:alldone', () => {
      fetchingAll = false;
      fetchingRepos = {};
      const now = new Date();
      lastFetchTime = now.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
      verifyAllCredentials();
    });

    events.on('discover:done', (data: any) => {
      discoverLoading = false;
      if (data.error) {
        // If SSH account has no API token, close discover modal and show the token setup modal.
        const acctKey = data.accountKey || discoverModal;
        const acct = acctKey ? $accounts[acctKey] : null;
        if (acct?.default_credential_type === 'ssh' && String(data.error).includes('no API token')) {
          discoverModal = null;
          sshDiscoverTokenModal = acctKey;
          // Fetch the guide for this account.
          bridge.getTokenGuide(acctKey).then(guide => {
            sshDiscoveryGuide = guide.creationURL || '';
            sshDiscoveryScopes = guide.scopes || '';
          });
          return;
        }
        discoverRepos = [];
        return;
      }
      discoverRepos = (data.repos || []).sort((a: DiscoverResult, b: DiscoverResult) => a.fullName.localeCompare(b.fullName));
      discoverSelected = {};
    });

    events.on('mirror:status', (results: any) => {
      if (Array.isArray(results)) {
        applyMirrorStatusResults(results);
        // Clear checking flag for the mirror group.
        for (const r of results) {
          if (r.mirrorKey) mirrorChecking = { ...mirrorChecking, [r.mirrorKey]: false };
        }
      }
    });

    events.on('mirror:discover:progress', (data: any) => {
      mirrorDiscoverProgress = data;
    });

    events.on('mirror:discover:done', (data: any) => {
      mirrorDiscoverLoading = false;
      mirrorDiscoverProgress = null;
      if (data.error) {
        mirrorDiscoverError = data.error;
        mirrorDiscoverResults = [];
      } else {
        mirrorDiscoverResults = data.results || [];
      }
    });

    events.on('mirror:setup:done', async (data: any) => {
      mirrorSetupResultModal = data;
      $configStore = await bridge.reloadConfig();
      checkAllMirrorStatus();
    });

    // Check if config failed to parse (file exists but is broken/unsupported).
    const loadError = await bridge.getConfigLoadError();
    if (loadError) {
      const proceed = confirm(
        `Config file could not be loaded:\n\n${loadError}\n\nDo you want to reinitialize the configuration?\n\nClick OK to start fresh, or Cancel to exit and fix the file manually.`
      );
      if (!proceed) {
        window.close();
        return;
      }
    }

    // Check first run — show onboarding if no folder configured.
    firstRun = await bridge.isFirstRun();
    if (firstRun) return; // Don't load dashboard until onboarding completes.

    await initDashboard();
  });

  function checkSyncDone() {
    let states: Record<string, RepoState> = {};
    repoStates.subscribe((v) => (states = v))();
    const stillRunning = Object.values(states).some(
      (r) => r.status === 'syncing' || r.status === 'cloning'
    );
    if (!stillRunning) syncing = false;
  }

  // Reactive helpers that depend on theme
  $: sc = (s: string) => statusColor(s, $themeStore);
  $: cc = (s: string) => credColor(s, $themeStore);
</script>

<!-- ════════════════════════════════════════════════════════════ -->
<!--  TEMPLATE                                                   -->
<!-- ════════════════════════════════════════════════════════════ -->

<!-- ── ONBOARDING ── -->
{#if firstRun}
<div class="onboarding">
  <div class="onboard-card">
    <svg class="onboard-logo" viewBox="0 0 500 500" xmlns="http://www.w3.org/2000/svg">
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
    <h1 class="onboard-title">Welcome to gitbox</h1>
    <p class="onboard-desc">Choose where to store your local clones</p>
    {#if onboardError}
      <p class="form-error">{onboardError}</p>
    {/if}
    <div class="onboard-input-row">
      <input class="form-input onboard-input" bind:value={onboardFolder} placeholder="~/00.git" />
      <button class="settings-btn onboard-browse" on:click={() => browseFolder('onboard')}>Browse</button>
    </div>
    <button class="btn-add onboard-go" on:click={finishOnboarding}>Get started</button>
  </div>
</div>
{:else}

<div class="app">

  {#if viewMode === 'compact'}
  <!-- ══════ COMPACT STATUS VIEW ══════ -->
  {@const cpct = Math.round(($summary.clean / Math.max($summary.total, 1)) * 100)}
  {@const callGood = $summary.clean === $summary.total}
  <div class="compact-strip">
    <!-- Global health -->
    <div class="compact-global">
      <svg viewBox="0 0 36 36" class="compact-ring">
        <circle cx="18" cy="18" r="14" fill="none" stroke="var(--ring-bg)" stroke-width="3"/>
        <circle cx="18" cy="18" r="14" fill="none"
          stroke={callGood ? sc('clean') : sc('behind')}
          stroke-width="3" stroke-linecap="round"
          stroke-dasharray="{($summary.clean / Math.max($summary.total, 1)) * 87.96} 87.96"
          transform="rotate(-90 18 18)"/>
      </svg>
      <div class="compact-global-text">
        <span class="compact-global-pct" style="color: {callGood ? sc('clean') : sc('behind')}">{cpct}%</span>
        <span class="compact-global-label">{$summary.clean}/{$summary.total} synced</span>
      </div>
    </div>

    <div class="compact-sep"></div>

    <!-- Account pills -->
    {#each Object.entries($accounts) as [key, acct]}
      {@const stats = $accountStats[key] || { total: 0, synced: 0, issues: 0 }}
      {@const compactCred = (credStatuses[key] || {primary: 'unknown'}).primary}
      <button class="compact-acct" class:compact-acct-expanded={compactExpanded[key]}
        class:compact-acct-cred-err={compactCred === 'none' || compactCred === 'error'}
        class:compact-acct-cred-warn={compactCred === 'warning'}
        on:click={() => toggleCompactAcct(key)}>
        <svg viewBox="0 0 36 36" class="compact-acct-ring">
          <circle cx="18" cy="18" r="14" fill="none" stroke="var(--ring-bg)" stroke-width="3"/>
          <circle cx="18" cy="18" r="14" fill="none"
            stroke={stats.issues === 0 ? sc('clean') : sc('behind')}
            stroke-width="3" stroke-linecap="round"
            stroke-dasharray="{(stats.synced / Math.max(stats.total, 1)) * 87.96} 87.96"
            transform="rotate(-90 18 18)"/>
        </svg>
        <div class="compact-acct-info">
          <span class="compact-acct-name">{key}</span>
          <span class="compact-acct-stat">
            {#if stats.issues === 0}
              <span style="color:{sc('clean')}">All good</span>
            {:else}
              <span style="color:{sc('behind')}">{stats.issues} need attention</span>
            {/if}
          </span>
        </div>
        <span class="compact-chevron">{compactExpanded[key] ? '▾' : '▸'}</span>
      </button>

      {#if compactExpanded[key] && $sources[key]}
        <div class="compact-repo-list" transition:slide={{ duration: 120 }}>
          {#each ($sources[key].repoOrder || Object.keys($sources[key].repos)) as repoName}
            {@const repoKey = `${key}/${repoName}`}
            {@const state = $repoStates[repoKey] || { status: 'error', behind: 0, modified: 0, ahead: 0 }}
            <div class="compact-row" class:compact-row-ok={state.status === 'clean'}>
              <span class="compact-dot" style="color: {sc(state.status)}">{statusSymbol(state.status)}</span>
              <span class="compact-repo-name">{repoName.includes('/') ? repoName.split('/').pop() : repoName}</span>
              {#if state.status === 'behind'}
                <span class="compact-badge" style="color: {sc('behind')}">{state.behind} behind</span>
              {:else if state.status === 'dirty'}
                <span class="compact-badge" style="color: {sc('dirty')}">{state.modified} changed</span>
              {:else if state.status === 'ahead'}
                <span class="compact-badge" style="color: {sc('ahead')}">{state.ahead} ahead</span>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    {/each}

    <!-- Mirror summary (compact) -->
    {#if $mirrorSummary.total > 0}
    <div class="compact-sep"></div>
    <div class="compact-mirror-pill">
      <span class="compact-mirror-dot" style="color:{$mirrorSummary.error > 0 ? sc('error') : sc('clean')}">●</span>
      <span class="compact-mirror-label">Mirrors {$mirrorSummary.active}/{$mirrorSummary.total}</span>
    </div>
    {/if}

    <!-- Bottom actions -->
    <div class="compact-sep"></div>
    <div class="compact-actions">
      <button class="compact-action-btn" on:click={cycleTheme} title="Theme: {themeChoice}">{themeIcon(themeChoice)}</button>
      <button class="compact-action-btn compact-full-btn" on:click={toggleViewMode}>◧ Full view</button>
    </div>
  </div>

  {:else}
  <!-- ══════ FULL DASHBOARD VIEW ══════ -->

  <!-- ── TOP BAR ── -->
  <header class="topbar">
    <div class="brand">
      <svg class="logo-svg" viewBox="0 0 500 500" xmlns="http://www.w3.org/2000/svg">
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
      <span class="title">gitbox</span>
      <span class="tagline">accounts & clones</span>
    </div>
    <div class="health">
      <span class="health-ring" style="--pct: {$summary.total ? ($summary.clean / $summary.total) * 100 : 0}">
        <span class="health-num">{$summary.clean}/{$summary.total}</span>
      </span>
      <span class="health-label">synced</span>
    </div>
    {#if $mirrorSummary.total > 0}
    <div class="health">
      <span class="health-ring" style="--pct: {$mirrorSummary.total ? ($mirrorSummary.active / $mirrorSummary.total) * 100 : 0}{$mirrorSummary.error > 0 ? '; --ring-accent: #D81E5B' : ''}">
        <span class="health-num">{$mirrorSummary.active}/{$mirrorSummary.total}</span>
      </span>
      <span class="health-label">mirrors</span>
    </div>
    {/if}
    <div class="topbar-actions">
      <button class="btn-gear" on:click={syncAll}
        disabled={syncing || ($summary.behind === 0 && $summary.notCloned === 0)}
        title="{syncing ? 'Pulling...' : 'Pull All'}">
        <svg class="topbar-icon" class:spinning={syncing} viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round">
          <line x1="8" y1="2" x2="8" y2="12"/><polyline points="4.5,8.5 8,12 11.5,8.5"/><line x1="4" y1="14" x2="12" y2="14"/>
        </svg>
      </button>
      <button class="btn-gear" on:click={runFetchAll} disabled={fetchingAll}
        title="{fetchingAll ? 'Fetching...' : 'Fetch All'}">
        <span class="sync-icon" class:spinning={fetchingAll}>&#8635;</span>
      </button>
      <button class="btn-gear btn-trash" class:delete-active={deleteMode} on:click={() => deleteMode = !deleteMode} disabled={Object.keys($accounts).length === 0} title="{deleteMode ? 'Exit delete mode' : 'Delete mode'}">&#128465;</button>
      <button class="btn-gear" on:click={toggleViewMode} title="Compact view">◧</button>
      <button class="btn-gear" on:click={cycleTheme} title="Theme: {themeChoice}">{themeIcon(themeChoice)}</button>
      <button class="btn-gear" on:click={() => showSettings = !showSettings} title="Settings" class:active-gear={showSettings}>&#9881;</button>
    </div>
  </header>

  <!-- ── GLOBAL IDENTITY WARNING ── -->
  {#if globalIdentityWarn}
  <div class="identity-warn">
    <span class="identity-warn-icon">&#9888;</span>
    <span class="identity-warn-text">
      Global <code>~/.gitconfig</code> has
      {#if globalIdentityWarn.hasName}user.name="{globalIdentityWarn.name}"{/if}{#if globalIdentityWarn.hasName && globalIdentityWarn.hasEmail}, {/if}{#if globalIdentityWarn.hasEmail}user.email="{globalIdentityWarn.email}"{/if}
      &mdash; this can override per-repo identity.
    </span>
    <button class="identity-warn-fix" on:click={fixGlobalIdentity}>Remove</button>
    <button class="identity-warn-dismiss" on:click={() => globalIdentityWarn = null}>&#10005;</button>
  </div>
  {/if}

  <!-- ── SETTINGS PANEL ── -->
  {#if showSettings}
    <div class="settings" transition:slide={{ duration: 150 }}>
      <div class="settings-row">
        <span class="settings-label">Config</span>
        <span class="settings-value">{configPath}</span>
        <button class="settings-btn" on:click={() => bridge.openFileInEditor(configPath)}>Open in Editor</button>
      </div>
      <div class="settings-row">
        <span class="settings-label">Clone folder</span>
        <span class="settings-value">{$configStore?.global?.folder || '(not set)'}</span>
        <button class="settings-btn" on:click={openChangeFolder}>Change</button>
      </div>
      <div class="settings-row">
        <span class="settings-label">Theme</span>
        <div class="theme-toggle">
          <button class="theme-btn" class:theme-active={themeChoice === 'system'} on:click={() => { themeChoice = 'system'; applyTheme(); }}>System</button>
          <button class="theme-btn" class:theme-active={themeChoice === 'light'} on:click={() => { themeChoice = 'light'; applyTheme(); }}>Light</button>
          <button class="theme-btn" class:theme-active={themeChoice === 'dark'} on:click={() => { themeChoice = 'dark'; applyTheme(); }}>Dark</button>
        </div>
      </div>
      <div class="settings-row">
        <span class="settings-label">Periodic status check</span>
        <div class="theme-toggle">
          <button class="theme-btn" class:theme-active={fetchInterval === 'off'} on:click={() => setFetchInterval('off')}>Off</button>
          <button class="theme-btn" class:theme-active={fetchInterval === '5m'} on:click={() => setFetchInterval('5m')}>5m</button>
          <button class="theme-btn" class:theme-active={fetchInterval === '15m'} on:click={() => setFetchInterval('15m')}>15m</button>
          <button class="theme-btn" class:theme-active={fetchInterval === '30m'} on:click={() => setFetchInterval('30m')}>30m</button>
        </div>
        {#if lastFetchTime}
          <span class="settings-value" style="font-size:10px; margin-left:4px">last {lastFetchTime}</span>
        {/if}
      </div>
      {#if autostartSupported}
        <div class="settings-row">
          <span class="settings-label">Run at startup</span>
          <div class="theme-toggle">
            <button class="theme-btn" class:theme-active={!autostartOn} on:click={() => { if (autostartOn) toggleAutostart(); }}>Off</button>
            <button class="theme-btn" class:theme-active={autostartOn} on:click={() => { if (!autostartOn) toggleAutostart(); }}>On</button>
          </div>
        </div>
      {/if}
      <div class="settings-row">
        <span class="settings-label">Version</span>
        <span class="settings-value">{appVersion}</span>
      </div>
      <div class="settings-row">
        <span class="settings-label">Author</span>
        <span class="settings-value">Luis Palacios Derqui &mdash; <a href="https://github.com/LuisPalacios/gitbox" on:click|preventDefault={() => BrowserOpenURL('https://github.com/LuisPalacios/gitbox')}>github.com/LuisPalacios/gitbox</a></span>
      </div>
    </div>
  {/if}

  <!-- ── TAB BAR ── -->
  <div class="cards-tab-bar">
    <button class="cards-tab" class:cards-tab-active={cardsTab === 'accounts'}
      on:click={() => cardsTab = 'accounts'}>Accounts</button>
    <button class="cards-tab" class:cards-tab-active={cardsTab === 'mirrors'}
      on:click={() => cardsTab = 'mirrors'}>Mirrors</button>
    {#if cardsTab === 'mirrors'}
      <div class="tab-bar-actions">
        <button class="btn-tab-action" on:click={runMirrorDiscover} disabled={mirrorDiscoverLoading}>{mirrorDiscoverLoading ? 'Scanning...' : 'Discover'}</button>
        <button class="btn-tab-action" on:click={checkAllMirrorStatus}>Check all</button>
      </div>
    {/if}
  </div>

  {#if cardsTab === 'accounts'}
  <!-- ── ACCOUNT CARDS ── -->
  <section class="cards-row">
    {#each Object.entries($accounts) as [key, acct]}
      {@const stats = $accountStats[key] || { total: 0, synced: 0, issues: 0 }}
      {@const credObj = credStatuses[key] || {status: 'unknown', primary: 'unknown', pat: 'unknown'}}
      {@const credPrimary = credObj.primary}
      <div class="card" class:card-delete-mode={deleteMode}
        style={credPrimary === 'none' || credPrimary === 'error' ? `background: ${resolvedTheme === 'light' ? '#fef2f2' : '#2a1215'}` : ''}>
        <div class="card-top">
          {#if deleteMode}
            <button class="btn-delete-x card-delete-btn" on:click={() => askDeleteAccount(key)} title="Delete account {key}">&#10005;</button>
          {:else}
            <span class="card-dot" style="background: {cc(credPrimary)}"></span>
          {/if}
          <span class="card-provider">{providerLabel(acct.provider)}</span>
          <button class="cred-badge cred-badge-{credPrimary === 'ok' ? 'ok' : credPrimary === 'error' ? 'err' : credPrimary === 'warning' ? 'warn' : credPrimary === 'none' ? 'none' : credPrimary === 'unknown' ? 'pending' : ''}"
            on:click={() => openCredChange(key, acct.default_credential_type || 'gcm')}
            title="Credential: {acct.default_credential_type || 'none'}">{credPrimary === 'unknown' ? '···' : credPrimary === 'none' ? 'config' : acct.default_credential_type || 'gcm'}</button>
        </div>
        <div class="card-name card-name-edit" on:click={() => openEditAccount(key)} title="Edit account">{key}</div>
        <div class="card-ring-row">
          <svg class="mini-ring" viewBox="0 0 36 36">
            <circle cx="18" cy="18" r="15" fill="none" stroke="#27272a" stroke-width="3"/>
            <circle cx="18" cy="18" r="15" fill="none"
              stroke="{stats.issues === 0 ? sc('clean') : sc('behind')}"
              stroke-width="3" stroke-linecap="round"
              stroke-dasharray="{(stats.synced / Math.max(stats.total, 1)) * 94.2} 94.2"
              transform="rotate(-90 18 18)"/>
          </svg>
          <span class="card-stat">{stats.synced}/{stats.total}</span>
          {#if stats.issues > 0}
            <span class="card-issues" style="color: {sc('behind')}">{stats.issues} need{stats.issues > 1 ? '' : 's'} attention</span>
          {:else}
            <span class="card-ok" style="color: {sc('clean')}">All good</span>
          {/if}
        </div>
        <div class="card-btn-row">
          <button class="card-btn" on:click={() => openDiscover(key)} disabled={credPrimary === 'none' || credPrimary === 'error'}>Find projects</button>
          <button class="card-btn" on:click={() => openCreateRepo(key)} disabled={credPrimary === 'none' || credPrimary === 'error'}>Create repo</button>
        </div>
      </div>
    {/each}
    <button class="card card-add" on:click={() => addAccountModal = true} title="Add account">
      <span class="card-add-icon">+</span>
    </button>
  </section>

  <!-- ── REPO LIST ── -->
  <section class="repo-list">
    {#each Object.entries($sources) as [sourceKey, source] (sourceKey)}
      <div class="source-group">
        <div class="source-header">{sourceKey}</div>
        {#each (source.repoOrder && source.repoOrder.length > 0 ? source.repoOrder : Object.keys(source.repos)) as repoName (repoName)}
          {@const repoKey = `${sourceKey}/${repoName}`}
          {@const state = $repoStates[repoKey] || { status: 'error', progress: 0, behind: 0, modified: 0, untracked: 0, ahead: 0 }}
          <div class="repo-row" class:repo-row-clickable={state.status !== 'clean' && state.status !== 'behind' && state.status !== 'not cloned' && state.status !== 'cloning' && state.status !== 'syncing'}
            on:click={() => toggleRepoDetail(sourceKey, repoName, state.status)}>
            {#if deleteMode}
              <button class="btn-delete-x" on:click|stopPropagation={() => askDelete(sourceKey, repoName, state.status)} title="Delete {repoName}">&#10005;</button>
            {/if}
            <span class="dot" style="color: {sc(state.status)}">{statusSymbol(state.status)}</span>
            <span class="repo-name">{repoName}</span>

            {#if state.status === 'syncing' || state.status === 'cloning'}
              <div class="progress-track">
                <div class="progress-fill" style="width:{state.progress}%; background:{sc(state.status)}"></div>
              </div>
              <span class="progress-pct" style="color:{sc(state.status)}">{state.progress}%</span>
            {:else}
              <span class="status-badges">
                {#if state.status === 'clean'}
                  <span class="status-text" style="color:{sc('clean')}">Synced</span>
                {:else if state.status === 'not cloned'}
                  <span class="status-text" style="color:{sc('not cloned')}">Not local</span>
                {:else if state.status === 'no upstream'}
                  <span class="status-text" style="color:{sc('no upstream')}">No upstream</span>
                {:else if state.status === 'error'}
                  <span class="status-text" style="color:{sc('error')}">Error</span>
                {:else}
                  <span class="status-pending">Pending</span>
                  {#if state.behind > 0}<span class="sbadge" style="color:{sc('behind')}" title="{state.behind} behind">↓{state.behind}</span>{/if}
                  {#if state.ahead > 0}<span class="sbadge" style="color:{sc('ahead')}" title="{state.ahead} ahead">↑{state.ahead}</span>{/if}
                  {#if state.modified > 0}<span class="sbadge" style="color:{sc('dirty')}" title="{state.modified} changed">✎{state.modified}</span>{/if}
                  {#if state.untracked > 0}<span class="sbadge" style="color:{sc('not cloned')}" title="{state.untracked} untracked">?{state.untracked}</span>{/if}
                {/if}
              </span>
              {#if state.status === 'behind'}
                <button class="btn-action" on:click|stopPropagation={() => syncRepo(sourceKey, repoName)}>Pull</button>
              {:else if state.status === 'not cloned'}
                <button class="btn-action" on:click|stopPropagation={() => cloneRepo(sourceKey, repoName)}>Bring Local</button>
              {/if}
              {#if !deleteMode && state.status !== 'not cloned'}
                <button class="btn-fetch" class:spinning={fetchingRepos[repoKey]} on:click|stopPropagation={() => fetchRepo(sourceKey, repoName)} title="Fetch origin" disabled={!!fetchingRepos[repoKey]}>&#8635;</button>
              {/if}
            {/if}
          </div>
          {#if expandedRepo === repoKey}
            <div class="repo-detail" transition:slide={{ duration: 150 }}>
              {#if detailLoading}
                <span class="detail-loading">Loading...</span>
              {:else if repoDetail?.error}
                <span class="detail-error">{repoDetail.error}</span>
              {:else if repoDetail}
                <div class="detail-header">
                  <span class="detail-branch">Branch: <strong>{repoDetail.branch}</strong></span>
                  {#if repoDetail.ahead > 0}
                    <span class="detail-badge" style="color:{sc('ahead')}">{repoDetail.ahead} ahead</span>
                  {/if}
                  {#if repoDetail.behind > 0}
                    <span class="detail-badge" style="color:{sc('behind')}">{repoDetail.behind} behind</span>
                  {/if}
                </div>
                {#if repoDetail.changed.length > 0}
                  <div class="detail-section-title">Changed files <span class="sbadge" style="color:{sc('dirty')}">✎{repoDetail.changed.length}</span></div>
                  {#each repoDetail.changed as file}
                    <div class="detail-file">
                      <span class="detail-kind" class:kind-added={file.kind === 'added'} class:kind-deleted={file.kind === 'deleted'} class:kind-conflict={file.kind === 'conflict'} title={kindLabel(file.kind)}>{kindIcon(file.kind)}</span>
                      <span class="detail-path">{file.path}</span>
                    </div>
                  {/each}
                {/if}
                {#if repoDetail.untracked.length > 0}
                  <div class="detail-section-title">New files (untracked) <span class="sbadge" style="color:{sc('not cloned')}">?{repoDetail.untracked.length}</span></div>
                  {#each repoDetail.untracked as file}
                    <div class="detail-file">
                      <span class="detail-kind kind-untracked" title="Untracked">?</span>
                      <span class="detail-path detail-path-dim">{file}</span>
                    </div>
                  {/each}
                {/if}
                {#if repoDetail.changed.length === 0 && repoDetail.untracked.length === 0 && repoDetail.ahead === 0 && repoDetail.behind === 0}
                  <span class="detail-clean">Everything is up to date.</span>
                {/if}
              {/if}
            </div>
          {/if}
        {/each}
      </div>
    {/each}

  </section>

  {:else}
  <!-- ── MIRROR CARDS ── -->
  {#if mirrorDiscoverLoading && mirrorDiscoverProgress}
  <div class="discover-progress">
    <div class="discover-progress-text">
      {#if mirrorDiscoverProgress.phase === 'listing'}
        Listing repos on <strong>{mirrorDiscoverProgress.account}</strong>...
      {:else}
        Analyzing <strong>{mirrorDiscoverProgress.account}</strong>: {mirrorDiscoverProgress.current}/{mirrorDiscoverProgress.total} repos
      {/if}
    </div>
    {#if mirrorDiscoverProgress.phase === 'analyzing' && mirrorDiscoverProgress.total > 0}
      <div class="discover-progress-bar">
        <div class="discover-progress-fill" style="width: {(mirrorDiscoverProgress.current / mirrorDiscoverProgress.total) * 100}%"></div>
      </div>
    {:else}
      <div class="discover-progress-bar">
        <div class="discover-progress-fill discover-progress-indeterminate"></div>
      </div>
    {/if}
  </div>
  {/if}
  <section class="cards-row">
    {#each Object.entries($mirrors) as [mirrorKey, mir]}
      {@const mstats = mirrorGroupStats[mirrorKey] || { active: 0, unchecked: 0, error: 0, total: 0 }}
      <div class="card card-mirror" class:card-delete-mode={deleteMode}>
        <div class="card-top">
          {#if deleteMode}
            <button class="btn-delete-x card-delete-btn" on:click={() => deleteMirrorGroupConfirm = mirrorKey} title="Delete mirror group {mirrorKey}">&#10005;</button>
          {:else}
            <span class="card-dot" style="background: {mstats.error > 0 ? sc('error') : mstats.active === mstats.total && mstats.total > 0 ? sc('clean') : sc('behind')}"></span>
          {/if}
          <span class="card-provider">MIRROR</span>
        </div>
        <div class="card-name card-mirror-name">{mir.account_src} ↔ {mir.account_dst}</div>
        <div class="card-ring-row">
          <svg class="mini-ring" viewBox="0 0 36 36">
            <circle cx="18" cy="18" r="15" fill="none" stroke="#27272a" stroke-width="3"/>
            <circle cx="18" cy="18" r="15" fill="none"
              stroke="{mstats.error > 0 ? sc('error') : sc('clean')}"
              stroke-width="3" stroke-linecap="round"
              stroke-dasharray="{(mstats.active / Math.max(mstats.total, 1)) * 94.2} 94.2"
              transform="rotate(-90 18 18)"/>
          </svg>
          <span class="card-stat">{mstats.active}/{mstats.total}</span>
          {#if mstats.error > 0}
            <span class="card-issues" style="color: {sc('error')}">{mstats.error} error{mstats.error > 1 ? 's' : ''}</span>
          {:else if mstats.unchecked > 0}
            <span class="card-issues" style="color: {sc('behind')}">{mstats.unchecked} unchecked</span>
          {:else if mstats.total > 0}
            <span class="card-ok" style="color: {sc('clean')}">All synced</span>
          {:else}
            <span class="card-ok" style="color: {sc('behind')}">No clones</span>
          {/if}
        </div>
        <button class="card-btn" on:click={() => checkMirrorStatus(mirrorKey)}>Check status</button>
      </div>
    {/each}
    <button class="card card-add" on:click={() => { addMirrorGroupModal = true; newMirrorKey = ''; newMirrorSrc = ''; newMirrorDst = ''; }} title="Add mirror group">
      <span class="card-add-icon">+</span>
    </button>
  </section>

  <!-- ── MIRROR DETAIL LIST ── -->
  {#if Object.keys($mirrors).length > 0}
  <section class="repo-list">
    <div class="mirror-list">
    <div class="mirror-section-header">
      <h3>Mirrors</h3>
    </div>

    {#each Object.entries($mirrors) as [mirrorKey, mir]}
      <div class="mirror-group">
        <div class="mirror-group-header">
          <span class="mirror-accounts">{mir.account_src} <span class="mirror-arrow">↔</span> {mir.account_dst}</span>
          <div class="mirror-group-actions">
            {#if mirrorChecking[mirrorKey]}
              <span class="mirror-checking"><div class="spinner-sm"></div></span>
            {/if}
            <button class="btn-tab-action" on:click={() => { addMirrorRepoModal = mirrorKey; newMirrorRepoKey = ''; newMirrorRepoDirection = 'push'; newMirrorRepoOrigin = 'src'; newMirrorRepoAutoSetup = false; mirrorRepoPickerRepos = []; mirrorRepoPickerLoaded = false; mirrorRepoPickerFilter = ''; }}>+ Repo</button>
            {#if deleteMode}
              <button class="btn-sm btn-danger" on:click={() => deleteMirrorGroupConfirm = mirrorKey}>✕</button>
            {/if}
          </div>
        </div>

        {#each (mir.repoOrder && mir.repoOrder.length > 0 ? mir.repoOrder : Object.keys(mir.repos)) as repoName}
          {@const repo = mir.repos[repoName]}
          {@const live = $mirrorStates[`${mirrorKey}/${repoName}`]}
          <div class="mirror-row">
            <span class="mirror-dot" style="color:{mirrorStatusColor(repo, live, $themeStore)}">{mirrorStatusSymbol(repo, live)}</span>
            <span class="mirror-repo-name">{repoName}</span>
            <span class="mirror-direction">{@html mirrorDirLabelHtml(repo, mir)}</span>
            <span class="mirror-status-text" style="color:{mirrorStatusColor(repo, live, $themeStore)}">{mirrorStatusText(repo, live)}</span>
            {#if live?.warning}
              <span class="mirror-warning" title={live.warning}>⚠</span>
            {/if}
            {#if live?.error && live.error.startsWith('missing API token in ') && !deleteMode}
              {@const errAcct = live.error.replace('missing API token in ', '')}
              <button class="btn-sm btn-fix" on:click={() => openTokenSetup(errAcct)} title="Set up API token for {errAcct}">Fix credentials</button>
            {:else if !deleteMode}
              <button class="btn-sm btn-setup" on:click={() => setupMirrorRepo(mirrorKey, repoName)}>Setup</button>
            {/if}
            {#if deleteMode}
              <button class="btn-sm btn-danger" on:click={() => deleteMirrorRepoConfirm = { mirrorKey, repoKey: repoName }}>✕</button>
            {/if}
          </div>
        {/each}

        {#if Object.keys(mir.repos).length === 0}
          <div class="mirror-empty">No repos in this mirror group.</div>
        {/if}
      </div>
    {/each}
    </div>
  </section>
  {/if}
  {/if}

  <!-- ── SUMMARY FOOTER ── -->
  <footer class="summary">
    <span class="sum" style="color:{sc('clean')}">{$summary.clean} synced</span>
    {#if $summary.syncing > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('syncing')}">{$summary.syncing} syncing</span>{/if}
    {#if $summary.behind > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('behind')}">{$summary.behind} behind</span>{/if}
    {#if $summary.dirty > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('dirty')}">{$summary.dirty} local changes</span>{/if}
    {#if $summary.notCloned > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('not cloned')}">{$summary.notCloned} not local</span>{/if}
    {#if $mirrorSummary.total > 0}<span class="sep">&middot;</span><span class="sum" style="color:{$mirrorSummary.error > 0 ? sc('error') : sc('clean')}">{$mirrorSummary.active}/{$mirrorSummary.total} mirrors</span>{/if}
  </footer>

  {/if}
  <!-- end viewMode -->

  <!-- ── DISCOVER MODAL ── -->
  {#if discoverModal}
    <div class="overlay" on:click={() => discoverModal = null} transition:fade={{ duration: 120 }}>
      <div class="modal modal-discover" on:click|stopPropagation transition:slide={{ duration: 180 }}>
        <div class="modal-head">
          <h3>Find projects &mdash; {discoverModal}</h3>
          <button class="btn-x" on:click={() => discoverModal = null}>&#10005;</button>
        </div>
        <div class="modal-body">
          {#if discoverLoading}
            <div class="loading"><div class="spinner"></div><span>Checking your account...</span></div>
          {:else if discoverRepos.length === 0}
            <p class="found">No new projects found.</p>
          {:else}
            <p class="found">Found {discoverRepos.length} projects{discoverFilter ? ` (showing ${filteredDiscoverRepos.length})` : ''}:</p>
            <input class="form-input discover-filter" type="text" placeholder="Filter projects..." bind:value={discoverFilter} />
            <label class="dr dr-all">
              <input type="checkbox" checked={allSelected} on:change={toggleAll} />
              <span class="dr-name">Select all{discoverFilter ? ' (filtered)' : ''}</span>
            </label>
            {#each filteredDiscoverRepos as repo}
              {@const alreadyAdded = !!($sources[discoverModal]?.repos?.[repo.fullName])}
              <label class="dr" class:dr-disabled={alreadyAdded}>
                <input type="checkbox" bind:checked={discoverSelected[repo.fullName]} disabled={alreadyAdded} />
                <span class="dr-name">{repo.fullName}</span>
                {#if alreadyAdded}<span class="dr-tag">added</span>{/if}
                {#if repo.archived}<span class="dr-tag">archived</span>{/if}
                {#if repo.fork}<span class="dr-tag">fork</span>{/if}
                <span class="dr-desc">{repo.description}</span>
              </label>
            {/each}
          {/if}
        </div>
        {#if !discoverLoading && discoverRepos.length > 0}
          <div class="modal-foot">
            <button class="btn-cancel" on:click={() => discoverModal = null}>Cancel</button>
            <button class="btn-add" on:click={addDiscovered} disabled={selectedCount === 0}>Add &amp; Pull ({selectedCount})</button>
          </div>
        {/if}
      </div>
    </div>
  {/if}

  <!-- ── SSH DISCOVERY TOKEN MODAL ── -->
  {#if sshDiscoverTokenModal}
    <div class="overlay" transition:fade={{ duration: 120 }}>
      <div class="modal modal-account" on:click|stopPropagation transition:slide={{ duration: 180 }}>
        <div class="modal-head">
          <h3>Discovery token &mdash; {sshDiscoverTokenModal}</h3>
          <button class="btn-x" on:click={() => { sshDiscoverTokenModal = null; sshDiscoveryTokenInput = ''; }}>&#10005;</button>
        </div>
        <div class="modal-body">
          <p class="cred-step-desc">SSH credentials cannot access provider APIs.<br/>To use 'Find projects', store a Personal Access Token for API access.</p>
          <p class="cred-step-desc">Otherwise you can configure your sources manually in the JSON config file.</p>
          {#if sshDiscoveryScopes}
            <p class="cred-step-desc" style="margin-top: 6px;"><strong>Required scopes:</strong> {sshDiscoveryScopes}</p>
          {/if}
          {#if sshDiscoveryGuide}
            <p class="cred-step-link" style="margin-top: 4px;"><a href={sshDiscoveryGuide} on:click|preventDefault={() => BrowserOpenURL(sshDiscoveryGuide)}>{sshDiscoveryGuide}</a></p>
          {/if}
          <div class="form-row" style="margin-top: 10px;">
            <label class="form-label" for="ssh-disc-token">Token</label>
            <input class="form-input" id="ssh-disc-token" type="password" bind:value={sshDiscoveryTokenInput} placeholder="Token..." />
          </div>
        </div>
        <div class="modal-foot">
          <button class="btn-cancel" on:click={() => { sshDiscoverTokenModal = null; sshDiscoveryTokenInput = ''; }}>Cancel</button>
          <button class="btn-add" on:click={storeSSHDiscoveryToken} disabled={sshDiscoveryBusy || !sshDiscoveryTokenInput.trim()}>
            {sshDiscoveryBusy ? 'Storing...' : 'Store & discover'}
          </button>
        </div>
      </div>
    </div>
  {/if}

  <!-- ── EDIT ACCOUNT MODAL ── -->
  {#if editAccountModal}
    <div class="overlay" on:click={() => editAccountModal = null} transition:fade={{ duration: 120 }}>
      <div class="modal modal-account" on:click|stopPropagation transition:slide={{ duration: 180 }}>
        <div class="modal-head">
          <h3>Edit account</h3>
          <button class="btn-x" on:click={() => editAccountModal = null}>&#10005;</button>
        </div>
        <div class="modal-body">
          {#if editAccountError}
            <p class="form-error">{editAccountError}</p>
          {/if}
          <div class="form-row">
            <label class="form-label" for="ea-key">Account key</label>
            <input class="form-input" id="ea-key" bind:value={editAcct.key} placeholder="account-key" />
          </div>
          <div class="form-row">
            <label class="form-label" for="ea-provider">Provider</label>
            <select class="form-input" id="ea-provider" bind:value={editAcct.provider}>
              <option value="github">GitHub</option>
              <option value="gitlab">GitLab</option>
              <option value="gitea">Gitea</option>
              <option value="forgejo">Forgejo</option>
              <option value="bitbucket">Bitbucket</option>
            </select>
          </div>
          <div class="form-row">
            <label class="form-label" for="ea-url">URL</label>
            <input class="form-input" id="ea-url" bind:value={editAcct.url} />
          </div>
          <div class="form-row">
            <label class="form-label" for="ea-user">Username</label>
            <input class="form-input" id="ea-user" bind:value={editAcct.username} />
          </div>
          <div class="form-row">
            <label class="form-label" for="ea-name">Name</label>
            <input class="form-input" id="ea-name" bind:value={editAcct.name} />
          </div>
          <div class="form-row">
            <label class="form-label" for="ea-email">Email</label>
            <input class="form-input" id="ea-email" bind:value={editAcct.email} />
          </div>
        </div>
        <div class="modal-foot">
          <button class="btn-cancel" on:click={() => editAccountModal = null}>Cancel</button>
          <button class="btn-add" on:click={submitEditAccount}>Save</button>
        </div>
      </div>
    </div>
  {/if}

  <!-- ── DELETE CONFIRMATION MODAL ── -->
  {#if deleteConfirm}
    <div class="overlay" on:click={cancelDelete} transition:fade={{ duration: 120 }}>
      <div class="modal modal-delete" on:click|stopPropagation transition:slide={{ duration: 180 }}>
        <div class="modal-head modal-head-delete">
          <h3>Delete local clone</h3>
          <button class="btn-x" on:click={cancelDelete}>&#10005;</button>
        </div>
        <div class="modal-body">
          {#if deleteConfirmStep === 1}
            <p class="delete-repo-name">{deleteConfirm.repoKey}</p>
            <p class="delete-warning" class:delete-danger={isDangerous(deleteConfirm.status)}>
              {deleteWarning(deleteConfirm.status)}
            </p>
          {:else}
            <p class="delete-repo-name">{deleteConfirm.repoKey}</p>
            <p class="delete-final">This action <strong>cannot be undone</strong>. Are you absolutely sure?</p>
          {/if}
        </div>
        <div class="modal-foot">
          <button class="btn-cancel" on:click={cancelDelete}>Cancel</button>
          {#if deleteConfirmStep === 1}
            <button class="btn-delete" on:click={confirmDelete}>Delete</button>
          {:else}
            <button class="btn-delete btn-delete-final" on:click={confirmDelete} disabled={deleting}>
              {deleting ? 'Deleting...' : 'Yes, delete permanently'}
            </button>
          {/if}
        </div>
      </div>
    </div>
  {/if}

  <!-- ── ADD ACCOUNT MODAL ── -->
  {#if addAccountModal}
    <div class="overlay" on:click={() => { if (addAccountStep === 'form') resetAddAccount(); }} transition:fade={{ duration: 120 }}>
      <div class="modal modal-account" on:click|stopPropagation transition:slide={{ duration: 180 }}>
        <div class="modal-head">
          <h3>{addAccountStep === 'form' ? 'Add account' : 'Credential setup'}</h3>
          <button class="btn-x" on:click={resetAddAccount}>&#10005;</button>
        </div>
        <div class="modal-body">
          {#if addAccountStep === 'form'}
            <!-- ── Step 1: Account details ── -->
            {#if addAccountError}
              <p class="form-error">{addAccountError}</p>
            {/if}
            <div class="form-row">
              <label class="form-label" for="aa-key">Account key</label>
              <input class="form-input" id="aa-key" bind:value={addAcct.key} placeholder="github-MyUser" />
            </div>
            <div class="form-row">
              <label class="form-label" for="aa-provider">Provider</label>
              <select class="form-input" id="aa-provider" bind:value={addAcct.provider} on:change={onProviderChange}>
                <option value="github">GitHub</option>
                <option value="gitlab">GitLab</option>
                <option value="gitea">Gitea</option>
                <option value="forgejo">Forgejo</option>
                <option value="bitbucket">Bitbucket</option>
              </select>
            </div>
            <div class="form-row">
              <label class="form-label" for="aa-url">URL</label>
              <input class="form-input" id="aa-url" bind:value={addAcct.url} placeholder="https://github.com" />
            </div>
            <div class="form-row">
              <label class="form-label" for="aa-user">Username</label>
              <input class="form-input" id="aa-user" bind:value={addAcct.username} placeholder="MyUser" />
            </div>
            <div class="form-row">
              <label class="form-label" for="aa-name">Name</label>
              <input class="form-input" id="aa-name" bind:value={addAcct.name} placeholder="Full Name" />
            </div>
            <div class="form-row">
              <label class="form-label" for="aa-email">Email</label>
              <input class="form-input" id="aa-email" bind:value={addAcct.email} placeholder="user@example.com" />
            </div>
            <div class="form-row">
              <label class="form-label" for="aa-cred">Credential type</label>
              <select class="form-input" id="aa-cred" bind:value={addAcct.credentialType}>
                <option value="gcm">GCM</option>
                <option value="ssh">SSH</option>
                <option value="token">Token</option>
              </select>
            </div>
          {:else}
            <!-- ── Step 2: Credential setup ── -->
            <p class="cred-step-intro">Account <strong>{addAcct.key}</strong> created. Set up credentials:</p>

            {#if addAcct.credentialType === 'gcm'}
              <p class="cred-step-desc">Click below to authenticate via browser (Git Credential Manager).</p>
              <button class="btn-add cred-action-btn" on:click={() => runCredSetup(addAcct.key, addAcct.credentialType)} disabled={credBusy || credResult?.ok}>
                {credBusy ? 'Authenticating...' : credResult?.ok ? 'Authenticated' : 'Authenticate with GCM'}
              </button>

            {:else if addAcct.credentialType === 'token'}
              <p class="cred-step-desc">Click on the link and login if necessary. Create a Token and paste it below.</p>
              {#if credTokenScopes}
                <p class="cred-step-desc"><strong>Required scopes:</strong> {credTokenScopes}</p>
              {/if}
              {#if credTokenGuide}
                <p class="cred-step-link"><a href={credTokenGuide} on:click|preventDefault={() => BrowserOpenURL(credTokenGuide)}>{credTokenGuide}</a></p>
              {/if}
              <div class="form-row">
                <label class="form-label" for="aa-token">Token</label>
                <input class="form-input" id="aa-token" type="password" bind:value={credTokenInput} placeholder="ghp_..." />
              </div>
              <button class="btn-add cred-action-btn" on:click={() => runCredSetup(addAcct.key, addAcct.credentialType)} disabled={credBusy || !credTokenInput.trim() || credResult?.ok}>
                {credBusy ? 'Storing...' : credResult?.ok ? 'Token stored' : 'Store token'}
              </button>

            {:else if addAcct.credentialType === 'ssh'}
              <p class="cred-step-desc">SSH Host and Key configuration.</p>
            {/if}

            {#if credResult}
              {#if credResult.sshPublicKey}
                <!-- SSH-specific structured result -->
                <div class="cred-result cred-result-ok">
                  <pre class="cred-result-msg">{credResult.message}</pre>
                </div>
                <div class="ssh-connection-status" class:ssh-connected={credResult.sshVerified} class:ssh-disconnected={!credResult.sshVerified}>
                  <span class="ssh-status-dot"></span>
                  <span>{credResult.sshVerified ? 'Connection verified' : 'Connection not verified'}</span>
                </div>
                {#if !credResult.sshVerified}
                  <p class="cred-step-desc" style="margin-top: 8px;">Copy the public SSH key and add it to your provider:</p>
                  <div class="ssh-key-box">
                    <code class="ssh-key-text">{credResult.sshPublicKey}</code>
                    <button class="ssh-key-copy" on:click={() => copySSHKey(credResult.sshPublicKey)} title="Copy to clipboard">{@html sshKeyCopied ? '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="20 6 9 17 4 12"/></svg>' : '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>'}</button>
                  </div>
                  {#if credResult.sshAddURL}
                    <p class="cred-step-link"><a href={credResult.sshAddURL} on:click|preventDefault={() => BrowserOpenURL(credResult.sshAddURL)}>{credResult.sshAddURL}</a></p>
                  {/if}
                {/if}
                <div style="display: flex; gap: 8px; margin-top: 8px;">
                  {#if !credResult.sshVerified}
                    <button class="btn-add cred-action-btn" on:click={verifySSHConnection} disabled={credBusy}>
                      {credBusy ? 'Verifying...' : 'Verify connection'}
                    </button>
                  {/if}
                  <button class="btn-cancel cred-action-btn" on:click={regenerateSSHKey} disabled={credBusy}>
                    {credBusy ? 'Regenerating...' : 'Regenerate SSH key'}
                  </button>
                </div>
              {:else}
                <!-- Generic result (GCM, Token) -->
                <div class="cred-result" class:cred-result-ok={credResult.ok} class:cred-result-err={!credResult.ok}>
                  <pre class="cred-result-msg">{credResult.message}</pre>
                </div>
              {/if}
            {:else if addAcct.credentialType === 'ssh'}
              <button class="btn-add cred-action-btn" on:click={() => runCredSetup(addAcct.key, addAcct.credentialType)} disabled={credBusy}>
                {credBusy ? 'Setting up SSH...' : 'Generate SSH key'}
              </button>
            {/if}

          {/if}
        </div>
        <div class="modal-foot">
          {#if addAccountStep === 'form'}
            <button class="btn-cancel" on:click={resetAddAccount}>Cancel</button>
            <button class="btn-add" on:click={submitAddAccount}>Add account</button>
          {:else}
            <button class="btn-cancel" on:click={resetAddAccount}>
              {credResult?.ok && (!credResult?.sshPublicKey || credResult?.sshVerified) ? 'Done' : 'Cancel'}
            </button>
          {/if}
        </div>
      </div>
    </div>
  {/if}

  <!-- ── CHANGE CREDENTIAL MODAL ── -->
  {#if credChangeModal}
    {@const currentAcct = $accounts[credChangeModal]}
    <div class="overlay" on:click={() => { if (!credBusy && !credDeleteBusy) closeCredChange(); }} transition:fade={{ duration: 120 }}>
      <div class="modal modal-account" on:click|stopPropagation transition:slide={{ duration: 180 }}>
        <div class="modal-head">
          <h3>{credForceToken ? 'API token' : 'Change credential'} &mdash; {credChangeModal}</h3>
          <button class="btn-x" on:click={closeCredChange}>&#10005;</button>
        </div>
        <div class="modal-body">
          {#if credForceToken}
          <!-- Simplified PAT-only flow for mirror fix -->
          <p class="cred-step-desc">Mirrors require an API token (PAT) for status checks. Primary credential: <strong>{currentAcct?.default_credential_type || 'none'}</strong>.</p>
          {:else}
          <p class="cred-doc-link"><a href="https://github.com/LuisPalacios/gitbox/blob/main/docs/credentials.md" on:click|preventDefault={() => BrowserOpenURL('https://github.com/LuisPalacios/gitbox/blob/main/docs/credentials.md')}>Learn about credential types</a></p>
          <div class="form-row">
            <label class="form-label" for="cc-type">Type</label>
            <select class="form-input" id="cc-type" bind:value={credChangeType} disabled={credBusy}>
              <option value="gcm">Git Credential Manager (GCM)</option>
              <option value="ssh">SSH</option>
              <option value="token">Token</option>
            </select>
          </div>

          {#if !credResult && credChangeType !== (currentAcct?.default_credential_type || '') && currentAcct?.default_credential_type}
            <p class="cred-change-warning">Setting up {credChangeType.toUpperCase()} will remove the current {(currentAcct?.default_credential_type || '').toUpperCase()} credential and its artifacts{currentAcct?.default_credential_type === 'ssh' ? ' (SSH key, ~/.ssh/config entry)' : currentAcct?.default_credential_type === 'token' ? ' (token from OS keyring)' : currentAcct?.default_credential_type === 'gcm' ? ' (cached GCM credential)' : ''}.</p>
          {/if}
          {/if}

          {#if (credForceToken || credChangeType === 'token') && credTokenGuide && !credResult}
            <p class="cred-step-desc">Click on the link and login if necessary. Create a Token and paste it below.</p>
            {#if credTokenScopes}
              <p class="cred-step-desc"><strong>Required scopes:</strong> {credTokenScopes}</p>
            {/if}
            <p class="cred-step-link"><a href={credTokenGuide} on:click|preventDefault={() => BrowserOpenURL(credTokenGuide)}>{credTokenGuide}</a></p>
            <div class="form-row">
              <label class="form-label" for="cc-token">Token</label>
              <input class="form-input" id="cc-token" type="password" bind:value={credTokenInput} placeholder="ghp_..." />
            </div>
          {/if}

          {#if credBusy}
            <div class="loading"><div class="spinner"></div><span>Setting up credential...</span></div>
          {/if}

          {#if credResult}
            {#if credResult.sshPublicKey}
              <div class="cred-result cred-result-ok">
                <pre class="cred-result-msg">{credResult.message}</pre>
              </div>
              <div class="ssh-connection-status" class:ssh-connected={credResult.sshVerified} class:ssh-disconnected={!credResult.sshVerified}>
                <span class="ssh-status-dot"></span>
                <span>{credResult.sshVerified ? 'Connection verified' : 'Connection not verified'}</span>
              </div>
              {#if !credResult.sshVerified}
                <p class="cred-step-desc" style="margin-top: 8px;">Copy the public SSH key and add it to your provider:</p>
                <div class="ssh-key-box">
                  <code class="ssh-key-text">{credResult.sshPublicKey}</code>
                  <button class="ssh-key-copy" on:click={() => copySSHKey(credResult.sshPublicKey)} title="Copy to clipboard">{@html sshKeyCopied ? '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="20 6 9 17 4 12"/></svg>' : '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>'}</button>
                </div>
                {#if credResult.sshAddURL}
                  <p class="cred-step-link"><a href={credResult.sshAddURL} on:click|preventDefault={() => BrowserOpenURL(credResult.sshAddURL)}>{credResult.sshAddURL}</a></p>
                {/if}
              {/if}
              <div style="display: flex; gap: 8px; margin-top: 8px;">
                {#if !credResult.sshVerified}
                  <button class="btn-add cred-action-btn" on:click={verifySSHConnection} disabled={credBusy}>
                    {credBusy ? 'Verifying...' : 'Verify connection'}
                  </button>
                {/if}
                <button class="btn-cancel cred-action-btn" on:click={regenerateSSHKey} disabled={credBusy}>
                  {credBusy ? 'Regenerating...' : 'Regenerate SSH key'}
                </button>
              </div>
            {:else}
              <div class="cred-result" class:cred-result-ok={credResult.ok} class:cred-result-err={!credResult.ok}>
                <pre class="cred-result-msg">{credResult.message}</pre>
              </div>
            {/if}
          {/if}

        </div>
        <div class="modal-foot">
          {#if currentAcct?.default_credential_type && !credResult && !credSetupStarted && credChangeType === (currentAcct?.default_credential_type || '')}
            <button class="btn-delete cred-delete-btn" on:click={deleteCredential} disabled={credDeleteBusy || credBusy}>
              {credDeleteBusy ? 'Deleting...' : `Delete current ${(currentAcct?.default_credential_type || '').toUpperCase()} credential`}
            </button>
          {/if}
          {#if credResult}
            <button class="btn-cancel" on:click={closeCredChange}>{credResult.ok ? 'Done' : 'Close'}</button>
          {:else if credChangeType === 'token' && credTokenGuide}
            {#if credTokenInput.trim()}
              <button class="btn-add" on:click={storeCredToken} disabled={credBusy}>
                {credBusy ? 'Storing...' : 'Store token'}
              </button>
            {:else}
              <button class="btn-cancel" on:click={closeCredChange}>Cancel</button>
            {/if}
          {:else}
            <button class="btn-cancel" on:click={closeCredChange}>Cancel</button>
            <button class="btn-add" on:click={applyCredChange} disabled={credBusy || (credChangeType === (currentAcct?.default_credential_type || '') && credStatuses[credChangeModal || '']?.status === 'ok')}>
              {credBusy ? 'Setting up...' : 'Setup'}
            </button>
          {/if}
        </div>
      </div>
    </div>
  {/if}

  <!-- ── DELETE ACCOUNT MODAL ── -->
  {#if deleteAcctConfirm}
    {@const acctSources = Object.entries($sources).filter(([_, s]) => s.account === deleteAcctConfirm)}
    {@const repoCount = acctSources.reduce((n, [_, s]) => n + Object.keys(s.repos).length, 0)}
    <div class="overlay" on:click={cancelDeleteAccount} transition:fade={{ duration: 120 }}>
      <div class="modal modal-delete" on:click|stopPropagation transition:slide={{ duration: 180 }}>
        <div class="modal-head modal-head-delete">
          <h3>Delete account</h3>
          <button class="btn-x" on:click={cancelDeleteAccount}>&#10005;</button>
        </div>
        <div class="modal-body">
          {#if deleteAcctStep === 1}
            <p class="delete-warning">Type the account name to confirm:</p>
            <div class="form-row" style="margin-top: 8px;">
              <input class="form-input" type="text" bind:value={deleteAcctInput} placeholder="{deleteAcctConfirm}" on:keydown={(e) => { if (e.key === 'Enter') deleteAcctCheckName(); }} />
            </div>
            {#if deleteAcctError}
              <p class="delete-danger" style="margin-top: 6px;">{deleteAcctError}</p>
            {/if}
          {:else if deleteAcctStep === 2}
            <p class="delete-repo-name">{deleteAcctConfirm}</p>
            <p class="delete-warning delete-danger">
              This will permanently delete the account, {acctSources.length} source{acctSources.length !== 1 ? 's' : ''}, {repoCount} repo{repoCount !== 1 ? 's' : ''}, and all their local clone folders.
            </p>
          {:else}
            <p class="delete-repo-name">{deleteAcctConfirm}</p>
            <p class="delete-final">This action <strong>cannot be undone</strong>. Are you absolutely sure?</p>
          {/if}
        </div>
        <div class="modal-foot">
          <button class="btn-cancel" on:click={cancelDeleteAccount}>Cancel</button>
          {#if deleteAcctStep === 1}
            <button class="btn-delete" on:click={deleteAcctCheckName} disabled={!deleteAcctInput.trim()}>Confirm name</button>
          {:else if deleteAcctStep === 2}
            <button class="btn-delete" on:click={confirmDeleteAccount}>Delete account</button>
          {:else}
            <button class="btn-delete btn-delete-final" on:click={confirmDeleteAccount} disabled={deleteAcctBusy}>
              {deleteAcctBusy ? 'Deleting...' : 'Yes, delete everything'}
            </button>
          {/if}
        </div>
      </div>
    </div>
  {/if}

  <!-- ── CHANGE FOLDER MODAL ── -->
  {#if changeFolderModal}
    <div class="overlay" on:click={() => changeFolderModal = false} transition:fade={{ duration: 120 }}>
      <div class="modal modal-account" on:click|stopPropagation transition:slide={{ duration: 180 }}>
        <div class="modal-head">
          <h3>Change clone folder</h3>
          <button class="btn-x" on:click={() => changeFolderModal = false}>&#10005;</button>
        </div>
        <div class="modal-body">
          <p class="delete-warning delete-danger"><strong>WARNING:</strong> Changing the clone folder will <strong>not</strong> move existing clones. They will show as "Not local" until re-cloned at the new location (or moved manually).</p>
          <div class="form-row" style="margin-top: 12px;">
            <label class="form-label">Current</label>
            <span class="settings-value">{$configStore?.global?.folder || '(not set)'}</span>
          </div>
          {#if changeFolderError}
            <p class="form-error">{changeFolderError}</p>
          {/if}
          <div class="form-row">
            <label class="form-label">New path</label>
            <input class="form-input" bind:value={changeFolderPath} placeholder="~/new-folder" />
            <button class="settings-btn" on:click={() => browseFolder('settings')}>Browse</button>
          </div>
        </div>
        <div class="modal-foot">
          <button class="btn-cancel" on:click={() => changeFolderModal = false}>Cancel</button>
          <button class="btn-add" on:click={confirmChangeFolder} disabled={!changeFolderPath.trim()}>Change folder</button>
        </div>
      </div>
    </div>
  {/if}

  <!-- ── ADD MIRROR GROUP MODAL ── -->
  {#if addMirrorGroupModal}
    <div class="overlay" on:click={() => addMirrorGroupModal = false} transition:fade={{ duration: 120 }}>
      <div class="modal modal-account" on:click|stopPropagation transition:slide={{ duration: 180 }}>
        <div class="modal-head">
          <h3>Add mirror group</h3>
          <button class="btn-x" on:click={() => addMirrorGroupModal = false}>&#10005;</button>
        </div>
        <div class="modal-body">
          <div class="form-row">
            <label class="form-label">Source account</label>
            <select class="form-input" bind:value={newMirrorSrc} on:change={() => { if (newMirrorSrc && newMirrorDst) newMirrorKey = newMirrorSrc + '-' + newMirrorDst; }}>
              <option value="">Select...</option>
              {#each Object.keys($accounts) as acctKey}
                <option value={acctKey}>{acctKey}</option>
              {/each}
            </select>
          </div>
          <div class="form-row">
            <label class="form-label">Destination account</label>
            <select class="form-input" bind:value={newMirrorDst} on:change={() => { if (newMirrorSrc && newMirrorDst) newMirrorKey = newMirrorSrc + '-' + newMirrorDst; }}>
              <option value="">Select...</option>
              {#each Object.keys($accounts).filter(k => k !== newMirrorSrc) as acctKey}
                <option value={acctKey}>{acctKey}</option>
              {/each}
            </select>
          </div>
          <div class="form-row">
            <label class="form-label">Mirror key</label>
            <input class="form-input" bind:value={newMirrorKey} placeholder="e.g. forgejo-github" />
          </div>
        </div>
        <div class="modal-foot">
          <button class="btn-cancel" on:click={() => addMirrorGroupModal = false}>Cancel</button>
          <button class="btn-add" on:click={submitAddMirrorGroup} disabled={!newMirrorKey || !newMirrorSrc || !newMirrorDst}>Create</button>
        </div>
      </div>
    </div>
  {/if}

  <!-- ── ADD MIRROR REPO MODAL ── -->
  {#if addMirrorRepoModal}
    {@const mir = $mirrors[addMirrorRepoModal]}
    {@const filteredPickerRepos = mirrorRepoPickerRepos.filter(r => !mirrorRepoPickerFilter || r.fullName.toLowerCase().includes(mirrorRepoPickerFilter.toLowerCase()))}
    <div class="overlay" on:click={() => addMirrorRepoModal = null} transition:fade={{ duration: 120 }}>
      <div class="modal modal-mirror-repo" on:click|stopPropagation transition:slide={{ duration: 180 }}>
        <div class="modal-head">
          <h3>Add mirror repo &mdash; {addMirrorRepoModal}</h3>
          <button class="btn-x" on:click={() => addMirrorRepoModal = null}>&#10005;</button>
        </div>
        <div class="modal-body">
          <div class="mirror-form-grid">
            <label class="mirror-form-label">Direction</label>
            <div class="radio-group">
              <label><input type="radio" bind:group={newMirrorRepoDirection} value="push" /> Push (origin pushes to backup)</label>
              <label><input type="radio" bind:group={newMirrorRepoDirection} value="pull" /> Pull (backup pulls from origin)</label>
            </div>

            <label class="mirror-form-label">Origin (source of truth)</label>
            <div class="radio-group">
              <label><input type="radio" bind:group={newMirrorRepoOrigin} value="src" on:change={loadMirrorRepoList} /> {mir?.account_src || 'src'}</label>
              <label><input type="radio" bind:group={newMirrorRepoOrigin} value="dst" on:change={loadMirrorRepoList} /> {mir?.account_dst || 'dst'}</label>
            </div>

            <label class="mirror-form-label">Repository</label>
            <div>
              {#if mirrorRepoPickerLoading}
                <div class="loading"><div class="spinner"></div><span>Loading repos...</span></div>
              {:else if mirrorRepoPickerLoaded}
                <input class="form-input form-input-sm" bind:value={mirrorRepoPickerFilter} placeholder="Filter repos..." />
                <div class="mirror-repo-picker">
                  {#each filteredPickerRepos as repo}
                    {@const alreadyMirrored = mir && mir.repos[repo.fullName]}
                    <label class="mrp-row" class:mrp-disabled={!!alreadyMirrored} class:mrp-selected={newMirrorRepoKey === repo.fullName}>
                      <input type="radio" bind:group={newMirrorRepoKey} value={repo.fullName} disabled={!!alreadyMirrored} hidden />
                      <span class="mrp-name">{repo.fullName}</span>
                      <span class="mrp-badges">
                        {#if repo.private}<span class="mrp-badge">private</span>{/if}
                        {#if repo.fork}<span class="mrp-badge">fork</span>{/if}
                        {#if repo.archived}<span class="mrp-badge">archived</span>{/if}
                        {#if alreadyMirrored}<span class="mrp-badge mrp-badge-dim">mirrored</span>{/if}
                      </span>
                    </label>
                  {/each}
                  {#if filteredPickerRepos.length === 0}
                    <p class="mrp-empty">No repos match filter.</p>
                  {/if}
                </div>
              {:else}
                <button class="btn-sm" on:click={loadMirrorRepoList}>Load repos from {newMirrorRepoOrigin === 'src' ? mir?.account_src : mir?.account_dst}</button>
              {/if}
            </div>

            <label class="mirror-form-label">Options</label>
            <label style="font-size: 12px; color: var(--text); display: flex; align-items: center; gap: 4px;"><input type="checkbox" bind:checked={newMirrorRepoAutoSetup} /> Set up immediately via API</label>
          </div>
        </div>
        <div class="modal-foot">
          <button class="btn-cancel" on:click={() => addMirrorRepoModal = null}>Cancel</button>
          <button class="btn-add" on:click={submitAddMirrorRepo} disabled={!newMirrorRepoKey}>Add</button>
        </div>
      </div>
    </div>
  {/if}

  <!-- ── DELETE MIRROR GROUP CONFIRM ── -->
  {#if deleteMirrorGroupConfirm}
    {@const delMir = $mirrors[deleteMirrorGroupConfirm]}
    <div class="overlay" on:click={() => deleteMirrorGroupConfirm = null} transition:fade={{ duration: 120 }}>
      <div class="modal modal-confirm" on:click|stopPropagation transition:slide={{ duration: 180 }}>
        <div class="modal-head"><h3>Delete mirror group</h3></div>
        <div class="modal-body">
          <p class="delete-warning">Remove mirror group <strong>{deleteMirrorGroupConfirm}</strong> ({delMir ? Object.keys(delMir.repos).length : 0} repos)?</p>
          <p class="delete-warning delete-danger">This only removes the config entry. Existing mirror configurations on the servers will remain.</p>
        </div>
        <div class="modal-foot">
          <button class="btn-cancel" on:click={() => deleteMirrorGroupConfirm = null}>Cancel</button>
          <button class="btn-danger" on:click={confirmDeleteMirrorGroup}>Delete</button>
        </div>
      </div>
    </div>
  {/if}

  <!-- ── DELETE MIRROR REPO CONFIRM ── -->
  {#if deleteMirrorRepoConfirm}
    <div class="overlay" on:click={() => deleteMirrorRepoConfirm = null} transition:fade={{ duration: 120 }}>
      <div class="modal modal-confirm" on:click|stopPropagation transition:slide={{ duration: 180 }}>
        <div class="modal-head"><h3>Remove mirrored repo</h3></div>
        <div class="modal-body">
          <p class="delete-warning">Remove <strong>{deleteMirrorRepoConfirm.repoKey}</strong> from mirror <strong>{deleteMirrorRepoConfirm.mirrorKey}</strong>?</p>
        </div>
        <div class="modal-foot">
          <button class="btn-cancel" on:click={() => deleteMirrorRepoConfirm = null}>Cancel</button>
          <button class="btn-danger" on:click={confirmDeleteMirrorRepo}>Remove</button>
        </div>
      </div>
    </div>
  {/if}

  <!-- ── MIRROR SETUP RESULT MODAL ── -->
  {#if mirrorSetupResultModal}
    <div class="overlay" on:click={() => mirrorSetupResultModal = null} transition:fade={{ duration: 120 }}>
      <div class="modal modal-confirm" on:click|stopPropagation transition:slide={{ duration: 180 }}>
        <div class="modal-head">
          <h3>Mirror setup {mirrorSetupResultModal.error ? 'failed' : 'complete'}</h3>
          <button class="btn-x" on:click={() => mirrorSetupResultModal = null}>&#10005;</button>
        </div>
        <div class="modal-body">
          {#if mirrorSetupResultModal.error}
            <p class="form-error">{mirrorSetupResultModal.error}</p>
          {:else}
            <p style="color: var(--text)">
              <strong>{mirrorSetupResultModal.repoKey}</strong> &mdash;
              {mirrorSetupResultModal.created ? 'target repo created' : ''}
              {mirrorSetupResultModal.created && mirrorSetupResultModal.mirrored ? ', ' : ''}
              {mirrorSetupResultModal.mirrored ? 'mirror configured' : ''}
            </p>
          {/if}
          {#if mirrorSetupResultModal.method === 'manual' && mirrorSetupResultModal.instructions}
            <pre class="mirror-instructions">{mirrorSetupResultModal.instructions}</pre>
          {/if}
        </div>
        <div class="modal-foot">
          <button class="btn-add" on:click={() => mirrorSetupResultModal = null}>OK</button>
        </div>
      </div>
    </div>
  {/if}

  <!-- ── MIRROR DISCOVER RESULTS MODAL ── -->
  {#if mirrorDiscoverResults !== null}
    <div class="overlay" on:click={() => mirrorDiscoverResults = null} transition:fade={{ duration: 120 }}>
      <div class="modal modal-discover" on:click|stopPropagation transition:slide={{ duration: 180 }}>
        <div class="modal-head">
          <h3>Mirror Discovery</h3>
          <button class="btn-x" on:click={() => mirrorDiscoverResults = null}>&#10005;</button>
        </div>
        <div class="modal-body">
          {#if mirrorDiscoverError}
            <p class="form-error">{mirrorDiscoverError}</p>
          {:else if mirrorDiscoverResults.length === 0}
            <p style="color: var(--text-muted); font-size: 12px;">No mirror relationships found across your accounts.</p>
          {:else}
            {#each mirrorDiscoverResults as dr}
              <div style="margin-bottom: 12px;">
                <div style="font-size: 12px; font-weight: 500; color: var(--text-primary); margin-bottom: 4px;">
                  {dr.AccountSrc} ↔ {dr.AccountDst}
                </div>
                {#each dr.Discovered as dm}
                  {@const alreadyConfigured = isDiscoveredRepoConfigured(dr.AccountSrc, dr.AccountDst, dm.RepoKey)}
                  {@const justAdded = discoverAdded[`${dr.AccountSrc}:${dr.AccountDst}:${dm.RepoKey}`]}
                  <div class="mrp-row" style="border-bottom: none; {alreadyConfigured || justAdded ? 'opacity: 0.5;' : ''}">
                    <span class="mrp-name">{dm.RepoKey}</span>
                    <span class="mrp-badge">{dm.Direction}</span>
                    <span class="mrp-badge">{dm.Origin}</span>
                    <span class="mrp-badge" class:mrp-badge-dim={dm.Confidence === 'possible'} style={dm.Confidence === 'confirmed' ? 'background: #166534; color: #dcfce7;' : dm.Confidence === 'likely' ? 'background: #1e40af; color: #dbeafe;' : ''}>{dm.Confidence}</span>
                    {#if alreadyConfigured || justAdded}
                      <span class="mrp-badge" style="background: #166534; color: #dcfce7;">configured</span>
                    {:else}
                      <button class="btn-sm" style="margin-left: auto; padding: 1px 8px; font-size: 10px;" on:click={() => addDiscoveredRepo(dr, dm)}>+ Add</button>
                    {/if}
                  </div>
                {/each}
              </div>
            {/each}
          {/if}
        </div>
        <div class="modal-foot">
          <button class="btn-cancel" on:click={() => mirrorDiscoverResults = null}>Close</button>
          {#if mirrorDiscoverResults.length > 0}
            <button class="btn-add" on:click={applyMirrorDiscovery}>Apply to config</button>
          {/if}
        </div>
      </div>
    </div>
  {/if}

  <!-- ── MIRROR CREDENTIAL WARNING MODAL ── -->
  {#if mirrorCredWarning}
    <div class="overlay" on:click={() => mirrorCredWarning = null} transition:fade={{ duration: 120 }}>
      <div class="modal modal-confirm" on:click|stopPropagation transition:slide={{ duration: 180 }}>
        <div class="modal-head">
          <h3>Mirror token needed</h3>
          <button class="btn-x" on:click={() => mirrorCredWarning = null}>&#10005;</button>
        </div>
        <div class="modal-body">
          <p class="delete-warning">{mirrorCredWarning.message}</p>
          <p style="color: var(--text-muted); margin-top: 8px;">Account: <strong>{mirrorCredWarning.accountKey}</strong></p>
          <p style="color: var(--text-muted);">GCM OAuth tokens are machine-local and can't be used by remote servers. You need to create a PAT and store it.</p>
        </div>
        <div class="modal-foot">
          <button class="btn-cancel" on:click={() => mirrorCredWarning = null}>Cancel</button>
          <button class="btn-add" on:click={() => {
            const key = mirrorCredWarning?.accountKey;
            mirrorCredWarning = null;
            if (key) {
              credChangeModal = key;
              credChangeType = 'token';
            }
          }}>Store PAT</button>
        </div>
      </div>
    </div>
  {/if}

  <!-- ── CREATE REPO MODAL ── -->
  {#if createRepoModal}
    <div class="overlay" on:click={() => { if (!createRepoBusy) createRepoModal = null; }} transition:fade={{ duration: 120 }}>
      <div class="modal modal-account" on:click|stopPropagation transition:slide={{ duration: 180 }}>
        <div class="modal-head">
          <h3>Create repository &mdash; {createRepoModal}</h3>
          <button class="btn-x" on:click={() => { if (!createRepoBusy) createRepoModal = null; }}>&#10005;</button>
        </div>
        <div class="modal-body">
          <p class="create-repo-warning">This will create a <strong>new</strong> repository in your <strong>{$accounts[createRepoModal]?.provider || 'provider'}</strong> account ({$accounts[createRepoModal]?.url || ''})</p>
          {#if createRepoError}
            <p class="form-error">{createRepoError}</p>
          {/if}
          <div class="form-row">
            <label class="form-label">Owner</label>
            <select class="form-input" bind:value={createRepoOwner} disabled={createRepoBusy}>
              {#each createRepoOrgs as org}
                <option value={org}>{org}</option>
              {/each}
            </select>
          </div>
          <div class="form-row">
            <label class="form-label">Name</label>
            <input class="form-input" bind:value={createRepoName} on:input={onCreateRepoNameInput} placeholder="my-new-repo" disabled={createRepoBusy} />
            {#if createRepoNameError}
              <span class="form-error" style="margin: 2px 0 0; font-size: 10px;">{createRepoNameError}</span>
            {/if}
          </div>
          <div class="form-row">
            <label class="form-label">Description</label>
            <input class="form-input" bind:value={createRepoDesc} placeholder="Short description (optional)" disabled={createRepoBusy} />
          </div>
          <div class="form-row" style="gap: 12px;">
            <label style="font-size: 12px; color: var(--text); display: flex; align-items: center; gap: 4px;">
              <input type="checkbox" bind:checked={createRepoPrivate} disabled={createRepoBusy} /> Private
            </label>
            <label style="font-size: 12px; color: var(--text); display: flex; align-items: center; gap: 4px;">
              <input type="checkbox" bind:checked={createRepoClone} disabled={createRepoBusy} /> Clone after creating
            </label>
          </div>
        </div>
        <div class="modal-foot">
          <button class="btn-cancel" on:click={() => { if (!createRepoBusy) createRepoModal = null; }} disabled={createRepoBusy}>Cancel</button>
          <button class="btn-add" on:click={submitCreateRepo}
            disabled={createRepoBusy || !createRepoOwner || !createRepoName || !!createRepoNameError}>
            {createRepoBusy ? 'Creating...' : createRepoClone ? 'Create & Clone' : 'Create'}
          </button>
        </div>
      </div>
    </div>
  {/if}

</div>
{/if}

<!-- ════════════════════════════════════════════════════════════ -->
<!--  STYLES                                                     -->
<!-- ════════════════════════════════════════════════════════════ -->

<style>
  /* ── Onboarding ── */
  .onboarding {
    position: fixed; inset: 0; background: var(--bg-base);
    display: flex; align-items: center; justify-content: center; z-index: 200;
  }
  .onboard-card {
    display: flex; flex-direction: column; align-items: center;
    max-width: 380px; width: 100%; padding: 40px 32px;
  }
  .onboard-logo { width: 64px; height: 64px; margin-bottom: 16px; }
  .onboard-title { font-size: 22px; font-weight: 700; margin: 0 0 6px; letter-spacing: -0.5px; }
  .onboard-desc { font-size: 13px; color: var(--text-secondary); margin: 0 0 20px; }
  .onboard-input-row { display: flex; gap: 8px; width: 100%; margin-bottom: 16px; }
  .onboard-input { flex: 1; }
  .onboard-browse { padding: 5px 12px; font-size: 12px; }
  .onboard-go { width: 100%; padding: 9px 0; font-size: 13px; font-weight: 600; border-radius: 7px; }

  :global([data-theme="dark"]) {
    --bg-base: #09090b; --bg-card: #18181b; --bg-hover: #27272a;
    --border: #27272a; --border-hover: #3f3f46;
    --text-primary: #fafafa; --text-secondary: #b4b4bd; --text-muted: #8e8e99; --text-dim: #71717a;
    --text-repo: #e4e4e7;
    --logo-dark: #4abdd4; --logo-light: #7cd9ec;
    --ring-bg: #27272a; --ring-accent: #61fd5f; --overlay: rgba(0,0,0,0.6);
    --card-shadow: 0 2px 8px rgba(0, 0, 0, 0.25);
    --spin-color: #0AFFFF;
  }
  :global([data-theme="light"]) {
    --bg-base: #fafafa; --bg-card: #ffffff; --bg-hover: #f4f4f5;
    --border: #e4e4e7; --border-hover: #d4d4d8;
    --text-primary: #18181b; --text-secondary: #52525b; --text-muted: #71717a; --text-dim: #a1a1aa;
    --text-repo: #27272a;
    --logo-dark: #1c5566; --logo-light: #2e9fc0;
    --ring-bg: #e4e4e7; --ring-accent: #166534; --overlay: rgba(0,0,0,0.3);
    --card-shadow: 0 2px 8px rgba(0, 0, 0, 0.08);
    --spin-color: #0000ff;
  }
  :global(html) { color-scheme: dark light; }
  :global(body) {
    margin: 0; background: var(--bg-base);
    font-family: -apple-system, BlinkMacSystemFont, 'Inter', 'Segoe UI', system-ui, sans-serif;
    color: var(--text-primary); -webkit-font-smoothing: antialiased;
    transition: background 0.2s, color 0.2s;
  }

  .app { max-width: 860px; margin: 0 auto; height: 100vh; display: flex; flex-direction: column; overflow: hidden; }

  .topbar { display: flex; align-items: center; gap: 16px; padding: 14px 24px; border-bottom: 1px solid var(--border); }
  .brand { display: flex; align-items: center; gap: 8px; }
  .logo-svg { width: 26px; height: 26px; }
  .title { font-size: 17px; font-weight: 700; letter-spacing: -0.3px; }
  .tagline { font-size: 11px; font-weight: 400; color: var(--text-muted); letter-spacing: 0.3px; margin-left: 6px; white-space: nowrap; }

  .health { display: flex; align-items: center; gap: 8px; margin-left: auto; }
  .health-ring {
    width: 38px; height: 38px; border-radius: 50%;
    background: conic-gradient(var(--ring-accent) calc(var(--pct) * 1%), var(--ring-bg) 0);
    display: flex; align-items: center; justify-content: center; position: relative;
  }
  .health-ring::before { content: ''; position: absolute; inset: 4px; background: var(--bg-base); border-radius: 50%; transition: background 0.2s; }
  .health-num { position: relative; z-index: 1; font-size: 10px; font-weight: 700; }
  .health-label { font-size: 12px; color: var(--text-muted); }

  .topbar-actions { display: flex; gap: 8px; }
  .topbar-icon { width: 16px; height: 16px; display: inline-block; }
  .sync-icon { font-size: 18px; display: inline-block; }
  .spinning { animation: spin 0.8s linear infinite; color: var(--spin-color) !important; stroke: var(--spin-color); stroke-width: 2.5; font-weight: 900; }
  @keyframes spin { to { transform: rotate(360deg); } }
  .btn-gear {
    background: none; border: 1px solid transparent; color: var(--text-muted);
    font-size: 18px; cursor: pointer; padding: 4px 6px; border-radius: 6px; transition: all 0.12s;
    width: 32px; height: 32px; display: inline-flex; align-items: center; justify-content: center;
    line-height: 1;
  }
  .btn-gear:hover:not(:disabled) { color: var(--text-primary); background: var(--bg-hover); }
  .btn-gear:disabled { opacity: 0.3; cursor: default; }
  .btn-trash { font-size: 15px; }

  .cards-tab-bar { display: flex; gap: 4px; padding: 12px 24px 0; align-items: center; }
  .tab-bar-actions { margin-left: auto; display: flex; gap: 6px; }
  .btn-tab-action {
    padding: 3px 10px; font-size: 11px; font-weight: 500;
    border: 1px solid #1e3a5f; background: rgba(37, 99, 235, 0.1);
    color: #93c5fd; border-radius: 5px;
    cursor: pointer; transition: all 0.12s;
  }
  .btn-tab-action:hover:not(:disabled) { background: rgba(37, 99, 235, 0.2); color: #bfdbfe; }
  .btn-tab-action:disabled { opacity: 0.5; cursor: default; }
  :global([data-theme="light"]) .btn-tab-action { border-color: #93c5fd; background: rgba(37, 99, 235, 0.06); color: #2563eb; }
  :global([data-theme="light"]) .btn-tab-action:hover:not(:disabled) { background: rgba(37, 99, 235, 0.12); }
  .cards-tab {
    padding: 4px 14px; font-size: 12px; font-weight: 600;
    border: 1px solid var(--border); background: transparent;
    color: var(--text-secondary); border-radius: 6px;
    cursor: pointer; transition: all 0.12s;
  }
  .cards-tab:hover { background: var(--bg-hover); color: var(--text-primary); }
  .cards-tab-active { background: var(--bg-hover); color: var(--text-primary); border-color: var(--border-hover); }

  .cards-row { display: flex; gap: 10px; padding: 18px 24px; overflow-x: auto; flex-shrink: 0; }
  .card {
    flex: 1; min-width: 165px; background: var(--bg-card); border: 1px solid var(--border);
    border-radius: 10px; padding: 12px 14px; transition: border-color 0.15s, box-shadow 0.15s;
    box-shadow: var(--card-shadow);
  }
  .card:hover { border-color: var(--border-hover); }
  .card-top { display: flex; align-items: center; gap: 6px; margin-bottom: 2px; }
  .card-dot { width: 7px; height: 7px; border-radius: 50%; }
  .card-provider { font-size: 10px; color: var(--text-muted); font-weight: 600; letter-spacing: 0.3px; flex: 1; }
  .cred-badge {
    font-size: 8px; font-weight: 700; letter-spacing: 0.3px; text-transform: uppercase;
    padding: 1px 4px; border-radius: 3px; cursor: pointer;
    background: var(--bg-hover); border: 1px solid var(--border); color: var(--text-muted);
    transition: all 0.12s; line-height: 1.3;
  }
  .cred-badge:hover { color: var(--text-primary); border-color: var(--border-hover); background: var(--bg-card); }
  .cred-badge-ok { background: #14532d; border-color: #166534; color: #86efac; }
  .cred-badge-err { background: #450a0a; border-color: #be123c; color: #fca5a5; }
  .cred-badge-warn { background: #431407; border-color: #c2410c; color: #fdba74; }
  .cred-badge-none { background: #1e3a5f; border-color: #2563eb; color: #93c5fd; }
  .cred-badge-pending { background: #27272a; border-color: #52525b; color: #a1a1aa; animation: pulse-badge 1.5s ease-in-out infinite; }
  @keyframes pulse-badge { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }
  :global([data-theme="light"]) .cred-badge-pending { background: #f4f4f5; border-color: #a1a1aa; color: #71717a; }
  :global([data-theme="light"]) .cred-badge-ok { background: #dcfce7; border-color: #166534; color: #166534; }
  :global([data-theme="light"]) .cred-badge-err { background: #fee2e2; border-color: #be123c; color: #be123c; }
  :global([data-theme="light"]) .cred-badge-warn { background: #ffedd5; border-color: #c2410c; color: #c2410c; }
  :global([data-theme="light"]) .cred-badge-none { background: #dbeafe; border-color: #2563eb; color: #2563eb; }
  .card-delete-btn { flex-shrink: 0; }
  .card-name { font-size: 14px; font-weight: 600; margin-bottom: 8px; }
  .card-mirror-name { font-size: 12px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .mirror-tab-actions { display: flex; gap: 6px; padding: 8px 24px 0; }
  .discover-progress { padding: 6px 24px 0; }
  .discover-progress-text { font-size: 11px; color: var(--text-secondary); margin-bottom: 4px; }
  .discover-progress-text strong { color: var(--text-primary); }
  .discover-progress-bar { width: 100%; height: 4px; background: var(--ring-bg); border-radius: 2px; overflow: hidden; }
  .discover-progress-fill { height: 100%; background: var(--ring-accent); border-radius: 2px; transition: width 0.2s; }
  .discover-progress-indeterminate {
    width: 30%; animation: indeterminate 1.2s ease-in-out infinite;
  }
  @keyframes indeterminate { 0% { margin-left: 0; } 50% { margin-left: 70%; } 100% { margin-left: 0; } }
  .card-name-edit { cursor: pointer; }
  .card-name-edit:hover { text-decoration: underline; }

  .card-ring-row { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; }
  .mini-ring { width: 28px; height: 28px; flex-shrink: 0; }
  .card-stat { font-size: 12px; font-weight: 600; color: var(--text-repo); }
  .card-issues { font-size: 11px; font-weight: 600; }
  .card-ok { font-size: 11px; font-weight: 600; }

  .card-btn-row { display: flex; gap: 4px; }
  .card-btn-row .card-btn { flex: 1; }
  .card-btn {
    width: 100%; padding: 5px 0; background: transparent; border: 1px solid var(--border);
    color: var(--text-secondary); border-radius: 6px; cursor: pointer; font-size: 11px; font-weight: 500;
    transition: all 0.15s;
  }
  .card-btn:hover:not(:disabled) { background: var(--bg-hover); color: var(--text-primary); border-color: var(--border-hover); }
  .card-btn:disabled { opacity: 0.35; cursor: default; }

  .repo-list { flex: 1; padding: 0 24px 12px; overflow-y: auto; min-height: 0; }
  .source-group { margin-bottom: 6px; }
  .source-header {
    font-size: 11px; font-weight: 600; color: var(--text-dim);
    text-transform: uppercase; letter-spacing: 0.8px;
    padding: 10px 0 5px; border-bottom: 1px solid var(--border);
  }
  .repo-row {
    display: flex; align-items: center; gap: 10px;
    padding: 8px 6px; border-radius: 6px; transition: background 0.1s;
  }
  .repo-row:hover { background: var(--bg-card); }

  .dot { font-size: 14px; flex-shrink: 0; width: 18px; text-align: center; }
  .repo-name { font-size: 13px; color: var(--text-repo); flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  .progress-track { width: 110px; height: 4px; background: var(--ring-bg); border-radius: 2px; overflow: hidden; flex-shrink: 0; }
  .progress-fill { height: 100%; border-radius: 2px; transition: width 0.12s ease-out; }
  .progress-pct { font-size: 11px; width: 32px; text-align: right; flex-shrink: 0; font-weight: 600; }

  .status-text { font-size: 12px; white-space: nowrap; font-weight: 600; }

  .btn-action {
    padding: 3px 10px; background: transparent; border: 1px solid var(--border);
    color: var(--text-secondary); border-radius: 5px; cursor: pointer;
    font-size: 11px; font-weight: 500; transition: all 0.15s; flex-shrink: 0;
  }
  .btn-action:hover { background: var(--bg-hover); color: var(--text-primary); border-color: var(--border-hover); }
  .btn-fetch {
    background: transparent; border: none; color: var(--text-dim); cursor: pointer;
    font-size: 14px; padding: 2px 4px; border-radius: 4px; transition: color 0.15s;
    flex-shrink: 0; line-height: 1;
  }
  .btn-fetch:hover { color: var(--text-primary); }
  .btn-fetch:disabled { opacity: 0.6; cursor: default; }
  .status-badges { display: flex; align-items: center; gap: 6px; }
  .sbadge { font-size: 11px; font-weight: 600; white-space: nowrap; }
  .status-pending { font-size: 12px; font-weight: 600; color: var(--text-dim); }

  /* ── Repo detail panel ── */
  .repo-row-clickable { cursor: pointer; }
  .repo-row-clickable:hover { background: var(--bg-card); }
  .repo-detail {
    margin: 0 0 2px 0; padding: 8px 16px 10px 30px;
    background: var(--bg-card); border: 1px solid var(--border); border-radius: 6px;
    max-height: 180px; overflow-y: auto; font-size: 12px;
  }
  .detail-loading, .detail-error, .detail-clean {
    color: var(--text-muted); font-style: italic;
  }
  .detail-error { color: var(--text-primary); }
  .detail-header {
    display: flex; align-items: center; gap: 10px; margin-bottom: 6px;
    font-size: 11px; color: var(--text-secondary);
  }
  .detail-branch { color: var(--text-muted); }
  .detail-branch strong { color: var(--text-primary); font-weight: 600; }
  .detail-badge { font-weight: 600; font-size: 11px; }
  .detail-section-title {
    font-size: 10px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.5px;
    color: var(--text-dim); margin: 6px 0 3px 0;
  }
  .detail-file {
    display: flex; align-items: baseline; gap: 6px; padding: 1px 0;
    font-family: 'SF Mono', 'Cascadia Code', 'Consolas', monospace;
    font-size: 10px;
  }
  .detail-kind {
    width: 14px; text-align: center; font-weight: 700; flex-shrink: 0;
    color: var(--text-secondary);
  }
  .kind-added { color: #61fd5f; }
  .kind-deleted { color: #D81E5B; }
  .kind-conflict { color: #D81E5B; }
  .kind-untracked { color: var(--text-dim); }
  .detail-path { color: var(--text-repo); word-break: break-all; }
  .detail-path-dim { color: var(--text-dim); }

  .summary {
    display: flex; align-items: center; justify-content: center; gap: 6px;
    padding: 10px 24px; border-top: 1px solid var(--border); font-size: 12px; font-weight: 500;
    flex-shrink: 0;
  }
  .sum { font-weight: 600; }
  .sep { color: var(--border-hover); }

  .settings {
    padding: 14px 24px; border-bottom: 1px solid var(--border);
    background: var(--bg-card); display: flex; flex-direction: column; gap: 8px;
  }
  .settings-row { display: flex; align-items: center; gap: 12px; }
  .settings-label { font-size: 11px; font-weight: 600; color: var(--text-muted); width: 80px; flex-shrink: 0; }
  .settings-value { font-size: 12px; color: var(--text-secondary); font-family: monospace; flex: 1; }
  .settings-btn {
    padding: 2px 8px; font-size: 10px; font-weight: 500;
    background: transparent; border: 1px solid var(--border); color: var(--text-secondary);
    border-radius: 4px; cursor: pointer; transition: all 0.12s; white-space: nowrap;
  }
  .settings-btn:hover { background: var(--bg-hover); color: var(--text-primary); border-color: var(--border-hover); }

  /* ── Global identity warning banner ── */
  .identity-warn {
    display: flex; align-items: center; gap: 8px;
    padding: 7px 12px; margin: 0 16px 8px;
    background: #431407; border: 1px solid #c2410c; border-radius: 8px;
    font-size: 12px; color: #fdba74;
  }
  :global([data-theme="light"]) .identity-warn {
    background: #fff7ed; border-color: #c2410c; color: #7c2d12;
  }
  .identity-warn-icon { font-size: 15px; flex-shrink: 0; }
  .identity-warn-text { flex: 1; line-height: 1.4; }
  .identity-warn-text code { font-size: 11px; padding: 1px 4px; background: rgba(255,255,255,0.1); border-radius: 3px; }
  :global([data-theme="light"]) .identity-warn-text code { background: rgba(0,0,0,0.06); }
  .identity-warn-fix {
    padding: 3px 10px; font-size: 11px; font-weight: 600;
    background: #c2410c; color: #fff; border: none; border-radius: 5px; cursor: pointer;
    white-space: nowrap;
  }
  .identity-warn-fix:hover { background: #ea580c; }
  .identity-warn-dismiss {
    background: none; border: none; color: #fdba74; cursor: pointer;
    font-size: 14px; padding: 0 2px; line-height: 1;
  }
  :global([data-theme="light"]) .identity-warn-dismiss { color: #7c2d12; }
  .identity-warn-dismiss:hover { opacity: 0.7; }

  .theme-toggle { display: flex; gap: 4px; }
  .theme-btn {
    padding: 3px 10px; font-size: 11px; border: 1px solid var(--border);
    background: transparent; color: var(--text-secondary); border-radius: 5px;
    cursor: pointer; transition: all 0.12s;
  }
  .theme-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
  .theme-active { background: var(--bg-hover); color: var(--text-primary); border-color: var(--border-hover); }
  .active-gear { color: var(--text-primary) !important; }

  .overlay {
    position: fixed; inset: 0; background: var(--overlay);
    display: flex; align-items: center; justify-content: center; z-index: 100;
  }
  .modal {
    background: var(--bg-card); border: 1px solid var(--border);
    border-radius: 12px; width: 440px; max-height: 70vh;
    display: flex; flex-direction: column;
  }
  .modal-head {
    display: flex; justify-content: space-between; align-items: center;
    padding: 14px 18px; border-bottom: 1px solid var(--border);
  }
  .modal-head h3 { margin: 0; font-size: 14px; font-weight: 600; }
  .btn-x { background: none; border: none; color: var(--text-muted); cursor: pointer; font-size: 16px; transition: color 0.12s; }
  .btn-x:hover { color: var(--text-primary); }
  .modal-body { padding: 14px 18px; overflow-y: auto; flex: 1; }
  .modal-foot {
    display: flex; justify-content: flex-end; gap: 8px;
    padding: 10px 18px; border-top: 1px solid var(--border);
  }

  .loading { display: flex; align-items: center; gap: 12px; padding: 20px 0; color: var(--text-secondary); font-size: 13px; }
  .spinner { width: 18px; height: 18px; border: 2px solid var(--border); border-top-color: #4B95E9; border-radius: 50%; animation: spin 0.7s linear infinite; }
  .found { font-size: 12px; color: var(--text-muted); margin: 0 0 10px; }

  .dr { display: flex; align-items: center; gap: 8px; padding: 6px 4px; cursor: pointer; border-radius: 4px; font-size: 13px; }
  .dr:hover { background: var(--bg-hover); }
  .dr-all { border-bottom: 1px solid var(--border); padding-bottom: 8px; margin-bottom: 4px; }
  .dr input { accent-color: #4B95E9; }
  .dr-name { color: var(--text-repo); font-weight: 500; }
  .dr-desc { color: var(--text-dim); font-size: 11px; margin-left: auto; }
  .dr-tag { font-size: 9px; padding: 1px 5px; background: var(--bg-hover); color: var(--text-dim); border-radius: 3px; }
  .dr-disabled { opacity: 0.4; cursor: default; }
  .dr-disabled:hover { background: transparent; }

  .btn-cancel { padding: 6px 12px; background: var(--bg-hover); border: 1px solid var(--border-hover); color: var(--text-secondary); border-radius: 6px; cursor: pointer; font-size: 12px; }
  .btn-cancel:hover { color: var(--text-primary); }
  .btn-add { padding: 6px 12px; background: #4B95E9; border: none; color: #fff; border-radius: 6px; cursor: pointer; font-size: 12px; font-weight: 500; }
  .btn-add:hover:not(:disabled) { background: #3b82f6; }
  .btn-add:disabled { opacity: 0.4; cursor: default; }

  /* ── Add account card ── */
  .card-add {
    min-width: 60px; max-width: 80px; display: flex; align-items: center; justify-content: center;
    cursor: pointer; border-style: dashed; background: transparent;
    color: var(--text-muted); transition: all 0.15s;
  }
  .card-add:hover { border-color: var(--border-hover); color: var(--text-primary); background: var(--bg-hover); }
  .card-add-icon { font-size: 24px; font-weight: 300; line-height: 1; }

  /* ── Add account form ── */
  .modal-account { width: 580px; }
  .modal-discover { width: 650px; }
  .discover-filter { width: 100%; margin-bottom: 8px; font-size: 13px; }
  .form-row { display: flex; align-items: center; gap: 10px; margin-bottom: 8px; }
  .form-label { font-size: 11px; font-weight: 600; color: var(--text-muted); width: 95px; flex-shrink: 0; }
  .form-input {
    flex: 1; padding: 5px 8px; font-size: 12px; border: 1px solid var(--border);
    background: var(--bg-base); color: var(--text-primary); border-radius: 5px;
    outline: none; font-family: inherit; transition: border-color 0.12s;
  }
  .form-input:focus { border-color: #4B95E9; }
  select.form-input { cursor: pointer; }
  .form-static { font-size: 13px; color: var(--text-muted); padding: 6px 0; }
  .form-error { font-size: 12px; color: #D81E5B; margin: 0 0 10px; font-weight: 600; }
  .create-repo-warning {
    font-size: 12px; color: #F07623; background: rgba(240, 118, 35, 0.1);
    border: 1px solid rgba(240, 118, 35, 0.3); border-radius: 4px;
    padding: 8px 10px; margin: 0 0 12px;
  }
  :global([data-theme="light"]) .create-repo-warning {
    color: #c2410c; background: #fff7ed; border-color: rgba(194, 65, 12, 0.3);
  }

  /* ── Credential setup step ── */
  .cred-step-intro { font-size: 13px; color: var(--text-primary); margin: 0 0 10px; }
  .cred-step-desc { font-size: 12px; color: var(--text-secondary); margin: 0 0 10px; line-height: 1.5; }
  .cred-change-warning { font-size: 11px; color: #f59e0b; margin: 8px 0; padding: 6px 10px; border-radius: 4px; background: #f59e0b12; border: 1px solid #f59e0b44; line-height: 1.4; }
  :global([data-theme="light"]) .cred-change-warning { color: #b45309; background: #fef3c7; border-color: #f59e0b66; }
  .cred-step-link { font-size: 11px; margin: 0 0 10px; }
  .cred-step-link a { color: #4B95E9; text-decoration: none; word-break: break-all; }
  .cred-step-link a:hover { text-decoration: underline; }
  .cred-doc-link { font-size: 11px; margin: 0 0 12px; }
  .cred-doc-link a { color: #4B95E9; text-decoration: underline; }
  .cred-doc-link a:hover { color: #6db0f7; }
  .cred-action-btn { margin-top: 4px; margin-bottom: 10px; }
  .cred-result { margin-top: 10px; padding: 8px 10px; border-radius: 6px; font-size: 12px; }
  .cred-result-ok { background: #16653412; border: 1px solid #166534; color: var(--text-primary); }
  .cred-result-err { background: #D81E5B12; border: 1px solid #D81E5B; color: #D81E5B; }
  .cred-result-msg { margin: 0; white-space: pre-wrap; font-family: monospace; font-size: 11px; line-height: 1.5; }
  .ssh-key-box { margin: 6px 0; padding: 6px 8px; background: var(--bg-hover, #222); border: 1px solid var(--border, #444); border-radius: 4px; display: flex; align-items: center; gap: 6px; }
  .ssh-key-text { font-family: monospace; font-size: 9px; word-break: break-all; white-space: nowrap; color: var(--text-primary); flex: 1; overflow-x: auto; user-select: all; }
  .ssh-key-copy { background: none; border: 1px solid var(--border, #444); border-radius: 3px; padding: 3px 6px; cursor: pointer; color: var(--text-muted); font-size: 12px; flex-shrink: 0; transition: all 0.15s; }
  .ssh-key-copy:hover { color: var(--text-primary); border-color: var(--border-hover, #666); background: var(--bg-card, #2a2a2a); }
  .ssh-connection-status { display: flex; align-items: center; gap: 6px; margin-top: 8px; font-size: 12px; font-weight: 600; }
  .ssh-status-dot { width: 10px; height: 10px; border-radius: 50%; flex-shrink: 0; }
  .ssh-connected .ssh-status-dot { background: #22c55e; box-shadow: 0 0 6px #22c55e88; }
  .ssh-connected { color: #86efac; }
  .ssh-disconnected .ssh-status-dot { background: #ef4444; box-shadow: 0 0 6px #ef444488; }
  .ssh-disconnected { color: #fca5a5; }
  :global([data-theme="light"]) .ssh-connected { color: #166534; }
  :global([data-theme="light"]) .ssh-disconnected { color: #be123c; }

  /* ── Delete mode ── */
  .delete-active { color: #D81E5B !important; }
  .btn-delete-x {
    background: none; border: 1px solid #D81E5B55; color: #D81E5B;
    cursor: pointer; font-size: 9px; font-weight: 600;
    width: 16px; height: 16px; padding: 0; flex-shrink: 0;
    border-radius: 3px; display: flex; align-items: center; justify-content: center;
    opacity: 0.7; transition: all 0.12s; line-height: 1;
  }
  .btn-delete-x:hover { opacity: 1; background: #D81E5B22; border-color: #D81E5B; }

  .modal-delete { width: 380px; }
  .modal-head-delete { border-bottom-color: #D81E5B44; }
  .modal-head-delete h3 { color: #D81E5B; }
  .delete-repo-name { font-size: 14px; font-weight: 600; color: var(--text-primary); margin: 0 0 8px; font-family: monospace; }
  .delete-warning { font-size: 13px; color: var(--text-secondary); margin: 0; line-height: 1.5; }
  .delete-danger { color: #D81E5B; font-weight: 600; }
  .delete-final { font-size: 13px; color: var(--text-secondary); margin: 0; line-height: 1.5; }
  .btn-delete {
    padding: 6px 12px; background: #D81E5B; border: none; color: #fff;
    border-radius: 6px; cursor: pointer; font-size: 12px; font-weight: 500;
  }
  .btn-delete:hover:not(:disabled) { background: #be123c; }
  .btn-delete-final { background: #991b1b; }
  .btn-delete-final:hover:not(:disabled) { background: #7f1d1d; }
  .btn-delete:disabled { opacity: 0.5; cursor: default; }
  .cred-delete-btn { margin-right: auto; }

  /* ── Compact status view ── */
  .compact-strip {
    width: 100%;
    min-height: 100vh;
    background: var(--bg-base);
    color: var(--text-primary);
    padding: 10px;
    display: flex;
    flex-direction: column;
    gap: 4px;
    overflow-y: hidden;
    box-sizing: border-box;
  }
  .compact-global {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 4px 0;
  }
  .compact-ring { width: 36px; height: 36px; flex-shrink: 0; }
  .compact-global-text { display: flex; flex-direction: column; }
  .compact-global-pct { font-size: 16px; font-weight: 700; line-height: 1.1; }
  .compact-global-label { font-size: 10px; color: var(--text-dim); }

  .compact-sep { border-top: 1px solid var(--border); margin: 2px 0; }

  .compact-acct {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 6px;
    border-radius: 8px;
    border: none;
    background: transparent;
    color: inherit;
    cursor: pointer;
    transition: background 0.1s;
    text-align: left;
    width: 100%;
    font-family: inherit;
  }
  .compact-acct:hover { background: var(--bg-hover); }
  .compact-acct-expanded { background: var(--bg-hover); }
  .compact-acct-cred-err { background: #3a161b; }
  .compact-acct-cred-err:hover { background: #4a1c23; }
  :global([data-theme="light"]) .compact-acct-cred-err { background: #fee2e2; }
  :global([data-theme="light"]) .compact-acct-cred-err:hover { background: #fcd5d5; }
  .compact-acct-cred-warn { background: #3a240e; }
  .compact-acct-cred-warn:hover { background: #4a2e12; }
  :global([data-theme="light"]) .compact-acct-cred-warn { background: #feebd0; }
  :global([data-theme="light"]) .compact-acct-cred-warn:hover { background: #fde0ba; }
  .compact-acct-ring { width: 24px; height: 24px; flex-shrink: 0; }
  .compact-acct-info { display: flex; flex-direction: column; min-width: 0; flex: 1; }
  .compact-acct-name {
    font-size: 11px; font-weight: 600;
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .compact-acct-stat { font-size: 9px; }
  .compact-chevron {
    font-size: 10px;
    color: var(--text-dim);
    flex-shrink: 0;
    width: 12px;
    text-align: center;
  }

  .compact-repo-list {
    padding: 0 0 4px 8px;
    border-left: 2px solid var(--border);
    margin-left: 17px;
    margin-bottom: 2px;
  }
  .compact-row {
    display: flex;
    align-items: center;
    gap: 5px;
    padding: 2px 6px;
    font-size: 11px;
    border-radius: 4px;
    transition: background 0.1s;
  }
  .compact-row:hover { background: var(--bg-hover); }
  .compact-row-ok { opacity: 0.5; }
  .compact-dot { font-size: 9px; flex-shrink: 0; width: 10px; text-align: center; }
  .compact-repo-name {
    flex: 1;
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
    font-weight: 500;
  }
  .compact-badge {
    font-size: 8px;
    font-weight: 600;
    white-space: nowrap;
    padding: 0 4px;
    border-radius: 4px;
    background: var(--bg-card);
  }

  .compact-actions {
    display: flex;
    gap: 6px;
    align-items: center;
  }
  .compact-action-btn {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-dim);
    border-radius: 6px;
    font-size: 10px;
    cursor: pointer;
    transition: all 0.12s;
    padding: 4px 8px;
  }
  .compact-action-btn:hover { color: var(--text-primary); border-color: #4B95E9; }
  .compact-full-btn { flex: 1; text-align: center; }

  /* ── Mirror section ── */
  .mirror-list { padding: 12px 0 0; }
  .mirror-section-header {
    display: flex; justify-content: space-between; align-items: center;
    margin-bottom: 8px; padding-bottom: 6px; border-bottom: 1px solid var(--border);
  }
  .mirror-section-header h3 { margin: 0; font-size: 13px; font-weight: 600; color: var(--text-primary); }
  .mirror-section-actions { display: flex; gap: 6px; }
  .mirror-group { margin-bottom: 12px; }
  .mirror-group-header {
    display: flex; justify-content: space-between; align-items: center;
    padding: 4px 0; font-size: 12px; color: var(--text-muted);
  }
  .mirror-accounts { font-weight: 500; color: var(--text-primary); }
  .mirror-arrow { color: var(--text-muted); margin: 0 2px; }
  .mirror-group-actions { display: flex; gap: 4px; align-items: center; }
  .mirror-row {
    display: flex; align-items: center; gap: 8px;
    padding: 3px 0 3px 8px; font-size: 12px;
  }
  .mirror-row:hover { background: var(--hover); border-radius: 4px; }
  .mirror-dot { font-size: 11px; flex-shrink: 0; width: 14px; text-align: center; }
  .mirror-repo-name { font-weight: 500; color: var(--text-primary); min-width: 120px; }
  .mirror-direction { font-size: 11px; flex-shrink: 0; }
  :global(.mir-origin) { color: #61fd5f; font-weight: 500; }
  :global(.mir-backup) { color: #F07623; font-weight: 500; }
  :global(.mir-arrow) { font-size: 14px; font-weight: 700; color: #F07623; vertical-align: middle; }
  :global([data-theme="light"]) :global(.mir-origin) { color: #166534; }
  :global([data-theme="light"]) :global(.mir-backup) { color: #c2410c; }
  :global([data-theme="light"]) :global(.mir-arrow) { color: #c2410c; }
  .mirror-status-text { font-size: 11px; margin-left: auto; font-family: var(--font-mono, monospace); }
  .mirror-warning { color: #F07623; font-size: 13px; cursor: help; }
  .mirror-empty { font-size: 11px; color: var(--text-muted); padding: 4px 8px; font-style: italic; }
  .mirror-checking { display: inline-flex; align-items: center; }
  .mirror-instructions { font-size: 11px; white-space: pre-wrap; background: var(--bg-raised); padding: 8px; border-radius: 4px; margin-top: 8px; overflow: auto; max-height: 200px; }
  .btn-setup { font-size: 10px; padding: 1px 6px; }
  .btn-fix { font-size: 10px; padding: 1px 6px; background: #431407; border-color: #c2410c; color: #fdba74; }
  .spinner-sm { width: 12px; height: 12px; border: 2px solid var(--border); border-top-color: var(--text-primary); border-radius: 50%; animation: spin 0.6s linear infinite; }
  .radio-group { display: flex; flex-direction: column; gap: 4px; }
  .radio-group label { font-size: 12px; color: var(--text); display: flex; align-items: center; gap: 4px; cursor: pointer; }

  /* Mirror repo modal */
  .modal-mirror-repo { max-width: 560px; width: 90vw; }
  .mirror-form-grid {
    display: grid; grid-template-columns: 160px 1fr; gap: 10px 12px; align-items: start;
  }
  .mirror-form-label {
    font-size: 12px; font-weight: 500; color: var(--text-muted);
    padding-top: 3px; white-space: nowrap;
  }
  .form-input-sm { font-size: 11px; padding: 4px 8px; margin-bottom: 6px; }
  .mirror-repo-picker {
    max-height: 220px; overflow-y: auto; border: 1px solid var(--border); border-radius: 4px;
  }
  .mrp-row {
    display: flex; align-items: center; gap: 8px;
    padding: 4px 8px; font-size: 11px; cursor: pointer;
    border-bottom: 1px solid var(--border);
  }
  .mrp-row:last-child { border-bottom: none; }
  .mrp-row:hover { background: var(--hover); }
  .mrp-selected { background: var(--hover); outline: 1px solid var(--text-primary); outline-offset: -1px; }
  .mrp-disabled { opacity: 0.4; cursor: default; }
  .mrp-name { flex: 1; color: var(--text-primary); font-weight: 500; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .mrp-badges { display: flex; gap: 4px; flex-shrink: 0; }
  .mrp-badge {
    font-size: 9px; padding: 1px 4px; border-radius: 3px;
    background: var(--bg-raised); color: var(--text-muted); white-space: nowrap;
  }
  .mrp-badge-dim { opacity: 0.6; }
  .mrp-empty { color: var(--text-muted); font-size: 11px; padding: 8px; margin: 0; }

  /* Compact mirror pill */
  .compact-mirror-pill {
    display: flex; align-items: center; gap: 4px;
    padding: 2px 8px; font-size: 11px; color: var(--text-muted);
  }
  .compact-mirror-dot { font-size: 10px; }
  .compact-mirror-label { font-weight: 500; }
</style>
