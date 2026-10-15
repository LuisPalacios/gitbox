// Long-running operations (Fetch all, Pull all) as data: what a run covers,
// how far it got, and the summary its result card shows (issue #138).
//
// Kept free of runtime imports so lib/activity.test.mjs can load it on its
// own: statuses and accounts come in as functions.

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
  // Fetch: each key's status when the run started, to tell what changed.
  before: Record<string, string>;
  // Pull: keys that were not cloned yet, so the run clones them.
  clones: Set<string>;
}

function start(kind: Run['kind'], keys: string[], accountOf: (key: string) => string, statusOf: (key: string) => string): Run {
  const scope = [...new Set(keys)];
  const before: Record<string, string> = {};
  for (const key of scope) before[key] = statusOf(key);
  return {
    kind, keys: scope,
    accounts: new Set(scope.map(accountOf)).size,
    done: new Set(), errors: {}, before,
    clones: new Set(kind === 'pull' ? scope.filter((key) => before[key] === 'not cloned') : []),
  };
}

export function startFetch(keys: string[], accountOf: (key: string) => string, statusOf: (key: string) => string): Run {
  return start('fetch', keys, accountOf, statusOf);
}

export function startPull(keys: string[], accountOf: (key: string) => string, statusOf: (key: string) => string): Run {
  return start('pull', keys, accountOf, statusOf);
}

// noteDone records one repo reporting back, with its error if it failed.
// Keys outside the run are ignored. It returns a new Run, so a Svelte
// assignment picks up the change.
export function noteDone(run: Run, key: string, error?: string): Run {
  if (!run.keys.includes(key)) return run;
  const done = new Set(run.done);
  done.add(key);
  const errors = { ...run.errors };
  if (error) errors[key] = error;
  else delete errors[key];
  return { ...run, done, errors };
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
  // Repos that failed for any other reason.
  failed: number;
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
  let checked = 0, nowBehind = 0, skipped = 0, failed = 0;
  for (const key of run.done) {
    if (run.errors[key]) {
      const account = accountOf(key);
      if (offline.has(account)) { skipped++; skippedAccounts.add(account); } else failed++;
      continue;
    }
    checked++;
    const now = statusOf(key);
    if ((now === 'behind' || now === 'diverged') && run.before[key] !== now) nowBehind++;
  }
  return {
    checked, nowBehind, skipped, skippedAccounts: [...skippedAccounts].sort(), failed,
    localWork: localWorkCount(run.keys, statusOf),
    changed: nowBehind > 0 || skipped > 0 || failed > 0,
  };
}

export interface PullSummary {
  pulled: number;
  cloned: number;
  failed: number;
  // The failed keys and their errors, in run order.
  failures: Array<{ key: string; error: string }>;
}

export function summarizePull(run: Run): PullSummary {
  let pulled = 0, cloned = 0;
  const failures: PullSummary['failures'] = [];
  for (const key of run.keys) {
    if (!run.done.has(key)) continue;
    if (run.errors[key]) failures.push({ key, error: run.errors[key] });
    else if (run.clones.has(key)) cloned++;
    else pulled++;
  }
  return { pulled, cloned, failed: failures.length, failures };
}
