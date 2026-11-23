// Long-running operations (Fetch all, Pull all) as data: what a run covers,
// how far it got, and the summary its result card shows (issue #138).
//
// Kept free of runtime imports so lib/activity.test.mjs can load it on its
// own: statuses and accounts come in as functions.

import type { PullSkipReason } from './types';

export interface Run {
  kind: 'fetch' | 'pull';
  // Repo keys ("source/repo") the run covers, without duplicates.
  keys: string[];
  // Distinct accounts among those keys, for "across N accounts".
  accounts: number;
  // Keys that reported back. A Set, so a repeated event never counts twice.
  done: Set<string>;
  // Error message per key that failed.
  errors: Record<string, string>;
  // What the backend says about each failure, as data: see Failure.
  failures: Record<string, Failure>;
  // Pull: reasons the backend skipped clones that have local work or changed settings.
  skips: Record<string, PullSkipReason>;
  // Fetch: each key's status when the run started, to tell what changed.
  before: Record<string, string>;
  // Pull: keys that were not cloned yet, so the run clones them.
  clones: Set<string>;
}

// Failure is what the backend reports about a failed repo besides its text.
// Summaries group by these facts and never read the message, which is made
// to be safe to show, not to be parsed.
export interface Failure {
  // Class of the error, such as "git.offline". Absent for an untyped error.
  code?: string;
  // The server that could not be reached, when the backend knows it.
  host?: string;
  // The connect was refused within a second instead of timing out.
  fast?: boolean;
}

function start(kind: Run['kind'], keys: string[], accountOf: (key: string) => string, statusOf: (key: string) => string): Run {
  const scope = [...new Set(keys)];
  const before: Record<string, string> = {};
  for (const key of scope) before[key] = statusOf(key);
  return {
    kind, keys: scope,
    accounts: new Set(scope.map(accountOf)).size,
    done: new Set(), errors: {}, failures: {}, skips: {}, before,
    clones: new Set(kind === 'pull' ? scope.filter((key) => before[key] === 'not cloned') : []),
  };
}

export function startFetch(keys: string[], accountOf: (key: string) => string, statusOf: (key: string) => string): Run {
  return start('fetch', keys, accountOf, statusOf);
}

export function startPull(keys: string[], accountOf: (key: string) => string, statusOf: (key: string) => string): Run {
  return start('pull', keys, accountOf, statusOf);
}

// noteDone records one repo reporting back, with its error if it failed and
// what the backend says about the failure. Keys outside the run are ignored.
// It returns a new Run, so a Svelte assignment picks up the change.
export function noteDone(run: Run, key: string, error?: string, failure?: Failure, skip?: PullSkipReason): Run {
  return noteCompletions(run, [{ key, error, failure, skip }]);
}

export interface Completion {
  key: string;
  error?: string;
  failure?: Failure;
  skip?: PullSkipReason;
}

// Several fetch completions may arrive before the next progress paint.
export function noteCompletions(run: Run, events: Completion[]): Run {
  const next = { ...run, done: new Set(run.done), errors: { ...run.errors }, failures: { ...run.failures }, skips: { ...run.skips } };
  for (const { key, error, failure, skip } of events) {
    if (!Object.hasOwn(run.before, key)) continue;
    next.done.add(key);
    if (error) { next.errors[key] = error; next.failures[key] = failure || {}; }
    else { delete next.errors[key]; delete next.failures[key]; }
    if (skip) next.skips[key] = skip;
    else delete next.skips[key];
  }
  return next;
}

export function progress(run: Run): { done: number; total: number } {
  return { done: run.done.size, total: run.keys.length };
}

// Repos holding local work that no fetch or fast-forward pull touches:
// the reassurance line of both result cards.
const LOCAL_WORK = new Set(['dirty', 'ahead', 'diverged', 'conflict']);
export function localWorkCount(keys: string[], statusOf: (key: string) => string): number {
  return keys.filter((key) => LOCAL_WORK.has(statusOf(key))).length;
}

export interface FetchSummary {
  // Repos fetched without an error.
  checked: number;
  // Repos that became behind (or diverged) during the run.
  nowBehind: number;
  // Repos that failed because their account is offline, and those accounts.
  skipped: number;
  skippedAccounts: string[];
  // Servers that couldn't be reached at all, grouped by host, busiest first.
  unreachable: Unreachable[];
  // Repos that failed for any other reason, and the first of them.
  failed: number;
  firstFailure: { key: string; error: string; code?: string } | null;
  // Repos with local work, all left untouched.
  localWork: number;
  // Something worth a card on a background (periodic or startup) fetch.
  changed: boolean;
}

// summarizeFetch reads a finished fetch: the status diff gives "now
// behind", and an error on an account whose credential is offline counts
// as skipped rather than failed.
export function summarizeFetch(run: Run, statusOf: (key: string) => string, accountOf: (key: string) => string, offlineAccounts: string[]): FetchSummary {
  const offline = new Set(offlineAccounts);
  const skippedAccounts = new Set<string>();
  const hosts = new Map<string, Unreachable>();
  let checked = 0, nowBehind = 0, skipped = 0, failed = 0;
  let firstFailure: FetchSummary['firstFailure'] = null;
  for (const key of run.done) {
    const error = run.errors[key];
    if (error) {
      const account = accountOf(key);
      const failure = run.failures[key] || {};
      if (offline.has(account)) { skipped++; skippedAccounts.add(account); }
      else if (UNREACHABLE.has(failure.code || '')) {
        const host = failure.host || '';
        const u = hosts.get(host) || { host, count: 0, fast: false };
        u.count++;
        u.fast = u.fast || !!failure.fast;
        hosts.set(host, u);
      } else {
        failed++;
        firstFailure = firstFailure || { key, error: cleanGitError(error), ...(failure.code ? { code: failure.code } : {}) };
      }
      continue;
    }
    checked++;
    const now = statusOf(key);
    if ((now === 'behind' || now === 'diverged') && run.before[key] !== now) nowBehind++;
  }
  const unreachable = [...hosts.values()].sort((x, y) => y.count - x.count || x.host.localeCompare(y.host));
  return {
    checked, nowBehind, skipped, skippedAccounts: [...skippedAccounts].sort(), unreachable, failed, firstFailure,
    localWork: localWorkCount(run.keys, statusOf),
    changed: nowBehind > 0 || skipped > 0 || unreachable.length > 0 || failed > 0,
  };
}

export interface Unreachable {
  // The server's host name, or '' when the backend doesn't know it.
  host: string;
  count: number;
  // At least one connect was refused within a second instead of timing
  // out: the OS blocked it (on macOS often the Local Network permission).
  fast: boolean;
}

// Connection-level failures: the server couldn't be reached at all, as
// opposed to git failing against a server it did reach.
const UNREACHABLE = new Set(['git.offline', 'git.timeout']);

// cleanGitError drops a leading "fatal:" or "error:" so a card shows the
// reason itself.
export function cleanGitError(error: string): string {
  return error.replace(/^(fatal|error):\s*/i, '').trim();
}

export interface PullSummary {
  pulled: number;
  cloned: number;
  skipped: number;
  failed: number;
  // The failed keys and their errors, in run order.
  failures: Array<{ key: string; error: string }>;
}

export function summarizePull(run: Run): PullSummary {
  let pulled = 0, cloned = 0, skipped = 0;
  const failures: PullSummary['failures'] = [];
  for (const key of run.keys) {
    if (!run.done.has(key)) continue;
    if (run.errors[key]) failures.push({ key, error: run.errors[key] });
    else if (run.skips[key]) skipped++;
    else if (run.clones.has(key)) cloned++;
    else pulled++;
  }
  return { pulled, cloned, skipped, failed: failures.length, failures };
}
