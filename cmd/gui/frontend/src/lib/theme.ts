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

export type ThemeChoice = 'system' | 'light' | 'dark';

// The saved global.theme as a choice; anything but light or dark (absent
// included) follows the OS.
export function themeChoiceOf(saved: string | undefined): ThemeChoice {
  return saved === 'light' || saved === 'dark' ? saved : 'system';
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

// Account names in the account groups take their provider's brand colour
// (the logo's main hue), darker in the light theme and lighter in the dark
// one so it reads on the card. An unknown provider keeps the text colour.
const providerColors: Record<string, [light: string, dark: string]> = {
  github: ['#4a56b0', '#8f9cf0'],
  gitlab: ['#c4501a', '#fc8a4f'],
  gitea: ['#4d7c1f', '#8bc34a'],
  forgejo: ['#b91c1c', '#f0605a'],
  bitbucket: ['#0052cc', '#4c9aff'],
};

export function providerColor(p: string | undefined, theme: string): string {
  const c = p ? providerColors[p] : undefined;
  return c ? c[theme === 'light' ? 0 : 1] : '';
}

export function providerLabel(p: string): string {
  const map: Record<string, string> = {
    github: 'GitHub', forgejo: 'Forgejo', gitea: 'Gitea',
    gitlab: 'GitLab', bitbucket: 'Bitbucket',
  };
  return map[p] || p;
}
