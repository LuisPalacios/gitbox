<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { fade, slide } from 'svelte/transition';
  import { bridge, events } from './lib/bridge';
  import type { DoctorPrecheckDTO } from './lib/bridge';
  import {
    configStore, accounts, sources, mirrors, repoStates, mirrorStates, mirrorSummary, summary,
    accountStats, themeStore, applyStatusResults, applyMirrorStatusResults, prsByAccount,
    prSettings, applyPRUpdate, lookupPRSummary, workspaces, workspaceOrder, workspaceMemberships,
    selectedClones, toggleCloneSelection, clearCloneSelection, isSynced
  } from './lib/stores';
  import { credColor, providerLabel } from './lib/theme';
  import { statusColor, simpleKind, simpleColor, simpleLabelKey, simpleUnsaved, rowBusy, rowGlyph, advancedLabel, COUNT_BADGES, countBadge, statusCountBadge, detailLocked, opensDetail, credBroken, canDiscoverWith, canCreateWith, credBadgeClass, credBadgeLabel, DISCOVER_DISABLED, CREATE_DISABLED } from './lib/status';
  import type { SimpleKind } from './lib/status';
  import { languageStore, normalizeLanguage, t } from './lib/i18n';
  import { WindowSetSize, WindowSetMinSize, WindowGetSize, WindowSetPosition, WindowGetPosition, Quit, EventsOn } from '../wailsjs/runtime/runtime';
  import type { RepoState, CredentialStatus, MirrorDTO, MirrorRepo, MirrorStatusResult, MirrorSetupResult, MirrorCredentialCheck, EditorInfo, AIHarnessInfo, TerminalAppInfo, ShellInfo, TerminalProfileInfo, PRAccountUpdateDTO, RepoDetailView, GlobalIdentityWarn, GCMWarn, GitignoreWarn, GitignoreInstalled, UpdateInfo, ConfigBackupInfo } from './lib/types';
  import LauncherMenu from './lib/LauncherMenu.svelte';
  import BrandIcon, { terminalLogo } from './lib/BrandIcon.svelte';
  import PRPopover from './lib/PRPopover.svelte';
  import TerminalsModal from './lib/TerminalsModal.svelte';
  import Onboarding from './lib/Onboarding.svelte';
  import DoctorModal from './lib/modals/DoctorModal.svelte';
  import EditAccountModal from './lib/modals/EditAccountModal.svelte';
  import DeleteCloneModal from './lib/modals/DeleteCloneModal.svelte';
  import SweepModal from './lib/modals/SweepModal.svelte';
  import OrphansModal from './lib/modals/OrphansModal.svelte';
  import DeleteAccountModal from './lib/modals/DeleteAccountModal.svelte';
  import ChangeFolderModal from './lib/modals/ChangeFolderModal.svelte';
  import CreateRepoModal from './lib/modals/CreateRepoModal.svelte';
  import DiscoverModal from './lib/modals/DiscoverModal.svelte';
  import SshTokenModal from './lib/modals/SshTokenModal.svelte';
  import AddAccountModal from './lib/modals/AddAccountModal.svelte';
  import CredentialModal from './lib/modals/CredentialModal.svelte';
  import MoveRepoModal from './lib/modals/MoveRepoModal.svelte';
  import ConfigErrorScreen from './lib/components/ConfigErrorScreen.svelte';
  import ReloadErrorBanner from './lib/components/ReloadErrorBanner.svelte';
  import TopBar from './lib/components/TopBar.svelte';
  import Banners from './lib/components/Banners.svelte';
  import SettingsPanel from './lib/components/SettingsPanel.svelte';
  import TabBar from './lib/components/TabBar.svelte';
  import Footer from './lib/components/Footer.svelte';
  import AddMirrorGroupModal from './lib/modals/AddMirrorGroupModal.svelte';
  import AddMirrorRepoModal from './lib/modals/AddMirrorRepoModal.svelte';
  import DeleteMirrorGroupModal from './lib/modals/DeleteMirrorGroupModal.svelte';
  import DeleteMirrorRepoModal from './lib/modals/DeleteMirrorRepoModal.svelte';
  import MirrorSetupResultModal from './lib/modals/MirrorSetupResultModal.svelte';
  import MirrorDiscoverModal from './lib/modals/MirrorDiscoverModal.svelte';
  import MirrorTokenModal from './lib/modals/MirrorTokenModal.svelte';
  import { repoTreeRows } from './lib/repo-tree';
  import { onActivateKey } from './lib/keyboard';

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

  // Selection mode toggle for the clone list. Distinct from deleteMode
  // because the two flows serve different goals and should not share state.
  let selectionMode = false;
  let compactExpanded: Record<string, boolean> = {};
  let savedFullSize: { w: number; h: number } | null = null;
  let savedFullPos: { x: number; y: number } | null = null;

  // A repo row toggles clone selection in workspace-selection mode and
  // its detail panel otherwise.
  function activateRepoRow(sourceKey: string, repoName: string, repoKey: string, status: string) {
    if (selectionMode) toggleCloneSelection(repoKey);
    else if (status !== 'unknown') toggleRepoDetail(sourceKey, repoName, status);
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

  // ── Action menu (kebab) ──
  function toggleActionMenu(repoKey: string) {
    actionMenuRepo = actionMenuRepo === repoKey ? null : repoKey;
  }

  function closeActionMenu() {
    actionMenuRepo = null;
  }

  // requireRepoPath returns the row's local clone path, or shows the
  // "clone first" dialog and returns null. Path-based kebab actions used
  // to return silently when the path was missing (#79); a row cloned
  // in-session has none until clone:done or the next status refresh.
  function requireRepoPath(repoKey: string, title: string): string | null {
    const p = $repoStates[repoKey]?.path;
    if (!p) { void bridge.showErrorDialog(title, $t('launcher.cloneFirst')); }
    return p ?? null;
  }

  async function openRepoInExplorer(repoKey: string) {
    const path = requireRepoPath(repoKey, revealLabel);
    if (path) await bridge.openInExplorer(path);
    closeActionMenu();
  }

  async function openRepoInApp(repoKey: string, command: string) {
    const path = requireRepoPath(repoKey, 'Open in editor');
    if (path) await bridge.openInApp(path, command);
    closeActionMenu();
  }

  // openRepoProfile is the v2.1 launcher entry point — resolves the repo's
  // working directory then asks the Go side to expand the profile's
  // template via pkg/launch.ResolveArgs and spawn the terminal.
  async function openRepoProfile(repoKey: string, profileID: string) {
    const path = requireRepoPath(repoKey, 'Open profile');
    if (path) {
      try { await bridge.openProfile(path, profileID); }
      catch (e: any) { await bridge.showErrorDialog('Open profile', (e?.message || String(e))); }
    }
    closeActionMenu();
  }

  async function openRepoInAIHarness(repoKey: string, harness: AIHarnessInfo) {
    const state = $repoStates[repoKey];
    if (!state?.path) {
      await bridge.showErrorDialog('Open in ' + harness.name, $t('launcher.cloneFirst'));
      closeActionMenu();
      return;
    }
    try {
      await bridge.openInAIHarness(state.path, harness.command, harness.args || []);
    } catch (e: any) {
      await bridge.showErrorDialog('Open in ' + harness.name, (e?.message || String(e)));
    }
    closeActionMenu();
  }

  // The URL is resolved on the Go side from the authoritative config
  // (git.RepoWebURL), so a stale frontend store can't turn this into a
  // silent no-op; lookup failures surface in a dialog (#79).
  async function openRepoInBrowser(sourceKey: string, repoName: string) {
    try {
      await bridge.openRepoInBrowser(sourceKey, repoName);
    } catch (e: any) {
      await bridge.showErrorDialog('Navigate to repo', (e?.message || String(e)));
    }
    closeActionMenu();
  }

  // ── Account-level actions (kebab on account card) ──
  function toggleAccountMenu(accountKey: string) {
    actionMenuAccount = actionMenuAccount === accountKey ? null : accountKey;
    actionMenuRepo = null;
  }

  function closeAccountMenu() {
    actionMenuAccount = null;
  }

  async function openAccountInExplorer(accountKey: string) {
    try { await bridge.openAccountFolder(accountKey); } catch (e) { console.error(e); }
    closeAccountMenu();
  }

  async function openAccountInApp(accountKey: string, command: string) {
    try { await bridge.openAccountInApp(accountKey, command); } catch (e) { console.error(e); }
    closeAccountMenu();
  }

  async function openAccountProfile(accountKey: string, profileID: string) {
    try { await bridge.openAccountProfile(accountKey, profileID); }
    catch (e: any) { await bridge.showErrorDialog('Open profile', (e?.message || String(e))); }
    closeAccountMenu();
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

  async function openAccountInAIHarness(accountKey: string, harness: AIHarnessInfo) {
    try {
      await bridge.openAccountInAIHarness(accountKey, harness.command, harness.args || []);
    } catch (e: any) {
      await bridge.showErrorDialog('Open in ' + harness.name, (e?.message || String(e)));
    }
    closeAccountMenu();
  }

  async function openAccountInBrowser(accountKey: string) {
    try { await bridge.openAccountInBrowser(accountKey); } catch (e) { console.error(e); }
    closeAccountMenu();
  }

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

  // isTentativeContainer reports whether the backend suggested this repo as a
  // container. The set is passed in (not read off the closure) so Svelte tracks
  // it as a reactive dependency of the row markup. Callers gate this behind the
  // reactive `isContainer` check, so an already-flagged container never shows
  // the hint.
  function isTentativeContainer(sourceKey: string, repoName: string, tc: Set<string>): boolean {
    return tc.has(`${sourceKey}/${repoName}`);
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
    if (selectionMode && tab !== 'accounts') toggleSelectionMode();
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

  function kindIcon(kind: string): string {
    return kind === 'deleted' ? '−' : kind === 'added' ? '+' : kind === 'renamed' ? '→' : '~';
  }

  function kindLabel(kind: string): string {
    return kind === 'deleted' ? 'Deleted' : kind === 'added' ? 'New file' : kind === 'renamed' ? 'Renamed' : kind === 'conflict' ? 'Conflict' : 'Changed';
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

  // repoCanMove is the kebab-enable predicate:
  // only a cloned, clean, in-sync repo can be moved. Returns a string
  // tooltip when disabled, or '' when enabled.
  function repoMoveDisabledReason(state: RepoState | undefined): string {
    if (!state) return 'Clone first';
    if (state.status === 'not cloned' || state.status === 'error') return 'Clone first';
    if (state.status !== 'clean') return 'Repo must be clean (commit/push/fetch first)';
    if ((state.ahead ?? 0) > 0 || (state.behind ?? 0) > 0) return 'Repo must be in sync with origin';
    return '';
  }

  function mirrorDirLabelHtml(repo: MirrorRepo, m: MirrorDTO): string {
    const origin = repo.origin === 'src' ? m.account_src : m.account_dst;
    const backup = repo.origin === 'src' ? m.account_dst : m.account_src;
    const src = `<span class="mir-origin">${origin}</span>`;
    const dst = `<span class="mir-backup">${backup}</span>`;
    if (repo.direction === 'push') return `${src} <span class="mir-arrow">⟶</span> ${dst}`;
    return `${dst} <span class="mir-arrow">⟵</span> ${src}`;
  }

  function mirrorStatusColor(_repo: MirrorRepo, live: MirrorStatusResult | undefined, theme: string): string {
    if (live?.needsSetup) return statusColor('not cloned', theme); // neutral — row has never been set up
    if (live?.error) return statusColor('error', theme);
    if (live?.syncStatus === 'synced') return statusColor('clean', theme);
    if (live?.syncStatus === 'behind') return statusColor('behind', theme);
    if (live?.syncStatus === 'ahead') return statusColor('error', theme);
    if (!live) return statusColor('not cloned', theme); // unchecked
    return statusColor('clean', theme);
  }

  function mirrorStatusSymbol(_repo: MirrorRepo, live: MirrorStatusResult | undefined): string {
    if (live?.needsSetup) return '○';
    if (live?.error) return '✕';
    if (live?.syncStatus === 'synced') return '●';
    if (live?.syncStatus === 'behind') return '↓';
    if (live?.syncStatus === 'ahead') return '↑';
    if (!live) return '○'; // unchecked
    return '●';
  }

  /** Summarize live mirror status in a short, user-friendly string. */
  function mirrorStatusText(_repo: MirrorRepo, live: MirrorStatusResult | undefined): string {
    if (live?.needsSetup) return 'needs setup';
    if (live?.error) return friendlyMirrorError(live.error);
    if (live?.syncStatus === 'synced') return 'Synced OK';
    if (live?.syncStatus === 'behind') return 'Backup is behind origin';
    if (live?.syncStatus === 'ahead') return 'Backup is ahead of origin';
    if (!live) return 'unchecked';
    return 'OK';
  }

  /** Turn raw Go error strings into concise, user-friendly messages. */
  function friendlyMirrorError(raw: string): string {
    // Errors from CheckStatus are already user-friendly (e.g. "missing API token in git-parchis-me").
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
        else if (live.needsSetup) unchecked++;
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

  function toggleSelectionMode() {
    selectionMode = !selectionMode;
    if (!selectionMode) clearCloneSelection();
  }

  // Lookup the ordered list of workspaces a given clone belongs to.
  // Returns an empty array when the clone has no memberships, so the
  // caller can cheaply check `.length > 0` to hide/show UI.
  function membershipsFor(repoKey: string): string[] {
    return $workspaceMemberships[repoKey] || [];
  }

  // "Mirror token needed" → Store PAT: the credential modal on the token type.
  function storeMirrorPat(key: string) {
    credentialDialog.openStorePat(key);
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
  function workspaceLabel(wsKey: string): string {
    const ws = $workspaces[wsKey];
    if (!ws) return wsKey;
    return ws.name && ws.name !== wsKey ? `${ws.name} (${wsKey})` : wsKey;
  }

  // Build the provider-native "all PRs" URL for a given repo full name.
  function providerPRsURL(providerName: string, baseURL: string, repoFull: string): string {
    const base = (baseURL || '').replace(/\/+$/, '') || defaultProviderBase(providerName);
    if (!base || !repoFull) return '';
    switch (providerName) {
      case 'github':   return `${base}/${repoFull}/pulls`;
      case 'gitlab':   return `${base}/${repoFull}/-/merge_requests`;
      case 'gitea':
      case 'forgejo':  return `${base}/${repoFull}/pulls`;
      default:         return '';
    }
  }

  function defaultProviderBase(providerName: string): string {
    if (providerName === 'github') return 'https://github.com';
    if (providerName === 'gitlab') return 'https://gitlab.com';
    return '';
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
    if (selectionMode) toggleSelectionMode();
    deleteMode = false;
  }

  // Simple row helpers: the short project name, the "not saved yet" detail,
  // and the one primary launcher (first AI harness, else editor, else folder).
  function shortName(repoName: string): string {
    return repoName.split('/').pop() || repoName;
  }
  function simpleTip(kind: SimpleKind, state: { status: string; modified: number; untracked: number }): string {
    if (kind === 'help') return $t('simple.tipHelp');
    if (kind !== 'unsaved') return '';
    if (state.status === 'ahead') return $t('simple.tipAhead');
    if (state.status === 'no upstream') return $t('simple.tipNoUpstream');
    return $t('simple.tipDirty', { n: (state.modified || 0) + (state.untracked || 0) });
  }
  // Every cloned row, Simple or Advanced, carries three icon-only launchers:
  // the first AI harness, the first editor, and the default terminal profile
  // (same defaults as the ⋯ menu). Each picks a brand logo by name, or a
  // generic icon.
  $: quickHarness = configAIHarnesses[0] || null;
  $: quickEditor = configEditors[0] || null;
  $: quickProfile = (() => {
    const visible = configProfiles.filter((p) => !p.hidden);
    return visible.find((p) => p.default) || visible[0] || null;
  })();
  function harnessIcon(h: AIHarnessInfo): string {
    const k = `${h.name} ${h.command}`.toLowerCase();
    if (k.includes('claude')) return 'claude';
    if (k.includes('codex') || k.includes('openai')) return 'openai';
    if (k.includes('gemini')) return 'gemini';
    if (k.includes('cursor')) return 'cursor';
    return 'ai';
  }
  function editorIcon(e: EditorInfo): string {
    const k = `${e.name} ${e.command}`.toLowerCase();
    if (k.includes('cursor')) return 'cursor';
    if (k.includes('zed')) return 'zed';
    if (k.includes('visual studio code') || k.includes('vs code') || k.includes('vscode') || /(^|[\\/ ])code(\.exe|\.cmd)?$/.test(e.command.toLowerCase())) return 'vscode';
    return 'ide';
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

  // Reactive helpers that depend on theme
  $: sc = (s: string) => statusColor(s, $themeStore);
  $: cc = (s: string) => credColor(s, $themeStore);
</script>

<!-- AI / editor / terminal launchers shared by Simple and Advanced repo rows. -->
{#snippet quickLaunch(repoKey: string)}
  <span class="launch-group">
    {#if quickHarness}
      {@const h = quickHarness}
      <button class="btn-launch" title={$t('simple.btnOpenWith', { name: h.name })} aria-label={$t('simple.btnOpenWith', { name: h.name })}
        on:click|stopPropagation={() => openRepoInAIHarness(repoKey, h)}><BrandIcon name={harnessIcon(h)} /></button>
    {/if}
    {#if quickEditor}
      {@const ed = quickEditor}
      <button class="btn-launch" title={$t('simple.btnOpenWith', { name: ed.name })} aria-label={$t('simple.btnOpenWith', { name: ed.name })}
        on:click|stopPropagation={() => openRepoInApp(repoKey, ed.command)}><BrandIcon name={editorIcon(ed)} /></button>
    {/if}
    {#if quickProfile}
      {@const pr = quickProfile}
      <button class="btn-launch" title={$t('simple.btnOpenWith', { name: pr.name })} aria-label={$t('simple.btnOpenWith', { name: pr.name })}
        on:click|stopPropagation={() => openRepoProfile(repoKey, pr.id)}><BrandIcon name={terminalLogo(pr.terminal, pr.shell)} /></button>
    {/if}
  </span>
{/snippet}

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
  {@const cpct = Math.round(($summary.clean / Math.max($summary.total, 1)) * 100)}
  {@const pendingStatus = Object.values($repoStates).some(state => state.status === 'unknown')}
  {@const callGood = !pendingStatus && $summary.clean === $summary.total}
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
        <span class="compact-global-pct" style="color: {callGood ? sc('clean') : sc('behind')}">{pendingStatus ? $t('simple.checking') : `${cpct}%`}</span>
        <span class="compact-global-label">{$summary.clean}/{$summary.total} synced</span>
      </div>
    </div>

    <div class="compact-sep"></div>

    <!-- Account pills -->
    {#each Object.keys($accounts) as key}
      {@const stats = $accountStats[key] || { total: 0, synced: 0, issues: 0 }}
      {@const compactCred = (credStatuses[key] || {status: 'unknown'}).status}
      <button class="compact-acct" class:compact-acct-expanded={compactExpanded[key]}
        class:compact-acct-cred-err={compactCred === 'none' || compactCred === 'error'}
        class:compact-acct-cred-warn={compactCred === 'warning'}
        class:compact-acct-cred-offline={compactCred === 'offline'}
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
            {#if Object.entries($sources).some(([sk, src]) => src.account === key && Object.keys(src.repos).some(rn => !$repoStates[`${sk}/${rn}`] || $repoStates[`${sk}/${rn}`].status === 'unknown'))}
              <span class="spinning">↻</span> {$t('simple.checking')}
            {:else if stats.issues === 0}
              <span style="color:{sc('clean')}">All good</span>
            {:else}
              <span style="color:{sc('behind')}">{stats.issues} need attention</span>
            {/if}
          </span>
        </div>
        <span class="compact-chevron">{compactExpanded[key] ? '▾' : '▸'}</span>
      </button>

      {#if compactExpanded[key]}
        <div class="compact-repo-list" transition:slide={{ duration: 120 }}>
          {#each Object.entries($sources).filter(([, source]) => source.account === key).flatMap(([sourceKey]) => treeRows[sourceKey] || []) as { key: repoKey, sourceKey: rowSourceKey, repoName, depth }}
            {@const state = $repoStates[repoKey] || { status: 'unknown', progress: 0, behind: 0, modified: 0, untracked: 0, ahead: 0, branch: '', isDefault: false }}
            {@const count = statusCountBadge(state.status)}
            <div class="compact-row" style={`padding-left: ${depth * 12}px`} class:compact-row-ok={state.status === 'clean'}>
              {#if rowSourceKey && $sources[rowSourceKey].account !== key}<span class="compact-badge">{$sources[rowSourceKey].account}</span>{/if}
              <span class="compact-dot" class:spinning={rowBusy(state.status, !!fetchingRepos[repoKey])} style="color: {sc(state.status)}">{rowGlyph(state.status, !!fetchingRepos[repoKey])}</span>
              <span class="compact-repo-name">{repoName.includes('/') ? repoName.split('/').pop() : repoName}</span>
              {#if state.branch === '(detached)'}
                <span class="compact-badge" style="color: {sc('error')}">detached</span>
              {/if}
              {#if count}
                <span class="compact-badge" style="color: {sc(count.color)}">{state[count.field]} {count.word}</span>
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
  <TopBar {simple} {syncing} {fetchingAll} {selectionMode} {cardsTab} {themeChoice} bind:deleteMode bind:showSettings
    {syncAll} {runFetchAll} {toggleSelectionMode} {toggleViewMode} {cycleTheme} {setUIMode} />

  <!-- update notification moved to summary footer -->

  <!-- ── GLOBAL WARNINGS (identity, GCM helper, gitignore) ── -->
  <Banners bind:globalIdentityWarn bind:globalGCMWarn bind:gitignoreWarn bind:gitignoreInstalled bind:gitignoreError
    {globalGCMFixing} {gitignoreInstalling} {fixGlobalIdentity} {fixGlobalGCM} {installGlobalGitignore} />

  <!-- ── SETTINGS PANEL ── -->
  <SettingsPanel {showSettings} {simple} {configPath} {languageChoice} bind:themeChoice {fetchInterval} {lastFetchTime}
    {autostartSupported} {autostartOn} {checkGitignorePref} {doctorSummary} {terminalsManagerOpening} {appVersion}
    {openChangeFolder} {setLanguage} {applyTheme} {setFetchInterval} {toggleAutostart} {setPRBadgesEnabled}
    {setPRIncludeDrafts} {setCheckGitignorePref} {openDoctorModal} {openTerminalsManagerWindow} />

  <!-- ── TAB BAR ── -->
  <TabBar {cardsTab} {cardsVisible} {simple} {credStatuses} {orphanCount} {mirrorDiscoverLoading} {workspaceBusy} {selectionMode}
    {selectCardsTab} {showOrphanModal} {runMirrorDiscover} {checkAllMirrorStatus} {refreshWorkspaces} />

  {#if cardsTab === 'accounts'}
  <!-- ── ACCOUNT CARDS ── -->
  {#if cardsVisible}
  <section class="cards-row" transition:slide={{ duration: 150 }}>
    {#each Object.entries($accounts) as [key, acct]}
      {@const credObj = credStatuses[key] || {status: 'unknown', primary: 'unknown', pat: 'unknown'}}
      {@const credOverall = credObj.status}
      {@const canDiscover = canDiscoverWith(credOverall)}
      {@const canCreate = canCreateWith(credOverall)}
      {#if expandedCard !== `acct:${key}`}
      <div class="card-chip" role="button" tabindex="0" title={key}
        on:click|stopPropagation={() => expandedCard = `acct:${key}`}
        on:keydown={onActivateKey(() => expandedCard = `acct:${key}`)}
        style={credBroken(credOverall) ? `background: ${resolvedTheme === 'light' ? '#fef2f2' : '#2a1215'}` : ''}>
        {#if deleteMode}
          <button class="btn-delete-x card-delete-btn" on:click|stopPropagation={() => askDeleteAccount(key)} title="{$t('account.delete')} {key}">&#10005;</button>
        {:else}
          <span class="card-dot" style="background: {cc(credOverall)}"></span>
        {/if}
        <span class="card-chip-name">{key}</span>
      </div>
      {:else}
      <div class="card card-open" class:card-delete-mode={deleteMode}
        style={credBroken(credOverall) ? `background: ${resolvedTheme === 'light' ? '#fef2f2' : '#2a1215'}` : ''}>
        <div class="card-top">
          {#if deleteMode}
            <button class="btn-delete-x card-delete-btn" on:click={() => askDeleteAccount(key)} title="{$t('account.delete')} {key}">&#10005;</button>
          {:else}
            <span class="card-dot" style="background: {cc(credOverall)}"></span>
          {/if}
          <div class="card-name card-name-edit" role="button" tabindex="0" on:click={() => openEditAccount(key)} on:keydown={onActivateKey(() => openEditAccount(key))} title="{$t('account.edit')}">{key}</div>
        </div>
        <div class="card-line">
          <span class="card-provider">{providerLabel(acct.provider)}</span>
          {#if simple}
            {#if credBroken(credOverall)}
              <button class="cred-badge cred-badge-err" on:click={() => openCredChange(key, acct.default_credential_type || 'gcm')}>{$t('simple.reconnect')}</button>
            {/if}
          {:else}
          <button class="cred-badge cred-badge-{credBadgeClass(credOverall)}"
            on:click={() => openCredChange(key, acct.default_credential_type || 'gcm')}
            title="Credential: {acct.default_credential_type || 'none'} — {credOverall}">{credBadgeLabel(credOverall, acct.default_credential_type)}</button>
          {/if}
          <span class="card-actions">
            <button class="card-btn card-btn-sm" on:click={() => openDiscover(key)} disabled={!canDiscover} title={canDiscover ? $t('account.findProjects') : DISCOVER_DISABLED}>{$t('account.find')}</button>
            <button class="card-btn card-btn-sm" on:click={() => openCreateRepo(key)} disabled={!canCreate} title={canCreate ? $t('account.createRepo') : CREATE_DISABLED}>{$t('account.new')}</button>
          </span>
        </div>
      </div>
      {/if}
    {/each}
    <button class="card card-add" on:click={() => { if (simple) connectModal = true; else addAccountDialog.open(); }} title={$t('account.add')}>
      <span class="card-add-icon">+</span>
    </button>
  </section>
  {/if}

  <!-- ── REPO LIST ── -->
  <section class="repo-list">
    {#each Object.entries($sources) as [sourceKey, source] (sourceKey)}
      {@const accountKey = source.account}
      {@const sourceCollapsed = collapsedSources.has(sourceKey)}
      {@const sourceRepoKeys = [...new Set((treeRows[sourceKey] || []).flatMap(row => [row.key, ...row.descendants]))]}
      {@const sourceIssues = sourceRepoKeys.filter(key => $repoStates[key] && $repoStates[key].status !== 'unknown' && !isSynced($repoStates[key])).length}
      {@const sourceUnsaved = sourceRepoKeys.filter(key => simpleUnsaved(($repoStates[key] || { status: 'unknown' }).status)).length}
      {#if sourceRepoKeys.length > 0}
      <div class="source-group">
        <div class="source-header" role="button" tabindex="0"
          aria-expanded={!sourceCollapsed}
          title={sourceCollapsed ? $t('collapse.expand') : $t('collapse.collapse')}
          on:click={(e) => { if (!(e.target instanceof Element && e.target.closest('.source-header-kebab'))) toggleSourceCollapsed(sourceKey); }}
          on:keydown={onActivateKey(() => toggleSourceCollapsed(sourceKey))}>
          <span class="collapse-chevron" aria-hidden="true">{sourceCollapsed ? '▸' : '▾'}</span>
          <span class="source-header-title">{sourceKey}</span>
          <div class="action-menu-container source-header-kebab">
            <button class="btn-kebab" on:click|stopPropagation={() => toggleAccountMenu(accountKey)} title={$t('account.actions')}>&#8942;</button>
            {#if actionMenuAccount === accountKey}
              <div transition:fade={{ duration: 80 }}>
                <LauncherMenu
                  kind="account"
                  editors={configEditors}
                  profiles={simple ? [] : configProfiles}
                  browserLabelOverride={simple ? $t('simple.openWeb') : ''}
                  aiHarnesses={configAIHarnesses}
                  {revealLabel}
                  onOpenBrowser={() => openAccountInBrowser(accountKey)}
                  onOpenFolder={() => openAccountInExplorer(accountKey)}
                  onOpenApp={(cmd) => openAccountInApp(accountKey, cmd)}
                  onOpenProfile={(id) => openAccountProfile(accountKey, id)}
                  onOpenAIHarness={(h) => openAccountInAIHarness(accountKey, h)}
                  onFind={() => { actionMenuAccount = null; openDiscover(accountKey); }}
                  findEnabled={canDiscoverWith(credStatuses[accountKey]?.status)}
                  findDisabledReason={DISCOVER_DISABLED}
                  onCreate={() => { actionMenuAccount = null; openCreateRepo(accountKey); }}
                  createEnabled={canCreateWith(credStatuses[accountKey]?.status)}
                  createDisabledReason={CREATE_DISABLED}
                />
              </div>
            {/if}
          </div>
          <span class="collapse-summary">
            {#if simple}
              {$t('simple.projects', { n: sourceRepoKeys.length })} ·
              {#if sourceRepoKeys.some(key => !$repoStates[key] || $repoStates[key].status === 'unknown')}
                {$t('simple.checking')}
              {:else if sourceUnsaved === 0}
                <span style="color:{sc('clean')}">{$t('simple.allSaved')}</span>
              {:else}
                <span style="color:{sc('dirty')}">{$t('simple.notSavedN', { n: sourceUnsaved })}</span>
              {/if}
            {:else}
            {$t('collapse.clones', { n: sourceRepoKeys.length })} ·
            {#if sourceRepoKeys.some(key => !$repoStates[key] || $repoStates[key].status === 'unknown')}
              {$t('simple.checking')}
            {:else if sourceIssues === 0}
              <span style="color:{sc('clean')}">{$t('account.allGood')}</span>
            {:else}
              <span style="color:{sc('behind')}">{sourceIssues} {sourceIssues === 1 ? $t('account.needsAttention') : $t('account.needAttentionPlural')}</span>
            {/if}
            {/if}
          </span>
        </div>
        {#if !sourceCollapsed}
        {#each (treeRows[sourceKey] || []) as { sourceKey: rowSourceKey, repoName, depth, childCount, descendants } (`${rowSourceKey}/${repoName}`)}
          {@const rowAccountKey = $sources[rowSourceKey].account}
          {@const repoKey = `${rowSourceKey}/${repoName}`}
          {@const isContainer = !!$sources[rowSourceKey]?.repos[repoName]?.container}
          {@const state = $repoStates[repoKey] || { status: 'unknown', progress: 0, behind: 0, modified: 0, untracked: 0, ahead: 0 }}
          {@const rowOpensDetail = opensDetail(state.status)}
          <div class="repo-row" class:repo-row-clickable={rowOpensDetail}
            class:repo-row-nested={depth > 0}
            style={depth > 0 ? `padding-left: ${10 + depth * 22}px` : ''}
            role="button"
            tabindex={rowOpensDetail || selectionMode ? 0 : -1}
            aria-disabled={!rowOpensDetail && !selectionMode}
            on:click={() => activateRepoRow(rowSourceKey, repoName, repoKey, state.status)}
            on:keydown={onActivateKey(() => activateRepoRow(rowSourceKey, repoName, repoKey, state.status))}>
            {#if rowSourceKey !== sourceKey}<span class="container-badge" title={rowSourceKey}>{rowAccountKey}</span>{/if}
            {#if selectionMode}
              <input type="checkbox" class="clone-select-box" checked={$selectedClones.has(repoKey)}
                on:click|stopPropagation
                on:change={() => toggleCloneSelection(repoKey)} title="{repoName}: {$t('repo.selectForWorkspace')}" />
            {:else if deleteMode}
              <button class="btn-delete-x" on:click|stopPropagation={() => askDelete(rowSourceKey, repoName, state.status)} title="Delete {repoName}">&#10005;</button>
            {/if}
            {#if childCount > 0}
              {@const repoCollapsed = collapsedRepos.has(repoKey)}
              <button class="collapse-chevron collapse-chevron-btn"
                aria-expanded={!repoCollapsed}
                aria-label="{repoCollapsed ? $t('collapse.expand') : $t('collapse.collapse')} {repoName}"
                title={repoCollapsed ? $t('collapse.expand') : $t('collapse.collapse')}
                on:click|stopPropagation={() => toggleRepoCollapsed(repoKey, descendants)}
                on:keydown|stopPropagation>{repoCollapsed ? '▸' : '▾'}</button>
            {/if}
            {#if simple}
              <!-- Simple row (issue #124): dot, short name, one sentence, one button, ⋯ -->
              {@const kind = simpleKind(state.status)}
              {@const kindColor = simpleColor(kind, $themeStore)}
              <span class="dot" class:spinning={rowBusy(state.status, !!fetchingRepos[repoKey])} style="color: {kindColor}">{rowBusy(state.status, !!fetchingRepos[repoKey]) ? '↻' : '●'}</span>
              {#if childCount > 0}
                <span class="repo-name" title={repoName}><span class="repo-name-toggle" role="button" tabindex="-1"
                  on:click={(e) => { e.stopPropagation(); toggleRepoCollapsed(repoKey, descendants); }}
                  on:keydown={onActivateKey(() => toggleRepoCollapsed(repoKey, descendants))}>{shortName(repoName)}</span></span>
              {:else}
                <span class="repo-name" title={repoName}>{shortName(repoName)}</span>
              {/if}
              {#if childCount > 0 && collapsedRepos.has(repoKey)}
                {@const belowUnsaved = descendants.filter((d) => simpleUnsaved(($repoStates[d] || { status: 'unknown' }).status)).length}
                <span class="collapse-summary collapse-summary-row">
                  {$t('collapse.below', { n: descendants.length })}{#if belowUnsaved > 0} · <span style="color:{sc('dirty')}">{$t('simple.notSavedN', { n: belowUnsaved })}</span>{/if}
                </span>
              {/if}
              {#if state.status === 'syncing' || state.status === 'cloning'}
                <div class="progress-track">
                  <div class="progress-fill" style="width:{state.progress}%; background:{kindColor}"></div>
                </div>
                <span class="progress-pct" style="color:{kindColor}">{state.progress}%</span>
              {:else}
                <span class="status-text" style="color: {kindColor}" title={simpleTip(kind, state)}>{$t(simpleLabelKey(kind))}</span>
                {#if kind === 'update'}
                  <button class="btn-action" on:click|stopPropagation={() => syncRepo(rowSourceKey, repoName)}>{$t('simple.btnUpdate')}</button>
                {:else if kind === 'online'}
                  <button class="btn-action" on:click|stopPropagation={() => cloneRepo(rowSourceKey, repoName)}>{$t('simple.btnDownload')}</button>
                {/if}
                {#if kind !== 'online' && kind !== 'busy'}
                  {@render quickLaunch(repoKey)}
                {/if}
              {/if}
              {#if kind !== 'online'}
                <div class="action-menu-container">
                  <button class="btn-kebab" on:click|stopPropagation={() => toggleActionMenu(repoKey)} title={$t('repo.actions')}>&#8942;</button>
                  {#if actionMenuRepo === repoKey}
                    <div transition:fade={{ duration: 80 }}>
                      <LauncherMenu
                        kind="repo"
                        editors={configEditors}
                        profiles={[]}
                        aiHarnesses={configAIHarnesses}
                        {revealLabel}
                        browserLabelOverride={$t('simple.openWeb')}
                        onOpenBrowser={() => openRepoInBrowser(rowSourceKey, repoName)}
                        onOpenFolder={() => openRepoInExplorer(repoKey)}
                        onOpenApp={(cmd) => openRepoInApp(repoKey, cmd)}
                        onOpenAIHarness={(h) => openRepoInAIHarness(repoKey, h)}
                        onDelete={() => { actionMenuRepo = null; askDelete(rowSourceKey, repoName, state.status); }}
                        deleteLabel={$t('simple.delete')}
                      />
                    </div>
                  {/if}
                </div>
              {/if}
            {:else}
            <span class="dot" class:spinning={rowBusy(state.status, !!fetchingRepos[repoKey])} style="color: {sc(state.status)}">{rowGlyph(state.status, !!fetchingRepos[repoKey])}</span>
            {#if membershipsFor(repoKey).length > 0}
              <div class="ws-badge-wrap">
                <button class="ws-badge"
                        title="Member of: {membershipsFor(repoKey).map(workspaceLabel).join(', ')}"
                        on:click|stopPropagation={() => toggleWsPopover(repoKey)}>
                  <svg class="ws-icon" viewBox="0 0 16 16" width="13" height="13" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round" stroke-linecap="round" aria-hidden="true">
                    <path d="M2 5.5 L8 2.5 L14 5.5 L8 8.5 Z"/>
                    <path d="M2 8.5 L8 11.5 L14 8.5"/>
                    <path d="M2 11.5 L8 14.5 L14 11.5"/>
                  </svg>
                </button>
                {#if openWsPopover === repoKey}
                  <div class="ws-popover" role="presentation" transition:fade={{ duration: 80 }} on:click|stopPropagation on:keydown|stopPropagation>
                    <div class="ws-popover-title">{$t('workspace.openWorkspace')}</div>
                    {#each membershipsFor(repoKey) as wsKey}
                      <button class="ws-popover-item"
                              on:click={() => openWorkspaceFromBadge(wsKey)}
                              disabled={workspaceBusy}>
                        <svg class="ws-icon" viewBox="0 0 16 16" width="13" height="13" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round" stroke-linecap="round" aria-hidden="true">
                          <path d="M2 5.5 L8 2.5 L14 5.5 L8 8.5 Z"/>
                          <path d="M2 8.5 L8 11.5 L14 8.5"/>
                          <path d="M2 11.5 L8 14.5 L14 11.5"/>
                        </svg>
                        {workspaceLabel(wsKey)}
                      </button>
                    {/each}
                  </div>
                {/if}
              </div>
            {/if}
            {#if childCount > 0}
              <!-- The name folds its clones too; selection mode keeps the row's own click. -->
              <span class="repo-name"><span class="repo-name-toggle" role="button" tabindex="-1"
                title={collapsedRepos.has(repoKey) ? $t('collapse.expand') : $t('collapse.collapse')}
                on:click={(e) => { if (selectionMode) return; e.stopPropagation(); toggleRepoCollapsed(repoKey, descendants); }}
                on:keydown={onActivateKey(() => toggleRepoCollapsed(repoKey, descendants))}>{repoName}</span></span>
            {:else}
              <span class="repo-name">{repoName}</span>
            {/if}
            {#if childCount > 0 && collapsedRepos.has(repoKey)}
              {@const below = descendants.filter(key => $repoStates[key] && $repoStates[key].status !== 'unknown' && !isSynced($repoStates[key])).length}
              <span class="collapse-summary collapse-summary-row">
                {$t('collapse.below', { n: descendants.length })}{#if below > 0} · <span style="color:{sc('behind')}">{below} {below === 1 ? $t('account.needsAttention') : $t('account.needAttentionPlural')}</span>{/if}
              </span>
            {/if}
            {#if isContainer}
              <span class="container-badge" title="Multi-repo container — holds nested clones">&#128193; container</span>
            {:else if isTentativeContainer(rowSourceKey, repoName, tentativeContainers)}
              <button class="tentative-container-badge"
                      on:click|stopPropagation={() => toggleContainer(rowSourceKey, repoName)}
                      title="This repo holds nested clones — mark it as a container to onboard them">&#128193; onboard nested clones</button>
            {/if}
            {#if state.branch === '(detached)'}
              <span class="branch-badge detached">detached</span>
            {:else if state.branch && !state.isDefault}
              <span class="branch-badge">{state.branch}</span>
            {/if}

            {#if state.status === 'syncing' || state.status === 'cloning'}
              <div class="progress-track">
                <div class="progress-fill" style="width:{state.progress}%; background:{sc(state.status)}"></div>
              </div>
              <span class="progress-pct" style="color:{sc(state.status)}">{state.progress}%</span>
            {:else}
              {#if state.status === 'unknown'}<span class="status-pending">{$t('simple.checking')}</span>{/if}
              {#if state.status !== 'unknown'}
              {@const label = advancedLabel(state.status, state.isDefault)}
              {#if !deleteMode && $prSettings.enabled}
                {@const prSummary = lookupPRSummary($prsByAccount, rowAccountKey, repoName)}
                {@const acctForPRs = $accounts[rowAccountKey]}
                {#if prSummary.authored.length > 0}
                  <div class="action-menu-container pr-badge-wrap">
                    <button class="pr-badge pr-badge-authored" on:click|stopPropagation={() => togglePRPopover(`${repoKey}|authored`)} title="{prSummary.authored.length} open PR{prSummary.authored.length === 1 ? '' : 's'} I authored">
                      &#128221; {prSummary.authored.length}
                    </button>
                    {#if openPRPopover === `${repoKey}|authored`}
                      <div transition:fade={{ duration: 80 }}>
                        <PRPopover
                          kind="authored"
                          prs={prSummary.authored}
                          providerName={acctForPRs?.provider || ''}
                          providerAllURL={providerPRsURL(acctForPRs?.provider || '', acctForPRs?.url || '', repoName)}
                          onOpenPR={(url) => { bridge.openInBrowser(url); openPRPopover = null; }}
                          onOpenAll={() => { const u = providerPRsURL(acctForPRs?.provider || '', acctForPRs?.url || '', repoName); if (u) bridge.openInBrowser(u); openPRPopover = null; }}
                        />
                      </div>
                    {/if}
                  </div>
                {/if}
                {#if prSummary.reviewRequested.length > 0}
                  <div class="action-menu-container pr-badge-wrap">
                    <button class="pr-badge pr-badge-review" on:click|stopPropagation={() => togglePRPopover(`${repoKey}|review`)} title="{prSummary.reviewRequested.length} PR{prSummary.reviewRequested.length === 1 ? '' : 's'} awaiting my review">
                      &#128064; {prSummary.reviewRequested.length}
                    </button>
                    {#if openPRPopover === `${repoKey}|review`}
                      <div transition:fade={{ duration: 80 }}>
                        <PRPopover
                          kind="review"
                          prs={prSummary.reviewRequested}
                          providerName={acctForPRs?.provider || ''}
                          providerAllURL={providerPRsURL(acctForPRs?.provider || '', acctForPRs?.url || '', repoName)}
                          onOpenPR={(url) => { bridge.openInBrowser(url); openPRPopover = null; }}
                          onOpenAll={() => { const u = providerPRsURL(acctForPRs?.provider || '', acctForPRs?.url || '', repoName); if (u) bridge.openInBrowser(u); openPRPopover = null; }}
                        />
                      </div>
                    {/if}
                  </div>
                {/if}
              {/if}
              <span class="status-badges">
                {#if label}
                  <span class="status-text" style="color:{sc(label.color)}">{$t(label.key)}</span>
                {:else}
                  <span class="status-pending">{$t('repo.pending')}</span>
                  {#each COUNT_BADGES as b}
                    {#if state[b.field] > 0}<span class="sbadge" style="color:{sc(b.color)}" title="{state[b.field]} {b.word}">{b.glyph}{state[b.field]}</span>{/if}
                  {/each}
                {/if}
              </span>
              {#if state.status === 'behind'}
                <button class="btn-action" on:click|stopPropagation={() => syncRepo(rowSourceKey, repoName)}>{$t('repo.pull')}</button>
              {:else if state.status === 'not cloned'}
                <button class="btn-action" on:click|stopPropagation={() => cloneRepo(rowSourceKey, repoName)}>{$t('repo.bringLocal')}</button>
              {/if}
              {/if}
              {#if !deleteMode && state.status !== 'not cloned'}
                {@render quickLaunch(repoKey)}
                <button class="btn-fetch" class:spinning={fetchingRepos[repoKey]} on:click|stopPropagation={() => fetchRepo(rowSourceKey, repoName)} title={$t('repo.fetchOrigin')} disabled={!!fetchingRepos[repoKey]}>&#8635;</button>
                <div class="action-menu-container">
                  <button class="btn-kebab" on:click|stopPropagation={() => toggleActionMenu(repoKey)} title={$t('repo.actions')}>&#8942;</button>
                  {#if actionMenuRepo === repoKey}
                    <div transition:fade={{ duration: 80 }}>
                      <LauncherMenu
                        kind="repo"
                        editors={configEditors}
                        profiles={configProfiles}
                        aiHarnesses={configAIHarnesses}
                        {revealLabel}
                        onOpenBrowser={() => openRepoInBrowser(rowSourceKey, repoName)}
                        onOpenFolder={() => openRepoInExplorer(repoKey)}
                        onOpenApp={(cmd) => openRepoInApp(repoKey, cmd)}
                        onOpenProfile={(id) => openRepoProfile(repoKey, id)}
                        onOpenAIHarness={(h) => openRepoInAIHarness(repoKey, h)}
                        onSweep={() => sweepBranches(rowSourceKey, repoName)}
                        onMove={() => { actionMenuRepo = null; openMoveRepo(rowSourceKey, repoName); }}
                        moveEnabled={repoMoveDisabledReason(state) === ''}
                        moveDisabledReason={repoMoveDisabledReason(state)}
                        isContainer={isContainerRepo(rowSourceKey, repoName)}
                        onToggleContainer={() => { actionMenuRepo = null; toggleContainer(rowSourceKey, repoName); }}
                        onRescanContainer={() => { actionMenuRepo = null; rescanContainer(); }}
                      />
                    </div>
                  {/if}
                </div>
              {/if}
            {/if}
            {/if}
          </div>
          {#if expandedRepo === repoKey}
            <div class="repo-detail" transition:slide={{ duration: 150 }}>
              {#if simple && (state.status === 'upstream gone' || repoDetail?.upstreamGone)}
                <div class="detail-gone">
                  <p class="detail-gone-body" style="color:{sc('upstream gone')}">&#9888; {$t('simple.gone')}</p>
                </div>
              {:else if state.status === 'upstream gone' || repoDetail?.upstreamGone}
                <div class="detail-gone">
                  <div class="detail-gone-title" style="color:{sc('upstream gone')}">&#9888; WARNING: The origin repository is gone</div>
                  <p class="detail-gone-body">
                    The remote repository for this clone was not found on the provider. It was
                    either deleted, made private without re-granting access, renamed without a
                    redirect, or the credential lost permission to see it.
                  </p>
                  <p class="detail-gone-body">
                    <strong>Your local clone is intact.</strong> You have two options:
                  </p>
                  <ul class="detail-gone-list">
                    <li><strong>Keep it as a local-only clone.</strong> Do nothing — the folder stays on disk with its full git history. Gitbox will keep flagging it as "Upstream gone" until you remove the remote or the entry.</li>
                    <li><strong>Delete it</strong> (trash icon in the top bar) if you no longer need the code. Since the upstream is gone, this local copy may be the only remaining one — make sure before confirming.</li>
                  </ul>
                </div>
              {:else if detailLoading}
                <span class="detail-loading">Loading...</span>
              {:else if repoDetail?.error}
                <span class="detail-error">{repoDetail.error}</span>
              {:else if repoDetail}
                {#if !simple}
                <div class="detail-header">
                  <span class="detail-branch">Branch: <strong>{repoDetail.branch}</strong></span>
                  {#if repoDetail.ahead > 0}
                    <span class="detail-badge" style="color:{sc(countBadge('ahead').color)}">{repoDetail.ahead} {countBadge('ahead').word}</span>
                  {/if}
                  {#if repoDetail.behind > 0}
                    <span class="detail-badge" style="color:{sc(countBadge('behind').color)}">{repoDetail.behind} {countBadge('behind').word}</span>
                  {/if}
                </div>
                {/if}
                {#if repoDetail.changed.length > 0}
                  <div class="detail-section-title">{simple ? $t('simple.changedFiles') : 'Changed files'} <span class="sbadge" style="color:{sc(countBadge('modified').color)}">{countBadge('modified').glyph}{repoDetail.changed.length}</span></div>
                  {#each repoDetail.changed as file}
                    <div class="detail-file">
                      <span class="detail-kind" class:kind-added={file.kind === 'added'} class:kind-deleted={file.kind === 'deleted'} class:kind-conflict={file.kind === 'conflict'} title={kindLabel(file.kind)}>{kindIcon(file.kind)}</span>
                      <span class="detail-path">{file.path}</span>
                    </div>
                  {/each}
                {/if}
                {#if repoDetail.untracked.length > 0}
                  <div class="detail-section-title">{simple ? $t('simple.newFiles') : 'New files (untracked)'} <span class="sbadge" style="color:{sc(countBadge('untracked').color)}">{countBadge('untracked').glyph}{repoDetail.untracked.length}</span></div>
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
        {/if}
      </div>
      {/if}
    {/each}

  </section>

  {:else if cardsTab === 'mirrors'}
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
  {#if cardsVisible}
  <section class="cards-row" transition:slide={{ duration: 150 }}>
    {#each Object.entries($mirrors) as [mirrorKey, mir]}
      {@const mstats = mirrorGroupStats[mirrorKey] || { active: 0, unchecked: 0, error: 0, total: 0 }}
      {@const mcolor = mstats.error > 0 ? sc('error') : mstats.active === mstats.total && mstats.total > 0 ? sc('clean') : sc('behind')}
      {@const mstate = mstats.error > 0 ? `${mstats.error} error${mstats.error > 1 ? 's' : ''}` : mstats.unchecked > 0 ? `${mstats.unchecked} ${$t('mirror.unchecked')}` : mstats.total > 0 ? $t('mirror.allSynced') : $t('mirror.noClones')}
      {#if expandedCard !== `mir:${mirrorKey}`}
      <div class="card-chip" role="button" tabindex="0" title={mstate}
        on:click|stopPropagation={() => expandedCard = `mir:${mirrorKey}`}
        on:keydown={onActivateKey(() => expandedCard = `mir:${mirrorKey}`)}>
        {#if deleteMode}
          <button class="btn-delete-x card-delete-btn" on:click|stopPropagation={() => deleteMirrorGroupDialog.ask(mirrorKey)} title="Delete mirror group {mirrorKey}">&#10005;</button>
        {:else}
          <span class="card-dot" style="background: {mcolor}"></span>
        {/if}
        <span class="card-chip-name">{mir.account_src} ↔ {mir.account_dst}</span>
      </div>
      {:else}
      <div class="card card-mirror card-open" class:card-delete-mode={deleteMode}>
        <div class="card-top">
          {#if deleteMode}
            <button class="btn-delete-x card-delete-btn" on:click={() => deleteMirrorGroupDialog.ask(mirrorKey)} title="Delete mirror group {mirrorKey}">&#10005;</button>
          {:else}
            <span class="card-dot" style="background: {mcolor}"></span>
          {/if}
          <div class="card-name card-mirror-name">{mir.account_src} ↔ {mir.account_dst}</div>
        </div>
        <div class="card-line">
          <span class="card-provider">MIRROR</span>
          <span class="card-count" style="color: {mcolor}" title={mstate}>{mstats.active}/{mstats.total}</span>
          <span class="card-actions">
            <button class="card-btn card-btn-sm" on:click={() => checkMirrorStatus(mirrorKey)} title={$t('mirror.checkStatus')}>{$t('mirror.check')}</button>
          </span>
        </div>
      </div>
      {/if}
    {/each}
    <button class="card card-add" on:click={() => addMirrorGroupDialog.open()} title={$t('mirror.addGroup')}>
      <span class="card-add-icon">+</span>
    </button>
  </section>
  {/if}

  <!-- ── MIRROR DETAIL LIST ── -->
  {#if Object.keys($mirrors).length > 0}
  <section class="repo-list">
    <div class="mirror-list">
    <div class="mirror-section-header">
      <h3>{$t('tab.mirrors')}</h3>
    </div>

    {#each Object.entries($mirrors) as [mirrorKey, mir]}
      <div class="mirror-group">
        <div class="mirror-group-header">
          <span class="mirror-accounts">{mir.account_src} <span class="mirror-arrow">↔</span> {mir.account_dst}</span>
          <div class="mirror-group-actions">
            {#if mirrorChecking[mirrorKey]}
              <span class="mirror-checking"><div class="spinner-sm"></div></span>
            {/if}
            <button class="btn-tab-action" on:click={() => addMirrorRepoDialog.open(mirrorKey)}>{$t('mirror.addRepo')}</button>
            {#if deleteMode}
              <button class="btn-sm btn-danger" on:click={() => deleteMirrorGroupDialog.ask(mirrorKey)}>✕</button>
            {/if}
          </div>
        </div>

        {#each (mir.repoOrder && mir.repoOrder.length > 0 ? mir.repoOrder : Object.keys(mir.repos)) as repoName}
          {@const repo = mir.repos[repoName]}
          {@const live = $mirrorStates[`${mirrorKey}/${repoName}`]}
          {@const settingUp = !!mirrorSettingUp[`${mirrorKey}/${repoName}`]}
          <div class="mirror-row">
            {#if settingUp}
              <span class="mirror-dot"><div class="spinner-sm"></div></span>
            {:else}
              <span class="mirror-dot" style="color:{mirrorStatusColor(repo, live, $themeStore)}">{mirrorStatusSymbol(repo, live)}</span>
            {/if}
            <span class="mirror-repo-name">{repoName}</span>
            <span class="mirror-direction">{@html mirrorDirLabelHtml(repo, mir)}</span>
            {#if settingUp}
              <span class="mirror-status-text">{$t('mirror.settingUp')}</span>
            {:else}
              <span class="mirror-status-text" style="color:{mirrorStatusColor(repo, live, $themeStore)}">{mirrorStatusText(repo, live)}</span>
            {/if}
            {#if live?.warning}
              <span class="mirror-warning" title={live.warning}>⚠</span>
            {/if}
            {#if live?.error && live.error.startsWith('missing API token in ') && !deleteMode}
              {@const errAcct = live.error.replace('missing API token in ', '')}
              <button class="btn-sm btn-fix" on:click={() => openTokenSetup(errAcct)} title="Set up API token for {errAcct}">{$t('mirror.fixCredentials')}</button>
            {:else if !deleteMode}
              <button class="btn-sm btn-setup" on:click={() => setupMirrorRepo(mirrorKey, repoName)} disabled={settingUp}>{$t('mirror.setup')}</button>
            {/if}
            {#if deleteMode}
              <button class="btn-sm btn-danger" on:click={() => deleteMirrorRepoDialog.ask(mirrorKey, repoName)}>✕</button>
            {/if}
          </div>
        {/each}

        {#if Object.keys(mir.repos).length === 0}
          <div class="mirror-empty">{$t('mirror.empty')}</div>
        {/if}
      </div>
    {/each}
    </div>
  </section>
  {/if}

  {:else}
  <!-- ── WORKSPACE CARDS ── -->
  {#if cardsVisible}
  <section class="cards-row" transition:slide={{ duration: 150 }}>
    {#each $workspaceOrder.length > 0 ? $workspaceOrder : Object.keys($workspaces) as wsKey}
      {@const ws = $workspaces[wsKey]}
      {#if ws}
        {@const members = ws.members?.length ?? 0}
        {#if expandedCard !== `ws:${wsKey}`}
        <div class="card-chip" role="button" tabindex="0" title={ws.name || wsKey}
          on:click|stopPropagation={() => expandedCard = `ws:${wsKey}`}
          on:keydown={onActivateKey(() => expandedCard = `ws:${wsKey}`)}>
          <span class="card-dot" style="background: {sc('clean')}"></span>
          <span class="card-chip-name">{ws.name || wsKey}</span>
        </div>
        {:else}
        <div class="card card-workspace card-open">
          <div class="card-top">
            <span class="card-dot" style="background: {sc('clean')}"></span>
            <div class="card-name">{ws.name || wsKey}</div>
          </div>
          <div class="card-line">
            <span class="card-provider">CODE</span>
            <span class="card-count" title="{members} member{members === 1 ? '' : 's'}">{members}</span>
            <span class="card-actions">
              <button class="card-btn card-btn-sm" on:click={() => openWorkspace(wsKey)} disabled={workspaceBusy}>{$t('workspace.open')}</button>
            </span>
          </div>
        </div>
        {/if}
      {/if}
    {/each}
  </section>
  {/if}

  <!-- ── WORKSPACE DETAIL LIST ── -->
  {#if Object.keys($workspaces).length > 0}
  <section class="repo-list">
    <div class="mirror-list">
      <div class="mirror-section-header">
        <h3>{$t('tab.workspaces')}</h3>
      </div>
      {#each $workspaceOrder.length > 0 ? $workspaceOrder : Object.keys($workspaces) as wsKey}
        {@const ws = $workspaces[wsKey]}
        {#if ws}
          <div class="mirror-group">
            <div class="mirror-group-header">
              <span class="mirror-accounts">{ws.name || wsKey} <span class="workspace-type">· .code-workspace</span></span>
              <div class="mirror-group-actions">
                <button class="btn-tab-action" on:click={() => openWorkspace(wsKey)} disabled={workspaceBusy}>{$t('workspace.open')}</button>
              </div>
            </div>
            {#if ws.file}
              <div class="workspace-file-row">
                <span class="workspace-file-label">{$t('workspace.file')}</span>
                <span class="workspace-file-path" title={ws.file}>{ws.file}</span>
              </div>
            {/if}
            {#if (ws.members?.length ?? 0) === 0}
              <div class="mirror-empty">{$t('workspace.noMembers')}</div>
            {:else}
              {#each ws.members as m}
                <div class="mirror-row">
                  <span class="mirror-dot" style="color: {sc('clean')}">●</span>
                  <span class="mirror-repo-name">{m.source}/{m.repo}</span>
                </div>
              {/each}
            {/if}
          </div>
        {/if}
      {/each}
    </div>
  </section>
  {:else}
  <section class="repo-list">
    <div class="workspace-empty-hint">
      {$t('workspace.empty')}
    </div>
  </section>
  {/if}
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
  <MirrorTokenModal bind:mirrorCredWarning {storeMirrorPat} />

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

  @keyframes spin { to { transform: rotate(360deg); } }


  .spinning { animation: spin 0.8s linear infinite; color: var(--spin-color) !important; stroke: var(--spin-color); stroke-width: 2.5; font-weight: 900; }
  @keyframes spin { to { transform: rotate(360deg); } }

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


  /* Chips and the open card are as wide as their content, so names are
     never cut; the row wraps instead of scrolling. */
  .cards-row { display: flex; flex-wrap: wrap; align-items: flex-start; gap: 8px; padding: 10px 24px; flex-shrink: 0; }
  .card-chip {
    display: inline-flex; align-items: center; gap: 6px;
    padding: 4px 10px; border: 1px solid var(--border); border-radius: 999px;
    background: var(--bg-card); box-shadow: var(--card-shadow);
    cursor: pointer; transition: border-color 0.15s, background 0.15s;
    white-space: nowrap;
  }
  .card-chip:hover { border-color: var(--border-hover); }
  .card-chip:focus-visible { outline: 1px solid var(--border-hover); outline-offset: 1px; }
  .card-chip-name { font-size: 12px; font-weight: 600; color: var(--text-primary); }
  .card {
    flex: 0 0 auto; background: var(--bg-card); border: 1px solid var(--border);
    border-radius: 10px; padding: 8px 12px; transition: border-color 0.15s, box-shadow 0.15s;
    box-shadow: var(--card-shadow);
  }
  .card:hover { border-color: var(--border-hover); }
  .card-top { display: flex; align-items: center; gap: 6px; margin-bottom: 4px; }
  .card-count { font-size: 10px; font-weight: 700; color: var(--text-muted); }
  .card-dot { width: 7px; height: 7px; border-radius: 50%; }
  .card-provider { font-size: 10px; color: var(--text-muted); font-weight: 600; letter-spacing: 0.3px; }
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
  .cred-badge-offline { background: #450a0a; border-color: #D81E5B; color: #fca5a5; }
  .cred-badge-pending { background: #27272a; border-color: #52525b; color: #a1a1aa; animation: pulse-badge 1.5s ease-in-out infinite; }
  @keyframes pulse-badge { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }
  :global([data-theme="light"]) .cred-badge-offline { background: #fee2e2; border-color: #be123c; color: #be123c; }
  :global([data-theme="light"]) .cred-badge-pending { background: #f4f4f5; border-color: #a1a1aa; color: #71717a; }
  :global([data-theme="light"]) .cred-badge-ok { background: #dcfce7; border-color: #166534; color: #166534; }
  :global([data-theme="light"]) .cred-badge-err { background: #fee2e2; border-color: #be123c; color: #be123c; }
  :global([data-theme="light"]) .cred-badge-warn { background: #ffedd5; border-color: #c2410c; color: #c2410c; }
  :global([data-theme="light"]) .cred-badge-none { background: #dbeafe; border-color: #2563eb; color: #2563eb; }
  .card-delete-btn { flex-shrink: 0; }
  .repo-row-nested { position: relative; }
  .repo-row-nested::before {
    content: ''; position: absolute; left: 18px; top: 0; bottom: 0;
    border-left: 2px solid var(--border); opacity: 0.6;
  }
  .container-badge {
    font-size: 10px; color: var(--text-secondary); border: 1px solid var(--border);
    border-radius: 4px; padding: 0 5px; margin-left: 6px; white-space: nowrap;
    flex: 0 0 auto;
  }
  /* Actionable hint on a repo that looks like a container but isn't flagged.
     Clicking it flags the container (which scans + opens the adopt modal). */
  .tentative-container-badge {
    font-size: 10px; color: var(--accent, #4B95E9);
    border: 1px solid var(--accent, #4B95E9); background: transparent;
    border-radius: 4px; padding: 0 5px; margin-left: 6px; white-space: nowrap;
    flex: 0 0 auto; cursor: pointer; line-height: 1.6;
  }
  .tentative-container-badge:hover {
    background: var(--accent, #4B95E9); color: #fff;
  }
  .card-name { font-size: 14px; font-weight: 600; margin-bottom: 8px; }
  .card-mirror-name { font-size: 12px; }
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

  /* Line 1 holds the status dot and the full name; line 2 the provider,
     badge or count on the left and the one-word buttons on the right. */
  .card-top .card-name { margin-bottom: 0; white-space: nowrap; }
  .card-line { display: flex; align-items: center; gap: 6px; }
  .card-actions { margin-left: auto; display: flex; gap: 4px; padding-left: 8px; }
  .card-btn {
    width: 100%; padding: 5px 0; background: transparent; border: 1px solid var(--border);
    color: var(--text-secondary); border-radius: 6px; cursor: pointer; font-size: 11px; font-weight: 500;
    transition: all 0.15s;
  }
  .card-btn:hover:not(:disabled) { background: var(--bg-hover); color: var(--text-primary); border-color: var(--border-hover); }
  .card-btn:disabled { opacity: 0.35; cursor: default; }
  .card-btn-sm { width: auto; flex: 0 0 auto; padding: 2px 8px; }

  .repo-list { flex: 1; padding: 0 24px 12px; overflow-y: auto; min-height: 0; }
  .source-group { margin-bottom: 6px; }
  .source-header {
    font-size: 11px; font-weight: 600; color: var(--text-dim);
    text-transform: uppercase; letter-spacing: 0.8px;
    padding: 10px 0 5px; border-bottom: 1px solid var(--border);
    display: flex; align-items: center; gap: 4px;
  }
  .source-header[role="button"] { cursor: pointer; user-select: none; }
  .source-header-title { flex: 0 0 auto; }
  .collapse-chevron { flex: 0 0 auto; width: 18px; text-align: center; font-size: 16px; line-height: 1; opacity: 0.85; }
  .collapse-chevron-btn { background: none; border: none; padding: 0; color: inherit; cursor: pointer; line-height: 1; }
  .collapse-chevron-btn:hover, .source-header[role="button"]:hover .collapse-chevron { opacity: 1; }
  .collapse-summary { flex: 0 0 auto; font-size: 11px; font-weight: normal; opacity: 0.8; }
  .collapse-summary-row { margin-left: 6px; }
  .repo-name-toggle { cursor: pointer; }
  .repo-name-toggle:hover { text-decoration: underline; }
  .source-header-kebab { font-size: 14px; flex: 0 0 auto; margin-right: auto; }
  .repo-row {
    display: flex; align-items: center; gap: 10px;
    padding: 8px 6px; border-radius: 6px; transition: background 0.1s;
  }
  .repo-row:hover { background: var(--bg-row-hover); }

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

  /* Repo-row launchers (both modes): icon-only AI, editor and terminal buttons. */
  .launch-group { display: inline-flex; align-items: center; gap: 2px; flex-shrink: 0; }
  .btn-launch {
    display: inline-flex; align-items: center; justify-content: center;
    width: 24px; height: 24px; padding: 0; border: none; border-radius: 5px;
    background: transparent; color: var(--text-secondary); cursor: pointer;
    transition: background 0.12s, color 0.12s;
  }
  .btn-launch:hover { background: var(--bg-hover); color: var(--text-primary); }

  .btn-kebab {
    background: transparent; border: none; color: var(--text-dim); cursor: pointer;
    font-size: 16px; padding: 2px 4px; border-radius: 4px; transition: color 0.15s;
    flex-shrink: 0; line-height: 1;
  }
  .btn-kebab:hover { color: var(--text-primary); }
  .action-menu-container { position: relative; }
  .status-badges { display: flex; align-items: center; gap: 6px; }
  .sbadge { font-size: 11px; font-weight: 600; white-space: nowrap; }
  .status-pending { font-size: 12px; font-weight: 600; color: var(--text-dim); }
  .branch-badge { font-size: 10px; padding: 1px 5px; border-radius: 3px; background: var(--bg-hover); color: var(--text-dim); white-space: nowrap; }
  .branch-badge.detached { color: var(--status-error, #D81E5B); }

  /* PR indicators (issue #29) */
  .pr-badge-wrap { position: relative; display: inline-flex; }
  .pr-badge {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: 11px;
    font-weight: 600;
    padding: 2px 7px;
    border-radius: 10px;
    border: 1px solid transparent;
    background: var(--bg-hover);
    color: var(--text-secondary);
    cursor: pointer;
    white-space: nowrap;
    transition: background 0.1s, border-color 0.1s, color 0.1s;
  }
  .pr-badge:hover { background: var(--bg-row-hover); color: var(--text-primary); }
  .pr-badge-authored { color: var(--text-secondary); }
  .pr-badge-review {
    color: var(--status-error, #D81E5B);
    border-color: color-mix(in srgb, var(--status-error, #D81E5B) 40%, transparent);
    background: color-mix(in srgb, var(--status-error, #D81E5B) 12%, var(--bg-hover));
  }
  .pr-badge-review:hover {
    background: color-mix(in srgb, var(--status-error, #D81E5B) 22%, var(--bg-hover));
    color: var(--status-error, #D81E5B);
  }

  /* ── Repo detail panel ── */
  .repo-row-clickable { cursor: pointer; }
  .repo-row-clickable:hover { background: var(--bg-row-hover); }
  .repo-detail {
    margin: 0 0 2px 0; padding: 8px 16px 10px 30px;
    background: var(--bg-card); border: 1px solid var(--border); border-radius: 6px;
    max-height: 180px; overflow-y: auto; font-size: 12px;
  }
  .detail-loading, .detail-error, .detail-clean {
    color: var(--text-muted); font-style: italic;
  }
  .detail-error { color: var(--text-primary); }
  .detail-gone {
    display: flex; flex-direction: column; gap: 6px;
    font-size: 12px; line-height: 1.45;
  }
  .detail-gone-title { font-weight: 600; font-size: 12.5px; }
  .detail-gone-body { margin: 0; color: var(--text-secondary); }
  .detail-gone-list {
    margin: 2px 0 0 0; padding-left: 18px;
    color: var(--text-secondary);
  }
  .detail-gone-list li + li { margin-top: 4px; }
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












  /* ── Add account card ── */
  .card-add {
    display: flex; align-items: center; justify-content: center;
    padding: 3px 12px; border-radius: 999px; box-shadow: none;
    cursor: pointer; border-style: dashed; background: transparent;
    color: var(--text-muted); transition: all 0.15s;
  }
  .card-add:hover { border-color: var(--border-hover); color: var(--text-primary); background: var(--bg-hover); }
  .card-add-icon { font-size: 16px; font-weight: 400; line-height: 1; }


  .btn-delete-x {
    background: none; border: 1px solid #D81E5B55; color: #D81E5B;
    cursor: pointer; font-size: 9px; font-weight: 600;
    width: 16px; height: 16px; padding: 0; flex-shrink: 0;
    border-radius: 3px; display: flex; align-items: center; justify-content: center;
    opacity: 0.7; transition: all 0.12s; line-height: 1;
  }
  .btn-delete-x:hover { opacity: 1; background: #D81E5B22; border-color: #D81E5B; }








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
  .compact-acct-cred-offline { background: #3a161b; }
  .compact-acct-cred-offline:hover { background: #4a1c23; }
  :global([data-theme="light"]) .compact-acct-cred-offline { background: #fee2e2; }
  :global([data-theme="light"]) .compact-acct-cred-offline:hover { background: #fcd5d5; }
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
  .btn-setup { font-size: 10px; padding: 1px 6px; }
  .btn-fix { font-size: 10px; padding: 1px 6px; background: #431407; border-color: #c2410c; color: #fdba74; }
  .spinner-sm { width: 12px; height: 12px; border: 2px solid var(--border); border-top-color: var(--text-primary); border-radius: 50%; animation: spin 0.6s linear infinite; }


  /* Compact mirror pill */
  .compact-mirror-pill {
    display: flex; align-items: center; gap: 4px;
    padding: 2px 8px; font-size: 11px; color: var(--text-muted);
  }
  .compact-mirror-dot { font-size: 10px; }
  .compact-mirror-label { font-weight: 500; }

  /* ── Workspaces (issue #27 / #49) ────────────────────────────────── */

  /* Per-row checkbox shown in selection mode. Sized to match the delete-X
     button it visually replaces so the row layout stays stable. */
  .clone-select-box {
    width: 14px; height: 14px;
    margin: 0 6px 0 0;
    accent-color: var(--text-primary);
    cursor: pointer;
    flex: 0 0 auto;
  }

  /* Small membership count chip on clone rows. Sits beside the branch
     badge and stays subtle — presence of the chip at all is already the
     signal the clone is in a workspace. */
  /* Negative margins claw back the .repo-row `gap: 10px` on each side so
     the badge sits visually flush against the status dot and the repo
     name, instead of looking like a 50px-wide gutter. */
  .ws-badge-wrap {
    position: relative;
    display: inline-flex;
    align-items: center;
    flex: 0 0 auto;
    margin-left: -6px;
    margin-right: -4px;
  }
  /* Now an action button: clicking it opens the workspace popover. The icon
     alone signals membership; the count is gone (the popover lists the
     workspaces by name). Compact footprint — one emoji glyph, tight padding,
     no border. */
  .ws-badge {
    line-height: 1;
    padding: 2px 3px;
    border: none;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    border-radius: 4px;
    transition: background 0.12s, color 0.12s;
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .ws-badge:hover { background: var(--bg-hover); color: var(--text-primary); }
  .ws-icon { display: block; }
  .ws-badge:focus-visible { outline: 1px solid var(--border-hover); outline-offset: 1px; }
  /* Anchor ABOVE the badge — bottom rows would otherwise spawn the popover
     beneath the visible viewport and need scrolling to reveal it. The
     popover is at most 3-5 entries tall so opening upward fits comfortably
     even for rows near the very top of the cards area. */
  .ws-popover {
    position: absolute;
    bottom: calc(100% + 4px);
    left: 0;
    z-index: 30;
    min-width: 200px;
    max-width: 320px;
    padding: 6px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 8px;
    box-shadow: 0 6px 16px rgba(0, 0, 0, 0.18);
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .ws-popover-title {
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.4px;
    color: var(--text-muted);
    padding: 4px 8px 2px;
  }
  .ws-popover-item {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 8px;
    border: none;
    background: transparent;
    color: var(--text-primary);
    font-size: 12px;
    text-align: left;
    border-radius: 6px;
    cursor: pointer;
  }
  .ws-popover-item:hover:not(:disabled) { background: var(--bg-hover); }
  .ws-popover-item:disabled { opacity: 0.5; cursor: not-allowed; }

  /* Workspace card styling: reuse the mirror card structure but with its
     own dot colour derived inline. */
  .card-workspace .card-provider { letter-spacing: 0.5px; }

  /* Workspace type sub-label in the detail list header. */
  .workspace-type { color: var(--text-muted); font-size: 11px; font-weight: 400; margin-left: 8px; }

  /* File path row inside a workspace group card. */
  .workspace-file-row {
    padding: 6px 12px;
    font-size: 11px;
    color: var(--text-muted);
    display: flex; gap: 6px;
    overflow: hidden;
  }
  .workspace-file-label { color: var(--text-dim); flex: 0 0 auto; }
  .workspace-file-path {
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
    font-family: 'SF Mono', 'Cascadia Code', 'Consolas', monospace;
  }

  .workspace-empty-hint {
    padding: 24px;
    text-align: center;
    color: var(--text-muted);
    font-size: 13px;
  }






</style>
