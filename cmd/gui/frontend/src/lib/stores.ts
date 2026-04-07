import { writable, derived } from 'svelte/store';
import type { ConfigDTO, Account, SourceDTO, MirrorDTO, MirrorStatusResult, RepoState, StatusResult } from './types';

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

// ── Per-repo state (status + progress) ──
// Key format: "sourceKey/repoKey"
export const repoStates = writable<Record<string, RepoState>>({});

// Apply status results from Go backend into repoStates store
export function applyStatusResults(results: StatusResult[]) {
  repoStates.update((states) => {
    const next: Record<string, RepoState> = {};
    for (const r of results) {
      const key = `${r.source}/${r.repo}`;
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
        default: status = 'error';
      }
      // Preserve in-flight progress if repo is syncing/cloning
      const existing = states[key];
      if (existing && (existing.status === 'syncing' || existing.status === 'cloning')) {
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
        };
      }
    }
    return next;
  });
}

// ── Derived: global summary ──
export const summary = derived(repoStates, ($rs) => {
  const vals = Object.values($rs);
  return {
    clean: vals.filter((r) => r.status === 'clean').length,
    behind: vals.filter((r) => r.status === 'behind').length,
    dirty: vals.filter((r) => r.status === 'dirty').length,
    ahead: vals.filter((r) => r.status === 'ahead').length,
    syncing: vals.filter((r) => r.status === 'syncing' || r.status === 'cloning').length,
    notCloned: vals.filter((r) => r.status === 'not cloned').length,
    error: vals.filter((r) => r.status === 'error').length,
    total: vals.length,
  };
});

// ── Derived: per-account stats ──
export const accountStats = derived([sources, repoStates], ([$src, $rs]) => {
  const stats: Record<string, { total: number; synced: number; issues: number }> = {};
  for (const [sourceKey, source] of Object.entries($src)) {
    const acctKey = source.account;
    if (!stats[acctKey]) stats[acctKey] = { total: 0, synced: 0, issues: 0 };
    const repoKeys = source.repoOrder && source.repoOrder.length > 0
      ? source.repoOrder
      : Object.keys(source.repos);
    for (const repoName of repoKeys) {
      const state = $rs[`${sourceKey}/${repoName}`];
      if (!state) continue;
      stats[acctKey].total++;
      if (state.status === 'clean') stats[acctKey].synced++;
      else stats[acctKey].issues++;
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

// ── Theme store ──
export const themeStore = writable<'light' | 'dark'>('dark');
