import { readable, writable } from 'svelte/store';
import { follow } from './session/active';
import type { PRSummaryDTO } from './types';

// Every session keeps its own stores (lib/session/bundle.ts, #381). The
// exports below follow the active session (lib/session/active.ts), so a
// component reads what the session it shows holds. They are read-only: a
// session controller writes to its own bundle, and a component that
// changes session state writes to the bundle of the session its action
// started in (get(activeSession), captured before the action's first await).
export { activeSession } from './session/active';

// ── Config, and what derives from it ──
export const configStore = follow('configStore');
export const accounts = follow('accounts');
export const sources = follow('sources');

// ── Per-repo state (status + progress) and its summaries ──
// Key format: "sourceKey/repoKey"
export const repoStates = follow('repoStates');
export const summary = follow('summary');
export const accountStats = follow('accountStats');

// ── Mirrors ──
export const mirrors = follow('mirrors');
// Key format: "mirrorKey/repoKey"
export const mirrorStates = follow('mirrorStates');
export const mirrorSummary = follow('mirrorSummary');

// ── PR indicators (issue #29) ──
export const prsByAccount = follow('prsByAccount');
export const prSettings = follow('prSettings');

// ── Workspaces ──
export const workspacePlaceholders = follow('workspacePlaceholders');
export const workspaceMembers = follow('workspaceMembers');
export const workspaceUnknown = follow('workspaceUnknown');
export const workspaceDiagnostics = follow('workspaceDiagnostics');

// ── Remote state (#240, #243, #294) ──
export const remoteWindow = follow('remoteWindow');
export const blocked = follow('blocked');
export const remoteHost = follow('remoteHost');
export const remoteTextHost = follow('remoteTextHost');

// ── Process-wide ──
// Keep relative status captions current with one clock for all rows.
export const statusClock = readable(Date.now(), (set) => {
  set(Date.now());
  const timer = setInterval(() => set(Date.now()), 30_000);
  return () => clearInterval(timer);
});

// The window's theme: one for every session.
export const themeStore = writable<'light' | 'dark'>('dark');

// Lookup summary for one clone. Returns an empty summary when absent.
export function lookupPRSummary(
  byAccount: Record<string, Record<string, PRSummaryDTO>>,
  accountKey: string,
  repoKey: string
): PRSummaryDTO {
  const empty: PRSummaryDTO = { authored: [], reviewRequested: [] };
  const acct = byAccount[accountKey];
  if (!acct) return empty;
  return acct[repoKey.toLowerCase()] ?? empty;
}
