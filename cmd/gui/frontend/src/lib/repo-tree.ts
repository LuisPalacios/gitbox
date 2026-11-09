import type { SourceDTO } from './types';

export interface RepoRow {
  key: string;
  sourceKey: string;
  repoName: string;
  depth: number;
  childCount: number;
  descendants: string[];
  // How many of the row's children are placeholders.
  placeholders: number;
  // Set on a placeholder row: a folder the parent container's workspace
  // file lists that is not cloned yet.
  placeholder?: Placeholder;
}

// A folder a container's .code-workspace lists that is not cloned yet; path
// is relative to the container.
export interface Placeholder { name: string; path: string }

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });

// Clone keys ("source/org/repo") in alphabetical order of the name a row
// shows (the last segment), ignoring case; the full key breaks ties.
export function compareRepoKeys(a: string, b: string): number {
  const name = (key: string) => key.slice(key.lastIndexOf('/') + 1);
  return collator.compare(name(a), name(b)) || collator.compare(a, b);
}

// I keep ownership in each row even when a clone lives under another source's container.
// Siblings, at every depth, sort alphabetically (compareRepoKeys). A
// container's placeholders (keyed by its "source/repo") follow its clones,
// in workspace file order.
export function repoTreeRows(sources: Record<string, SourceDTO>, nesting: Record<string, string>, collapsed: Set<string>, placeholders: Record<string, Placeholder[]> = {}): Record<string, RepoRow[]> {
  const refs = new Map<string, { sourceKey: string; repoName: string }>();
  for (const [sourceKey, source] of Object.entries(sources)) {
    const order = [...new Set([...(source.repoOrder || []), ...Object.keys(source.repos)])];
    for (const repoName of order) {
      if (source.repos[repoName]) refs.set(`${sourceKey}/${repoName}`, { sourceKey, repoName });
    }
  }
  const parents = new Map<string, string>();
  for (const key of refs.keys()) {
    const parent = nesting[key];
    if (parent && parent !== key && refs.has(parent)) parents.set(key, parent);
  }
  // I break malformed cycles before walking, so every clone has exactly one row.
  for (const key of refs.keys()) {
    const seen = new Set([key]);
    let parent = parents.get(key);
    while (parent) {
      if (seen.has(parent)) { parents.delete(key); break; }
      seen.add(parent);
      parent = parents.get(parent);
    }
  }
  const children = new Map<string, string[]>();
  for (const key of refs.keys()) {
    const parent = parents.get(key);
    if (parent) children.set(parent, [...(children.get(parent) || []), key]);
  }
  const descendantsOf = (key: string): string[] => (children.get(key) || []).flatMap(child => [child, ...descendantsOf(child)]);
  const ordered = (keys: string[]): string[] => [...keys].sort(compareRepoKeys);
  const rows: Record<string, RepoRow[]> = Object.fromEntries(Object.keys(sources).map(key => [key, []]));
  const walk = (key: string, source: string, depth: number) => {
    const ref = refs.get(key)!;
    const waiting = placeholders[key] || [];
    rows[source].push({ key, ...ref, depth, childCount: (children.get(key) || []).length + waiting.length, descendants: descendantsOf(key), placeholders: waiting.length });
    if (collapsed.has(key)) return;
    for (const child of ordered(children.get(key) || [])) walk(child, source, depth + 1);
    for (const p of waiting) {
      rows[source].push({ key: `${key}#${p.path}`, sourceKey: ref.sourceKey, repoName: p.name, depth: depth + 1, childCount: 0, descendants: [], placeholders: 0, placeholder: p });
    }
  };
  for (const key of ordered([...refs.keys()].filter(key => !parents.has(key)))) walk(key, refs.get(key)!.sourceKey, 0);
  return rows;
}

// One account's share of a Needs attention group, with how many clones it holds.
export interface AccountRows { sourceKey: string; rows: RepoRow[]; count: number }

// I split one Needs attention group (#148) by account, alphabetically, each
// as a tree built from the group's clones only: a container nests the clones
// beside it, and a clone whose container sits in another group stays on top.
export function groupTreeRows(keys: string[], sources: Record<string, SourceDTO>, nesting: Record<string, string>, collapsed: Set<string>): AccountRows[] {
  const picked: Record<string, SourceDTO> = {};
  for (const key of keys) {
    const slash = key.indexOf('/');
    const sourceKey = key.slice(0, slash), repoName = key.slice(slash + 1);
    const source = sources[sourceKey];
    if (!source?.repos[repoName]) continue;
    (picked[sourceKey] ??= { ...source, repos: {} }).repos[repoName] = source.repos[repoName];
  }
  const tree = repoTreeRows(picked, nesting, collapsed);
  return Object.keys(tree).filter(sourceKey => tree[sourceKey].length > 0).sort(collator.compare)
    .map(sourceKey => ({ sourceKey, rows: tree[sourceKey], count: new Set(tree[sourceKey].flatMap(row => [row.key, ...row.descendants])).size }));
}
