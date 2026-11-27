// SessionController (#353, #379): the event subscriptions of one window's
// session and the run bookkeeping they update (status passes, Fetch all,
// Pull all, clones, mirrors, credentials, reconcile, updates). App.svelte
// creates one, renders from its stores and hands it the lifecycle it still
// owns through SessionHost.
import { tick } from 'svelte';
import { get, readonly, writable } from 'svelte/store';
import { accounts, applyMirrorStatusResults, applyPRUpdate, applyStatusResults, configStore, mirrors, repoStates, sources, workspaceUnknown } from '../stores';
import { acceptStatusBatch, applyRepairReports } from '../status';
import type { statusPaintQueue } from '../status';
import { completionStatuses, hasConfiguredRepo } from '../events';
import type { createEventRouter } from '../events';
import type { bridge as appBridge, RemoteState } from '../bridge';
import type { EventMetadata, EventPayloads } from '../dto.generated';
import { startFetch, startPull, noteDone, noteCompletions, summarizeFetch, summarizePull, localWorkCount } from '../activity';
import type { Run, Completion, FetchSummary, Unsafe } from '../activity';
import { pushNotice, dismissNotice, errorText } from '../notices';
import { t } from '../i18n';
import type { Vars } from '../i18n';
import { tintOf } from '../tint';
import type { CredentialStatus, MirrorSetupResult, PullSkipReason, RepoState, UpdateInfo } from '../types';

export type EventRouter = ReturnType<typeof createEventRouter>;

/** The bridge calls a session makes on its own. */
export type SessionBridge = Pick<typeof appBridge,
  'getAllStatus' | 'getConfig' | 'reloadConfig' | 'runTask' | 'pullAll' | 'credentialVerify' | 'getRemoteTint'>;

/** The status pass the window's lifecycle started and status:batch fills. */
export interface StartupStream {
  id: number;
  first: () => void;
  finish: () => void;
  full: boolean;
  painted: boolean;
  pending: Set<string>;
  paint: ReturnType<typeof statusPaintQueue>;
}

/** What the session asks of the window: lifecycle and UI state that stay in App.svelte. */
export interface SessionHost {
  simple(): boolean;
  serverOS(): string;
  stream(): StartupStream | null;
  markStartup(stage: string): void;
  finishTiming(): void;
  restartDashboard(): Promise<void>;
  refreshDetail(): void;
  applyReconcile(res: EventPayloads['reconcile:done']): Promise<void>;
  discoverDone(data: EventPayloads['discover:done']): void;
  quit(): void;
}

export interface SessionDeps {
  bridge: SessionBridge;
  events: EventRouter;
  host: SessionHost;
}

export type SessionController = ReturnType<typeof createSessionController>;

export function createSessionController({ bridge, events, host }: SessionDeps) {
  const $t = (key: string, vars?: Vars) => get(t)(key, vars);
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

  const configuredRepo = (source: string, repo: string) => hasConfiguredRepo(get(configStore), source, repo);
  const statusOfKey = (key: string) => get(repoStates)[key]?.status || 'unknown';
  const accountOfKey = (key: string) => get(sources)[key.slice(0, key.indexOf('/'))]?.account || key.slice(0, key.indexOf('/'));
  const repoKey = ({ source, repo }: { source: string; repo: string }) => `${source}/${repo}`;

  function publishCompletionRow(row: EventPayloads['status:one']) {
    host.stream()?.paint.flush();
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
    const lanHint = host.serverOS() === 'darwin' && sum.unreachable.some((u) => u.fast) && $t('result.lanHint');
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
        void host.applyReconcile(res).catch(console.error);
      } else if (res?.workspaceUnknown) workspaceUnknown.set(new Set(res.workspaceUnknown));
    });

    onEvent('status:batch', async (batch) => {
      const stream = host.stream();
      if (!stream || stream.full || batch.passID !== stream.id) return;
      if (acceptStatusBatch(batch, stream.id, get(configStore)?.revision)) {
        if (batch.items?.length) {
          stream.paint.push(batch.items);
          if (!stream.painted) {
            stream.painted = true;
            await tick();
            if (stream !== host.stream() || batch.revision !== get(configStore)?.revision) return;
            host.markStartup('firstStatus');
            stream.first();
          }
        }
        if (batch.done) {
          stream.paint.flush();
          await tick();
          if (stream !== host.stream() || batch.revision !== get(configStore)?.revision) return;
          if (stream.pending.size) {
            // A remote transport may miss an event. Recover the authoritative sweep.
            const rows = await bridge.getAllStatus();
            if (stream !== host.stream() || batch.revision !== get(configStore)?.revision) return;
            applyStatusResults(rows, true);
            for (const row of rows) stream.pending.delete(`${row.source}/${row.repo}`);
            if (rows.length) host.markStartup('firstStatus');
            await tick();
          }
          if (!stream.pending.size) host.markStartup('fullStatus');
          stream.full = true;
          stream.first();
          host.finishTiming();
          stream.finish();
        }
      } else if (batch.done) {
        // A config change superseded this pass. A fresh snapshot owns the next one.
        void host.restartDashboard().catch(console.error);
      }
    }, batch => {
      const stream = host.stream();
      if (batch.done && stream && batch.passID === stream.id && !stream.full) {
        void host.restartDashboard().catch(console.error);
      }
    });

    onEvent('status:updated', (results) => {
      host.stream()?.paint.flush();
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

  // watchRemoteState follows the connection of a remote window.
  function watchRemoteState(changed: (state: RemoteState) => void) {
    onEvent('remote:state', (st) => { changed(st as RemoteState); });
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

  function destroy() {
    for (const off of subscriptions.splice(0)) off();
    clearTimeout(fetchProgressTimer);
  }

  return {
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
    /** Bumped by every status result the session applies, for reloads that race them. */
    get statusGeneration() { return statusGeneration; },
    listen,
    loadRemoteTint,
    watchProfiles,
    watchRemoteState,
    destroy,
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
