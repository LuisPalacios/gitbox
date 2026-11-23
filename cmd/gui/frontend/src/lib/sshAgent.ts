// Which key git on a remote host uses for an SSH account (#244). When the
// connection forwards this computer's ssh agent (ssh -A), git on the host
// can reach those keys too. The Host entry gitbox writes pins the account's
// own key (IdentitiesOnly yes), so a forwarded key only matters when the
// entry was edited without it. The Go side reports fingerprints only
// (CredentialSSHAgentReport); this module picks what the window says. It
// has no runtime imports, so ssh-agent.test.mjs can load it alone.

export type AgentReport = {
  keyFile: string;
  keyFileFingerprint: string;
  agentFingerprints: string[] | null;
  agentForwarded: boolean;
  match: boolean;
  identitiesOnly: boolean;
};

// pinned: the agent's other keys reach the host, git still uses the
// account's key. unpinned: git on the host may sign in with one of them.
// same: the agent holds the account's key too.
export type AgentNote = '' | 'pinned' | 'unpinned' | 'same';

// agentNote is what to say about a report, in a remote window only: a
// local window's agent is not forwarded from anywhere.
export function agentNote(report: AgentReport | null | undefined, remoteWindow: boolean): AgentNote {
  if (!remoteWindow || !report || !report.agentForwarded) return '';
  if (report.match) return 'same';
  if (!Array.isArray(report.agentFingerprints) || report.agentFingerprints.length === 0) return '';
  return report.identitiesOnly ? 'pinned' : 'unpinned';
}

const KEYS: Record<Exclude<AgentNote, ''>, { simple: string; advanced: string }> = {
  pinned: { simple: 'cred.agentPinned', advanced: 'cred.agentPinnedAdv' },
  unpinned: { simple: 'cred.agentUnpinned', advanced: 'cred.agentUnpinnedAdv' },
  same: { simple: 'cred.agentSame', advanced: 'cred.agentSameAdv' },
};

// agentNoteKey is the catalog key of a note: Simple mode names no ssh
// options.
export function agentNoteKey(note: AgentNote, simple: boolean): string {
  if (!note) return '';
  return simple ? KEYS[note].simple : KEYS[note].advanced;
}
