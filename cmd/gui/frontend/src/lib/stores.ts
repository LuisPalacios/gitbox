import type { RemoteHost } from './remotehost';
import { isBlocked } from './readonly';
import type { CallName } from './bridge';
import { writable, derived, readable, get } from 'svelte/store';
import type { Placeholder } from './repo-tree';
import { CALM_RANK, derivePresentation } from './presentation';
import type { ConfigDTO, Account, SourceDTO, MirrorDTO, MirrorStatusResult, RepoState, StatusResult, PRSummaryDTO, PRAccountUpdateDTO, WorkspaceDiagnosticDTO } from './types';

// ── Config store — loaded once from Go backend ──
export const configStore = writable<ConfigDTO | null>(null);

// ── Derived: accounts map ──
export const accounts = derived(configStore, ($cfg) =>
  $cfg ? $cfg.accounts : {} as Record<string, Account>
);

// ── Derived: sources map ──
export const sources = derived(configStore, ($cfg) =>
  $cfg ? $cfg.sources : {} as Record<string, SourceDTO>
);

// Keep relative status captions current with one clock for all rows.
export const statusClock = readable(Date.now(), (set) => {
  set(Date.now());
  const timer = setInterval(() => set(Date.now()), 30_000);
  return () => clearInterval(timer);
});

// ── Per-repo state (status + progress) ──
// Key format: "sourceKey/repoKey"
export const repoStates = writable<Record<string, RepoState>>({});

// Apply status results from Go backend into repoStates store
export function applyStatusResults(results: StatusResult[], partial = false, completion = false) {
  const revision = get(configStore)?.revision ?? 0;
  repoStates.update((states) => {
    const next: Record<string, RepoState> = partial ? { ...states } : {};
    for (const r of results) {
      const key = `${r.source}/${r.repo}`;
      if (partial && !states[key]) continue;
      // Map Go status.State strings to our frontend states
      let status: RepoState['status'];
      switch (r.state) {
        case 'clean': status = 'clean'; break;
        case 'dirty': status = 'dirty'; break;
        case 'behind': status = 'behind'; break;
        case 'ahead': status = 'ahead'; break;
        case 'diverged': status = 'diverged'; break;
        case 'conflict': status = 'conflict'; break;
        case 'not cloned': status = 'not cloned'; break;
        case 'no upstream': status = 'no upstream'; break;
        case 'upstream gone': status = 'upstream gone'; break;
        case 'root offline': status = 'root offline'; break;
        default: status = 'error';
      }
      // Preserve in-flight progress if repo is syncing/cloning
      const existing = states[key];
      if (existing && (existing.status === 'syncing' || existing.status === 'cloning' || (existing.status === 'pending' && !completion && (existing.statusCheckRevision ?? 0) >= revision))) {
        next[key] = existing;
      } else {
        next[key] = {
          status,
          path: r.path,
          progress: 0,
          behind: r.behind,
          modified: r.modified,
          untracked: r.untracked,
          ahead: r.ahead,
          error: r.error,
          problem: r.problem ?? (existing?.failedOperation === 'fetch' && existing.path === r.path && status !== 'not cloned' ? existing.problem : undefined),
          failedOperation: existing?.failedOperation === 'fetch' && existing.path === r.path && status !== 'not cloned' ? 'fetch' : undefined,
          pullSkipped: status === 'dirty' && existing?.pullSkipped === 'dirty' ? 'dirty' : undefined,
          branch: r.branch,
          isDefault: r.isDefault,
          checkedAt: r.checkedAt,
          repairWarnings: status !== 'not cloned' && (!existing?.path || existing.path === r.path) ? existing?.repairWarnings : undefined,
        };
      }
    }
    return next;
  });
}

// The ring fills for known saved clones. Rows still being classified count
// towards the total, and only actionable rows contribute to the badge.
export const summary = derived(repoStates, ($rs) => {
  const counts = { saved: 0, wip: 0, update: 0, unpushed: 0, online: 0, offline: 0, help: 0, busy: 0, checking: 0,
    attention: 0, ahead: 0, syncing: 0, total: 0 };
  for (const state of Object.values($rs)) {
    const row = derivePresentation(state);
    counts[row.kind]++;
    counts.total++;
    if (row.attention < CALM_RANK) counts.attention++;
    // The Advanced footer reports measured commits ahead and transfers in progress.
    if (state.status === 'ahead') counts.ahead++;
    if (state.status === 'syncing' || state.status === 'cloning') counts.syncing++;
  }
  return counts;
});

// ── Derived: per-account stats ──
export const accountStats = derived([sources, repoStates], ([$src, $rs]) => {
  const stats: Record<string, { total: number; synced: number; issues: number; checking: boolean }> = {};
  for (const [sourceKey, source] of Object.entries($src)) {
    const acctKey = source.account;
    if (!stats[acctKey]) stats[acctKey] = { total: 0, synced: 0, issues: 0, checking: false };
    const repoKeys = source.repoOrder && source.repoOrder.length > 0
      ? source.repoOrder
      : Object.keys(source.repos);
    for (const repoName of repoKeys) {
      const state = $rs[`${sourceKey}/${repoName}`];
      const row = derivePresentation(state);
      const account = stats[acctKey];
      account.total++;
      if (row.kind === 'saved') account.synced++;
      if (row.attention < CALM_RANK) account.issues++;
      if (row.stale) account.checking = true;
    }
  }
  return stats;
});

// ── Derived: mirrors map ──
export const mirrors = derived(configStore, ($cfg) =>
  $cfg?.mirrors ?? {} as Record<string, MirrorDTO>
);

// ── Per-mirror-repo live status (populated by mirror:status events) ──
// Key format: "mirrorKey/repoKey"
export const mirrorStates = writable<Record<string, MirrorStatusResult>>({});

export function applyMirrorStatusResults(results: MirrorStatusResult[]) {
  mirrorStates.update((states) => {
    const next = { ...states };
    for (const r of results) {
      next[`${r.mirrorKey}/${r.repoKey}`] = r;
    }
    return next;
  });
}

// ── Derived: mirror summary counts (from live status results) ──
export const mirrorSummary = derived([mirrors, mirrorStates], ([$m, $ms]) => {
  let active = 0, unchecked = 0, error = 0, total = 0;
  for (const [mirrorKey, mir] of Object.entries($m)) {
    for (const repoKey of Object.keys(mir.repos)) {
      total++;
      const live = $ms[`${mirrorKey}/${repoKey}`];
      if (!live) unchecked++;
      else if (live.error) error++;
      else active++;
    }
  }
  return { active, unchecked, error, total };
});

// ── PR indicators (issue #29) ──
// Keyed by accountKey → (repoFull lowercase → PRSummaryDTO).
// A nested map avoids collisions when two accounts share the same owner/repo
// path (rare but possible across providers) and makes per-account clearing cheap.
export const prsByAccount = writable<Record<string, Record<string, PRSummaryDTO>>>({});

// PR feature settings mirrored from the Go side.
export const prSettings = writable<{ enabled: boolean; includeDrafts: boolean }>(
  { enabled: true, includeDrafts: true }
);

// Merge one account's worth of PR data into the store. A failed refresh
// (token lookup timing out, network down after sleep, rate limit) keeps
// the account's last good data instead of wiping its pills (#148).
export function applyPRUpdate(upd: PRAccountUpdateDTO) {
  if (!upd || !upd.accountKey) return;
  if (upd.error) {
    console.warn(`PR refresh failed for ${upd.accountKey}: ${upd.error}`);
    return;
  }
  prsByAccount.update((cur) => {
    const next = { ...cur };
    next[upd.accountKey] = upd.byRepo ?? {};
    return next;
  });
}

// Lookup summary for one clone. Returns an empty summary when absent.
export function lookupPRSummary(
  byAccount: Record<string, Record<string, PRSummaryDTO>>,
  accountKey: string,
  repoKey: string
): PRSummaryDTO {
  const empty: PRSummaryDTO = { authored: [], reviewRequested: [] };
  const acct = byAccount[accountKey];
  if (!acct) return empty;
  return acct[repoKey.toLowerCase()] ?? empty;
}

// ── Workspace placeholders ──
// Folders each container's .code-workspace lists that are not cloned yet,
// keyed by the container's "source/repo"; refreshed by every reconcile pass.
export const workspacePlaceholders = writable<Record<string, Placeholder[]>>({});
// The clones ("source/repo") a container's workspace lists inside it.
export const workspaceMembers = writable<Set<string>>(new Set());
// Containers whose last workspace scan could not read the clone root.
export const workspaceUnknown = writable<Set<string>>(new Set());
export const workspaceDiagnostics = writable<Record<string, WorkspaceDiagnosticDTO>>({});

// ── Theme store ──
export const themeStore = writable<'light' | 'dark'>('dark');

// remoteWindow is set in a remote window once it connects (#240). Only the
// calls its server does not serve are refused there (#294): blocked says
// which, the controls that end in one show dimmed (lib/ro.ts) and the
// bridge refuses them (lib/readonly.ts).
export const remoteWindow = writable(false);
export const blocked = derived(remoteWindow, ($remote) => (name: CallName) => isBlocked(name, $remote));

// remoteHost is the host of a remote window, for its launchers (#243): the
// editors that open its folders. null outside a remote window. The top bar
// sets it from the remote state it shows (lib/remotehost.ts).
export const remoteHost = writable<RemoteHost>(null);
