<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { fade, slide } from 'svelte/transition';
  import { bridge, events } from './lib/bridge';
  import {
    configStore, accounts, sources, repoStates, summary,
    accountStats, themeStore, applyStatusResults
  } from './lib/stores';
  import { statusColor, credColor, statusLabel, providerLabel, statusSymbol } from './lib/theme';
  import { WindowSetSize, WindowSetMinSize, WindowGetSize } from '../wailsjs/runtime/runtime';
  import type { RepoState, DiscoverResult } from './lib/types';

  // ── View mode ──
  let viewMode: 'full' | 'compact' = 'full';
  let compactExpanded: Record<string, boolean> = {};
  let savedFullSize: { w: number; h: number } | null = null;

  async function toggleViewMode() {
    // SetViewMode saves current position to the slot we're leaving,
    // then persists the new mode.
    if (viewMode === 'full') {
      const size = await WindowGetSize();
      savedFullSize = { w: size.w, h: size.h };
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
      setTimeout(() => WindowSetSize(target.w, target.h), 50);
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

  function openDiscover(accountKey: string) {
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
  let editAcct = { key: '', provider: '', url: '', username: '', name: '', email: '', defaultBranch: '' };

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
      defaultBranch: acct.default_branch || 'main',
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
  let addAcct = { key: '', provider: 'github', url: 'https://github.com', username: '', name: '', email: '', defaultBranch: 'main', credentialType: 'gcm' };

  // Credential setup state (shown after account is created)
  let credSetupBusy = false;
  let credSetupResult: { ok: boolean; message: string } | null = null;
  let credTokenInput = '';
  let credTokenGuide = '';

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

  function resetAddAccount() {
    addAcct = { key: '', provider: 'github', url: 'https://github.com', username: '', name: '', email: '', defaultBranch: 'main', credentialType: 'gcm' };
    addAccountError = '';
    addAccountStep = 'form';
    addAccountModal = false;
    credSetupBusy = false;
    credSetupResult = null;
    credTokenInput = '';
    credTokenGuide = '';
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
      // Move to credential setup step.
      addAccountStep = 'credential';
      credSetupResult = null;
      // Auto-run for GCM and SSH; token needs user input first.
      if (addAcct.credentialType === 'gcm' || addAcct.credentialType === 'ssh') {
        runCredentialSetup();
      } else if (addAcct.credentialType === 'token') {
        const guide = await bridge.getTokenGuide(addAcct.key);
        credTokenGuide = guide.creationURL || '';
      }
    } catch (err: any) {
      addAccountError = err?.message || String(err);
    }
  }

  async function runCredentialSetup() {
    credSetupBusy = true;
    credSetupResult = null;
    try {
      if (addAcct.credentialType === 'gcm') {
        credSetupResult = await bridge.credentialSetupGCM(addAcct.key);
      } else if (addAcct.credentialType === 'token') {
        credSetupResult = await bridge.credentialStoreToken(addAcct.key, credTokenInput);
      } else if (addAcct.credentialType === 'ssh') {
        credSetupResult = await bridge.credentialSetupSSH(addAcct.key);
      }
    } catch (err: any) {
      credSetupResult = { ok: false, message: err?.message || String(err) };
    }
    credSetupBusy = false;
    // Refresh credential status on cards.
    if (credSetupResult?.ok) {
      bridge.credentialVerify(addAcct.key).then((cs) => {
        credStatuses[addAcct.key] = cs.status;
        credStatuses = credStatuses;
      });
    }
  }

  // ── Change credential type ──
  let credChangeModal: string | null = null; // account key
  let credChangeType = '';
  let credChangeBusy = false;
  let credChangeResult: { ok: boolean; message: string } | null = null;
  let credChangeTokenInput = '';
  let credChangeTokenGuide = '';
  let credDeleteBusy = false;

  function openCredChange(accountKey: string, currentType: string) {
    credChangeModal = accountKey;
    credChangeType = currentType;
    credChangeBusy = false;
    credChangeResult = null;
    credChangeTokenInput = '';
    credChangeTokenGuide = '';
    credDeleteBusy = false;
  }

  async function deleteCredential() {
    if (!credChangeModal) return;
    const key = credChangeModal;
    credDeleteBusy = true;
    try {
      await bridge.credentialDelete(key);
      await reloadFromDisk();
      credStatuses[key] = 'none';
      credStatuses = credStatuses;
    } catch (err: any) {
      credChangeResult = { ok: false, message: err?.message || String(err) };
      credDeleteBusy = false;
      return;
    }
    credDeleteBusy = false;
    credChangeModal = null;
  }

  function closeCredChange() {
    credChangeModal = null;
    credChangeResult = null;
  }

  async function applyCredChange() {
    if (!credChangeModal) return;
    credChangeBusy = true;
    credChangeResult = null;
    try {
      await bridge.changeCredentialType(credChangeModal, credChangeType);
      await reloadFromDisk();
      // Auto-run setup for GCM/SSH.
      if (credChangeType === 'gcm') {
        credChangeResult = await bridge.credentialSetupGCM(credChangeModal);
      } else if (credChangeType === 'ssh') {
        credChangeResult = await bridge.credentialSetupSSH(credChangeModal);
      } else if (credChangeType === 'token') {
        const guide = await bridge.getTokenGuide(credChangeModal);
        credChangeTokenGuide = guide.creationURL || '';
        credChangeBusy = false;
        return; // Wait for user to paste token.
      }
    } catch (err: any) {
      credChangeResult = { ok: false, message: err?.message || String(err) };
    }
    credChangeBusy = false;
    if (credChangeResult?.ok && credChangeModal) {
      bridge.credentialVerify(credChangeModal).then((cs) => {
        credStatuses[credChangeModal!] = cs.status;
        credStatuses = credStatuses;
      });
    }
  }

  async function storeCredChangeToken() {
    if (!credChangeModal) return;
    credChangeBusy = true;
    try {
      credChangeResult = await bridge.credentialStoreToken(credChangeModal, credChangeTokenInput);
    } catch (err: any) {
      credChangeResult = { ok: false, message: err?.message || String(err) };
    }
    credChangeBusy = false;
    if (credChangeResult?.ok && credChangeModal) {
      bridge.credentialVerify(credChangeModal).then((cs) => {
        credStatuses[credChangeModal!] = cs.status;
        credStatuses = credStatuses;
      });
    }
  }

  // ── Delete account ──
  let deleteAcctConfirm: string | null = null; // account key
  let deleteAcctStep = 0;
  let deleteAcctBusy = false;

  function askDeleteAccount(accountKey: string) {
    deleteAcctConfirm = accountKey;
    deleteAcctStep = 1;
  }

  function cancelDeleteAccount() {
    deleteAcctConfirm = null;
    deleteAcctStep = 0;
  }

  async function confirmDeleteAccount() {
    if (!deleteAcctConfirm) return;
    if (deleteAcctStep === 1) { deleteAcctStep = 2; return; }
    deleteAcctBusy = true;
    try {
      await bridge.deleteAccount(deleteAcctConfirm);
      await reloadFromDisk();
    } finally {
      deleteAcctBusy = false;
      deleteAcctConfirm = null;
      deleteAcctStep = 0;
      deleteMode = false;
    }
  }

  // ── Settings ──
  let showSettings = false;
  let configPath = '';
  let appVersion = '';

  // ── Periodic fetch ──
  let fetchInterval: string = 'off';
  let fetchTimerId: ReturnType<typeof setInterval> | null = null;
  let lastFetchTime: string = '';
  let fetchingAll = false;

  function applyFetchInterval(val: string) {
    fetchInterval = val;
    if (fetchTimerId) { clearInterval(fetchTimerId); fetchTimerId = null; }
    const minutes = val === '5m' ? 5 : val === '15m' ? 15 : val === '30m' ? 30 : 0;
    if (minutes > 0) {
      fetchTimerId = setInterval(() => { runFetchAll(); verifyAllCredentials(); }, minutes * 60 * 1000);
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
  let credStatuses: Record<string, string> = {};

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

    // Check for global ~/.gitconfig identity (warns user if set).
    checkGlobalIdentity();

    // Start periodic fetch if previously configured.
    if (fetchInterval !== 'off') applyFetchInterval(fetchInterval);
  }

  function verifyAllCredentials() {
    for (const key of Object.keys($accounts)) {
      bridge.credentialVerify(key).then((cs) => {
        credStatuses[key] = cs.status;
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
    });

    events.on('discover:done', (data: any) => {
      discoverLoading = false;
      if (data.error) {
        discoverRepos = [];
        return;
      }
      discoverRepos = (data.repos || []).sort((a: DiscoverResult, b: DiscoverResult) => a.fullName.localeCompare(b.fullName));
      discoverSelected = {};
    });

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
    <p class="onboard-desc">Choose where to store your repositories</p>
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
      <button class="compact-acct" class:compact-acct-expanded={compactExpanded[key]} on:click={() => toggleCompactAcct(key)}>
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
        <span class="settings-label">Periodic fetch</span>
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
      <div class="settings-row">
        <span class="settings-label">Version</span>
        <span class="settings-value">{appVersion}</span>
      </div>
    </div>
  {/if}

  <!-- ── ACCOUNT CARDS ── -->
  <section class="cards-row">
    {#each Object.entries($accounts) as [key, acct]}
      {@const stats = $accountStats[key] || { total: 0, synced: 0, issues: 0 }}
      {@const cred = credStatuses[key] || 'unknown'}
      <div class="card" class:card-delete-mode={deleteMode}
        style={cred === 'none' || cred === 'error' ? `background: ${resolvedTheme === 'light' ? '#fef2f2' : '#2a1215'}` : ''}>
        <div class="card-top">
          {#if deleteMode}
            <button class="btn-delete-x card-delete-btn" on:click={() => askDeleteAccount(key)} title="Delete account {key}">&#10005;</button>
          {:else}
            <span class="card-dot" style="background: {cc(cred)}"></span>
          {/if}
          <span class="card-provider">{providerLabel(acct.provider)}</span>
          <button class="cred-badge cred-badge-{cred === 'ok' ? 'ok' : cred === 'error' ? 'err' : cred === 'warning' ? 'warn' : cred === 'none' ? 'none' : cred === 'unknown' ? 'pending' : ''}"
            on:click={() => openCredChange(key, acct.default_credential_type || 'gcm')}
            title="Credential: {acct.default_credential_type || 'none'}">{cred === 'none' ? 'config' : acct.default_credential_type || 'gcm'}</button>
        </div>
        <div class="card-name card-name-edit" on:click={() => openEditAccount(key)} title="Edit account">{key.replace(/^(github|git)-/, '')}</div>
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
        <button class="card-btn" on:click={() => openDiscover(key)} disabled={cred === 'none' || cred === 'error'}>Find projects</button>
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

  <!-- ── SUMMARY FOOTER ── -->
  <footer class="summary">
    <span class="sum" style="color:{sc('clean')}">{$summary.clean} synced</span>
    {#if $summary.syncing > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('syncing')}">{$summary.syncing} syncing</span>{/if}
    {#if $summary.behind > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('behind')}">{$summary.behind} behind</span>{/if}
    {#if $summary.dirty > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('dirty')}">{$summary.dirty} local changes</span>{/if}
    {#if $summary.notCloned > 0}<span class="sep">&middot;</span><span class="sum" style="color:{sc('not cloned')}">{$summary.notCloned} not local</span>{/if}
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
          <div class="form-row">
            <label class="form-label" for="ea-branch">Default branch</label>
            <input class="form-input" id="ea-branch" bind:value={editAcct.defaultBranch} />
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
    <div class="overlay" on:click={resetAddAccount} transition:fade={{ duration: 120 }}>
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
            <div class="form-row">
              <label class="form-label" for="aa-branch">Default branch</label>
              <input class="form-input" id="aa-branch" bind:value={addAcct.defaultBranch} placeholder="main" />
            </div>
          {:else}
            <!-- ── Step 2: Credential setup ── -->
            <p class="cred-step-intro">Account <strong>{addAcct.key}</strong> created. Set up credentials:</p>

            {#if addAcct.credentialType === 'gcm'}
              <p class="cred-step-desc">Click below to authenticate via browser (Git Credential Manager).</p>
              <button class="btn-add cred-action-btn" on:click={runCredentialSetup} disabled={credSetupBusy}>
                {credSetupBusy ? 'Authenticating...' : 'Authenticate with GCM'}
              </button>

            {:else if addAcct.credentialType === 'token'}
              <p class="cred-step-desc">Create a Personal Access Token at your provider and paste it below.</p>
              {#if credTokenGuide}
                <p class="cred-step-link"><a href={credTokenGuide} target="_blank" rel="noopener">{credTokenGuide}</a></p>
              {/if}
              <div class="form-row">
                <label class="form-label" for="aa-token">Token</label>
                <input class="form-input" id="aa-token" type="password" bind:value={credTokenInput} placeholder="ghp_..." />
              </div>
              <button class="btn-add cred-action-btn" on:click={runCredentialSetup} disabled={credSetupBusy || !credTokenInput.trim()}>
                {credSetupBusy ? 'Storing...' : 'Store token'}
              </button>

            {:else if addAcct.credentialType === 'ssh'}
              <p class="cred-step-desc">This will generate an SSH key and configure ~/.ssh/config.</p>
              <button class="btn-add cred-action-btn" on:click={runCredentialSetup} disabled={credSetupBusy}>
                {credSetupBusy ? 'Setting up SSH...' : 'Generate SSH key'}
              </button>
            {/if}

            {#if credSetupResult}
              <div class="cred-result" class:cred-result-ok={credSetupResult.ok} class:cred-result-err={!credSetupResult.ok}>
                <pre class="cred-result-msg">{credSetupResult.message}</pre>
              </div>
            {/if}
          {/if}
        </div>
        <div class="modal-foot">
          {#if addAccountStep === 'form'}
            <button class="btn-cancel" on:click={resetAddAccount}>Cancel</button>
            <button class="btn-add" on:click={submitAddAccount}>Add account</button>
          {:else}
            <button class="btn-cancel" on:click={resetAddAccount}>
              {credSetupResult?.ok ? 'Done' : 'Skip & close'}
            </button>
          {/if}
        </div>
      </div>
    </div>
  {/if}

  <!-- ── CHANGE CREDENTIAL MODAL ── -->
  {#if credChangeModal}
    {@const currentAcct = $accounts[credChangeModal]}
    <div class="overlay" on:click={closeCredChange} transition:fade={{ duration: 120 }}>
      <div class="modal modal-account" on:click|stopPropagation transition:slide={{ duration: 180 }}>
        <div class="modal-head">
          <h3>Change credential &mdash; {credChangeModal}</h3>
          <button class="btn-x" on:click={closeCredChange}>&#10005;</button>
        </div>
        <div class="modal-body">
          <div class="form-row">
            <label class="form-label" for="cc-type">Type</label>
            <select class="form-input" id="cc-type" bind:value={credChangeType} disabled={credChangeBusy}>
              <option value="gcm">Git Credential Manager (GCM)</option>
              <option value="ssh">SSH</option>
              <option value="token">Token</option>
            </select>
          </div>

          {#if credChangeType === 'token' && credChangeTokenGuide && !credChangeResult}
            <p class="cred-step-link"><a href={credChangeTokenGuide} target="_blank" rel="noopener">{credChangeTokenGuide}</a></p>
            <div class="form-row">
              <label class="form-label" for="cc-token">Token</label>
              <input class="form-input" id="cc-token" type="password" bind:value={credChangeTokenInput} placeholder="ghp_..." />
            </div>
            <button class="btn-add cred-action-btn" on:click={storeCredChangeToken} disabled={credChangeBusy || !credChangeTokenInput.trim()}>
              {credChangeBusy ? 'Storing...' : 'Store token'}
            </button>
          {/if}

          {#if credChangeBusy}
            <div class="loading"><div class="spinner"></div><span>Setting up credential...</span></div>
          {/if}

          {#if credChangeResult}
            <div class="cred-result" class:cred-result-ok={credChangeResult.ok} class:cred-result-err={!credChangeResult.ok}>
              <pre class="cred-result-msg">{credChangeResult.message}</pre>
            </div>
          {/if}
        </div>
        <div class="modal-foot">
          {#if currentAcct?.default_credential_type && !credChangeResult}
            <button class="btn-delete cred-delete-btn" on:click={deleteCredential} disabled={credDeleteBusy || credChangeBusy}>
              {credDeleteBusy ? 'Deleting...' : 'Delete credential'}
            </button>
          {/if}
          {#if credChangeResult}
            <button class="btn-cancel" on:click={closeCredChange}>{credChangeResult.ok ? 'Done' : 'Close'}</button>
          {:else if credChangeType === 'token' && credChangeTokenGuide}
            <button class="btn-cancel" on:click={closeCredChange}>Cancel</button>
          {:else}
            <button class="btn-cancel" on:click={closeCredChange}>Cancel</button>
            <button class="btn-add" on:click={applyCredChange} disabled={credChangeBusy || (credChangeType === (currentAcct?.default_credential_type || '') && credStatuses[credChangeModal || ''] === 'ok')}>
              {credChangeBusy ? 'Setting up...' : 'Change & setup'}
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
          <p class="delete-warning delete-danger"><strong>WARNING:</strong> Changing the clone folder will <strong>not</strong> move existing repos. They will show as "Not local" until re-cloned at the new location (or moved manually).</p>
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
  .cred-badge-pending { background: #27272a; border-color: #52525b; color: #a1a1aa; }
  :global([data-theme="light"]) .cred-badge-pending { background: #f4f4f5; border-color: #a1a1aa; color: #71717a; }
  :global([data-theme="light"]) .cred-badge-ok { background: #dcfce7; border-color: #166534; color: #166534; }
  :global([data-theme="light"]) .cred-badge-err { background: #fee2e2; border-color: #be123c; color: #be123c; }
  :global([data-theme="light"]) .cred-badge-warn { background: #ffedd5; border-color: #c2410c; color: #c2410c; }
  :global([data-theme="light"]) .cred-badge-none { background: #dbeafe; border-color: #2563eb; color: #2563eb; }
  .card-delete-btn { flex-shrink: 0; }
  .card-name { font-size: 14px; font-weight: 600; margin-bottom: 8px; }
  .card-name-edit { cursor: pointer; }
  .card-name-edit:hover { text-decoration: underline; }

  .card-ring-row { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; }
  .mini-ring { width: 28px; height: 28px; flex-shrink: 0; }
  .card-stat { font-size: 12px; font-weight: 600; color: var(--text-repo); }
  .card-issues { font-size: 11px; font-weight: 600; }
  .card-ok { font-size: 11px; font-weight: 600; }

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
  .modal-account { width: 420px; }
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

  /* ── Credential setup step ── */
  .cred-step-intro { font-size: 13px; color: var(--text-primary); margin: 0 0 10px; }
  .cred-step-desc { font-size: 12px; color: var(--text-secondary); margin: 0 0 10px; line-height: 1.5; }
  .cred-step-link { font-size: 11px; margin: 0 0 10px; }
  .cred-step-link a { color: #4B95E9; text-decoration: none; word-break: break-all; }
  .cred-step-link a:hover { text-decoration: underline; }
  .cred-action-btn { margin-top: 4px; margin-bottom: 10px; }
  .cred-result { margin-top: 10px; padding: 8px 10px; border-radius: 6px; font-size: 12px; }
  .cred-result-ok { background: #16653412; border: 1px solid #166534; color: var(--text-primary); }
  .cred-result-err { background: #D81E5B12; border: 1px solid #D81E5B; color: #D81E5B; }
  .cred-result-msg { margin: 0; white-space: pre-wrap; font-family: monospace; font-size: 11px; line-height: 1.5; }

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
</style>
