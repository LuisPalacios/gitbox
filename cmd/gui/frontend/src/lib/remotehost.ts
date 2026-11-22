// The host of a remote window (#243), for the launchers: which editors open
// its folders, and which actions have no remote equivalent. The Go side
// decides and refuses; this module only shows the controls disabled with
// the reason. It has no runtime imports, so remotehost.test.mjs can load it
// alone.

// RemoteHost is what a launcher needs to know: null outside a remote
// window. editors are the editor command names that open remote folders
// (pkg/remote.RemoteEditors, sent in the remote state).
export type RemoteHost = { editors: string[] } | null;

// The reason a launcher is off in a remote window, or '' when it works.
export type RemoteOff = '' | 'unavailable' | 'editor';

// hostFromState turns the remote state of a remote window into its host.
export function hostFromState(state: { remoteEditors?: string[] | null } | null | undefined): RemoteHost {
  if (!state) return null;
  return { editors: Array.isArray(state.remoteEditors) ? state.remoteEditors.filter((e) => typeof e === 'string') : [] };
}

// editorName is the name of an editor command: its last path element,
// lowercased, without .exe, .cmd or .bat (pkg/remote.EditorName).
export function editorName(command: string): string {
  return (command.split(/[\\/]/).pop() || '').toLowerCase().replace(/\.(exe|cmd|bat)$/, '');
}

// editorOff: '' when the editor opens folders of the host (or the window
// is local), 'editor' otherwise.
export function editorOff(command: string, host: RemoteHost): RemoteOff {
  if (!host) return '';
  return host.editors.includes(editorName(command)) ? '' : 'editor';
}

// unavailableOff: the file manager, the account folder and the host's
// config file have no remote equivalent.
export function unavailableOff(host: RemoteHost): RemoteOff {
  return host ? 'unavailable' : '';
}

// offTip is the catalog key of the tooltip of a control that is off.
export function offTip(off: RemoteOff): string {
  return off === 'editor' ? 'remote.editorNoRemoteTip' : off === 'unavailable' ? 'remote.notAvailableTip' : '';
}
