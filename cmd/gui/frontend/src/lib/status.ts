// Status presentation — the one place that decides how a repo status looks:
// its shape (symbol), colour, label, Simple kind, count badges and whether
// its row opens the detail panel. Every view that renders repo status (the
// compact row, the Advanced and Simple rows, the detail panel and the
// footer) reads from here instead of repeating the mapping.
//
// Kept free of runtime imports so lib/status.test.mjs can load it on its own.

// ── Colours (palettes matching prototype-final.svelte) ──

const darkPalette: Record<string, string> = {
  clean: '#61fd5f', behind: '#D91C9A', dirty: '#F07623',
  ahead: '#4B95E9', syncing: '#4B95E9', cloning: '#4B95E9', fetching: '#4B95E9',
  'not cloned': '#71717a', 'no upstream': '#71717a',
  diverged: '#D81E5B', conflict: '#D81E5B', error: '#D81E5B',
  'upstream gone': '#D81E5B',
};

const lightPalette: Record<string, string> = {
  clean: '#166534', behind: '#a21caf', dirty: '#c2410c',
  ahead: '#2563eb', syncing: '#2563eb', cloning: '#2563eb', fetching: '#2563eb',
  'not cloned': '#52525b', 'no upstream': '#52525b',
  diverged: '#be123c', conflict: '#be123c', error: '#be123c',
  'upstream gone': '#be123c',
};

export function statusColor(status: string, theme: string): string {
  const palette = theme === 'light' ? lightPalette : darkPalette;
  return palette[status] || (theme === 'light' ? '#52525b' : '#71717a');
}

// ── Shape ──

export function statusSymbol(status: string): string {
  const map: Record<string, string> = {
    clean: '●', behind: '◗', dirty: '◆', ahead: '▲',
    syncing: '◔', cloning: '◔', fetching: '◔', 'not cloned': '○',
    'no upstream': '~', diverged: '⚠', conflict: '⚡', error: '✕',
    'upstream gone': '⊗',
    // 'unknown' = first scan hasn't completed yet. Render the same shape as
    // 'clean' but in the muted grey returned by statusColor() so the row
    // says "I don't know yet" instead of lying with a red ✕.
    unknown: '●',
  };
  // Hasownproperty test rather than `||` so a deliberately-empty mapping
  // value would be respected (none today, but defensive against future use).
  return Object.prototype.hasOwnProperty.call(map, status) ? map[status] : '?';
}

// A row still waiting for its first scan, or fetching right now, shows a
// spinning ↻ instead of its status shape.
export function rowBusy(status: string, fetching: boolean): boolean {
  return status === 'unknown' || fetching;
}

export function rowGlyph(status: string, fetching: boolean): string {
  return rowBusy(status, fetching) ? '↻' : statusSymbol(status);
}

// ── Simple mode (issue #124) ──
// Simple mode folds the git statuses into the one question a
// non-developer asks: is my work safe? Each kind maps to one sentence and
// one action in the repo row. Local work splits in two: work in progress
// (changed files) and saved here but not uploaded (commits).
export type SimpleKind = 'saved' | 'wip' | 'unpushed' | 'update' | 'online' | 'help' | 'checking' | 'busy';

const simpleKinds: Record<string, SimpleKind> = {
  clean: 'saved',
  dirty: 'wip', ahead: 'unpushed', 'no upstream': 'unpushed',
  behind: 'update',
  'not cloned': 'online',
  diverged: 'help', conflict: 'help', 'upstream gone': 'help', error: 'help',
  unknown: 'checking',
  syncing: 'busy', cloning: 'busy', fetching: 'busy',
};

export function simpleKind(status: string): SimpleKind {
  return simpleKinds[status] || 'help';
}

// Colors reuse the Advanced palette entry that means the same thing.
const simpleColorKeys: Record<SimpleKind, string> = {
  saved: 'clean', wip: 'dirty', unpushed: 'ahead', update: 'behind', online: 'not cloned',
  help: 'error', checking: 'not cloned', busy: 'syncing',
};

export function simpleColor(kind: SimpleKind, theme: string): string {
  return statusColor(simpleColorKeys[kind], theme);
}

// i18n key of the one-word Simple label; a busy row reads "checking".
export function simpleLabelKey(kind: SimpleKind): string {
  return `simple.${kind === 'busy' ? 'checking' : kind}`;
}

// Simple kinds that count as "not saved yet" in the Simple summaries.
export function simpleUnsaved(status: string): boolean {
  return ['wip', 'unpushed', 'help'].includes(simpleKind(status));
}

// ── Attention ──
// How urgently a status needs me, worst first: 0 needs help, 1 local work,
// 2 behind, 3 not cloned, 4 nothing to do. A row being pulled or cloned
// keeps the rank it had, so it doesn't jump while it runs. A feature branch
// without an upstream is fine, as in isSynced.
export const ATTENTION_GROUPS = ['help', 'local', 'behind', 'notCloned'] as const;
export type AttentionGroup = typeof ATTENTION_GROUPS[number];
export const CALM_RANK = ATTENTION_GROUPS.length;

const attentionRanks: Record<string, number> = {
  diverged: 0, conflict: 0, 'upstream gone': 0, error: 0,
  dirty: 1, ahead: 1, 'no upstream': 1,
  behind: 2, syncing: 2,
  'not cloned': 3, cloning: 3,
};

export function attentionRank(status: string, isDefault?: boolean): number {
  if (status === 'no upstream' && isDefault === false) return CALM_RANK;
  return attentionRanks[status] ?? CALM_RANK;
}

// ── Glossary labels (issue #136) ──
// The words a row shows for its status: i18n key and variables, an
// optional dimmed note after it, an optional short form that replaces
// both in a narrow row (#148), the palette entry that colours it, and
// whether the Advanced count badges follow (only where the words don't
// carry the counts).
export interface LabelState { status: string; ahead?: number; behind?: number; modified?: number; untracked?: number; isDefault?: boolean }
export interface StatusLabel { key: string; vars?: Record<string, number>; note?: string; noteVars?: Record<string, number>; short?: string; shortVars?: Record<string, number>; color: string; badges: boolean }

export function statusLabel(st: LabelState, simple: boolean): StatusLabel {
  const changes = (st.modified || 0) + (st.untracked || 0);
  if (simple) {
    const kind = simpleKind(st.status);
    const color = simpleColorKeys[kind];
    if (kind === 'wip') {
      const one = changes === 1;
      return { key: 'simple.wip', note: one ? 'simple.wipSafeOne' : 'simple.wipSafe', noteVars: { n: changes },
        short: one ? 'simple.wipShortOne' : 'simple.wipShort', shortVars: { n: changes }, color, badges: false };
    }
    return { key: simpleLabelKey(kind), color, badges: false };
  }
  switch (st.status) {
    case 'clean': return { key: 'status.upToDate', color: 'clean', badges: false };
    case 'behind': return { key: 'status.behind', vars: { n: st.behind || 0 }, color: 'behind', badges: false };
    case 'ahead': return { key: 'status.ahead', vars: { n: st.ahead || 0 }, color: 'ahead', badges: false };
    case 'dirty': return { key: changes === 1 ? 'status.localChange' : 'status.localChanges', vars: { n: changes }, note: changes === 1 ? 'status.protectedOne' : 'status.protected', color: 'dirty', badges: false };
    case 'diverged': return { key: 'status.diverged', color: 'diverged', badges: true };
    case 'conflict': return { key: 'status.conflict', color: 'conflict', badges: true };
    case 'upstream gone': return { key: 'status.upstreamGone', color: 'upstream gone', badges: false };
    case 'error': return { key: 'status.error', color: 'error', badges: false };
    case 'not cloned': return { key: 'status.notCloned', color: 'not cloned', badges: false };
    case 'no upstream': return st.isDefault ? { key: 'repo.noUpstream', color: 'no upstream', badges: false } : { key: 'repo.localBranch', color: 'clean', badges: false };
    default: return { key: 'simple.checking', color: 'not cloned', badges: false };
  }
}

// ── Counts ──
// A count badge: the RepoState field it reads, its glyph, the palette entry
// that colours it and the i18n key of the word that follows the number
// ("3 behind").
export interface CountBadge { field: 'behind' | 'ahead' | 'modified' | 'untracked'; glyph: string; color: string; word: string }

export const COUNT_BADGES: CountBadge[] = [
  { field: 'behind', glyph: '↓', color: 'behind', word: 'count.behind' },
  { field: 'ahead', glyph: '↑', color: 'ahead', word: 'count.ahead' },
  { field: 'modified', glyph: '✎', color: 'dirty', word: 'count.changed' },
  { field: 'untracked', glyph: '?', color: 'not cloned', word: 'count.untracked' },
];

export function countBadge(field: CountBadge['field']): CountBadge {
  return COUNT_BADGES.find((b) => b.field === field) as CountBadge;
}

// The single count the compact row shows for a status, if any.
export function statusCountBadge(status: string): CountBadge | null {
  if (status === 'behind') return countBadge('behind');
  if (status === 'dirty') return countBadge('modified');
  if (status === 'ahead') return countBadge('ahead');
  return null;
}

// ── Detail panel ──
// Statuses with nothing to inspect: an open detail panel folds when its
// row reaches one of them, and clicking such a row does nothing.
const noDetailStatuses = new Set(['clean', 'behind', 'not cloned', 'cloning', 'syncing']);

export function detailLocked(status: string): boolean {
  return noDetailStatuses.has(status);
}

// Whether clicking the row opens its detail panel ('unknown' waits for
// the first scan).
export function opensDetail(status: string): boolean {
  return status !== 'unknown' && !detailLocked(status);
}

// ── Credential status rules ──
// Shared by the account card, the account kebab and the cards panel alert.
export function credBroken(status: string | undefined): boolean {
  return status === 'none' || status === 'error';
}
export function canDiscoverWith(status: string | undefined): boolean {
  return !!status && status !== 'none' && status !== 'error' && status !== 'offline' && status !== 'unknown';
}
export function canCreateWith(status: string | undefined): boolean {
  return status === 'ok';
}
// i18n keys of why Find and + New are disabled.
export const DISCOVER_DISABLED = 'account.findDisabled';
export const CREATE_DISABLED = 'account.createDisabled';

// ── Account health ──
// What the account card and the sidebar say about a credential: a mark and
// its palette entry, the words (Simple or Advanced), the fix action and its
// label, and whether the account belongs in Needs attention.
//   retry     re-check the connection (offline)
//   reconnect Simple's credential flow (broken)
//   change    Advanced's credential modal (warning or failure)
//   setup     Advanced's credential modal when there is none
//   replace   Advanced's credential modal for a token about to expire
//   renew     Simple's credential flow for a token about to expire
export type HealthFix = 'retry' | 'reconnect' | 'change' | 'setup' | 'replace' | 'renew' | null;
export interface AccountHealth { level: 'ok' | 'warn' | 'bad' | 'none' | 'pending'; mark: string; color: string; key: string; vars: Record<string, number>; fix: HealthFix; fixKey: string; attention: boolean }

const DAY = 24 * 60 * 60 * 1000;
// A working token this close to its expiry turns the health line into a warning.
export const EXPIRY_WARN_DAYS = 14;

// Whole days until expiresAt (RFC 3339); null when unknown or unparsable.
export function daysToExpiry(expiresAt: string | undefined, now: number = Date.now()): number | null {
  const at = expiresAt ? Date.parse(expiresAt) : NaN;
  return Number.isNaN(at) ? null : Math.max(0, Math.floor((at - now) / DAY));
}

export function accountHealth(status: string | undefined, simple: boolean, expiresAt?: string, now: number = Date.now()): AccountHealth {
  const h = (level: AccountHealth['level'], mark: string, color: string, word: string, fix: HealthFix): AccountHealth => ({
    level, mark, color, fix, vars: {},
    key: `health.${word}${simple ? 'Simple' : ''}`,
    fixKey: fix ? `health.fix.${fix}${simple && fix === 'retry' ? 'Simple' : ''}` : '',
    attention: level === 'bad' || level === 'none',
  });
  const days = status === 'ok' ? daysToExpiry(expiresAt, now) : null;
  if (days !== null && days <= EXPIRY_WARN_DAYS) {
    const fix = simple ? 'renew' : 'replace';
    const key = days === 0 ? 'health.expiresToday' : days === 1 ? 'health.expiresInOne' : 'health.expiresIn';
    return { level: 'warn', mark: '◆', color: 'dirty', key, vars: { n: days }, fix, fixKey: `health.fix.${fix}`, attention: false };
  }
  switch (status) {
    case 'ok': return h('ok', '●', 'clean', 'ok', null);
    case 'warning': return h('warn', '◆', 'dirty', 'warning', simple ? null : 'change');
    case 'offline': return h('bad', '✕', 'error', 'offline', 'retry');
    case 'error': return h('bad', '✕', 'error', 'error', simple ? 'reconnect' : 'change');
    case 'none': return h('none', '○', 'ahead', 'none', simple ? 'reconnect' : 'setup');
    default: return h('pending', '◔', 'not cloned', 'checking', null);
  }
}

// Suffix of the account card's cred-badge-* class for a credential status.
export function credBadgeClass(status: string): string {
  return status === 'ok' ? 'ok' : status === 'error' ? 'err' : status === 'warning' ? 'warn' : status === 'offline' ? 'offline' : status === 'none' ? 'none' : status === 'unknown' ? 'pending' : '';
}

// Text of the account card's credential badge.
export function credBadgeLabel(status: string, credentialType: string | undefined): string {
  return status === 'unknown' ? '···' : status === 'none' ? 'config' : status === 'offline' ? 'offline' : credentialType || 'gcm';
}
