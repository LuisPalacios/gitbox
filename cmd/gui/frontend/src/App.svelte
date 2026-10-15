<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { bridge, events } from './lib/bridge';
  import type { DoctorPrecheckDTO } from './lib/bridge';
  import {
    configStore, accounts, sources, mirrors, repoStates, themeStore, applyStatusResults,
    applyMirrorStatusResults, prsByAccount, prSettings, applyPRUpdate
  } from './lib/stores';
  import { detailLocked, credBroken } from './lib/status';
  import { languageStore, normalizeLanguage } from './lib/i18n';
  import { WindowSetSize, WindowSetMinSize, WindowGetSize, WindowSetPosition, WindowGetPosition, Quit, EventsOn } from '../wailsjs/runtime/runtime';
  import type { RepoState, CredentialStatus, MirrorSetupResult, MirrorCredentialCheck, EditorInfo, AIHarnessInfo, TerminalAppInfo, ShellInfo, TerminalProfileInfo, PRAccountUpdateDTO, RepoDetailView, GlobalIdentityWarn, GCMWarn, GitignoreWarn, GitignoreInstalled, UpdateInfo, ConfigBackupInfo } from './lib/types';
  import TerminalsModal from './lib/TerminalsModal.svelte';
  import Onboarding from './lib/Onboarding.svelte';
  import ConfigErrorScreen from './lib/components/ConfigErrorScreen.svelte';
  import ReloadErrorBanner from './lib/components/ReloadErrorBanner.svelte';
  import CompactView from './lib/components/CompactView.svelte';
  import TopBar from './lib/components/TopBar.svelte';
  import Banners from './lib/components/Banners.svelte';
  import SettingsPanel from './lib/components/SettingsPanel.svelte';
  import TabBar from './lib/components/TabBar.svelte';
  import AccountCards from './lib/components/AccountCards.svelte';
  import RepoList from './lib/components/RepoList.svelte';
  import MirrorsTab from './lib/components/MirrorsTab.svelte';
  import WorkspacesTab from './lib/components/WorkspacesTab.svelte';
  import Footer from './lib/components/Footer.svelte';
  import DiscoverModal from './lib/modals/DiscoverModal.svelte';
  import DoctorModal from './lib/modals/DoctorModal.svelte';
  import SshTokenModal from './lib/modals/SshTokenModal.svelte';
  import EditAccountModal from './lib/modals/EditAccountModal.svelte';
  import DeleteCloneModal from './lib/modals/DeleteCloneModal.svelte';
  import SweepModal from './lib/modals/SweepModal.svelte';
  import OrphansModal from './lib/modals/OrphansModal.svelte';
  import AddAccountModal from './lib/modals/AddAccountModal.svelte';
  import CredentialModal from './lib/modals/CredentialModal.svelte';
  import DeleteAccountModal from './lib/modals/DeleteAccountModal.svelte';
  import ChangeFolderModal from './lib/modals/ChangeFolderModal.svelte';
  import AddMirrorGroupModal from './lib/modals/AddMirrorGroupModal.svelte';
  import AddMirrorRepoModal from './lib/modals/AddMirrorRepoModal.svelte';
  import DeleteMirrorGroupModal from './lib/modals/DeleteMirrorGroupModal.svelte';
  import DeleteMirrorRepoModal from './lib/modals/DeleteMirrorRepoModal.svelte';
  import MirrorSetupResultModal from './lib/modals/MirrorSetupResultModal.svelte';
  import MirrorDiscoverModal from './lib/modals/MirrorDiscoverModal.svelte';
  import MirrorTokenModal from './lib/modals/MirrorTokenModal.svelte';
  import MoveRepoModal from './lib/modals/MoveRepoModal.svelte';
  import CreateRepoModal from './lib/modals/CreateRepoModal.svelte';
  import { repoTreeRows } from './lib/repo-tree';

  // ── View mode ──
  let viewMode: 'full' | 'compact' = 'full';
  let cardsTab: 'accounts' | 'mirrors' | 'workspaces' = 'accounts';
  let languageChoice: 'en' | 'es' = 'en';

  // ── Experience (issue #124) ──
  // Simple hides git vocabulary and power features for non-developers;
  // Advanced is everything. Persisted as global.ui_mode.
  let uiMode: 'simple' | 'advanced' = 'advanced';
  $: simple = uiMode === 'simple';

  // ── Workspaces (issue #27 / #49) ──
  // UI state for the read-only Workspaces tab.
  let workspaceBusy = false;

  let compactExpanded: Record<string, boolean> = {};
  let savedFullSize: { w: number; h: number } | null = null;
  let savedFullPos: { x: number; y: number } | null = null;

  // A repo row toggles its detail panel once its first scan is in.
  function activateRepoRow(sourceKey: string, repoName: string, status: string) {
    if (status !== 'unknown') toggleRepoDetail(sourceKey, repoName, status);
  }

  // ── Action menu (kebab) ──
  let actionMenuRepo: string | null = null;
  let actionMenuAccount: string | null = null;
  $: configEditors = ($configStore?.global?.editors || []) as EditorInfo[];
  // Harnesses the Go-side sync flagged `missing` (binary uninstalled) stay
  // in config but never reach the launcher menus (issue #81).
  $: configAIHarnesses = (($configStore?.global?.ai_harnesses || []) as AIHarnessInfo[]).filter(h => !h.missing);
  // v2.1 Terminal Profile model (issue #69). configProfiles drives the
  // per-row launcher's terminal section; configApps + configShells are
  // referenced by the Gear-panel TerminalsSection for editing.
  $: configApps = ($configStore?.global?.terminal_apps || []) as TerminalAppInfo[];
  $: configShells = ($configStore?.global?.shells || []) as ShellInfo[];
  $: configProfiles = ($configStore?.global?.terminal_profiles || []) as TerminalProfileInfo[];

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
      const savedState = await bridge.setViewMode('full');
      viewMode = 'full';
      WindowSetMinSize(640, 480);
      await tick();
      const target = savedFullSize
        ?? (savedState ? { w: savedState.width, h: savedState.height } : { w: 900, h: 700 });
      const pos = savedFullPos
        ?? (savedState ? { x: savedState.x, y: savedState.y } : null);
      setTimeout(async () => {
        WindowSetSize(target.w, target.h);
        if (pos) {
          const onScreen = await bridge.isPositionOnScreen(pos.x, pos.y, target.w, target.h);
          if (onScreen) {
            WindowSetPosition(pos.x, pos.y);
          }
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
  let globalIdentityWarn: GlobalIdentityWarn | null = null;

  // ── Global GCM credential-helper warning ──
  // Surfaces when a GCM account exists but ~/.gitconfig has no (or wrong)
  // global `credential.helper` / `credential.credentialStore`. Without those,
  // GCM fills fall through to a TTY prompt and fail with the cryptic
  // "Device not configured" error inside the GUI.
  let globalGCMWarn: GCMWarn | null = null;
  let globalGCMFixing = false;

  // ── Global gitignore warning ──
  // Banner shown when ~/.gitignore_global is missing the recommended block
  // (or core.excludesfile is unset, or duplicates live outside the sentinels).
  // The user can install in one click; gitbox creates a timestamped .bak-…
  // backup of any existing file. Gated by the check_global_gitignore
  // preference — the gear-panel toggle binds to `checkGitignorePref`.
  let gitignoreWarn: GitignoreWarn | null = null;
  let gitignoreInstalling = false;
  let gitignoreInstalled: GitignoreInstalled | null = null;
  let gitignoreError = '';
  let checkGitignorePref = true;

  // ── Update state ──
  let updateInfo: UpdateInfo | null = null;
  let updateApplying = false;
  let updateProgress = '';
  let updateDone = false;
  let updateError = '';
  // Version whose pill the user dismissed: hidden for this session only,
  // while the periodic re-check keeps reporting it.
  let updateDismissed = '';

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

  async function checkGlobalGCM() {
    try {
      const needed = await bridge.isGlobalGCMConfigNeeded();
      if (!needed) { globalGCMWarn = null; return; }
      const s = await bridge.checkGlobalGCMConfig();
      globalGCMWarn = s.needsFix ? s as GCMWarn : null;
    } catch { globalGCMWarn = null; }
  }

  async function fixGlobalGCM() {
    if (globalGCMFixing) return;
    globalGCMFixing = true;
    try {
      await bridge.fixGlobalGCMConfig();
      globalGCMWarn = null;
    } catch (e: any) {
      console.error('Failed to fix global GCM config:', e);
    } finally {
      globalGCMFixing = false;
    }
  }

  async function loadCheckGitignorePref() {
    try {
      checkGitignorePref = await bridge.getCheckGlobalGitignore();
    } catch { checkGitignorePref = true; }
  }

  async function setCheckGitignorePref(enabled: boolean) {
    try {
      await bridge.setCheckGlobalGitignore(enabled);
      checkGitignorePref = enabled;
      if (enabled) {
        // Re-run the check immediately so the user sees the result without
        // having to restart the app.
        checkGlobalGitignore();
      } else {
        // Dismiss any currently-shown banner when opting out.
        gitignoreWarn = null;
      }
    } catch (e: any) {
      console.error('Failed to save check_global_gitignore preference:', e);
    }
  }

  async function checkGlobalGitignore() {
    try {
      const s = await bridge.checkGlobalGitignore();
      gitignoreWarn = s.needsAction ? s : null;
    } catch (e: any) {
      console.error('Failed to check global gitignore:', e);
      gitignoreWarn = null;
    }
  }

  async function installGlobalGitignore() {
    gitignoreInstalling = true;
    gitignoreError = '';
    try {
      const res = await bridge.installGlobalGitignore();
      gitignoreInstalled = {
        backupPath: res.backupPath,
        setExcludesfile: res.setExcludesfile,
        alreadyUpToDate: res.alreadyUpToDate,
      };
      // Re-check so the banner reflects the post-install state.
      await checkGlobalGitignore();
    } catch (e: any) {
      console.error('Failed to install global gitignore:', e);
      gitignoreError = String(e);
    } finally {
      gitignoreInstalling = false;
    }
  }

  // ── Onboarding ──
  let firstRun = false;
  // The dashboard loads the language, but onboarding shows before it does.
  $: if (firstRun) bridge.getLanguage().then((l) => languageStore.set(normalizeLanguage(l))).catch(() => {});

  // ── Config-load-error screen (issue #60, smoke-test feedback) ──
  // Rendered when Startup detected a config file that existed but failed to
  // parse. Four recovery actions:
  //   • Restore from a dated backup under ~/.config/gitbox/ (primary path)
  //   • Auto-repair — drop dangling mirror/workspace references in place
  //   • Start fresh — walk through onboarding, overwriting the broken file
  //   • Exit — close GitboxApp so the user can fix the file manually
  //
  // Rendered as a full-screen Svelte view (NOT a window.confirm) because
  // macOS WebKit was observed to auto-dismiss confirm() without showing a
  // dialog, causing the app to flash and exit on corrupted configs.
  let cfgLoadErrorModal = false;
  let cfgLoadErrorMsg = '';
  let cfgLoadErrorBusy = false;
  let cfgRepairFailure = '';
  let cfgBackups: ConfigBackupInfo[] = [];

  async function cfgLoadBackups() {
    try {
      cfgBackups = await bridge.listConfigBackups();
    } catch (err) {
      console.error('listConfigBackups failed', err);
      cfgBackups = [];
    }
  }

  async function cfgRestoreBackup(path: string) {
    cfgLoadErrorBusy = true;
    cfgRepairFailure = '';
    try {
      const res = await bridge.restoreFromBackup(path);
      if (!res.success) {
        cfgRepairFailure = res.error || 'unknown error';
        await cfgLoadBackups(); // the failed restore may have altered backup count
        return;
      }
      cfgLoadErrorModal = false;
      cfgLoadErrorMsg = '';
      firstRun = await bridge.isFirstRun();
      if (firstRun) return;
      await initDashboard();
    } catch (err: any) {
      cfgRepairFailure = err?.message || String(err);
    } finally {
      cfgLoadErrorBusy = false;
    }
  }

  async function cfgDoRepair() {
    cfgLoadErrorBusy = true;
    cfgRepairFailure = '';
    try {
      const res = await bridge.repairConfig();
      if (!res.success) {
        cfgRepairFailure = res.error || 'unknown error';
        await cfgLoadBackups();
        return;
      }
      cfgLoadErrorModal = false;
      cfgLoadErrorMsg = '';
      firstRun = await bridge.isFirstRun();
      if (firstRun) return;
      await initDashboard();
    } catch (err: any) {
      cfgRepairFailure = err?.message || String(err);
    } finally {
      cfgLoadErrorBusy = false;
    }
  }

  async function cfgStartFresh() {
    // Acknowledge the corruption so SetGlobalFolder no longer refuses the
    // first save. The dated backup in ~/.config/gitbox/ preserves the
    // pre-overwrite copy, so the user can recover manually if needed.
    cfgLoadErrorBusy = true;
    try {
      await bridge.acknowledgeConfigError();
    } finally {
      cfgLoadErrorBusy = false;
    }
    cfgLoadErrorModal = false;
    cfgLoadErrorMsg = '';
    cfgRepairFailure = '';
    firstRun = true;
  }

  async function finishOnboarding(result: { sourceKey: string; repos: string[] } | null) {
    firstRun = false;
    await initDashboard();
    for (const repo of result?.repos || []) cloneRepo(result!.sourceKey, repo);
  }

  // Simple mode's "Add account" runs the connect + pick steps of the same
  // guided setup; Advanced keeps the full form.
  let connectModal = false;
  async function finishConnect(result: { sourceKey: string; repos: string[] } | null) {
    connectModal = false;
    await reloadFromDisk();
    for (const repo of result?.repos || []) cloneRepo(result!.sourceKey, repo);
  }

  // Re-run the nested-clone scan for a container and open the orphans modal so
  // newly cloned siblings (or ones surfaced by a deeper scan) can be adopted.
  async function rescanContainer() {
    await loadOrphans();
    if (orphanList.some(o => o.matchedAccount && !o.localOnly)) {
      showOrphanModal();
    } else {
      alert('No new nested clones found to onboard.');
    }
  }

  // ── Change folder (settings) ──
  let changeFolderDialog: ChangeFolderModal;
  const openChangeFolder = () => changeFolderDialog.openChangeFolder();

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

  // ── Action menu (kebab) ──
  function toggleActionMenu(repoKey: string) {
    actionMenuRepo = actionMenuRepo === repoKey ? null : repoKey;
  }

  function closeActionMenu() {
    actionMenuRepo = null;
  }

  // ── Account-level actions (kebab on account card) ──
  function toggleAccountMenu(accountKey: string) {
    actionMenuAccount = actionMenuAccount === accountKey ? null : accountKey;
    actionMenuRepo = null;
  }

  function closeAccountMenu() {
    actionMenuAccount = null;
  }

  // saveTerminalProfilesAndReload pipes the TerminalsSection draft back to
  // disk and reloads the in-memory config so every consumer (kebab menus,
  // detail views) sees the new flags on the next tick.
  async function saveTerminalProfilesAndReload(apps: TerminalAppInfo[], shells: ShellInfo[], profiles: TerminalProfileInfo[]) {
    await bridge.saveTerminalProfiles(apps, shells, profiles);
    const fresh = await bridge.reloadConfig();
    configStore.set(fresh);
  }

  async function reloadConfigFromDisk() {
    await bridge.reloadConfig();
    const snapshot = await bridge.getStartupSnapshot();
    repoNesting = snapshot.nesting || {};
    configStore.set(snapshot.config);
  }

  // openTerminalsManagerWindow asks the Go side to spawn a sub-process of
  // gitbox.exe with --terminals-window, which renders the standalone
  // Profile editor in its own OS window. Fire-and-forget; the parent
  // window stays interactive and refreshes config on focus when the user
  // clicks back here.
  let terminalsManagerOpening = false;
  let terminalsManagerHasOpened = false;
  async function openTerminalsManagerWindow() {
    if (terminalsManagerOpening) return;
    terminalsManagerOpening = true;
    // Cold-start of a Wails subprocess + WebView2/WebKitGTK + first paint
    // takes ~1.5s on Win/Linux. Subsequent re-opens go through
    // HideWindowOnClose + SingleInstanceLock and appear instantly, so
    // we cap the "Opening…" affordance shorter once we know the
    // subprocess is already alive (issue #69 user feedback).
    const cooldown = terminalsManagerHasOpened ? 250 : 1500;
    try {
      await bridge.openTerminalsManagerWindow();
      terminalsManagerHasOpened = true;
    } catch (e: any) {
      await bridge.showErrorDialog('Open Terminals Manager', (e?.message || String(e)));
    } finally {
      setTimeout(() => { terminalsManagerOpening = false; }, cooldown);
    }
  }

  // revealLabel is the OS-aware "Reveal in <file manager>" string passed to
  // every LauncherMenu instance. Computed once from hostOS so the kebabs
  // don't each re-derive it.
  $: revealLabel = hostOS === 'windows' ? 'Reveal in Explorer'
    : hostOS === 'darwin' ? 'Reveal in Finder'
    : 'Reveal in files';

  // ── Orphan adoption ──
  let orphanList: import('./lib/types').OrphanRepoDTO[] = [];
  let orphanCount = 0;
  let orphansDialog: OrphansModal;
  const showOrphanModal = () => orphansDialog.showOrphanModal();

  async function loadOrphans() {
    await loadNesting();
    try {
      const result = await bridge.findOrphans();
      orphanList = result || [];
      orphanCount = orphanList.filter(o => o.matchedAccount && !o.localOnly).length;
    } catch { orphanCount = 0; }
    await loadTentativeContainers();
  }

  // ── Nested-clone display (container repos) ──
  // child "source/repo" → parent "source/repo", derived from resolved paths.
  let repoNesting: Record<string, string> = {};

  async function loadNesting() {
    try {
      repoNesting = await bridge.repoNesting();
    } catch { /* I retain the last known nesting when a refresh fails. */ }
  }

  // ── Tentative containers ──
  // Managed repos that hold a .code-workspace at their root but are not yet
  // flagged as containers. We surface an inline "onboard nested clones" hint on
  // their row so the user can flag them (which then scans + opens the adopt
  // modal). Keyed by "source/repo" for O(1) lookup during row rendering.
  let tentativeContainers: Set<string> = new Set();

  async function loadTentativeContainers() {
    try {
      const refs = await bridge.tentativeContainers();
      tentativeContainers = new Set((refs || []).map(r => `${r.source}/${r.repo}`));
    } catch { tentativeContainers = new Set(); }
  }

  // ── Collapse / expand (issue #117) ──
  // Folded account groups (source keys) and folded parent clones
  // ("source/repo"), persisted in global.collapsed. A clone is collapsible
  // because it has clones below it, whatever made them its children.
  let collapsedSources: Set<string> = new Set();
  let collapsedRepos: Set<string> = new Set();

  function saveCollapsed() {
    bridge.setCollapsed({ sources: [...collapsedSources], repos: [...collapsedRepos], cards_open: cardsOpen })
      .catch((e) => console.error('saving collapse state failed', e));
  }

  // ── Cards panel (issue #123) ──
  // The card row under the tab bar starts collapsed and remembers its state
  // (global.collapsed.cards_open). It also opens on its own, without saving,
  // while delete mode needs the cards' ✕ buttons or an account needs a look
  // (no accounts yet, or a missing / broken credential). Collapsing it then
  // dismisses that alert for the rest of the session.
  let cardsOpen = false;
  let cardsAlertDismissed = false;
  // Inside the panel every card starts as a chip (dot + name). Clicking one
  // opens it into the full two-line card; clicking outside it or Escape
  // folds it back. One open card at a time, keyed "acct:", "mir:" or "ws:".
  let expandedCard: string | null = null;
  // cardsReady holds the alert back until the config and collapse state are
  // loaded, so the panel doesn't flash open while $accounts is still empty.
  let cardsReady = false;
  $: cardsAlert = cardsReady && (Object.keys($accounts).length === 0 ||
    Object.keys($accounts).some((k) => credBroken(credStatuses[k]?.status)));
  $: cardsVisible = cardsOpen || deleteMode || (cardsAlert && !cardsAlertDismissed);

  function toggleCards() {
    if (cardsVisible) {
      cardsOpen = false;
      cardsAlertDismissed = true;
    } else {
      cardsOpen = true;
    }
    saveCollapsed();
  }

  // selectCardsTab switches tabs; clicking the active tab folds or unfolds
  // the panel, and switching to another tab always shows its cards.
  function selectCardsTab(tab: typeof cardsTab) {
    if (tab === cardsTab) {
      toggleCards();
      return;
    }
    cardsTab = tab;
    if (!cardsVisible) toggleCards();
  }

  // closeDetailIf drops the open detail panel when a collapse hides its row.
  function closeDetailIf(hidden: (key: string) => boolean) {
    if (expandedRepo && !detailLoading && hidden(expandedRepo)) {
      expandedRepo = null;
      repoDetail = null;
    }
  }

  function toggleSourceCollapsed(sourceKey: string) {
    if (collapsedSources.has(sourceKey)) {
      collapsedSources.delete(sourceKey);
    } else {
      collapsedSources.add(sourceKey);
      const hidden = new Set((treeRows[sourceKey] || []).flatMap(row => [row.key, ...row.descendants]));
      closeDetailIf(k => hidden.has(k));
    }
    collapsedSources = collapsedSources;
    saveCollapsed();
  }

  function toggleRepoCollapsed(repoKey: string, descendants: string[]) {
    if (collapsedRepos.has(repoKey)) {
      collapsedRepos.delete(repoKey);
    } else {
      collapsedRepos.add(repoKey);
      const hidden = new Set(descendants);
      closeDetailIf(k => hidden.has(k));
    }
    collapsedRepos = collapsedRepos;
    saveCollapsed();
  }

  $: treeRows = repoTreeRows($sources, repoNesting, collapsedRepos);

  // ── Sweep branches modal ──
  let sweepDialog: SweepModal;
  const sweepBranches = (sourceKey: string, repoKey: string) => sweepDialog.sweepBranches(sourceKey, repoKey);

  // ── Repo detail panel ──
  let expandedRepo: string | null = null;
  let repoDetail: RepoDetailView | null = null;
  let detailLoading = false;

  // Auto-collapse expanded detail when the repo no longer needs attention.
  $: if (expandedRepo) {
    const st = $repoStates[expandedRepo];
    if (st && detailLocked(st.status)) {
      expandedRepo = null;
      repoDetail = null;
    }
  }

  async function toggleRepoDetail(sourceKey: string, repoName: string, status: string) {
    // Only expand for repos that need attention (not clean, behind, not cloned, or in-progress)
    if (detailLocked(status)) return;
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

  // ── Discovery ──
  let discoverDialog: DiscoverModal;
  let sshTokenDialog: SshTokenModal;

  function openDiscover(accountKey: string) {
    const acct = $accounts[accountKey];
    // SSH accounts need a PAT for API discovery. Check if one exists by
    // attempting discovery — if it fails, the discover:done event will show
    // the error. Instead, proactively check: if SSH and credential is ok but
    // we know API access needs a token, show the token modal.
    if (acct?.default_credential_type === 'ssh') {
      // Try discovery — if it fails with "no API token", the event handler
      // will open the token modal. This avoids a redundant backend call.
      sshTokenDialog.close();
    }
    discoverDialog.open(accountKey);
  }

  // Banner surfaced when reloadFromDisk() or a mutation fails — usually a
  // symptom of on-disk config drift (e.g. a dangling reference left by a
  // prior bug). Cleared the next time reload succeeds.
  let reloadError = '';
  let reloadGeneration = 0;
  let statusGeneration = 0;

  // Re-read config from disk and refresh all repo statuses. Throws on
  // failure; callers catch and populate reloadError so the UI does not
  // silently swallow integrity issues.
  //
  // No-op while the config-load-error modal is up — the on-disk file is
  // known-broken and reloading it would just re-surface the same error
  // as a banner on top of the modal. The user's choice (Repair / Start
  // fresh / Exit) is what drives recovery, not ambient focus events.
  // Same during first-run onboarding: until the folder step saves, there is
  // no file to read, and a focus event would leave a stale "open …: no such
  // file" banner behind on the dashboard.
  async function reloadFromDisk() {
    if (cfgLoadErrorModal || firstRun) return;
    const generation = ++reloadGeneration;
    try {
      await bridge.reloadConfig();
      const snapshot = await bridge.getStartupSnapshot();
      if (generation !== reloadGeneration) return;
      repoNesting = snapshot.nesting || {};
      configStore.set(snapshot.config);
      configPath = snapshot.configPath;
      const saved = snapshot.config.global.periodic_sync || 'off';
      if (saved !== fetchInterval) applyFetchInterval(saved);
      if (!fetchingAll) {
        const statusVersion = statusGeneration;
        const statuses = await bridge.getAllStatus();
        if (generation !== reloadGeneration) return;
        if (statusVersion === statusGeneration && !fetchingAll) applyStatusResults(statuses);
        if (!fetchingAll) verifyAllCredentials();
      }
      reloadError = '';
    } catch (err: any) {
      if (generation === reloadGeneration) reloadError = err?.message || String(err);
      throw err;
    }
  }

  // ── Delete mode ──
  let deleteMode = false;
  let deleteCloneDialog: DeleteCloneModal;
  const askDelete = (sourceKey: string, repoKey: string, status: string) => deleteCloneDialog.askDelete(sourceKey, repoKey, status);

  // ── Edit account ──
  let editAccountDialog: EditAccountModal;
  const openEditAccount = (key: string) => editAccountDialog.openEditAccount(key);

  // ── Add account ──
  let addAccountDialog: AddAccountModal;

  // Shared credential operation state — used by BOTH add-account and change-credential flows.
  // Only one modal can be active at a time, so sharing is safe.
  let credResult: { ok: boolean; message: string; needsPAT?: boolean; sshPublicKey?: string; sshAddURL?: string; sshVerified?: boolean } | null = null;
  let credBusy = false;
  let credTokenInput = '';
  let credTokenGuide = '';
  let credTokenScopes = '';

  // Helper to reset all shared credential state.
  function resetCredState() {
    credResult = null;
    credBusy = false;
    credTokenInput = '';
    credTokenGuide = '';
    credTokenScopes = '';
    sshTokenDialog?.resetFields();
    credPrecheck = null;
  }

  // Precheck result for the credential type being set up. When non-OK, the
  // credential step renders a banner with install hints and skips the
  // automatic setup run — the user sees what's missing before they hit a
  // cryptic TTY / not-found error at auth time.
  let credPrecheck: DoctorPrecheckDTO | null = null;

  async function runCredentialPrecheck(credType: string) {
    credPrecheck = null;
    try {
      credPrecheck = await bridge.doctorPrecheck(credType);
    } catch {
      credPrecheck = null;
    }
  }

  // ── Shared credential functions (used by both add-account and change-credential) ──

  let sshKeyCopied = false;
  function copySSHKey(text: string) {
    navigator.clipboard.writeText(text).then(() => {
      sshKeyCopied = true;
      setTimeout(() => { sshKeyCopied = false; }, 2000);
    });
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
      credStatuses[accountKey] = {status: cs.status, primary: cs.primary, pat: cs.pat, primaryMsg: cs.primaryMsg, patMsg: cs.patMsg};
      credStatuses = credStatuses;
    }).catch(() => {
      credStatuses[accountKey] = {status: 'ok', primary: 'ok', pat: 'ok', primaryMsg: '', patMsg: ''};
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

  // Store a token for the account of the open credential modal (the
  // modal passes its account key).
  async function storeCredToken(key: string) {
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
  async function verifySSHConnection(key: string) {
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
  async function regenerateSSHKey(key: string) {
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

  // ── Change credential type ──
  let credentialDialog: CredentialModal;
  const openCredChange = (accountKey: string, currentType: string) => credentialDialog.openCredChange(accountKey, currentType);
  const openTokenSetup = (accountKey: string) => credentialDialog.openTokenSetup(accountKey);

  // ── Delete account ──
  let deleteAccountDialog: DeleteAccountModal;
  const askDeleteAccount = (accountKey: string) => deleteAccountDialog.askDeleteAccount(accountKey);

  // ── Mirrors ──
  let mirrorChecking: Record<string, boolean> = {};
  // Per-row "setup in progress" flag, keyed by `${mirrorKey}/${repoKey}`.
  // Set before calling bridge.setupMirrorRepo, cleared on the mirror:setup:done event.
  let mirrorSettingUp: Record<string, boolean> = {};
  let addMirrorGroupDialog: AddMirrorGroupModal;
  let addMirrorRepoDialog: AddMirrorRepoModal;
  let deleteMirrorGroupDialog: DeleteMirrorGroupModal;
  let deleteMirrorRepoDialog: DeleteMirrorRepoModal;
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

  // Track which repos have been individually added during this discover session.
  let discoverAdded: Record<string, boolean> = {};

  // Create repo modal
  let createRepoDialog: CreateRepoModal;
  const openCreateRepo = (accountKey: string) => createRepoDialog.openCreateRepo(accountKey);

  // ── Move repository (issue #64) ──
  let moveDialog: MoveRepoModal;
  const openMoveRepo = (sourceKey: string, repoKey: string) => moveDialog.openMoveRepo(sourceKey, repoKey);

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
    // Flip the per-row spinner on before firing the async bridge call. Cleared
    // in the mirror:setup:done handler below.
    const key = `${mirrorKey}/${repoKey}`;
    mirrorSettingUp = { ...mirrorSettingUp, [key]: true };
    await bridge.setupMirrorRepo(mirrorKey, repoKey);
  }

  // ── Workspace actions (read-only: open + refresh) ─────────────────

  async function openWorkspace(key: string) {
    try {
      await bridge.openWorkspace(key);
    } catch (e: any) {
      alert(e?.message || e);
    }
  }

  async function refreshWorkspaces() {
    workspaceBusy = true;
    try {
      const res = await bridge.discoverWorkspaces();
      if (res.changed) $configStore = await bridge.reloadConfig();
    } catch (e: any) {
      alert(e?.message || e);
    } finally {
      workspaceBusy = false;
    }
  }

  // ── Multi-repo container toggle ────────────────────────────────────

  function isContainerRepo(sourceKey: string, repoName: string): boolean {
    return !!$configStore?.sources?.[sourceKey]?.repos?.[repoName]?.container;
  }

  async function toggleContainer(sourceKey: string, repoName: string) {
    const next = !isContainerRepo(sourceKey, repoName);
    try {
      await bridge.setRepoContainer(sourceKey, repoName, next);
      $configStore = await bridge.reloadConfig();
      if (next) {
        // Newly a container — rescan and open the orphans modal so the user
        // reviews and adopts the nested clones via the tested flow. They adopt
        // in place (an absolute clone_folder inside the container).
        await loadOrphans();
        if (orphanList.some(o => o.nested && o.matchedAccount && !o.localOnly)) {
          showOrphanModal();
        }
      }
    } catch (e: any) {
      alert(e?.message || e);
    }
  }

  // ── Settings ──
  let showSettings = false;
  let configPath = '';
  let appVersion = '';
  let hostOS = ''; // "darwin" | "windows" | "linux" — loaded on init
  // windowMode tells the frontend which UI to render. The default "main"
  // shows the full gitbox app; "terminals" hides everything except the
  // standalone Profile editor window spawned by openTerminalsManagerWindow
  // (issue #69 — keeps the editor in its own OS window so the parent
  // stays interactive).
  let windowMode: 'main' | 'terminals' = 'main';
  let autostartOn = false;
  let autostartSupported = true;

  // ── Doctor (System check) ──
  // DoctorModal runs the check; Settings shows its one-line summary.
  let doctorSummary = '';
  let doctorModal: DoctorModal;
  const openDoctorModal = () => doctorModal.openDoctorModal();

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
      fetchTimerId = setInterval(() => { runFetchAll(); checkAllMirrorStatus(); refreshAllPRs(); }, minutes * 60 * 1000);
    }
  }

  // ── PR badges (issue #29) ──
  async function loadPRSettings() {
    try {
      const s = await bridge.getPRSettings();
      prSettings.set({ enabled: !!s.enabled, includeDrafts: !!s.includeDrafts });
    } catch (e) { console.error(e); }
  }

  function refreshAllPRs() {
    let enabled = false;
    prSettings.subscribe((v) => { enabled = v.enabled; })();
    if (!enabled) return;
    bridge.refreshAllPRs().catch((e) => console.error(e));
  }

  async function setPRBadgesEnabled(enabled: boolean) {
    try {
      await bridge.setPRBadgesEnabled(enabled);
      prSettings.update((v) => ({ ...v, enabled }));
      if (enabled) refreshAllPRs();
      else prsByAccount.set({});
    } catch (e) { console.error(e); }
  }

  async function setPRIncludeDrafts(include: boolean) {
    try {
      await bridge.setPRIncludeDrafts(include);
      prSettings.update((v) => ({ ...v, includeDrafts: include }));
      refreshAllPRs();
    } catch (e) { console.error(e); }
  }

  // Which PR popover is open: key is `${sourceKey}/${repoName}|${kind}` or null.
  let openPRPopover: string | null = null;
  function togglePRPopover(key: string) {
    openPRPopover = openPRPopover === key ? null : key;
  }

  // Which workspace popover is open: keyed by `${sourceKey}/${repoName}` or null.
  // Driven by the workspace badge on each clone row; lists every workspace the
  // clone belongs to, opens one on click and dismisses itself.
  let openWsPopover: string | null = null;
  function toggleWsPopover(key: string) {
    openWsPopover = openWsPopover === key ? null : key;
  }
  async function openWorkspaceFromBadge(wsKey: string) {
    openWsPopover = null;
    await openWorkspace(wsKey);
  }
  function setFetchInterval(val: string) {
    applyFetchInterval(val);
    bridge.setPeriodicSync(val);
  }

  async function runFetchAll() {
    if (fetchingAll) return;
    fetchingAll = true;
    try {
      await bridge.fetchAllRepos();
    } catch (e) {
      lastFetchTime = 'fetch-all error: ' + (e instanceof Error ? e.message : String(e));
    } finally {
      fetchingAll = false;
      fetchingRepos = {};
    }
  }

  // ── Credential status cache ──
  let credStatuses: Record<string, any> = {};
  let credentialGeneration = 0;

  // ── Lifecycle ──
  async function initDashboard() {
    const snapshot = await bridge.getStartupSnapshot();
    const cfg = snapshot.config;
    repoNesting = snapshot.nesting || {};
    const collapsed = cfg.global?.collapsed;
    collapsedSources = new Set(collapsed?.sources || []);
    collapsedRepos = new Set(collapsed?.repos || []);
    cardsOpen = !!collapsed?.cards_open;
    cardsReady = true;
    languageChoice = normalizeLanguage(cfg.global?.language);
    languageStore.set(languageChoice);
    configPath = snapshot.configPath;
    appVersion = snapshot.appVersion;
    hostOS = snapshot.os;
    fetchInterval = cfg.global?.periodic_sync || 'off';
    uiMode = cfg.global?.ui_mode === 'simple' ? 'simple' : 'advanced';
    if (uiMode === 'simple') enterSimple();
    const savedMode = cfg.global?.view_mode;
    configStore.set(cfg);
    repoStates.set(Object.fromEntries(Object.entries(cfg.sources).flatMap(([sourceKey, source]) =>
      Object.keys(source.repos).map(repoName => [`${sourceKey}/${repoName}`, { status: 'unknown' as const, progress: 0, behind: 0, modified: 0, untracked: 0, ahead: 0 }])
    )));
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

    try {
      applyStatusResults(await bridge.getAllStatus());
    } catch (e) {
      repoStates.update(states => Object.fromEntries(Object.entries(states).map(([key, state]) =>
        [key, state.status === 'unknown' ? { ...state, status: 'error' as const, error: String(e) } : state]
      )));
    }

    // Fetch all repos on startup to detect behind/ahead changes.
    runFetchAll();

    // Check mirror sync status on startup.
    checkAllMirrorStatus();

    // Check for global ~/.gitconfig identity (warns user if set).
    checkGlobalIdentity();

    // Check that ~/.gitconfig has the GCM credential.helper set globally
    // when at least one account uses GCM. Without this, GCM setup fails
    // with "Device not configured" on hosts that don't have a per-host
    // helper override.
    checkGlobalGCM();

    // Load the check-global-gitignore preference and, if enabled, run
    // the startup check so the banner surfaces immediately. Explicit
    // user actions (the gear toggle, the Install button) bypass the
    // preference and always run.
    await loadCheckGitignorePref();
    if (checkGitignorePref) {
      checkGlobalGitignore();
    }

    // Load autostart state.
    loadAutostart();

    // Scan for orphan repos in the background.
    loadOrphans();

    // Start periodic status check if previously configured.
    if (fetchInterval !== 'off') applyFetchInterval(fetchInterval);

    // Load PR badge feature flags and kick off initial PR fetch.
    await loadPRSettings();
    refreshAllPRs();
  }

  async function setUIMode(mode: 'simple' | 'advanced') {
    if (uiMode === mode) return;
    uiMode = mode;
    if (mode === 'simple') enterSimple();
    await bridge.setUIMode(mode);
  }

  // enterSimple leaves the Advanced-only states Simple has no controls for.
  function enterSimple() {
    cardsTab = 'accounts';
    deleteMode = false;
  }

  async function setLanguage(lang: 'en' | 'es') {
    if (languageChoice === lang) return;
    languageChoice = lang;
    languageStore.set(lang);
    await bridge.setLanguage(lang);
    const cfg = await bridge.getConfig();
    configStore.set(cfg);
  }

  function verifyAllCredentials() {
    const generation = ++credentialGeneration;
    for (const key of Object.keys($accounts)) {
      const account = JSON.stringify($accounts[key]);
      bridge.credentialVerify(key).then((cs) => {
        if (generation !== credentialGeneration || account !== JSON.stringify($accounts[key])) return;
        credStatuses[key] = {status: cs.status, primary: cs.primary, pat: cs.pat, primaryMsg: cs.primaryMsg, patMsg: cs.patMsg};
        credStatuses = credStatuses;
      }).catch(console.error);
    }
  }

  onMount(async () => {
    // Load the window mode before anything else — the terminals sub-process
    // skips most of the heavy init below and renders only the Profile
    // editor.
    try {
      const mode = await bridge.getWindowMode();
      if (mode === 'terminals') {
        windowMode = 'terminals';
      }
    } catch { /* fall back to main mode */ }

    if (windowMode === 'terminals') {
      // Terminals sub-process: just hydrate the bits the editor needs.
      const hydrate = async () => {
        try {
          const cfg = await bridge.getConfig();
          configStore.set(cfg);
        } catch (e) { console.error(e); }
      };
      try { hostOS = await bridge.getOS(); } catch (e) { console.error(e); }
      await hydrate();
      applyTheme();
      // The parent uses HideWindowOnClose, so a "second open" of the
      // Manager just re-shows this same hidden window. Re-hydrate the
      // config on focus so the table reflects any wezterm.lua /
      // installed-shell changes the user made between visits. The Go
      // side also emits 'profiles:reloaded' from the
      // OnSecondInstanceLaunch hook after running SyncProfiles —
      // listen for that explicitly so we pick the refresh up even if
      // the focus event timing is flaky on Linux/Windows.
      window.addEventListener('focus', () => { hydrate(); });
      EventsOn('profiles:reloaded', () => { hydrate(); });
      // The Wails subprocess starts hidden so the user never sees a
      // dark-empty-webview flash before Svelte mounts. Reveal the
      // window only after Svelte has flushed the DOM AND the browser
      // has rasterised the first frame — the canonical "after paint"
      // point is tick() + two requestAnimationFrames (the first RAF
      // runs before paint, the second runs after). Without this the
      // window appears black for a few frames on Windows + Linux.
      // (Issue #69 user feedback. macOS WebKit happens to paint
      // synchronously so it never showed the flash there.)
      await tick();
      requestAnimationFrame(() => {
        requestAnimationFrame(() => {
          bridge.showWindow();
        });
      });
      return;
    }

    applyTheme();
    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
      if (themeChoice === 'system') applyTheme();
    });

    // Re-read config when the window regains focus (picks up external edits),
    // and ask the Go side to re-probe AI harnesses (throttled there) so a
    // tool installed or removed while we were in the background shows up.
    let focusTimer: ReturnType<typeof setTimeout> | null = null;
    window.addEventListener('focus', () => {
      if (focusTimer) clearTimeout(focusTimer);
      focusTimer = setTimeout(() => reloadFromDisk(), 300);
      bridge.refreshAIHarnesses().catch(() => {});
    });

    // ── Event listeners (always registered) ──
    // Background harness detection changed global.ai_harnesses and saved
    // it; swap the list in the store without a full config reload.
    events.on('harnesses:updated', (list: AIHarnessInfo[] | null) => {
      configStore.update((c) => c ? { ...c, global: { ...c.global, ai_harnesses: list || [] } } : c);
    });

    events.on('tools:updated', () => {
      bridge.getConfig().then(cfg => configStore.update(current => current ? { ...current, global: { ...current.global, editors: cfg.global.editors, terminal_apps: cfg.global.terminal_apps, shells: cfg.global.shells, terminal_profiles: cfg.global.terminal_profiles } } : current)).catch(console.error);
    });

    events.on('status:updated', (results: any) => {
      statusGeneration++;
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
            // Rows seeded in-session (create-repo, Bring Local) have no path
            // until the next full status refresh; take it from the event so
            // path-based kebab actions work right away (#79).
            path: data.path || s[key].path,
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

    events.on('fetch:allstart', () => {
      fetchingAll = true;
    });

    events.on('fetch:alldone', (data: { error?: string } | null) => {
      fetchingAll = false;
      fetchingRepos = {};
      const now = new Date();
      lastFetchTime = data?.error ? 'fetch-all error: ' + data.error
        : now.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
    });

    events.on('credentials:updated', (statuses: Record<string, CredentialStatus>) => {
      credentialGeneration++;
      credStatuses = statuses || {};
    });

    events.on('discover:done', (data: any) => discoverDialog.onDone(data));

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
      // Clear the per-row spinner. The Go side now emits the mirrorKey too
      // (issue #59) so we can key off ${mirrorKey}/${repoKey} directly.
      if (data?.mirrorKey && data?.repoKey) {
        const key = `${data.mirrorKey}/${data.repoKey}`;
        const { [key]: _removed, ...rest } = mirrorSettingUp;
        mirrorSettingUp = rest;
      }
      $configStore = await bridge.reloadConfig();
      checkAllMirrorStatus();
    });

    // Discovery emitted on startup and on every manual rescan. Refresh
    // the config store so the new entries appear in the Workspaces tab.
    events.on('workspaces:discovered', async (_data: any) => {
      $configStore = await bridge.reloadConfig();
    });

    events.on('update:available', (info: any) => {
      if (info?.latest !== updateDismissed) updateInfo = info;
    });

    events.on('update:progress', (msg: string) => {
      updateProgress = msg;
    });

    // Installed and the new version is starting: the backend quits next.
    events.on('update:restarting', (latest: string) => {
      updateProgress = `Restarting into ${latest}…`;
    });

    // Installed, but the new version could not be started automatically.
    events.on('update:done', () => {
      updateApplying = false;
      updateProgress = '';
      updateDone = true;
    });

    events.on('update:quit', (latest: string) => {
      // Elevated update script is running — quit so it can overwrite the
      // binaries; the script starts the new version once it has copied them.
      updateProgress = `Restarting into ${latest}…`;
      // Auto-quit after a brief moment so the user sees the message.
      setTimeout(() => Quit(), 1500);
    });

    events.on('pr:refreshed', (upd: any) => {
      applyPRUpdate(upd as PRAccountUpdateDTO);
    });

    events.on('move:progress', (p: any) => moveDialog.onProgress(p));

    events.on('move:done', (r: any) => moveDialog.onDone(r));

    // Check if config failed to parse (file exists but is broken/unsupported).
    // Render an in-app modal rather than window.confirm — macOS WebKit has
    // been observed to auto-dismiss confirm() without showing a dialog,
    // which caused GitboxApp to "flash and exit" on mac with a malformed
    // config. The modal stays mounted until the user clicks a button.
    const loadError = await bridge.getConfigLoadError();
    if (loadError) {
      cfgLoadErrorMsg = loadError;
      // Populate configPath and the backup list before showing the screen
      // so everything renders in one pass — no flash of empty state.
      try {
        configPath = await bridge.getConfigPath();
      } catch { /* fall back to the template's default placeholder */ }
      await cfgLoadBackups();
      cfgLoadErrorModal = true;
      return; // onMount stops here; dashboard init resumes after the user
              // picks Restore / Auto-repair / Start fresh / Exit.
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

</script>

<svelte:window on:click={(e) => {
  const inMenu = e.target instanceof Element && e.target.closest('.action-menu-container');
  const inWsPopover = e.target instanceof Element && e.target.closest('.ws-popover, .ws-badge');
  const inPRPopover = e.target instanceof Element && e.target.closest('.pr-badge-wrap');
  if (actionMenuRepo && !inMenu) closeActionMenu();
  if (actionMenuAccount && !inMenu) closeAccountMenu();
  if (expandedCard && !(e.target instanceof Element && e.target.closest('.card-open'))) expandedCard = null;
  if (openWsPopover && !inWsPopover) openWsPopover = null;
  if (openPRPopover && !inPRPopover) openPRPopover = null;
}} on:keydown={(e) => {
  if (e.key === 'Escape' && openWsPopover) openWsPopover = null;
  if (e.key === 'Escape' && expandedCard) expandedCard = null;
  if (e.key === 'Escape' && openPRPopover) openPRPopover = null;
}} />

<!-- ════════════════════════════════════════════════════════════ -->
<!--  TEMPLATE                                                   -->
<!-- ════════════════════════════════════════════════════════════ -->

<!-- ── TERMINALS SUB-PROCESS WINDOW (issue #69) ── -->
{#if windowMode === 'terminals'}
  <div class="terminals-window-host">
    <TerminalsModal
      open={true}
      mode="window"
      apps={configApps}
      shells={configShells}
      profiles={configProfiles}
      onSave={saveTerminalProfilesAndReload}
      onConfigReloaded={reloadConfigFromDisk}
      onClose={() => { /* no-op — closing happens via the OS window chrome */ }}
    />
  </div>
{/if}

{#if windowMode === 'main'}

<!-- ── CONFIG LOAD ERROR ── -->
{#if cfgLoadErrorModal}
<ConfigErrorScreen {configPath} {cfgLoadErrorMsg} {cfgRepairFailure} {cfgBackups} {cfgLoadErrorBusy} {cfgRestoreBackup} {cfgDoRepair} {cfgStartFresh} />

<!-- ── ONBOARDING ── -->
{:else if firstRun}
<Onboarding mode="first-run" onDone={finishOnboarding} />
{:else}

<div class="app">

  <ReloadErrorBanner bind:reloadError />

  {#if viewMode === 'compact'}
  <!-- ══════ COMPACT STATUS VIEW ══════ -->
  <CompactView {compactExpanded} {credStatuses} {treeRows} {fetchingRepos} {themeChoice} {toggleCompactAcct} {cycleTheme} {toggleViewMode} />
  {:else}
  <!-- ══════ FULL DASHBOARD VIEW ══════ -->

  <!-- ── TOP BAR ── -->
  <TopBar {simple} {syncing} {fetchingAll} {themeChoice} bind:deleteMode bind:showSettings
    {syncAll} {runFetchAll} {toggleViewMode} {cycleTheme} {setUIMode} />

  <!-- ── GLOBAL WARNINGS (identity, GCM helper, gitignore) ── -->
  <Banners bind:globalIdentityWarn bind:globalGCMWarn bind:gitignoreWarn bind:gitignoreInstalled bind:gitignoreError
    {globalGCMFixing} {gitignoreInstalling} {fixGlobalIdentity} {fixGlobalGCM} {installGlobalGitignore} />

  <!-- ── SETTINGS PANEL ── -->
  <SettingsPanel {showSettings} {simple} {configPath} {languageChoice} bind:themeChoice {fetchInterval} {lastFetchTime}
    {autostartSupported} {autostartOn} {checkGitignorePref} {doctorSummary} {terminalsManagerOpening} {appVersion}
    {openChangeFolder} {setLanguage} {applyTheme} {setFetchInterval} {toggleAutostart} {setPRBadgesEnabled}
    {setPRIncludeDrafts} {setCheckGitignorePref} {openDoctorModal} {openTerminalsManagerWindow} />

  <!-- ── TAB BAR ── -->
  <TabBar {cardsTab} {cardsVisible} {simple} {credStatuses} {orphanCount} {mirrorDiscoverLoading} {workspaceBusy}
    {selectCardsTab} {showOrphanModal} {runMirrorDiscover} {checkAllMirrorStatus} {refreshWorkspaces} />

  {#if cardsTab === 'accounts'}
  <!-- ── ACCOUNT CARDS ── -->
  <AccountCards {cardsVisible} bind:expandedCard {deleteMode} {simple} {credStatuses} {resolvedTheme}
    {askDeleteAccount} {openEditAccount} {openCredChange} {openDiscover} {openCreateRepo}
    onAddAccount={() => { if (simple) connectModal = true; else addAccountDialog.open(); }} />

  <!-- ── REPO LIST ── -->
  <RepoList {treeRows} {collapsedSources} {collapsedRepos} {simple} {deleteMode} {fetchingRepos} {credStatuses}
    {tentativeContainers} {workspaceBusy} {openWsPopover} {revealLabel} {configEditors} {configProfiles} {configAIHarnesses}
    {expandedRepo} {repoDetail} {detailLoading}
    bind:actionMenuAccount bind:actionMenuRepo bind:openPRPopover
    {toggleSourceCollapsed} {toggleRepoCollapsed} {toggleAccountMenu} {closeAccountMenu} {openDiscover} {openCreateRepo}
    {activateRepoRow} {askDelete} {syncRepo} {cloneRepo} {fetchRepo} {toggleActionMenu} {closeActionMenu} {toggleWsPopover}
    {openWorkspaceFromBadge} {togglePRPopover} {sweepBranches} {openMoveRepo} {toggleContainer} {rescanContainer} {isContainerRepo} />

  {:else if cardsTab === 'mirrors'}
  <!-- ── MIRRORS TAB ── -->
  <MirrorsTab {cardsVisible} bind:expandedCard {deleteMode} {mirrorDiscoverLoading} {mirrorDiscoverProgress} {mirrorChecking} {mirrorSettingUp}
    {checkMirrorStatus} {setupMirrorRepo} {openTokenSetup}
    onAddGroup={() => addMirrorGroupDialog.open()} onAddRepo={(mirrorKey) => addMirrorRepoDialog.open(mirrorKey)}
    onDeleteGroup={(mirrorKey) => deleteMirrorGroupDialog.ask(mirrorKey)} onDeleteRepo={(mirrorKey, repoKey) => deleteMirrorRepoDialog.ask(mirrorKey, repoKey)} />
  {:else}
  <!-- ── WORKSPACES TAB ── -->
  <WorkspacesTab {cardsVisible} bind:expandedCard {workspaceBusy} {openWorkspace} />
  {/if}

  <!-- ── SUMMARY FOOTER ── -->
  <Footer {simple} bind:updateInfo bind:updateApplying bind:updateError bind:updateDismissed {updateProgress} {updateDone} />

  {/if}
  <!-- end viewMode -->

  <!-- ── DISCOVER MODAL ── -->
  <DiscoverModal bind:this={discoverDialog} {reloadFromDisk} {cloneRepo} {openCredChange} onNeedsToken={(acctKey) => sshTokenDialog.openFor(acctKey)} />

  <!-- ── TERMINALS & SHELLS (issue #69) ──
       The Manager opens in its own OS window via openTerminalsManagerWindow
       (subprocess spawn), so no in-app modal is mounted here. -->

  <!-- ── DOCTOR (SYSTEM CHECK) MODAL ── -->
  <DoctorModal bind:this={doctorModal} bind:doctorSummary />

  <!-- ── SSH DISCOVERY TOKEN MODAL ── -->
  <SshTokenModal bind:this={sshTokenDialog} {openDiscover} />

  <!-- ── EDIT ACCOUNT MODAL ── -->
  <EditAccountModal bind:this={editAccountDialog} {reloadFromDisk} />

  <!-- ── DELETE CONFIRMATION MODAL ── -->
  <DeleteCloneModal bind:this={deleteCloneDialog} bind:expandedRepo bind:repoDetail bind:deleteMode {reloadFromDisk} />

  <!-- ── SWEEP CONFIRM MODAL ── -->
  <SweepModal bind:this={sweepDialog} {closeActionMenu} />

  <!-- ── ORPHAN ADOPTION MODAL ── -->
  <OrphansModal bind:this={orphansDialog} {orphanList} {loadOrphans} />

  <!-- ── ADD ACCOUNT MODAL ── -->
  {#if connectModal}
    <Onboarding mode="add" onDone={finishConnect} onCancel={() => { connectModal = false; reloadFromDisk(); }} />
  {/if}

  <AddAccountModal bind:this={addAccountDialog} bind:credResult bind:credTokenInput bind:credPrecheck {credBusy} {credTokenGuide} {credTokenScopes} {sshKeyCopied} {copySSHKey} {resetCredState} {verifyAllCredentials} {reloadFromDisk} {runCredentialPrecheck} {runCredSetup} {fetchTokenGuide} {verifySSHConnection} {regenerateSSHKey} />

  <!-- ── CHANGE CREDENTIAL MODAL ── -->
  <CredentialModal bind:this={credentialDialog} bind:credResult bind:credBusy bind:credTokenInput bind:credPrecheck bind:credStatuses {credTokenGuide} {credTokenScopes} {sshKeyCopied} {hostOS} {copySSHKey} {resetCredState} {verifyAllCredentials} {reloadFromDisk} {runCredentialPrecheck} {fetchTokenGuide} {credPostSetup} {storeCredToken} {verifySSHConnection} {regenerateSSHKey} />

  <!-- ── DELETE ACCOUNT MODAL ── -->
  <DeleteAccountModal bind:this={deleteAccountDialog} bind:reloadError bind:deleteMode {reloadFromDisk} />

  <!-- ── CHANGE FOLDER MODAL ── -->
  <ChangeFolderModal bind:this={changeFolderDialog} {reloadFromDisk} />

  <!-- ── ADD MIRROR GROUP MODAL ── -->
  <!-- Workspaces are read-only: no create/delete modals. -->
  <AddMirrorGroupModal bind:this={addMirrorGroupDialog} />

  <!-- ── ADD MIRROR REPO MODAL ── -->
  <AddMirrorRepoModal bind:this={addMirrorRepoDialog} {setupMirrorRepo} />

  <!-- ── DELETE MIRROR GROUP CONFIRM ── -->
  <DeleteMirrorGroupModal bind:this={deleteMirrorGroupDialog} bind:deleteMode />

  <!-- ── DELETE MIRROR REPO CONFIRM ── -->
  <DeleteMirrorRepoModal bind:this={deleteMirrorRepoDialog} bind:deleteMode />

  <!-- ── MIRROR SETUP RESULT MODAL ── -->
  <MirrorSetupResultModal bind:mirrorSetupResultModal />

  <!-- ── MIRROR DISCOVER RESULTS MODAL ── -->
  <MirrorDiscoverModal bind:mirrorDiscoverResults bind:discoverAdded {mirrorDiscoverError} {checkAllMirrorStatus} />

  <!-- ── MIRROR CREDENTIAL WARNING MODAL ── -->
  <MirrorTokenModal bind:mirrorCredWarning storeMirrorPat={openTokenSetup} />

  <!-- ── MOVE REPO MODAL (issue #64) ── -->
  <MoveRepoModal bind:this={moveDialog} {reloadFromDisk} {openCredChange} {openTokenSetup} />

  <!-- ── CREATE REPO MODAL ── -->
  <CreateRepoModal bind:this={createRepoDialog} />

</div>
{/if}

{/if}
<!-- /windowMode === 'main' -->

<!-- ════════════════════════════════════════════════════════════ -->
<!--  STYLES                                                     -->
<!-- ════════════════════════════════════════════════════════════ -->

<style>
  .app { max-width: 860px; margin: 0 auto; height: 100vh; display: flex; flex-direction: column; overflow: hidden; }
</style>
