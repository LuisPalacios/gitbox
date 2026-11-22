// The read-only remote (#240): a remote window shows the host's data and
// changes nothing there until P5. The Go side refuses every mutation before
// it leaves this machine; this module is the frontend's backstop, so a
// control that was missed fails at once with the read-only message instead
// of half-starting an action. Disabled buttons are only the visible part.
// It has no runtime imports, so readonly.test.mjs can load it alone.

// The bridge calls that change the host, and the three launches that have
// no remote equivalent (the file manager, the account folder and the
// host's config file: the Go side refuses them with not_available_remote).
// The editor, terminal and AI harness launches open the host's folders
// over ssh (#243), and the harness refresh is a read. Every other call is a
// read or a client preference.
export const READ_ONLY_BLOCKED: ReadonlySet<string> = new Set([
  'acknowledgeConfigError', 'addAccount', 'addDiscoveredRepos', 'addDiscoveredReposToFolder',
  'addMirrorGroup', 'addMirrorRepo', 'addRoot', 'adoptClones', 'applyDiscoveredMirrors',
  'changeCredentialType', 'cloneRepo', 'confirmSweep', 'createNewRepo', 'credentialDelete',
  'credentialRegenerateSSH', 'credentialSetupGCM', 'credentialSetupSSH', 'credentialStoreToken',
  'deleteAccount', 'deleteMirrorGroup', 'deleteMirrorRepo', 'deleteRepo', 'discardAccount',
  'fixGlobalGCMConfig', 'ignoreClone', 'initRoot', 'installGlobalGitignore', 'moveRepo',
  'pullRepo', 'reconcile', 'removeGlobalIdentity', 'removeRoot',
  'renameAccount', 'repairConfig', 'restoreFromBackup', 'setCheckGlobalGitignore',
  'setDefaultRoot', 'setNestedScanDepth', 'setPRBadgesEnabled', 'setPRIncludeDrafts',
  'setRepoContainer', 'setRepoWorkspace', 'setupMirrorRepo', 'updateAccount',
  'openAccountFolder', 'openFileInEditor', 'openInExplorer',
]);

// ReadOnlyError is what a blocked call rejects with.
export class ReadOnlyError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'ReadOnlyError';
  }
}

type Calls = Record<string, (...args: never[]) => unknown>;

// guardCalls returns calls with every blocked one refusing while
// isReadOnly() holds, and every failure passed through normalize (see
// decodeWindowError). message() gives the translated refusal.
export function guardCalls<T extends Calls>(calls: T, isReadOnly: () => boolean, message: () => string, normalize: (e: unknown) => unknown = (e) => e): T {
  const out: Record<string, unknown> = {};
  for (const [name, fn] of Object.entries(calls)) {
    const call = (...args: never[]) => {
      const result = fn(...args);
      return result instanceof Promise ? result.catch((e: unknown) => { throw normalize(e); }) : result;
    };
    out[name] = READ_ONLY_BLOCKED.has(name)
      ? (...args: never[]) => (isReadOnly() ? Promise.reject(new ReadOnlyError(message())) : call(...args))
      : call;
  }
  return out as T;
}

// WindowError is a failed call of a remote window: its message is what to
// show, and code and hint say what failed (cmd/gui/errors.go, WindowErrorDTO).
export class WindowError extends Error {
  code: string;
  hint: string;
  constructor(message: string, code: string, hint: string) {
    super(message);
    this.name = 'WindowError';
    this.code = code;
    this.hint = hint;
  }
}

const IDENTIFIER = /^[A-Za-z0-9._-]{1,64}$/;

// decodeWindowError turns the JSON text a remote window's calls fail with
// back into a WindowError whose message is the translated hint, or the
// backend's text when the catalog has none (tr returns the key itself).
// Anything else, such as the plain text of a local window, comes back as it
// came.
export function decodeWindowError(e: unknown, tr: (key: string) => string): unknown {
  const text = e instanceof Error ? e.message : typeof e === 'string' ? e : '';
  if (!text.startsWith('{')) return e;
  let dto: { message?: unknown; code?: unknown; hint?: unknown };
  try { dto = JSON.parse(text); } catch { return e; }
  if (!dto || typeof dto.message !== 'string') return e;
  const code = typeof dto.code === 'string' && IDENTIFIER.test(dto.code) ? dto.code : '';
  const hint = typeof dto.hint === 'string' && IDENTIFIER.test(dto.hint) ? dto.hint : '';
  const translated = hint ? tr(hint) : '';
  return new WindowError(translated && translated !== hint ? translated : dto.message, code, hint);
}

// isDisconnectError: the call failed because the remote window was not
// connected.
export function isDisconnectError(e: unknown): boolean {
  return e instanceof WindowError && e.code === 'remote.not_connected';
}

// backgroundAllowed: a remote window starts no background call (the focus
// reload, the periodic fetch) while it is not connected; the disconnect
// banner already says why nothing changes.
export function backgroundAllowed(remoteWindow: boolean, state: string | undefined): boolean {
  return !remoteWindow || state === 'connected';
}

// reloadErrorShown: a failed reload raises the config error banner, unless
// it failed because the remote window was not connected.
export function reloadErrorShown(e: unknown, remoteWindow: boolean, state: string | undefined): boolean {
  return !isDisconnectError(e) && backgroundAllowed(remoteWindow, state);
}

// clearsOnReconnect: a new connection clears the errors the disconnection
// left behind.
export function clearsOnReconnect(previous: string | undefined, next: string): boolean {
  return next === 'connected' && previous !== 'connected';
}

// The automatic work that starts when the window regains focus: a remote
// window re-reads the host's config and re-probes its AI harnesses, which a
// read-only server keeps in memory (#243), but does not reconcile, which
// saves.
export function focusWork(readOnly: boolean): { reload: boolean; reconcile: boolean; refreshHarnesses: boolean } {
  return { reload: true, reconcile: !readOnly, refreshHarnesses: true };
}
