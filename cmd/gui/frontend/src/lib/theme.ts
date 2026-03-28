// Theme helpers — color palettes matching prototype-final.svelte

const darkPalette: Record<string, string> = {
  clean: '#61fd5f', behind: '#D91C9A', dirty: '#F07623',
  ahead: '#4B95E9', syncing: '#4B95E9', cloning: '#4B95E9', fetching: '#4B95E9',
  'not cloned': '#71717a', 'no upstream': '#71717a',
  diverged: '#D81E5B', conflict: '#D81E5B', error: '#D81E5B',
};

const lightPalette: Record<string, string> = {
  clean: '#166534', behind: '#a21caf', dirty: '#c2410c',
  ahead: '#2563eb', syncing: '#2563eb', cloning: '#2563eb', fetching: '#2563eb',
  'not cloned': '#52525b', 'no upstream': '#52525b',
  diverged: '#be123c', conflict: '#be123c', error: '#be123c',
};

export function statusColor(status: string, theme: string): string {
  const palette = theme === 'light' ? lightPalette : darkPalette;
  return palette[status] || (theme === 'light' ? '#52525b' : '#71717a');
}

export function credColor(status: string, theme: string): string {
  if (status === 'none') return theme === 'light' ? '#2563eb' : '#4B95E9';
  if (theme === 'light') {
    return status === 'ok' ? '#166534' : status === 'warning' ? '#c2410c' : '#be123c';
  }
  return status === 'ok' ? '#61fd5f' : status === 'warning' ? '#F07623' : '#D81E5B';
}

export function statusLabel(status: string, behind: number, modified: number): string {
  const map: Record<string, string> = {
    clean: 'Synced',
    behind: `${behind} behind`,
    dirty: `${modified} local change${modified > 1 ? 's' : ''}`,
    ahead: 'Ahead',
    syncing: 'Refreshing...',
    cloning: 'Bringing local...',
    fetching: 'Fetching...',
    'not cloned': 'Not local',
    'no upstream': 'No upstream',
    diverged: 'Diverged',
    conflict: 'Conflict',
    error: 'Error',
  };
  return map[status] || status;
}

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
  };
  return map[status] || '?';
}
