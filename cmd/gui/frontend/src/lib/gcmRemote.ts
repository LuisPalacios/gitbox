// GCM on a remote host (#242): a remote window offers no GCM, because its
// host refuses it (gcm_unsupported_remote). The reason names the store GCM
// would use on the host's system; the same catalog keys translate the
// host's refusal. It has no runtime imports, so a test can load it alone.

const STORE_OS: ReadonlySet<string> = new Set(['linux', 'darwin', 'windows']);

// gcmRemoteReasonKey is the catalog key of the reason for a host running
// serverOS.
export function gcmRemoteReasonKey(serverOS: string): string {
  return STORE_OS.has(serverOS) ? `error.gcm_unsupported_remote.${serverOS}` : 'error.gcm_unsupported_remote';
}

// gcmOffered: GCM is a choice in this window.
export function gcmOffered(remoteWindow: boolean): boolean {
  return !remoteWindow;
}

// defaultCredentialType is the type a new account starts with: GCM where it
// is offered, a token on a remote host.
export function defaultCredentialType(remoteWindow: boolean): 'gcm' | 'token' {
  return gcmOffered(remoteWindow) ? 'gcm' : 'token';
}
