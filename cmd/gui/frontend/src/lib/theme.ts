// Theme helpers. Status colours, shapes and Simple kinds live in
// lib/status.ts; this module keeps the credential colour and provider data.

export function credColor(status: string, theme: string): string {
  if (status === 'none') return theme === 'light' ? '#2563eb' : '#4B95E9';
  if (status === 'unknown') return theme === 'light' ? '#a1a1aa' : '#71717a';
  if (status === 'offline') return theme === 'light' ? '#be123c' : '#D81E5B';
  if (theme === 'light') {
    return status === 'ok' ? '#166534' : status === 'warning' ? '#c2410c' : '#be123c';
  }
  return status === 'ok' ? '#61fd5f' : status === 'warning' ? '#F07623' : '#D81E5B';
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
