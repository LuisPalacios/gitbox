// Navigation views and the Needs attention list (issue #138).
//
// Kept free of runtime imports so lib/views.test.mjs can load it on its
// own: the status rules come in as functions (rankOf) from lib/status.ts.

export type View = 'attention' | 'all' | 'mirrors' | 'workspaces' | `account:${string}`;

export const ACCOUNT_VIEW = 'account:';

// The view to show for a saved or requested one. A missing account, an
// Advanced-only view in Simple, or anything unknown shows Needs attention.
// The saved value itself stays, so switching back to Advanced (or the
// account coming back) restores it.
export function resolveView(view: string | null | undefined, accountKeys: string[], simple: boolean): View {
  if (view === 'attention' || view === 'all') return view;
  if (view === 'mirrors' || view === 'workspaces') return simple ? 'attention' : view;
  if (view && view.startsWith(ACCOUNT_VIEW) && accountKeys.includes(view.slice(ACCOUNT_VIEW.length))) return view as View;
  return 'attention';
}

export function accountView(accountKey: string): View {
  return `${ACCOUNT_VIEW}${accountKey}`;
}

// The account a view shows, or '' for the other views.
export function viewAccount(view: string): string {
  return view.startsWith(ACCOUNT_VIEW) ? view.slice(ACCOUNT_VIEW.length) : '';
}

// One row of Needs attention: its key, the rank group it shows in, and
// whether it has turned fine since the view opened (shown faded).
export interface AttentionItem { key: string; rank: number; settled: boolean }

// The Needs attention rows, worst group first and in the given order inside
// each group. rankOf returns the attention rank (calm = calmRank). pinned
// holds the rank each row had when the view first showed it: a pinned row
// keeps that place, and stays (settled) after it turns fine, until the view
// closes and the caller clears pinned.
export function attentionItems(keys: string[], rankOf: (key: string) => number, calmRank: number, pinned: Record<string, number> = {}): AttentionItem[] {
  const items: AttentionItem[] = [];
  for (const key of keys) {
    const now = rankOf(key);
    const was = pinned[key];
    if (was !== undefined) items.push({ key, rank: was, settled: now >= calmRank });
    else if (now < calmRank) items.push({ key, rank: now, settled: false });
  }
  return items.sort((a, b) => a.rank - b.rank);
}

// pinAttention records the rank of every row on screen that isn't pinned
// yet, so the next render keeps them in place.
export function pinAttention(items: AttentionItem[], pinned: Record<string, number>): Record<string, number> {
  const next = { ...pinned };
  for (const item of items) if (next[item.key] === undefined) next[item.key] = item.rank;
  return next;
}

// How many repos need me, per account and in total. Accounts whose
// credential needs a fix (broken) count once each toward the total, since
// they head the Needs attention view.
export function attentionCounts(keysByAccount: Record<string, string[]>, rankOf: (key: string) => number, calmRank: number, broken: string[] = []): { total: number; byAccount: Record<string, number> } {
  const byAccount: Record<string, number> = {};
  const seen = new Set<string>();
  let total = 0;
  for (const [account, keys] of Object.entries(keysByAccount)) {
    byAccount[account] = 0;
    for (const key of keys) {
      if (seen.has(key)) continue;
      seen.add(key);
      if (rankOf(key) < calmRank) { byAccount[account]++; total++; }
    }
  }
  return { total: total + new Set(broken).size, byAccount };
}
