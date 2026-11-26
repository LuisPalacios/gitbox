// Codes describe observed failures. Context supplies only actions it can perform.
export type ErrorAction = 'retry' | 'credential' | 'token' | 'editor' | 'terminal' | 'upstream';
export type ErrorActions = Partial<Record<ErrorAction, () => void>>;
export interface ErrorInfo {
  code?: string;
  detail?: string;
  retryAt?: number;
  // git.unsafe_config: the setting that paused the clone and where it lives.
  unsafe?: UnsafeConfig;
}

export interface UnsafeConfig {
  setting: string;
  scope: string;
}

// The catalog name of each setting a clone can be paused for (#344).
const UNSAFE_SETTINGS: Record<string, string> = {
  'core.hooksPath': 'hooksPath',
  'core.fsmonitor': 'fsmonitor',
  'url.*.insteadOf': 'insteadOf',
};
const UNSAFE_SCOPES = new Set(['local', 'worktree', 'include']);

// unsafeHelp is what the window says about a clone paused for safety: the
// catalog keys of the reason and of where the setting lives, and the commands
// that show and remove it. Unknown names, from an older or untrusted server,
// give undefined and the generic text stands.
export function unsafeHelp(unsafe: UnsafeConfig | undefined, path?: string) {
  const name = unsafe && Object.hasOwn(UNSAFE_SETTINGS, unsafe.setting) ? UNSAFE_SETTINGS[unsafe.setting] : '';
  if (!unsafe || !name || !UNSAFE_SCOPES.has(unsafe.scope)) return undefined;
  const git = path ? `git -C "${path}" config` : 'git config';
  const commands = [name === 'insteadOf'
    ? `${git} --show-origin --show-scope --get-regexp "^url\\..*\\.insteadof$"`
    : `${git} --show-origin --show-scope --get-all ${unsafe.setting}`];
  // An included file holds the setting itself, and an insteadOf rule is
  // named by its own URL: the first command shows which file to edit.
  if (name !== 'insteadOf' && unsafe.scope !== 'include') commands.push(`${git} --${unsafe.scope} --unset ${unsafe.setting}`);
  return {
    reasonKey: `error.git.unsafe_config.${name}`,
    whereKey: `error.git.unsafe_config.scope.${unsafe.scope}`,
    commands,
  };
}

const actions = {
  'git.auth_failed': 'credential',
  'git.not_fast_forward': 'terminal',
  'git.no_upstream': 'terminal',
  'git.offline': 'retry',
  'git.timeout': 'retry',
  'git.cancelled': null,
  'git.not_a_repo': null,
  'git.lock_held': 'retry',
  'git.repo_not_found': null,
  'git.unsafe_config': 'terminal',
  'git.unknown': null,
  'provider.unauthorized': 'credential',
  'provider.scope_denied': 'token',
  'provider.forbidden': null,
  'provider.rate_limited': 'retry',
  'provider.not_found': null,
  'provider.server_error': 'retry',
  'provider.offline': 'retry',
  'provider.timeout': 'retry',
  'provider.unknown': null,
  'credential.missing': 'credential',
  'repo.dirty': 'editor',
  'repo.diverged': 'terminal',
  'repo.conflict': 'terminal',
  'repo.upstream_gone': 'upstream',
  'repo.root_offline': null,
  'unknown': null,
} satisfies Record<string, ErrorAction | null>;

export function explain(code: string | undefined) {
  const known = code && Object.hasOwn(actions, code) ? code as keyof typeof actions : 'unknown';
  const kind = actions[known];
  return {
    titleKey: `error.${known}.title`,
    bodyKey: `error.${known}.body`,
    action: kind ? { labelKey: `error.${known}.action`, kind } : undefined,
    unknown: known === 'unknown' || known === 'git.unknown' || known === 'provider.unknown',
  };
}

export function errorAction(info: ErrorInfo, available: ErrorActions, now: number) {
  const action = explain(info.code).action;
  if (!action || !available[action.kind]) return undefined;
  if (info.code === 'provider.rate_limited' && (!info.retryAt || now < info.retryAt)) return undefined;
  return action;
}

// Legacy failures have no typed cause. Preserve only bounded diagnostic text.
export function errorInfo(info?: ErrorInfo, detail?: string): ErrorInfo {
  const text = typeof info?.detail === 'string' ? info.detail : detail;
  return { ...info, detail: typeof text === 'string' ? text.slice(0, 300) : '' };
}

export function caughtIssue(error: unknown): ErrorInfo {
  if (error && typeof error === 'object' && 'code' in error && typeof error.code === 'string') {
    const { code, detail, retryAt } = error as ErrorInfo;
    return errorInfo({ code, detail, retryAt });
  }
  return {};
}

export function accountIssue(status: { status: string; problem?: ErrorInfo; primaryMsg?: string }): ErrorInfo {
  if (status.problem) return errorInfo(status.problem);
  if (status.status === 'none') return { code: 'credential.missing' };
  if (status.status === 'offline') return { code: 'provider.offline' };
  return errorInfo(undefined, status.primaryMsg);
}

export function repoIssue(state: { status: string; pullSkipped?: string; behind?: number; error?: string; problem?: ErrorInfo }): ErrorInfo | undefined {
  switch (state.status) {
    case 'root offline': return { code: 'repo.root_offline' };
    case 'upstream gone': return { code: 'repo.upstream_gone' };
    case 'diverged': return { code: 'repo.diverged' };
    case 'conflict': return { code: 'repo.conflict' };
  }
  if (state.pullSkipped === 'dirty' || (state.status === 'dirty' && (state.behind ?? 0) > 0)) return { code: 'repo.dirty' };
  if (state.problem || state.error || state.status === 'error') return errorInfo(state.problem, state.error);
}
