<script lang="ts">
  import { applyRepairReports, statusPaintQueue } from './lib/status';
  import { createSessionController, type StartupStream } from './lib/session/controller';
  import { onDestroy, onMount, tick } from 'svelte';
  import { settingsSaveFailed, bridge, events, setRemoteWindow } from './lib/bridge';
  import type { main } from '../wailsjs/go/models';
  import PullPlanModal from './lib/modals/PullPlanModal.svelte';
  import type { DoctorPrecheckDTO, RemoteState } from './lib/bridge';
  import { backgroundAllowed, reloadErrorShown, clearsOnReconnect } from './lib/readonly';
  import {
    configStore, accounts, sources, mirrors, repoStates, themeStore, applyStatusResults,
    prsByAccount, prSettings, workspacePlaceholders, workspaceMembers, workspaceUnknown, workspaceDiagnostics,
    remoteWindow, remoteHost
  } from './lib/stores';
  import { detailLocked, accountHealth } from './lib/status';
  import { derivePresentation, CALM_RANK } from './lib/presentation';
  import { createDetailLoader } from './lib/repo-detail';
  import { canCreateWith, type HealthFix } from './lib/status';
  import { editorOff } from './lib/remotehost';
  import type { ErrorActions } from './lib/explain';
  import { resolveView, viewAccount, accountView, attentionCounts } from './lib/views';
  import { languageStore, normalizeLanguage, remoteTextHost, t } from './lib/i18n';
  import { pushNotice, errorText } from './lib/notices';
  import { Quit } from '../wailsjs/runtime/runtime';
  import type { ConfigDTO, CredentialFinding, CredentialStatus, RepoState, MirrorCredentialCheck, EditorInfo, AIHarnessInfo, TerminalAppInfo, ShellInfo, TerminalProfileInfo, OrphanRepoDTO, ReconcileDTO, WorkspaceChoiceDTO, RepoDetailView, GlobalIdentityWarn, GCMWarn, GitignoreWarn, GitignoreInstalled, ConfigBackupInfo } from './lib/types';
  import Onboarding from './lib/Onboarding.svelte';
  import ConfigErrorScreen from './lib/components/ConfigErrorScreen.svelte';
  import ReloadErrorBanner from './lib/components/ReloadErrorBanner.svelte';
  import OfflineBanner from './lib/components/OfflineBanner.svelte';
  import TopBar from './lib/components/TopBar.svelte';
  import Banners from './lib/components/Banners.svelte';
  import CredentialWarning from './lib/components/CredentialWarning.svelte';
  import SettingsPanel from './lib/components/SettingsPanel.svelte';
  import Sidebar from './lib/components/Sidebar.svelte';
  import AttentionView from './lib/components/AttentionView.svelte';
  import AccountView from './lib/components/AccountView.svelte';
  import RepoList from './lib/components/RepoList.svelte';
  import type { RowProps } from './lib/components/RepoRow.svelte';
  import MirrorsTab from './lib/components/MirrorsTab.svelte';
  import Footer from './lib/components/Footer.svelte';
  import ActivityStrip from './lib/components/ActivityStrip.svelte';
  import Notices from './lib/components/Notices.svelte';
  import ExternalConfirm from './lib/components/ExternalConfirm.svelte';
  import { setLinkNoticeTitle } from './lib/external';
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
  import RootsModal from './lib/modals/RootsModal.svelte';
  import RootPickerModal from './lib/modals/RootPickerModal.svelte';
  import WorkspacePickerModal from './lib/modals/WorkspacePickerModal.svelte';
  import AddMirrorGroupModal from './lib/modals/AddMirrorGroupModal.svelte';
  import AddMirrorRepoModal from './lib/modals/AddMirrorRepoModal.svelte';
  import DeleteMirrorGroupModal from './lib/modals/DeleteMirrorGroupModal.svelte';
  import DeleteMirrorRepoModal from './lib/modals/DeleteMirrorRepoModal.svelte';
  import MirrorSetupResultModal from './lib/modals/MirrorSetupResultModal.svelte';
  import MirrorDiscoverModal from './lib/modals/MirrorDiscoverModal.svelte';
  import MirrorTokenModal from './lib/modals/MirrorTokenModal.svelte';
  import CreateRepoModal from './lib/modals/CreateRepoModal.svelte';
  import { repoTreeRows } from './lib/repo-tree';
  import type { Placeholder } from './lib/repo-tree';
  import { themeChoiceOf, accountPrefill } from './lib/theme';
  import { applyTint } from './lib/tint';
  import type { ThemeChoice } from './lib/theme';

  let languageChoice: 'en' | 'es' = 'en';

  // ── Experience (issue #124) ──
  // Simple hides git vocabulary and power features for non-developers;
  // Advanced is everything. Persisted as global.ui_mode.
  let uiMode: 'simple' | 'advanced' = 'advanced';
  $: simple = uiMode === 'simple';

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

  // ── Session (#379) ──
  // The controller owns the event listeners and the run bookkeeping they
  // update; the window renders from its stores and keeps the lifecycle.
  const session = createSessionController({
    bridge,
    events,
    host: {
      simple: () => simple,
      serverOS: () => serverOS,
      stream: () => currentStream,
      markStartup,
      finishTiming: () => finishTiming(),
      restartDashboard: () => initDashboard(),
      refreshDetail,
      applyReconcile,
      discoverDone: (data) => discoverDialog?.onDone(data),
      quit: () => Quit(),
    },
  });
  const {
    syncing, fetchRun, pullRun, fetchingAll, fetchingRepos, lastFetchTime, networkDown, credStatuses,
    mirrorChecking, mirrorSettingUp, mirrorSetupResultModal, mirrorDiscoverLoading, mirrorDiscoverResults,
    mirrorDiscoverError, mirrorDiscoverProgress, updateInfo, updateApplying, updateProgress, updateDone,
    updateDismissed, rollbackVersion, remoteTint,
    executePullAll, failPullAll, markSyncStart, checkAllMirrorStatus, verifyAllCredentials,
  } = session;

  // ── Update state ──
  // Footer shows the update the session heard of; the error of applying
  // it stays here.
  let updateError = '';

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
      settingsSaveFailed(e, () => setCheckGitignorePref(enabled));
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
      gitignoreError = errorText(e);
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
  let rollbackBusy = false;
  let rollbackError = false;
  async function resolveUpdate(rollback: boolean) {
    rollbackBusy = true;
    rollbackError = false;
    try {
      await bridge.resolveUpdateRecovery(rollback);
      $rollbackVersion = '';
    } catch { rollbackError = true; }
    finally { rollbackBusy = false; }
  }

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
        cfgRepairFailure = res.error || $t('app.unknownError');
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
        cfgRepairFailure = res.error || $t('app.unknownError');
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
    if (result) await cloneRepos(result.sourceKey, result.repos);
  }

  // Simple mode's "Add account" runs the connect + pick steps of the same
  // guided setup; Advanced keeps the full form.
  let connectModal = false;
  const addAccount = () => { if (simple) connectModal = true; else addAccountDialog.open(); };
  async function finishConnect(result: { sourceKey: string; repos: string[] } | null) {
    connectModal = false;
    await reloadFromDisk();
    if (result) await cloneRepos(result.sourceKey, result.repos);
  }

  // Re-run the scan for a container: nested clones gitbox recognises are
  // added on the spot; the Unmatched list opens for the rest.
  async function rescanContainer() {
    const res = await runReconcile();
    if (orphanList.some(o => o.nested)) {
      showOrphanModal();
    } else if (res && res.adopted > 0) {
      pushNotice('info', $t(res.adopted === 1 ? 'notice.pickedUpOne' : 'notice.pickedUp', { n: res.adopted }));
    } else {
      pushNotice('info', $t('notice.noNested'));
    }
  }

  // ── Clone roots (settings) ──
  let rootsDialog: RootsModal;
  const openRoots = () => rootsDialog.openRoots();

  // ── Theme ──
  // Persisted as global.theme (empty = follow the OS).
  let themeChoice: ThemeChoice = 'system';
  let resolvedTheme: 'light' | 'dark' = 'dark';
  // remoteTint is the color a remote window tints its top bar and sidebar
  // with (#331); a local window never paints it.
  $: applyTint(document.documentElement, windowMode === 'remote' && $remoteTint ? $remoteTint : null, $themeStore);

  function applyTheme() {
    if (themeChoice === 'system') {
      resolvedTheme = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
    } else {
      resolvedTheme = themeChoice;
    }
    themeStore.set(resolvedTheme);
    document.documentElement.setAttribute('data-theme', resolvedTheme);
  }

  function setTheme(choice: ThemeChoice) {
    themeChoice = choice;
    applyTheme();
    bridge.setTheme(choice).catch((e) => console.error('saving theme failed', e));
  }

  function cycleTheme() {
    const order: ThemeChoice[] = ['system', 'light', 'dark'];
    setTheme(order[(order.indexOf(themeChoice) + 1) % 3]);
  }

  let pullPlan: main.PullPlanDTO | null = null;
  let loadingPullPlan = false;
  $: if (remoteOffline) pullPlan = null;

  async function syncAll() {
    if ($syncing || loadingPullPlan || pullPlan || remoteOffline) return;
    if (!($configStore?.global?.show_pull_plan ?? simple)) {
      await executePullAll('');
      return;
    }
    loadingPullPlan = true;
    try {
      const plan = await bridge.previewPullAll();
      if (!lifetime.signal.aborted && !remoteOffline) pullPlan = plan;
    } catch (error) {
      failPullAll(String(error));
    } finally {
      loadingPullPlan = false;
    }
  }

  function confirmPullPlan() {
    if (!pullPlan) return;
    const { planID } = pullPlan;
    pullPlan = null;
    if (planID && !remoteOffline) void executePullAll(planID);
  }

  function syncRepo(sourceKey: string, repoKey: string) {
    markSyncStart(`${sourceKey}/${repoKey}`, 'syncing');
    bridge.pullRepo(sourceKey, repoKey);
  }

  // startClone clones one repo into root ('' = its account's own root).
  function startClone(sourceKey: string, repoKey: string, root = '') {
    session.markCloneStart(`${sourceKey}/${repoKey}`);
    bridge.cloneRepo(sourceKey, repoKey, root);
  }

  // An explicit clone asks once which root the batch goes to (#149), when
  // there are two or more; cancelling clones nothing.
  let rootPicker: RootPickerModal;
  async function cloneRepos(sourceKey: string, repoKeys: string[]) {
    if (repoKeys.length === 0) return;
    const root = await rootPicker.pick(sourceKey, repoKeys);
    if (root === null) return;
    for (const repoKey of repoKeys) startClone(sourceKey, repoKey, root);
  }
  const cloneRepo = (sourceKey: string, repoKey: string) => cloneRepos(sourceKey, [repoKey]);

  function fetchRepo(sourceKey: string, repoKey: string) {
    const key = `${sourceKey}/${repoKey}`;
    $fetchingRepos[key] = true;
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

  function onRecipeImported(cfg: ConfigDTO, added: string[]) {
    // Every account created by import still needs local sign-in.
    const refs = new Set(added);
    for (const [key, account] of Object.entries(cfg.accounts)) {
      if (refs.has(account.url + '/' + account.username)) $credStatuses[key] = {status: 'none'} as CredentialStatus;
    }
    configStore.set(cfg);
  }

  async function reloadConfigFromDisk() {
    await bridge.reloadConfig();
    const snapshot = await bridge.getStartupSnapshot();
    repoNesting = snapshot.nesting || {};
    credentialFindings = snapshot.credentialFindings || [];
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
      pushNotice('error', $t('notice.terminalsFailed'), [errorText(e)]);
    } finally {
      setTimeout(() => { terminalsManagerOpening = false; }, cooldown);
    }
  }

  // revealLabel is the OS-aware "Reveal in <file manager>" string passed to
  // every LauncherMenu instance. Computed once from hostOS so the kebabs
  // don't each re-derive it.
  $: revealLabel = hostOS === 'windows' ? $t('app.revealExplorer')
    : hostOS === 'darwin' ? $t('app.revealFinder')
    : $t('app.revealFiles');

  // ── Disk reconcile (#149) ──
  // A pass adopts the clones gitbox recognises and re-links moved ones by
  // itself; orphanList holds the clones that need me (Unmatched clones).
  let orphanList: OrphanRepoDTO[] = [];
  let skippedClones: NonNullable<ReconcileDTO['skipped']> = [];
  let ignoredClones: OrphanRepoDTO[] = [];
  let orphanCount = 0;
  let skippedFolders = 0;
  let skippedScans: NonNullable<ReconcileDTO['skippedScans']> = [];
  let orphansDialog: OrphansModal;
  const showOrphanModal = () => orphansDialog.showOrphanModal();

  async function runReconcile(): Promise<ReconcileDTO | null> {
    try {
      const res = await bridge.reconcile();
      await applyReconcile(res);
      return res;
    } catch (e) {
      pushNotice('error', $t('notice.reconcileFailed'), [errorText(e)]);
      return null;
    }
  }

  const incompleteShown = new Set<string>();
  // applyReconcile shows a pass: the lists, and the config and statuses when
  // it added or moved anything.
  async function applyReconcile(res: ReconcileDTO) {
    if (res.repairs?.length) $repoStates = applyRepairReports($repoStates, res.repairs);
    if (res.workspaceUnknown) $workspaceUnknown = new Set(res.workspaceUnknown);
    orphanList = res.unmatched || [];
    ignoredClones = res.ignored || [];
    skippedFolders = res.skippedFolders || 0;
    skippedScans = res.skippedScans || [];
    skippedClones = res.skipped || [];
    orphanCount = orphanList.length + skippedClones.length;
    if (res.error) pushNotice('error', $t('notice.reconcileFailed'), [res.error]);
    // A clone git never finished is reported once per session (#295).
    const incomplete = (res.incomplete || []).filter(c => !incompleteShown.has(c.path));
    for (const c of incomplete) incompleteShown.add(c.path);
    if (incomplete.length) pushNotice('error', $t('notice.incompleteClone'), incomplete.map(c => `${c.repo}: ${c.path}`));
    if (res.adopted + res.relinked > 0 || res.workspacesChanged) {
      $configStore = await bridge.reloadConfig();
      applyStatusResults(await bridge.getAllStatus());
    }
    const byParent: Record<string, Placeholder[]> = {};
    for (const p of res.placeholders || []) (byParent[p.parent] ??= []).push({ name: p.name, path: p.path, outsidePath: p.outsidePath });
    $workspacePlaceholders = byParent;
    $workspaceMembers = new Set(res.workspaceMembers || []);
    $workspaceDiagnostics = Object.fromEntries((res.workspaceDiagnostics || []).map(d => [d.parent, d]));
    workspaceChoices = new Set((res.workspaceChoices || []).map(c => `${c.source}/${c.repo}`));
    await loadNesting();
    askWorkspaceChoices(res.workspaceChoices || []);
  }

  // ── Container workspace files ──
  async function findWorkspaceFolder(path: string) {
    try {
      const found = await bridge.scanFolderForClones(path);
      orphansDialog.showWorkspaceSearch((found || []).filter(o => o.workspaceRoot === path));
    } catch (e) {
      pushNotice('error', $t('notice.reconcileFailed'), [errorText(e)]);
    }
  }


  // Containers whose root holds several .code-workspace files and no pick;
  // each is asked once per session, then keeps a "choose workspace" hint.
  let workspaceChoices: Set<string> = new Set();
  const workspaceAsked = new Set<string>();
  let workspacePicker: WorkspacePickerModal;

  // askWorkspaceChoices opens the picker for each new choice, one after the
  // other, without holding up the pass that found them.
  async function askWorkspaceChoices(choices: WorkspaceChoiceDTO[]) {
    for (const c of choices) {
      const key = `${c.source}/${c.repo}`;
      if (workspaceAsked.has(key)) continue;
      workspaceAsked.add(key);
      await pickWorkspace(c.source, c.repo, c.files);
    }
  }

  async function pickWorkspace(sourceKey: string, repoName: string, files: string[]) {
    const current = $configStore?.sources?.[sourceKey]?.repos?.[repoName]?.workspace || '';
    const file = await workspacePicker.pick(repoName, files, current);
    if (!file || file === current) return;
    try {
      await bridge.setRepoWorkspace(sourceKey, repoName, file);
      // The pass below sees no change to report, so I reload the config
      // myself: the row's editor button reads repo.workspace from it.
      $configStore = await bridge.reloadConfig();
      await runReconcile();
    } catch (e) {
      pushNotice('error', $t('notice.workspaceFailed'), [errorText(e)]);
    }
  }

  async function chooseWorkspace(sourceKey: string, repoName: string) {
    try {
      await pickWorkspace(sourceKey, repoName, await bridge.repoWorkspaceFiles(sourceKey, repoName));
    } catch (e) {
      pushNotice('error', $t('notice.workspaceFailed'), [errorText(e)]);
    }
  }

  // ── Nested-clone display (container repos) ──
  // child "source/repo" → parent "source/repo", derived from resolved paths.
  let repoNesting: Record<string, string> = {};

  async function loadNesting() {
    try {
      repoNesting = await bridge.repoNesting();
    } catch { /* I retain the last known nesting when a refresh fails. */ }
  }

  // ── Collapse / expand (issue #117) ──
  // Folded account groups (source keys) and folded parent clones
  // ("source/repo"), persisted in global.collapsed. A clone is collapsible
  // because it has clones below it, whatever made them its children.
  let collapsedSources: Set<string> = new Set();
  let collapsedRepos: Set<string> = new Set();

  // ── Navigation (issue #138) ──
  // The selected view (attention, all, mirrors or account:<key>) and the sidebar folded to its rail, both persisted in
  // global.collapsed. resolveView shows Needs attention for a missing
  // account or an Advanced-only view in Simple, without touching `view`,
  // so switching back to Advanced restores Mirrors. A window narrower than
  // 780px starts on the rail; the sidebar button then widens the sidebar in
  // place and the content narrows to fit (railPeek, not saved).
  let view = 'attention';
  let sidebarCollapsed = false;
  let railPeek = false;
  let innerWidth = 1040;
  $: narrow = innerWidth < 780;
  $: if (!narrow) railPeek = false;
  $: rail = narrow ? !railPeek : sidebarCollapsed;
  $: currentView = resolveView(view, Object.keys($accounts), simple);
  $: viewAcct = viewAccount(currentView);
  $: sidebarShortcut = hostOS === 'darwin' ? '⌘B' : 'Ctrl+B';

  // saveLayout persists the folded groups and clones, the sidebar and the view.
  function saveLayout() {
    bridge.setCollapsed({ sources: [...collapsedSources], repos: [...collapsedRepos], sidebar: sidebarCollapsed, view })
      .catch((e) => console.error('saving layout failed', e));
  }

  // setView switches views; the detail panel and any open menu close.
  function setView(next: string) {
    view = next;
    expandedRepo = null;
    repoDetail = null;
    actionMenuRepo = null;
    actionMenuAccount = null;
    openPRPopover = null;
    expandedCard = null;
    saveLayout();
  }
  const showAccountView = (accountKey: string) => setView(accountView(accountKey));

  function toggleSidebar() {
    if (narrow) {
      railPeek = !railPeek;
      return;
    }
    sidebarCollapsed = !sidebarCollapsed;
    saveLayout();
  }

  // ⌘B (macOS) or Ctrl+B folds the sidebar, except while typing.
  function sidebarKey(e: KeyboardEvent) {
    if (e.key.toLowerCase() !== 'b' || e.altKey || e.shiftKey || !(hostOS === 'darwin' ? e.metaKey : e.ctrlKey)) return;
    if (firstRun || cfgLoadErrorModal || windowMode !== 'main') return;
    if (e.target instanceof Element && e.target.closest('input, textarea, select, [contenteditable="true"]')) return;
    e.preventDefault();
    toggleSidebar();
  }

  // The mirror cards start as chips. Clicking one opens it into the full
  // two-line card; clicking outside it or Escape folds it back. One open
  // card at a time, keyed "mir:".
  let expandedCard: string | null = null;

  // closeDetailIf drops the open detail panel when a collapse hides its row.
  function closeDetailIf(hidden: (key: string) => boolean) {
    if (expandedRepo && hidden(expandedRepo)) {
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
    saveLayout();
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
    saveLayout();
  }

  // How much each clone needs me, for Needs attention and the counts.
  $: rankOf = (key: string) => {
    return derivePresentation($repoStates[key]).attention;
  };
  // Clones in alphabetical order (repo-tree.ts).
  $: treeRows = repoTreeRows($sources, repoNesting, collapsedRepos, $workspacePlaceholders);

  // How many repos need me, per account and in total; accounts whose
  // credential needs a fix count too (Needs attention lists them first).
  $: keysByAccount = (() => {
    const byAccount: Record<string, string[]> = Object.fromEntries(Object.keys($accounts).map((k) => [k, []]));
    for (const [sourceKey, source] of Object.entries($sources)) {
      (byAccount[source.account] ||= []).push(...Object.keys(source.repos).map((r) => `${sourceKey}/${r}`));
    }
    return byAccount;
  })();
  $: brokenAccounts = Object.keys($accounts).filter((k) => accountHealth($credStatuses[k]?.status, simple).attention || $credStatuses[k]?.problem);
  $: attention = (() => {
    const counts = attentionCounts(keysByAccount, rankOf, CALM_RANK, brokenAccounts);
    return { ...counts, total: counts.total + ($configStore?.pathConflicts?.length ?? 0) };
  })();
  $: totalRepos = Object.values(keysByAccount).reduce((n, keys) => n + keys.length, 0);

  // ── Sweep branches modal ──
  let sweepDialog: SweepModal;
  const sweepBranches = (sourceKey: string, repoKey: string) => {
    const state = $repoStates[`${sourceKey}/${repoKey}`];
    const terminal = state && repoActions(sourceKey, repoKey, state, false).terminal;
    return sweepDialog.sweepBranches(sourceKey, repoKey, terminal);
  };

  // ── Repo detail panel ──
  let expandedRepo: string | null = null;
  let repoDetail: RepoDetailView | null = null;
  let detailLoading = false;
  const detailLoader = createDetailLoader(bridge.getRepoDetail);

  $: if (!expandedRepo) {
    detailLoader.cancel();
    detailLoading = false;
  }

  // Auto-collapse expanded detail when the clone becomes unavailable.
  $: if (expandedRepo) {
    const st = $repoStates[expandedRepo];
    if (st && detailLocked(st.status)) {
      expandedRepo = null;
      repoDetail = null;
    }
  }

  function toggleRepoDetail(sourceKey: string, repoName: string, status: string) {
    // I include incoming updates among the states with details to inspect.
    if (detailLocked(status)) return;
    const key = `${sourceKey}/${repoName}`;
    if (expandedRepo === key) {
      detailLoader.cancel();
      expandedRepo = null;
      repoDetail = null;
      return;
    }
    expandedRepo = key;
    if (status === 'clean') {
      detailLoader.cancel();
      repoDetail = null;
      detailLoading = false;
    } else {
      void loadRepoDetail(sourceKey, repoName);
    }
  }

  async function loadRepoDetail(sourceKey: string, repoName: string) {
    const key = `${sourceKey}/${repoName}`;
    repoDetail = null;
    detailLoading = true;
    const detail = await detailLoader.load(sourceKey, repoName);
    if (!detail || expandedRepo !== key) return;
    repoDetail = detail;
    detailLoading = false;
  }

  // refreshDetail reloads the open detail panel after a status refresh,
  // unless its clone is now unavailable (a reactive statement collapses it).
  function refreshDetail() {
    if (expandedRepo && !detailLocked($repoStates[expandedRepo]?.status)) {
      const slash = expandedRepo.indexOf('/');
      const sk = expandedRepo.substring(0, slash);
      const rn = expandedRepo.substring(slash + 1);
      void loadRepoDetail(sk, rn);
    }
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
    // A disconnected remote window has nothing to reload: the disconnect
    // banner already says so, and the next connection reloads everything.
    if (!backgroundAllowed(windowMode === 'remote', remoteState?.state)) return;
    const generation = ++reloadGeneration;
    try {
      await bridge.reloadConfig();
      const snapshot = await bridge.getStartupSnapshot();
      if (generation !== reloadGeneration) return;
      repoNesting = snapshot.nesting || {};
      credentialFindings = snapshot.credentialFindings || [];
      configStore.set(snapshot.config);
      events.snapshot(snapshot.eventWatermark, () => $configStore?.revision ?? 0);
      configPath = snapshot.configPath;
      // A renamed account: Go already moved the saved view to its new key.
      // A deleted one: Go cleared it, so Needs attention shows.
      const shown = viewAccount(view);
      if (shown && !snapshot.config.accounts?.[shown]) view = snapshot.config.global?.collapsed?.view || 'attention';
      const saved = snapshot.config.global.periodic_sync || 'off';
      fetchInterval = saved;
      if (!$fetchingAll) {
        const statusVersion = session.statusGeneration;
        const statuses = await bridge.getAllStatus();
        if (generation !== reloadGeneration) return;
        if (statusVersion === session.statusGeneration && !$fetchingAll) applyStatusResults(statuses);
        if (!$fetchingAll) verifyAllCredentials();
      }
      reloadError = '';
    } catch (err: any) {
      if (generation === reloadGeneration && reloadErrorShown(err, windowMode === 'remote', remoteState?.state)) reloadError = errorText(err);
      throw err;
    }
  }

  // ── Delete mode ──
  let deleteMode = false;
  let folderDeleteFailures: Record<string, string> = {};
  function setFolderDeleteFailure(key: string, message: string) {
    if (message) folderDeleteFailures[key] = message;
    else delete folderDeleteFailures[key];
    folderDeleteFailures = { ...folderDeleteFailures };
  }
  let deleteCloneDialog: DeleteCloneModal;
  const askDelete = (sourceKey: string, repoKey: string, status: string, removeFolder = false) => deleteCloneDialog.askDelete(sourceKey, repoKey, status, removeFolder);

  // ── Edit account ──
  let editAccountDialog: EditAccountModal;
  const openEditAccount = (key: string) => editAccountDialog.openEditAccount(key);

  // ── Add account ──
  let addAccountDialog: AddAccountModal;
  // An unmatched clone's server has no account (#351): the form starts
  // from that server, and a scan once the account exists turns the row
  // adoptable while the Unmatched list stays open.
  const addAccountForClone = (host: string) => addAccountDialog.open(accountPrefill(host), () => void runReconcile());

  // Shared credential operation state — used by BOTH add-account and change-credential flows.
  // Only one modal can be active at a time, so sharing is safe.
  let credResult: { ok: boolean; message: string; needsPAT?: boolean; sshPublicKey?: string; sshAddURL?: string; sshVerified?: boolean; findings?: CredentialFinding[] } | null = null;
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
      $credStatuses[accountKey] = { ...cs };
    }).catch(() => {
      $credStatuses[accountKey] = {status: 'ok', primary: 'ok', pat: 'ok', primaryMsg: '', patMsg: ''} as CredentialStatus;
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
        if (credResult?.ok) tokenStored(credResult);
      } else if (credType === 'ssh') {
        credResult = await bridge.credentialSetupSSH(accountKey);
      }
    } catch (err: any) {
      credResult = { ok: false, message: err?.message || String(err) };
    }
    credBusy = false;
    await credPostSetup(accountKey);
  }

  // tokenStored clears the token field once the host has the token (#163,
  // #242) and shows what the credential audit found right after the store:
  // nothing found clears an earlier warning.
  function tokenStored(res: { findings?: CredentialFinding[] }) {
    credTokenInput = '';
    credentialFindings = res.findings || [];
  }

  // Store a token for the account of the open credential modal (the
  // modal passes its account key).
  async function storeCredToken(key: string) {
    credBusy = true;
    try {
      credResult = await bridge.credentialStoreToken(key, credTokenInput);
      if (credResult?.ok) tokenStored(credResult);
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

  // The account card's and Needs attention's fix action: retry re-checks
  // the connection; the others open the credential modal.
  function fixCredential(accountKey: string, fix: HealthFix) {
    if (fix === 'retry') retryCredential(accountKey);
    else if (fix) openCredChange(accountKey, $accounts[accountKey]?.default_credential_type || 'gcm');
  }

  async function retryCredential(accountKey: string) {
    const before = $credStatuses[accountKey];
    $credStatuses[accountKey] = { ...(before || {}), status: 'unknown', problem: undefined } as CredentialStatus;
    try {
      const cs = await bridge.credentialVerify(accountKey);
      $credStatuses[accountKey] = { ...cs };
    } catch (e) {
      console.error(e);
      $credStatuses[accountKey] = before;
    }
  }

  function fixProofCredential(accountKey: string) {
    const account = $accounts[accountKey];
    if (account) openCredChange(accountKey, account.default_credential_type || 'gcm');
  }

  const openTokenSetup = (accountKey: string) => credentialDialog.openTokenSetup(accountKey);

  // ── Delete account ──
  let deleteAccountDialog: DeleteAccountModal;
  const askDeleteAccount = (accountKey: string) => deleteAccountDialog.askDeleteAccount(accountKey);

  // ── Mirrors ──
  // The session's mirrorSettingUp is set before calling
  // bridge.setupMirrorRepo and cleared on the mirror:setup:done event.
  let addMirrorGroupDialog: AddMirrorGroupModal;
  let addMirrorRepoDialog: AddMirrorRepoModal;
  let deleteMirrorGroupDialog: DeleteMirrorGroupModal;
  let deleteMirrorRepoDialog: DeleteMirrorRepoModal;
  let mirrorCredWarning: MirrorCredentialCheck | null = null;

  // Mirror discovery
  async function runMirrorDiscover() {
    $mirrorDiscoverLoading = true;
    $mirrorDiscoverResults = null;
    $mirrorDiscoverError = '';
    $mirrorDiscoverProgress = null;
    discoverAdded = {};
    await bridge.discoverMirrors();
  }

  // Track which repos have been individually added during this discover session.
  let discoverAdded: Record<string, boolean> = {};

  // Create repo modal
  let createRepoDialog: CreateRepoModal;
  const openCreateRepo = (accountKey: string) => createRepoDialog.openCreateRepo(accountKey);

  function repoActions(source: string, name: string, state: RepoState, forDetail: boolean, credentialStatus?: string): ErrorActions {
    const account = $sources[source]?.account;
    const path = state.path;
    const editor = configEditors.find((item) => !editorOff(item.command, $remoteHost));
    const profiles = configProfiles.filter((item) => !item.hidden);
    const profile = profiles.find((item) => item.default) || profiles[0];
    const [owner, repo, extra] = name.split('/');
    const retry = () => {
      if (!state.failedOperation) {
        void bridge.refreshStatus();
      } else if (state.failedOperation === 'clone') {
        void cloneRepo(source, name);
      } else if (state.failedOperation === 'pull') {
        syncRepo(source, name);
      } else if (!$fetchingRepos[`${source}/${name}`]) {
        fetchRepo(source, name);
      }
    };
    return {
      retry: forDetail ? () => void loadRepoDetail(source, name) : retry,
      credential: account ? () => fixCredential(account, 'change') : undefined,
      editor: path && editor ? () => void bridge.openInApp(path, editor.command).catch((error) => pushNotice('error', $t('notice.openEditor'), [errorText(error)])) : undefined,
      terminal: path && profile ? () => void bridge.openProfile(path, profile.id).catch((error) => pushNotice('error', $t('notice.openTerminalFailed'), [errorText(error)])) : undefined,
      upstream: account && owner && repo && !extra && canCreateWith(credentialStatus) ? () => void createRepoDialog.openCreateRepo(account, { owner, name: repo }) : undefined,
    };
  }

  async function checkMirrorStatus(mirrorKey: string) {
    $mirrorChecking = { ...$mirrorChecking, [mirrorKey]: true };
    await bridge.getMirrorStatus(mirrorKey);
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
    $mirrorSettingUp = { ...$mirrorSettingUp, [key]: true };
    await bridge.setupMirrorRepo(mirrorKey, repoKey);
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
        // Newly a container — rescan: recognised nested clones are added in
        // place; the Unmatched list opens for any it couldn't place.
        await runReconcile();
        if (orphanList.some(o => o.nested)) showOrphanModal();
      }
    } catch (e) {
      pushNotice('error', $t('notice.containerFailed'), [errorText(e)]);
    }
  }

  // ── Settings ──
  let showSettings = false;
  let configPath = '';
  let appVersion = '';
  let hostOS = ''; // "darwin" | "windows" | "linux" — loaded on init
  // serverOS is the system of the host whose data the window shows: hostOS
  // in a local window, the remote's in a remote one (#240).
  let serverOS = '';
  $: setLinkNoticeTitle($t('link.notOpened'));
  // windowMode tells the frontend which UI to render. The default "main"
  // shows the full gitbox app; "terminals" hides everything except the
  // standalone Profile editor window spawned by openTerminalsManagerWindow
  // (issue #69 — keeps the editor in its own OS window so the parent
  // stays interactive).
  // "remote" is the window of one remote (#239), which shows only the
  // connected view until P4. "remotes" is the Remote hosts window (#330).
  let windowMode: 'main' | 'terminals' | 'remote' | 'remotes' = 'main';
  // The remote view loads only in a remote window, so the main bundle
  // does not carry it.
  let RemoteConnected: typeof import('./lib/components/RemoteConnected.svelte').default | null = null;
  // Likewise the Profile editor loads only in the terminals window.
  let TerminalsModal: typeof import('./lib/TerminalsModal.svelte').default | null = null;
  // And the Remote hosts window's view only in that window (#330).
  let RemoteHostsWindow: typeof import('./lib/components/RemoteHostsWindow.svelte').default | null = null;
  // A remote window (#240) shows the connection card until the host's data
  // first arrives, then the full UI, limited to what its server serves
  // (#294). After a disconnect it keeps
  // the last state, dimmed, under a Reconnect banner. A host without a
  // config shows the first-run onboarding, run on the host (#349); while
  // that window is not connected it shows the card, which can reconnect.
  let remoteState: RemoteState | null = null;
  let remoteLoaded = false;
  let remoteLoading = false;
  $: remoteShowMain = windowMode === 'remote' && remoteLoaded && !(firstRun && remoteState?.state !== 'connected');
  // Actions stay off from the disconnect until the host's state is known
  // again: through the reload, the reconcile and the status refresh that
  // follow each reconnect (#295).
  $: remoteOffline = remoteShowMain && (remoteState?.state !== 'connected' || remoteLoading);

  async function onRemoteState(s: RemoteState) {
    const wasConnected = remoteState?.state === 'connected';
    // A new connection clears what the disconnection left behind.
    if (clearsOnReconnect(remoteState?.state, s.state)) reloadError = '';
    remoteState = s;
    if (s.state !== 'connected') {
      // The window keeps what it shows until it knows whether the next
      // connection resumes the host's daemon (#247): the events it missed
      // then arrive through the same router, before the connected state.
      if (wasConnected) session.noteDisconnect();
      if (wasConnected && currentStream) {
        statusPass++;
        retireStream();
      }
      if (s.state === 'disconnected' || s.state === 'failed') markStartup('finished');
      return;
    }
    if (wasConnected || remoteLoading) return;
    remoteLoading = true;
    // A connection that resumed the host's daemon with every event the
    // window missed keeps the view, the rows and the work in flight; the
    // window reads the config and the status again, and starts no fetch
    // and no reconcile (#247). A window still in onboarding loads again:
    // the host may have saved its config before the cut (#349).
    if (remoteLoaded && s.resumed && !firstRun) {
      try {
        await reloadFromDisk();
        const refreshed = session.nextStatus(30_000);
        await bridge.refreshStatus();
        await refreshed;
      } catch (e) {
        console.error(e);
      } finally {
        remoteLoading = false;
      }
      return;
    }
    // Any other connection, the first one and a reconnect that could not
    // resume, loads the host's data again; a reconnect keeps the view.
    if (remoteLoaded) {
      events.reset();
      session.forgetOperations();
      // Onboarding waits behind the card until the host says whether it
      // still has no config.
      if (firstRun) remoteLoaded = false;
    }
    try {
      remoteWindow.set(true);
      remoteTextHost.set(s.alias);
      setRemoteWindow(true, $t('remote.readOnlyTip'));
      // What the host did meanwhile is not all known: its rows come back
      // from the host's status.
      session.forgetRuns();
      repoStates.set({});
      await startMain();
      // A host without a config shows the onboarding, which saves it there.
      remoteLoaded = true;
      if (!firstRun) {
        // What the host's clones are now: a clone the cut stopped shows as
        // missing or incomplete before any action is offered.
        await runReconcile();
        const refreshed = session.nextStatus(30_000);
        await bridge.refreshStatus();
        await refreshed;
      }
    } catch (e) {
      console.error(e);
    } finally {
      remoteLoading = false;
    }
  }
  let autostartOn = false;
  let autostartSupported = true;

  // ── Doctor (System check) ──
  // DoctorModal runs the check; Settings shows its one-line summary.
  let doctorSummary = '';
  let credentialFindings: CredentialFinding[] = [];
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
    bridge.runTask('pr-refresh').catch((e) => console.error(e));
  }

  async function setPRBadgesEnabled(enabled: boolean) {
    try {
      await bridge.setPRBadgesEnabled(enabled);
      prSettings.update((v) => ({ ...v, enabled }));
      if (enabled) refreshAllPRs();
      else prsByAccount.set({});
    } catch (e) { settingsSaveFailed(e, () => setPRBadgesEnabled(enabled)); }
  }

  async function setPRIncludeDrafts(include: boolean) {
    try {
      await bridge.setPRIncludeDrafts(include);
      prSettings.update((v) => ({ ...v, includeDrafts: include }));
      refreshAllPRs();
    } catch (e) { settingsSaveFailed(e, () => setPRIncludeDrafts(include)); }
  }

  // Which PR popover is open: key is `${sourceKey}/${repoName}|${kind}` or null.
  let openPRPopover: string | null = null;
  function togglePRPopover(key: string) {
    openPRPopover = openPRPopover === key ? null : key;
  }
  function closePRPopover() {
    openPRPopover = null;
  }

  async function setFetchInterval(val: string) {
    try {
      await bridge.setPeriodicSync(val);
      fetchInterval = val;
    } catch (e) {
      fetchInterval = await bridge.getPeriodicSync();
      pushNotice('error', errorText(e));
    }
  }

  // runFetchAll fetches every clone through the session; a manual run
  // that started one also rescans the disk.
  async function runFetchAll(manual = false) {
    if (await session.runFetchAll(manual) && manual) runReconcile();
  }

  // Everything a clone row shares with its list, passed unchanged through
  // every repo view (RowProps in RepoRow.svelte).
  let rowProps: RowProps;
  $: rowProps = {
    simple, deleteMode, folderDeleteFailures, fetchingRepos: $fetchingRepos, collapsedRepos, workspaceChoices,
    revealLabel, configEditors, configProfiles, configAIHarnesses, actionMenuRepo, openPRPopover,
    expandedRepo, repoDetail, detailLoading, repoActions, credentialStatuses: $credStatuses, fixProofCredential,
    activateRepoRow, askDelete, syncRepo, cloneRepo, fetchRepo, toggleActionMenu, closeActionMenu,
    togglePRPopover, closePRPopover, sweepBranches,
    toggleContainer, rescanContainer, chooseWorkspace, findWorkspaceFolder, toggleRepoCollapsed, isContainerRepo, showAccountView,
  };

  let statusPass = 0;
  let currentStream: StartupStream | null = null;
  function retireStream() {
    currentStream?.paint.discard();
    currentStream?.first();
    currentStream?.finish();
    currentStream = null;
  }
  const observedStages = new Set<string>();
  let timingQueue = Promise.resolve();
  function markStartup(stage: string) {
    if (observedStages.has(stage)) return;
    observedStages.add(stage);
    timingQueue = timingQueue.then(() => bridge.markStartup(stage)).catch(console.error);
  }
  let rowObserver: MutationObserver | undefined;
  function observeFirstRow() {
    if (observedStages.has('firstRow')) return;
    const detect = () => {
      if (!document.querySelector('.repo-row')) return;
      markStartup('firstRow');
      rowObserver?.disconnect();
      rowObserver = undefined;
    };
    rowObserver?.disconnect();
    rowObserver = new MutationObserver(detect);
    rowObserver.observe(document.body, { childList: true, subtree: true });
    detect();
  }
  let remoteScheduled = false;
  const finishTiming = () => {
    if (currentStream?.full && remoteScheduled) {
      markStartup('finished');
    }
  };

  // ── Lifecycle ──
  async function initDashboard() {
    const snapshot = await bridge.getStartupSnapshot();
    if (lifetime.signal.aborted) return;
    const cfg = snapshot.config;
    themeChoice = themeChoiceOf(cfg.global?.theme);
    applyTheme();
    repoNesting = snapshot.nesting || {};
    credentialFindings = snapshot.credentialFindings || [];
    const collapsed = cfg.global?.collapsed;
    collapsedSources = new Set(collapsed?.sources || []);
    collapsedRepos = new Set(collapsed?.repos || []);
    sidebarCollapsed = !!collapsed?.sidebar;
    // A remote window keeps its view across reconnects: the host's config
    // does not hold it.
    if (!(windowMode === 'remote' && remoteLoaded)) view = collapsed?.view || 'attention';
    languageChoice = normalizeLanguage(cfg.global?.language);
    languageStore.set(languageChoice);
    configPath = snapshot.configPath;
    appVersion = snapshot.appVersion;
    hostOS = snapshot.os;
    serverOS = snapshot.serverOS || snapshot.os;
    fetchInterval = cfg.global?.periodic_sync || 'off';
    uiMode = cfg.global?.ui_mode === 'simple' ? 'simple' : 'advanced';
    if (uiMode === 'simple') enterSimple();
    observeFirstRow();
    configStore.set(cfg);
    repoStates.set(Object.fromEntries(Object.entries(cfg.sources).flatMap(([sourceKey, source]) =>
      Object.keys(source.repos).map(repoName => [`${sourceKey}/${repoName}`, $repoStates[`${sourceKey}/${repoName}`] ?? { status: 'unknown' as const, progress: 0, behind: 0, modified: 0, untracked: 0, ahead: 0 }])
    )));
    events.snapshot(snapshot.eventWatermark, () => $configStore?.revision ?? 0);
    const id = ++statusPass;
    retireStream();
    let first!: () => void;
    const firstReady = new Promise<void>(resolve => { first = resolve; });
    let finish!: () => void;
    const completed = new Promise<void>(resolve => { finish = resolve; });
    const paint = statusPaintQueue(rows => {
      if (currentStream?.id !== id || $configStore?.revision !== cfg.revision) return;
      for (const row of rows) currentStream.pending.delete(`${row.source}/${row.repo}`);
      applyStatusResults(rows, true);
    });
    currentStream = { id, first, finish, full: false, painted: false, pending: new Set(Object.keys($repoStates)), paint };
    remoteScheduled = false;
    try {
      await bridge.beginStatus(id);
      await firstReady;
    } catch (e) {
      if (id !== statusPass) return;
      repoStates.update(states => Object.fromEntries(Object.entries(states).map(([key, state]) =>
        [key, state.status === 'unknown' ? { ...state, status: 'error' as const, error: errorText(e) } : state]
      )));
      finish();
      markStartup('finished');
    }
    if (id !== statusPass) return;
    if (Object.keys(cfg.accounts).length) markStartup('firstRemote');
    remoteScheduled = true;
    finishTiming();

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

    // Load autostart state. A remote window has no autostart of its own.
    if (windowMode !== 'remote') loadAutostart();

    // Start fleet work on the first batch, including the optional PR fetch.
    await loadPRSettings();
    refreshAllPRs();

    // Reconcile walks the same clone trees as local status. Let the first
    // sweep finish before starting that second disk traversal.
    if (windowMode === 'remote') {
      await completed; // The reconnect flow starts its own reconcile after init.
    } else {
      void completed.then(() => { if (id === statusPass) void runReconcile(); });
    }
  }

  async function setUIMode(mode: 'simple' | 'advanced') {
    if (uiMode === mode) return;
    uiMode = mode;
    if (mode === 'simple') enterSimple();
    await bridge.setUIMode(mode);
  }

  // enterSimple leaves the Advanced-only states Simple has no controls for.
  function enterSimple() {
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

  const lifetime = new AbortController();
  let focusTimer: ReturnType<typeof setTimeout> | undefined;
  onDestroy(() => {
    lifetime.abort();
    session.destroy();
    clearTimeout(focusTimer);
    rowObserver?.disconnect();
    reloadGeneration++;
    statusPass++;
    retireStream();
  });

  onMount(async () => {
    markStartup('mount');
    // Load the window mode before anything else — the terminals sub-process
    // skips most of the heavy init below and renders only the Profile
    // editor.
    try {
      const mode = await bridge.getWindowMode();
      if (mode === 'terminals') {
        windowMode = 'terminals';
      } else if (mode === 'remote') {
        windowMode = 'remote';
      } else if (mode === 'remotes') {
        windowMode = 'remotes';
      }
    } catch { /* fall back to main mode */ }

    if (lifetime.signal.aborted) return;

    if (windowMode === 'remote') {
      // Language and UI mode are client preferences, so they are here
      // before the connection; the theme follows the OS until the host's
      // config arrives. The rest of the setup below runs too, and the
      // host's data loads once the connection is up (onRemoteState).
      try { languageStore.set(normalizeLanguage(await bridge.getLanguage())); } catch (e) { console.error(e); }
      try { uiMode = (await bridge.getUIMode()) === 'simple' ? 'simple' : 'advanced'; } catch (e) { console.error(e); }
      try { hostOS = await bridge.getOS(); } catch (e) { console.error(e); }
      applyTheme();
      // The tint of remote windows, from this computer's preferences; the
      // Go side tells of a change any process saves (#331).
      await session.loadRemoteTint();
      RemoteConnected = (await import('./lib/components/RemoteConnected.svelte')).default;
    }

    if (windowMode === 'remotes') {
      // The Remote hosts window (#330): the language and theme of this
      // computer and its editors, then the view, which loads the remotes
      // itself and again on every focus. Shown after the first paint, as in
      // the terminals window below.
      try { languageStore.set(normalizeLanguage(await bridge.getLanguage())); } catch (e) { console.error(e); }
      const hydrate = async () => {
        try {
          const cfg = await bridge.reloadConfig();
          configStore.set(cfg);
          themeChoice = themeChoiceOf(cfg.global?.theme);
        } catch (e) { console.error(e); }
        applyTheme();
      };
      try { hostOS = await bridge.getOS(); } catch (e) { console.error(e); }
      RemoteHostsWindow = (await import('./lib/components/RemoteHostsWindow.svelte')).default;
      await hydrate();
      if (lifetime.signal.aborted) return;
      markStartup('finished');
      window.addEventListener('focus', () => { hydrate(); }, { signal: lifetime.signal });
      await tick();
      // A hidden WebKit view runs no animation frames, so waiting for two
      // of them left the window to the 1.5 s safety net of DomReady. The
      // first paint is done shortly after the tick; whichever comes first
      // shows the window.
      let shown = false;
      const show = () => { if (!shown) { shown = true; bridge.showWindow(); } };
      requestAnimationFrame(() => requestAnimationFrame(show));
      setTimeout(show, 30);
      return;
    }

    if (windowMode === 'terminals') {
      // Terminals sub-process: just hydrate the bits the editor needs.
      // reloadConfig re-reads the file, so preferences another window saved
      // since this one opened show up too (#240).
      const hydrate = async () => {
        try {
          const cfg = await bridge.reloadConfig();
          configStore.set(cfg);
          themeChoice = themeChoiceOf(cfg.global?.theme);
        } catch (e) { console.error(e); }
        applyTheme();
      };
      try { hostOS = await bridge.getOS(); } catch (e) { console.error(e); }
      TerminalsModal = (await import('./lib/TerminalsModal.svelte')).default;
      await hydrate();
      if (lifetime.signal.aborted) return;
      markStartup('finished');
      // The parent uses HideWindowOnClose, so a "second open" of the
      // Manager just re-shows this same hidden window. Re-hydrate the
      // config on focus so the table reflects any wezterm.lua /
      // installed-shell changes the user made between visits. The Go
      // side also emits 'profiles:reloaded' from the
      // OnSecondInstanceLaunch hook after running SyncProfiles —
      // listen for that explicitly so we pick the refresh up even if
      // the focus event timing is flaky on Linux/Windows.
      window.addEventListener('focus', () => { hydrate(); }, { signal: lifetime.signal });
      session.watchProfiles(() => { hydrate(); });
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

    if (lifetime.signal.aborted) return;
    applyTheme();
    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
      if (themeChoice === 'system') applyTheme();
    }, { signal: lifetime.signal });

    // Re-read config when the window regains focus (picks up external edits),
    // and ask the Go side to re-probe AI harnesses (throttled there) so a
    // tool installed or removed while we were in the background shows up.
    let lostFocus = false;
    window.addEventListener('blur', () => { lostFocus = true; }, { signal: lifetime.signal });
    // A remote window does the same on the host (#295), only while it is
    // connected and not still syncing after a reconnect.
    window.addEventListener('focus', () => {
      // Showing this window for the first time already has a fresh snapshot.
      if (!lostFocus && !currentStream?.full) return;
      lostFocus = false;
      if (firstRun) return; // The wizard owns the choice of the first project.
      if (windowMode === 'remote' && (!remoteShowMain || remoteLoading || !backgroundAllowed(true, remoteState?.state))) return;
      if (focusTimer) clearTimeout(focusTimer);
      focusTimer = setTimeout(() => reloadFromDisk().then(() => { runReconcile(); }, () => {}), 300);
      bridge.refreshAIHarnesses().catch(() => {});
    }, { signal: lifetime.signal });

    // ── Event listeners (always registered) ──
    session.listen();

    if (windowMode === 'remote') {
      session.watchRemoteState((st) => { void onRemoteState(st); });
      try { await onRemoteState(await bridge.remoteInfo()); } catch (e) { console.error(e); }
      return;
    }
    try { $rollbackVersion = await bridge.checkUpdateRecovery(false); }
    catch { pushNotice('error', $t('app.rollbackFailed')); }
    if (lifetime.signal.aborted) return;
    await startMain();
    // Let the first frame paint before confirmation and backup cleanup.
    if (!$rollbackVersion && !lifetime.signal.aborted) requestAnimationFrame(() => setTimeout(() => {
      if (lifetime.signal.aborted) return;
      bridge.checkUpdateRecovery(true).catch(() => pushNotice('error', $t('app.rollbackFailed')));
    }, 0));
  });

  // startMain loads the config and the dashboard: at launch in a local
  // window, on each connection in a remote one.
  async function startMain() {
    // Check if config failed to parse (file exists but is broken/unsupported).
    // Render an in-app modal rather than window.confirm — macOS WebKit has
    // been observed to auto-dismiss confirm() without showing a dialog,
    // which caused GitboxApp to "flash and exit" on mac with a malformed
    // config. The modal stays mounted until the user clicks a button.
    const loadError = await bridge.getConfigLoadError();
    if (lifetime.signal.aborted) return;
    if (loadError) {
      cfgLoadErrorMsg = loadError;
      // Populate configPath and the backup list before showing the screen
      // so everything renders in one pass — no flash of empty state.
      try {
        configPath = await bridge.getConfigPath();
      } catch { /* fall back to the template's default placeholder */ }
      await cfgLoadBackups();
      cfgLoadErrorModal = true;
      markStartup('finished');
      return; // onMount stops here; dashboard init resumes after the user
              // picks Restore / Auto-repair / Start fresh / Exit.
    }

    // Check first run — show onboarding if no folder configured.
    firstRun = await bridge.isFirstRun();
    if (lifetime.signal.aborted) return;
    if (firstRun) {
      events.snapshot(0, () => 0);
      markStartup('finished');
      return;
    }

    try { await initDashboard(); }
    catch (e) { markStartup('finished'); throw e; }
  }

</script>

<svelte:window bind:innerWidth on:click={(e) => {
  const inMenu = e.target instanceof Element && e.target.closest('.action-menu-container');
  const inPRPopover = e.target instanceof Element && e.target.closest('.pr-badge-wrap');
  if (actionMenuRepo && !inMenu) closeActionMenu();
  if (actionMenuAccount && !inMenu) closeAccountMenu();
  if (expandedCard && !(e.target instanceof Element && e.target.closest('.card-open'))) expandedCard = null;
  if (openPRPopover && !inPRPopover) openPRPopover = null;
}} on:keydown={(e) => {
  sidebarKey(e);
  if (e.key === 'Escape' && expandedCard) expandedCard = null;
  if (e.key === 'Escape' && openPRPopover) openPRPopover = null;
}} />

<!-- ════════════════════════════════════════════════════════════ -->
<!--  TEMPLATE                                                   -->
<!-- ════════════════════════════════════════════════════════════ -->

<!-- ── TERMINALS SUB-PROCESS WINDOW (issue #69) ── -->
{#if windowMode === 'terminals' && TerminalsModal}
  <div class="terminals-window-host">
    <svelte:component this={TerminalsModal}
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

<!-- ── REMOTE HOSTS WINDOW (#330) ── -->
{#if windowMode === 'remotes' && RemoteHostsWindow}
  <svelte:component this={RemoteHostsWindow} />
{/if}

<!-- ── REMOTE WINDOW (#239, #240): the connection card until the host's
     data arrives, then the full UI below, limited ── -->
{#if windowMode === 'remote' && RemoteConnected && !remoteShowMain}
  <svelte:component this={RemoteConnected} {simple} />
{/if}

{#if windowMode === 'main' || remoteShowMain}

{#if $rollbackVersion}
  <div class="rollback-notice" role="status">
    <span class="rollback-title">{$t($rollbackVersion === 'previous' ? 'app.rollbackUnknown' : 'app.rollbackNotice', { version: $rollbackVersion })}</span>
    <button class="text-btn" disabled={rollbackBusy} on:click={() => resolveUpdate(true)}>{$t('app.rollback')}</button>
    {#if !cfgLoadErrorModal}
      <button class="text-btn" disabled={rollbackBusy} on:click={() => resolveUpdate(false)}>{$t('app.keepUpdate')}</button>
    {/if}
    {#if rollbackError}<span role="alert">{$t('app.rollbackFailed')}</span>{/if}
  </div>
{/if}
<!-- ── CONFIG LOAD ERROR ── -->
{#if cfgLoadErrorModal}
<ConfigErrorScreen {configPath} {cfgLoadErrorMsg} {cfgRepairFailure} {cfgBackups} {cfgLoadErrorBusy} {cfgRestoreBackup} {cfgDoRepair} {cfgStartFresh} />

<!-- ── ONBOARDING ── -->
{:else if firstRun}
<Onboarding {simple} mode="first-run" onDone={finishOnboarding} />
{:else}

<div class="app" class:remote-offline={remoteOffline}>

  {#if remoteOffline && remoteState}
    <div class="remote-offline-banner" role="status">
      {#if remoteState.state === 'connected'}
        <span>{$t('remote.syncing', { alias: remoteState.alias })}</span>
      {:else}
        <span>{$t('remote.offline', { alias: remoteState.alias })}</span>
        {#if remoteState.state === 'connecting' && remoteState.waiting}<span class="remote-waiting" role="status"><span aria-hidden="true">&#9888;</span> {$t(`remote.waiting.${remoteState.waiting}`)}</span>{/if}
        {#if remoteState.state === 'connecting' && remoteState.attempt}<span class="remote-attempt">{$t('remote.reconnecting', { attempt: remoteState.attempt, attempts: remoteState.attempts ?? remoteState.attempt })}</span>{/if}
        <button class="remote-reconnect" disabled={remoteState.state === 'connecting' || remoteState.state === 'installing'}
          on:click={() => bridge.remoteReconnect().catch(console.error)}>{$t('remote.reconnect')}</button>
      {/if}
    </div>
  {/if}
  <ReloadErrorBanner bind:reloadError remote={windowMode === 'remote'} />
  <OfflineBanner hosts={$networkDown} retrying={$fetchingAll} onRetry={() => runFetchAll(true)} />
  <CredentialWarning findings={credentialFindings} onReview={openDoctorModal} />

  <!-- ══════ DASHBOARD ══════ -->

  <!-- ── TOP BAR ── -->
  <TopBar {simple} syncing={$syncing} pullPlanBusy={loadingPullPlan || !!pullPlan} fetchingAll={$fetchingAll} {themeChoice} bind:deleteMode bind:showSettings remote={windowMode === 'remote' ? remoteState : null}
    {syncAll} runFetchAll={() => runFetchAll(true)} {cycleTheme} {setUIMode} {rail} {sidebarShortcut} {toggleSidebar} {setView} />

  <!-- ── ACTIVITY STRIP (Fetch all / Pull all) ── -->
  <ActivityStrip run={$pullRun || $fetchRun} {simple} />

  <!-- ── GLOBAL WARNINGS (identity, GCM helper, gitignore) ── -->
  <Banners bind:globalIdentityWarn bind:globalGCMWarn bind:gitignoreWarn bind:gitignoreInstalled bind:gitignoreError
    {globalGCMFixing} {gitignoreInstalling} {fixGlobalIdentity} {fixGlobalGCM} {installGlobalGitignore} />

  <!-- ── SETTINGS PANEL ── -->
  <SettingsPanel {onRecipeImported} {showSettings} {simple} {configPath} {languageChoice} {themeChoice} {fetchInterval} lastFetchTime={$lastFetchTime}
    {autostartSupported} {autostartOn} {checkGitignorePref} {doctorSummary} {terminalsManagerOpening} {appVersion}
    {openRoots} {setLanguage} {setTheme} {setFetchInterval} {toggleAutostart} {setPRBadgesEnabled}
    {setPRIncludeDrafts} {setCheckGitignorePref} {openDoctorModal} {openTerminalsManagerWindow} />

  <div class="body">
    <!-- ── SIDEBAR ── -->
    <Sidebar view={currentView} {rail} {simple} credStatuses={$credStatuses} {attention} {totalRepos} {orphanCount} scanIncomplete={skippedFolders > 0} ignoredCount={ignoredClones.length}
      {setView} onAddAccount={addAccount} {showOrphanModal} />

    <main class="content">
    {#if currentView === 'attention'}
      <!-- ── NEEDS ATTENTION ── -->
      <AttentionView {rankOf} {repoNesting} credStatuses={$credStatuses} {rowProps} {setView} {fixCredential} onAddAccount={addAccount} />
    {:else if currentView === 'all'}
      <!-- ── ALL REPOS ── -->
      <RepoList {treeRows} {collapsedSources} credStatuses={$credStatuses} {actionMenuAccount} {rowProps}
        {toggleSourceCollapsed} {toggleAccountMenu} {closeAccountMenu} {askDeleteAccount} {openDiscover} {openCreateRepo} {openEditAccount} {openCredChange} />
    {:else if currentView === 'mirrors'}
      <!-- ── MIRRORS ── -->
      <MirrorsTab bind:expandedCard {deleteMode} mirrorDiscoverLoading={$mirrorDiscoverLoading} mirrorDiscoverProgress={$mirrorDiscoverProgress} mirrorChecking={$mirrorChecking} mirrorSettingUp={$mirrorSettingUp}
        {checkMirrorStatus} {setupMirrorRepo} {openTokenSetup} {runMirrorDiscover} {checkAllMirrorStatus}
        onAddGroup={() => addMirrorGroupDialog.open()} onAddRepo={(mirrorKey) => addMirrorRepoDialog.open(mirrorKey)}
        onDeleteGroup={(mirrorKey) => deleteMirrorGroupDialog.ask(mirrorKey)} onDeleteRepo={(mirrorKey, repoKey) => deleteMirrorRepoDialog.ask(mirrorKey, repoKey)} />
    {:else}
      <!-- ── ONE ACCOUNT ── -->
      <AccountView accountKey={viewAcct} {repoNesting} {rankOf} credStatuses={$credStatuses} {actionMenuAccount} {rowProps}
        {toggleAccountMenu} {closeAccountMenu} {askDeleteAccount} {openEditAccount} {openCredChange} {openDiscover} {openCreateRepo} {fixCredential} />
    {/if}
    </main>
  </div>

  <!-- ── SUMMARY FOOTER ── -->
  <Footer {simple} bind:updateInfo={$updateInfo} bind:updateApplying={$updateApplying} bind:updateError bind:updateDismissed={$updateDismissed} updateProgress={$updateProgress} updateDone={$updateDone} />

  {#if pullPlan}
    <PullPlanModal plan={pullPlan} {simple} onCancel={() => { pullPlan = null; }} onConfirm={confirmPullPlan} />
  {/if}

  <!-- ── DISCOVER MODAL ── -->
  <DiscoverModal bind:this={discoverDialog} {reloadFromDisk} {cloneRepos} {openCredChange} onNeedsToken={(acctKey) => sshTokenDialog.openFor(acctKey)} />

  <!-- ── TERMINALS & SHELLS (issue #69) ──
       The Manager opens in its own OS window via openTerminalsManagerWindow
       (subprocess spawn), so no in-app modal is mounted here. -->

  <!-- ── DOCTOR (SYSTEM CHECK) MODAL ── -->
  <DoctorModal advanced={!simple} bind:this={doctorModal} bind:doctorSummary bind:credentialFindings />

  <!-- ── SSH DISCOVERY TOKEN MODAL ── -->
  <SshTokenModal bind:this={sshTokenDialog} {openDiscover} />

  <!-- ── EDIT ACCOUNT MODAL ── -->
  <EditAccountModal bind:this={editAccountDialog} {reloadFromDisk} />

  <!-- ── DELETE CONFIRMATION MODAL ── -->
  <DeleteCloneModal bind:this={deleteCloneDialog} bind:expandedRepo bind:repoDetail bind:deleteMode {simple} {reloadFromDisk} {setFolderDeleteFailure} />

  <!-- ── SWEEP CONFIRM MODAL ── -->
  <SweepModal bind:this={sweepDialog} {closeActionMenu} />

  <!-- ── ORPHAN ADOPTION MODAL ── -->
  <OrphansModal bind:this={orphansDialog} unmatched={orphanList} ignored={ignoredClones} skipped={skippedClones} {skippedFolders} {skippedScans} {applyReconcile} {revealLabel} profiles={configProfiles} addAccountFor={addAccountForClone} />

  <!-- ── ADD ACCOUNT MODAL ── -->
  {#if connectModal}
    <Onboarding {simple} mode="add" onDone={finishConnect} onCancel={() => { connectModal = false; reloadFromDisk(); }} />
  {/if}

  <AddAccountModal bind:this={addAccountDialog} {simple} bind:credResult bind:credTokenInput bind:credPrecheck {credBusy} {credTokenGuide} {credTokenScopes} {sshKeyCopied} {copySSHKey} {resetCredState} {verifyAllCredentials} {reloadFromDisk} {runCredentialPrecheck} {runCredSetup} {fetchTokenGuide} {verifySSHConnection} {regenerateSSHKey} />

  <!-- ── CHANGE CREDENTIAL MODAL ── -->
  <CredentialModal bind:this={credentialDialog} {simple} bind:credResult bind:credBusy bind:credTokenInput bind:credPrecheck bind:credStatuses={$credStatuses} {credTokenGuide} {credTokenScopes} {sshKeyCopied} {serverOS} {copySSHKey} {resetCredState} {verifyAllCredentials} {reloadFromDisk} {runCredentialPrecheck} {fetchTokenGuide} {credPostSetup} {storeCredToken} {verifySSHConnection} {regenerateSSHKey} />

  <!-- ── DELETE ACCOUNT MODAL ── -->
  <DeleteAccountModal bind:this={deleteAccountDialog} bind:deleteMode {simple} {reloadFromDisk} />

  <!-- ── CHANGE FOLDER MODAL ── -->
  <RootsModal bind:this={rootsDialog} {reloadFromDisk} {runReconcile} />
  <RootPickerModal bind:this={rootPicker} />
  <WorkspacePickerModal bind:this={workspacePicker} />

  <!-- ── ADD MIRROR GROUP MODAL ── -->
  <AddMirrorGroupModal bind:this={addMirrorGroupDialog} />

  <!-- ── ADD MIRROR REPO MODAL ── -->
  <AddMirrorRepoModal bind:this={addMirrorRepoDialog} {setupMirrorRepo} />

  <!-- ── DELETE MIRROR GROUP CONFIRM ── -->
  <DeleteMirrorGroupModal bind:this={deleteMirrorGroupDialog} bind:deleteMode />

  <!-- ── DELETE MIRROR REPO CONFIRM ── -->
  <DeleteMirrorRepoModal bind:this={deleteMirrorRepoDialog} bind:deleteMode />

  <!-- ── MIRROR SETUP RESULT MODAL ── -->
  <MirrorSetupResultModal bind:mirrorSetupResultModal={$mirrorSetupResultModal} />

  <!-- ── MIRROR DISCOVER RESULTS MODAL ── -->
  <MirrorDiscoverModal bind:mirrorDiscoverResults={$mirrorDiscoverResults} bind:discoverAdded mirrorDiscoverError={$mirrorDiscoverError} {checkAllMirrorStatus} />

  <!-- ── MIRROR CREDENTIAL WARNING MODAL ── -->
  <MirrorTokenModal bind:mirrorCredWarning storeMirrorPat={openTokenSetup} />

  <!-- ── CREATE REPO MODAL ── -->
  <CreateRepoModal bind:this={createRepoDialog} />

  <!-- ── RESULT CARDS AND MESSAGES ── -->
  <Notices />
  <ExternalConfirm />

</div>
{/if}

{/if}
<!-- /windowMode === 'main' -->

<!-- ════════════════════════════════════════════════════════════ -->
<!--  STYLES                                                     -->
<!-- ════════════════════════════════════════════════════════════ -->

<style>
  .rollback-notice {
    position: fixed; top: 12px; right: 12px; z-index: 10001;
    display: flex; flex-wrap: wrap; gap: 8px; align-items: center;
    width: min(400px, calc(100vw - 24px)); box-sizing: border-box;
    padding: 12px; background: var(--bg-card); color: var(--text-primary);
    border: 1px solid var(--border); border-radius: 8px; box-shadow: 0 8px 28px #0004;
  }
  .rollback-title { width: 100%; }
  .app { height: 100vh; display: flex; flex-direction: column; overflow: hidden; }
  /* A remote window after a disconnect (#240): the last state stays,
     dimmed and inert, under the banner. */
  .app.remote-offline > :global(:not(.remote-offline-banner)) { opacity: 0.45; pointer-events: none; filter: grayscale(0.6); transition: opacity 0.2s; }
  .remote-offline-banner {
    display: flex; align-items: center; gap: 12px; padding: 8px 24px; font-size: 13px;
    color: var(--text-primary); background: rgba(216, 30, 91, 0.10); border-bottom: 1px solid rgba(216, 30, 91, 0.35);
  }
  .remote-reconnect {
    margin-left: auto; padding: 4px 14px; border-radius: 6px; border: 1px solid #2563eb; background: #2563eb;
    color: #fff; font-size: 12px; font-weight: 500; cursor: pointer;
  }
  .remote-reconnect:disabled { opacity: 0.55; cursor: default; }
  /* The colors of the global warnings (Banners.svelte): an action waits. */
  .remote-waiting {
    padding: 4px 10px; background: #431407; border: 1px solid #c2410c; border-radius: 6px;
    font-size: 12px; color: #fdba74; line-height: 1.4;
  }
  :global([data-theme="light"]) .remote-waiting { background: #fff7ed; border-color: #c2410c; color: #7c2d12; }
  /* Sidebar next to the view; each view scrolls on its own. */
  .body { flex: 1; min-height: 0; display: flex; position: relative; }
  .content { flex: 1; min-width: 0; display: flex; flex-direction: column; container: content / inline-size; }
  /* Holds the rail's place while the full sidebar opens over the content. */
</style>
