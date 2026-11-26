import type { RepairDTO, RepoState, StatusResult } from './types';

// Adoption can report a clone before its first status arrives. Later status
// scans describe the working tree; only another repair can settle its warning.
export function applyRepairReports(states: Record<string, RepoState>, reports: RepairDTO[]): Record<string, RepoState> {
  const next = { ...states };
  for (const report of reports) {
    const key = `${report.source}/${report.repo}`;
    if (!next[key]?.repairWarnings && !report.warnings?.length) continue;
    const row = next[key] ?? { status: 'unknown', progress: 0, ahead: 0, behind: 0, modified: 0, untracked: 0 };
    next[key] = { ...row, repairWarnings: report.warnings?.length ? report.warnings : undefined };
  }
  return next;
}

// Shared palette, glyphs, count badges, account health and formatting.

// ── Colours (palettes matching prototype-final.svelte) ──

const darkPalette: Record<string, string> = {
  clean: '#61fd5f', behind: '#D91C9A', dirty: '#F07623',
  ahead: '#4B95E9', syncing: '#4B95E9', cloning: '#4B95E9', fetching: '#4B95E9',
  'not cloned': '#71717a', 'no upstream': '#71717a', 'root offline': '#71717a',
  diverged: '#D81E5B', conflict: '#D81E5B', error: '#D81E5B',
  'upstream gone': '#D81E5B',
};

const lightPalette: Record<string, string> = {
  clean: '#166534', behind: '#a21caf', dirty: '#c2410c',
  ahead: '#2563eb', syncing: '#2563eb', cloning: '#2563eb', fetching: '#2563eb',
  'not cloned': '#52525b', 'no upstream': '#52525b', 'root offline': '#52525b',
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
    // Its clone root is missing (#149): an unmounted drive, not a problem.
    'root offline': '◌',
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
  return status === 'unknown' || status === 'pending' || fetching;
}

export function rowGlyph(status: string, fetching: boolean): string {
  return rowBusy(status, fetching) ? '↻' : statusSymbol(status);
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

// ── Detail panel ──
// Statuses with no available clone: an open detail panel folds when its
// row reaches one of them, and clicking such a row does nothing.
const noDetailStatuses = new Set(['not cloned', 'root offline', 'cloning', 'syncing', 'pending']);

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

export function checkedAgo(checkedAt: string, language: string, now: number): string | null {
  const checked = Date.parse(checkedAt);
  if (!Number.isFinite(checked)) return null;
  const age = Math.max(0, now - checked);
  const relative = new Intl.RelativeTimeFormat(language, { numeric: 'auto' });
  if (age < 60_000) return relative.format(0, 'second');
  if (age < 3_600_000) return relative.format(-Math.floor(age / 60_000), 'minute');
  if (age < DAY) return relative.format(-Math.floor(age / 3_600_000), 'hour');
  return relative.format(-Math.floor(age / DAY), 'day');
}
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
    key: simple && word === 'offline' ? 'simple.connectionCheck' : `health.${word}${simple ? 'Simple' : ''}`,
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

export interface StatusBatch {
  passID: number;
  revision: number;
  items: StatusResult[] | null;
  done: boolean;
}

export function acceptStatusBatch(batch: StatusBatch, passID: number, revision: number | undefined): boolean {
  return batch.passID === passID && batch.revision === revision;
}


// Paint the first results immediately. Later backend events share a render
// while local git processes, fleet checks and the webview compete for CPU.
export function statusPaintQueue(apply: (rows: StatusResult[]) => void) {
  let rows: StatusResult[] = [];
  let timer: ReturnType<typeof setTimeout> | undefined;
  let first = true;
  const discard = () => {
    clearTimeout(timer);
    timer = undefined;
    rows = [];
  };
  const flush = () => {
    const ready = rows;
    discard();
    if (ready.length) apply(ready);
  };
  return {
    push(batch: StatusResult[]) {
      if (first) {
        first = false;
        apply(batch);
      } else {
        rows.push(...batch);
        timer ??= setTimeout(flush, 125);
      }
    },
    flush,
    discard,
  };
}
