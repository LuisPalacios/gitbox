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
// one action in the repo row.
export type SimpleKind = 'saved' | 'unsaved' | 'update' | 'online' | 'help' | 'checking' | 'busy';

const simpleKinds: Record<string, SimpleKind> = {
  clean: 'saved',
  dirty: 'unsaved', ahead: 'unsaved', 'no upstream': 'unsaved',
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
  saved: 'clean', unsaved: 'dirty', update: 'behind', online: 'not cloned',
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
  return ['unsaved', 'help'].includes(simpleKind(status));
}

// ── Advanced label ──
// The one-word label an Advanced row shows for a settled status, with the
// palette entry that colours it. null means the row shows its count badges
// instead (dirty, ahead, behind, diverged, conflict).
export function advancedLabel(status: string, isDefault: boolean | undefined): { key: string; color: string } | null {
  switch (status) {
    case 'clean': return { key: 'repo.synced', color: 'clean' };
    case 'not cloned': return { key: 'repo.notLocal', color: 'not cloned' };
    case 'no upstream': return isDefault ? { key: 'repo.noUpstream', color: 'no upstream' } : { key: 'repo.localBranch', color: 'clean' };
    case 'error': return { key: 'repo.error', color: 'error' };
    case 'upstream gone': return { key: 'repo.upstreamGone', color: 'upstream gone' };
    default: return null;
  }
}

// ── Counts ──
// A count badge: the RepoState field it reads, its glyph, the palette entry
// that colours it and the word that follows the number ("3 behind").
export interface CountBadge { field: 'behind' | 'ahead' | 'modified' | 'untracked'; glyph: string; color: string; word: string }

export const COUNT_BADGES: CountBadge[] = [
  { field: 'behind', glyph: '↓', color: 'behind', word: 'behind' },
  { field: 'ahead', glyph: '↑', color: 'ahead', word: 'ahead' },
  { field: 'modified', glyph: '✎', color: 'dirty', word: 'changed' },
  { field: 'untracked', glyph: '?', color: 'not cloned', word: 'untracked' },
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
export const DISCOVER_DISABLED = 'No working credential for this account';
export const CREATE_DISABLED = 'Needs a working API credential (GCM that accepts the API, or a companion PAT)';

// Suffix of the account card's cred-badge-* class for a credential status.
export function credBadgeClass(status: string): string {
  return status === 'ok' ? 'ok' : status === 'error' ? 'err' : status === 'warning' ? 'warn' : status === 'offline' ? 'offline' : status === 'none' ? 'none' : status === 'unknown' ? 'pending' : '';
}

// Text of the account card's credential badge.
export function credBadgeLabel(status: string, credentialType: string | undefined): string {
  return status === 'unknown' ? '···' : status === 'none' ? 'config' : status === 'offline' ? 'offline' : credentialType || 'gcm';
}
