// The host of a remote window (#243), for the launchers: which editors open
// its folders, and which actions have no remote equivalent. The Go side
// decides and refuses; this module only shows the controls disabled with
// the reason. It has no runtime imports, so remotehost.test.mjs can load it
// alone.

// RemoteHost is what a launcher needs to know: null outside a remote
// window. editors are the editor command names that open remote folders
// (pkg/remote.RemoteEditors, sent in the remote state); editor is the one
// chosen for this remote in Settings, '' for Auto (#287).
export type RemoteHost = { editors: string[]; editor: string } | null;

// The reason a launcher is off in a remote window, or '' when it works.
export type RemoteOff = '' | 'unavailable' | 'editor' | 'chosen';

// hostFromState turns the remote state of a remote window into its host.
export function hostFromState(state: { remoteEditors?: string[] | null; editor?: string | null } | null | undefined): RemoteHost {
  if (!state) return null;
  return {
    editors: Array.isArray(state.remoteEditors) ? state.remoteEditors.filter((e) => typeof e === 'string') : [],
    editor: typeof state.editor === 'string' ? state.editor : '',
  };
}

// editorName is the name of an editor command: its last path element,
// lowercased, without .exe, .cmd or .bat (pkg/remote.EditorName).
export function editorName(command: string): string {
  return (command.split(/[\\/]/).pop() || '').toLowerCase().replace(/\.(exe|cmd|bat)$/, '');
}

// editorOff: '' when the editor opens folders of the host (or the window
// is local), 'editor' when it cannot, and 'chosen' when this remote has
// another editor chosen in Settings.
export function editorOff(command: string, host: RemoteHost): RemoteOff {
  if (!host) return '';
  const name = editorName(command);
  if (!host.editors.includes(name)) return 'editor';
  return host.editor && host.editor !== name ? 'chosen' : '';
}

// remoteEditorChoices are the configured editors a remote can be set to in
// Settings: those whose command opens remote folders.
export function remoteEditorChoices<E extends { command: string }>(editors: E[], remoteEditors: string[]): E[] {
  return editors.filter((e) => remoteEditors.includes(editorName(e.command)));
}

// unavailableOff: the file manager, the account folder and the host's
// config file have no remote equivalent.
export function unavailableOff(host: RemoteHost): RemoteOff {
  return host ? 'unavailable' : '';
}

// offTip is the catalog key of the tooltip of a control that is off.
export function offTip(off: RemoteOff): string {
  switch (off) {
    case 'editor': return 'remote.editorNoRemoteTip';
    case 'chosen': return 'remote.editorNotChosenTip';
    case 'unavailable': return 'remote.notAvailableTip';
  }
  return '';
}
