// The store bundle of one session (#353, #381): the config and what derives
// from it, the repository states and their summaries, mirrors, pull
// requests, workspaces, the remote state and the notices. Each session
// controller writes to its own bundle; the shell reads the active one
// through the proxies in lib/stores.ts.
import { derived, get, writable } from 'svelte/store';
import type { RemoteHost } from '../remotehost';
import { isBlocked } from '../readonly';
import type { CallName } from '../bridge';
import type { Placeholder } from '../repo-tree';
import { CALM_RANK, derivePresentation } from '../presentation';
import { createNotices } from '../notice-list';
import type { ConfigDTO, Account, SourceDTO, MirrorDTO, MirrorStatusResult, RepoState, StatusResult, PRSummaryDTO, PRAccountUpdateDTO, WorkspaceDiagnosticDTO } from '../types';

export type StoreBundle = ReturnType<typeof createStoreBundle>;

export function createStoreBundle() {
  // ── Config, loaded from the session's backend ──
  const configStore = writable<ConfigDTO | null>(null);
  const accounts = derived(configStore, ($cfg) => $cfg ? $cfg.accounts : {} as Record<string, Account>);
  const sources = derived(configStore, ($cfg) => $cfg ? $cfg.sources : {} as Record<string, SourceDTO>);

  // ── Per-repo state (status + progress) ──
  // Key format: "sourceKey/repoKey"
  const repoStates = writable<Record<string, RepoState>>({});

  // applyStatusResults applies status results from the backend.
  function applyStatusResults(results: StatusResult[], partial = false, completion = false) {
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
  const summary = derived(repoStates, ($rs) => {
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

  // ── Per-account stats ──
  const accountStats = derived([sources, repoStates], ([$src, $rs]) => {
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

  // ── Mirrors ──
  const mirrors = derived(configStore, ($cfg) => $cfg?.mirrors ?? {} as Record<string, MirrorDTO>);
  // Per-mirror-repo live status (populated by mirror:status events).
  // Key format: "mirrorKey/repoKey"
  const mirrorStates = writable<Record<string, MirrorStatusResult>>({});

  function applyMirrorStatusResults(results: MirrorStatusResult[]) {
    mirrorStates.update((states) => {
      const next = { ...states };
      for (const r of results) {
        next[`${r.mirrorKey}/${r.repoKey}`] = r;
      }
      return next;
    });
  }

  // Mirror summary counts (from live status results).
  const mirrorSummary = derived([mirrors, mirrorStates], ([$m, $ms]) => {
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
  const prsByAccount = writable<Record<string, Record<string, PRSummaryDTO>>>({});
  // PR feature settings mirrored from the Go side.
  const prSettings = writable<{ enabled: boolean; includeDrafts: boolean }>({ enabled: true, includeDrafts: true });

  // applyPRUpdate merges one account's worth of PR data. A failed refresh
  // (token lookup timing out, network down after sleep, rate limit) keeps
  // the account's last good data instead of wiping its pills (#148).
  function applyPRUpdate(upd: PRAccountUpdateDTO) {
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

  // ── Workspace placeholders ──
  // Folders each container's .code-workspace lists that are not cloned yet,
  // keyed by the container's "source/repo"; refreshed by every reconcile pass.
  const workspacePlaceholders = writable<Record<string, Placeholder[]>>({});
  // The clones ("source/repo") a container's workspace lists inside it.
  const workspaceMembers = writable<Set<string>>(new Set());
  // Containers whose last workspace scan could not read the clone root.
  const workspaceUnknown = writable<Set<string>>(new Set());
  const workspaceDiagnostics = writable<Record<string, WorkspaceDiagnosticDTO>>({});

  // ── Remote state ──
  // remoteWindow is set in a remote session once it connects (#240). Only
  // the calls its server does not serve are refused there (#294): blocked
  // says which, the controls that end in one show dimmed (lib/ro.ts) and
  // the bridge refuses them (lib/readonly.ts).
  const remoteWindow = writable(false);
  const blocked = derived(remoteWindow, ($remote) => (name: CallName) => isBlocked(name, $remote));
  // remoteHost is the host of a remote session, for its launchers (#243):
  // the editors that open its folders. null in a local session.
  const remoteHost = writable<RemoteHost>(null);
  // remoteTextHost is the alias of the host a remote session shows (#295),
  // null in a local one. Its texts read the "<key>.remote" variants with
  // {host} filled in (lib/i18n.ts, createTexts).
  const remoteTextHost = writable<string | null>(null);

  // ── Notices ──
  const { notices, pushNotice, dismissNotice } = createNotices();

  return {
    configStore,
    accounts,
    sources,
    repoStates,
    summary,
    accountStats,
    mirrors,
    mirrorStates,
    mirrorSummary,
    prsByAccount,
    prSettings,
    workspacePlaceholders,
    workspaceMembers,
    workspaceUnknown,
    workspaceDiagnostics,
    remoteWindow,
    blocked,
    remoteHost,
    remoteTextHost,
    notices,
    applyStatusResults,
    applyMirrorStatusResults,
    applyPRUpdate,
    pushNotice,
    dismissNotice,
  };
}
