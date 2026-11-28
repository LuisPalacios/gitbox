// SessionController (#353, #379, #380, #381): one window's session. It owns
// the event subscriptions, the run bookkeeping they update (status passes,
// Fetch all, Pull all, clones, mirrors, credentials, reconcile, updates)
// and the lifecycle: the config load, the dashboard snapshot, the startup
// status pass and a remote window's connection. It reads and writes only
// its own store bundle (lib/session/bundle.ts), notices and texts included.
// App.svelte creates one, starts it, renders from its stores and keeps
// layout, modals and navigation, which the session reaches through
// SessionHost.
import { tick } from 'svelte';
import { get, readonly, writable } from 'svelte/store';
import type { StoreBundle } from './bundle';
import { acceptStatusBatch, applyRepairReports, statusPaintQueue } from '../status';
import { completionStatuses, hasConfiguredRepo } from '../events';
import type { createEventRouter } from '../events';
import type { bridge as appBridge, RemoteState } from '../bridge';
import type { EventMetadata, EventPayloads } from '../dto.generated';
import { startFetch, startPull, noteDone, noteCompletions, summarizeFetch, summarizePull, localWorkCount } from '../activity';
import type { Run, Completion, FetchSummary, Unsafe } from '../activity';
import { errorText } from '../notice-list';
import { backgroundAllowed, clearsOnReconnect, reloadErrorShown } from '../readonly';
import { createTexts } from '../i18n';
import type { Vars } from '../i18n';
import { tintOf } from '../tint';
import { hostFromState } from '../remotehost';
import type { Placeholder } from '../repo-tree';
import type { ConfigBackupInfo, CredentialFinding, CredentialStatus, MirrorSetupResult, OrphanRepoDTO, PullSkipReason, ReconcileDTO, RepoState, StartupSnapshot, UpdateInfo, WorkspaceChoiceDTO } from '../types';

export type EventRouter = ReturnType<typeof createEventRouter>;

/** The bridge calls a session makes on its own. */
export type SessionBridge = Pick<typeof appBridge,
  'getAllStatus' | 'getConfig' | 'reloadConfig' | 'runTask' | 'pullAll' | 'credentialVerify' | 'getRemoteTint'
  | 'getStartupSnapshot' | 'beginStatus' | 'markStartup' | 'refreshStatus' | 'reconcile' | 'repoNesting' | 'remoteInfo'
  | 'getConfigLoadError' | 'getConfigPath' | 'isFirstRun' | 'listConfigBackups' | 'restoreFromBackup' | 'repairConfig' | 'acknowledgeConfigError'>;

/** The status pass the session's lifecycle started and status:batch fills. */
interface StartupStream {
  id: number;
  first: () => void;
  finish: () => void;
  full: boolean;
  painted: boolean;
  pending: Set<string>;
  paint: ReturnType<typeof statusPaintQueue>;
}

/** What the session asks of the window: the UI state that stays in App.svelte. */
export interface SessionHost {
  simple(): boolean;
  /** Applies the window's own settings a dashboard snapshot carries; keepView on a remote window's reconnect. */
  applySnapshot(snapshot: StartupSnapshot, keepView: boolean): void;
  /** Applies the window settings a reload from disk may change. */
  applyReload(snapshot: StartupSnapshot): void;
  /** The window's checks once the first status is in: global git config, autostart, PR badges. */
  startupChecks(): Promise<void>;
  askWorkspaceChoices(choices: WorkspaceChoiceDTO[]): void;
  /** Marks the window as the remote one of alias, before its data loads; the session has already marked its own bundle. */
  markRemote(alias: string): void;
  refreshDetail(): void;
  discoverDone(data: EventPayloads['discover:done']): void;
  quit(): void;
}

export interface SessionDeps {
  bridge: SessionBridge;
  events: EventRouter;
  host: SessionHost;
  /** The session's own stores (createStoreBundle); the only ones it writes. */
  stores: StoreBundle;
}

export type SessionController = ReturnType<typeof createSessionController>;

export function createSessionController({ bridge, events, host, stores }: SessionDeps) {
  const {
    configStore, accounts, sources, repoStates, mirrors, workspacePlaceholders, workspaceMembers, workspaceUnknown,
    workspaceDiagnostics, applyStatusResults, applyMirrorStatusResults, applyPRUpdate, pushNotice, dismissNotice,
  } = stores;
  // The session's texts name its own host (#295).
  const texts = createTexts(stores.remoteTextHost);
  const $t = (key: string, vars?: Vars) => get(texts.t)(key, vars);
  const subscriptions: (() => void)[] = [];
  const onEvent: EventRouter['on'] = (name, handler, stale) => {
    const off = events.on(name, handler, stale);
    subscriptions.push(off);
    return off;
  };

  // ── What the window renders ──
  const syncing = writable(false);
  // Fetch all and Pull all runs (#138): the strip shows their scope while
  // they run, and a result card sums them up.
  const fetchRun = writable<Run | null>(null);
  const pullRun = writable<Run | null>(null);
  const fetchingAll = writable(false);
  // The repos fetching right now (separate from status).
  const fetchingRepos = writable<Record<string, boolean>>({});
  const lastFetchTime = writable('');
  // Servers the backend cannot reach right now; the offline banner shows
  // while the list is not empty.
  const networkDown = writable<string[]>([]);
  const credStatuses = writable<Record<string, CredentialStatus>>({});
  const mirrorChecking = writable<Record<string, boolean>>({});
  // Per-row "setup in progress", keyed by `${mirrorKey}/${repoKey}`.
  const mirrorSettingUp = writable<Record<string, boolean>>({});
  const mirrorSetupResultModal = writable<MirrorSetupResult | null>(null);
  const mirrorDiscoverLoading = writable(false);
  const mirrorDiscoverResults = writable<NonNullable<EventPayloads['mirror:discover:done']['results']> | null>(null);
  const mirrorDiscoverError = writable('');
  const mirrorDiscoverProgress = writable<EventPayloads['mirror:discover:progress'] | null>(null);
  const updateInfo = writable<UpdateInfo | null>(null);
  const updateApplying = writable(false);
  const updateProgress = writable('');
  const updateDone = writable(false);
  // The version whose pill was dismissed, for this session only.
  const updateDismissed = writable('');
  const rollbackVersion = writable('');
  // The color a remote window tints its top bar and sidebar with (#331).
  const remoteTint = writable('');
  // No folder configured yet: the window shows the first-run onboarding.
  const firstRun = writable(false);
  // A config file that exists but failed to parse (#60): the window shows
  // the recovery screen, with the dated backups it can restore.
  const cfgLoadErrorModal = writable(false);
  const cfgLoadErrorMsg = writable('');
  const cfgLoadErrorBusy = writable(false);
  const cfgRepairFailure = writable('');
  const cfgBackups = writable<ConfigBackupInfo[]>([]);
  const configPath = writable('');
  // The system of the host whose data the window shows (#240).
  const serverOS = writable('');
  // The banner of a failed reload, cleared by the next one that succeeds.
  const reloadError = writable('');
  // child "source/repo" → parent "source/repo", derived from resolved paths.
  const repoNesting = writable<Record<string, string>>({});
  const credentialFindings = writable<CredentialFinding[]>([]);
  // The last reconcile pass (#149): the clones that need me (Unmatched
  // clones), and the containers with several workspace files and no pick.
  const orphanList = writable<OrphanRepoDTO[]>([]);
  const ignoredClones = writable<OrphanRepoDTO[]>([]);
  const skippedClones = writable<NonNullable<ReconcileDTO['skipped']>>([]);
  const skippedFolders = writable(0);
  const skippedScans = writable<NonNullable<ReconcileDTO['skippedScans']>>([]);
  const orphanCount = writable(0);
  const workspaceChoices = writable<Set<string>>(new Set());
  // A remote window's connection (#240): its data loads on the first
  // connection and again on a reconnect that could not resume.
  const remoteState = writable<RemoteState | null>(null);
  const remoteLoaded = writable(false);
  const remoteLoading = writable(false);

  // ── Run bookkeeping ──
  const completion = completionStatuses();
  const fetchOperations = new Map<string, string>();
  // The status each row had before a pull or clone started, to put it back
  // when the backend did not attempt the work because the server is down.
  const statusBeforeSync: Record<string, RepoState['status']> = {};
  let fetchOperation = '';
  let mirrorDiscoveryDone = '';
  let fetchManual = false;
  // A remote window's run that crossed a reconnect (#247): the call waits
  // for the host's run, and when the host's fetch:alldone did not reach
  // the window, the answer ends the run instead.
  let fetchCrossedReconnect = false;
  // The last fetch card with failures and the repos it lists. It goes away
  // when those repos fetch fine again or the next fetch run starts.
  let fetchFailNotice: { id: number; keys: Set<string> } | null = null;
  let fetchCompletions: Completion[] = [];
  let fetchProgressTimer: ReturnType<typeof setTimeout> | undefined;
  let pullRunID: string | null = null;
  let pullRunOperation = '';
  let pullRunPlanID = '';
  let statusGeneration = 0;
  let credentialGeneration = 0;
  let statusWaiters: Array<() => void> = [];
  const recheckingAccounts = new Set<string>();

  // ── Lifecycle bookkeeping ──
  let remote = false;
  let stopped = false;
  let statusPass = 0;
  let currentStream: StartupStream | null = null;
  // The startup work after the first status is scheduled (startup timing).
  let remoteScheduled = false;
  let reloadGeneration = 0;
  const observedStages = new Set<string>();
  let timingQueue = Promise.resolve();
  // A clone git never finished is reported once per session (#295).
  const incompleteShown = new Set<string>();

  const configuredRepo = (source: string, repo: string) => hasConfiguredRepo(get(configStore), source, repo);
  const statusOfKey = (key: string) => get(repoStates)[key]?.status || 'unknown';
  const accountOfKey = (key: string) => get(sources)[key.slice(0, key.indexOf('/'))]?.account || key.slice(0, key.indexOf('/'));
  const repoKey = ({ source, repo }: { source: string; repo: string }) => `${source}/${repo}`;

  function publishCompletionRow(row: EventPayloads['status:one']) {
    currentStream?.paint.flush();
    statusGeneration++;
    applyStatusResults([row], true, true);
  }

  // notAttempted handles a pull or clone the backend stopped for lack of a
  // server: the row goes back to what it was instead of turning into an
  // error.
  function notAttempted(key: string, data: { code?: string; error?: string }, operation: 'clone' | 'pull') {
    repoStates.update((s) => {
      if (s[key]) s[key] = {
        ...s[key],
        status: statusBeforeSync[key] || s[key].status,
        progress: 0,
        problem: { code: data.code, detail: data.error },
        failedOperation: operation,
      };
      return { ...s };
    });
  }

  function markSyncStart(key: string, next: 'syncing' | 'cloning') {
    repoStates.update((s) => {
      const row = s[key];
      if (!row || row.status === next) return s;
      statusBeforeSync[key] = row.status;
      return { ...s, [key]: { ...row, status: next, progress: 0, pullSkipped: undefined, problem: undefined, failedOperation: undefined } };
    });
  }

  // markCloneStart shows a clone the window starts as cloning.
  function markCloneStart(key: string) {
    statusBeforeSync[key] = get(repoStates)[key]?.status || 'unknown';
    repoStates.update((s) => {
      s[key] = { ...s[key], status: 'cloning', progress: 0, problem: undefined, failedOperation: undefined };
      return { ...s };
    });
  }

  function updateRepairWarning(source: string, repo: string, warnings?: string[]) {
    if (warnings && get(repoStates)[`${source}/${repo}`]?.repairWarnings) {
      repoStates.set(applyRepairReports(get(repoStates), [{ source, repo, warnings }]));
    }
  }

  // ── Fetch all ──
  function clearFetchFailNotice() {
    if (fetchFailNotice) dismissNotice(fetchFailNotice.id);
    fetchFailNotice = null;
  }

  function flushFetchProgress() {
    clearTimeout(fetchProgressTimer);
    fetchProgressTimer = undefined;
    const run = get(fetchRun);
    if (run && fetchCompletions.length) fetchRun.set(noteCompletions(run, fetchCompletions));
    fetchCompletions = [];
  }

  // unsafeLines says, once per setting, how many clones Gitbox paused for
  // safety and where to look (#344).
  function unsafeLines(unsafe: Unsafe[]): string[] {
    const simple = host.simple();
    return unsafe.map((u) => simple
      ? $t('result.unsafeSimple', { n: u.count })
      : $t('result.unsafe', { n: u.count, setting: u.setting || $t('result.unsafeAnySetting') }));
  }

  // failureText explains a failure: the catalog entry for its code when the
  // backend classified it, its own text otherwise.
  function failureText(failure: { error: string; code?: string }): string {
    const key = failure.code ? `error.${failure.code}` : '';
    const hint = key ? $t(key) : '';
    return hint && hint !== key ? hint : failure.error;
  }

  // fetchProblem is the short reason a fetch-all run failed, from its
  // summary, for the line in Preferences; '' when the summary has none.
  function fetchProblem(sum: FetchSummary | null): string {
    if (!sum) return '';
    return [
      ...unsafeLines(sum.unsafe),
      sum.firstFailure && $t('result.failed', { n: sum.failed, error: `${sum.firstFailure.key}: ${failureText(sum.firstFailure)}` }),
    ].filter(Boolean).join(' · ');
  }

  // finishFetch leaves the result card of a fetch run and returns its
  // summary, or null when there was no run. Startup and periodic fetches
  // only leave a card when something changed or failed.
  function finishFetch(error?: string): FetchSummary | null {
    flushFetchProgress();
    const run = get(fetchRun);
    const manual = fetchManual;
    const simple = host.simple();
    fetchRun.set(null);
    fetchManual = false;
    if (!run) {
      if (manual && error) pushNotice('error', $t(simple ? 'result.checkTitle' : 'result.fetchTitle'), [error]);
      return null;
    }
    const creds = get(credStatuses);
    const offline = Object.keys(creds).filter((k) => creds[k]?.status === 'offline');
    const sum = summarizeFetch(run, statusOfKey, accountOfKey, offline);
    if (!manual && !sum.changed) return sum;
    // Nothing was fetched because no server answered: the offline banner
    // already says so, and one card per attempt would only pile up.
    if (get(networkDown).length > 0 && sum.checked === 0 && sum.failed === 0 && sum.unsafe.length === 0) return sum;
    const skippedAccounts = sum.skippedAccounts.join(', ');
    const unreachable = sum.unreachable.map((u) => $t(simple ? 'result.unreachableSimple' : 'result.unreachable', { n: u.count, host: u.host || $t('result.theServer') }));
    // A connect refused within milliseconds is the OS blocking it, not a
    // slow or down server; on macOS that's usually Local Network privacy.
    // The fetches ran on the host that has the data: its OS decides.
    const lanHint = get(serverOS) === 'darwin' && sum.unreachable.some((u) => u.fast) && $t('result.lanHint');
    const failedKind = sum.unreachable.length > 0 || sum.unsafe.length > 0 || sum.failed > 0;
    const id = pushNotice(failedKind ? 'error' : 'success', $t(simple ? 'result.checkTitle' : 'result.fetchTitle'), simple ? [
      $t('result.checked', { n: sum.checked }),
      sum.nowBehind > 0 && $t('result.newerOnline', { n: sum.nowBehind }),
      sum.skipped > 0 && $t('result.skippedSimple', { n: sum.skipped, accounts: skippedAccounts }),
      ...unreachable,
      ...unsafeLines(sum.unsafe),
      sum.failed > 0 && $t('result.needHelp', { n: sum.failed }),
      lanHint,
      sum.localWork > 0 && $t('result.wipUntouched'),
    ] : [
      $t('result.fetched', { n: sum.checked }),
      sum.nowBehind > 0 && $t('result.nowBehind', { n: sum.nowBehind }),
      sum.skipped > 0 && $t('result.skipped', { n: sum.skipped, accounts: skippedAccounts }),
      ...unreachable,
      ...unsafeLines(sum.unsafe),
      sum.firstFailure && $t('result.failed', { n: sum.failed, error: `${sum.firstFailure.key}: ${failureText(sum.firstFailure)}` }),
      lanHint,
      sum.localWork > 0 && $t('result.localUntouched'),
    ]);
    if (failedKind) fetchFailNotice = { id, keys: new Set(Object.keys(run.errors)) };
    return sum;
  }

  // fetchAllEnded closes the fetch-all run the window shows.
  function fetchAllEnded(error?: string) {
    fetchOperation = '';
    fetchOperations.clear();
    fetchingAll.set(false);
    fetchCrossedReconnect = false;
    fetchingRepos.set({});
    const sum = finishFetch(error);
    const now = new Date();
    lastFetchTime.set(error ? $t('app.fetchAllError', { error: fetchProblem(sum) || error })
      : now.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }));
  }

  // runFetchAll fetches every clone. A manual run (the top-bar button)
  // always leaves a result card, also when it joins a run already going.
  // It returns false when the window joined a run already going.
  async function runFetchAll(manual = false): Promise<boolean> {
    if (manual) fetchManual = true;
    if (get(fetchingAll)) return false;
    fetchingAll.set(true);
    fetchCrossedReconnect = false;
    try {
      if (await bridge.runTask('fetch-all') === 'already-running') return false;
      if (fetchCrossedReconnect && get(fetchingAll)) fetchAllEnded();
    } catch (e) {
      lastFetchTime.set($t('app.fetchAllError', { error: errorText(e) }));
      if (get(fetchingAll)) finishFetch(errorText(e));
      fetchingAll.set(false);
      fetchingRepos.set({});
    }
    return true;
  }

  // ── Pull all ──
  function finishPull(skippedSincePreview = 0) {
    const run = get(pullRun);
    pullRun.set(null);
    if (!run) return;
    const simple = host.simple();
    const sum = summarizePull(run);
    const unsafeCount = sum.unsafe.reduce((n, u) => n + u.count, 0);
    if (sum.pulled + sum.cloned + sum.skipped + sum.failed + unsafeCount + skippedSincePreview === 0) return;
    // Nothing was pulled because no server answered: the offline banner
    // already says so.
    const stoppedByNetwork = sum.failed > 0 && sum.failures.every((f) => run.failures[f.key]?.code === 'git.offline');
    if (get(networkDown).length > 0 && stoppedByNetwork && sum.pulled === 0 && sum.cloned === 0 && sum.skipped === 0 && unsafeCount === 0 && skippedSincePreview === 0) return;
    const first = sum.failures[0];
    const localWork = localWorkCount(Object.keys(get(repoStates)), statusOfKey) > 0;
    pushNotice(sum.failed > 0 || unsafeCount > 0 ? 'error' : 'success', $t(simple ? 'result.updateTitle' : 'result.pullTitle'), simple ? [
      sum.pulled > 0 && $t('result.updated', { n: sum.pulled }),
      sum.cloned > 0 && $t('result.downloaded', { n: sum.cloned }),
      !run.planID && sum.skipped > 0 && $t('result.pullSkipped', { n: sum.skipped }),
      skippedSincePreview > 0 && $t('result.pullPlanSkipped', { n: skippedSincePreview }),
      ...unsafeLines(sum.unsafe),
      sum.failed > 0 && $t('result.needHelp', { n: sum.failed }),
      localWork && $t('result.wipUntouched'),
    ] : [
      sum.pulled > 0 && $t('result.pulled', { n: sum.pulled }),
      sum.cloned > 0 && $t('result.cloned', { n: sum.cloned }),
      !run.planID && sum.skipped > 0 && $t('result.pullSkipped', { n: sum.skipped }),
      skippedSincePreview > 0 && $t('result.pullPlanSkipped', { n: skippedSincePreview }),
      ...unsafeLines(sum.unsafe),
      first && $t('result.failed', { n: sum.failed, error: `${first.key}: ${first.error}` }),
      localWork && $t('result.localUntouched'),
    ]);
  }

  async function executePullAll(planID: string) {
    syncing.set(true);
    const run = crypto.getRandomValues(new Uint32Array(4)).join('-');
    pullRunID = run;
    pullRunPlanID = planID;
    try {
      await bridge.pullAll(run, planID);
    } catch (error) {
      if (pullRunID === run) failPullAll(String(error));
    }
  }

  function failPullAll(error: string) {
    syncing.set(false);
    pullRunID = null;
    pullRunOperation = '';
    pullRunPlanID = '';
    pullRun.set(null);
    pushNotice('error', $t(host.simple() ? 'result.updateTitle' : 'result.pullTitle'), [error]);
  }

  // ── Credentials ──
  function setCredStatus(key: string, status: CredentialStatus) {
    credStatuses.update((s) => { s[key] = status; return s; });
  }

  // An account marked offline whose repo just fetched fine is reachable
  // after all (a refusal right after launch, a VPN that came up), so its
  // credential is checked again, once at a time.
  function recheckOfflineAccount(key: string) {
    if (get(credStatuses)[key]?.status !== 'offline' || recheckingAccounts.has(key)) return;
    recheckingAccounts.add(key);
    const account = JSON.stringify(get(accounts)[key]);
    bridge.credentialVerify(key).then((cs) => {
      if (account !== JSON.stringify(get(accounts)[key])) return;
      setCredStatus(key, { ...cs });
    }).catch(console.error).finally(() => recheckingAccounts.delete(key));
  }

  function verifyAllCredentials() {
    const generation = ++credentialGeneration;
    for (const key of Object.keys(get(accounts))) {
      const account = JSON.stringify(get(accounts)[key]);
      bridge.credentialVerify(key).then((cs) => {
        if (generation !== credentialGeneration || account !== JSON.stringify(get(accounts)[key])) return;
        setCredStatus(key, { ...cs });
      }).catch(console.error);
    }
  }

  // ── Mirrors ──
  async function checkAllMirrorStatus() {
    const keys = Object.keys(get(mirrors));
    if (!keys.length) return;
    for (const key of keys) mirrorChecking.update((m) => ({ ...m, [key]: true }));
    try {
      await bridge.runTask('mirror-status');
    } catch (e) { console.error(e); }
    for (const key of keys) mirrorChecking.update((m) => ({ ...m, [key]: false }));
  }

  // nextStatus resolves on the next status:updated, or after ms.
  function nextStatus(ms: number): Promise<void> {
    return new Promise((resolve) => {
      const timer = setTimeout(resolve, ms);
      statusWaiters.push(() => { clearTimeout(timer); resolve(); });
    });
  }

  // ── Event listeners of the main and remote windows ──
  function listen() {
    // Background harness detection changed global.ai_harnesses and saved
    // it; swap the list in the store without a full config reload.
    onEvent('harnesses:updated', (list) => {
      configStore.update((c) => c ? { ...c, global: { ...c.global, ai_harnesses: list || [] } } : c);
    });

    onEvent('tools:updated', () => {
      bridge.getConfig().then(cfg => configStore.update(current => current ? { ...current, global: { ...current.global, editors: cfg.global.editors, terminal_apps: cfg.global.terminal_apps, shells: cfg.global.shells, terminal_profiles: cfg.global.terminal_profiles } } : current)).catch(console.error);
    });

    onEvent('reconcile:done', (res) => {
      // The coalesced background retry has no caller to apply its result.
      if (res?.background) {
        void applyReconcile(res).catch(console.error);
      } else if (res?.workspaceUnknown) workspaceUnknown.set(new Set(res.workspaceUnknown));
    });

    onEvent('status:batch', async (batch) => {
      const stream = currentStream;
      if (!stream || stream.full || batch.passID !== stream.id) return;
      if (acceptStatusBatch(batch, stream.id, get(configStore)?.revision)) {
        if (batch.items?.length) {
          stream.paint.push(batch.items);
          if (!stream.painted) {
            stream.painted = true;
            await tick();
            if (stream !== currentStream || batch.revision !== get(configStore)?.revision) return;
            markStartup('firstStatus');
            stream.first();
          }
        }
        if (batch.done) {
          stream.paint.flush();
          await tick();
          if (stream !== currentStream || batch.revision !== get(configStore)?.revision) return;
          if (stream.pending.size) {
            // A remote transport may miss an event. Recover the authoritative sweep.
            const rows = await bridge.getAllStatus();
            if (stream !== currentStream || batch.revision !== get(configStore)?.revision) return;
            applyStatusResults(rows, true);
            for (const row of rows) stream.pending.delete(`${row.source}/${row.repo}`);
            if (rows.length) markStartup('firstStatus');
            await tick();
          }
          if (!stream.pending.size) markStartup('fullStatus');
          stream.full = true;
          stream.first();
          finishTiming();
          stream.finish();
        }
      } else if (batch.done) {
        // A config change superseded this pass. A fresh snapshot owns the next one.
        void initDashboard().catch(console.error);
      }
    }, batch => {
      const stream = currentStream;
      if (batch.done && stream && batch.passID === stream.id && !stream.full) {
        void initDashboard().catch(console.error);
      }
    });

    onEvent('status:updated', (results) => {
      currentStream?.paint.flush();
      statusGeneration++;
      applyStatusResults((results || []).filter(row => configuredRepo(row.source, row.repo)));
      for (const resolve of statusWaiters.splice(0)) resolve();
      // Refresh an expanded detail (a reactive statement handles auto-collapse).
      host.refreshDetail();
    });

    const startPullRun = (data: EventPayloads['pull:allstart'], meta: EventMetadata) => {
      if (data.run !== pullRunID || (data.planID || '') !== pullRunPlanID || pullRunOperation) return;
      pullRunOperation = meta.operationID;
      pullRun.set(startPull(data.pulls.map(repoKey), data.clones.map(repoKey), accountOfKey, statusOfKey, data.planID));
    };
    const finishPullRun = (data: EventPayloads['pull:alldone'], meta: EventMetadata) => {
      if (data.run !== pullRunID || (data.planID || '') !== pullRunPlanID || (!meta.client && pullRunOperation && pullRunOperation !== meta.operationID)) return;
      if (data.planExpired) { failPullAll($t('pullPlan.expired')); return; }
      if (data.error) { failPullAll(data.error); return; }
      syncing.set(false);
      pullRunID = null;
      pullRunOperation = '';
      pullRunPlanID = '';
      finishPull(data.skippedSincePreview);
    };
    onEvent('pull:allstart', startPullRun, startPullRun);
    onEvent('pull:alldone', finishPullRun, finishPullRun);

    const noteClone = (data: EventPayloads['clone:done'], meta: EventMetadata) => {
      const run = get(pullRun);
      if (run && data.run === pullRunID) {
        pullRun.set(noteDone(run, repoKey(data), data.error, { code: data.code, host: data.host }, data.skipped ? 'settingsChanged' : undefined, meta.operationID));
      }
    };
    const notePull = (data: EventPayloads['pull:done'], meta: EventMetadata) => {
      const skip = typeof data.skipped === 'string' ? data.skipped as PullSkipReason : undefined;
      const run = get(pullRun);
      if (run && data.run === pullRunID) {
        pullRun.set(noteDone(run, repoKey(data), data.error, { code: data.code, host: data.host, setting: data.unsafeConfig?.setting }, skip, meta.operationID, data.unchanged));
      }
    };

    for (const name of ['clone:start', 'pull:start'] as const) {
      onEvent(name, (data, meta) => {
        const key = repoKey(data);
        if (configuredRepo(data.source, data.repo) && completion.start(key, meta.operationID)) {
          markSyncStart(key, name === 'clone:start' ? 'cloning' : 'syncing');
        }
      });
    }

    onEvent('clone:progress', (data, meta) => {
      if (!configuredRepo(data.source, data.repo)) return;
      const key = `${data.source}/${data.repo}`;
      if (!completion.start(key, meta.operationID)) return;
      repoStates.update((s) => {
        if (s[key]) {
          s[key] = { ...s[key], progress: data.percent };
        }
        return { ...s };
      });
    });

    onEvent('clone:done', (data, meta) => {
      noteClone(data, meta);
      if (!configuredRepo(data.source, data.repo)) return;
      const key = `${data.source}/${data.repo}`;
      const checked = meta.client ? { accept: true } : completion.done(key, meta.operationID, !data.error && !data.skipped);
      if (!checked.accept) return;
      if (data.skipped) { notAttempted(key, data, 'clone'); return; }
      repoStates.update((s) => {
        if (s[key]) {
          s[key] = {
            ...s[key],
            status: data.error ? 'error' : 'pending',
            statusCheckRevision: meta.configRevision,
            // Rows seeded in-session (create-repo, Bring Local) have no path
            // until the next full status refresh; take it from the event so
            // path-based kebab actions work right away (#79).
            path: data.path || s[key].path,
            progress: 0,
            error: data.error,
            problem: data.error ? { code: data.code, detail: data.error } : undefined,
            failedOperation: data.error ? 'clone' : undefined,
            repairWarnings: data.error ? s[key].repairWarnings : data.warnings,
          };
        }
        return { ...s };
      });
      if ('row' in checked && checked.row) publishCompletionRow(checked.row);
    }, noteClone);

    onEvent('pull:done', (data, meta) => {
      notePull(data, meta);
      if (!configuredRepo(data.source, data.repo)) return;
      const key = `${data.source}/${data.repo}`;
      const skipped = data.skipped;
      const checked = meta.client ? { accept: true } : completion.done(key, meta.operationID, !data.error && !skipped && !data.unchanged);
      if (!checked.accept) return;
      updateRepairWarning(data.source, data.repo, data.repairWarnings);
      if (skipped === true) { notAttempted(key, data, 'pull'); return; }
      const reason = skipped ? skipped as PullSkipReason : undefined;
      repoStates.update((s) => {
        const row = s[key];
        if (!row) return s;
        if (skipped || data.unchanged) {
          s[key] = {
            ...row,
            status: (data.state as RepoState['status'] | undefined) ?? statusBeforeSync[key] ?? 'unknown',
            progress: 0,
            modified: data.modified ?? row.modified,
            untracked: data.untracked ?? row.untracked,
            ahead: data.ahead ?? row.ahead,
            behind: data.behind ?? row.behind,
            pullSkipped: reason,
            error: undefined,
            problem: undefined,
            failedOperation: undefined,
          };
        } else {
          s[key] = {
            ...row,
            status: data.error ? 'error' : 'pending',
            statusCheckRevision: meta.configRevision,
            progress: 0,
            pullSkipped: undefined,
            error: data.error,
            problem: data.error ? { code: data.code, detail: data.error, unsafe: data.unsafeConfig } : undefined,
            failedOperation: data.error ? 'pull' : undefined,
          };
        }
        return { ...s };
      });
      if ('row' in checked && checked.row) publishCompletionRow(checked.row);
    }, notePull);

    onEvent('status:one', (row, meta) => {
      if (!configuredRepo(row.source, row.repo)) return;
      const key = `${row.source}/${row.repo}`;
      const result = completion.status(key, meta.operationID, row);
      if (result && get(repoStates)[key]?.status === 'pending') publishCompletionRow(result);
    });

    onEvent('fetch:start', (data, meta) => {
      if (!configuredRepo(data.source, data.repo)) return;
      const key = `${data.source}/${data.repo}`;
      fetchOperations.set(key, meta.operationID);
      fetchingRepos.update((f) => { f[key] = true; return f; });
    });

    onEvent('fetch:done', (data, meta) => {
      if (!configuredRepo(data.source, data.repo)) return;
      const key = `${data.source}/${data.repo}`;
      const active = fetchOperations.get(key);
      if (active && BigInt(active) > BigInt(meta.operationID)) return;
      fetchOperations.delete(key);
      updateRepairWarning(data.source, data.repo, data.repairWarnings);
      if (get(fetchingRepos)[key]) {
        fetchingRepos.update((f) => { delete f[key]; return f; });
      }
      if (get(fetchRun)) {
        fetchCompletions.push({ key, operationID: meta.operationID, error: data.error, failure: { code: data.code, host: data.host, fast: data.refusedFast, setting: data.unsafeConfig?.setting } });
        fetchProgressTimer ??= setTimeout(flushFetchProgress, 125);
      }
      repoStates.update((states) => {
        const state = states[key];
        if (!state || (state.problem && state.failedOperation !== 'fetch') || (!data.error && state.failedOperation !== 'fetch')) return states;
        return { ...states, [key]: {
          ...state,
          problem: data.error ? { code: data.code, detail: data.error, unsafe: data.unsafeConfig } : undefined,
          failedOperation: data.error ? 'fetch' : undefined,
        } };
      });
      if (!data.error && fetchFailNotice?.keys.delete(key) && fetchFailNotice.keys.size === 0) clearFetchFailNotice();
      if (!data.error) recheckOfflineAccount(accountOfKey(key));
    });

    onEvent('network:state', (data) => {
      networkDown.set(data?.offline ? (data.hosts || []) : []);
    });

    onEvent('fetch:allstart', (_, meta) => {
      fetchOperation = meta.operationID;
      fetchingAll.set(true);
      clearFetchFailNotice();
      if (!get(fetchRun)) fetchRun.set(startFetch(Object.keys(get(repoStates)).filter((k) => !['not cloned', 'root offline'].includes(statusOfKey(k))), accountOfKey, statusOfKey));
    });

    onEvent('fetch:alldone', (data, meta) => {
      if (fetchOperation && fetchOperation !== meta.operationID) return;
      fetchAllEnded(data?.error);
    });

    onEvent('credentials:updated', (statuses) => {
      credentialGeneration++;
      credStatuses.set(Object.fromEntries(Object.entries(statuses || {}).filter(([key]) => !!get(configStore)?.accounts[key])));
    });

    onEvent('discover:done', (data) => host.discoverDone(data));

    onEvent('mirror:status', (results) => {
      if (results) {
        applyMirrorStatusResults(results.filter(row => !!get(configStore)?.mirrors[row.mirrorKey]?.repos?.[row.repoKey]));
        // Clear checking flag for the mirror group.
        for (const r of results) {
          if (r.mirrorKey) mirrorChecking.update((m) => ({ ...m, [r.mirrorKey]: false }));
        }
      }
    });

    onEvent('mirror:discover:progress', (data, meta) => {
      if (mirrorDiscoveryDone && BigInt(meta.operationID) <= BigInt(mirrorDiscoveryDone)) return;
      mirrorDiscoverProgress.set(data);
    });

    onEvent('mirror:discover:done', (data, meta) => {
      mirrorDiscoveryDone = meta.operationID;
      mirrorDiscoverLoading.set(false);
      mirrorDiscoverProgress.set(null);
      if (data.error) {
        mirrorDiscoverError.set(data.error);
        mirrorDiscoverResults.set([]);
      } else {
        mirrorDiscoverResults.set(data.results || []);
      }
    });

    onEvent('mirror:setup:done', async (data) => {
      mirrorSetupResultModal.set(data);
      // Clear the per-row spinner. The Go side emits the mirrorKey too
      // (issue #59), so the key is ${mirrorKey}/${repoKey}.
      if (data?.mirrorKey && data?.repoKey) {
        const key = `${data.mirrorKey}/${data.repoKey}`;
        mirrorSettingUp.update(({ [key]: _removed, ...rest }) => rest);
      }
      configStore.set(await bridge.reloadConfig());
      checkAllMirrorStatus();
    });

    onEvent('update:available', (info) => {
      if (info?.latest !== get(updateDismissed)) updateInfo.set(info);
    });

    onEvent('update:recoveryExpired', () => { rollbackVersion.set(''); });
    onEvent('update:progress', (msg: string) => {
      updateProgress.set(msg);
    });

    // Installed and the new version is starting: the backend quits next.
    onEvent('update:restarting', (latest: string) => {
      updateProgress.set($t('app.restarting', { version: latest }));
    });

    // Installed, but the new version could not be started automatically.
    onEvent('update:done', () => {
      updateApplying.set(false);
      updateProgress.set('');
      updateDone.set(true);
    });

    onEvent('update:quit', (latest: string) => {
      // The elevated helper has verified the release and waits for this
      // process to exit before it can replace the executable.
      updateProgress.set($t('app.restarting', { version: latest }));
      // Auto-quit after a brief moment so the user sees the message.
      setTimeout(() => host.quit(), 1500);
    });

    onEvent('pr:refreshed', (upd) => {
      if (get(configStore)?.accounts[upd.accountKey]) applyPRUpdate(upd);
    });
  }

  // loadRemoteTint reads the tint of remote windows from this computer's
  // preferences and follows the changes any process saves (#331). It
  // subscribes first, so no change is missed; a change that arrives before
  // the first read answers wins over it.
  async function loadRemoteTint() {
    let changed = false;
    onEvent('remote:tint', (color: string) => { changed = true; remoteTint.set(tintOf(color)); });
    try {
      const saved = tintOf(await bridge.getRemoteTint());
      if (!changed) remoteTint.set(saved);
    } catch (e) { console.error(e); }
  }

  // watchProfiles calls reload when the Go side re-synced the terminal
  // profiles (the terminals window).
  function watchProfiles(reload: () => void) {
    onEvent('profiles:reloaded', () => { reload(); });
  }

  // noteDisconnect marks a Fetch all that was running when a remote
  // window's connection dropped.
  function noteDisconnect() {
    if (get(fetchingAll)) fetchCrossedReconnect = true;
  }

  // forgetOperations drops the operations a reconnect that could not
  // resume leaves unknown.
  function forgetOperations() {
    completion.clear();
    fetchOperations.clear();
    fetchOperation = '';
    mirrorDiscoveryDone = '';
  }

  // forgetRuns drops the runs in flight: their rows come back from the
  // host's status.
  function forgetRuns() {
    syncing.set(false);
    pullRunID = null;
    pullRunOperation = '';
    pullRun.set(null);
    mirrorSettingUp.set({});
    fetchingRepos.set({});
  }

  // ── Startup timing ──
  // markStartup records each startup stage once, in order.
  function markStartup(stage: string) {
    if (observedStages.has(stage)) return;
    observedStages.add(stage);
    timingQueue = timingQueue.then(() => bridge.markStartup(stage)).catch(console.error);
  }

  function finishTiming() {
    if (currentStream?.full && remoteScheduled) markStartup('finished');
  }

  function retireStream() {
    currentStream?.paint.discard();
    currentStream?.first();
    currentStream?.finish();
    currentStream = null;
  }

  // ── Reconcile (#149) ──
  // A pass adopts the clones gitbox recognises and re-links moved ones by
  // itself; the lists hold the clones that need me.
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

  async function loadNesting() {
    try {
      repoNesting.set(await bridge.repoNesting());
    } catch { /* I retain the last known nesting when a refresh fails. */ }
  }

  // applyReconcile shows a pass: the lists, and the config and statuses when
  // it added or moved anything.
  async function applyReconcile(res: ReconcileDTO) {
    if (res.repairs?.length) repoStates.set(applyRepairReports(get(repoStates), res.repairs));
    if (res.workspaceUnknown) workspaceUnknown.set(new Set(res.workspaceUnknown));
    const unmatched = res.unmatched || [];
    const skipped = res.skipped || [];
    orphanList.set(unmatched);
    ignoredClones.set(res.ignored || []);
    skippedFolders.set(res.skippedFolders || 0);
    skippedScans.set(res.skippedScans || []);
    skippedClones.set(skipped);
    orphanCount.set(unmatched.length + skipped.length);
    if (res.error) pushNotice('error', $t('notice.reconcileFailed'), [res.error]);
    const incomplete = (res.incomplete || []).filter(c => !incompleteShown.has(c.path));
    for (const c of incomplete) incompleteShown.add(c.path);
    if (incomplete.length) pushNotice('error', $t('notice.incompleteClone'), incomplete.map(c => `${c.repo}: ${c.path}`));
    if (res.adopted + res.relinked > 0 || res.workspacesChanged) {
      configStore.set(await bridge.reloadConfig());
      applyStatusResults(await bridge.getAllStatus());
    }
    const byParent: Record<string, Placeholder[]> = {};
    for (const p of res.placeholders || []) (byParent[p.parent] ??= []).push({ name: p.name, path: p.path, outsidePath: p.outsidePath });
    workspacePlaceholders.set(byParent);
    workspaceMembers.set(new Set(res.workspaceMembers || []));
    workspaceDiagnostics.set(Object.fromEntries((res.workspaceDiagnostics || []).map(d => [d.parent, d])));
    workspaceChoices.set(new Set((res.workspaceChoices || []).map(c => `${c.source}/${c.repo}`)));
    await loadNesting();
    host.askWorkspaceChoices(res.workspaceChoices || []);
  }

  // ── Reloads ──
  // reloadFromDisk re-reads the config and refreshes every status. It
  // throws on failure, after it shows the reload banner, so the UI never
  // swallows an integrity issue. It does nothing while the config-load
  // error screen is up (the user's choice drives recovery, not focus
  // events), during first-run onboarding (there is no file to read yet),
  // or in a disconnected remote window (the next connection reloads
  // everything).
  async function reloadFromDisk() {
    if (get(cfgLoadErrorModal) || get(firstRun)) return;
    if (!backgroundAllowed(remote, get(remoteState)?.state)) return;
    const generation = ++reloadGeneration;
    try {
      await bridge.reloadConfig();
      const snapshot = await bridge.getStartupSnapshot();
      if (generation !== reloadGeneration) return;
      repoNesting.set(snapshot.nesting || {});
      credentialFindings.set(snapshot.credentialFindings || []);
      configStore.set(snapshot.config);
      events.snapshot(snapshot.eventWatermark, () => get(configStore)?.revision ?? 0);
      configPath.set(snapshot.configPath);
      host.applyReload(snapshot);
      if (!get(fetchingAll)) {
        const statusVersion = statusGeneration;
        const statuses = await bridge.getAllStatus();
        if (generation !== reloadGeneration) return;
        if (statusVersion === statusGeneration && !get(fetchingAll)) applyStatusResults(statuses);
        if (!get(fetchingAll)) verifyAllCredentials();
      }
      reloadError.set('');
    } catch (err) {
      if (generation === reloadGeneration && reloadErrorShown(err, remote, get(remoteState)?.state)) reloadError.set(errorText(err));
      throw err;
    }
  }

  // reloadConfigFromDisk re-reads the config alone (the terminals window).
  async function reloadConfigFromDisk() {
    await bridge.reloadConfig();
    const snapshot = await bridge.getStartupSnapshot();
    repoNesting.set(snapshot.nesting || {});
    credentialFindings.set(snapshot.credentialFindings || []);
    configStore.set(snapshot.config);
  }

  // ── Dashboard ──
  async function initDashboard() {
    const snapshot = await bridge.getStartupSnapshot();
    if (stopped) return;
    const cfg = snapshot.config;
    // A remote window keeps its view across reconnects: the host's config
    // does not hold it.
    host.applySnapshot(snapshot, remote && get(remoteLoaded));
    repoNesting.set(snapshot.nesting || {});
    credentialFindings.set(snapshot.credentialFindings || []);
    configPath.set(snapshot.configPath);
    serverOS.set(snapshot.serverOS || snapshot.os);
    configStore.set(cfg);
    const known = get(repoStates);
    repoStates.set(Object.fromEntries(Object.entries(cfg.sources).flatMap(([sourceKey, source]) =>
      Object.keys(source.repos).map(repoName => [`${sourceKey}/${repoName}`, known[`${sourceKey}/${repoName}`] ?? { status: 'unknown' as const, progress: 0, behind: 0, modified: 0, untracked: 0, ahead: 0 }])
    )));
    events.snapshot(snapshot.eventWatermark, () => get(configStore)?.revision ?? 0);
    const id = ++statusPass;
    retireStream();
    let first!: () => void;
    const firstReady = new Promise<void>(resolve => { first = resolve; });
    let finish!: () => void;
    const completed = new Promise<void>(resolve => { finish = resolve; });
    const paint = statusPaintQueue(rows => {
      if (currentStream?.id !== id || get(configStore)?.revision !== cfg.revision) return;
      for (const row of rows) currentStream.pending.delete(`${row.source}/${row.repo}`);
      applyStatusResults(rows, true);
    });
    currentStream = { id, first, finish, full: false, painted: false, pending: new Set(Object.keys(get(repoStates))), paint };
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

    // The window's own checks: global git config, autostart, PR badges.
    await host.startupChecks();

    // Reconcile walks the same clone trees as local status. Let the first
    // sweep finish before starting that second disk traversal.
    if (remote) {
      await completed; // The reconnect flow starts its own reconcile after init.
    } else {
      void completed.then(() => { if (id === statusPass) void runReconcile(); });
    }
  }

  // startMain loads the config and the dashboard: at launch in a local
  // window, on each connection in a remote one.
  async function startMain() {
    // A config that failed to parse (file exists but is broken or
    // unsupported) shows the in-app recovery screen, not window.confirm:
    // macOS WebKit has been observed to auto-dismiss confirm() without
    // showing a dialog, which made GitboxApp "flash and exit".
    const loadError = await bridge.getConfigLoadError();
    if (stopped) return;
    if (loadError) {
      cfgLoadErrorMsg.set(loadError);
      // Populate configPath and the backup list before showing the screen
      // so everything renders in one pass — no flash of empty state.
      try {
        configPath.set(await bridge.getConfigPath());
      } catch { /* fall back to the template's default placeholder */ }
      await loadBackups();
      cfgLoadErrorModal.set(true);
      markStartup('finished');
      return; // The dashboard loads after the user picks Restore / Auto-repair / Start fresh / Exit.
    }

    // No folder configured: the window shows onboarding.
    firstRun.set(await bridge.isFirstRun());
    if (stopped) return;
    if (get(firstRun)) {
      events.snapshot(0, () => 0);
      markStartup('finished');
      return;
    }

    try { await initDashboard(); }
    catch (e) { markStartup('finished'); throw e; }
  }

  // ── Config-load error recovery (#60) ──
  async function loadBackups() {
    try {
      cfgBackups.set(await bridge.listConfigBackups());
    } catch (err) {
      console.error('listConfigBackups failed', err);
      cfgBackups.set([]);
    }
  }

  const failureMessage = (err: unknown) => (err as { message?: string } | null)?.message || String(err);

  // recover runs a restore or a repair and loads the dashboard when it
  // succeeded, or onboarding when the config has no folder.
  async function recover(run: () => Promise<{ success: boolean; error?: string }>) {
    cfgLoadErrorBusy.set(true);
    cfgRepairFailure.set('');
    try {
      const res = await run();
      if (!res.success) {
        cfgRepairFailure.set(res.error || $t('app.unknownError'));
        await loadBackups(); // the failed attempt may have altered the backup count
        return;
      }
      cfgLoadErrorModal.set(false);
      cfgLoadErrorMsg.set('');
      firstRun.set(await bridge.isFirstRun());
      if (get(firstRun)) return;
      await initDashboard();
    } catch (err) {
      cfgRepairFailure.set(failureMessage(err));
    } finally {
      cfgLoadErrorBusy.set(false);
    }
  }

  const restoreBackup = (path: string) => recover(() => bridge.restoreFromBackup(path));
  const repairConfig = () => recover(() => bridge.repairConfig());

  // startFresh acknowledges the corruption so the first save is no longer
  // refused, and walks through onboarding. The dated backup keeps the
  // pre-overwrite copy, so the user can recover manually if needed.
  async function startFresh() {
    cfgLoadErrorBusy.set(true);
    try {
      await bridge.acknowledgeConfigError();
    } finally {
      cfgLoadErrorBusy.set(false);
    }
    cfgLoadErrorModal.set(false);
    cfgLoadErrorMsg.set('');
    cfgRepairFailure.set('');
    firstRun.set(true);
  }

  // ── Remote connection (#240, #247) ──
  async function onRemoteState(s: RemoteState) {
    const previous = get(remoteState);
    const wasConnected = previous?.state === 'connected';
    // A new connection clears what the disconnection left behind.
    if (clearsOnReconnect(previous?.state, s.state)) reloadError.set('');
    remoteState.set(s);
    // The launchers learn from the remote state which editors open the
    // host's folders (#243).
    stores.remoteHost.set(hostFromState(s));
    if (s.state !== 'connected') {
      // The window keeps what it shows until it knows whether the next
      // connection resumes the host's daemon (#247): the events it missed
      // then arrive through the same router, before the connected state.
      if (wasConnected) noteDisconnect();
      if (wasConnected && currentStream) {
        statusPass++;
        retireStream();
      }
      if (s.state === 'disconnected' || s.state === 'failed') markStartup('finished');
      return;
    }
    if (wasConnected || get(remoteLoading)) return;
    remoteLoading.set(true);
    // A connection that resumed the host's daemon with every event the
    // window missed keeps the view, the rows and the work in flight; the
    // window reads the config and the status again, and starts no fetch
    // and no reconcile (#247). A window still in onboarding loads again:
    // the host may have saved its config before the cut (#349).
    if (get(remoteLoaded) && s.resumed && !get(firstRun)) {
      try {
        await reloadFromDisk();
        const refreshed = nextStatus(30_000);
        await bridge.refreshStatus();
        await refreshed;
      } catch (e) {
        console.error(e);
      } finally {
        remoteLoading.set(false);
      }
      return;
    }
    // Any other connection, the first one and a reconnect that could not
    // resume, loads the host's data again; a reconnect keeps the view.
    if (get(remoteLoaded)) {
      events.reset();
      forgetOperations();
      // Onboarding waits behind the card until the host says whether it
      // still has no config.
      if (get(firstRun)) remoteLoaded.set(false);
    }
    try {
      stores.remoteWindow.set(true);
      stores.remoteTextHost.set(s.alias);
      host.markRemote(s.alias);
      // What the host did meanwhile is not all known: its rows come back
      // from the host's status.
      forgetRuns();
      repoStates.set({});
      await startMain();
      // A host without a config shows the onboarding, which saves it there.
      remoteLoaded.set(true);
      if (!get(firstRun)) {
        // What the host's clones are now: a clone the cut stopped shows as
        // missing or incomplete before any action is offered.
        await runReconcile();
        const refreshed = nextStatus(30_000);
        await bridge.refreshStatus();
        await refreshed;
      }
    } catch (e) {
      console.error(e);
    } finally {
      remoteLoading.set(false);
    }
  }

  // ── Start, stop, reset ──
  // start loads the session: at once in a local window; in a remote one,
  // on each connection, following the connection state from here on.
  async function start(remoteWindow: boolean) {
    remote = remoteWindow;
    if (!remote) {
      await startMain();
      return;
    }
    onEvent('remote:state', (st) => { void onRemoteState(st as RemoteState); });
    try { await onRemoteState(await bridge.remoteInfo()); } catch (e) { console.error(e); }
  }

  // reset leaves onboarding and loads the dashboard from a fresh snapshot.
  async function reset() {
    firstRun.set(false);
    await initDashboard();
  }

  // stop ends the session: no listener, no pending status pass or reload.
  function stop() {
    stopped = true;
    for (const off of subscriptions.splice(0)) off();
    clearTimeout(fetchProgressTimer);
    reloadGeneration++;
    statusPass++;
    retireStream();
  }

  return {
    /** The session's own store bundle. */
    stores,
    syncing: readonly(syncing),
    fetchRun: readonly(fetchRun),
    pullRun: readonly(pullRun),
    fetchingAll: readonly(fetchingAll),
    fetchingRepos,
    lastFetchTime: readonly(lastFetchTime),
    networkDown: readonly(networkDown),
    credStatuses,
    mirrorChecking,
    mirrorSettingUp,
    mirrorSetupResultModal,
    mirrorDiscoverLoading,
    mirrorDiscoverResults,
    mirrorDiscoverError,
    mirrorDiscoverProgress,
    updateInfo,
    updateApplying,
    updateProgress: readonly(updateProgress),
    updateDone: readonly(updateDone),
    updateDismissed,
    rollbackVersion,
    remoteTint: readonly(remoteTint),
    firstRun: readonly(firstRun),
    cfgLoadErrorModal: readonly(cfgLoadErrorModal),
    cfgLoadErrorMsg: readonly(cfgLoadErrorMsg),
    cfgLoadErrorBusy: readonly(cfgLoadErrorBusy),
    cfgRepairFailure: readonly(cfgRepairFailure),
    cfgBackups: readonly(cfgBackups),
    configPath: readonly(configPath),
    serverOS: readonly(serverOS),
    reloadError,
    repoNesting: readonly(repoNesting),
    credentialFindings,
    orphanList: readonly(orphanList),
    ignoredClones: readonly(ignoredClones),
    skippedClones: readonly(skippedClones),
    skippedFolders: readonly(skippedFolders),
    skippedScans: readonly(skippedScans),
    orphanCount: readonly(orphanCount),
    workspaceChoices: readonly(workspaceChoices),
    remoteState: readonly(remoteState),
    remoteLoaded: readonly(remoteLoaded),
    remoteLoading: readonly(remoteLoading),
    /** Bumped by every status result the session applies, for reloads that race them. */
    get statusGeneration() { return statusGeneration; },
    /** True once the startup status pass has every row. */
    statusComplete: () => !!currentStream?.full,
    start,
    stop,
    reset,
    markStartup,
    reloadFromDisk,
    reloadConfigFromDisk,
    runReconcile,
    applyReconcile,
    restoreBackup,
    repairConfig,
    startFresh,
    listen,
    loadRemoteTint,
    watchProfiles,
    runFetchAll,
    executePullAll,
    failPullAll,
    markSyncStart,
    markCloneStart,
    checkAllMirrorStatus,
    verifyAllCredentials,
    nextStatus,
    noteDisconnect,
    forgetOperations,
    forgetRuns,
  };
}
