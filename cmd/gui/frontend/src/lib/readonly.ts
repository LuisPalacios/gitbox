// The read-only remote (#240): a remote window shows the host's data and
// changes nothing there until P5. The Go side refuses every mutation before
// it leaves this machine; this module is the frontend's backstop, so a
// control that was missed fails at once with the read-only message instead
// of half-starting an action. Disabled buttons are only the visible part.
// It has no runtime imports, so readonly.test.mjs can load it alone.

// The bridge calls that change the host, or launch a local program on one
// of its paths (P6). Every other call is a read or a client preference.
export const READ_ONLY_BLOCKED: ReadonlySet<string> = new Set([
  'acknowledgeConfigError', 'addAccount', 'addDiscoveredRepos', 'addDiscoveredReposToFolder',
  'addMirrorGroup', 'addMirrorRepo', 'addRoot', 'adoptClones', 'applyDiscoveredMirrors',
  'changeCredentialType', 'cloneRepo', 'confirmSweep', 'createNewRepo', 'credentialDelete',
  'credentialRegenerateSSH', 'credentialSetupGCM', 'credentialSetupSSH', 'credentialStoreToken',
  'deleteAccount', 'deleteMirrorGroup', 'deleteMirrorRepo', 'deleteRepo', 'discardAccount',
  'fixGlobalGCMConfig', 'ignoreClone', 'initRoot', 'installGlobalGitignore', 'moveRepo',
  'pullRepo', 'reconcile', 'refreshAIHarnesses', 'removeGlobalIdentity', 'removeRoot',
  'renameAccount', 'repairConfig', 'restoreFromBackup', 'setCheckGlobalGitignore',
  'setDefaultRoot', 'setNestedScanDepth', 'setPRBadgesEnabled', 'setPRIncludeDrafts',
  'setRepoContainer', 'setRepoWorkspace', 'setupMirrorRepo', 'updateAccount',
  'openAccountFolder', 'openAccountInAIHarness', 'openAccountInApp', 'openAccountProfile',
  'openFileInEditor', 'openInAIHarness', 'openInApp', 'openInExplorer', 'openProfile',
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
// isReadOnly() holds. message() gives the translated refusal.
export function guardCalls<T extends Calls>(calls: T, isReadOnly: () => boolean, message: () => string): T {
  const out: Record<string, unknown> = {};
  for (const [name, fn] of Object.entries(calls)) {
    out[name] = READ_ONLY_BLOCKED.has(name)
      ? (...args: never[]) => (isReadOnly() ? Promise.reject(new ReadOnlyError(message())) : fn(...args))
      : fn;
  }
  return out as T;
}

// The automatic work that starts when the window regains focus: a remote
// window re-reads the host's config, but neither reconciles it nor
// re-probes the AI harnesses, which both save.
export function focusWork(readOnly: boolean): { reload: boolean; reconcile: boolean; refreshHarnesses: boolean } {
  return { reload: true, reconcile: !readOnly, refreshHarnesses: !readOnly };
}
