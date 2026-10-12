// Theme helpers — color palettes matching prototype-final.svelte

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

export function credColor(status: string, theme: string): string {
  if (status === 'none') return theme === 'light' ? '#2563eb' : '#4B95E9';
  if (status === 'unknown') return theme === 'light' ? '#a1a1aa' : '#71717a';
  if (status === 'offline') return theme === 'light' ? '#be123c' : '#D81E5B';
  if (theme === 'light') {
    return status === 'ok' ? '#166534' : status === 'warning' ? '#c2410c' : '#be123c';
  }
  return status === 'ok' ? '#61fd5f' : status === 'warning' ? '#F07623' : '#D81E5B';
}

// Simple mode (issue #124) folds the git statuses into the one question a
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

// Default server URL per provider; Gitea and Forgejo are self-hosted, so
// they have none and the user types it.
export const providerURLs: Record<string, string> = {
  github: 'https://github.com',
  gitlab: 'https://gitlab.com',
  gitea: '',
  forgejo: '',
  bitbucket: 'https://bitbucket.org',
};

export function providerLabel(p: string): string {
  const map: Record<string, string> = {
    github: 'GitHub', forgejo: 'Forgejo', gitea: 'Gitea',
    gitlab: 'GitLab', bitbucket: 'Bitbucket',
  };
  return map[p] || p;
}

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
